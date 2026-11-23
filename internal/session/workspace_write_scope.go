package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// prepareWorkspaceWriteScope admits identity-backed private roots once, before
// the worker starts. The paths in model prose never grant shell write access.
func prepareWorkspaceWriteScope(config *Config) error {
	if config.WorkspaceSet == nil {
		return nil
	}
	set := config.WorkspaceSet.Clone()
	if set.Version != 1 || len(set.Roots) == 0 || len(set.Roots) > workspaceSetRootLimit {
		return errors.New("invalid worker workspace set")
	}
	records := map[string]workspaceidentity.Record{}
	resolve := func(uri string) (workspaceidentity.Record, error) {
		if record, ok := records[uri]; ok {
			return record, nil
		}
		record, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), uri)
		if err == nil {
			records[uri] = record
		}
		return record, err
	}
	writes := map[string]bool{}
	for _, root := range set.Roots {
		if root.Copy != nil && root.Access == WorkspaceWrite {
			writes[root.Copy.URI] = true
		}
	}
	var owner, base string
	primary := false
	seen := map[string]bool{}
	for _, use := range set.Roots {
		if use.Copy == nil || use.Copy.URI == "" || !validWorkspaceSlot(use.Slot) || seen[strings.ToLower(use.Slot)] || use.Access != WorkspaceRead && use.Access != WorkspaceWrite {
			return errors.New("invalid worker workspace root")
		}
		seen[strings.ToLower(use.Slot)] = true
		record, err := resolve(use.Copy.URI)
		if err != nil {
			return err
		}
		if record.Role != workspaceidentity.Run || record.OwnerURI != use.Copy.ChatURI || record.ProjectURI != use.Copy.ProjectURI || record.Path != filepath.Clean(use.Copy.Dir) || filepath.Base(record.Path) != use.Slot {
			return errors.New("worker workspace ownership does not match its admitted copy")
		}
		if owner != "" && owner != record.OwnerURI || base != "" && base != filepath.Dir(record.Path) {
			return errors.New("worker workspace roots do not share an admitted layout")
		}
		owner, base = record.OwnerURI, filepath.Dir(record.Path)
		chat, err := resolve(record.OwnerURI)
		if err != nil {
			return err
		}
		if chat.Role != workspaceidentity.Chat || filepath.Dir(filepath.Dir(record.Path)) != filepath.Join(chat.Path, cell.TreesDir) {
			return errors.New("worker copy is outside its owning conversation layout")
		}
		project, err := resolve(record.ProjectURI)
		if err != nil {
			return err
		}
		if project.Role != workspaceidentity.Project {
			return errors.New("worker workspace project identity is invalid")
		}
		original, err := filepath.EvalSymlinks(project.Path)
		if err != nil {
			return err
		}
		config.workspaceProtectedRoots = append(config.workspaceProtectedRoots, original)
		protected, err := workerProtectedSources(use, record.OwnerURI, writes, resolve)
		if err != nil {
			return err
		}
		config.workspaceProtectedRoots = append(config.workspaceProtectedRoots, protected...)
		real, err := filepath.EvalSymlinks(record.Path)
		if err != nil {
			return err
		}
		if use.Slot == set.Primary {
			primary = true
			if filepath.Clean(config.Workspace) != record.Path {
				return errors.New("worker primary workspace differs from its set")
			}
		}
		if use.Access == WorkspaceWrite {
			config.workspaceWriteRoots = append(config.workspaceWriteRoots, real)
		} else {
			config.workspaceProtectedRoots = append(config.workspaceProtectedRoots, real)
		}
	}
	if !primary {
		return errors.New("worker workspace primary is missing")
	}
	config.WorkspaceSet = set
	return nil
}

func (a *Agent) taskMayWrite(path string) bool {
	if a.config.WorkspaceSet == nil {
		return insideGround(a.taskGround(), path)
	}
	real, ok := workspaceWriteTarget(path)
	if !ok {
		return false
	}
	for _, root := range a.config.workspaceWriteRoots {
		if withinDir(root, real) {
			return true
		}
	}
	for _, root := range a.config.workspaceProtectedRoots {
		if withinDir(root, real) {
			return false
		}
	}
	if strings.HasPrefix(filepath.Clean(path), "/dev/") {
		return true
	}
	return scratchPath(path) && !scratchPath(a.taskGround())
}

// Resolve the closest existing ancestor, including a symlink above a not-yet
// created directory, before deciding whether a named write stays in its copy.
func workspaceWriteTarget(path string) (string, bool) {
	path = filepath.Clean(path)
	var tail []string
	for {
		if _, err := os.Lstat(path); err == nil {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", false
			}
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, true
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", false
		}
		tail = append(tail, filepath.Base(path))
		path = parent
	}
}

func workerProtectedSources(use WorkspaceRootUse, owner string, writes map[string]bool, resolve func(string) (workspaceidentity.Record, error)) ([]string, error) {
	var paths []string
	for _, uri := range []string{use.SourceURI, use.Copy.GroundURI, use.Copy.RootURI} {
		if uri == "" || writes[uri] {
			continue
		}
		record, err := resolve(uri)
		if err != nil {
			return nil, err
		}
		if record.Role != workspaceidentity.Project && record.Role != workspaceidentity.Run || record.Role == workspaceidentity.Run && record.OwnerURI != owner {
			return nil, errors.New("worker source identity is not an admitted project or same-chat copy")
		}
		path, err := filepath.EvalSymlinks(record.Path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// The task's cwd and its complete write scope are different facts. A refusal
// names the latter so the worker can use a declared sibling without guessing.
func (a *Agent) taskOutsideRefusal(what string) string {
	if a.config.WorkspaceSet == nil {
		return outsideGroundRefusal(what, a.taskGround())
	}
	roots := strings.Join(a.config.workspaceWriteRoots, ", ")
	if roots == "" {
		return what + " is not writable: this task has no admitted writable copies. Report requested changes instead."
	}
	return what + " is outside this task's admitted writable copies: " + roots + ". Keep edits in those copies; report requested changes elsewhere. Landing uses the task's explicit association."
}
