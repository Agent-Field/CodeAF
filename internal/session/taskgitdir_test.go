package session

import (
	"context"
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
// TestStatusReadsWithNoDirectoryNeverRuns is the same proof for the two call
// sites that built `git -C <dir>` themselves instead of asking [gitWith]:
// [worktreeDirtIn]'s dirty fingerprint and [UnsavedEditsNote]'s status reading.
// `git -C ""` is a no-op — git runs where the process is standing — so a direct
// exec.Command with an empty directory slipped past the refusal in gitWith.
// The repository the process stands in holds a TRACKED file with an unsaved
// edit, because [UnsavedEditsNote] reads with --untracked-files=no and an
// untracked file alone would leave it silent whether or not git ran.
func TestStatusReadsWithNoDirectoryNeverRuns(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "codeaf"}} {
		if _, err := fixtureGit(repo, args...); err != nil {
			t.Skipf("no usable git here: %v", err)
		}
	}
	writeFile(t, filepath.Join(repo, "note.md"), "work\n")
	mustGit(t, repo, "add", "-A", "--", ".")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "the note")
	writeFile(t, filepath.Join(repo, "note.md"), "more work\n")

	// Both readings see the edit when they are told where to look; that is what
	// makes their silence below a refusal rather than an empty status.
	if got := UnsavedEditsNote(repo); got != unsavedEditsWord {
		t.Fatalf("UnsavedEditsNote(repo) = %q, so the edit is not visible to a status read", got)
	}
	if got := worktreeDirtIn(context.Background(), repo); got == "" {
		t.Fatal("worktreeDirtIn(repo) answered \"\", so the edit is not visible to a status read")
	}

	t.Chdir(repo)

	if got := worktreeDirtIn(context.Background(), ""); got != "" {
		t.Errorf("worktreeDirtIn with no directory returned %q, so a git command ran", got)
	}
	if got := worktreeDirt(""); got != "" {
		t.Errorf("worktreeDirt with no directory returned %q, so a git command ran", got)
	}
	if got := UnsavedEditsNote(""); got != "" {
		t.Errorf("UnsavedEditsNote with no directory returned %q, so a git command ran", got)
	}
}

func TestAGitCommandWithNoDirectoryNeverRuns(t *testing.T) {
	// The proof that it never RAN, rather than merely never committed: this is
	// a real repository holding a real change, and a `git add` that reached it
	// would leave something staged.
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "codeaf"}} {
		if out, err := fixtureGit(repo, args...); err != nil {
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

// A private commit must finish all its writers before a copy can be walked or
// removed. Override a repository that explicitly enables automatic maintenance
// and keep that repository's saved preference intact.
func TestPrivateGitDoesNotStartDetachedMaintenance(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "config", "--local", "maintenance.auto", "true")
	mustGit(t, repo, "config", "--local", "gc.auto", "1")
	trace := filepath.Join(t.TempDir(), "git.trace")
	t.Setenv("GIT_TRACE", trace)
	if out, err := privateGit(repo, "-c", "user.name=t", "-c", "user.email=t@t",
		"commit", "--allow-empty", "-qm", "private work"); err != nil {
		t.Fatalf("private commit: %v: %s", err, out)
	}
	calls := readFile(t, trace)
	if strings.Contains(calls, "maintenance run") || strings.Contains(calls, "gc --auto") {
		t.Fatalf("private commit started automatic maintenance: %s", calls)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "config", "--local", "maintenance.auto")); got != "true" {
		t.Fatalf("private command changed the repository preference: %q", got)
	}
}

func TestFixtureGitDisablesRepositoryMaintenance(t *testing.T) {
	repo := newTestRepo(t)
	for key, want := range map[string]string{"maintenance.auto": "false", "gc.auto": "0"} {
		if got := strings.TrimSpace(gitOut(t, repo, "config", "--local", key)); got != want {
			t.Fatalf("fixture %s = %q, want %q", key, got, want)
		}
	}
}

// Copy ownership, rather than the commit helper's name, decides the command's
// maintenance policy: the same landing road also serves an in-place run.
func TestTaskCommitSuppressesMaintenanceOnlyInOwnedCopies(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "person", true: "copy"}[owned], func(t *testing.T) {
			repo := newTestRepo(t)
			writeFile(t, filepath.Join(repo, "shared.txt"), "the completed work\n")
			trace := filepath.Join(t.TempDir(), "git.jsonl")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			_, _, _, err := commitTaskWorkAs(repo, "completed work", []string{"shared.txt"}, gitSignature{off: true}, false, owned)
			if err != nil {
				t.Fatal(err)
			}
			if suppressed := strings.Contains(readFile(t, trace), `"maintenance.auto=false"`); suppressed != owned {
				t.Fatalf("owned copy %t: maintenance override = %t", owned, suppressed)
			}
		})
	}
}
