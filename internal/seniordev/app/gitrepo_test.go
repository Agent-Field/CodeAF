//go:build !windows

package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitTestRepo is a one-commit repository with a pipeline pointed at it, for
// tests that exercise the tree helpers without a model or a runtime.
func gitTestRepo(t *testing.T) *pipeline {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return &pipeline{
		workspace: dir, events: newEventWriter(io.Discard), notes: io.Discard,
		recorder: newGitRecorder(dir, func(string) {}),
	}
}

// verificationWith is a completed verification with the given number of
// failing entrypoints plus one passing one.
func verificationWith(failing int) projectVerificationResult {
	commands := []any{}
	for i := 0; i < failing; i++ {
		commands = append(commands, map[string]any{"exit": float64(1)})
	}
	commands = append(commands, map[string]any{"exit": float64(0)})
	return projectVerificationResult{Commands: commands}
}

func writeWorkspace(t *testing.T, runner *pipeline, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(runner.workspace, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
