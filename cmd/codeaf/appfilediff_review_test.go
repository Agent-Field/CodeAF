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
