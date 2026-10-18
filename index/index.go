package index

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Result is one search hit.
type Result struct {
	ID       string  `json:"id"`
	Distance float32 `json:"distance"`
}

// Index is the interface shared by all index implementations.
type Index interface {
	Add(id string, vector []float32) error
	Delete(id string) bool
	Search(query []float32, k int) ([]Result, error)
	Len() int
	Dim() int
}

var (
	ErrDimension = errors.New("vector has wrong dimension")
	ErrEmptyID   = errors.New("id must not be empty")
)

func checkVector(dim int, v []float32) error {
	if len(v) != dim {
		return fmt.Errorf("%w: got %d, want %d", ErrDimension, len(v), dim)
	}
	return nil
}

// Flat is an exact index that compares the query with every vector. It is
// the reference implementation used to measure HNSW recall.
type Flat struct {
	mu      sync.RWMutex
	dim     int
	metric  Metric
	dist    DistanceFunc
	vectors map[string][]float32
}

// NewFlat returns an empty exact index.
func NewFlat(dim int, metric Metric) (*Flat, error) {
	f, err := metric.Func()
	if err != nil {
		return nil, err
	}
	return &Flat{dim: dim, metric: metric, dist: f, vectors: map[string][]float32{}}, nil
}

func (f *Flat) prepare(v []float32) []float32 {
	if f.metric == Cosine {
		return Normalize(v)
	}
	return append([]float32(nil), v...)
}

// Add inserts or replaces the vector stored under id.
func (f *Flat) Add(id string, v []float32) error {
	if id == "" {
		return ErrEmptyID
	}
	if err := checkVector(f.dim, v); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vectors[id] = f.prepare(v)
	return nil
}

// Delete removes id and reports whether it existed.
func (f *Flat) Delete(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.vectors[id]
	delete(f.vectors, id)
	return ok
}

// Search returns the k nearest vectors to query, closest first.
func (f *Flat) Search(query []float32, k int) ([]Result, error) {
	if err := checkVector(f.dim, query); err != nil {
		return nil, err
	}
	q := f.prepare(query)
	f.mu.RLock()
	res := make([]Result, 0, len(f.vectors))
	for id, v := range f.vectors {
		res = append(res, Result{ID: id, Distance: f.dist(q, v)})
	}
	f.mu.RUnlock()
	sortResults(res)
	if k < len(res) {
		res = res[:k]
	}
	return res, nil
}

func (f *Flat) Len() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.vectors)
}

func (f *Flat) Dim() int { return f.dim }

// sortResults orders by distance, breaking ties by id for determinism.
func sortResults(res []Result) {
	sort.Slice(res, func(i, j int) bool {
		if res[i].Distance != res[j].Distance {
			return res[i].Distance < res[j].Distance
		}
		return res[i].ID < res[j].ID
	})
}
