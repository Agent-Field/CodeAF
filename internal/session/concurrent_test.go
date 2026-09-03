package session

// Two aforge windows, one directory.
//
// Everything here simulates the shape a person actually works in — two sessions
// on one repository at the same moment — and the thing each test is watching for
// is the harness treating the other window's live work as its own leftovers.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

// ── the working copy ────────────────────────────────────────────────────────

// THE DEFECT: task ids come from a counter that starts at one in every fresh
// conversation, so two windows both call their first task 1. With the id alone
// in the path, the second one to start found the directory occupied, removed the
// worktree and deleted the tree — with the first window's uncommitted edits in
// it, while the first window was still writing into it.
func TestTwoSessionsDoNotDestroyEachOthersWorktrees(t *testing.T) {
	repo := newTestRepo(t)

	first, err := prepareTaskTree(Place{}, repo, "aaaa1111aaaa1111", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree (first session): %v", err)
	}
	// What the first window's node has written and not committed: the work the
	// old code destroyed.
	writeFile(t, filepath.Join(first.dir, "in-progress.txt"), "half of it\n")

	second, err := prepareTaskTree(Place{}, repo, "bbbb2222bbbb2222", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree (second session): %v", err)
	}

	if _, err := os.Stat(filepath.Join(first.dir, "in-progress.txt")); err != nil {
		t.Fatalf("the second session destroyed the first one's uncommitted work: %v", err)
	}
	if got := readFile(t, filepath.Join(first.dir, "in-progress.txt")); got != "half of it\n" {
		t.Fatalf("the first session's uncommitted work reads %q", got)
	}
	if first.dir == second.dir {
		t.Fatalf("both sessions were given one directory: %s", first.dir)
	}
	// And the worktree is still a worktree, not a directory git has forgotten.
	if out, err := git(first.dir, "status", "--porcelain"); err != nil {
		t.Fatalf("the first session's worktree is broken: %v\n%s", err, out)
	}
	list := gitOut(t, repo, "worktree", "list")
	for _, dir := range []string{first.dir, second.dir} {
		if !strings.Contains(list, dir) {
			t.Fatalf("%s is not registered:\n%s", dir, list)
		}
	}

	// Both come home, and both bring their own work with them.
	writeFile(t, filepath.Join(second.dir, "the-other.txt"), "all of it\n")
	if merge, detail, _ := first.comeHome("do the thing", []string{"in-progress.txt"}); merge != mergeMerged {
		t.Fatalf("the first session's merge = %q (%s)", merge, detail)
	}
	if merge, detail, _ := second.comeHome("do the thing", []string{"the-other.txt"}); merge != mergeMerged {
		t.Fatalf("the second session's merge = %q (%s)", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "in-progress.txt")); got != "half of it\n" {
		t.Fatalf("the first session's work did not land: %q", got)
	}
	if got := readFile(t, filepath.Join(repo, "the-other.txt")); got != "all of it\n" {
		t.Fatalf("the second session's work did not land: %q", got)
	}
}

// The same session finding its own directory occupied is the case the forced
// remove was written for — a run that died with the process — and it still
// reclaims, because one live process holds one session id.
func TestASessionReclaimsItsOwnLeftoverWorktree(t *testing.T) {
	repo := newTestRepo(t)

	dead, err := prepareTaskTree(Place{}, repo, "cccc3333cccc3333", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(dead.dir, "leftover.txt"), "from the run that died\n")

	resumed, err := prepareTaskTree(Place{}, repo, "cccc3333cccc3333", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree (resumed): %v", err)
	}
	if resumed.dir != dead.dir {
		t.Fatalf("the resumed session moved: %s then %s", dead.dir, resumed.dir)
	}
	if _, err := os.Stat(filepath.Join(resumed.dir, "leftover.txt")); !os.IsNotExist(err) {
		t.Fatalf("the stale worktree was not reclaimed (%v)", err)
	}
	if out, err := git(resumed.dir, "status", "--porcelain"); err != nil {
		t.Fatalf("the reclaimed worktree is broken: %v\n%s", err, out)
	}
}

// A session with no journal still gets a name of its own, and never the shared
// one two unfiled windows would both answer to.
func TestAnUnfiledSessionStillGetsItsOwnName(t *testing.T) {
	named := taskTreeSession("aaaa1111aaaa1111")
	if named != "aaaa1111aaaa1111" {
		t.Fatalf("a filed session is named %q", named)
	}
	unfiled := taskTreeSession("")
	if unfiled == named || strings.TrimSpace(unfiled) == "" {
		t.Fatalf("an unfiled session is named %q", unfiled)
	}
	if again := taskTreeSession("  "); again != unfiled {
		t.Fatalf("one process answered to two unfiled names: %q then %q", unfiled, again)
	}
}

// A worktree that has gone home takes its session's directory with it, so a
// repository does not collect one empty directory per conversation.
func TestAMergedWorktreeLeavesNoEmptyDirectoryBehind(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "dddd4444dddd4444", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "done.txt"), "all of it\n")
	if merge, detail, _ := tree.comeHome("do the thing", []string{"done.txt"}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s)", merge, detail)
	}
	if _, err := os.Stat(filepath.Dir(tree.dir)); !os.IsNotExist(err) {
		t.Fatalf("the session's directory is still there (%v)", err)
	}
}

// ── the lock over the root repository ───────────────────────────────────────

// THE DEFECT: the lock was a mutex, which one process holds and another cannot
// see. This is the proof that it now reaches the filesystem — a second opener,
// standing in for the other terminal, is refused while the lock is held and
// admitted the moment it is released.
func TestTheGitRootLockIsVisibleToAnotherProcess(t *testing.T) {
	root := t.TempDir()

	release := lockGitRoot(Place{}, root)
	other := otherWindowsLock(t, Place{}, root)
	defer other.Close()
	if err := filelock.Lock(other, true, true); err == nil {
		t.Fatal("another window took the lock while this one held it")
	} else if !filelock.IsBusy(err) {
		t.Skipf("this filesystem does not do locks: %v", err)
	}

	release()
	if err := filelock.Lock(other, true, true); err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	_ = filelock.Unlock(other)
}

// And the loser waits rather than failing: a second session's merge is ordinary
// work that has to happen, just not at this instant.
func TestTheGitRootLockWaitsForTheOtherWindow(t *testing.T) {
	root := t.TempDir()

	other := otherWindowsLock(t, Place{}, root)
	defer other.Close()
	if err := filelock.Lock(other, true, true); err != nil {
		t.Skipf("this filesystem does not do locks: %v", err)
	}

	held := 200 * time.Millisecond
	released := make(chan struct{})
	go func() {
		time.Sleep(held)
		_ = filelock.Unlock(other)
		close(released)
	}()

	start := time.Now()
	release := lockGitRoot(Place{}, root)
	waited := time.Since(start)
	release()
	<-released

	if waited < held/2 {
		t.Fatalf("the lock was taken after %s, while the other window held it", waited)
	}
	if waited > gitRootPatience {
		t.Fatalf("the wait outlasted its patience: %s", waited)
	}
}

// otherWindowsLock is the second terminal: the same file production derives,
// opened the way production opens it, so a test can never prove the lock works
// by locking a path nobody else would have picked.
func otherWindowsLock(t *testing.T, place Place, root string) *os.File {
	t.Helper()
	file := openGitRootLock(place, root)
	if file == nil {
		t.Fatalf("no root lock could be opened for %s", root)
	}
	return file
}
