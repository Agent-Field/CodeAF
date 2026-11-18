package main

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	homepkg "github.com/Agent-Field/codeaf/internal/home"
)

// These tests pin the one fact #1758 is about: `codeaf exec` works in the
// directory it was pointed at, and the harness's own files — the turn recorder,
// its stream sidecar, the .obs spills — land in codeaf's state root, NOT in
// that directory. A person who runs exec in their repository and then commits
// with `git add -A` must not publish the run's machinery.

// gitRepoForExec makes a fresh repository with a seed commit, the shape a
// person's checkout is in when they reach for exec.
func gitRepoForExec(t *testing.T) string {
	t.Helper()
	if _, err := osexec.LookPath("git"); err != nil {
		t.Skip("no git on PATH to read the workspace's status with")
	}
	dir := t.TempDir()
	if out, err := gitIn(dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := gitIn(dir, "config", "user.email", "test@example.invalid"); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	if out, err := gitIn(dir, "config", "user.name", "test"); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	return dir
}

// workspaceStatus is what git says about the directory after a run, one line
// per dirty path.
func workspaceStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := gitIn(dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status in %s: %v\n%s", dir, err, out)
	}
	return out
}

// refuseTheWorkspace holds the two facts every test here asserts: git sees
// nothing in the person's directory, and none of the machinery's names are in
// it either — the second so a person without git in the loop is covered too.
func refuseTheWorkspace(t *testing.T, dir string) {
	t.Helper()
	if dirty := workspaceStatus(t, dir); strings.TrimSpace(dirty) != "" {
		t.Fatalf("exec left the person's workspace dirty:\n%s", dirty)
	}
	for _, machinery := range []string{".codeaf", ".obs"} {
		if _, err := os.Stat(filepath.Join(dir, machinery)); err == nil {
			t.Fatalf("exec left its own %s directory in the person's workspace", machinery)
		}
	}
}

// scratchTraceFor is where this workspace's turn recorder lives once scratch
// has been moved to the state root: <state root>/scratch/<workspace name>,
// keyed the same way `codeaf plan run` keys its own. The location is spelled
// with [home.Join] — the one seam the state root resolves through, and the
// same call the door makes — rather than by hand, so the assertion says where
// the recorder must be however the state root itself is resolved.
func scratchTraceFor(t *testing.T, dir string) string {
	t.Helper()
	if os.Getenv(homepkg.EnvVar) == "" {
		t.Fatal("CODEAF_HOME is unset; the test did not isolate its state root")
	}
	return homepkg.Join("scratch", filepath.Base(dir), ".codeaf", "trace", "task-1.trace.log")
}

// runOneScratchExec drives one exec run in-process against the named
// directory, with the walls kept tight so the run settles fast.
func runOneScratchExec(t *testing.T, model, dir string) {
	t.Helper()
	if err := runExec([]string{
		"answer with one sentence", "--dir", dir, "--max-turns", "2",
		"--token-budget", "2000", "--model", model, "--json",
	}); err != nil {
		t.Fatalf("the exec run did not settle cleanly: %v", err)
	}
}

// TestExecLeavesTheWorkspaceGitClean is the issue's own replication, driven
// in-process: one exec in a fresh git repository, then `git status
// --porcelain`, which must be empty. The recorder has to have gone somewhere, so its new
// address under the state root is asserted beside it.
func TestExecLeavesTheWorkspaceGitClean(t *testing.T) {
	model, _ := execPendingHome(t)
	repo := gitRepoForExec(t)

	runOneScratchExec(t, model, repo)

	refuseTheWorkspace(t, repo)
	trace := scratchTraceFor(t, repo)
	if _, err := os.Stat(trace); err != nil {
		t.Fatalf("the turn recorder did not land where scratch was moved to (%s): %v", trace, err)
	}
}

// TestExecScratchAppendsAcrossRuns pins the other half of the move: two runs
// of exec in the same directory share one scratch home keyed by the
// workspace's name, so the second run APPENDS to the recorder the first wrote
// instead of starting a new file somewhere else — and still leaves the
// workspace clean.
func TestExecScratchAppendsAcrossRuns(t *testing.T) {
	model, _ := execPendingHome(t)
	repo := gitRepoForExec(t)

	runOneScratchExec(t, model, repo)
	trace := scratchTraceFor(t, repo)
	first, err := os.Stat(trace)
	if err != nil {
		t.Fatalf("the turn recorder after the first run: %v", err)
	}

	runOneScratchExec(t, model, repo)

	second, err := os.Stat(trace)
	if err != nil {
		t.Fatalf("the turn recorder after the second run: %v", err)
	}
	if second.Size() <= first.Size() {
		t.Fatalf("the second run did not append to the recorder it already wrote: %d bytes, was %d",
			second.Size(), first.Size())
	}
	refuseTheWorkspace(t, repo)
}
