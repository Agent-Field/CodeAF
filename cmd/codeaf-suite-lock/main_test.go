//go:build linux || darwin

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestLockFollowsSuiteAfterWrapperKilled(t *testing.T) {
	if os.Getenv("CODEAF_SUITE_LOCK_FIXTURE") != "" {
		os.Exit(run(os.Args[1], os.Args[2:]))
	}
	lock := filepath.Join(t.TempDir(), "suite.lock")
	childInput, suiteInput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	suiteOutput, childOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	holder := exec.Command(os.Args[0], lock, "sh", "-c", "sleep 0.05; echo $$; read release")
	holder.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	holder.Stdin = childInput
	holder.Stdout = childOutput
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	_ = childInput.Close()
	_ = childOutput.Close()
	defer func() {
		_ = suiteInput.Close()
		_ = suiteOutput.Close()
		_ = holder.Process.Kill()
		_ = holder.Wait()
	}()

	line, err := bufio.NewReader(suiteOutput).ReadString('\n')
	if err != nil {
		t.Fatalf("read suite pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse suite pid %q: %v", line, err)
	}
	metadata, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(metadata))
	if len(fields) != 2 {
		t.Fatalf("metadata %q", metadata)
	}
	if fields[0] != strconv.Itoa(childPID) {
		t.Fatalf("metadata pid %q, suite %d, wrapper %d", fields[0], childPID, holder.Process.Pid)
	}

	if err := holder.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err == nil {
		t.Fatal("killed wrapper succeeded")
	}
	child, err := os.FindProcess(childPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("suite pid %d is not live after wrapper death: %v", childPID, err)
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

	if _, err := suiteInput.Write([]byte("release\n")); err != nil {
		t.Fatal(err)
	}
	if err := suiteInput.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(suiteOutput); err != nil {
		t.Fatal(err)
	}
	next := exec.Command(os.Args[0], lock, "true")
	next.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	if output, err := next.CombinedOutput(); err != nil {
		t.Fatalf("lock remained after suite exit: %v: %s", err, output)
	}
}

// TestRefusalNamesADeadHolderWhenAGrandchildKeepsTheLock covers the cost of the
// inherited descriptor: the suite can leak it into a process that outlives the
// recorded holder, so the lock file names a dead pid while the lock is still
// held. The refusal must say the recorded holder is gone and how to find the
// real one, rather than send the next suite to wait on nothing.
func TestRefusalNamesADeadHolderWhenAGrandchildKeepsTheLock(t *testing.T) {
	if os.Getenv("CODEAF_SUITE_LOCK_FIXTURE") != "" {
		os.Exit(run(os.Args[1], os.Args[2:]))
	}
	lock := filepath.Join(t.TempDir(), "suite.lock")
	suiteOutput, childOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// The suite prints its own pid, backgrounds a grandchild that inherits the
	// locked descriptor, and exits, so the recorded pid dies while the lock
	// stays held by the leaked grandchild.
	holder := exec.Command(os.Args[0], lock, "sh", "-c", "sleep 300 & echo $$; echo gc $!")
	holder.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	holder.Stdout = childOutput
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	_ = childOutput.Close()

	reader := bufio.NewReader(suiteOutput)
	pidLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read suite pid: %v", err)
	}
	gcLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	suitePID, err := strconv.Atoi(strings.TrimSpace(pidLine))
	if err != nil {
		t.Fatalf("parse suite pid %q: %v", pidLine, err)
	}
	gcPID, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(gcLine), "gc ")))
	if err != nil {
		t.Fatalf("parse grandchild pid %q: %v", gcLine, err)
	}
	defer func() {
		if gc, findErr := os.FindProcess(gcPID); findErr == nil {
			_ = gc.Signal(syscall.SIGKILL)
		}
		_ = suiteOutput.Close()
		_ = holder.Process.Kill()
		_ = holder.Wait()
	}()

	// The wrapper exits once its suite child exits; wait for that so the
	// recorded pid is really gone before the contender reads it.
	if err := holder.Wait(); err != nil {
		t.Fatalf("wrapper wait: %v", err)
	}
	if err := (&os.Process{Pid: suitePID}).Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("suite pid %d still live; the test needs it dead", suitePID)
	}
	if err := (&os.Process{Pid: gcPID}).Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("grandchild %d is not live: %v", gcPID, err)
	}

	metadata, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(metadata))
	if len(fields) == 0 || fields[0] != strconv.Itoa(suitePID) {
		t.Fatalf("metadata %q does not record suite pid %d", metadata, suitePID)
	}

	contender := exec.Command(os.Args[0], lock, "true")
	contender.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	output, err := contender.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("contender err %v, output %s", err, output)
	}
	if !strings.Contains(string(output), "pid "+fields[0]+" is gone") {
		t.Fatalf("output %q lacks the dead-holder line for pid %s", output, fields[0])
	}
	if !strings.Contains(string(output), "lsof "+lock) {
		t.Fatalf("output %q lacks the lsof hint", output)
	}
}
