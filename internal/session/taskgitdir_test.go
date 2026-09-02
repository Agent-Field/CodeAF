package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAGitCommandWithNoDirectoryNeverRuns is the deterministic half of the
// checkout guard in hermetic_checkout_test.go, which can only witness a run
// that already went wrong and only inside a checkout.
//
// exec.Cmd reads an empty Dir as the calling process's own working directory,
// so a caller whose ground was never made used to commit into whatever
// repository the process happened to be sitting in: one run of this package
// wrote "task: Rewrite", "task: Measure" and "task: Paint" onto the branch of
// the worktree it was launched from. Every call site names a directory, so one
// that answers "" is a defect, and it fails here rather than somewhere else.
func TestAGitCommandWithNoDirectoryNeverRuns(t *testing.T) {
	// The proof that it never RAN, rather than merely never committed: this is
	// a real repository holding a real change, and a `git add` that reached it
	// would leave something staged.
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "aforge"}} {
		if out, err := git(repo, args...); err != nil {
			t.Skipf("no usable git here: %s %v", out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "note.md"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// t.Chdir puts the package's own directory back when this test ends, which
	// is what lets the standing there be the point: the process is inside a
	// repository, and the refused command must still not have touched it.
	t.Chdir(repo)

	out, err := git("", "add", "-A", "--", ".")
	if err == nil {
		t.Fatal("a git command with no directory was allowed to run")
	}
	if !strings.Contains(err.Error(), "no directory") {
		t.Errorf("the refusal reads %q, and it has to say what was missing", err)
	}
	if out != "" {
		t.Errorf("the command produced output, so it ran: %q", out)
	}
	if staged, problem := stagedPaths(repo); problem != "" {
		t.Fatalf("read the index: %s", problem)
	} else if len(staged) > 0 {
		t.Errorf("the refused command staged %v in the repository the process was standing in", staged)
	}
}
