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

func TestEagerCommit(t *testing.T) {
	dir := initGitRepo(t)
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, dir, "add", "file.txt")
	gitTestRun(t, dir, "commit", "-qm", "initial")
	if err := os.WriteFile(file, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	previous := skipEagerCommit.Load()
	skipEagerCommit.Store(false)
	defer func() { skipEagerCommit.Store(previous) }()
	EagerCommit(context.Background(), EagerCommitOptions{Cwd: dir, FilePath: file, Label: "write"})
	subject := strings.TrimSpace(gitTestRun(t, dir, "log", "-1", "--pretty=%s"))
	if subject != "wip(write): file.txt" {
		t.Fatalf("subject = %q", subject)
	}
	// The commit carries senior-dev's own identity, so a machine that was
	// never told who commits can still take it.
	author := strings.TrimSpace(gitTestRun(t, dir, "log", "-1", "--pretty=%cn <%ce>"))
	if author != CommitterName+" <"+CommitterEmail+">" {
		t.Fatalf("committer = %q", author)
	}
}

// A path spelled through a symlink still commits: the per-file commit is
// measured against git's resolved top level, and a workspace reached through a
// link (every temporary folder on macOS) used to walk out of the repository.
func TestEagerCommitThroughASymlinkedWorkspace(t *testing.T) {
	dir := initGitRepo(t)
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, dir, "add", "file.txt")
	gitTestRun(t, dir, "commit", "-qm", "initial")
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	previous := skipEagerCommit.Load()
	skipEagerCommit.Store(false)
	defer func() { skipEagerCommit.Store(previous) }()
	EagerCommit(context.Background(), EagerCommitOptions{
		Cwd: link, FilePath: filepath.Join(link, "file.txt"), Label: "edit",
	})
	subject := strings.TrimSpace(gitTestRun(t, dir, "log", "-1", "--pretty=%s"))
	if subject != "wip(edit): file.txt" {
		t.Fatalf("subject = %q, want the per-file commit through the link", subject)
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
