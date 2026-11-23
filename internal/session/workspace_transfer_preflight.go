package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// ValidateTaskRestoreRoots rejects unbound root sets before any task repository
// or identity is mutated. primary is the receiver's intended folder, never a
// source machine path borrowed from the incoming metadata.
func ValidateTaskRestoreRoots(place Place, primary string) error {
	return validateTransferRoots(place, primary, false)
}

// ValidateForkTaskRoots proves one source root before detaching task history.
// A fork does not rebind that root: only its historical output files are opened.
func ValidateForkTaskRoots(place Place, primary string) error {
	return validateTransferRoots(place, primary, true)
}

func validateTransferRoots(place Place, primary string, fork bool) error {
	if !WorkspaceOwnershipEnabled(place) {
		return nil
	}
	raw, err := os.ReadFile(place.Tasks())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return err
	}
	// The singleton transfer contract cannot represent execution layouts. Refuse
	// before parent planning or any receiver-side import, including queued work
	// which has not acquired a primary Copy yet.
	for _, run := range document.Runs {
		if run.WorkspaceSet != nil || run.PendingRun != nil && run.PendingRun.WorkspaceSet != nil {
			var checked error
			if fork {
				_, checked = TaskWorkspaceTransferPlan(place)
			} else {
				checked = validateAdmittedWorkspaceTransfer(place)
			}
			if checked == nil {
				return nil
			}
			return unboundTransferRoots(fmt.Errorf("this workspace-set layout needs per-root transfer bindings; the singleton restore path cannot restore it: %w", checked))
		}
	}
	// Resolve portable fields against the already chosen receiving folder.
	document = document.mapPaths(codecFor(place.Dir, primary).resolve)
	if _, err := incomingRestoreParents(place, document); err != nil {
		return err
	}
	roots, err := transferredRootSet(place, document)
	if err != nil {
		return unboundTransferRoots(err)
	}
	return validateKnownTransferRoots(roots, primary, fork)
}

func validateKnownTransferRoots(roots map[string]bool, primary string, fork bool) error {
	knownPath := ""
	for root := range roots {
		record, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), root)
		if errors.Is(err, workspaceidentity.ErrUnknown) && len(roots) == 1 {
			return nil
		}
		if err != nil {
			return unboundTransferRoots(err)
		}
		if record.Role != workspaceidentity.Project {
			return unboundTransferRoots(errors.New("root identity does not name a working root"))
		}
		path := canonicalPath(record.Path)
		if knownPath != "" && knownPath != path {
			return unboundTransferRoots(errors.New("incoming roots have different local bindings"))
		}
		if !fork && path != canonicalPath(primary) {
			return unboundTransferRoots(errors.New("root is not bound to the receiving folder"))
		}
		knownPath = path
	}
	return nil
}

func transferredRootSet(place Place, document taskDocument) (map[string]bool, error) {
	roots := map[string]bool{}
	names := map[string]bool{}
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return nil, err
	}
	for _, copy := range incomingTaskCopyRecords(document) {
		if copy == nil || copy.URI == "" {
			continue
		}
		if err := validateIncomingRestoreCopy(place, copy, meta.ChatURI); err != nil {
			return nil, err
		}
		roots[copy.ProjectURI] = true
		names[filepath.Base(copy.Dir)] = true
	}
	if len(roots) > 0 {
		for _, name := range taskcopy.Carried(cell.Cell{ID: place.ID(), Root: place.Dir}) {
			if !names[name] {
				return nil, errors.New("a carried task has no structured working-root binding")
			}
		}
	}
	return roots, nil
}

func unboundTransferRoots(cause error) error {
	return fmt.Errorf("these task working roots need explicit receiving bindings before they can be restored; the incoming archives are preserved and no task was restored: %w", cause)
}
