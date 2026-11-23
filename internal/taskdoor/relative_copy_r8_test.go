//go:build linux || darwin

package taskdoor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// R7-5 preserves the linked-copy landing contract when Git uses relative worktree links.
func TestARelativeWorktreeLinkSurvivesResultInstallation(t *testing.T) {
	a, b := copyHomes(t)
	ground := copyRepo(t)
	local := filepath.Join(t.TempDir(), "task")
	copyGit(t, ground, "-c", "worktree.useRelativePaths=true", "worktree", "add", "-q", "-b", "task-relative", local, "HEAD")
	raw, err := os.ReadFile(filepath.Join(local, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	target := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir: "))
	if filepath.IsAbs(target) {
		relative, err := filepath.Rel(local, target)
		if err != nil {
			t.Fatal(err)
		}
		raw = []byte("gitdir: " + relative + "\n")
		if err := os.WriteFile(filepath.Join(local, ".git"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	in, err := taskdoor.SendCopy(t.Context(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "work")
	if err := taskdoor.ReceiveCopy(t.Context(), b, in, work); err != nil {
		t.Fatal(err)
	}
	copyFile(t, work, "edit.txt", "returned through a relative worktree")
	copyGit(t, work, "add", "--all")
	copyGit(t, work, "commit", "-qm", "relative return")
	result, err := taskdoor.SendResult(t.Context(), b, work, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveResult(t.Context(), a, result, local, in.Fingerprint); err != nil {
		t.Fatalf("relative worktree result refused: %v", err)
	}
	if got := copyGit(t, local, "rev-parse", "HEAD"); got != copyGit(t, work, "rev-parse", "HEAD") {
		t.Fatal("returned commit was not installed")
	}
	after, err := os.ReadFile(filepath.Join(local, ".git"))
	if err != nil || string(after) != string(raw) {
		t.Fatalf("relative worktree link changed: %q, %v", after, err)
	}
	if got := copyGit(t, local, "branch", "--show-current"); got != "task-relative" {
		t.Fatalf("landing branch = %q", got)
	}
}
