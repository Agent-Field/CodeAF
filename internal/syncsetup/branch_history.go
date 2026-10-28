package syncsetup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// detachBranchWork keeps completed outputs discoverable while removing every
// inherited executable frontier. It runs only on an explicitly opened branch,
// before any new agent can load a plan or schedule recovered work.
func detachBranchWork(c cell.Cell) error {
	if !session.WorkspaceOwnershipEnabled(session.Place{Dir: c.Root}) {
		return nil
	}
	place := session.Place{Dir: c.Root, Owned: true}
	history := session.ForkHistoryDir(place)
	ready := filepath.Join(history, "independent-ready")
	if _, err := os.Stat(ready); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	workspaceSet, err := detachWorkspaceSetHistory(c, place, history)
	if err != nil {
		return err
	}
	var parents map[string]string
	var selections []taskcopy.ResultSelection
	if !workspaceSet {
		parents, err = session.ForkTaskRestoreParents(place)
		if err != nil {
			return err
		}
		selections, err = session.ForkTaskHistory(place)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(history, 0700); err != nil {
		return err
	}
	// A restored fork has no local writer. Preserve the complete plan and WAL
	// together under history so the new conversation opens a fresh plan store.
	plans, err := filepath.Glob(filepath.Join(c.Root, "plandb.db*"))
	if err != nil {
		return err
	}
	for _, path := range plans {
		if err := moveForkHistory(path, filepath.Join(history, filepath.Base(path))); err != nil {
			return err
		}
	}
	liveArchive := filepath.Join(c.Root, cell.CarriedTreesPath)
	archivedTrees := filepath.Join(history, "trees")
	// Completed result reconstruction reads the normal carrier layout but writes
	// only ordinary historical outputs, never this chat's live trees/ folder.
	if _, err := os.Stat(liveArchive); !workspaceSet && err == nil {
		outputs, err := taskcopy.RestoreCompletedResultsWithParents(c, workspaceOf(c.Root), filepath.Join(history, "results"), selections, parents)
		if err != nil {
			return err
		}
		if err := recordForkArtifacts(c, place, outputs); err != nil {
			return err
		}
		if err := moveForkHistory(liveArchive, archivedTrees); err != nil {
			return err
		}
	} else if !workspaceSet && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := session.ForkChatWorkspaceIdentity(place); err != nil {
		return err
	}
	return os.WriteFile(ready, []byte("independent\n"), 0600)
}

func moveForkHistory(source, destination string) error {
	if _, err := os.Lstat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("independent conversation history already contains %s", filepath.Base(destination))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}
