package taskcopy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

const coldRecord = "cold.json"

type coldCarry struct {
	Snapshot string               `json:"snapshot"`
	Withheld []inventory.Withheld `json:"withheld,omitempty"`
}

// PrepareCold freezes only the screened, portable task artifact while the
// session still owns the quiescent working copy. Full local archives stay local.
func (k Carry) PrepareCold(c cell.Cell, tree string) error {
	rel, e := filepath.Rel(liveRoot(c), tree)
	if e != nil || !filepath.IsLocal(rel) || filepath.Dir(rel) != "." {
		return errors.New("cold task copy is outside its cell")
	}
	if !isRepository(tree) {
		return errors.New("cold task copy has no portable repository")
	}
	return carryWithHistory(tree, filepath.Join(carriedRoot(c), rel), k.Project != "" && !isRepository(k.Project) && !isLinkedWorktree(tree))
}

// MarkCold binds the already-prepared relay artifact to the verified local
// archive. A missing marker blocks sealing rather than dropping task recovery.
func (Carry) MarkCold(c cell.Cell, tree, snapshot string) error {
	rel, e := filepath.Rel(liveRoot(c), tree)
	if e != nil || !filepath.IsLocal(rel) || filepath.Dir(rel) != "." || snapshot == "" {
		return errors.New("invalid cold task artifact")
	}
	dest := filepath.Join(carriedRoot(c), rel)
	if _, e = readRecord(dest); e != nil {
		return e
	}
	return writeCold(dest, coldCarry{Snapshot: snapshot, Withheld: leftOutOf(tree)})
}
func writeCold(dest string, marker coldCarry) error {
	raw, e := json.Marshal(marker)
	if e != nil {
		return e
	}
	temp, e := os.CreateTemp(dest, ".cold-")
	if e != nil {
		return e
	}
	name := temp.Name()
	defer os.Remove(name)
	if e = temp.Chmod(0600); e == nil {
		_, e = temp.Write(raw)
	}
	if e == nil {
		e = temp.Sync()
	}
	closed := temp.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return e
	}
	return os.Rename(name, filepath.Join(dest, coldRecord))
}

// coldCopies validates cached portable artifacts without expanding any archive.
// It runs before the ordinary carried-copy sweep can mistake cold for deleted.
func coldCopies(c cell.Cell) ([]string, []inventory.Withheld, error) {
	manager, enabled := taskstorage.Prototype(liveRoot(c))
	if !enabled {
		// Capability switches do not erase durable lifecycle evidence. Read it
		// without enabling capture, recovery or retirement for this profile.
		manager = taskstorage.Manager{Root: home.Join("task-storage"), Trees: liveRoot(c)}
		entries, err := os.ReadDir(carriedRoot(c))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		for _, entry := range entries {
			marker := filepath.Join(carriedRoot(c), entry.Name(), coldRecord)
			if _, err := os.Lstat(marker); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, nil, err
			}
			return nil, nil, errors.New("cold task recovery requires task storage support before sealing; portable artifacts were retained")
		}
		if _, err := os.Lstat(manager.Root); errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		} else if err != nil {
			return nil, nil, err
		}
	}
	paths, e := manager.Paths(context.Background())
	if e != nil {
		return nil, nil, e
	}
	var cold []string
	var withheld []inventory.Withheld
	for _, tree := range paths {
		state, e := manager.Inspect(context.Background(), tree)
		if e != nil {
			return nil, nil, e
		}
		if state.Phase == taskstorage.Resident {
			continue
		}
		if !enabled {
			return nil, nil, errors.New("saved task storage needs its supported reader before sealing; portable artifacts were retained")
		}
		if state.Phase != taskstorage.Cold {
			return nil, nil, errors.New("task storage transition needs recovery before sealing")
		}
		dest := filepath.Join(carriedRoot(c), filepath.Base(tree))
		var marker coldCarry
		raw, e := os.ReadFile(filepath.Join(dest, coldRecord))
		if e != nil || json.Unmarshal(raw, &marker) != nil || marker.Snapshot != state.Snapshot {
			return nil, nil, fmt.Errorf("cold task %s has no matching prepared relay artifact", filepath.Base(tree))
		}
		if e := screenCarriedHistory(dest); e != nil {
			return nil, nil, e
		}
		cold = append(cold, tree)
		withheld = append(withheld, marker.Withheld...)
	}
	return cold, withheld, nil
}
