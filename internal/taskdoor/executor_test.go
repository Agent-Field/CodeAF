//go:build linux || darwin

package taskdoor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// The scripted child exercises the real process road without buying a provider
// call. All control files and its home belong to this attempt's fixture.
func TestTaskChildHelper(t *testing.T) {
	if os.Getenv("CODEAF_TASK_CHILD_HELPER") != "1" {
		return
	}
	args := os.Args
	value := func(flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	work, store := value("--dir"), value("--task-store")
	if work == "" || store == "" {
		os.Exit(1)
	}
	var request taskdoor.Start
	requestRaw, err := os.ReadFile(filepath.Join(filepath.Dir(store), "request.json"))
	if err != nil || json.Unmarshal(requestRaw, &request) != nil || args[len(args)-1] != request.Instruction {
		os.Exit(1)
	}
	for _, arg := range args {
		if arg == "--yes-spend" {
			os.Exit(1)
		}
	}
	if value("--timeout") != taskdoor.DefaultTaskTimeout.String() {
		os.Exit(1)
	}
	if pgid, err := syscall.Getpgid(os.Getpid()); err != nil || pgid != os.Getpid() {
		os.Exit(1)
	}
	for _, stream := range []*os.File{os.Stdout, os.Stderr} {
		if info, err := stream.Stat(); err != nil || !info.Mode().IsRegular() {
			os.Exit(1)
		}
	}
	_ = os.MkdirAll(filepath.Join(store, "tasks", "1"), 0700)
	_ = os.MkdirAll(filepath.Join(store, "tasks", "2"), 0700)
	header, _ := os.Create(filepath.Join(store, "tasks", "2", "fixture_worker.jsonl"))
	_ = json.NewEncoder(header).Encode(map[string]string{"type": "session", "id": "child-session"})
	_ = header.Close()
	f, _ := os.OpenFile(filepath.Join(filepath.Dir(store), "starts"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = fmt.Fprintln(f, os.Getpid())
	_ = f.Close()
	steps, _ := os.Create(filepath.Join(store, "tasks", "2", "trajectory.jsonl"))
	if os.Getenv("CODEAF_TASK_CHILD_MODE") == "corrupt" {
		_, _ = steps.WriteString("unreadable step\n")
	} else {
		_ = json.NewEncoder(steps).Encode(run.Step{Kind: "step", Step: 1, Command: "first", RecordedOrder: 1})
	}
	_ = steps.Close()
	ledger, _ := os.OpenFile(os.Getenv("CODEAF_TASK_CHILD_LEDGER"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	_ = json.NewEncoder(ledger).Encode(map[string]any{"session": "another-session", "workspace": work, "model": "other-model", "calls": 1, "usd": 9.0})
	_ = json.NewEncoder(ledger).Encode(map[string]any{"session": "child-session", "workspace": work, "model": "test-model", "seat": "worker", "calls": 1, "usd": 0.125, "in": 20, "out": 5})
	_ = ledger.Close()
	signals := make(chan os.Signal, 1)
	if os.Getenv("CODEAF_TASK_CHILD_MODE") == "deaf" {
		signal.Ignore(os.Interrupt)
	} else {
		signal.Notify(signals, os.Interrupt)
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(store), "ready"), []byte("ready"), 0600)
	stopped := false
	for {
		if _, err := os.Stat(filepath.Join(filepath.Dir(store), "finish")); err == nil {
			break
		}
		select {
		case <-signals:
			stopped = true
		case <-time.After(10 * time.Millisecond):
		}
		if stopped {
			break
		}
	}
	if os.Getenv("CODEAF_TASK_CHILD_DIE") == "1" {
		os.Exit(9)
	}
	if os.Getenv("CODEAF_TASK_CHILD_MODE") == "late" {
		for {
			if _, err := os.Stat(filepath.Join(filepath.Dir(store), "allow-exit")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	steps, _ = os.Create(filepath.Join(store, "tasks", "1", "trajectory.jsonl"))
	_ = json.NewEncoder(steps).Encode(run.Step{Kind: "step", Step: 2, Command: "second", RecordedOrder: 2})
	_ = steps.Close()
	ledger, _ = os.OpenFile(os.Getenv("CODEAF_TASK_CHILD_LEDGER"), os.O_APPEND|os.O_WRONLY, 0600)
	_ = json.NewEncoder(ledger).Encode(map[string]any{"session": "child-session", "workspace": work, "model": "test-model", "seat": "worker", "calls": 1, "usd": 0.375, "in": 30, "out": 7})
	_ = ledger.Close()
	_ = os.WriteFile(filepath.Join(work, "child.txt"), []byte("the child worked"), 0640)
	stop, code := "done", 0
	if stopped {
		stop, code = "incomplete", 2
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": !stopped, "stop": stop, "answer": "the child's report", "spend_usd": 0.5, "model": "test-model", "files": []string{"child.txt"}})
	os.Exit(code)
}

type childRig struct {
	r                      *rig
	copies                 taskdoor.CopyDeps
	input                  taskdoor.Input
	ledger, dir, childHome string
	id                     string
	mode                   string
	clock                  taskdoor.Clock
}

func newChildRig(t *testing.T) *childRig {
	t.Helper()
	a, b := copyHomes(t)
	in, err := taskdoor.SendCopy(context.Background(), a, copyRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t)
	short, err := os.MkdirTemp("", "rn-l1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	return &childRig{r: r, copies: b, input: in, ledger: filepath.Join(short, "usage.jsonl"), childHome: short, id: attempt(70), dir: filepath.Join(r.root, attempt(70))}
}

func (c *childRig) config(t *testing.T, die bool) taskdoor.Config {
	t.Helper()
	config := c.r.config()
	config.Executor = taskdoor.NewExecutor(taskdoor.ExecutorConfig{Copies: c.copies, Ledger: c.ledger, Clock: c.clock,
		Command: func(args []string) *exec.Cmd {
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestTaskChildHelper$", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "HOME="+c.childHome, "CODEAF_HOME="+c.childHome, "CODEAF_TASK_CHILD_HELPER=1", "CODEAF_TASK_CHILD_LEDGER="+c.ledger, "CODEAF_TASK_CHILD_MODE="+c.mode, fmt.Sprintf("CODEAF_TASK_CHILD_DIE=%d", map[bool]int{true: 1}[die]))
			return cmd
		}})
	return config
}

// Contract 12 also covers a crash after launch intent but before the pid
// rename: the waiting child is found by its attempt paths, never restarted.
func TestAnAdoptedLaunchIntentFindsItsChild(t *testing.T) {
	c := newChildRig(t)
	stop := c.r.serve(c.config(t, false))
	conn := c.start(t)
	_ = conn.Close()
	stop()
	path := filepath.Join(c.dir, "state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state taskdoor.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state.PID = 0
	state.ProcessStart = ""
	state.CommandHash = ""
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c.r.serve(c.config(t, false))
	_ = os.WriteFile(filepath.Join(c.dir, "finish"), nil, 0600)
	_, _, done := childFinish(t, c.r.dial(), c.id)
	if done.Outcome != "done" {
		t.Fatalf("launch was not adopted: %+v", done)
	}
	starts, _ := os.ReadFile(filepath.Join(c.dir, "starts"))
	if len(strings.Fields(string(starts))) != 1 {
		t.Fatalf("child restarted: %q", starts)
	}
}

// A follower error cannot report completion and release a running slot while
// its child is still making provider calls outside the saved journal.
func TestAFollowingErrorStopsTheChildBeforeFinishing(t *testing.T) {
	c := newChildRig(t)
	c.mode = "corrupt"
	c.r.serve(c.config(t, false))
	conn := c.r.dial()
	req := request()
	req.Input = c.input
	req.Instruction = "--help"
	send(t, conn, c.id, req)
	receive(t, conn, "started")
	_, _, done := childFinish(t, conn, c.id)
	if done.Outcome != "incomplete" {
		t.Fatalf("finish: %+v", done)
	}
	raw, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state taskdoor.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(state.PID, 0); err == nil {
		t.Fatal("unfollowed child still lives after finished")
	}
}

type childGraceClock struct{ *manualClock }

func (c childGraceClock) NewTicker(d time.Duration) taskdoor.Ticker {
	if d < time.Second {
		d = time.Second
	}
	return c.manualClock.NewTicker(d)
}

// DESIGN section 3.4 gives an interrupt thirty seconds to land partial work;
// an unresponsive process is then killed without waiting thirty real seconds.
func TestAChildIgnoringStopIsKilledAfterItsGrace(t *testing.T) {
	c := newChildRig(t)
	c.mode = "deaf"
	clock := newClock()
	c.clock = childGraceClock{clock}
	c.r.serve(c.config(t, false))
	conn := c.start(t)
	clock.awaitTicker(t)
	send(t, conn, c.id, taskdoor.Stop{})
	receive(t, conn, "stopping")
	clock.advance(taskdoor.ChildStopGrace - time.Second)
	raw, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state taskdoor.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(state.PID, 0); err != nil {
		t.Fatal("child killed before grace", err)
	}
	clock.advance(time.Second)
	_, _, done := childFinish(t, conn, c.id)
	if done.Outcome != "stopped" {
		t.Fatalf("finish: %+v", done)
	}
}
func (c *childRig) start(t *testing.T) *taskdoor.Conn {
	t.Helper()
	conn := c.r.dial()
	req := request()
	req.Input = c.input
	send(t, conn, c.id, req)
	receive(t, conn, "started")
	untilFile(t, filepath.Join(c.dir, "ready"))
	return conn
}
func untilFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "result.json")); err == nil {
			t.Fatalf("child ended before ready: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not write %s", path)
}
func childFinish(t *testing.T, conn *taskdoor.Conn, id string) ([]taskdoor.Step, []taskdoor.Spend, taskdoor.Finished) {
	t.Helper()
	send(t, conn, id, taskdoor.Watch{})
	var steps []taskdoor.Step
	var spend []taskdoor.Spend
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		f, err := conn.Receive()
		if err != nil {
			t.Fatal(err)
		}
		switch f.Type {
		case "step":
			var s taskdoor.Step
			decode(t, f, &s)
			steps = append(steps, s)
		case "spend":
			var s taskdoor.Spend
			decode(t, f, &s)
			spend = append(spend, s)
		case "finished":
			var done taskdoor.Finished
			decode(t, f, &done)
			return steps, spend, done
		case "phase":
		default:
			t.Fatalf("unexpected event %s", f.Payload)
		}
	}
}

// Contract 12: a serving restart follows the surviving process and replays
// each journaled step and charge once, without re-running the command.
func TestAnAdoptedChildIsFollowedNotRestarted(t *testing.T) {
	c := newChildRig(t)
	stop := c.r.serve(c.config(t, false))
	conn := c.start(t)
	send(t, conn, c.id, taskdoor.Watch{})
	receive(t, conn, "step")
	_ = conn.Close()
	stop()
	var saved taskdoor.State
	raw, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ProcessStart == "" || saved.CommandHash == "" {
		t.Fatalf("child identity missing: %+v", saved)
	}
	c.r.serve(c.config(t, false))
	if err := os.WriteFile(filepath.Join(c.dir, "finish"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	steps, spend, done := childFinish(t, c.r.dial(), c.id)
	if len(steps) != 2 || steps[0].Step.Command != "first" || steps[1].Step.Command != "second" {
		t.Fatalf("steps: %+v, finish: %+v, state: %+v", steps, done, saved)
	}
	if len(spend) != 2 || done.Outcome != "done" || done.Result == nil || done.Model != "test-model" {
		t.Fatalf("finish: %+v, spend=%+v", done, spend)
	}
	starts, err := os.ReadFile(filepath.Join(c.dir, "starts"))
	if err != nil || len(strings.Fields(string(starts))) != 1 {
		t.Fatalf("child restarted: %q, %v", starts, err)
	}
}

// Contract 12: a dead child without a complete receipt is interrupted; a
// serving restart must never repeat the tools it may already have run.
func TestAChildThatDiedWithoutAnAnswerIsInterrupted(t *testing.T) {
	c := newChildRig(t)
	stop := c.r.serve(c.config(t, true))
	conn := c.start(t)
	_ = conn.Close()
	stop()
	var state taskdoor.State
	raw, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(state.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	c.r.serve(c.config(t, true))
	_, _, done := childFinish(t, c.r.dial(), c.id)
	if done.Outcome != "interrupted" {
		t.Fatalf("finish: %+v", done)
	}
	starts, _ := os.ReadFile(filepath.Join(c.dir, "starts"))
	if len(strings.Fields(string(starts))) != 1 {
		t.Fatalf("child restarted: %q", starts)
	}
}

// Contract 5: the origin receives one charge per provider call, and the sum
// is the child's own receipt rather than an estimate from step counts.
func TestRemoteSpendSumsToTheRunsOwnBill(t *testing.T) {
	c := newChildRig(t)
	c.r.serve(c.config(t, false))
	conn := c.start(t)
	_ = os.WriteFile(filepath.Join(c.dir, "finish"), nil, 0600)
	_, spend, _ := childFinish(t, conn, c.id)
	var bill struct {
		Spend float64 `json:"spend_usd"`
	}
	raw, err := os.ReadFile(filepath.Join(c.dir, "do.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &bill); err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, s := range spend {
		sum += s.Line.USD
	}
	if len(spend) != 2 || math.Abs(sum-bill.Spend) > 1e-9 {
		t.Fatalf("spend=%+v sum=%g bill=%g", spend, sum, bill.Spend)
	}
}

// DESIGN section 4's stop road must interrupt the child's group, keep its
// partial work and publish a stopped result that can still come home.
func TestAStopEndsTheChildAndFinishesStopped(t *testing.T) {
	c := newChildRig(t)
	c.r.serve(c.config(t, false))
	conn := c.start(t)
	send(t, conn, c.id, taskdoor.Stop{})
	receive(t, conn, "stopping")
	_, _, done := childFinish(t, conn, c.id)
	if done.Outcome != "stopped" || done.Result == nil {
		t.Fatalf("finish: %+v", done)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "work", "child.txt")); err != nil {
		t.Fatal("partial work was lost", err)
	}
}

// Contracts 11 and 12: abandonment remains durable while the child lands its
// interrupt, and its final writes cannot recreate a broken attempt on restart.
func TestAnAbandonedRealChildIsRemovedAfterItStops(t *testing.T) {
	c := newChildRig(t)
	c.mode = "late"
	stop := c.r.serve(c.config(t, false))
	conn := c.start(t)
	send(t, conn, c.id, taskdoor.Ack{Disposition: "abandoned"})
	receive(t, conn, "acked")
	_ = conn.Close()
	stop()
	c.r.serve(c.config(t, false))
	if err := os.WriteFile(filepath.Join(c.dir, "allow-exit"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(c.dir); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("abandoned child left an attempt folder")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn = c.r.dial()
	send(t, conn, c.id, taskdoor.Watch{})
	receive(t, conn, "unknown")
}

// Contract 3's ignore rule applies to local capture checks too: a build's
// ignored socket is neither task input nor a reason to refuse the transfer.
func TestIgnoredRuntimeFilesDoNotPreventSendingATaskCopy(t *testing.T) {
	a, b := copyHomes(t)
	local := copyRepo(t)
	if err := os.MkdirAll(filepath.Join(local, "ignored"), 0700); err != nil {
		t.Fatal(err)
	}
	// A short socket path avoids platform limits even with a long test name.
	socketDir, err := os.MkdirTemp("", "rn-l1-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	listener, err := net.Listen("unix", filepath.Join(socketDir, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Rename(filepath.Join(socketDir, "socket"), filepath.Join(local, "ignored", "socket")); err != nil {
		t.Fatal(err)
	}
	in, err := taskdoor.SendCopy(t.Context(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "work")
	if err := taskdoor.ReceiveCopy(t.Context(), b, in, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "ignored")); !os.IsNotExist(err) {
		t.Fatalf("ignored runtime files traveled: %v", err)
	}
}
