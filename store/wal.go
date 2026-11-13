package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Amineabdessalem/vecstore/index"
)

// WAL is an append-only log of writes, one JSON entry per line. Together with
// snapshots it lets a restarted server recover writes made since the last
// snapshot: load the snapshot, then replay the log.
//
// Entries are written before the change is applied, while the affected
// collection is locked, so the log order matches the order of changes.
// Replaying is idempotent, which keeps checkpoints simple (see Checkpoint).
type WAL struct {
	mu   sync.Mutex
	path string
	f    *os.File
	sync bool
}

type walEntry struct {
	Op         string       `json:"op"`
	Collection string       `json:"c"`
	Dim        int          `json:"dim,omitempty"`
	Metric     index.Metric `json:"metric,omitempty"`
	Records    []Record     `json:"records,omitempty"`
	ID         string       `json:"id,omitempty"`
}

// OpenWAL opens (or creates) the log at path for appending. With syncEvery,
// each entry is fsynced before the write is acknowledged.
func OpenWAL(path string, syncEvery bool) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &WAL{path: path, f: f, sync: syncEvery}, nil
}

func (w *WAL) append(e walEntry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("wal: %w", err)
	}
	if w.sync {
		return w.f.Sync()
	}
	return nil
}

// rotate moves the current log aside to path+".old" and starts an empty one.
func (w *WAL) rotate() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	old := w.path + ".old"
	if err := w.f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(w.path, old); err != nil {
		return "", err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	w.f = f
	return old, nil
}

// Close flushes and closes the log file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.f.Sync(); err != nil {
		return err
	}
	return w.f.Close()
}

// AttachWAL makes every later Create, Drop, Upsert and Delete append to w.
func (s *Store) AttachWAL(w *WAL) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wal = w
}

func (s *Store) logEntry(e walEntry) error {
	if s.wal == nil {
		return nil
	}
	return s.wal.append(e)
}

// Checkpoint writes a snapshot to snapPath and discards the log entries it
// covers. The log is rotated first, so writes racing with the snapshot land
// in the new log; they may also be in the snapshot, which is harmless because
// replay is idempotent. A crash at any point leaves snapshot + logs that
// recover every acknowledged write.
func (s *Store) Checkpoint(snapPath string) error {
	if s.wal == nil {
		return s.SaveFile(snapPath)
	}
	old, err := s.wal.rotate()
	if err != nil {
		return err
	}
	if err := s.SaveFile(snapPath); err != nil {
		return err
	}
	return os.Remove(old)
}

// Recover loads the snapshot at snapPath (if any), then replays the log at
// walPath+".old" and walPath (if present). It returns how many entries were replayed.
func (s *Store) Recover(snapPath, walPath string) (int, error) {
	if err := s.LoadFile(snapPath); err != nil {
		return 0, err
	}
	total := 0
	for _, p := range []string{walPath + ".old", walPath} {
		f, err := os.Open(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return total, err
		}
		n, err := s.replay(f)
		f.Close()
		total += n
		if err != nil {
			return total, fmt.Errorf("%s: %w", p, err)
		}
	}
	return total, nil
}

// replay applies log entries from r. A torn final line (a crash mid-write)
// is ignored; a corrupt line elsewhere is an error.
func (s *Store) replay(r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	n := 0
	var pending error
	for sc.Scan() {
		if pending != nil {
			return n, pending
		}
		var e walEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			pending = fmt.Errorf("entry %d: %w", n+1, err)
			continue
		}
		if err := s.apply(e); err != nil {
			return n, fmt.Errorf("entry %d (%s %q): %w", n+1, e.Op, e.Collection, err)
		}
		n++
	}
	return n, sc.Err()
}

// apply performs one entry without logging it, tolerating entries that were
// already applied (a collection that exists, or one that is already gone).
func (s *Store) apply(e walEntry) error {
	switch e.Op {
	case "create":
		if _, err := s.create(e.Collection, e.Dim, e.Metric, false); err != nil && !errors.Is(err, ErrExists) {
			return err
		}
	case "drop":
		if err := s.drop(e.Collection, false); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	case "upsert", "delete":
		c, err := s.Get(e.Collection)
		if errors.Is(err, ErrNotFound) {
			return nil // dropped later in the log
		}
		if err != nil {
			return err
		}
		if e.Op == "upsert" {
			return c.upsert(e.Records, false)
		}
		c.delete(e.ID, false)
	default:
		return fmt.Errorf("unknown op %q", e.Op)
	}
	return nil
}
