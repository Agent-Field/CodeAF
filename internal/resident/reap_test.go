package resident

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ONE NODE, ONE WORKER.
//
// The ink run of 2026-08-29 released `task-2` at 07:30:13.856 and a fresh worker
// claimed it at 07:30:13.873 — seventeen milliseconds later — while the worker
// that had held it went on making model calls and editing the same checkout for
// another eleven minutes. The store shows both streams under one node: turns
// 1-25 of the new attempt flushed in between turns 45 and 67 of the old one.
//
// The claim was never the thing to take. The worker is stopped, and its own
// landing hands the claim back, so the node is never pending while somebody is
// still working it.
func TestAReapedLeafIsStoppedBeforeItsNodeIsClaimableAgain(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	var mu sync.Mutex
	var statusAtCancel store.Status
	var tokenAtCancel uint64
	starts := make(chan struct{}, 4)
	saw := make(chan struct{}, 4)

	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		starts <- struct{}{}
		<-ctx.Done()
		// THE MOMENT THAT MATTERS. The worker has just been told to stop; the
		// node must still be its own, because a node released while its worker
		// is still here is a node two workers can hold.
		current, _, err := graph.Node(node.ID)
		mu.Lock()
		if err == nil && statusAtCancel == "" {
			statusAtCancel, tokenAtCancel = current.Status, current.ClaimToken
		}
		mu.Unlock()
		saw <- struct{}{}
		return ExecResult{}, ctx.Err()
	}, "reaper", 2)
	// A window short enough that the very first sweep after dispatch finds the
	// claim silent: nothing this worker does writes a usage row or a turn.
	runner.WithStaleAge(20 * time.Millisecond)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	waitFor(t, starts, "the leaf never started")
	waitFor(t, saw, "the reaper never cancelled the leaf")
	// The second claim proves the node came back at all.
	waitFor(t, starts, "the node never became claimable again")
	stop()
	<-served
	runner.Wait()

	mu.Lock()
	status, token := statusAtCancel, tokenAtCancel
	mu.Unlock()
	if status != store.Running {
		t.Fatalf("the node was %s when its worker was told to stop, want still running on its own claim", status)
	}
	if token != 1 {
		t.Fatalf("the claim had already moved to token %d under a live worker", token)
	}

	// And the record says so in the one order that can be checked afterwards:
	// the worker reported it stopped, and only then was the claim freed.
	stopped, released := journalOrder(t, graph, "task-2", 1)
	if stopped == 0 {
		t.Fatal("no leaf_stopped was journaled for the claim the reaper cancelled")
	}
	if released == 0 {
		t.Fatal("the cancelled claim was never released")
	}
	if stopped > released {
		t.Fatalf("leaf_stopped is at seq %d and the release at %d — the claim was freed before its worker stopped",
			stopped, released)
	}
}

// And the successor starts only after the predecessor has gone. This is the
// same invariant read from the other end: not "the row was free" but "the
// goroutine had returned".
func TestAResumedLeafStartsOnlyAfterTheOldOneStopped(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	var mu sync.Mutex
	var order []string
	note := func(what string) {
		mu.Lock()
		order = append(order, what)
		mu.Unlock()
	}
	attempts := 0
	starts := make(chan struct{}, 8)

	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		attempts++
		mine := attempts
		mu.Unlock()
		note("start")
		starts <- struct{}{}
		if mine > 1 {
			// The successor: it only has to have started, and the assertion is
			// about what the record already holds by then.
			<-ctx.Done()
			note("stop")
			return ExecResult{}, ctx.Err()
		}
		<-ctx.Done()
		// A worker does not vanish the instant it is told to. Everything it does
		// while unwinding — flushing its record, releasing a lock — happens
		// before its claim can move, or the next worker is racing it.
		time.Sleep(150 * time.Millisecond)
		note("stop")
		return ExecResult{}, ctx.Err()
	}, "reaper", 4)
	runner.WithStaleAge(20 * time.Millisecond)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()
	waitFor(t, starts, "the leaf never started")
	waitFor(t, starts, "the node was never picked up again")
	stop()
	<-served
	runner.Wait()

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) < 3 || got[0] != "start" || got[1] != "stop" || got[2] != "start" {
		t.Fatalf("the order was %v, want the first worker to have stopped before the second started", got)
	}
}

// spliceOneLeaf puts a single claimable leaf under the root.
func spliceOneLeaf(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: "grid layout", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "reaper", Intent: "grid layout"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
}

// waitFor takes one signal or fails the test, so a broken invariant reads as
// what did not happen rather than as a ten-second hang.
func waitFor(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(8 * time.Second):
		t.Fatal(what)
	}
}

// journalOrder finds where a node's worker reported it had stopped and where the
// claim it held was released, by sequence, for one token.
func journalOrder(t *testing.T, graph *store.Store, nodeID string, token uint64) (stopped, released int64) {
	t.Helper()
	events, err := graph.Events(0, 500)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, event := range events {
		if event.NodeID != nodeID {
			continue
		}
		var carried struct {
			Token uint64 `json:"token"`
		}
		if json.Unmarshal(event.Payload, &carried) != nil || carried.Token != token {
			continue
		}
		switch event.Kind {
		case store.EventLeafStopped:
			stopped = event.Seq
		case store.EventNodeReleased:
			released = event.Seq
		}
	}
	return stopped, released
}

// A CALL IN FLIGHT IS A SIGN OF LIFE, AND IT IS THE ONE THE JOURNAL CANNOT HOLD.
//
// Every durable mark a worker leaves is written when something FINISHES — a
// usage row when a call is billed, a transcript flush when sixty-four entries
// fill — so a leaf spending its whole window inside one long shell command
// writes nothing at all and reads, to a sweep of the store, as a corpse. This is
// the case that made the batching in store/transcript.go and the reaper
// incompatible, and it is answered without a flush timer: the process doing the
// waiting says it is waiting.
//
// Both halves are driven by calling Tick directly rather than by running Serve,
// so what is being measured is the sweep's judgement and not the dispatch
// loop's poll interval.
func TestALeafInsideOneLongCommandIsNotReaped(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	const window = 60 * time.Millisecond
	started := make(chan struct{}, 4)
	var mu sync.Mutex
	reapedDuringTheCommand := false

	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		started <- struct{}{}
		// One command, three windows long, writing nothing to the store: no
		// usage row, no transcript flush, no turn recorded.
		defer executor.Working(ctx)()
		select {
		case <-time.After(3 * window):
		case <-ctx.Done():
			mu.Lock()
			reapedDuringTheCommand = true
			mu.Unlock()
		}
		return ExecResult{Summary: "the command finished"}, nil
	}, "reaper", 2)
	runner.WithStaleAge(window)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if _, err := runner.Tick(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitFor(t, started, "the leaf never started")
	sweepFor(t, runner, ctx, 4*window)
	runner.Wait()

	mu.Lock()
	reaped := reapedDuringTheCommand
	mu.Unlock()
	if reaped {
		t.Fatal("a leaf waiting on one long command was reaped — the sweep read only what had already been written down")
	}
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node.Status != store.Done {
		t.Fatalf("task-2 is %s, want the command's own landing", node.Status)
	}
}

// And the same leaf with nothing in flight is reaped, which is what keeps the
// mark honest: it reports work, it does not confer immunity.
func TestALeafWithNothingInFlightIsStillReaped(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	const window = 60 * time.Millisecond
	started := make(chan struct{}, 4)
	cancelled := make(chan struct{}, 4)

	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		started <- struct{}{}
		// The same three windows of wall clock, and nothing to show for them:
		// no tool issued, no model call, nothing recorded.
		select {
		case <-time.After(3 * window):
		case <-ctx.Done():
			cancelled <- struct{}{}
			return ExecResult{}, ctx.Err()
		}
		return ExecResult{Summary: "never reached"}, nil
	}, "reaper", 2)
	runner.WithStaleAge(window)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if _, err := runner.Tick(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	waitFor(t, started, "the leaf never started")
	sweepFor(t, runner, ctx, 4*window)
	select {
	case <-cancelled:
	default:
		t.Fatal("a leaf with no call in flight and nothing written down was left holding its claim")
	}
	runner.Wait()
}

// sweepFor runs the reaper's own pass repeatedly for a stretch of wall clock, so
// a test measures what the sweep decides rather than how often Serve polls.
func sweepFor(t *testing.T, runner *Runner, ctx context.Context, howLong time.Duration) {
	t.Helper()
	deadline := time.Now().Add(howLong)
	for time.Now().Before(deadline) {
		if _, err := runner.Tick(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		time.Sleep(howLong / 20)
	}
}
