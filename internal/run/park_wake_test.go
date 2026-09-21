package run_test

// THE WAIT'S LAW: A PARKED TASK WAKES ONCE, WHEN ITS WAIT IS OVER.
//
// These tests pin the two facts that end a wait and the one that does not. A
// parked task's children land one at a time (each on the one before), so the
// supervisor runs exactly one pass between landings — which makes a wake on a
// mere claim, or on a single sibling's landing while another is still running,
// show up as an extra launch of the parent's worker, deterministically and with
// no sleeps.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// feedsInto is a hard edge, so the child that carries it is not ready until its
// upstream is done — how these tests land a parked parent's children in order.
func feedsInto(id string) []plandb.Dependency {
	return []plandb.Dependency{{TaskID: id, Kind: plandb.DepFeedsInto}}
}

func isTerminal(status plandb.Status) bool {
	return status == plandb.StatusDone || status == plandb.StatusFailed || status == plandb.StatusCancelled
}

// TestSupervisorWakesAParkedRootOnceWhenItsChildrenLand: a root that planned
// three leaves and parked is launched again once, when the last of them lands,
// and the clause it opens on names all three. The children land one at a time,
// so a wake on a claim — or on one sibling's landing while another still runs —
// would show as extra root turns; and the root's own report after the wake is
// the run's result, not the empty one a premature close would leave.
func TestSupervisorWakesAParkedRootOnceWhenItsChildrenLand(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()

	var (
		mu      sync.Mutex
		turns   int
		clauses []string
	)
	seat.actions["root"] = func(c context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		turns++
		if turns == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "a", Title: "leaf a", ParentID: task.ID},
				{ID: "b", Title: "leaf b", ParentID: task.ID, Dependencies: feedsInto("a")},
				{ID: "c", Title: "leaf c", ParentID: task.ID, Dependencies: feedsInto("b")},
			})
			if err != nil {
				return run.Report{}, err
			}
			if _, err := store.Wait(task.ID, task.ID); err != nil {
				return run.Report{}, err
			}
			return run.Report{Waiting: true, Steps: 1, USD: 0.01}, nil
		}
		clauses = append(clauses, run.WakeClause(c))
		return run.Report{Result: "integrated three leaves", Steps: 2, USD: 0.05}, nil
	}

	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{}, seat.workerFor)
	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if turns != 2 {
		t.Fatalf("the root ran %d times, want twice — the split-and-park and one wake", turns)
	}
	if len(clauses) != 1 {
		t.Fatalf("wake clauses = %d, want exactly one", len(clauses))
	}
	for _, want := range []string{"a", "leaf a", "did a", "b", "leaf b", "did b", "c", "leaf c", "did c"} {
		if !strings.Contains(clauses[0], want) {
			t.Fatalf("the wake clause = %q, want it to carry %q", clauses[0], want)
		}
	}
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusDone || root.Result != "integrated three leaves" {
		t.Fatalf("root = %s with %q, want done with the woken root's own result", root.Status, root.Result)
	}
}

// TestSupervisorDoesNotWakeAParkedRootOnAChildBeingClaimed: the root parks on
// two children; the first lands, which makes the second ready and its worker
// claims it. A CLAIM IS NOT A REASON — the root is run again only when the
// second one lands, so it runs twice and not three times.
func TestSupervisorDoesNotWakeAParkedRootOnAChildBeingClaimed(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()

	var (
		mu      sync.Mutex
		turns   int
		clauses []string
	)
	seat.actions["root"] = func(c context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		turns++
		if turns == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "first", Title: "the first leaf", ParentID: task.ID},
				{ID: "second", Title: "the second leaf", ParentID: task.ID, Dependencies: feedsInto("first")},
			})
			if err != nil {
				return run.Report{}, err
			}
			if _, err := store.Wait(task.ID, task.ID); err != nil {
				return run.Report{}, err
			}
			return run.Report{Waiting: true, Steps: 1, USD: 0.01}, nil
		}
		clauses = append(clauses, run.WakeClause(c))
		return run.Report{Result: "integrated both leaves", Steps: 2, USD: 0.05}, nil
	}

	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{}, seat.workerFor)
	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if turns != 2 {
		t.Fatalf("the root ran %d times, want twice — a claim of the second child must not wake it", turns)
	}
	if len(clauses) != 1 || !strings.Contains(clauses[0], "first") || !strings.Contains(clauses[0], "second") {
		t.Fatalf("the wake clause = %v, want one clause naming both children", clauses)
	}
}

// TestSupervisorWakesAParkedRootAtOnceWhenAChildFails: a child that ends failed
// while its siblings are still open wakes the parked root AT ONCE — not when the
// siblings land — and the clause names that child's failure. The two siblings
// are held open by a gate the test releases only after the wake, so the wake
// genuinely happens while they are open.
func TestSupervisorWakesAParkedRootAtOnceWhenAChildFails(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()

	gateA := make(chan struct{})
	gateC := make(chan struct{})

	var (
		mu    sync.Mutex
		turns int
	)
	woke := make(chan string, 4)
	seat.actions["root"] = func(c context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		turns++
		if turns == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "a", Title: "leaf a", ParentID: task.ID},
				{ID: "b", Title: "leaf b", ParentID: task.ID},
				{ID: "c", Title: "leaf c", ParentID: task.ID},
			})
			if err != nil {
				return run.Report{}, err
			}
			if _, err := store.Wait(task.ID, task.ID); err != nil {
				return run.Report{}, err
			}
			return run.Report{Waiting: true, Steps: 1, USD: 0.01}, nil
		}
		woke <- run.WakeClause(c)
		// Its siblings are still open, so it parks again and waits for them.
		if _, err := store.Wait(task.ID, task.ID); err != nil {
			return run.Report{}, err
		}
		return run.Report{Waiting: true, Steps: 1, USD: 0.01}, nil
	}
	seat.actions["a"] = func(context.Context, plandb.Task) (run.Report, error) {
		<-gateA
		return run.Report{Result: "did a", Steps: 1, USD: 0.01}, nil
	}
	seat.actions["c"] = func(context.Context, plandb.Task) (run.Report, error) {
		<-gateC
		return run.Report{Result: "did c", Steps: 1, USD: 0.01}, nil
	}
	seat.actions["b"] = func(context.Context, plandb.Task) (run.Report, error) {
		return run.Report{}, errors.New("the checkout step refused")
	}

	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{}, seat.workerFor)
	done := make(chan run.Outcome, 1)
	go func() { done <- supervisor.Run(ctx) }()

	select {
	case clause := <-woke:
		if !strings.Contains(clause, "b") || !strings.Contains(clause, "failed") {
			t.Fatalf("the failure wake clause = %q, want it naming child b's failure", clause)
		}
		if a := store.Task("a"); a == nil || isTerminal(a.Status) {
			t.Fatalf("child a = %v, want it still open when the failed child woke the root", a.Status)
		}
		if c := store.Task("c"); c == nil || isTerminal(c.Status) {
			t.Fatalf("child c = %v, want it still open when the failed child woke the root", c.Status)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("the parked root was never woken by the child that failed")
	}
	close(gateA)
	close(gateC)
	if outcome := <-done; outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q — the failed child left the tree unfinished", outcome, run.OutcomeIncomplete)
	}
}
