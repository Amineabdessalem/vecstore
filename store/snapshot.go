package store

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Amineabdessalem/vecstore/index"
)

const snapshotVersion = 1

type snapshot struct {
	Version     int
	Collections []collectionSnapshot
}

type collectionSnapshot struct {
	Name    string
	Dim     int
	Metric  index.Metric
	Records []Record
}

func init() {
	// Metadata values decoded from JSON are these concrete types.
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// WriteSnapshot serialises every collection to w. Graphs are not stored;
// they are rebuilt from the vectors on load, which keeps the format small
// and independent of HNSW parameters.
func (s *Store) WriteSnapshot(w io.Writer) error {
	s.mu.RLock()
	snap := snapshot{Version: snapshotVersion}
	for _, c := range s.collections {
		cs := collectionSnapshot{Name: c.Name, Dim: c.idx.Dim(), Metric: c.idx.Metric()}
		c.mu.RLock()
		c.idx.Each(func(id string, v []float32) {
			cs.Records = append(cs.Records, Record{ID: id, Values: v, Metadata: c.meta[id]})
		})
		c.mu.RUnlock()
		snap.Collections = append(snap.Collections, cs)
	}
	s.mu.RUnlock()
	return gob.NewEncoder(w).Encode(snap)
}

// ReadSnapshot replaces the store's contents with a snapshot from r.
func (s *Store) ReadSnapshot(r io.Reader) error {
	var snap snapshot
	if err := gob.NewDecoder(r).Decode(&snap); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	if snap.Version != snapshotVersion {
		return fmt.Errorf("unsupported snapshot version %d", snap.Version)
	}
	fresh := New(s.cfg)
	for _, cs := range snap.Collections {
		c, err := fresh.Create(cs.Name, cs.Dim, cs.Metric)
		if err != nil {
			return fmt.Errorf("collection %q: %w", cs.Name, err)
		}
		if err := c.Upsert(cs.Records); err != nil {
			return fmt.Errorf("collection %q: %w", cs.Name, err)
		}
	}
	s.mu.Lock()
	s.collections = fresh.collections
	s.mu.Unlock()
	return nil
}

// SaveFile writes a snapshot atomically: to a temp file, then renamed.
func (s *Store) SaveFile(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vecstore-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	bw := bufio.NewWriter(tmp)
	if err := s.WriteSnapshot(bw); err != nil {
		tmp.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// LoadFile restores a snapshot written by SaveFile. A missing file is not
// an error, so a fresh data directory starts empty.
func (s *Store) LoadFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return s.ReadSnapshot(bufio.NewReader(f))
}
