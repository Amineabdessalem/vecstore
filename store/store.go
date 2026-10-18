// Package store manages named collections of vectors with metadata on top
// of the HNSW index.
package store

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/Amineabdessalem/vecstore/index"
)

var (
	ErrNotFound = errors.New("collection not found")
	ErrExists   = errors.New("collection already exists")
	ErrBadName  = errors.New("collection name must match [a-zA-Z0-9_-]{1,64}")
	ErrBadDim   = errors.New("dimension must be between 1 and 4096")
)

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Metadata is arbitrary JSON-compatible data attached to a vector.
type Metadata map[string]any

// Record is a vector with its id and metadata.
type Record struct {
	ID       string    `json:"id"`
	Values   []float32 `json:"values"`
	Metadata Metadata  `json:"metadata,omitempty"`
}

// Hit is a search result.
type Hit struct {
	ID       string   `json:"id"`
	Distance float32  `json:"distance"`
	Metadata Metadata `json:"metadata,omitempty"`
}

// Filter keeps hits whose metadata equals every key/value pair it holds.
type Filter map[string]any

func (f Filter) match(m Metadata) bool {
	for k, v := range f {
		if fmt.Sprint(m[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// Collection is a single HNSW index plus metadata.
type Collection struct {
	Name string
	mu   sync.RWMutex
	idx  *index.HNSW
	meta map[string]Metadata
}

// Info describes a collection.
type Info struct {
	Name   string       `json:"name"`
	Dim    int          `json:"dim"`
	Metric index.Metric `json:"metric"`
	Count  int          `json:"count"`
}

func (c *Collection) Info() Info {
	return Info{Name: c.Name, Dim: c.idx.Dim(), Metric: c.idx.Metric(), Count: c.idx.Len()}
}

// Upsert inserts or replaces records. It validates every record first so a
// bad batch leaves the collection unchanged.
func (c *Collection) Upsert(records []Record) error {
	for i, r := range records {
		if r.ID == "" {
			return fmt.Errorf("record %d: %w", i, index.ErrEmptyID)
		}
		if len(r.Values) != c.idx.Dim() {
			return fmt.Errorf("record %d: %w: got %d, want %d", i, index.ErrDimension, len(r.Values), c.idx.Dim())
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range records {
		if err := c.idx.Add(r.ID, r.Values); err != nil {
			return err
		}
		if len(r.Metadata) > 0 {
			c.meta[r.ID] = r.Metadata
		} else {
			delete(c.meta, r.ID)
		}
	}
	return nil
}

// Delete removes a record and reports whether it existed.
func (c *Collection) Delete(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.meta, id)
	return c.idx.Delete(id)
}

// Get returns a stored record. Cosine collections return the normalised vector.
func (c *Collection) Get(id string) (Record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.idx.Vector(id)
	if !ok {
		return Record{}, false
	}
	return Record{ID: id, Values: v, Metadata: c.meta[id]}, true
}

// SearchOptions controls a query.
type SearchOptions struct {
	K      int
	Ef     int
	Filter Filter
}

// Search returns the nearest records to query. With a filter, extra
// candidates are fetched so enough matches remain after filtering.
func (c *Collection) Search(query []float32, opt SearchOptions) ([]Hit, error) {
	if opt.K <= 0 {
		opt.K = 10
	}
	fetch, ef := opt.K, opt.Ef
	if len(opt.Filter) > 0 {
		fetch = opt.K * 10
	}
	if ef <= 0 {
		ef = c.idx.Config().EfSearch
	}
	res, err := c.idx.SearchEf(query, fetch, max(ef, fetch))
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	hits := make([]Hit, 0, opt.K)
	for _, r := range res {
		m := c.meta[r.ID]
		if len(opt.Filter) > 0 && !opt.Filter.match(m) {
			continue
		}
		hits = append(hits, Hit{ID: r.ID, Distance: r.Distance, Metadata: m})
		if len(hits) == opt.K {
			break
		}
	}
	return hits, nil
}

// Store is a set of named collections.
type Store struct {
	mu          sync.RWMutex
	collections map[string]*Collection
	cfg         index.HNSWConfig
}

// New returns an empty store whose collections use cfg.
func New(cfg index.HNSWConfig) *Store {
	return &Store{collections: map[string]*Collection{}, cfg: cfg}
}

// Create adds a new empty collection.
func (s *Store) Create(name string, dim int, metric index.Metric) (*Collection, error) {
	if !nameRE.MatchString(name) {
		return nil, ErrBadName
	}
	if dim < 1 || dim > 4096 {
		return nil, ErrBadDim
	}
	idx, err := index.NewHNSW(dim, metric, s.cfg)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.collections[name]; ok {
		return nil, ErrExists
	}
	c := &Collection{Name: name, idx: idx, meta: map[string]Metadata{}}
	s.collections[name] = c
	return c, nil
}

// Get returns the named collection.
func (s *Store) Get(name string) (*Collection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.collections[name]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

// Drop deletes a collection.
func (s *Store) Drop(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.collections[name]; !ok {
		return ErrNotFound
	}
	delete(s.collections, name)
	return nil
}

// List describes all collections sorted by name.
func (s *Store) List() []Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Info, 0, len(s.collections))
	for _, c := range s.collections {
		out = append(out, c.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
