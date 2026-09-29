package executor

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"
)

func sh(script string) ExecRequest {
	return ExecRequest{Argv: []string{"sh", "-c", script}}
}

func exec1(t *testing.T, req ExecRequest, on func(Chunk)) ExecResult {
	t.Helper()
	res, err := Local{Root: t.TempDir()}.Exec(context.Background(), req, on)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return res
}

func TestLocalRunsAndCapturesOutput(t *testing.T) {
	var streamed []Chunk
	res := exec1(t, sh("echo out; echo err >&2"), func(c Chunk) { streamed = append(streamed, c) })
	if res.Exit != 0 || string(res.Stdout) != "out\n" || string(res.Stderr) != "err\n" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(streamed) != 2 {
		t.Fatalf("want 2 streamed chunks, got %d", len(streamed))
	}
}

func TestLocalReportsExitCode(t *testing.T) {
	if res := exec1(t, sh("exit 7"), nil); res.Exit != 7 || res.TimedOut {
		t.Fatalf("want exit 7, got %+v", res)
	}
}

func TestLocalTimesOutAndKillsTheGroup(t *testing.T) {
	req := sh("sleep 30 & sleep 30")
	req.Timeout = 150 * time.Millisecond
	res := exec1(t, req, nil)
	if !res.TimedOut || res.Exit == 0 || len(res.Services) != 0 {
		t.Fatalf("want a timed-out kill with no services, got %+v", res)
	}
	if res.Wall > 5*time.Second {
		t.Fatalf("timeout was not prompt: %v", res.Wall)
	}
}

func TestLocalRecordsALeftRunningChildAsAService(t *testing.T) {
	res := exec1(t, sh("sleep 30 >/dev/null 2>&1 &"), nil)
	if len(res.Services) != 1 {
		t.Fatalf("want one service, got %+v", res.Services)
	}
	pgid := res.Services[0].PGID
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	if res.Wall > 5*time.Second {
		t.Fatalf("a background child held the call for %v", res.Wall)
	}
}

func TestLocalCleanExitLeavesNoServices(t *testing.T) {
	if res := exec1(t, sh("true"), nil); len(res.Services) != 0 {
		t.Fatalf("want no services, got %+v", res.Services)
	}
}

func TestLocalRunsInTheRelativeDirectory(t *testing.T) {
	root := t.TempDir()
	req := sh("pwd")
	req.Dir = "."
	res, err := Local{Root: root}.Exec(context.Background(), req, nil)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(res.Stdout)), root[strings.LastIndex(root, "/"):]) {
		t.Fatalf("pwd = %q, err %v", res.Stdout, err)
	}
}

func TestLocalRefusesBadRequests(t *testing.T) {
	bad := map[string]ExecRequest{
		"escape":     {Argv: []string{"true"}, Dir: "../x"},
		"absolute":   {Argv: []string{"true"}, Dir: "/etc"},
		"cell":       {Argv: []string{"true"}, Dir: ".cell/env"},
		"empty argv": {},
	}
	for name, req := range bad {
		if _, err := (Local{Root: t.TempDir()}).Exec(context.Background(), req, nil); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := (Local{Root: t.TempDir(), Class: FilesOnly}).Exec(context.Background(), sh("true"), nil); err == nil {
		t.Error("files only: want an error")
	}
}

func TestLocalSurfacesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := (Local{Root: t.TempDir()}).Exec(ctx, sh("sleep 30"), nil); err == nil {
		t.Fatal("want the cancellation reported")
	}
}

func TestRemoteIsNotImplemented(t *testing.T) {
	var _ Executor = Remote{}
	if _, err := (Remote{}).Exec(context.Background(), sh("true"), nil); err != ErrRemoteNotImplemented {
		t.Fatalf("got %v", err)
	}
}

func TestCombinedStreamKeepsArrivalOrderAndRetainsNothing(t *testing.T) {
	req := sh("echo a; echo b >&2; echo c")
	req.Combined, req.Stream = true, true
	var got strings.Builder
	res := exec1(t, req, func(c Chunk) { got.Write(c.Data) })
	if got.String() != "a\nb\nc\n" || len(res.Stdout) != 0 || len(res.Stderr) != 0 {
		t.Fatalf("got %q, result kept %q/%q", got.String(), res.Stdout, res.Stderr)
	}
}

func TestFailureNamesTheProcessStatus(t *testing.T) {
	res := exec1(t, sh("exit 3"), nil)
	if err := res.Failure(); err == nil || err.Error() != "exit status 3" {
		t.Fatalf("Failure = %v", err)
	}
	if exec1(t, sh("true"), nil).Failure() != nil {
		t.Fatal("a clean exit is not a failure")
	}
}

func TestCommandBuildsAnUnstartedCommandInItsOwnGroup(t *testing.T) {
	cmd, err := Local{Root: t.TempDir()}.Command(context.Background(), sh("true"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Process != nil || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("want an unstarted command with Setpgid, got %+v", cmd)
	}
}
