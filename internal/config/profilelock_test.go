//go:build unix

package config

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProfileLockHelper(t *testing.T) {
	mode := os.Getenv("CODEAF_PROFILE_LOCK_HELPER")
	if mode == "" {
		return
	}
	dir := os.Getenv("CODEAF_PROFILE_LOCK_DIR")
	if mode == "crash" {
		file, err := os.OpenFile(BudgetConfigPath(dir)+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		mustWriteSignal(t, os.Getenv("CODEAF_PROFILE_LOCK_READY"))
		os.Exit(0)
	}
	mustWriteSignal(t, os.Getenv("CODEAF_PROFILE_LOCK_READY"))
	if err := writeProfileValue(dir, os.Getenv("CODEAF_PROFILE_LOCK_KEY"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentProcessesPreserveDistinctProfileKeys(t *testing.T) {
	dir := t.TempDir()
	barrier := holdProfileLock(t, dir)
	first := startProfileWriter(t, dir, "test.process.first", "one")
	second := startProfileWriter(t, dir, "test.process.second", "two")
	waitForSignal(t, first.ready)
	waitForSignal(t, second.ready)
	if err := barrier.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCommand(t, first.cmd)
	waitForCommand(t, second.cmd)

	data, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"test.process.first", "test.process.second"} {
		if _, ok := values[key]; !ok {
			t.Fatalf("concurrent process write lost %s: %s", key, data)
		}
	}
}

func TestProfileLockTimesOut(t *testing.T) {
	dir := t.TempDir()
	barrier := holdProfileLock(t, dir)
	defer barrier.Close()
	started := time.Now()
	err := writeProfileValue(dir, "test.timeout", true)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "timed out after 2s") {
		t.Fatalf("write error = %v, want 2s lock timeout", err)
	}
	if elapsed < profileLockWait || elapsed > profileLockWait+time.Second {
		t.Fatalf("timeout took %s, want %s with scheduling allowance", elapsed, profileLockWait)
	}
}

func TestProfileLockRecoversAfterHolderCrashes(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := helperCommand(dir, "", "crash", ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, ready)
	waitForCommand(t, cmd)
	if err := writeProfileValue(dir, "test.after-crash", true); err != nil {
		t.Fatalf("write after crashed holder: %v", err)
	}
}

type profileWriterProcess struct {
	cmd   *exec.Cmd
	ready string
}

func startProfileWriter(t *testing.T, dir, key, value string) profileWriterProcess {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := helperCommand(dir, key, value, ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return profileWriterProcess{cmd: cmd, ready: ready}
}

func helperCommand(dir, key, value, ready string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProfileLockHelper$")
	cmd.Env = append(os.Environ(),
		"CODEAF_PROFILE_LOCK_HELPER="+value,
		"CODEAF_PROFILE_LOCK_DIR="+dir,
		"CODEAF_PROFILE_LOCK_KEY="+key,
		"CODEAF_PROFILE_LOCK_READY="+ready,
	)
	return cmd
}

func holdProfileLock(t *testing.T, dir string) *os.File {
	t.Helper()
	path := BudgetConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return file
}

func mustWriteSignal(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForSignal(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for helper signal %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForCommand(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper failed: %v", err)
	}
}
