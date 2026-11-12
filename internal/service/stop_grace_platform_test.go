//go:build linux || darwin

package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// A real group that ignores TERM must survive the grace, then its leader and
// descendant must both disappear. Fake-runtime timing cannot prove OS teardown.
func TestATermIgnoringServiceAndItsChildStopAfterTheRealGrace(t *testing.T) {
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	work := t.TempDir()
	s := openReal(t, t.TempDir())
	defer s.Close()
	row, err := s.Keep("grace-owner", Spec{Command: "trap '' TERM; sleep 300 & echo $! > child.pid; wait", Dir: work, Health: Health{Cmd: "true"}}, PolicyNever, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	group := processgroup.CaptureGroup(row.PID)
	t.Cleanup(func() { _ = group.Kill() })
	child := readPID(t, filepath.Join(work, "child.pid"))
	t.Logf("owned leader=%d child=%d group=%d", row.PID, child, row.PID)
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*StopGrace+5*time.Second)
	defer cancel()
	if err := s.StopAll(ctx, "grace-test"); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed < StopGrace-100*time.Millisecond {
		t.Fatalf("TERM-ignoring group ended before grace: %s", elapsed)
	}
	waitFor(t, "the TERM-ignoring leader and child were reaped", func() bool { return gone(row.PID) && gone(child) })
	got, err := s.Get("grace-owner", row.ID)
	if err != nil || got.Running || got.Stopping || got.StoppedBy != "grace-test" {
		t.Fatalf("final row: %+v %v", got, err)
	}
	t.Logf("TERM then KILL completed in %s; both owned PIDs absent", elapsed)
}

// The original OS process really exits, so its Wait goroutine cannot help the
// next supervisor reap or identify the kept group.
func TestAServiceIsAdoptedAfterItsOriginalProcessQuits(t *testing.T) {
	const helperEnv = "CODEAF_SERVICE_QUIT_TEST_DIR"
	if dir := os.Getenv(helperEnv); dir != "" {
		s := openReal(t, filepath.Join(dir, "store"))
		row, err := s.Keep("quit-owner", Spec{Command: "sleep 300 & echo $! > child.pid; wait", Dir: dir, Health: Health{Cmd: "true"}}, PolicyNever, Provenance{})
		if err != nil {
			t.Fatal(err)
		}
		group := processgroup.CaptureGroup(row.PID)
		t.Cleanup(func() { _ = group.Kill() })
		_ = readPID(t, filepath.Join(dir, "child.pid"))
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "row.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		s.Close()
		os.Exit(0)
	}
	if processgroup.BootID() == "" {
		t.Skip("this machine names no boot")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	helper := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAServiceIsAdoptedAfterItsOriginalProcessQuits$")
	helper.Env = append(os.Environ(), helperEnv+"="+dir)
	if out, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("original process: %v %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "row.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before Row
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	group := processgroup.CaptureGroup(before.PID)
	t.Cleanup(func() { _ = group.Kill() })
	child := readPID(t, filepath.Join(dir, "child.pid"))
	if gone(before.PID) || gone(child) {
		t.Fatal("the original process quitting stopped its kept group")
	}
	s := openReal(t, filepath.Join(dir, "store"))
	defer s.Close()
	row, err := s.Get("quit-owner", before.ID)
	if err != nil || !row.Running || row.PID != before.PID || !row.ReAdopted {
		t.Fatalf("adopted row: %+v %v", row, err)
	}
	t.Logf("original process=%d exited; adopted leader=%d child=%d group=%d", helper.Process.Pid, row.PID, child, row.PID)
	if err := s.StopAll(ctx, "new-owner-quit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the re-adopted leader and child were reaped", func() bool { return gone(row.PID) && gone(child) })
}
