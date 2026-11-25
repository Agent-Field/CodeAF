//go:build linux

package processgroup

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestCleanupDescendantsLeavesProcessesWhenTheRunKeepsThem(t *testing.T) {
	t.Setenv("SENIOR_DEV_KEEP_SHELL_PROCESSES", "1")
	service := exec.Command("sleep", "300")
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Process.Kill(); _ = service.Wait() })
	CleanupDescendants()
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(service.Process.Pid, 0); err != nil {
		t.Fatalf("descendant %d was killed although the run keeps its shell processes: %v", service.Process.Pid, err)
	}
	var status syscall.WaitStatus
	if pid, _ := syscall.Wait4(service.Process.Pid, &status, syscall.WNOHANG, nil); pid == service.Process.Pid {
		t.Fatalf("descendant %d exited (%v) although the run keeps its shell processes", pid, status)
	}
}
