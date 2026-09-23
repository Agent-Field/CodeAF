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
}
