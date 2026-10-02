//go:build !windows

package util

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTestRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTestRun(t, dir, "init", "-q")
	gitTestRun(t, dir, "config", "user.email", "test@example.com")
	gitTestRun(t, dir, "config", "user.name", "Test")
	return dir
}

func TestEnsureSeniorDevExcludedIdempotent(t *testing.T) {
	dir := initGitRepo(t)
	ok, err := EnsureSeniorDevExcluded(context.Background(), dir)
	if err != nil || !ok {
		t.Fatalf("ensure = %v, %v", ok, err)
	}
	ok, err = EnsureSeniorDevExcluded(context.Background(), dir)
	if err != nil || !ok {
		t.Fatalf("second ensure = %v, %v", ok, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, excludeSentinel) != 1 {
		t.Fatalf("exclude:\n%s", text)
	}
	for _, path := range ExcludedPaths {
		if strings.Count(text, path+"\n") != 1 {
			t.Fatalf("%q count in:\n%s", path, text)
		}
	}
}

// A task's working copy is a linked worktree, whose own info/ folder git does
// not read. The exclude has to land where git looks, or `.senior-dev/` is
// untracked work that a landing would commit.
func TestEnsureSeniorDevExcludedReachesALinkedWorktree(t *testing.T) {
	dir := initGitRepo(t)
	gitTestRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	copyDir := filepath.Join(t.TempDir(), "copy")
	gitTestRun(t, dir, "worktree", "add", "-q", "--detach", copyDir, "HEAD")
	if err := os.MkdirAll(filepath.Join(copyDir, ".senior-dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, ".senior-dev", "spec.md"), []byte("brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status := gitTestRun(t, copyDir, "status", "--porcelain", "--untracked-files=all"); !strings.Contains(status, ".senior-dev/") {
		t.Fatalf("the fixture is wrong: .senior-dev is not untracked before the exclude:\n%s", status)
	}
	ok, err := EnsureSeniorDevExcluded(context.Background(), copyDir)
	if err != nil || !ok {
		t.Fatalf("ensure = %v, %v", ok, err)
	}
	if status := gitTestRun(t, copyDir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, ".senior-dev") {
		t.Fatalf("senior-dev's folder is still untracked in the linked worktree:\n%s", status)
	}
}
