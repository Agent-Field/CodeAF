package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// AN IN-PLACE RUN NAMES ONLY WHAT IT CHANGED. The person's own uncommitted edit
// and their untracked file were there before the run and still hold what they
// held, so they are not the run's; a file the run wrote, a file of the
// person's the run edited further, and a file the run committed itself are.
func TestARunTreeSnapshotNamesOnlyTheRunsChanges(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own edit\n")
	writeFile(t, filepath.Join(repo, "secret.env"), "TOKEN=mine\n")
	writeFile(t, filepath.Join(repo, "draft.md"), "the person's draft\n")

	snapshot := SnapshotRunTree(repo)

	// What the run does.
	writeFile(t, filepath.Join(repo, "out.txt"), "written by the run\n")
	writeFile(t, filepath.Join(repo, "draft.md"), "the run finished the draft\n")
	writeFile(t, filepath.Join(repo, ".codeaf", "plandb.db"), "harness\n")
	writeFile(t, filepath.Join(repo, "committed.txt"), "the run committed this\n")
	mustGit(t, repo, "add", "committed.txt")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the run's own commit")

	root := canonicalPath(repo)
	want := []string{
		filepath.Join(root, "committed.txt"),
		filepath.Join(root, "draft.md"),
		filepath.Join(root, "out.txt"),
	}
	sort.Strings(want)
	got := snapshot.Changed()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the run's changes = %v, want %v", got, want)
	}
}

// A FOLDER THAT IS NOT A REPOSITORY HAS NO STATUS TO READ, and the snapshot
// answers nothing rather than guessing.
func TestARunTreeSnapshotOutsideARepositoryNamesNothing(t *testing.T) {
	dir := t.TempDir()
	snapshot := SnapshotRunTree(dir)
	if err := os.WriteFile(filepath.Join(dir, "out.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Changed(); len(got) != 0 {
		t.Fatalf("a folder with no repository named %v", got)
	}
}
