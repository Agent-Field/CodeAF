package syncsetup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// detachWorkspaceSetHistory extracts settled outputs before the ordinary fork
// frontier writer replaces the authenticated checkpoint used by the carrier.
func detachWorkspaceSetHistory(c cell.Cell, place session.Place, history string) (bool, error) {
	plan, err := session.TaskWorkspaceTransferPlan(place)
	if err != nil {
		return false, err
	}
	live := filepath.Join(c.Root, taskcopy.WorkspaceCarrierPath)
	archived := filepath.Join(history, filepath.Base(taskcopy.WorkspaceCarrierPath))
	if plan == nil {
		// Frontier detachment is written only after result extraction and artifact
		// registration. Finish an interrupted archive move without reinterpreting
		// the now intentionally authority-free task rows as runnable copies.
		if _, err := os.Stat(filepath.Join(history, "frontier-detached")); err != nil {
			return false, nil
		}
		if _, err := os.Stat(archived); err == nil {
			return true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		if _, err := os.Stat(live); errors.Is(err, fs.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, moveForkHistory(live, archived)
	}
	selections, err := session.TaskWorkspaceForkResults(place)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(history, 0700); err != nil {
		return false, err
	}
	outputs, err := taskcopy.RestoreCompletedWorkspaceResults(c, *plan, filepath.Join(history, "results"), selections)
	if err != nil {
		return false, err
	}
	if err := recordForkArtifacts(c, place, outputs); err != nil {
		return false, err
	}
	if _, err := session.ForkTaskHistory(place); err != nil {
		return false, err
	}
	return true, moveForkHistory(live, archived)
}

func recordForkArtifacts(c cell.Cell, place session.Place, outputs map[string]string) error {
	for id, root := range outputs {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			// Literal links remain in the historical result and complete carrier.
			// The deliverables picker cites regular files, never a link's target.
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !entry.Type().IsRegular() {
				return errors.New("historical artifact is not a regular output file")
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			label := id
			if layout, err := filepath.Rel(filepath.Join(session.ForkHistoryDir(place), "results"), root); err == nil && filepath.IsLocal(layout) {
				label = filepath.ToSlash(layout)
			}
			session.RecordSealedArtifact(place, session.Artifact{Path: path, Session: c.ID, Title: fmt.Sprintf("Task %s · %s", label, relative), Kind: "document", Created: time.Now()})
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
