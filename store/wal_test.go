package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Amineabdessalem/vecstore/index"
)

func openStore(t *testing.T, dir string) (*Store, *WAL, int) {
	t.Helper()
	s := New(index.DefaultHNSWConfig())
	n, err := s.Recover(filepath.Join(dir, "snap"), filepath.Join(dir, "wal"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := OpenWAL(filepath.Join(dir, "wal"), false)
	if err != nil {
		t.Fatal(err)
	}
	s.AttachWAL(w)
	return s, w, n
}

func TestWALRecoversWritesWithoutSnapshot(t *testing.T) {
	dir := t.TempDir()
	s, w, _ := openStore(t, dir)
	c, _ := s.Create("docs", 2, index.Euclidean)
	_ = c.Upsert([]Record{{ID: "a", Values: []float32{1, 0}, Metadata: Metadata{"k": "v"}}, {ID: "b", Values: []float32{0, 1}}})
	c.Delete("b")
	_, _ = s.Create("tmp", 2, index.Cosine)
	_ = s.Drop("tmp")
	w.Close()

	r, _, n := openStore(t, dir)
	if n != 5 {
		t.Fatalf("replayed %d entries, want 5", n)
	}
	got, err := r.Get("docs")
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok := got.Get("a"); !ok || rec.Metadata["k"] != "v" {
		t.Fatalf("record a not recovered: %+v", rec)
	}
	if _, ok := got.Get("b"); ok {
		t.Fatal("deleted record came back")
	}
	if _, err := r.Get("tmp"); err == nil {
		t.Fatal("dropped collection came back")
	}
}

func TestCheckpointTruncatesLogAndKeepsData(t *testing.T) {
	dir := t.TempDir()
	s, w, _ := openStore(t, dir)
	c, _ := s.Create("docs", 2, index.Euclidean)
	_ = c.Upsert([]Record{{ID: "a", Values: []float32{1, 0}}})
	if err := s.Checkpoint(filepath.Join(dir, "snap")); err != nil {
		t.Fatal(err)
	}
	_ = c.Upsert([]Record{{ID: "b", Values: []float32{0, 1}}})
	w.Close()
	if _, err := os.Stat(filepath.Join(dir, "wal.old")); !os.IsNotExist(err) {
		t.Fatal("rotated log should be removed after a checkpoint")
	}

	r, _, n := openStore(t, dir)
	if n != 1 {
		t.Fatalf("replayed %d entries, want only the post-checkpoint write", n)
	}
	got, _ := r.Get("docs")
	if got.Info().Count != 2 {
		t.Fatalf("count %d, want 2", got.Info().Count)
	}
}

func TestReplayIsIdempotentAndToleratesTornTail(t *testing.T) {
	dir := t.TempDir()
	s, w, _ := openStore(t, dir)
	c, _ := s.Create("docs", 2, index.Euclidean)
	_ = c.Upsert([]Record{{ID: "a", Values: []float32{1, 0}}})
	w.Close()
	// Simulate a crash during a checkpoint (old log left behind, holding the
	// same entries) and a torn final write.
	data, _ := os.ReadFile(filepath.Join(dir, "wal"))
	_ = os.WriteFile(filepath.Join(dir, "wal.old"), data, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "wal"), append(data, []byte(`{"op":"upsert","c":"do`)...), 0o644)

	r, _, _ := openStore(t, dir)
	got, err := r.Get("docs")
	if err != nil || got.Info().Count != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCorruptMiddleEntryIsAnError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "wal"), []byte("not json\n{\"op\":\"create\",\"c\":\"x\",\"dim\":2}\n"), 0o644)
	s := New(index.DefaultHNSWConfig())
	if _, err := s.Recover(filepath.Join(dir, "snap"), filepath.Join(dir, "wal")); err == nil {
		t.Fatal("expected an error for a corrupt entry before the end of the log")
	}
}
