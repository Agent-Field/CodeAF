package cellstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

const failedArrivalOwnerEnv = "CODEAF_TEST_FAILED_ARRIVAL_OWNER_ROOT"

// TestFailedArrivalOwnerHelperProcess is the child, not a test: it plays the
// process whose files arrived badly. It begins an arrival for the cell, fails
// it with the reason it is told, and stays alive until its parent lets go of
// stdin, as the app does while the person reads the failure.
func TestFailedArrivalOwnerHelperProcess(t *testing.T) {
	root := os.Getenv(failedArrivalOwnerEnv)
	if root == "" {
		return
	}
	a := cell.BeginWorkspaceArrival(root, root, nil, nil)
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
		}
		fmt.Println("ok")
	}
	os.Exit(0)
}

// Item 10: while another process holds a FAILED arrival, a seal or a capture of
// the workspace refuses with the failure's reason, however long ago the failure
// was. A seal that went ahead would publish the partial (or empty) tree as the
// chat's files. A retry that succeeds lets the next seal through.
func TestW10SealAndCaptureRefuseWhileAnArrivalIsFailed(t *testing.T) {
	c := newCell(t)
	marker := cell.ArrivalMarkerPath(c.Root)
	if marker == "" {
		t.Fatalf("no arrival marker path for %s", c.Root)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestFailedArrivalOwnerHelperProcess$")
	cmd.Env = append(os.Environ(), failedArrivalOwnerEnv+"="+c.Root)
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
	watchdog := time.AfterFunc(60*time.Second, func() { _ = cmd.Process.Kill() })
	t.Cleanup(func() {
		watchdog.Stop()
		_ = in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	reader := bufio.NewReader(out)
	ok := func() {
		t.Helper()
		if line, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(line) != "ok" {
			t.Fatalf("arrival owner helper answered %q, %v; want ok", line, err)
		}
	}
	tell := func(line string) {
		t.Helper()
		if _, err := fmt.Fprintln(in, line); err != nil {
			t.Fatal(err)
		}
		ok()
	}
	ok()

	const reason = "sync could not be reached"
	tell("fail " + reason)
	// More than two minutes later by the fake clock: the old marker had aged out.
	longAgo := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(marker, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}

	// The refusal comes from the gate at the top of the call; no engine is
	// needed, and none may be reached. A panic here means a seal got past it.
	refused := func(name string, call func(context.Context) error) {
		t.Helper()
		var callErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					callErr = fmt.Errorf("%s went past the arrival gate and panicked: %v", name, r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			callErr = call(ctx)
		}()
		if callErr == nil || !strings.Contains(callErr.Error(), reason) {
			t.Fatalf("%s while the arrival is failed = %v; want a refusal carrying %q", name, callErr, reason)
		}
	}
	var e Engine
	refused("Seal", func(ctx context.Context) error { _, err := e.Seal(ctx, c, TurnInfo{}); return err })
	refused("Capture", func(ctx context.Context) error { _, err := e.Capture(ctx, c); return err })
	refused("Prepare", func(ctx context.Context) error { return e.Prepare(ctx, c) })

	// A retry that succeeds clears it: the gate lets the next caller through.
	tell("retry")
	tell("complete")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cell.WaitWorkspace(ctx, c.Root); err != nil {
		t.Fatalf("after the retry succeeded the gate must open: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a completed arrival leaves no marker; stat err = %v", err)
	}
}

// Item 10, fail closed: the process that was bringing the files fails, and then
// goes away (the person closed the window). The tree on disk is a partial one
// and nobody is left to retry, so a seal or a capture of the workspace in
// another process must STILL refuse, and say what to do. The old rule removed
// the dead owner's marker, and the next seal published the partial tree as the
// chat's files.
func TestW10bSealAndCaptureRefuseAfterTheOwnerOfAFailedArrivalDied(t *testing.T) {
	c := newCell(t)
	marker := cell.ArrivalMarkerPath(c.Root)
	if marker == "" {
		t.Fatalf("no arrival marker path for %s", c.Root)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFailedArrivalOwnerHelperProcess$")
	cmd.Env = append(os.Environ(), failedArrivalOwnerEnv+"="+c.Root)
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
	watchdog := time.AfterFunc(60*time.Second, func() { _ = cmd.Process.Kill() })
	t.Cleanup(func() { watchdog.Stop(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	reader := bufio.NewReader(out)
	ok := func() {
		t.Helper()
		if line, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(line) != "ok" {
			t.Fatalf("arrival owner helper answered %q, %v; want ok", line, err)
		}
	}
	ok()
	const reason = "sync could not be reached"
	if _, err := fmt.Fprintln(in, "fail "+reason); err != nil {
		t.Fatal(err)
	}
	ok()
	// The window closes: the owner is gone, and really reaped.
	_ = in.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	var e Engine
	for _, call := range []struct {
		name string
		do   func(context.Context) error
	}{
		{"Seal", func(ctx context.Context) error { _, err := e.Seal(ctx, c, TurnInfo{}); return err }},
		{"Capture", func(ctx context.Context) error { _, err := e.Capture(ctx, c); return err }},
		{"Prepare", func(ctx context.Context) error { return e.Prepare(ctx, c) }},
	} {
		var callErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					callErr = fmt.Errorf("%s went past the arrival gate and panicked: %v", call.name, r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			callErr = call.do(ctx)
		}()
		if !errors.Is(callErr, cell.ErrWorkspaceArrivalAbandoned) || !strings.Contains(callErr.Error(), reason) || !strings.Contains(callErr.Error(), "move the chat here again") {
			t.Fatalf("%s after the owner of a failed arrival died = %v; want a refusal that carries %q and says to move the chat here again", call.name, callErr, reason)
		}
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the failure must outlive its owner: %v", err)
	}
}
