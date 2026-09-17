package plandb

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const stateVersion = 2

// state is the whole plan on disk: one file, written whole, renamed into
// place. The aforge-v1 port carried exactly this file; the adaptation adds
// notes to it and nothing else, so a state file the old build wrote still
// loads (notes were its absence, not a different shape).
type state struct {
	Version  int              `json:"version"`
	Project  string           `json:"project"`
	RootID   string           `json:"root_id"`
	Tasks    map[string]*Task `json:"tasks"`
	Order    []string         `json:"order"`
	Notes    []Note           `json:"notes,omitempty"`
	Contexts []ContextEntry   `json:"contexts,omitempty"`
	NextID   uint64           `json:"next_id"`
}

func loadState(path string) (state, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return state{}, err
	}
	var value state
	if err := json.Unmarshal(data, &value); err != nil {
		return state{}, fmt.Errorf("decode plan store: %w", err)
	}
	if value.Version != stateVersion {
		return state{}, fmt.Errorf("unsupported plan store version %d", value.Version)
	}
	if value.RootID == "" || value.Project == "" || value.Tasks == nil {
		return state{}, errors.New("invalid plan store")
	}
	if err := validateLoadedState(value); err != nil {
		return state{}, fmt.Errorf("validate plan store: %w", err)
	}
	return value, nil
}

// saveState writes the whole state through a temp file and a rename, so a
// reader never sees half a file. The cross-process story is the lock's
// (lock.go): this function is only ever called with it held.
func saveState(path string, value state) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create plan store directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("plan store path must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".plandb-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func cloneState(source state) state {
	copyState := source
	copyState.Order = append([]string(nil), source.Order...)
	copyState.Notes = append([]Note(nil), source.Notes...)
	copyState.Contexts = append([]ContextEntry(nil), source.Contexts...)
	copyState.Tasks = make(map[string]*Task, len(source.Tasks))
	for id, task := range source.Tasks {
		copyTask := *task
		copyTask.Dependencies = append([]Dependency(nil), task.Dependencies...)
		copyTask.Capabilities = append([]string(nil), task.Capabilities...)
		copyTask.Resources = append([]ResourceClaim(nil), task.Resources...)
		copyTask.ContextInputs = append([]string(nil), task.ContextInputs...)
		copyTask.Deliverables = append([]string(nil), task.Deliverables...)
		copyTask.EvidenceRequirements = append([]string(nil), task.EvidenceRequirements...)
		copyTask.Artifacts = append([]string(nil), task.Artifacts...)
		copyTask.Evidence = append([]string(nil), task.Evidence...)
		copyState.Tasks[id] = &copyTask
	}
	return copyState
}

func validateLoadedState(value state) error {
	root := value.Tasks[value.RootID]
	if root == nil || root.ParentID != "" {
		return errors.New("root task is missing or has a parent")
	}
	seen := make(map[string]bool, len(value.Order))
	for _, id := range value.Order {
		if seen[id] || value.Tasks[id] == nil {
			return fmt.Errorf("invalid task order entry %q", id)
		}
		seen[id] = true
	}
	if len(seen) != len(value.Tasks) {
		return errors.New("task order does not cover every task")
	}
	for id, task := range value.Tasks {
		if id != task.ID {
			return fmt.Errorf("task map key %q does not match id %q", id, task.ID)
		}
		if !validStatus(task.Status) {
			return fmt.Errorf("task %q has invalid status %q", id, task.Status)
		}
		if id != value.RootID {
			if err := validateSpec(task.TaskSpec); err != nil {
				return fmt.Errorf("task %q: %w", id, err)
			}
			if value.Tasks[task.ParentID] == nil {
				return fmt.Errorf("task %q has unknown parent %q", id, task.ParentID)
			}
		}
		for _, dep := range task.Dependencies {
			if value.Tasks[dep.TaskID] == nil {
				return fmt.Errorf("task %q has unknown dependency %q", id, dep.TaskID)
			}
		}
	}
	if err := validateGraphs(value); err != nil {
		return err
	}
	for id, task := range value.Tasks {
		hasChild := false
		for _, candidate := range value.Tasks {
			if candidate.ParentID == id {
				hasChild = true
				break
			}
		}
		if task.Composite != hasChild {
			return fmt.Errorf("task %q has inconsistent composite flag", id)
		}
	}
	return nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusPending, StatusReady, StatusClaimed, StatusRunning, StatusDone, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}
