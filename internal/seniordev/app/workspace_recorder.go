//go:build !windows

package app

import (
	"context"
)

// workspaceRecorder is how a run identifies, compares, freezes and restores
// the workspace tree. The run's logic is written against this and never against
// git: git is one way to keep these promises, not the only one.
//
// Identifiers (the strings returned by Base, Snapshot and Record) are opaque.
// The run passes them back in and compares them for equality; it never parses
// them. Under the git recorder they are object names, which is why they read
// like SHAs in the event stream.
type workspaceRecorder interface {
	// Kind names the recorder on the run contract: "git" or "snapshot".
	Kind() string

	// Prepare checks the workspace is usable and arranges for senior-dev's own
	// artifacts to stay out of the answer. It runs once, before the base is
	// resolved, and its error refuses the run.
	Prepare(ctx context.Context) error

	// Base identifies the tree the run starts from. Everything the run reports
	// as changed is changed relative to this.
	Base(ctx context.Context) (string, error)

	// Snapshot identifies the tree as it stands right now, including files no
	// one has committed or added. Two identical trees give the same identifier
	// and two different trees do not.
	Snapshot() (string, error)

	// Record captures the tree named by treeID as something Restore can bring
	// back, and returns a handle to it. label is human-readable provenance.
	Record(treeID, label string) (string, error)

	// Publish makes a recorded handle reachable from outside this process under
	// a stable name, so a run killed between recording and finalizing still has
	// something to recover. Best-effort: a failure is noted, never fatal.
	Publish(name, handle string) error

	// Restore makes the working tree the one Record captured, and proves it by
	// re-identifying the result. wantTree is that proof; a mismatch is an error.
	Restore(handle, wantTree string) error

	// BaseTree resolves a base identifier from Base to the tree identifier it
	// names, so a base can be used as a restore target of last resort. ok is
	// false when the base cannot be resolved, which is not an error.
	BaseTree(base string) (treeID string, ok bool)

	// Change compares the working tree against a base identifier, excluding
	// senior-dev's own artifacts. This is what the submit gate consults.
	Change(base string) (soloTreeChange, error)

	// ListPaths enumerates every path belonging to the tree, ignores honoured,
	// sorted. It gives up rather than reading an unbounded tree: overBudget
	// reports that the listing exceeded maxBytes.
	ListPaths(ctx context.Context, maxBytes int) (
		paths []string, consumed int, overBudget bool, err error,
	)

	// Summary describes the run's final diff against base for the patch-summary
	// event. It is observational: nothing in the run acts on it, so a recorder
	// that cannot produce a field omits it rather than failing. The returned
	// status is the event's status.
	Summary(ctx context.Context, base string) (data map[string]any, status string)

	// CommitsOnWrite reports whether the recorder wants a checkpoint taken
	// after each file write. Git does, because a per-write commit is nearly
	// free; copying the tree after every edit would not be.
	CommitsOnWrite() bool
}

// newWorkspaceRecorder picks the recorder for a run. Git is the default and
// the only recorder chosen by inspecting the workspace; --in-place is an
// explicit choice, never inferred. Inference would be wrong in the case that
// matters most: a run inside a real repository that must not touch its
// history is indistinguishable, from the filesystem, from one that should.
func newWorkspaceRecorder(
	args cliArgs, workspace string, note func(string),
) workspaceRecorder {
	if args.InPlace {
		return newSnapshotRecorder(workspace, note)
	}
	return newGitRecorder(workspace, note)
}
