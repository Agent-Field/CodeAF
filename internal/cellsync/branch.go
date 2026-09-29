package cellsync

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Brancher turns turns that lost their lease into a new cell (L12). Nothing is
// overwritten: the old cell keeps its head and the new one names the old as
// its parent.
type Brancher struct {
	Dir   directory.Client
	NewID func() (string, error) // cell.NewID
	// Map records old id -> new id on this device so the local chat opens as
	// the branch. Nil records nothing.
	Map *BranchMap
}

// Branch creates the child cell for head, whose objects are already in the
// store, and points from at it. turns is how many turns the branch holds that
// the parent does not.
func (b Brancher) Branch(ctx context.Context, from *Driving, head string, turns uint32, in PublishInfo) (string, error) {
	id, err := b.NewID()
	if err != nil {
		return "", err
	}
	_, err = b.Dir.Create(ctx, id, directory.CellInit{
		Head: head, Class: in.Class, Title: in.Title, Size: in.Size, Keys: in.Keys,
		ParentCell: from.ID(), OrphanTurns: turns,
	})
	if err != nil {
		return "", err
	}
	if err := b.Map.Set(from.ID(), id); err != nil {
		return "", err
	}
	from.Remote, from.Fence, from.Head = id, 1, head
	return id, nil
}

// BranchMap is the device-local file <home>/v3/sync/branches.json.
type BranchMap struct {
	path string
	mu   sync.Mutex
	m    map[string]string
}

type branchFile struct {
	V   uint16            `json:"V"`
	Map map[string]string `json:"map"`
}

// OpenBranchMap reads the map under home; a missing file is an empty map.
func OpenBranchMap(home string) (*BranchMap, error) {
	m := &BranchMap{path: filepath.Join(home, "v3", "sync", "branches.json"), m: map[string]string{}}
	raw, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var f branchFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	maps.Copy(m.m, f.Map)
	return m, nil
}

// Set records that the chat that was old is now next, and saves the file.
func (m *BranchMap) Set(old, next string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[old] = next
	return m.save()
}

// Resolve follows old -> new links to the newest branch; an id never branched
// resolves to itself. The walk is bounded so a damaged file cannot loop it.
func (m *BranchMap) Resolve(id string) string {
	if m == nil {
		return id
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for range len(m.m) {
		next, ok := m.m[id]
		if !ok {
			break
		}
		id = next
	}
	return id
}

// save writes the file whole and renames it into place, so a crash leaves the
// old map or the new one, never half of either.
func (m *BranchMap) save() error {
	raw, err := json.Marshal(branchFile{V: 1, Map: m.m})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
