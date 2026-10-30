package taskdoor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

type fakeExecutor struct {
	calls  atomic.Int32
	adopts atomic.Int32
	stops  chan string
	run    func(context.Context, *taskdoor.Attempt, taskdoor.Journal) error
	adopt  func(context.Context, *taskdoor.Attempt, taskdoor.Journal) error
}

func (f *fakeExecutor) Run(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
	f.calls.Add(1)
	if f.run != nil {
		return f.run(ctx, a, j)
	}
	return j.Append(taskdoor.Finished{Outcome: "done", Report: "the work is home", Model: "test-model"})
}
func (f *fakeExecutor) Adopt(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
	f.adopts.Add(1)
	if f.adopt != nil {
		return f.adopt(ctx, a, j)
	}
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeExecutor) Stop(id string) error {
	if f.stops != nil {
		f.stops <- id
	}
	return nil
}

type rig struct {
	t                   *testing.T
	id                  identity.Identity
	a, b                identity.Dev
	home, root, service string
	executor            *fakeExecutor
	rosterMu            sync.Mutex
	active              map[string]bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeA, homeB := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(homeA)
	if err != nil {
		t.Fatal(err)
	}
	a, err := identity.LoadDevice(homeA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Install(homeB, id, b); err != nil {
		t.Fatal(err)
	}
	if _, err := devname.Set(homeB, "work computer"); err != nil {
		t.Fatal(err)
	}
	relayServer := httptest.NewServer(&relay.Server{})
	t.Cleanup(relayServer.Close)
	t.Cleanup(relayServer.CloseClientConnections)
	return &rig{t: t, id: id, a: a, b: b, home: homeB, root: filepath.Join(homeB, "v3", "taskdoor"), service: relayServer.URL, executor: &fakeExecutor{}, active: map[string]bool{a.ID(): true, b.ID(): true}}
}
func (r *rig) config() taskdoor.Config {
	return taskdoor.Config{Service: r.service, Identity: r.id, Device: r.b, Home: r.home, Root: r.root, Executor: r.executor,
		Roster: func(context.Context) (map[string]bool, error) {
			r.rosterMu.Lock()
			defer r.rosterMu.Unlock()
			out := map[string]bool{}
			for k, v := range r.active {
				out[k] = v
			}
			return out, nil
		}}
}
func (r *rig) serve(config taskdoor.Config) func() {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan struct{}, 1)
	say := config.Say
	config.Say = func(line string) {
		if say != nil {
			say(line)
		}
		if line == taskdoor.TakingTasksLine {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	}
	go func() { done <- taskdoor.Serve(ctx, config) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					r.t.Errorf("Serve: %v", err)
				}
			case <-time.After(5 * time.Second):
				r.t.Error("Serve did not stop")
			}
		})
	}
	r.t.Cleanup(stop)
	select {
	case <-ready:
	case err := <-done:
		cancel()
		r.t.Fatalf("Serve before ready: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		r.t.Fatal("door never registered")
	}
	return stop
}
func (r *rig) dial() *taskdoor.Conn {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	c, err := taskdoor.Dial(ctx, r.service, r.id, r.a, r.b.ID(), "work computer")
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}
func send(t *testing.T, c *taskdoor.Conn, id string, payload any) {
	t.Helper()
	if err := c.Send(id, payload); err != nil {
		t.Fatal(err)
	}
}
func receive(t *testing.T, c *taskdoor.Conn, kind string) taskdoor.Frame {
	t.Helper()
	result := make(chan taskdoor.Frame, 1)
	failed := make(chan error, 1)
	go func() {
		frame, err := c.Receive()
		if err != nil {
			failed <- err
		} else {
			result <- frame
		}
	}()
	select {
	case frame := <-result:
		if frame.Type != kind {
			t.Fatalf("frame = %s, want %s (%s)", frame.Type, kind, frame.Payload)
		}
		return frame
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		_ = c.Close()
		t.Fatalf("no %s frame", kind)
	}
	return taskdoor.Frame{}
}
func request() taskdoor.Start {
	return taskdoor.Start{Chat: "opaque-chat", Task: 7, Title: "secret title violet heron", Instruction: "secret instruction amber apples", Input: taskdoor.Head{Head: "head-input", Cell: "cell-task"}, OriginName: "home laptop"}
}
func attempt(n int) string { return fmt.Sprintf("att_%016x", n) }
func decode(t *testing.T, frame taskdoor.Frame, into any) {
	t.Helper()
	if err := frame.Decode(into); err != nil {
		t.Fatal(err)
	}
}

// Contract 12: retrying admission, including after a door restart, must never
// replay an executor's tools.
func TestADoorTaskStartedTwiceRunsOnce(t *testing.T) {
	r := newRig(t)
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		for _, name := range []string{"request.json", "state.json"} {
			if _, err := os.Stat(filepath.Join(a.Dir, name)); err != nil {
				return err
			}
		}
		return j.Append(taskdoor.Finished{Outcome: "done", Report: "finished once", Model: "test-model"})
	}
	stop := r.serve(r.config())
	c := r.dial()
	id := attempt(1)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "finished")
	_ = c.Close()
	stop()
	r.serve(r.config())
	c = r.dial()
	send(t, c, id, request())
	var started taskdoor.Started
	decode(t, receive(t, c, "started"), &started)
	if started.State.Phase != "done" {
		t.Fatalf("recorded state = %+v", started.State)
	}
	if got := r.executor.calls.Load(); got != 1 {
		t.Fatalf("executor ran %d times", got)
	}
	if got := r.executor.adopts.Load(); got != 0 {
		t.Fatalf("completed attempt adopted %d times", got)
	}
}

// Contract 13: holding the identity seed is insufficient after device removal.
func TestARemovedComputerIsTurnedAwayAtTheDoor(t *testing.T) {
	r := newRig(t)
	r.active[r.a.ID()] = false
	r.serve(r.config())
	c, err := taskdoor.Dial(context.Background(), r.service, r.id, r.a, r.b.ID(), "work computer")
	if c != nil {
		_ = c.Close()
	}
	if err == nil || err.Error() != "this computer was removed from your devices" {
		t.Fatalf("removed device heard %v", err)
	}
	if r.executor.calls.Load() != 0 {
		t.Fatal("removed device started work")
	}
}

// Contract 8's wire half: an absent target gives the actionable serving line.
func TestADoorThatIsNotServingSaysSo(t *testing.T) {
	r := newRig(t)
	since := time.Now()
	c, err := taskdoor.Dial(context.Background(), r.service, r.id, r.a, r.b.ID(), "work computer")
	if c != nil {
		_ = c.Close()
	}
	if err == nil || err.Error() != "work computer is not taking tasks right now — run codeaf serve on it" {
		t.Fatalf("absent door heard %v", err)
	}
	if time.Since(since) > 10*time.Second {
		t.Fatal("absent door exceeded the dial timeout")
	}
}

// Contract 9's wire half: a watch replays only the durable events the origin
// has not received, even when it reconnects while the worker is still running.
func TestAWatchResumesFromItsLastSeq(t *testing.T) {
	r := newRig(t)
	advance := make(chan struct{})
	first := make(chan struct{})
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		if err := j.Append(taskdoor.Step{Step: run.Step{Kind: "step", Step: 1, Command: "first step"}}); err != nil {
			return err
		}
		close(first)
		select {
		case <-advance:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := j.Append(taskdoor.Step{Step: run.Step{Kind: "step", Step: 2, Command: "second step"}}); err != nil {
			return err
		}
		if err := j.Append(taskdoor.Spend{Line: taskdoor.SpendLine{Model: "test-model", Seat: "worker", InputTokens: 10, OutputTokens: 20, CachedTokens: 3, USD: 0.01}}); err != nil {
			return err
		}
		return j.Append(taskdoor.Finished{Outcome: "done", Report: "all done", Changed: []string{"out.txt"}, Model: "test-model", Result: &taskdoor.Head{Head: "result-head", Cell: "task-cell"}})
	}
	r.serve(r.config())
	c := r.dial()
	id := attempt(2)
	send(t, c, id, request())
	receive(t, c, "started")
	<-first
	send(t, c, id, taskdoor.Watch{})
	frame := receive(t, c, "step")
	if frame.Seq != 1 {
		t.Fatalf("first seq = %d", frame.Seq)
	}
	_ = c.Close()
	close(advance)
	c = r.dial()
	send(t, c, id, taskdoor.Watch{From: frame.Seq})
	for i, kind := range []string{"step", "spend", "finished"} {
		frame = receive(t, c, kind)
		if frame.Seq != uint64(i+2) {
			t.Fatalf("replay seq = %d", frame.Seq)
		}
	}
	var end taskdoor.Finished
	decode(t, frame, &end)
	if end.Result == nil || end.Result.Head != "result-head" || end.Model != "test-model" {
		t.Fatalf("lost result: %+v", end)
	}
}

// Contract 12's admission boundary also limits work to two concurrent attempts.
func TestAThirdTaskAtOnceIsRefused(t *testing.T) {
	r := newRig(t)
	running := make(chan struct{}, 2)
	r.executor.run = func(ctx context.Context, _ *taskdoor.Attempt, _ taskdoor.Journal) error {
		running <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(r.config())
	c := r.dial()
	for n := 1; n <= 2; n++ {
		send(t, c, attempt(n), request())
		receive(t, c, "started")
		<-running
	}
	send(t, c, attempt(3), request())
	var refusal taskdoor.Refused
	decode(t, receive(t, c, "refused"), &refusal)
	if refusal.Reason != "work computer is already running two tasks" {
		t.Fatalf("busy line = %q", refusal.Reason)
	}
	if r.executor.calls.Load() != 2 {
		t.Fatal("third executor ran")
	}
}

// Contracts 11 and 12 retain attempts until the origin explicitly acknowledges
// either landing the result or abandoning it.
func TestAnAckRemovesTheAttempt(t *testing.T) {
	for _, disposition := range []string{"landed", "abandoned"} {
		t.Run(disposition, func(t *testing.T) {
			r := newRig(t)
			r.serve(r.config())
			c := r.dial()
			id := attempt(4)
			send(t, c, id, request())
			receive(t, c, "started")
			send(t, c, id, taskdoor.Watch{})
			receive(t, c, "finished")
			send(t, c, id, taskdoor.Ack{Disposition: disposition})
			receive(t, c, "acked")
			if _, err := os.Stat(filepath.Join(r.root, id)); !os.IsNotExist(err) {
				t.Fatalf("ack left attempt folder: %v", err)
			}
			send(t, c, id, taskdoor.Watch{})
			receive(t, c, "unknown")
		})
	}
}

// Contract 12's restart rule adopts running attempts instead of replaying Run.
func TestARunningAttemptIsAdoptedAfterServeRestarts(t *testing.T) {
	r := newRig(t)
	running := make(chan struct{}, 1)
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		if err := a.UpdateState(func(s *taskdoor.State) { s.PID = 123 }); err != nil {
			return err
		}
		if err := j.Append(taskdoor.Phase{Words: "running before restart"}); err != nil {
			return err
		}
		running <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	r.executor.adopt = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		if a.State().PID != 123 {
			return fmt.Errorf("lost child pid")
		}
		return j.Append(taskdoor.Finished{Outcome: "interrupted", Report: "the child stopped"})
	}
	stop := r.serve(r.config())
	c := r.dial()
	id := attempt(5)
	send(t, c, id, request())
	receive(t, c, "started")
	<-running
	_ = c.Close()
	stop()
	r.serve(r.config())
	c = r.dial()
	send(t, c, id, taskdoor.Watch{})
	if frame := receive(t, c, "phase"); frame.Seq != 1 {
		t.Fatalf("saved phase seq = %d", frame.Seq)
	}
	if frame := receive(t, c, "finished"); frame.Seq != 2 {
		t.Fatalf("adopted finish seq = %d", frame.Seq)
	}
	send(t, c, id, request())
	receive(t, c, "started")
	if r.executor.calls.Load() != 1 || r.executor.adopts.Load() != 1 {
		t.Fatalf("run/adopt = %d/%d", r.executor.calls.Load(), r.executor.adopts.Load())
	}
	raw, err := os.ReadFile(filepath.Join(r.root, id, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result taskdoor.Finished
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "interrupted" {
		t.Fatalf("saved outcome = %q", result.Outcome)
	}
}
