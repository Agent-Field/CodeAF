package resident

import (
	"context"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A LEAF THAT RAN OUT HAS NO ACCOUNT, EVEN WHEN IT CAME BACK ON ITS OWN TWO
// FEET. The budget lands a leaf by buying it a short reserve, and the leaf
// complying with that order returns with err == nil — which the settle path
// used to read as an ordinary finish and settle the node done over a leaf that
// was still working when its grant ran out. The headless run of 2026-09-01
// journaled `leaf_exhausted` and, two seconds later, `node_completed` with the
// leaf's own last line of prose as its summary. Exhaustion is a measured fact;
// doneness is a claim.
func TestAnExhaustedResultWithRecordedTurnsRequeues(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")
	recordTurns(t, graph, "task-2", 3)

	var mu sync.Mutex
	attempts := 0
	starts := make(chan struct{}, 8)
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		attempts++
		mine := attempts
		mu.Unlock()
		starts <- struct{}{}
		if mine == 1 {
			// Exactly what cmd/aforge hands back when the executor was told to
			// land: no error, the bound that fired, and the meter that named it.
			return ExecResult{
				Summary: "All 722 tests pass. Let me verify the dry-run tests specifically:",
				Stop:    executor.StopBudget,
				Meter:   executor.Meter{Name: "cost", Reached: 178086, Allowed: 176834, Unit: "tokens of billed work"},
			}, nil
		}
		return ExecResult{Summary: "grid layout landed"}, nil
	}, "chat-runner", 1)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	waitFor(t, starts, "the leaf never started")
	waitFor(t, starts, "the exhausted node was never offered again — it was settled done")
	deadline := time.Now().Add(8 * time.Second)
	var final store.Node
	for time.Now().Before(deadline) {
		node, found, err := graph.Node("task-2")
		if err != nil {
			t.Fatalf("read the node: %v", err)
		}
		if found && node.Status == store.Done {
			final = node
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-served
	runner.Wait()

	if final.Status != store.Done {
		t.Fatalf("the node settled %q, want done — the requeue must resume, not end, the node", final.Status)
	}
	if final.Attempt < 2 {
		t.Fatalf("the node was claimed %d times, want the re-claim that resumes the work", final.Attempt)
	}
	if final.Summary == "All 722 tests pass. Let me verify the dry-run tests specifically:" {
		t.Fatal("the node's summary is the exhausted leaf's own last line of prose — the exact defect this fixes")
	}
	// The release says why, naming what survived, so the next claim resumes
	// rather than cold-starting.
	reason := releaseReason(t, graph, "task-2")
	if reason == "" {
		t.Fatal("the exhausted claim was released with no reason; the stream has nothing to say")
	}
	for _, want := range []string{"budget", "3 turns"} {
		if !contains(reason, want) {
			t.Fatalf("the release reason %q does not name %q", reason, want)
		}
	}
}

// AND A LEAF THAT RAN OUT WITH NOTHING RECORDED IS NOT RESUMABLE, so the node
// fails — with the exhaustion named, never with the leaf's prose as a verdict.
func TestAnExhaustedResultWithNothingRecordedFails(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	starts := make(chan struct{}, 8)
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		starts <- struct{}{}
		return ExecResult{
			Summary: "All 722 tests pass. Let me verify the dry-run tests specifically:",
			Stop:    executor.StopBudget,
			Meter:   executor.Meter{Name: "cost", Reached: 178086, Allowed: 176834, Unit: "tokens of billed work"},
		}, nil
	}, "chat-runner", 1)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	waitFor(t, starts, "the leaf never started")
	deadline := time.Now().Add(8 * time.Second)
	var final store.Node
	for time.Now().Before(deadline) {
		node, found, err := graph.Node("task-2")
		if err != nil {
			t.Fatalf("read the node: %v", err)
		}
		if found && node.Status == store.Failed {
			final = node
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-served
	runner.Wait()

	if final.Status != store.Failed {
		t.Fatalf("a leaf that recorded nothing settled %q, want failed", final.Status)
	}
	if final.Summary == "All 722 tests pass. Let me verify the dry-run tests specifically:" {
		t.Fatal("the node's summary is the exhausted leaf's own last line of prose")
	}
	if !contains(final.Error, "budget") {
		t.Fatalf("the failure %q does not name the exhaustion", final.Error)
	}
}
