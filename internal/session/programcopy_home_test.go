package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// pinSharedCopies puts the account's shared copy folder in a folder of this
// test's own and answers it.
func pinSharedCopies(t *testing.T) string {
	t.Helper()
	shared := filepath.Join(t.TempDir(), "cache", "codeaf", "worktrees")
	previous := programCopyShared
	programCopyShared = func() string { return shared }
	t.Cleanup(func() { programCopyShared = previous })
	return shared
}

// A home the person keeps, the desktop app's `--home`, is backed up and opened
// by a person: its throwaway copies of their repositories go to the cache
// folder, each home in a folder of its own.
func TestAKeptHomesCopiesGoToTheCacheInAFolderOfItsOwn(t *testing.T) {
	shared := pinSharedCopies(t)
	login := t.TempDir()
	first, second := filepath.Join(t.TempDir(), "workaf"), filepath.Join(t.TempDir(), "workaf")
	t.Setenv(home.LoginEnvVar, login)

	t.Setenv(home.EnvVar, first)
	one := defaultProgramCopyRoot()
	t.Setenv(home.EnvVar, second)
	two := defaultProgramCopyRoot()

	for _, root := range []string{one, two} {
		if filepath.Dir(root) != shared {
			t.Errorf("a kept home's copies are made in %q, want a folder directly under %q", root, shared)
		}
	}
	if one == two {
		t.Errorf("two kept homes share the copy folder %q", one)
	}
	if again := defaultProgramCopyRoot(); again != two {
		t.Errorf("the same home answered %q and then %q", two, again)
	}
}

// A disposable home, one that only CODEAF_HOME moved, takes its copies with it
// and leaves the person's cache alone.
func TestADisposableHomesCopiesStayInsideIt(t *testing.T) {
	pinSharedCopies(t)
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	t.Setenv(home.LoginEnvVar, "")
	if got, want := defaultProgramCopyRoot(), filepath.Join(state, "worktrees"); got != want {
		t.Fatalf("a disposable home's copies are made in %q, want %q", got, want)
	}
}

// A kept home's copy of a repository counts as a copy for every install that
// works in it, because the exclude file git reads is the repository's own, and
// one install taking its lines out would strip them from another's live copy.
func TestACopyInAnotherInstallsFolderKeepsTheExcludeLines(t *testing.T) {
	shared := pinSharedCopies(t)
	repo := newTestRepo(t)
	mine := filepath.Join(shared, "workaf-aaaaaaaaaaaa")
	programCopyRootBefore := programCopyRoot
	programCopyRoot = func() string { return mine }
	t.Cleanup(func() { programCopyRoot = programCopyRootBefore })

	if programCopiesOnDisk(repo) {
		t.Fatal("a repository with no copy of it anywhere is said to have one")
	}
	other := filepath.Join(shared, "codeaf-bbbbbbbbbbbb", "repo-copy")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "task/other", other)
	if !programCopiesOnDisk(repo) {
		t.Fatal("another install's live copy was not counted, so this install would take the exclude lines out from under it")
	}
}
