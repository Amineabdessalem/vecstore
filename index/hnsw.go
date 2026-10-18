package index

import (
	"container/heap"
	"math"
	"math/rand"
	"sync"
)

// HNSWConfig holds the tuning knobs of the HNSW graph.
type HNSWConfig struct {
	// M is the number of neighbours kept per node on upper layers; layer 0
	// keeps 2*M. Larger M improves recall at the cost of memory.
	M int
	// EfConstruction is the candidate list size used while inserting.
	EfConstruction int
	// EfSearch is the default candidate list size used while searching. It
	// is raised to k when k is larger.
	EfSearch int
	// Seed makes level assignment, and therefore the graph, reproducible.
	Seed int64
}

// DefaultHNSWConfig returns settings that work well for up to ~1M vectors.
func DefaultHNSWConfig() HNSWConfig {
	return HNSWConfig{M: 16, EfConstruction: 200, EfSearch: 64, Seed: 42}
}

type node struct {
	id        string
	vector    []float32
	neighbors [][]int32 // neighbors[layer] = node indices
	deleted   bool
}

// HNSW is an approximate nearest neighbour index based on Hierarchical
// Navigable Small World graphs (Malkov & Yashunin, 2016).
//
// Deletes are soft: the node stays in the graph for navigation but is never
// returned. Re-adding an existing id replaces it with a fresh node.
type HNSW struct {
	mu        sync.RWMutex
	cfg       HNSWConfig
	dim       int
	metric    Metric
	dist      DistanceFunc
	levelMult float64
	rng       *rand.Rand
	nodes     []*node
	byID      map[string]int32
	entry     int32
	maxLevel  int
	liveCount int
}

// NewHNSW returns an empty HNSW index.
func NewHNSW(dim int, metric Metric, cfg HNSWConfig) (*HNSW, error) {
	f, err := metric.Func()
	if err != nil {
		return nil, err
	}
	if cfg.M < 2 {
		cfg.M = 2
	}
	if cfg.EfConstruction < cfg.M {
		cfg.EfConstruction = cfg.M
	}
	if cfg.EfSearch < 1 {
		cfg.EfSearch = 1
	}
	return &HNSW{
		cfg:       cfg,
		dim:       dim,
		metric:    metric,
		dist:      f,
		levelMult: 1 / math.Log(float64(cfg.M)),
		rng:       rand.New(rand.NewSource(cfg.Seed)),
		byID:      map[string]int32{},
		entry:     -1,
	}, nil
}

func (h *HNSW) Dim() int           { return h.dim }
func (h *HNSW) Config() HNSWConfig { return h.cfg }
func (h *HNSW) Metric() Metric     { return h.metric }

func (h *HNSW) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.liveCount
}

func (h *HNSW) randomLevel() int {
	return int(math.Floor(-math.Log(1-h.rng.Float64()) * h.levelMult))
}

func (h *HNSW) maxNeighbors(layer int) int {
	if layer == 0 {
		return 2 * h.cfg.M
	}
	return h.cfg.M
}

// Add inserts id, replacing any vector already stored under it.
func (h *HNSW) Add(id string, vector []float32) error {
	if id == "" {
		return ErrEmptyID
	}
	if err := checkVector(h.dim, vector); err != nil {
		return err
	}
	v := append([]float32(nil), vector...)
	if h.metric == Cosine {
		v = Normalize(v)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.byID[id]; ok && !h.nodes[old].deleted {
		h.nodes[old].deleted = true
		h.liveCount--
	}
	h.insert(id, v, h.randomLevel())
	return nil
}

func (h *HNSW) insert(id string, v []float32, level int) {
	idx := int32(len(h.nodes))
	n := &node{id: id, vector: v, neighbors: make([][]int32, level+1)}
	h.nodes = append(h.nodes, n)
	h.byID[id] = idx
	h.liveCount++

	if h.entry < 0 {
		h.entry, h.maxLevel = idx, level
		return
	}

	ep := h.entry
	for l := h.maxLevel; l > level; l-- {
		ep = h.greedyClosest(v, ep, l)
	}
	for l := min(level, h.maxLevel); l >= 0; l-- {
		cands := h.searchLayer(v, []int32{ep}, h.cfg.EfConstruction, l)
		neigh := h.selectNeighbors(cands, h.cfg.M)
		n.neighbors[l] = neigh
		for _, nb := range neigh {
			h.link(nb, idx, l)
		}
		ep = cands[0].idx
	}
	if level > h.maxLevel {
		h.entry, h.maxLevel = idx, level
	}
}

// link adds a directed edge from -> to on layer l, pruning from's edge list
// back to its maximum size by keeping the closest neighbours.
func (h *HNSW) link(from, to int32, l int) {
	fn := h.nodes[from]
	fn.neighbors[l] = append(fn.neighbors[l], to)
	if len(fn.neighbors[l]) <= h.maxNeighbors(l) {
		return
	}
	cands := make([]candidate, len(fn.neighbors[l]))
	for i, nb := range fn.neighbors[l] {
		cands[i] = candidate{idx: nb, dist: h.dist(fn.vector, h.nodes[nb].vector)}
	}
	sortCandidates(cands)
	fn.neighbors[l] = h.selectNeighbors(cands, h.maxNeighbors(l))
}

// selectNeighbors applies the HNSW diversity heuristic: a candidate is kept
// only if it is closer to the new node than to any already-kept neighbour.
// Remaining slots are filled with the closest discarded candidates so nodes
// in tight clusters still get a full edge list. cands must be sorted.
func (h *HNSW) selectNeighbors(cands []candidate, m int) []int32 {
	kept := make([]int32, 0, m)
	var skipped []int32
	for _, c := range cands {
		if len(kept) >= m {
			break
		}
		good := true
		for _, k := range kept {
			if h.dist(h.nodes[c.idx].vector, h.nodes[k].vector) < c.dist {
				good = false
				break
			}
		}
		if good {
			kept = append(kept, c.idx)
		} else {
			skipped = append(skipped, c.idx)
		}
	}
	for _, s := range skipped {
		if len(kept) >= m {
			break
		}
		kept = append(kept, s)
	}
	return kept
}

func (h *HNSW) greedyClosest(q []float32, ep int32, l int) int32 {
	best, bestDist := ep, h.dist(q, h.nodes[ep].vector)
	for changed := true; changed; {
		changed = false
		for _, nb := range h.nodes[best].neighbors[l] {
			if d := h.dist(q, h.nodes[nb].vector); d < bestDist {
				best, bestDist, changed = nb, d, true
			}
		}
	}
	return best
}

// searchLayer is the beam search from the HNSW paper. It returns up to ef
// candidates sorted closest first.
func (h *HNSW) searchLayer(q []float32, eps []int32, ef, l int) []candidate {
	visited := map[int32]struct{}{}
	frontier := &minHeap{}
	results := &maxHeap{}
	for _, ep := range eps {
		c := candidate{idx: ep, dist: h.dist(q, h.nodes[ep].vector)}
		visited[ep] = struct{}{}
		heap.Push(frontier, c)
		heap.Push(results, c)
	}
	for frontier.Len() > 0 {
		c := heap.Pop(frontier).(candidate)
		if c.dist > (*results)[0].dist && results.Len() >= ef {
			break
		}
		for _, nb := range h.nodes[c.idx].neighbors[l] {
			if _, seen := visited[nb]; seen {
				continue
			}
			visited[nb] = struct{}{}
			d := h.dist(q, h.nodes[nb].vector)
			if results.Len() < ef || d < (*results)[0].dist {
				heap.Push(frontier, candidate{idx: nb, dist: d})
				heap.Push(results, candidate{idx: nb, dist: d})
				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}
	out := make([]candidate, results.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(results).(candidate)
	}
	return out
}

// Search returns the k approximate nearest neighbours of query.
func (h *HNSW) Search(query []float32, k int) ([]Result, error) {
	return h.SearchEf(query, k, h.cfg.EfSearch)
}

// SearchEf is Search with an explicit candidate list size.
func (h *HNSW) SearchEf(query []float32, k, ef int) ([]Result, error) {
	if err := checkVector(h.dim, query); err != nil {
		return nil, err
	}
	q := query
	if h.metric == Cosine {
		q = Normalize(query)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.entry < 0 || k <= 0 {
		return nil, nil
	}
	ep := h.entry
	for l := h.maxLevel; l > 0; l-- {
		ep = h.greedyClosest(q, ep, l)
	}
	// Ask for extra candidates so soft-deleted nodes don't starve the result.
	want := max(ef, k) + (len(h.nodes) - h.liveCount)
	cands := h.searchLayer(q, []int32{ep}, want, 0)
	res := make([]Result, 0, k)
	for _, c := range cands {
		if n := h.nodes[c.idx]; !n.deleted {
			res = append(res, Result{ID: n.id, Distance: c.dist})
			if len(res) == k {
				break
			}
		}
	}
	return res, nil
}

// Delete soft-deletes id and reports whether it was present.
func (h *HNSW) Delete(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	idx, ok := h.byID[id]
	if !ok || h.nodes[idx].deleted {
		return false
	}
	h.nodes[idx].deleted = true
	h.liveCount--
	return true
}

// Vector returns a copy of the stored (possibly normalised) vector for id.
func (h *HNSW) Vector(id string) ([]float32, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	idx, ok := h.byID[id]
	if !ok || h.nodes[idx].deleted {
		return nil, false
	}
	return append([]float32(nil), h.nodes[idx].vector...), true
}

type candidate struct {
	idx  int32
	dist float32
}

func sortCandidates(c []candidate) {
	h := minHeap(c)
	heap.Init(&h)
	out := make([]candidate, 0, len(c))
	for h.Len() > 0 {
		out = append(out, heap.Pop(&h).(candidate))
	}
	copy(c, out)
}

type minHeap []candidate

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].dist < h[j].dist }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(candidate)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

type maxHeap []candidate

func (h maxHeap) Len() int           { return len(h) }
func (h maxHeap) Less(i, j int) bool { return h[i].dist > h[j].dist }
func (h maxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxHeap) Push(x any)        { *h = append(*h, x.(candidate)) }
func (h *maxHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// Each calls fn for every live vector in insertion order.
func (h *HNSW) Each(fn func(id string, vector []float32)) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, n := range h.nodes {
		if !n.deleted {
			fn(n.id, n.vector)
		}
	}
}
