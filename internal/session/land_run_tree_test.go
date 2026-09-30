package session

// LandRunTree is the run engine's landing stated where the commit road lives:
// the working copy's own git status committed on its branch, answering the
// branch, the paths and the refusal. The tests here are the door's own — the
// same repository in a temp directory the landing tests use — and the run
// side's [`internal/run`] tests cover the door reached through `Land`.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A RUN'S WORK IS THE TREE'S OWN. Two files the tree says are changed land in
// one commit on the branch the copy stands on, and the commit is the paths the
// index built, not any list handed in.
func TestLandRunTreeCommitsTheTreesOwnWorkOntoItsBranch(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "second.txt"), "two\n")
	writeFile(t, filepath.Join(repo, "shared.txt"), "the changed line\n")

	branch, changed, refusal, err := LandRunTree(repo, "", "do the thing", "")
	if err != nil {
		t.Fatalf("LandRunTree: %v", err)
	}
	if branch != "work" {
		t.Fatalf("branch = %q, want the branch the copy stands on", branch)
	}
	if want := []string{"second.txt", "shared.txt"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	if refusal != "" {
		t.Fatalf("refusal = %q, want none on a landing", refusal)
	}
	landed := gitOut(t, repo, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"second.txt", "shared.txt"} {
		if !strings.Contains(landed, want) {
			t.Fatalf("the commit does not carry %s:\n%s", want, landed)
		}
	}
}

// A TREE WITH NOTHING TO LAND IS A REFUSAL, NOT A FAULT: the branch would
// carry what it always carried, so the door names no branch and says nothing
// happened.
func TestLandRunTreeRefusesATreeWithNothingToLand(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)

	branch, changed, refusal, err := LandRunTree(repo, "", "only read", "")
	if err != nil {
		t.Fatalf("LandRunTree: %v", err)
	}
	if branch != "" || len(changed) != 0 {
		t.Fatalf("branch = %q changed = %v, want no landing", branch, changed)
	}
	if refusal == "" {
		t.Fatal("refusal is empty, want the sentence that says there was nothing to land")
	}
}

// Contract 7: with a known base, a read-only run still reports no work.
func TestLandRunTreeWithBaseRefusesReadOnlyRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	branch, changed, refusal, err := LandRunTree(repo, base, "only read", "")
	if err != nil || branch != "" || len(changed) != 0 || refusal != runNothingToLand {
		t.Fatalf("read-only landing = %q, %v, %q, %v", branch, changed, refusal, err)
	}
}

// Contract 7: two commits that cancel each other's tree still came home, so
// landing names their touched file instead of claiming there was no work.
func TestLandRunTreeNamesCommittedThenRevertedWork(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	runCommitFile(t, repo, "reverted.txt", "add reverted")
	mustGit(t, repo, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "revert", "--no-edit", "HEAD")
	branch, changed, refusal, err := LandRunTree(repo, base, "work then revert", "")
	if err != nil || branch != "work" || refusal != "" || !reflect.DeepEqual(changed, []string{"reverted.txt"}) {
		t.Fatalf("landing = %q, %v, %q, %v", branch, changed, refusal, err)
	}
	for _, rev := range []string{"HEAD", "HEAD~1"} {
		if message := gitOut(t, repo, "log", "-1", "--format=%B", rev); strings.Count(message, "Assisted-by: CodeAF") != 1 {
			t.Fatalf("%s not signed once: %q", rev, message)
		}
	}
}

// THE SWITCH IS THE BELT'S. With CODEAF_TASK_BELT off the door refuses and not
// one byte of the tree moves: no commit, no index, the work still on the floor.
func TestLandRunTreeLeavesTheTreeAloneWithTheFlagOff(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	repo := newTestRepo(t)
	before := gitOut(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "second.txt"), "two\n")

	if _, _, _, err := LandRunTree(repo, "", "do the thing", ""); err == nil {
		t.Fatal("LandRunTree with the belt off returned no error, want a refusal")
	}
	if after := gitOut(t, repo, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved with the belt off: %s -> %s", before, after)
	}
	if out := gitOut(t, repo, "status", "--porcelain"); !strings.Contains(out, "second.txt") {
		t.Fatalf("the tree's own work is not still uncommitted:\n%s", out)
	}
}
