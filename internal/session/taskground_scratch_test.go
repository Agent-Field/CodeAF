package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scratchInsideARepository fabricates the shape that moved a person's HEAD: a
// scratch directory made INSIDE a checkout, with GOTMPDIR and TMPDIR pointed at
// it the way a `go test` run does, and a working directory under it. It answers
// the enclosing repository and the directory the work would happen in.
func scratchInsideARepository(t *testing.T) (repo, work string) {
	t.Helper()
	repo = newTestRepo(t)
	scratch := filepath.Join(repo, ".gotmp")
	work = filepath.Join(scratch, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("make the scratch: %v", err)
	}
	// BOTH, because Go reads GOTMPDIR first and TMPDIR after it, and either one
	// pointed in here is the whole bug.
	t.Setenv("GOTMPDIR", scratch)
	t.Setenv("TMPDIR", scratch)
	return repo, work
}

// A ground never climbs out of a scratch directory its path lives in. The
// repository that merely CONTAINS the machine's temp directory is a repository
// this task was never given, so the reading answers nothing at all rather than
// the person's own checkout.
func TestAGroundNeverClimbsOutOfScratch(t *testing.T) {
	// The ordinary repository is made BEFORE the environment moves, so it sits
	// outside every scratch root the way a person's project does.
	outside := newTestRepo(t)

	repo, work := scratchInsideARepository(t)

	if root, ok := repositoryRoot(work); ok {
		t.Fatalf("repositoryRoot(%q) = %q, true — it climbed out of the scratch and onto %q", work, root, repo)
	}

	// A repository made INSIDE the scratch root is somebody's actual work for
	// the length of the run: it never climbs out, so it still answers.
	inner := filepath.Join(filepath.Dir(work), "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("make the inner repository: %v", err)
	}
	mustGit(t, inner, "init")
	root, ok := repositoryRoot(inner)
	if !ok || root != canonicalPath(inner) {
		t.Fatalf("repositoryRoot(%q) = %q, %v — a repository inside the scratch must still answer", inner, root, ok)
	}

	// And an ordinary repository outside every scratch root is untouched.
	root, ok = repositoryRoot(outside)
	if !ok || root != canonicalPath(outside) {
		t.Fatalf("repositoryRoot(%q) = %q, %v — an ordinary repository must still answer", outside, root, ok)
	}
}

// The refusal falls through to the rung that was already there: the node works
// in place, and the enclosing repository is left exactly as it was — no task
// branch cut off the person's HEAD, no worktree registered against it.
func TestATaskInScratchNeverBranchesTheEnclosingRepository(t *testing.T) {
	repo, work := scratchInsideARepository(t)

	before := gitOut(t, repo, "rev-parse", "HEAD")
	tree, err := prepareTaskTree(Place{}, work, "s1", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.dir != work {
		t.Fatalf("dir = %q, want the workspace itself (%q)", tree.dir, work)
	}
	if tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("tree = %+v, want inplace with no branch", tree)
	}
	if merge, _ := tree.comeHome("do the thing", nil); merge != mergeInPlace {
		t.Fatalf("comeHome = %q, want inplace", merge)
	}

	if branches := gitOut(t, repo, "branch", "--list"); strings.Contains(branches, "task/") {
		t.Fatalf("the enclosing repository grew a task branch:\n%s", branches)
	}
	if worktrees := gitOut(t, repo, "worktree", "list"); strings.Count(strings.TrimSpace(worktrees), "\n") != 0 {
		t.Fatalf("the enclosing repository grew a worktree:\n%s", worktrees)
	}
	if after := gitOut(t, repo, "rev-parse", "HEAD"); after != before {
		t.Fatalf("the enclosing repository's HEAD moved: %q -> %q", before, after)
	}
}

// The two lists are read in opposite directions, and merging them would loosen
// the write guard in exactly the configuration this fix exists to protect: a
// name on scratchPath's list EXEMPTS a task standing elsewhere from having to
// answer for a write there. So GOTMPDIR — which a developer points at a folder
// inside their own checkout — tightens the ground law and must never appear on
// the permissive side.
func TestGOTMPDIRTightensTheGroundLawAndNeverTheWriteGuard(t *testing.T) {
	// A path no temp root contains, so the only way it could read as scratch is
	// through GOTMPDIR itself.
	checkout := filepath.Join(string(filepath.Separator), "not-a-temp-root", "checkout")
	gotmp := filepath.Join(checkout, ".gotmp")
	t.Setenv("GOTMPDIR", gotmp)

	work := filepath.Join(gotmp, "work")
	if scratchPath(work) {
		t.Fatalf("scratchPath(%q) = true — GOTMPDIR is on the write guard's permissive list, which exempts a task from answering for a write into somebody's checkout", work)
	}
	// And the ground law, reading the other list, does refuse to climb out of it.
	if !climbsOutOfScratch(work, checkout) {
		t.Fatalf("climbsOutOfScratch(%q, %q) = false — the ground law is not reading GOTMPDIR", work, checkout)
	}
}
