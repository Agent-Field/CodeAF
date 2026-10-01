//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A WRITE OR AN EDIT COMMITS NOTHING. Until 2026-09-30 each one was committed
// on the run's branch as it happened (`wip(write): <path>`,
// `wip(edit): <path>`, under senior-dev's own name), and a pull request cut
// from such a branch carried fifty of them with senior-dev as an author. The
// workspace here is shaped the way codeaf hands one over — a repository on a
// `task/` branch, with the variable that used to fence those commits to it —
// so the test fails if anything brings the checkpoint back.
func TestAWriteAndAnEditLeaveTheRunsBranchWhereItWas(t *testing.T) {
	workDir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		command.Dir = workDir
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	writeTestFile(t, workDir, "kept.txt", "one\n")
	git("add", "kept.txt")
	git("commit", "-q", "-m", "base")
	git("switch", "-q", "-c", "task/run")
	t.Setenv("SENIOR_DEV_EXPECTED_BRANCH", "task/run")
	base := git("rev-parse", "HEAD")

	registry := New(workDir)
	if _, err := execute(t, registry, "write", map[string]any{"filePath": "added.txt", "content": "new\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := execute(t, registry, "edit", map[string]any{"filePath": "kept.txt", "oldString": "one", "newString": "two"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if head := git("rev-parse", "HEAD"); head != base {
		t.Fatalf("HEAD moved from %s to %s: %s", base, head, git("log", "--format=%an %s", base+"..HEAD"))
	}
	if staged := git("diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("the tools staged %q", staged)
	}
	status := git("status", "--porcelain")
	for _, want := range []string{"M kept.txt", "?? added.txt"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status = %q, want %q in it", status, want)
		}
	}
	if data, err := os.ReadFile(filepath.Join(workDir, "kept.txt")); err != nil || string(data) != "two\n" {
		t.Fatalf("kept.txt = %q, %v", data, err)
	}
}
