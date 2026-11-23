package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A bounded stdout pipe does not bound the memory Git uses while reading a
// very large worktree file or deleted blob. Refuse both before diffing them.
func TestAppDiffReviewLargeTrackedAndDeletedBase(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), strings.Repeat("x", appwire.FileReadMax+1))
	if d := diffT(t, repo, "a.txt", ""); d.State != appwire.DiffUnavailable || d.Reason != appwire.DiffReasonTooLarge || d.Text != "" {
		t.Fatalf("large tracked file was diffed: state=%s reason=%s bytes=%d", d.State, d.Reason, len(d.Text))
	}
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-qm", "large base")
	if err := os.Remove(filepath.Join(repo, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if d := diffT(t, repo, "a.txt", ""); d.State != appwire.DiffUnavailable || d.Reason != appwire.DiffReasonTooLarge || d.Text != "" {
		t.Fatalf("large deleted blob was diffed: state=%s reason=%s bytes=%d", d.State, d.Reason, len(d.Text))
	}
}

// Git can classify Latin-1 without NUL as text; JSON would silently replace its
// bytes. A preview must refuse that body rather than show altered contents.
func TestAppDiffReviewInvalidUTF8TrackedText(t *testing.T) {
	repo := diffRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("caf\xe9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := diffT(t, repo, "a.txt", ""); !d.Binary || d.Text != "" {
		t.Fatalf("invalid UTF-8 became a text diff: binary=%v bytes=%d", d.Binary, len(d.Text))
	}
}

func TestAppDiffReviewSourceRevisionGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	_, missing := os.Stat(path)
	if !appSameDiffSource(nil, missing, nil, missing) {
		t.Fatal("a deleted file that stays deleted changed revision")
	}
	write(t, path, "original\n")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !appSameDiffSource(before, nil, before, nil) {
		t.Fatal("an unchanged file changed revision")
	}
	if appSameDiffSource(nil, missing, before, nil) || appSameDiffSource(before, nil, nil, missing) {
		t.Fatal("creation or deletion was not detected")
	}
	write(t, path, "different length\n")
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if appSameDiffSource(before, nil, after, nil) {
		t.Fatal("a rewritten file was not detected")
	}
	other := path + ".new"
	write(t, other, "original\n")
	if err := os.Chtimes(other, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	after, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if appSameDiffSource(before, nil, after, nil) {
		t.Fatal("atomic replacement with the same size and time was not detected")
	}
}
