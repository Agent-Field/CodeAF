//go:build linux || darwin

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// TestMain answers for every role these tests run this binary in: the wrapper,
// a contender, and the lock holder the wrapper starts beside a suite. It has to
// happen before testing parses flags, because the holder is invoked with one
// this binary understands and the test flag set does not.
func TestMain(m *testing.M) {
	if os.Getenv("CODEAF_SUITE_LOCK_FIXTURE") != "" {
		os.Exit(dispatch(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestLockFollowsSuiteAfterWrapperKilled(t *testing.T) {
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
	if len(fields) != 3 {
		t.Fatalf("metadata %q", metadata)
	}
	if fields[0] != strconv.Itoa(childPID) {
		t.Fatalf("metadata pid %q, suite %d, wrapper %d", fields[0], childPID, holder.Process.Pid)
	}
	// The third field names the process that actually holds the lock, so a free
	// lock under a running suite can still be read back to whoever dropped it.
	if !strings.HasPrefix(fields[2], "holder=") || fields[2] == "holder=0" {
		t.Fatalf("metadata names no holder: %q", metadata)
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
	// The lock is released by the holder beside the suite, which notices within
	// its poll rather than the instant the suite exits, so this waits a bounded
	// time instead of asking once.
	var said []byte
	var refused error
	for attempt := 0; attempt < 40; attempt++ {
		next := exec.Command(os.Args[0], lock, "true")
		next.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
		if said, refused = next.CombinedOutput(); refused == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("lock remained two seconds after suite exit: %v: %s", refused, said)
}

// A REFUSAL NAMES A PID IT CANNOT SEE WITHOUT CALLING IT GONE.
//
// The lock file's pid is meaningful only in the holder's own pid namespace, so
// a contender that cannot see it has learned nothing about whether a suite is
// running: it may be live in another namespace. (The other reading, a leaked
// descriptor outliving the recorded pid, is what the holder removes.) The
// refusal must give both readings and name the file-level way to find the real
// holder, rather than send anyone to end a healthy suite.
func TestRefusalNamesAPidItCannotSeeWithoutCallingItGone(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "suite.lock")
	held, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := filelock.Lock(held, true, true); err != nil {
		t.Fatal(err)
	}

	// A pid this namespace cannot see: one that has already exited and been
	// reaped, which is exactly what a pid from another namespace looks like
	// from here.
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	unseen := gone.Process.Pid
	if err := syscall.Kill(unseen, 0); err == nil {
		t.Skipf("pid %d is still visible, so it cannot stand in for an unseen holder", unseen)
	}
	if _, err := fmt.Fprintf(held, "%d 2026-09-19T22:00:00Z holder=%d\n", unseen, unseen); err != nil {
		t.Fatal(err)
	}

	contender := exec.Command(os.Args[0], lock, "true")
	contender.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	output, err := contender.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("contender err %v, output %s", err, output)
	}
	if !strings.Contains(string(output), fmt.Sprintf("pid %d is not visible from here", unseen)) {
		t.Fatalf("output %q lacks the not-visible line for pid %d", output, unseen)
	}
	if strings.Contains(string(output), "gone") {
		t.Fatalf("output %q calls a holder it cannot see gone", output)
	}
	if !strings.Contains(string(output), "lsof "+lock) {
		t.Fatalf("output %q lacks the lsof hint", output)
	}
}

// A SUITE'S LEFTOVERS MUST NOT HOLD THE BOX.
//
// The lock used to be handed to the suite, and an advisory lock lives on the
// open file description, so every process the suite left behind kept it alive:
// on 2026-09-19 one orphaned test shell held this lock for eight minutes after
// a green run. The suite is given no descriptor now, so when the suite exits
// the next acquirer succeeds even while its leftovers are still running.
func TestTheLockIsFreeWhenTheSuiteEndsThoughItLeftAProcessBehind(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "suite.lock")
	suiteOutput, childOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// The suite starts a process that outlives it and then exits, which is what
	// a test leaving a background shell behind does to a real suite.
	wrapper := exec.Command(os.Args[0], lock, "sh", "-c", "sleep 300 & echo $!")
	wrapper.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	wrapper.Stdout = childOutput
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	_ = childOutput.Close()
	defer func() { _ = suiteOutput.Close() }()

	line, err := bufio.NewReader(suiteOutput).ReadString('\n')
	if err != nil {
		t.Fatalf("read leftover pid: %v", err)
	}
	leftover, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse leftover pid %q: %v", line, err)
	}
	defer func() {
		if process, err := os.FindProcess(leftover); err == nil {
			_ = process.Signal(syscall.SIGKILL)
		}
	}()
	if err := wrapper.Wait(); err != nil {
		t.Fatalf("wrapper: %v", err)
	}
	if err := syscall.Kill(leftover, 0); err != nil {
		t.Skipf("the leftover pid %d did not outlive its suite: %v", leftover, err)
	}

	contender := exec.Command(os.Args[0], lock, "true")
	contender.Env = append(os.Environ(), "CODEAF_SUITE_LOCK_FIXTURE=1")
	output, err := contender.CombinedOutput()
	if err != nil {
		t.Fatalf("a suite's leftover still holds the lock: %v\n%s", err, output)
	}
}
