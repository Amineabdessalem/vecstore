package store

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/Amineabdessalem/vecstore/index"
)

func TestSnapshotRoundTrip(t *testing.T) {
	s := New(index.DefaultHNSWConfig())
	a, _ := s.Create("a", 2, index.Euclidean)
	b, _ := s.Create("b", 3, index.Cosine)
	_ = a.Upsert([]Record{
		{ID: "p", Values: []float32{1, 2}, Metadata: Metadata{"tags": []any{"x", "y"}, "n": 1.5}},
		{ID: "q", Values: []float32{3, 4}},
		{ID: "gone", Values: []float32{9, 9}},
	})
	a.Delete("gone")
	_ = b.Upsert([]Record{{ID: "r", Values: []float32{0, 3, 4}}})

	var buf bytes.Buffer
	if err := s.WriteSnapshot(&buf); err != nil {
		t.Fatal(err)
	}
	restored := New(index.DefaultHNSWConfig())
	if err := restored.ReadSnapshot(&buf); err != nil {
		t.Fatal(err)
	}
	if got := restored.List(); len(got) != 2 || got[0].Count != 2 || got[1].Count != 1 || got[1].Metric != index.Cosine {
		t.Fatalf("list after restore: %+v", got)
	}
	ra, _ := restored.Get("a")
	p, ok := ra.Get("p")
	if !ok || p.Values[1] != 2 || p.Metadata["n"] != 1.5 || len(p.Metadata["tags"].([]any)) != 2 {
		t.Errorf("record p: %+v", p)
	}
	if _, ok := ra.Get("gone"); ok {
		t.Error("deleted record was restored")
	}
	hits, _ := ra.Search([]float32{3, 4}, SearchOptions{K: 1})
	if hits[0].ID != "q" {
		t.Errorf("search after restore: %+v", hits)
	}
}

func TestSaveLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.snap")
	s := New(index.DefaultHNSWConfig())
	if err := s.LoadFile(path); err != nil {
		t.Fatalf("missing file should load empty: %v", err)
	}
	c, _ := s.Create("c", 1, index.Euclidean)
	_ = c.Upsert([]Record{{ID: "z", Values: []float32{7}}})
	if err := s.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	other := New(index.DefaultHNSWConfig())
	if err := other.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if got := other.List(); len(got) != 1 || got[0].Count != 1 {
		t.Errorf("loaded %+v", got)
	}
}

func TestReadSnapshotRejectsGarbage(t *testing.T) {
	s := New(index.DefaultHNSWConfig())
	if err := s.ReadSnapshot(bytes.NewBufferString("not a snapshot")); err == nil {
		t.Error("expected error")
	}
}
