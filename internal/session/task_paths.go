package session

// task_paths.go spells the folders a task checkpoint names (node journals,
// heartbeat, worktree, run copies) against the session's named bases when the
// checkpoint is sealed in a cell (pathcodec.go, law L1), and resolves them
// against this machine's folders when it is read. A path under no base is
// machine-local and is cleared: the record keeps everything else.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (r *taskRecord) pathFields() []*string {
	return []*string{&r.Journal, &r.Beat, &r.OriginJournal, &r.Worktree, &r.Ground}
}

func (r *runRecord) pathFields() []*string {
	var fields []*string
	if r.Copy != nil {
		copied := *r.Copy
		r.Copy = &copied
		fields = append(fields, &copied.Dir, &copied.Root, &copied.Ground)
	}
	r.WorkspaceSet = cloneWorkspaceSet(r.WorkspaceSet)
	if r.WorkspaceSet != nil {
		for i := range r.WorkspaceSet.Roots {
			if copied := r.WorkspaceSet.Roots[i].Copy; copied != nil {
				fields = append(fields, &copied.Dir, &copied.Root, &copied.Ground)
			}
		}
	}
	if r.PendingRun != nil {
		pending := *r.PendingRun
		r.PendingRun = &pending
		fields = append(fields, &pending.Ground)
		if pending.WorkspaceSet != nil {
			request := *pending.WorkspaceSet
			request.Roots = append([]WorkspaceRootRequest(nil), request.Roots...)
			pending.WorkspaceSet = &request
			for i := range request.Roots {
				fields = append(fields, &request.Roots[i].Source, &request.Roots[i].Landing)
			}
		}
	}
	return fields
}

// clearPaths returns a copy of in with every non-empty path field rewritten by
// f, and cleared where f refuses it.
func clearPaths[T any, P interface {
	*T
	pathFields
}](in []T, f func(string) (string, bool)) []T {
	out := make([]T, len(in))
	copy(out, in)
	for i := range out {
		for _, field := range P(&out[i]).pathFields() {
			if *field != "" {
				*field, _ = f(*field)
			}
		}
	}
	return out
}

func (d taskDocument) mapPaths(f func(string) (string, bool)) taskDocument {
	d.Nodes = clearPaths[taskRecord](d.Nodes, f)
	d.Runs = clearPaths[runRecord](d.Runs, f)
	return d
}

// checkpointCodec is the codec of the checkpoint file at path, and false when
// the file is not inside a cell's state directory (the legacy layout keeps
// paths as they always were).
func checkpointCodec(path string) (pathCodec, bool) {
	state := filepath.Dir(path)
	if filepath.Base(state) != cellStateDir {
		return nil, false
	}
	dir := filepath.Dir(state)
	return codecOf(dir, summaryOf(dir)), true
}

// summaryOf is the workspace and ownership meta.json records, read without
// the rest of [LoadMeta]'s work: it names where this machine keeps the
// session's tools root.
func summaryOf(dir string) Meta {
	var m Meta
	if raw, err := os.ReadFile(filepath.Join(dir, placeMeta)); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}
