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
	inv, err := s.read()
	if err != nil {
		return nil, err
	}
	s.inv = inv
	return s, nil
}

// read is the file as it is now, or the empty inventory of this platform where
// there is none.
func (s *Store) read() (Inventory, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Inventory{V: schemaV, Platform: Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}}, nil
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("open inventory: %w", err)
	}
	var inv Inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		return Inventory{}, fmt.Errorf("open inventory: %w", err)
	}
	return inv, nil
}

// Record opens the inventory of the cell rooted at root, applies change and
// persists it. It is the door for a step that writes one part of the record and
// does not hold a store: the seal's own steps, which run beside the session's
// observer and must not overwrite what it wrote.
func Record(root string, change func(*Inventory)) error {
	s, err := Open(root)
	if err != nil {
		return err
	}
	return s.Update(change)
}

// Snapshot is a copy of the current inventory.
func (s *Store) Snapshot() Inventory {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return clone(s.inv)
}

// refresh takes the file's current contents, because another store of the same
// file may have written since this one last looked. A file that cannot be read
// keeps what this store holds.
func (s *Store) refresh() {
	if inv, err := s.read(); err == nil {
		s.inv = inv
	}
}

// Update applies change to a copy and persists it when it differs. The copy is
// made from the file as it is now, so two stores of one cell never erase each
// other's part of the record.
func (s *Store) Update(change func(*Inventory)) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
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

// writeMu orders every read-change-write of the file in this process, whichever
// store does it.
var writeMu sync.Mutex

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
