//go:build !windows

// K6 keeps Unix process, lock and mode assertions on the systems that provide them.
package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestTheHeartbeatStopsAfterItsParentIsKilled(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not on PATH")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	root := repositoryRoot(t)
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	cmd := exec.Command(bashPath, filepath.Join(root, "scripts/test-report.sh"), filepath.Join(t.TempDir(), "r.json"), sleepPath, "60")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TEST_REPORT_HEARTBEAT=1", "GOFLAGS=-buildvcs=false")
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		contents, readErr := os.ReadFile(stderrPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(contents, []byte("still running")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat did not report within 10 seconds; stderr = %q", contents)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	// COUNT THE HEARTBEATS AND NOT THE BYTES. The killed run's own command is still
	// in the pipeline and may yet say something on this stream; what is being asked
	// is whether the heartbeat loop is still speaking for a run that is gone.
	before := heartbeatsIn(t, stderrPath)
	time.Sleep(4 * time.Second)
	if after := heartbeatsIn(t, stderrPath); after != before {
		contents, _ := os.ReadFile(stderrPath)
		t.Fatalf("the heartbeat outlived its run: %d reports before the kill, %d four seconds after:\n%s", before, after, contents)
	}
}
