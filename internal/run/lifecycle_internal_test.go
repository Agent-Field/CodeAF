package run

// A RUN'S ENDING IS WRITTEN WHERE THE NEXT REQUEST READS IT, AND ITS LIMITS END
// WHAT THEY SAY THEY END.
//
// The supervisor's own half of the run-lifecycle findings: Start ran a root that
// already carried another run's brief, wrote no ending on the root for any
// ending but the tree's own completion, a receipt that reached the dollar limit
// ended none of its peers, and a review the store would not seat was read only
// after a root that said done had already answered done.

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// lifecycleStore opens a fresh store whose root carries the brief given.
func lifecycleStore(t *testing.T, brief string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "lifecycle", "root", "the run", brief)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestStartDoesNotRunARootThatCarriesAnotherBrief is the root cause of the
// stale-run adoption the reviewer measured ("FIRST BRIEF" run under a second
// hand-off): a door handed Start a store an earlier run had left open, and Start
// wrote the new brief only onto a bare root, so it ran the old one.
func TestStartDoesNotRunARootThatCarriesAnotherBrief(t *testing.T) {
	store := lifecycleStore(t, "FIRST BRIEF")
	var launches atomic.Int32
	factory := func(plandb.Task) Worker {
		launches.Add(1)
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			return Report{Result: "ran the first brief again"}, nil
		})
	}
	outcome, _ := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "the second run", Brief: "SECOND BRIEF", Factory: factory,
	})
	if outcome != OutcomeCannotRun {
		t.Fatalf("outcome = %q, want %q for a root that carries another run's brief", outcome, OutcomeCannotRun)
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("%d workers were launched on another run's brief", got)
	}
	if root := store.Task("root"); root.Description != "FIRST BRIEF" || terminalStatus(root.Status) {
		t.Fatalf("the other run's root was touched: %s %q", root.Status, root.Description)
	}
}

// TestStartWritesTheRootsEndingWhenALimitEndsTheRun: only a person's stop and
// the tree's completion wrote an ending on the run's own task, so a run that
// reached its dollar limit stayed `running` in its store.
func TestStartWritesTheRootsEndingWhenALimitEndsTheRun(t *testing.T) {
	store := lifecycleStore(t, "run until the limit")
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "leaf", ParentID: "root"}}); err != nil {
		t.Fatalf("add a leaf: %v", err)
	}
	factory := func(plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.ID == "root" {
				bankSpend(ctx, 2)
			}
			<-ctx.Done()
			return Report{USD: 0}, ctx.Err()
		})
	}
	outcome, _ := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "the run", Brief: "run until the limit",
		Slots: 1, Limits: Limits{CostUSD: 1}, Factory: factory,
	})
	if outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	root := store.Task("root")
	if root.Status != plandb.StatusFailed {
		t.Fatalf("the run's own task after its limit = %s, want failed", root.Status)
	}
	if leaf := store.Task("leaf"); !terminalStatus(leaf.Status) {
		t.Fatalf("work still open under a run its limit ended = %s", leaf.Status)
	}
}

// TestStartWritesTheRootsEndingWhenTheRootWorkerFails is the same law for the
// other ending the run owns: its own worker failing.
func TestStartWritesTheRootsEndingWhenTheRootWorkerFails(t *testing.T) {
	store := lifecycleStore(t, "fail at once")
	factory := func(plandb.Task) Worker {
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			return Report{}, errors.New("the root worker broke")
		})
	}
	outcome, _ := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "the run", Brief: "fail at once", Factory: factory,
	})
	if outcome != OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeIncomplete)
	}
	if root := store.Task("root"); root.Status != plandb.StatusFailed {
		t.Fatalf("the run's own task after its worker failed = %s, want failed", root.Status)
	}
}

// TestStartLeavesARunItsCallerCutOpen is the one ending deliberately left
// unwritten: a context the caller cancelled is a closing conversation or a
// process told to stop, which decided nothing about the work. The door that
// opens the store next sets such a run aside as interrupted.
func TestStartLeavesARunItsCallerCutOpen(t *testing.T) {
	store := lifecycleStore(t, "run until cut")
	ctx, cancel := context.WithCancel(context.Background())
	factory := func(plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, _ plandb.Task) (Report, error) {
			cancel()
			<-ctx.Done()
			return Report{}, ctx.Err()
		})
	}
	outcome, _ := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Title: "the run", Brief: "run until cut", Factory: factory})
	if outcome != OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeIncomplete)
	}
	if root := store.Task("root"); terminalStatus(root.Status) {
		t.Fatalf("a run its caller cut was written as %s", root.Status)
	}
}

// TestAReceiptThatReachesTheDollarLimitEndsThePeers is the reviewer's finding
// five: a worker's receipt carried the run past its dollar limit, the limit was
// marked, and nothing was cancelled, so the peer went on working (and spending)
// until it came home on its own. The peer here holds until its context ends;
// the watchdog is only the failure road, cut the moment the run answers.
func TestAReceiptThatReachesTheDollarLimitEndsThePeers(t *testing.T) {
	store := lifecycleStore(t, "spend past the limit")
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "peer", Title: "peer", ParentID: "root"}}); err != nil {
		t.Fatalf("add the peer: %v", err)
	}
	outer, stopOuter := context.WithCancel(context.Background())
	defer stopOuter()
	watchdog := time.AfterFunc(10*time.Second, stopOuter)
	defer watchdog.Stop()

	peerStarted := make(chan struct{})
	var endedByTheRun atomic.Bool
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.ID == "peer" {
				close(peerStarted)
				<-ctx.Done()
				endedByTheRun.Store(outer.Err() == nil)
				return Report{USD: 0.1}, ctx.Err()
			}
			// THE ROOT COMES HOME WITH A RECEIPT PAST THE LIMIT AND BANKED
			// NOTHING WHILE IT WORKED, so the receipt is the road the limit is
			// reached on.
			<-peerStarted
			return Report{USD: 2}, errors.New("the root spent its all")
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{CostUSD: 1}, factory)
	outcome := supervisor.Run(outer)
	watchdog.Stop()
	if outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	if !endedByTheRun.Load() {
		t.Fatal("the peer ran on after a receipt reached the dollar limit, until the watchdog cut it")
	}
}

// TestARootThatWroteDoneStillEndsIncompleteWhenItsReviewCouldNotBeSeated is
// #1275's promise read in the order the pass reads it: a root that already
// reads done answered done before the pass looked at the mark a refused review
// seating leaves.
func TestARootThatWroteDoneStillEndsIncompleteWhenItsReviewCouldNotBeSeated(t *testing.T) {
	store := landingStore(t)
	if err := store.CompleteRoot("the root's own answer"); err != nil {
		t.Fatalf("complete the root: %v", err)
	}
	s := NewSupervisor(store, t.TempDir(), 1, Limits{ReviewRound: true}, func(plandb.Task) Worker { return nil })
	s.dispatchedRoot = true
	// THE SEATING THE STORE REFUSED, by the road addReviewCheck marks it.
	s.addReviewCheck(plandb.Task{TaskSpec: plandb.TaskSpec{ID: "lost", Title: "lost", ParentID: "missing"}}, "finished")
	if !s.rootFailed {
		t.Fatal("the refused seating left no mark")
	}
	if got := s.pass(context.Background(), "root"); got != OutcomeIncomplete {
		t.Fatalf("pass = %q over a review that could not be seated, want %q", got, OutcomeIncomplete)
	}
}
