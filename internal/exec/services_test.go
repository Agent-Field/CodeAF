package exec

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStopServiceProcessEscalatesFromTermToKill uses a child that deliberately
// ignores SIGTERM: the grace must be honored, and the group must still die.
func TestStopServiceProcessEscalatesFromTermToKill(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "service.log")
	pid, startedAt, err := StartDetachedService(
		`trap '' TERM; echo ready; while true; do sleep 0.05; done`, t.TempDir(), logPath)
	if err != nil {
		t.Fatalf("start detached service: %v", err)
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	if startedAt.IsZero() {
		t.Fatal("detached service recorded no start time")
	}
	// The trap has to be installed before the stop, or the escalation this
	// test exists for never happens.
	ready := time.Now().Add(10 * time.Second)
	for {
		body, _ := os.ReadFile(logPath)
		if strings.Contains(string(body), "ready") {
			break
		}
		if time.Now().After(ready) {
			t.Fatal("detached service never signalled ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	begun := time.Now()
	if err := StopServiceProcess(pid); err != nil {
		t.Fatalf("stop service process: %v", err)
	}
	if elapsed := time.Since(begun); elapsed < serviceTerminateGrace {
		t.Fatalf("kill escalated after %s, before the %s grace", elapsed, serviceTerminateGrace)
	}
	deadline := time.Now().Add(2 * time.Second)
	for serviceLeaderAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(serviceStopPoll)
	}
	if serviceLeaderAlive(pid) {
		t.Fatal("process group survived TERM and KILL")
	}
	if err := StopServiceProcess(pid); err != nil {
		t.Fatalf("stopping an already dead group is not an error: %v", err)
	}
}

// TestProcessIdentityRefusesPIDReuse is the guard that keeps re-adoption from
// claiming whatever process happens to hold a recycled pid.
func TestProcessIdentityRefusesPIDReuse(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "service.log")
	pid, startedAt, err := StartDetachedService(`sleep 30`, t.TempDir(), logPath)
	if err != nil {
		t.Fatalf("start detached service: %v", err)
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)

	matched, err := ProcessIdentityMatches(pid, startedAt)
	if err != nil || !matched {
		t.Fatalf("live identity matched=%v err=%v", matched, err)
	}
	matched, err = ProcessIdentityMatches(pid, startedAt.Add(-time.Hour))
	if err != nil || matched {
		t.Fatalf("recycled identity matched=%v err=%v", matched, err)
	}
}
