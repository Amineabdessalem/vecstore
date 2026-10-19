package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Amineabdessalem/vecstore/index"
)

func newStore(t *testing.T) (*Store, *Collection) {
	t.Helper()
	s := New(index.DefaultHNSWConfig())
	c, err := s.Create("docs", 2, index.Euclidean)
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestCreateValidation(t *testing.T) {
	s, _ := newStore(t)
	cases := []struct {
		name string
		dim  int
		want error
	}{
		{"docs", 2, ErrExists},
		{"bad name!", 2, ErrBadName},
		{"ok", 0, ErrBadDim},
	}
	for _, tc := range cases {
		if _, err := s.Create(tc.name, tc.dim, index.Cosine); !errors.Is(err, tc.want) {
			t.Errorf("Create(%q, %d) = %v, want %v", tc.name, tc.dim, err, tc.want)
		}
	}
	if _, err := s.Create("x", 2, "manhattan"); err == nil {
		t.Error("expected unknown metric error")
	}
}

func TestUpsertSearchFilter(t *testing.T) {
	_, c := newStore(t)
	err := c.Upsert([]Record{
		{ID: "a", Values: []float32{0, 0}, Metadata: Metadata{"lang": "go"}},
		{ID: "b", Values: []float32{1, 0}, Metadata: Metadata{"lang": "python"}},
		{ID: "c", Values: []float32{2, 0}, Metadata: Metadata{"lang": "go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, _ := c.Search([]float32{1, 0}, SearchOptions{K: 1})
	if hits[0].ID != "b" || hits[0].Metadata["lang"] != "python" {
		t.Errorf("unexpected top hit %+v", hits[0])
	}
	hits, _ = c.Search([]float32{1, 0}, SearchOptions{K: 5, Filter: Filter{"lang": "go"}})
	if len(hits) != 2 || hits[0].Metadata["lang"] != "go" || hits[1].Metadata["lang"] != "go" {
		t.Errorf("filter failed: %+v", hits)
	}
}

func TestUpsertIsAtomicOnValidationError(t *testing.T) {
	_, c := newStore(t)
	err := c.Upsert([]Record{{ID: "a", Values: []float32{0, 0}}, {ID: "b", Values: []float32{1}}})
	if !errors.Is(err, index.ErrDimension) {
		t.Fatalf("want dimension error, got %v", err)
	}
	if c.Info().Count != 0 {
		t.Errorf("partial batch was applied")
	}
}

func TestGetDeleteDropList(t *testing.T) {
	s, c := newStore(t)
	_ = c.Upsert([]Record{{ID: "a", Values: []float32{3, 4}, Metadata: Metadata{"k": 1}}})
	if r, ok := c.Get("a"); !ok || r.Values[0] != 3 || r.Metadata["k"] != 1 {
		t.Errorf("get failed: %+v", r)
	}
	if !c.Delete("a") {
		t.Error("delete failed")
	}
	if _, ok := c.Get("a"); ok {
		t.Error("deleted record still returned")
	}
	if got := s.List(); len(got) != 1 || got[0].Name != "docs" || got[0].Count != 0 {
		t.Errorf("list = %+v", got)
	}
	if err := s.Drop("docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("docs"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestAutoCompactAfterManyDeletes(t *testing.T) {
	s := New(index.DefaultHNSWConfig())
	c, _ := s.Create("big", 2, index.Euclidean)
	recs := make([]Record, 600)
	for i := range recs {
		recs[i] = Record{ID: fmt.Sprintf("r%d", i), Values: []float32{float32(i), 0}}
	}
	if err := c.Upsert(recs); err != nil {
		t.Fatal(err)
	}
	for _, r := range recs[:400] {
		c.Delete(r.ID)
	}
	info := c.Info()
	if info.Count != 200 || info.Deleted >= autoCompactMin {
		t.Errorf("expected auto-compaction, got %+v", info)
	}
	hits, _ := c.Search([]float32{599, 0}, SearchOptions{K: 1})
	if hits[0].ID != recs[599].ID {
		t.Errorf("search after compaction: %+v", hits)
	}
}
