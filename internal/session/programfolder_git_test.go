package session

// A PROGRAM'S COPY, CUT AND FINISHED IN REAL GIT (programcopy.go): the copy is
// cut with the repository's hooks off and leaves nothing behind when the cut
// fails, the ending names the branch and leaves the person's own alone, the
// commit that finishes a run goes whatever the program's notes folder is, and
// what cannot be committed is kept as a patch.

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

// A COPY IS CUT WITH THE REPOSITORY'S HOOKS OFF. A post-checkout hook that
// fails — an LFS hook on a PATH with no git-lfs — would fail the cut after the
// copy was made, and the program has nothing for a hook to do: its copy is cut
// from a commit, whether the hooks are in .git/hooks or where core.hooksPath
// points.
func TestACopyIsCutWithTheRepositorysHooksOff(t *testing.T) {
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
			if head := currentBranch(folder.Dir); head != folder.Branch {
				t.Fatalf("the copy is on %q after the cut, want %q", head, folder.Branch)
			}
			if head := currentBranch(repo); head != "work" {
				t.Fatalf("the person's checkout is on %q, want it where it was", head)
			}
			end := folder.Finish("")
			if end.Kept || end.Refused != "" || end.CopyLeft != "" || branchCommit(repo, folder.Branch) == "" {
				t.Fatalf("a run that changed nothing ended %+v, want its branch kept and its copy gone", end)
			}
			if _, err := os.Stat(mark); !os.IsNotExist(err) {
				t.Fatalf("the repository's hook ran on codeaf's cut: %v", err)
			}
		})
	}
}

// A CUT THAT FAILED LEAVES NOTHING BEHIND: no branch in the person's
// repository, no copy, no worktree git remembers, and nothing owed or held.
func TestACutThatFailedLeavesNoBranchBehind(t *testing.T) {
	repo := newTestRepo(t)
	// A FILE WHERE `task/` WOULD BE A FOLDER is a branch git cannot make.
	writeFile(t, filepath.Join(repo, ".git", "refs", "heads", "task"), "")
	before, _ := filepath.Glob(filepath.Join(programCopyRoot(), "*"))
	_, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "Fix the parser", Holder: "task 7 (Fix the parser)", Keep: t.TempDir()})
	if err == nil || !strings.HasPrefix(err.Error(), "could not cut fake's copy of "+repo+": ") {
		t.Fatalf("PrepareProgramFolder = %v, want the cut refused", err)
	}
	if head := currentBranch(repo); head != "work" {
		t.Fatalf("the checkout is on %q, want the person's branch", head)
	}
	if branches := strings.TrimSpace(gitOut(t, repo, "for-each-ref", "refs/heads/task/")); branches != "" {
		t.Fatalf("the failed cut left a branch behind: %q", branches)
	}
	if after, _ := filepath.Glob(filepath.Join(programCopyRoot(), "*")); len(after) != len(before) || worktreeCount(t, repo) != 1 {
		t.Fatalf("the failed cut left a copy behind: %v, %d worktrees", after, worktreeCount(t, repo))
	}
	if holder := programFolderHolder(canonicalPath(repo)); holder != "" {
		t.Fatalf("the failed cut still holds the folder: %q", holder)
	}
}

// moveBranch puts a commit of nobody's on branch without checking it out, the
// way a person committing on their own branch while the program works leaves
// it, and answers the commit.
func moveBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", branch))
	moved := strings.TrimSpace(gitOut(t, repo, "-c", "user.name=s", "-c", "user.email=s@s", "commit-tree", tip+"^{tree}", "-p", tip, "-m", "a commit nobody read"))
	mustGit(t, repo, "update-ref", "refs/heads/"+branch, moved)
	return moved
}

// THE ENDING NAMES THE PROGRAM'S BRANCH AND LEAVES THE PERSON'S ALONE. The
// person may commit on their own branch while the program works in its copy —
// that is what the copy is for — and nothing of codeaf's moves it back, checks
// it out, or says a word about it.
func TestTheEndingOfARunInACopyNamesItsBranchAndLeavesThePersonsAlone(t *testing.T) {
	t.Run("with work", func(t *testing.T) {
		repo := newTestRepo(t)
		folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
		writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
		moved := moveBranch(t, repo, "work")
		said := folder.Finish("done").Sentence()
		want := "its work is on the branch " + folder.Branch + " in " + repo + ", 1 file; your checkout was not touched, and `git -C " +
			shellQuoted(repo) + " merge " + folder.Branch + "` brings it in"
		if said != want {
			t.Fatalf("the ending = %q, want %q", said, want)
		}
		if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")); tip != moved || currentBranch(repo) != "work" {
			t.Fatalf("codeaf moved the person's branch to %s (on %q)", tip, currentBranch(repo))
		}
	})
	t.Run("with nothing", func(t *testing.T) {
		repo := newTestRepo(t)
		folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
		end := folder.Finish("")
		want := "it changed nothing; its branch " + folder.Branch + " in " + repo + " is kept where it began, and your checkout was not touched"
		if said := end.Sentence(); said != want || branchCommit(repo, folder.Branch) != folder.Start {
			t.Fatalf("the ending = %q, want %q and the branch kept", said, want)
		}
	})
}

// THE COMMIT THAT FINISHES A RUN GOES WHATEVER THE NOTES FOLDER IS. A notes
// folder named by an exclude pathspec made `git add` exit 1 whenever it was
// there and ignored — senior-dev ignores its own in every repository — so its
// leftovers were never committed. The notes never enter the branch, and a run
// that changed nothing keeps its branch as it was cut.
func TestTheLeftoversAreCommittedWhateverTheNotesFolderIs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ready func(t *testing.T, repo string)
	}{
		{"ignored", func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), ".fake-notes/\n")
		}},
		{"not ignored", func(*testing.T, string) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			tc.ready(t, repo)
			folder := prepareIn(t, notesProgram(), repo, "Fix the parser")
			writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
			writeFile(t, filepath.Join(folder.Dir, ".fake-notes", "checklist.md"), "- [x] fix\n")
			end := folder.Finish("done")
			if end.Refused != "" || !end.Kept {
				t.Fatalf("the run's leftovers were not committed: %+v", end)
			}
			if files := strings.Fields(gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch)); strings.Join(files, " ") != "fix.go shared.txt" {
				t.Fatalf("the branch holds %q, want the work and none of the notes", files)
			}
			if _, err := os.Stat(filepath.Join(folder.Keep, "fake", "checklist.md")); err != nil {
				t.Fatalf("the notes were not kept in the run's record folder: %v", err)
			}
		})
		t.Run(tc.name+", changing nothing", func(t *testing.T) {
			repo := newTestRepo(t)
			tc.ready(t, repo)
			folder := prepareIn(t, notesProgram(), repo, "Fix the parser")
			writeFile(t, filepath.Join(folder.Dir, ".fake-notes", "checklist.md"), "- [ ] fix\n")
			if end := folder.Finish(""); end.Kept || end.Refused != "" || branchCommit(repo, folder.Branch) != folder.Start {
				t.Fatalf("a run that changed nothing ended %+v, want its branch kept where it was cut", end)
			}
		})
	}
}

// A MERGE THE PROGRAM LEFT HALF DONE IS NOT COMMITTED: a commit then would
// conclude it, conflict markers and all, under codeaf's name. What it left is
// kept as a patch in the run's record folder, and the copy still goes.
func TestAMergeTheProgramLeftHalfDoneIsNotCommitted(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Fix the parser")
	copyDir := folder.Dir
	mustGit(t, copyDir, "checkout", "-q", "-b", "other", "work")
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "theirs\n")
	mustGit(t, copyDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "theirs")
	mustGit(t, copyDir, "checkout", "-q", folder.Branch)
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "ours\n")
	mustGit(t, copyDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "ours")
	if _, err := git(copyDir, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "other"); err == nil {
		t.Fatal("the merge did not stop on its conflict")
	}
	head := strings.TrimSpace(gitOut(t, copyDir, "rev-parse", "HEAD"))
	end := folder.Finish("done")
	if end.Refused != copyDir+" is in the middle of a merge" || end.Patch == "" {
		t.Fatalf("the ending = %+v, want the merge named, nothing committed and a patch kept", end)
	}
	if tip := branchCommit(repo, folder.Branch); tip != head {
		t.Fatalf("a half-done merge was committed: the branch moved from %s to %s", head, tip)
	}
	if patch := readFile(t, end.Patch); !strings.Contains(patch, "shared.txt") {
		t.Fatalf("the patch does not hold what the merge left: %q", patch)
	}
	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Fatalf("the copy of a run that left a merge half done is still there: %v", err)
	}
}
