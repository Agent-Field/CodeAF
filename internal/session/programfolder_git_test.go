package session

// THE PERSON'S BRANCH AND CHECKOUT ARE NEVER CLAIMED SAFE WITHOUT BEING READ
// (programfolder.go), in real git in temporary repositories: a switch runs with
// the repository's hooks off and is read again when it fails, the ending looks
// at the person's branch before it says it is as it was, and the commit that
// finishes a run goes whatever the program's notes folder is.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// notesProgram is the fake program with a notes folder of its own, the way
// senior-dev keeps `.senior-dev/`.
func notesProgram() delegate.Delegate {
	program := testPrograms("fake")[0]
	program.Notes = ".fake-notes"
	return program
}

// prepareIn readies repo for a run of program, failing the test on a refusal.
func prepareIn(t *testing.T, program delegate.Delegate, repo, title string) *ProgramFolder {
	t.Helper()
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo, Title: title, Holder: "task 7 (" + title + ")", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return folder
}

// failingHook installs a post-checkout hook in hooks that fails and leaves a
// mark, the way an LFS hook does on a PATH with no git-lfs, and answers where
// the mark would be.
func failingHook(t *testing.T, hooks string) string {
	t.Helper()
	mark := filepath.Join(t.TempDir(), "the-hook-ran")
	writeFile(t, filepath.Join(hooks, "post-checkout"), "#!/bin/sh\necho ran > '"+mark+"'\nexit 2\n")
	if err := os.Chmod(filepath.Join(hooks, "post-checkout"), 0o755); err != nil {
		t.Fatal(err)
	}
	return mark
}

// A SWITCH RUNS WITH THE REPOSITORY'S HOOKS OFF. A post-checkout hook that
// fails used to make `git switch -c` exit non-zero after HEAD had moved, and
// the refusal then said nothing was changed while the person's checkout sat
// on an orphan branch; the switch back of a run that changed nothing failed
// the same way. Both switches go between two names for one commit, so no hook
// has anything to do — neither in .git/hooks nor where core.hooksPath points.
func TestAProgramsSwitchesRunWithTheRepositorysHooksOff(t *testing.T) {
	for _, where := range []string{"git-hooks", "hooks-path"} {
		t.Run(where, func(t *testing.T) {
			repo := newTestRepo(t)
			hooks := filepath.Join(repo, ".git", "hooks")
			if where == "hooks-path" {
				hooks = filepath.Join(t.TempDir(), "husky")
				mustGit(t, repo, "config", "core.hooksPath", hooks)
			}
			mark := failingHook(t, hooks)
			folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
			if head := currentBranch(repo); head != folder.Branch {
				t.Fatalf("the checkout is on %q after the cut, want %q", head, folder.Branch)
			}
			end := folder.Finish("")
			if !end.Dropped || end.Refused != "" {
				t.Fatalf("a run that changed nothing ended %+v, want its branch dropped", end)
			}
			if head := currentBranch(repo); head != "work" {
				t.Fatalf("the checkout is on %q after a run that changed nothing, want the person's branch", head)
			}
			if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
				t.Fatalf("the empty branch was left behind: %q", branches)
			}
			if _, err := os.Stat(mark); !os.IsNotExist(err) {
				t.Fatalf("the repository's hook ran on codeaf's switch: %v", err)
			}
		})
	}
}

// A CUT THAT FAILED IS READ AGAIN BEFORE IT IS ANSWERED. A lock git could not
// take after it had made the branch left a `task/…` branch in the person's
// repository that nothing knew about; now the stray branch is deleted, the
// checkout is where it was, and nothing is owed or held.
func TestACutThatFailedLeavesNoBranchBehind(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".git", "HEAD.lock"), "")
	_, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "Fix the parser", Holder: "task 7 (Fix the parser)", Keep: t.TempDir()})
	if err == nil || !strings.HasPrefix(err.Error(), "could not cut fake's branch in "+repo+": ") {
		t.Fatalf("PrepareProgramFolder = %v, want the cut refused", err)
	}
	if strings.Contains(err.Error(), "the checkout is now on") {
		t.Fatalf("the refusal says the checkout moved when it did not: %v", err)
	}
	if head := currentBranch(repo); head != "work" {
		t.Fatalf("the checkout is on %q, want the person's branch", head)
	}
	if branches := strings.TrimSpace(gitOut(t, repo, "branch", "--list", "task/*")); branches != "" {
		t.Fatalf("the failed cut left a branch behind: %q", branches)
	}
	if record, ok := readProgramFolder(canonicalPath(repo)); ok && record.Ended == "" {
		t.Fatalf("the failed cut left a run owed: %+v", record)
	}
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("the failed cut still holds the folder: %q", holder)
	}
}

// moveBranch puts a commit of nobody's on branch without checking it out, the
// way a program's shell that checked the person's branch out, committed there
// and switched back leaves it, and answers the commit.
func moveBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", branch))
	moved := strings.TrimSpace(gitOut(t, repo, "-c", "user.name=s", "-c", "user.email=s@s", "commit-tree", tip+"^{tree}", "-p", tip, "-m", "a commit nobody read"))
	mustGit(t, repo, "update-ref", "refs/heads/"+branch, moved)
	return moved
}

// THE ENDING LOOKS AT THE PERSON'S BRANCH BEFORE IT SAYS IT IS AS IT WAS. A
// program's shell that committed on the person's branch mid-run was reported
// as `your branch work is as it was`; a run that changed nothing then switched
// the checkout onto commits nobody had read and said it changed nothing.
func TestTheEndingSaysThePersonsBranchMovedDuringTheRun(t *testing.T) {
	t.Run("with work", func(t *testing.T) {
		repo := newTestRepo(t)
		start := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))
		folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
		writeFile(t, filepath.Join(repo, "fix.go"), "package fix\n")
		moved := moveBranch(t, repo, "work")
		said := folder.Finish("done").Sentence()
		want := "your branch work moved during the run, from " + shortSha(start) + " to " + shortSha(moved) +
			", and codeaf did not move it: look at it before you push or merge it; `git -C "
		if !strings.Contains(said, want) || strings.Contains(said, "as it was") {
			t.Fatalf("the ending = %q, want it to say %q", said, want)
		}
		if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")); tip != moved {
			t.Fatalf("codeaf moved the person's branch to %s", tip)
		}
	})
	t.Run("with nothing", func(t *testing.T) {
		repo := newTestRepo(t)
		start := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))
		folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
		moved := moveBranch(t, repo, "work")
		end := folder.Finish("")
		if end.Dropped || currentBranch(repo) != folder.Branch {
			t.Fatalf("a run whose person's branch moved was dropped onto it: %+v, on %q", end, currentBranch(repo))
		}
		want := "it changed nothing, but your branch work moved during the run, from " + shortSha(start) + " to " + shortSha(moved) +
			", so codeaf did not switch back to it: its empty branch " + folder.Branch + " is still checked out in " + repo
		if said := end.Sentence(); said != want {
			t.Fatalf("the ending = %q, want %q", said, want)
		}
	})
	t.Run("gone", func(t *testing.T) {
		repo := newTestRepo(t)
		start := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))
		folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
		writeFile(t, filepath.Join(repo, "fix.go"), "package fix\n")
		mustGit(t, repo, "branch", "-D", "work")
		said := folder.Finish("done").Sentence()
		want := "your branch work is gone: it was at " + shortSha(start) + " when the run began, and codeaf did not make it again"
		if !strings.HasSuffix(said, want) {
			t.Fatalf("the ending = %q, want it to end %q", said, want)
		}
	})
}

// AND THE RECEIPT READS IT TOO before it promises the branch does not move.
func TestTheReceiptSaysWhenThePersonsBranchHasAlreadyMoved(t *testing.T) {
	repo := newTestRepo(t)
	start := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))
	moved := moveBranch(t, repo, "work")
	record := &TaskCopyRecord{Dir: repo, Branch: "task/pong-abc123", Home: "work", HomeSha: start}
	want := "It is fake's: it works alone in " + repo + " itself, on a new branch task/pong-abc123; your branch work has already moved, from " +
		shortSha(start) + " to " + shortSha(moved) + ", and codeaf does not move it, and when it ends task/pong-abc123 stays checked out there with its work."
	if got := delegateReceipt(repo, testPrograms("fake")[0], record); got != want {
		t.Fatalf("the receipt = %q, want %q", got, want)
	}
}

// THE COMMIT THAT FINISHES A RUN GOES WHATEVER THE NOTES FOLDER IS. A notes
// folder named by an exclude pathspec made `git add` exit 1 whenever it was
// there and ignored — senior-dev ignores its own in every repository — so a
// folder whose notes predated the run never had its leftovers committed and
// never had an empty branch dropped.
func TestTheLeftoversAreCommittedWhateverTheNotesFolderIs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ready func(t *testing.T, repo string)
	}{
		{"there and ignored", func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, ".fake-notes", "old.md"), "an earlier run's checklist\n")
			writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), ".fake-notes/\n")
		}},
		{"there and not ignored", func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, ".fake-notes", "old.md"), "an earlier run's checklist\n")
		}},
		{"not there", func(*testing.T, string) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			tc.ready(t, repo)
			folder := prepareIn(t, notesProgram(), repo, "Fix the parser")
			writeFile(t, filepath.Join(repo, "fix.go"), "package fix\n")
			writeFile(t, filepath.Join(repo, ".fake-notes", "checklist.md"), "- [x] fix\n")
			end := folder.Finish("done")
			if end.Refused != "" || !end.Kept {
				t.Fatalf("the run's leftovers were not committed: %+v", end)
			}
			if files := strings.Fields(gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch)); strings.Join(files, " ") != "fix.go shared.txt" {
				t.Fatalf("the branch holds %q, want the work and none of the notes", files)
			}
			if staged := strings.TrimSpace(gitOut(t, repo, "diff", "--cached", "--name-only")); staged != "" {
				t.Fatalf("the notes were left staged: %q", staged)
			}
		})
		t.Run(tc.name+", changing nothing", func(t *testing.T) {
			repo := newTestRepo(t)
			tc.ready(t, repo)
			folder := prepareIn(t, notesProgram(), repo, "Fix the parser")
			writeFile(t, filepath.Join(repo, ".fake-notes", "checklist.md"), "- [ ] fix\n")
			if end := folder.Finish(""); !end.Dropped || end.Refused != "" {
				t.Fatalf("a run that changed nothing ended %+v, want its branch dropped", end)
			}
			if head := currentBranch(repo); head != "work" {
				t.Fatalf("the checkout is on %q, want the person's branch", head)
			}
		})
	}
}

// A CHECKOUT THE PROGRAM LEFT IN THE MIDDLE OF A MERGE IS NOT COMMITTED: a
// commit then would conclude the merge, conflict markers and all, under
// codeaf's name.
func TestAMergeTheProgramLeftHalfDoneIsNotCommitted(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	mustGit(t, repo, "checkout", "-q", "-b", "other", "work")
	writeFile(t, filepath.Join(repo, "shared.txt"), "theirs\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "theirs")
	mustGit(t, repo, "checkout", "-q", folder.Branch)
	writeFile(t, filepath.Join(repo, "shared.txt"), "ours\n")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "ours")
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "other"); err == nil {
		t.Fatal("the merge did not stop on its conflict")
	}
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	end := folder.Finish("done")
	if end.Refused != repo+" is in the middle of a merge" {
		t.Fatalf("the ending = %+v, want the merge named and nothing committed", end)
	}
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != head {
		t.Fatalf("a half-done merge was committed: HEAD moved from %s to %s", head, after)
	}
}
