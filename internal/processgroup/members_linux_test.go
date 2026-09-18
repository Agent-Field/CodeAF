//go:build linux

package processgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A shell that has exited but not been reaped is a zombie: still in /proc,
// still holding its pid and its pgrp, still answering kill(-pgid, 0). Nothing
// of the job is running, so the group is not alive — the contract the detached
// sweep leans on to report a finished job at once rather than after the
// termination grace.
func TestAGroupWhoseOnlyMemberIsAZombieLeaderIsNotAlive(t *testing.T) {
	cmd := exec.Command("true")
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	group := CaptureGroup(cmd.Process.Pid)
	waitForState(t, cmd.Process.Pid, 'Z')
	if !Alive(cmd.Process.Pid) {
		t.Fatalf("the signal probe no longer answers for a zombie; this test's premise is gone")
	}
	if group.Alive() {
		t.Fatalf("a group whose only member is a zombie leader read as alive")
	}
	_ = cmd.Wait()
}

// A shell that detached a child and exited leaves a live member in its group
// while the leader is a zombie: the group is alive, its signals reach the
// child, and the group is dead once the child is.
func TestAGroupWithAZombieLeaderAndALiveChildIsAliveUntilTheChildIsGone(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("bash", "-c", "sleep 30 & echo $! > "+pidFile)
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	group := CaptureGroup(cmd.Process.Pid)
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	childPID := waitForPID(t, pidFile)
	waitForState(t, cmd.Process.Pid, 'Z')
	if !group.Alive() {
		t.Fatalf("a group with live detached child %d read as dead while its leader was a zombie", childPID)
	}
	if err := group.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && group.Alive() {
		time.Sleep(20 * time.Millisecond)
	}
	if group.Alive() {
		t.Fatalf("the group read as alive after its one live member %d was killed", childPID)
	}
}

// waitForState polls /proc until the process reads in the wanted state.
func waitForState(t *testing.T, pid int, want byte) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if state, _, ok := processStateAndGroup(readStat(pid)); ok && state == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _, _ := processStateAndGroup(readStat(pid))
	t.Fatalf("process %d never reached state %c (last read %c)", pid, want, state)
}

// waitForPID reads the pid a shell wrote to a file once the file has it.
func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if text := strings.TrimSpace(string(raw)); err == nil && text != "" {
			pid, err := strconv.Atoi(text)
			if err != nil {
				t.Fatalf("child pid %q: %v", text, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no pid was written to %s", path)
	return 0
}
