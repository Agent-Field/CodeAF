package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// The default answer is per-workspace under the state root, so two doors
// pointed at the same workspace name share one recorder directory and a resume
// appends instead of forking. The env escape hatch answers verbatim, which is
// what lets CI name its own temp dir.
func TestScratchRootForDefaultsToStateRootPerWorkspace(t *testing.T) {
	root := filepath.Join("somewhere", "my-project")

	got := scratchRootFor(root)

	// Outside the workspace itself — never beside a person's repository — and
	// under the state root keyed by the workspace's base name. home.Dir() is
	// the one resolution the helper itself is allowed to rely on, so the test
	// asks it rather than guessing a quarantine the test binary may impose.
	if want := home.Join("scratch", filepath.Base(root)); got != want {
		t.Fatalf("scratchRootFor = %q, want %q", got, want)
	}
}

// The env var names the scratch root itself, not a parent, and is not joined
// onto anything — "point scratch at the runner temp dir" in the issue is a
// verbatim place.
func TestScratchRootForHonorsEnvVerbatim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_SCRATCH", dir)

	got := scratchRootFor(filepath.Join("somewhere", "my-project"))

	if got != dir {
		t.Fatalf("scratchRootFor = %q, want the env value verbatim %q", got, dir)
	}
}
