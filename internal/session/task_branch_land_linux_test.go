//go:build linux

package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// running is a process that exists and has not exited: a zombie waiting for
// its reaper ran to its end and counts as gone.
func running(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(stat), ')')
	return end < 0 || end+2 >= len(stat) || stat[end+2] != 'Z'
}

// A root question git never answers ends at the read's deadline with git and
// everything it started killed; nothing is left running behind a busy.
func TestASleepyRootQuestionIsKilledWithItsChildren(t *testing.T) {
	repo, place := landRepo(t)
	old := deliverGitCallMax
	deliverGitCallMax = 300 * time.Millisecond
	t.Cleanup(func() { deliverGitCallMax = old })
	pidFile := filepath.Join(t.TempDir(), "pid")
	real := landCommand
	t.Cleanup(func() { landCommand = real })
	landCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		for _, arg := range args {
			if arg == "--show-toplevel" {
				command := exec.CommandContext(ctx, "sh", "-c", "sleep 600 & echo $! > '"+pidFile+"'; wait")
				command.Dir = dir
				return command
			}
		}
		return real(ctx, dir, args...)
	}
	started := time.Now()
	if _, err := PlanTaskBranchLand(landCtx(t), place, repo, landBranch); !errors.Is(err, ErrLandBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the sleepy git never started its child: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for running(pid) {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the sleepy git's child %d outlived the read", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
