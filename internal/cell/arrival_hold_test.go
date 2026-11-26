package cell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// arrivalOwnerEnv names the cell root a helper process begins an arrival for.
// The helper stands in for "the process that is putting the files in place":
// every waiter in these tests is a DIFFERENT process from it, which is the
// whole point of the cross-process gate (items 9 and 10).
const arrivalOwnerEnv = "CODEAF_TEST_ARRIVAL_OWNER_ROOT"

// TestArrivalOwnerHelperProcess is the child, not a test: unless the parent set
// the variable it returns at once. The child begins an arrival, then does what
// its parent tells it on stdin, one command per line, answering "ok" to each:
// "fail <reason>", "retry", "complete", or "exit" (which leaves WITHOUT
// completing, as a quit or a crash would).
func TestArrivalOwnerHelperProcess(t *testing.T) {
	root := os.Getenv(arrivalOwnerEnv)
	if root == "" {
		return
	}
	a := BeginWorkspaceArrival(root, filepath.Join(root, "workspace"), nil, nil)
	fmt.Println("ok")
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		verb, arg, _ := strings.Cut(scan.Text(), " ")
		switch verb {
		case "fail":
			a.Fail(errors.New(arg))
		case "retry":
			a.BeginRetry()
		case "complete":
			a.Complete()
		case "exit":
			os.Exit(0)
		}
		fmt.Println("ok")
	}
	os.Exit(0)
}

type arrivalOwner struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// startArrivalOwner runs the helper and returns once its arrival is begun.
func startArrivalOwner(t *testing.T, cellRoot string) *arrivalOwner {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestArrivalOwnerHelperProcess$")
	cmd.Env = append(os.Environ(), arrivalOwnerEnv+"="+cellRoot)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	o := &arrivalOwner{cmd: cmd, in: in, out: bufio.NewReader(out)}
	// A hung child must not hang the suite: killing it ends every read below.
	watchdog := time.AfterFunc(30*time.Second, func() { _ = cmd.Process.Kill() })
	t.Cleanup(func() {
		watchdog.Stop()
		_ = in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	o.expectOK(t)
	return o
}

func (o *arrivalOwner) expectOK(t *testing.T) {
	t.Helper()
	line, err := o.out.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ok" {
		t.Fatalf("arrival owner helper answered %q, %v; want ok", line, err)
	}
}

func (o *arrivalOwner) say(t *testing.T, line string) {
	t.Helper()
	if _, err := fmt.Fprintln(o.in, line); err != nil {
		t.Fatalf("tell arrival owner %q: %v", line, err)
	}
	o.expectOK(t)
}

// leave makes the owner exit without completing and reaps it, so its pid is
// really gone before the caller looks.
func (o *arrivalOwner) leave(t *testing.T) {
	t.Helper()
	_, _ = fmt.Fprintln(o.in, "exit")
	_ = o.cmd.Wait()
}

// arrivalCell lays out a cell the way the engine does, so the marker lives in
// the per-cell store beside it and never inside the tree.
func arrivalCell(t *testing.T, id string) (cellRoot, workDir, marker string) {
	t.Helper()
	cellRoot = filepath.Join(t.TempDir(), "v3", "cells", id)
	workDir = filepath.Join(cellRoot, "workspace")
	for _, d := range []string{filepath.Join(cellRoot, StateDir), workDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker = ArrivalMarkerPath(cellRoot)
	if marker == "" {
		t.Fatal("expected a non-empty ArrivalMarkerPath")
	}
	return cellRoot, workDir, marker
}

func setArrivalBound(t *testing.T, d time.Duration) {
	t.Helper()
	prev := DefaultArrivalStallBound
	DefaultArrivalStallBound = d
	t.Cleanup(func() { DefaultArrivalStallBound = prev })
}

// backdate is the fake clock: it makes the marker look as though its owner last
// wrote to it ago ago, without anyone sleeping through it.
func backdate(t *testing.T, path string, ago time.Duration) {
	t.Helper()
	when := time.Now().Add(-ago)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func waitWithin(t *testing.T, limit time.Duration, paths ...string) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	start := time.Now()
	err := WaitWorkspace(ctx, paths...)
	return time.Since(start), err
}

// Item 9: a process that is still running must keep the gate closed, on every
// platform. On Windows the old probe (signal 0) is unsupported, read as "dead",
// and deleted the live owner's marker, so another process ran tools on a tree
// that was still arriving.
func TestW9LiveOwnerInAnotherProcessKeepsTheGate(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w9-live")
	owner := startArrivalOwner(t, cellRoot)

	if !WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("a live owner in another process: WorkspaceArriving = false, want true")
	}
	_, err := waitWithin(t, 300*time.Millisecond, cellRoot, workDir)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitWorkspace with a live owner returned %v; want it to keep waiting (deadline exceeded)", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("a live owner's marker must stay: %v", statErr)
	}

	// Once the owner is really gone the very same waiter lets go at once.
	owner.leave(t)
	took, err := waitWithin(t, 5*time.Second, cellRoot, workDir)
	if err != nil || took > time.Second {
		t.Fatalf("WaitWorkspace after the owner left = %v after %s; want nil at once", err, took)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a dead owner's marker must be cleaned up; stat err = %v", statErr)
	}
}

// Item 9: when a platform cannot tell whether a process is running, the owner
// is taken to be ALIVE and the stall bound decides. Only "there is no such
// process" is proof of a death.
func TestW9UnknownLivenessIsAliveNeverDead(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		alive bool
	}{
		{"signal delivered", nil, true},
		{"permission denied is alive", os.ErrPermission, true},
		{"EPERM is alive", syscall.EPERM, true},
		// What Process.Signal(0) answers on Windows: the platform cannot say.
		{"not supported by windows cannot say", errors.New("not supported by windows"), true},
		{"anything unexpected cannot say", errors.New("probe failed in some new way"), true},
		{"process done is dead", os.ErrProcessDone, false},
		{"ESRCH is dead", syscall.ESRCH, false},
	}
	for _, c := range cases {
		if got := signalProbeAlive(c.err); got != c.alive {
			t.Errorf("%s: signalProbeAlive(%v) = %v, want %v", c.name, c.err, got, c.alive)
		}
	}
}

// Item 10: after a failure no process may proceed, however long ago it was, and
// the person is told why. The old marker aged out on its two-minute bound, after
// which a seal in another process saw "ready" and could publish a partial or
// empty tree as the chat's files.
func TestW10FailedArrivalKeepsRefusingEveryProcessPastTheBound(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-refuse")
	const reason = "sync could not be reached"
	owner := startArrivalOwner(t, cellRoot)
	owner.say(t, "fail "+reason)

	// A fresh failure: the waiter may give a retry its chance, but what it ends
	// with is the failure and its reason, never success.
	setArrivalBound(t, 300*time.Millisecond)
	took, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
	if err == nil || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), "could not arrive") {
		t.Fatalf("waiter after a fresh failure = %v (after %s); want the failure with its reason %q", err, took, reason)
	}

	// Well past the bound by the wall clock: still refusing, with the reason.
	time.Sleep(400 * time.Millisecond)
	for i := 0; i < 2; i++ {
		_, err = waitWithin(t, 10*time.Second, cellRoot, workDir)
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("waiter %d after the bound = %v; want the failure with its reason, not success", i+1, err)
		}
	}
	if !WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("WorkspaceArriving = false for a failed arrival; the files have not arrived")
	}

	// The fake clock: the default two minutes, and the failure ten minutes old.
	setArrivalBound(t, 2*time.Minute)
	backdate(t, marker, 10*time.Minute)
	took, err = waitWithin(t, 10*time.Second, cellRoot, workDir)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("waiter ten minutes after the failure = %v; want the failure with its reason", err)
	}
	if took > 2*time.Second {
		t.Fatalf("a failure older than the bound took %s to be reported; want it at once", took)
	}
	if !WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("WorkspaceArriving = false ten minutes after the failure; want true")
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("the failed arrival's marker must stay while its owner lives: %v", statErr)
	}
}

// Item 10: a waiter already waiting when the arrival fails gets the failure too.
func TestW10WaiterAlreadyWaitingLearnsOfTheFailure(t *testing.T) {
	cellRoot, workDir, _ := arrivalCell(t, "cell-w10-midwait")
	const reason = "the relay stopped answering"
	owner := startArrivalOwner(t, cellRoot)
	setArrivalBound(t, 250*time.Millisecond)

	result := make(chan error, 1)
	go func() {
		_, err := waitWithin(t, 20*time.Second, cellRoot, workDir)
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	owner.say(t, "fail "+reason)
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("a waiter that was waiting when it failed got %v; want the failure with its reason", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a waiter that was waiting when the arrival failed never heard of it")
	}
}

// Item 10: a retry that succeeds clears the failure, and a retry that fails
// again records the NEW reason. Until it succeeds the gate stays closed.
func TestW10ARetryThatSucceedsClearsTheFailure(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-retry")
	owner := startArrivalOwner(t, cellRoot)
	owner.say(t, "fail first reason")
	backdate(t, marker, 10*time.Minute)

	if _, err := waitWithin(t, 10*time.Second, cellRoot, workDir); err == nil || !strings.Contains(err.Error(), "first reason") {
		t.Fatalf("before the retry: %v; want the failure with its reason", err)
	}

	// The retry begins: the old failure is gone, and the tree is arriving again,
	// so the waiter holds rather than succeeding or reporting the old reason.
	owner.say(t, "retry")
	_, err := waitWithin(t, 300*time.Millisecond, cellRoot, workDir)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("during the retry: %v; want the waiter to keep holding (deadline exceeded)", err)
	}

	// The retry fails again: the new reason replaces the old one.
	owner.say(t, "fail second reason")
	backdate(t, marker, 10*time.Minute)
	if _, err := waitWithin(t, 10*time.Second, cellRoot, workDir); err == nil || !strings.Contains(err.Error(), "second reason") || strings.Contains(err.Error(), "first reason") {
		t.Fatalf("after the retry failed again: %v; want the second reason only", err)
	}

	// And a retry that succeeds lets everyone through and leaves nothing behind.
	owner.say(t, "retry")
	owner.say(t, "complete")
	took, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
	if err != nil || took > time.Second {
		t.Fatalf("after the retry succeeded: %v after %s; want nil at once", err, took)
	}
	if WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("WorkspaceArriving = true after the retry succeeded")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a completed arrival leaves no marker; stat err = %v", statErr)
	}
}

// Item 10, fail closed: a FAILED arrival whose owner is gone (the person closed
// the window, or the process crashed) is not "ready". The files did not arrive,
// the tree on disk is a partial one, and no retry is coming, so every waiter
// keeps being refused, however long ago it was, with a reason that says what to
// do. The old rule removed the dead owner's marker, and a seal in another
// process then published the partial tree as the chat's files.
func TestW10bFailedArrivalWhoseOwnerDiedKeepsRefusingEveryProcess(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-abandoned")
	const reason = "the files could not be fetched"
	owner := startArrivalOwner(t, cellRoot)
	owner.say(t, "fail "+reason)
	owner.leave(t)

	check := func(when string) {
		t.Helper()
		took, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
		if err == nil {
			t.Fatalf("%s: waiter on a failed arrival whose owner is gone = nil; want a refusal, the tree is a partial one", when)
		}
		if !errors.Is(err, ErrWorkspaceArrivalAbandoned) || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), "move the chat here again") {
			t.Fatalf("%s: refusal = %v; want it to carry the reason %q and say to move the chat here again", when, err, reason)
		}
		if took > 2*time.Second {
			t.Fatalf("%s: the refusal took %s; with no owner left there is nothing to wait for", when, took)
		}
		if !WorkspaceArriving(cellRoot, workDir) {
			t.Fatalf("%s: WorkspaceArriving = false for files that did not arrive", when)
		}
		if _, statErr := os.Stat(marker); statErr != nil {
			t.Fatalf("%s: the failure must outlive its owner: %v", when, statErr)
		}
	}
	check("just after the owner left")
	check("asked again")
	backdate(t, marker, 10*time.Minute)
	check("ten minutes later by the fake clock")
}

// Item 10, the restart: the next process that opens or takes the chat begins a
// new arrival, which replaces the abandoned failure; while that one runs the
// waiters wait on it, and when its files land the marker is gone for good.
func TestW10bANewArrivalReplacesTheAbandonedFailureAndClearsIt(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-restart")
	first := startArrivalOwner(t, cellRoot)
	first.say(t, "fail the first fetch failed")
	first.leave(t)

	// Before the restart the failure is refused.
	if _, err := waitWithin(t, 10*time.Second, cellRoot, workDir); !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("before the restart: %v; want the abandoned failure", err)
	}

	// The restart is a new arrival over the same cell, in a new process.
	second := startArrivalOwner(t, cellRoot)
	_, err := waitWithin(t, 300*time.Millisecond, cellRoot, workDir)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("while the new arrival runs: %v; want waiters to hold on it (deadline exceeded), not the old failure", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("the new arrival's marker: %v", statErr)
	}

	// Its files land: nothing is left to refuse, in any process.
	second.say(t, "complete")
	took, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
	if err != nil || took > time.Second {
		t.Fatalf("after the files landed: %v after %s; want nil at once", err, took)
	}
	if WorkspaceArriving(cellRoot, workDir) {
		t.Fatal("WorkspaceArriving = true after the files landed")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a completed arrival leaves no marker; stat err = %v", statErr)
	}
}

// The crashed-owner rule for a RUNNING arrival stands: the dead owner's marker
// is removed at once, so nothing stalls on it for the whole bound.
func TestW10bRunningArrivalOfADeadOwnerIsStillCleanedUp(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-crashed-running")
	owner := startArrivalOwner(t, cellRoot)
	owner.leave(t)
	took, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
	if err != nil || took > time.Second {
		t.Fatalf("waiter on a running arrival whose owner is dead = %v after %s; want nil at once", err, took)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a dead owner's running marker must be removed; stat err = %v", statErr)
	}
}

// Item 10, same process: the failure outlives the bound here too.
func TestW10FailedArrivalInThisProcessRefusesPastTheBound(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-inproc")
	const reason = "no space left on the device"
	a := BeginWorkspaceArrival(cellRoot, workDir, func() time.Duration { return 60 * time.Millisecond }, nil)
	defer a.Complete()
	a.Fail(errors.New(reason))
	time.Sleep(150 * time.Millisecond)

	for _, wait := range []func() error{
		func() error { _, err := waitWithin(t, 5*time.Second, cellRoot, workDir); return err },
		func() error { return a.Wait(context.Background()) },
	} {
		if err := wait(); err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("a failed arrival, past the bound, in this process = %v; want the failure with its reason", err)
		}
	}
	// The same failure is on disk for the other processes, with its reason.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the failed arrival keeps its marker for the other processes: %v", err)
	}
	a.Complete()
	if err := WaitWorkspace(context.Background(), cellRoot, workDir); err != nil {
		t.Fatalf("after Complete: %v", err)
	}
}

// Item 10, the abort: a take begins a replacement arrival over an abandoned
// failure and then cannot go on. Completing it would declare the files here; the
// failure must be given back instead, refusing every waiter in this process and
// every other, until a new arrival replaces it and lands.
func TestW10bAReplacementArrivalThatIsAbandonedKeepsTheFailure(t *testing.T) {
	cellRoot, workDir, marker := arrivalCell(t, "cell-w10-abort")
	first := startArrivalOwner(t, cellRoot)
	first.say(t, "fail the first fetch failed")
	first.leave(t)
	if _, err := waitWithin(t, 10*time.Second, cellRoot, workDir); !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("before the restart: %v; want the abandoned failure", err)
	}

	// The restart begins in this process, so its waiters wait on it.
	a := BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
	_, err := waitWithin(t, 200*time.Millisecond, cellRoot, workDir)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("while the replacement runs: %v; want waiters to hold on it", err)
	}

	// It could not go on. The failure goes back, as it was.
	a.Abandon("the first fetch failed")
	for i := 0; i < 2; i++ {
		_, err := waitWithin(t, 10*time.Second, cellRoot, workDir)
		reason, ok := AbandonedReason(err)
		if !errors.Is(err, ErrWorkspaceArrivalAbandoned) || !ok || reason != "the first fetch failed" {
			t.Fatalf("after the replacement was abandoned (%d) = %v; want the abandoned failure with its reason", i+1, err)
		}
	}
	// Completing an abandoned arrival does not make the files arrive.
	a.Complete()
	if _, err := waitWithin(t, 10*time.Second, cellRoot, workDir); !errors.Is(err, ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("after Complete on an abandoned arrival = %v; want the failure to stay", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("the failure must stay on disk: %v", statErr)
	}

	// A new arrival over the same cell replaces it, and its files landing clears it.
	b := BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
	if _, err := waitWithin(t, 200*time.Millisecond, cellRoot, workDir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("while the next arrival runs: %v; want waiters to hold on it", err)
	}
	b.Complete()
	if err := WaitWorkspace(context.Background(), cellRoot, workDir); err != nil {
		t.Fatalf("after the files landed: %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a completed arrival leaves no marker; stat err = %v", statErr)
	}
}
