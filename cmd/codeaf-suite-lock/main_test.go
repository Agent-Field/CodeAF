//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestContentionNamesHolderAndDeathUnlocks(t *testing.T) {
	if os.Getenv("CODEAF_SUITE_LOCK_FIXTURE") != "" {
		os.Exit(run(os.Args[1], os.Args[2:]))
	}
	lock := filepath.Join(t.TempDir(), "suite.lock")
	holder := exec.Command(os.Args[0], lock, "sh", "-c", "echo ready; exec sleep 30")
	holder.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	ready, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Process.Kill(); _ = holder.Wait() }()
	one := make([]byte, 6)
	if _, err := ready.Read(one); err != nil || string(one) != "ready\n" {
		t.Fatalf("holder ready: %q, %v", one, err)
	}

	metadata, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(metadata))
	if len(fields) != 2 {
		t.Fatalf("metadata %q", metadata)
	}
	if fields[0] != strconv.Itoa(holder.Process.Pid) {
		t.Fatalf("metadata pid %q, holder %d", fields[0], holder.Process.Pid)
	}

	contender := exec.Command(os.Args[0], lock, "true")
	contender.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	output, err := contender.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("contender err %v, output %s", err, output)
	}
	want := "another heavy suite is already running on this box (pid " + fields[0] + ", started " + fields[1] + ")."
	if !strings.Contains(string(output), want) {
		t.Fatalf("output %q lacks %q", output, want)
	}

	if err := holder.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err == nil {
		t.Fatal("killed holder succeeded")
	}
	next := exec.Command(os.Args[0], lock, "true")
	next.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	if output, err := next.CombinedOutput(); err != nil {
		t.Fatalf("lock remained after death: %v: %s", err, output)
	}
}
