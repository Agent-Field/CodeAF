package shape

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// AddChange appends one request and its full-text diffs. Only passed changes
// join files; a checking/failed/question change stays in the ordered journal.
func (d *draft) AddChange(change bridge.Change, diffs []bridge.FileDiff) error {
	if change.ID == "" {
		return bridge.Errorf(bridge.CodeInvalid, "change id is required")
	}
	if d.index(change.ID) >= 0 {
		return bridge.Errorf(bridge.CodeConflict, "change already exists")
	}
	candidate := d.copy()
	if change.N == 0 {
		for _, c := range d.Changes {
			change.N = max(change.N, c.N)
		}
		change.N++
	}
	if change.At == "" {
		change.At = timestamp()
	}
	candidate.Changes = append(candidate.Changes, cloneChange(change))
	candidate.Diffs[change.ID] = slices.Clone(diffs)
	return d.commit(candidate)
}

// SetChange replaces a request and its diffs without moving its position.
// Preserve the existing diffs when updating only pipeline progress.
func (d *draft) SetChange(change bridge.Change, diffs []bridge.FileDiff) error {
	i := d.index(change.ID)
	if i < 0 {
		return bridge.Errorf(bridge.CodeNotFound, "no change "+change.ID)
	}
	candidate := d.copy()
	if change.N == 0 {
		change.N = d.Changes[i].N
	}
	if change.At == "" {
		change.At = d.Changes[i].At
	}
	candidate.Changes[i] = cloneChange(change)
	candidate.Diffs[change.ID] = slices.Clone(diffs)
	return d.commit(candidate)
}

// ApplyFiles accepts the complete checked definition for a change, including
// removed files. It stores the diff from that change's preceding files and
// marks the change passed. Later passed changes are reapplied in order.
func (d *draft) ApplyFiles(change string, files spec.Files) error {
	i := d.index(change)
	if i < 0 {
		return bridge.Errorf(bridge.CodeNotFound, "no change "+change)
	}
	before := d.copy()
	before.Changes = before.Changes[:i]
	base, err := before.rebuild()
	if err != nil {
		return err
	}
	candidate := d.copy()
	candidate.Diffs[change] = fileDiffs(base, files)
	candidate.Changes[i].State = "passed"
	candidate.Changes[i].Files = []string{}
	for _, diff := range candidate.Diffs[change] {
		candidate.Changes[i].Files = append(candidate.Changes[i].Files, diff.Path)
	}
	return d.commit(candidate)
}

// Files returns a detached complete definition. Metadata is authoritative so
// callers never observe a partially written directory from a previous crash.
func (d *draft) Files() (spec.Files, error) { return d.rebuild() }

// Rebase starts replay on a new immutable base and clears each old diff.
// The pipeline keeps the requests, checks them again, then uses ApplyFiles.
func (d *draft) Rebase(newBase int, files spec.Files) error {
	if newBase < 1 {
		return bridge.Errorf(bridge.CodeInvalid, "invalid draft base")
	}
	candidate := d.copy()
	candidate.Base, candidate.BaseFiles = newBase, cloneFiles(files)
	candidate.Replayed = nil
	for i := range candidate.Changes {
		candidate.Changes[i].State = "checking"
		candidate.Changes[i].Round = 0
		candidate.Changes[i].Steps = []bridge.ShapeStep{}
		candidate.Changes[i].Files = []string{}
		candidate.Changes[i].Check = nil
		candidate.Changes[i].Question = nil
		candidate.Changes[i].Error = ""
		candidate.Diffs[candidate.Changes[i].ID] = []bridge.FileDiff{}
	}
	ctx := d.s.cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := d.refreshOverlay(ctx); err != nil {
		return err
	}
	return d.commit(candidate)
}

func (d *draft) removeChange(id string) error {
	i := d.index(id)
	if i < 0 {
		return bridge.Errorf(bridge.CodeNotFound, "no change "+id)
	}
	candidate := d.copy()
	candidate.Changes = slices.Delete(candidate.Changes, i, i+1)
	delete(candidate.Diffs, id)
	return d.commit(candidate)
}

func (s *Service) CancelChange(ctx context.Context, scope svc.Scope, p bridge.ShapeChangeCancelParams) (bridge.ShapeChangeCancelResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Draft{}, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.Draft{}, err
	}
	if err := d.removeChange(p.Change); err != nil {
		return bridge.Draft{}, err
	}
	return d.snapshot(), nil
}

func (s *Service) Diff(ctx context.Context, scope svc.Scope, p bridge.ShapeDiffParams) (bridge.ShapeDiffResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.ShapeDiffResult{}, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.ShapeDiffResult{}, err
	}
	if p.Change != "" {
		if d.index(p.Change) < 0 {
			return bridge.ShapeDiffResult{}, bridge.Errorf(bridge.CodeNotFound, "no change "+p.Change)
		}
		return bridge.ShapeDiffResult{Files: append([]bridge.FileDiff{}, d.Diffs[p.Change]...)}, nil
	}
	files, err := d.Files()
	if err != nil {
		return bridge.ShapeDiffResult{}, err
	}
	return bridge.ShapeDiffResult{Files: fileDiffs(d.BaseFiles, files)}, nil
}

func (d *draft) index(id string) int {
	return slices.IndexFunc(d.Changes, func(c bridge.Change) bool { return c.ID == id })
}

func (d *draft) copy() *draft {
	candidate := *d
	candidate.Draft = d.snapshot()
	candidate.BaseFiles = cloneFiles(d.BaseFiles)
	candidate.Diffs = make(map[string][]bridge.FileDiff, len(d.Diffs))
	for id, diffs := range d.Diffs {
		candidate.Diffs[id] = slices.Clone(diffs)
	}
	return &candidate
}

func cloneChange(change bridge.Change) bridge.Change {
	d := &draft{Draft: bridge.Draft{Changes: []bridge.Change{change}}}
	return d.snapshot().Changes[0]
}

func (d *draft) commit(candidate *draft) error {
	files, err := candidate.rebuild()
	if err != nil {
		return err
	}
	candidate.State = draftState(candidate.Changes)
	if err := replaceFiles(d.dir, files); err != nil {
		return err
	}
	if err := candidate.persist(); err != nil {
		previous, rebuildErr := d.rebuild()
		return errors.Join(err, rebuildErr, replaceFiles(d.dir, previous))
	}
	*d = *candidate
	d.publish()
	return nil
}

func draftState(changes []bridge.Change) string {
	state := "open"
	for _, change := range changes {
		if change.State == "checking" {
			return "checking"
		}
		if change.State == "question" {
			state = "question"
		}
	}
	if state == "question" {
		return state
	}
	if len(changes) == 0 {
		return "open"
	}
	for _, change := range changes {
		if change.State != "passed" {
			return "open"
		}
	}
	return "ready"
}

func (d *draft) rebuild() (spec.Files, error) {
	files := cloneFiles(d.BaseFiles)
	if err := validateFiles(files); err != nil {
		return nil, err
	}
	for _, change := range d.Changes {
		if change.State != "passed" {
			continue
		}
		for _, diff := range d.Diffs[change.ID] {
			if err := applyDiff(files, diff); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func fileDiffs(before, after spec.Files) []bridge.FileDiff {
	names := cloneFiles(before)
	for name := range after {
		names[name] = nil
	}
	diffs := []bridge.FileDiff{}
	for _, name := range names.Paths() {
		old, had := before[name]
		new, has := after[name]
		if had == has && bytes.Equal(old, new) {
			continue
		}
		op := "modify"
		if !had {
			op = "add"
		}
		if !has {
			op = "delete"
		}
		diffs = append(diffs, bridge.FileDiff{Path: name, Op: op, Before: string(old), After: string(new)})
	}
	return diffs
}

func applyDiff(files spec.Files, diff bridge.FileDiff) error {
	if !validFilePath(diff.Path) {
		return bridge.Errorf(bridge.CodeInvalid, "invalid definition path "+diff.Path)
	}
	current, exists := files[diff.Path]
	switch diff.Op {
	case "add":
		if exists {
			return bridge.Errorf(bridge.CodeConflict, "file already exists: "+diff.Path)
		}
		files[diff.Path] = []byte(diff.After)
	case "delete":
		if !exists || string(current) != diff.Before {
			return bridge.Errorf(bridge.CodeConflict, "cannot delete changed file: "+diff.Path)
		}
		delete(files, diff.Path)
	case "modify":
		if !exists {
			return bridge.Errorf(bridge.CodeConflict, "missing changed file: "+diff.Path)
		}
		if string(current) == diff.Before {
			files[diff.Path] = []byte(diff.After)
			return nil
		}
		// Reapply the edit, not its old full after-text, so cancelling an
		// earlier change does not silently bring that cancelled edit back.
		dmp := diffmatchpatch.New()
		dmp.DiffTimeout = 0
		merged, applied := dmp.PatchApply(dmp.PatchMake(diff.Before, diff.After), string(current))
		for _, ok := range applied {
			if !ok {
				return bridge.Errorf(bridge.CodeConflict, "cannot reapply change: "+diff.Path)
			}
		}
		files[diff.Path] = []byte(merged)
	default:
		return bridge.Errorf(bridge.CodeInvalid, "invalid file diff operation")
	}
	return nil
}

func cloneFiles(files spec.Files) spec.Files {
	copy := make(spec.Files, len(files))
	for name, data := range files {
		copy[name] = bytes.Clone(data)
	}
	return copy
}

func validFilePath(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsAny(name, "\\\x00") && !strings.HasPrefix(name, ".") && path.Clean(name) == name
}

func validateFiles(files spec.Files) error {
	for name := range files {
		if !validFilePath(name) {
			return bridge.Errorf(bridge.CodeInvalid, "invalid definition path "+name)
		}
	}
	return nil
}

func writeFiles(dir string, files spec.Files) error {
	if err := validateFiles(files); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, name := range files.Paths() {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[name], 0600); err != nil {
			return err
		}
	}
	return nil
}

func replaceFiles(dir string, files spec.Files) error {
	stage, err := os.MkdirTemp(dir, ".files-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := writeFiles(stage, files); err != nil {
		return err
	}
	current, backup := filepath.Join(dir, "files"), stage+"-previous"
	if err := os.Rename(current, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, current); err != nil {
		return errors.Join(err, os.Rename(backup, current))
	}
	return os.RemoveAll(backup)
}
