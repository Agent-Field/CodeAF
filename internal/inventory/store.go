package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
)

// Store owns one cell's inventory file. Every change goes through Update, so
// the file is rewritten whole, atomically, and only when something changed.
type Store struct {
	mu   sync.Mutex
	path string
	inv  Inventory
}

// Open loads the inventory of the cell rooted at root, or starts an empty one
// for this platform.
func Open(root string) (*Store, error) {
	s := &Store{path: filepath.Join(root, filepath.FromSlash(Path))}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.inv = Inventory{V: schemaV, Platform: Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}}
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("open inventory: %w", err)
	}
	if err := json.Unmarshal(raw, &s.inv); err != nil {
		return nil, fmt.Errorf("open inventory: %w", err)
	}
	return s, nil
}

// Snapshot is a copy of the current inventory.
func (s *Store) Snapshot() Inventory {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.inv)
}

// Update applies change to a copy and persists it when it differs.
func (s *Store) Update(change func(*Inventory)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.inv)
	change(&next)
	next.V = schemaV
	if reflect.DeepEqual(next, s.inv) {
		return nil
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.inv = next
	return nil
}

// Annotate records agent-supplied notes about one key. It cannot touch any
// other field: annotations are the model's only door into the inventory (L13).
func (s *Store) Annotate(key string, note map[string]any) error {
	return s.Update(func(inv *Inventory) {
		if inv.Annotations == nil {
			inv.Annotations = map[string]map[string]any{}
		}
		inv.Annotations[key] = note
	})
}

func (s *Store) write(inv Inventory) error {
	raw, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, raw)
}

func writeAtomic(path string, data []byte) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".inventory-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// clone deep-copies through JSON, which is also the shape that persists.
func clone(inv Inventory) Inventory {
	raw, _ := json.Marshal(inv)
	var out Inventory
	_ = json.Unmarshal(raw, &out)
	return out
}
