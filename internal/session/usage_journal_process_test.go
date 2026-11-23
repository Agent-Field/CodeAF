//go:build unix

package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestUsageJournalConcurrentProcessesAndCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	var children []*exec.Cmd
	for _, role := range []string{"writer-a", "writer-b", "compact"} {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageJournalProcessHelper$", "--", "usage-process", path, role)
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
	}
	deadline := time.Now().Add(10 * time.Second)
	for _, role := range []string{"writer-a", "writer-b", "compact"} {
		for {
			if _, err := os.Stat(path + "." + role); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("helper did not become ready: " + role)
			}
			time.Sleep(time.Millisecond)
		}
	}
	if err := os.WriteFile(path+".start", nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range children {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	RecoverUsage(path)
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 200 {
		t.Fatalf("rows=%d err=%v, want 200", len(rows), err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		id := row.ChargeID()
		if seen[id] {
			t.Fatal("duplicate charge: " + id)
		}
		seen[id] = true
	}
	for _, role := range []string{"writer-a", "writer-b"} {
		for i := range 100 {
			if !seen[role+"-"+strconv.Itoa(i)] {
				t.Fatal("lost charge")
			}
		}
	}
	raw, err := os.ReadFile(path + usagePendingSuffix)
	if err != nil || len(raw) != 0 {
		t.Fatalf("pending bytes=%d err=%v", len(raw), err)
	}
}

func TestUsageJournalSIGKILLAfterReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageJournalProcessHelper$", "--", "usage-process", path, "kill")
	output, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("child was not killed: %v %s", err, output)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("exit=%v %s", exit, output)
	}
	// The projection and background sync are blocked in the child. Only write()
	// can have preserved this row for a fresh process to replay.
	for range 2 {
		restart := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageJournalProcessHelper$", "--", "usage-process", path, "restart")
		if output, err := restart.CombinedOutput(); err != nil {
			t.Fatalf("restart: %v %s", err, output)
		}
	}
}

func TestUsageJournalProcessHelper(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-3] != "usage-process" {
		t.Skip("subprocess helper")
	}
	path, role := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if role == "restart" {
		rows, err := ReadUsage(path, time.Time{})
		if err != nil || len(rows) != 1 || rows[0].ChargeID() != "killed-charge" || rows[0].USD != .0090004 {
			t.Fatalf("replay=%+v %v", rows, err)
		}
		raw, err := os.ReadFile(path + usagePendingSuffix)
		if err != nil || len(raw) != 0 {
			t.Fatalf("pending=%d %v", len(raw), err)
		}
		return
	}
	if role == "kill" {
		pendingUsage(path).syncFile = func(*os.File) error { select {} }
		usageWriterFor(path).beforeWrite = func() { select {} }
		RecordUsage(path, UsageLine{ID: "killed-charge", Input: 1, USD: .0090004})
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	if err := os.WriteFile(path+"."+role, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path + ".start"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("start deadline")
		}
		time.Sleep(time.Millisecond)
	}
	if role == "compact" {
		for range 100 {
			RecoverUsage(path)
			time.Sleep(time.Millisecond)
		}
		return
	}
	for i := range 100 {
		RecordUsage(path, UsageLine{ID: fmt.Sprintf("%s-%d", role, i), Input: 1, USD: .0090004})
		if i%5 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if !StopUsageWriter(path) {
		t.Fatal("writer did not drain")
	}
}
