package run_test

// The woken-parent law's tests: a composite task — the root included — is run
// again once every child it dispatched has landed, its opening carries what
// they reported, its new report becomes its result, and the run wakes it at
// most a fixed number of times. They are scripted the way the supervisor's
// other tests are — a fake seat, a fresh store, a wall on the run — and the
// wake is seen where the supervisor hands it to the worker: the resume clause
// a real seat opens on, read from the task's context.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// usdClose compares a run's dollar figure against a scripted sum, which a
// float accumulation of tenths never lands on exactly.
func usdClose(got, want float64) bool {
	diff := got - want
	return diff < 1e-9 && diff > -1e-9
}

// TestSupervisorWakesARootWithItsChildrensResults is the law's first half: the
// root splits into two leaves, both land, and the root is run a second time
// with an opening that carries both leaves' titles, statuses and results. The
// report it gives then — not its first turn's account of the split — is the
// run's Result, and the wake counts as a worker in the run's figures.
func TestSupervisorWakesARootWithItsChildrensResults(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	calls := 0
	wakeClause := ""
	seat.actions["root"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "l1", Title: "the first leaf", ParentID: task.ID},
				{ID: "l2", Title: "the second leaf", ParentID: task.ID},
			})
			return run.Report{Result: "split into two leaves", Steps: 1, USD: 0.10}, err
		}
		wakeClause = run.WakeClause(ctx)
		return run.Report{Result: "integrated both leaves", Steps: 2, USD: 0.05}, nil
	}
	seat.actions["l1"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		return run.Report{Result: "l1 built the parser", Steps: 3, USD: 0.20}, nil
	}
	seat.actions["l2"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		return run.Report{Result: "l2 wrote the tests", Steps: 4, USD: 0.30}, nil
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "split the work and integrate it",
		Slots:     3,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone || summary.Outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if calls != 2 {
		t.Fatalf("root ran %d times, want twice — split, then woken to integrate", calls)
	}
	// THE OPENING THE WOKEN ROOT READS CARRIES BOTH CHILDREN, VERBATIM.
	for _, want := range []string{
		"l1", "the first leaf", "l1 built the parser",
		"l2", "the second leaf", "l2 wrote the tests",
		"Integrate", "verify the combined result", "add more children", "report",
	} {
		if !strings.Contains(wakeClause, want) {
			t.Fatalf("wake clause = %q, want it to carry %q", wakeClause, want)
		}
	}
	// THE RUN'S RESULT IS THE ROOT'S SECOND REPORT.
	if summary.Result != "integrated both leaves" {
		t.Fatalf("summary result = %q, want the woken root's report", summary.Result)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusDone || root.Result != "integrated both leaves" {
		t.Fatalf("root = %s with result %q, want done with the woken root's report", root.Status, root.Result)
	}
	// THE COUNTERS COUNT THE WAKE: the root, its two leaves, and the root again.
	if summary.Nodes != 4 {
		t.Fatalf("summary nodes = %d, want the root, its two leaves and the wake", summary.Nodes)
	}
	if !usdClose(summary.USD, 0.65) {
		t.Fatalf("summary usd = %v, want every worker's spend, the wake included", summary.USD)
	}
	if summary.Steps != 10 {
		t.Fatalf("summary steps = %d, want every worker's steps, the wake included", summary.Steps)
	}
}

// TestSupervisorWakesARootAgainWhenItAddsAChild is the law's second half: a
// root woken once that adds a further child waits again, and is woken again
// with the new child's report once it lands. The second wake's opening carries
// the child the first wake did not know about.
func TestSupervisorWakesARootAgainWhenItAddsAChild(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	calls := 0
	clauses := []string{}
	seat.actions["root"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch calls {
		case 1:
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "l1", Title: "leaf one", ParentID: task.ID},
				{ID: "l2", Title: "leaf two", ParentID: task.ID},
			})
			return run.Report{Result: "split into two", Steps: 1}, err
		case 2:
			clauses = append(clauses, run.WakeClause(ctx))
			_, err := store.AddMany([]plandb.TaskSpec{{ID: "l3", Title: "the gap leaf", ParentID: task.ID}})
			return run.Report{Result: "integrated two, added a third", Steps: 1}, err
		default:
			clauses = append(clauses, run.WakeClause(ctx))
			return run.Report{Result: "integrated all three", Steps: 1}, nil
		}
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "split the work and integrate it",
		Slots:     4,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if calls != 3 {
		t.Fatalf("root ran %d times, want three — split, a wake that added a child, and a wake for it", calls)
	}
	if summary.Result != "integrated all three" {
		t.Fatalf("summary result = %q, want the last wake's report", summary.Result)
	}
	if len(clauses) != 2 {
		t.Fatalf("wakes = %d, want two", len(clauses))
	}
	if strings.Contains(clauses[0], "the gap leaf") {
		t.Fatalf("first wake clause = %q, want it to know nothing of the child added after it", clauses[0])
	}
	if !strings.Contains(clauses[1], "l3") || !strings.Contains(clauses[1], "the gap leaf") || !strings.Contains(clauses[1], "did l3") {
		t.Fatalf("second wake clause = %q, want the child the first wake added and what it did", clauses[1])
	}
}

// TestSupervisorStopsWakingAParentAtTheCap holds the cap: a root that adds a
// fresh child on every turn would be woken forever, so the run wakes it a fixed
// number of times and then completes it with the last report it gave.
func TestSupervisorStopsWakingAParentAtTheCap(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	calls := 0
	seat.actions["root"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		// One more leaf every turn: without a cap the root would never stop.
		id := fmt.Sprintf("l%d", calls)
		_, err := store.AddMany([]plandb.TaskSpec{{ID: id, Title: "leaf " + id, ParentID: task.ID}})
		return run.Report{Result: fmt.Sprintf("call %d", calls), Steps: 1}, err
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "keep adding leaves",
		Slots:     8,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	// The first turn plus four wakes, and no fifth: the cap is what stops a
	// parent that keeps spawning from looping.
	if calls != 5 {
		t.Fatalf("root ran %d times, want the first turn and four wakes at the cap", calls)
	}
	if summary.Result != "call 5" {
		t.Fatalf("summary result = %q, want the report the last capped run gave", summary.Result)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusDone {
		t.Fatalf("root status = %s, want done once the cap closed it", root.Status)
	}
}

// waitLanded blocks until every task named has landed done, and fails the test
// on the way to its wall. It is how a test makes the ordinary shape of a split
// deterministic: children go out into free slots and are shorter than the turn
// that dispatched them, so they are terminal BEFORE their parent's own worker
// returns — the case a wait in absorb has to answer without asking whether any
// child is still open.
func waitLanded(t *testing.T, store *plandb.Store, ids ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		landed := true
		for _, id := range ids {
			if task := store.Task(id); task == nil || task.Status != plandb.StatusDone {
				landed = false
				break
			}
		}
		if landed {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("tasks %v never landed", ids)
}

// TestSupervisorWakesACoordinatorBelowTheRootAgainWhenItAddsAChild is the law
// under the root: a coordinator of its own — a leaf that split, which is what a
// planning seat below the root is — is woken with what its children reported,
// the report it gives then is its result, and a wake that adds a child waits
// again and is woken again for it. THE STORE HAS TO LEAVE THE PARENT OPEN FOR
// BOTH GENERATIONS: a composite its own worker holds is not the store's to
// close, so the second wake can add a child at all instead of meeting
// AddMany's "terminal parent" refusal.
func TestSupervisorWakesACoordinatorBelowTheRootAgainWhenItAddsAChild(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	rootCalls, midCalls := 0, 0
	clauses := []string{}
	seat.actions["root"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		rootCalls++
		if rootCalls == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{{ID: "m", Title: "the coordinator", ParentID: task.ID}})
			return run.Report{Result: "split into a coordinator", Steps: 1}, err
		}
		return run.Report{Result: "root integrated: " + store.Task("m").Result, Steps: 1}, nil
	}
	seat.actions["m"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		midCalls++
		switch midCalls {
		case 1:
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "c1", Title: "child one", ParentID: task.ID},
				{ID: "c2", Title: "child two", ParentID: task.ID},
			})
			return run.Report{Result: "the coordinator split", Steps: 1}, err
		case 2:
			// THE FIRST WAKE OPENS ON BOTH CHILDREN, and adds the child their
			// reports showed up as missing — which the store only lets it do
			// because it left the parent open.
			clauses = append(clauses, run.WakeClause(ctx))
			_, err := store.AddMany([]plandb.TaskSpec{{ID: "c3", Title: "the gap child", ParentID: task.ID}})
			if err != nil {
				t.Errorf("a woken coordinator could not add a child: %v", err)
			}
			return run.Report{Result: "integrated two, added a third", Steps: 1}, err
		default:
			clauses = append(clauses, run.WakeClause(ctx))
			return run.Report{Result: "integrated all three", Steps: 1}, nil
		}
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "split twice over",
		Slots:     4,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if midCalls != 3 {
		t.Fatalf("the coordinator ran %d times, want three — a split and two wakes", midCalls)
	}
	for _, want := range []string{"c1", "child one", "did c1", "c2", "child two", "did c2", "Integrate"} {
		if !strings.Contains(clauses[0], want) {
			t.Fatalf("first wake clause = %q, want it to carry %q", clauses[0], want)
		}
	}
	if strings.Contains(clauses[0], "the gap child") {
		t.Fatalf("first wake clause = %q, want it to know nothing of the child added after it", clauses[0])
	}
	if !strings.Contains(clauses[1], "c3") || !strings.Contains(clauses[1], "did c3") {
		t.Fatalf("second wake clause = %q, want the child the first wake added and what it did", clauses[1])
	}
	// THE COORDINATOR'S RESULT IS ITS LAST REPORT, and the root's wake reads it.
	if mid := store.Task("m"); mid.Status != plandb.StatusDone || mid.Result != "integrated all three" {
		t.Fatalf("the coordinator is %s with result %q, want done with its last wake's report", mid.Status, mid.Result)
	}
	if summary.Result != "root integrated: integrated all three" {
		t.Fatalf("summary result = %q, want the root's wake reading the coordinator's result", summary.Result)
	}
}

// TestSupervisorWakesAParentWhoseChildrenLandedFirst is the race the law lives
// for. Children are dispatched into free slots and land when they land, so a
// coordinator whose children are all done BEFORE its own turn ends still owes
// itself a wake: their landings are not in the report it is writing, and a wait
// that asked only whether any child was still open would make its first word
// its result — the parent's own version of the root's bug, with nobody having
// integrated anything.
func TestSupervisorWakesAParentWhoseChildrenLandedFirst(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	rootCalls, midCalls := 0, 0
	clause := ""
	seat.actions["root"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		rootCalls++
		if rootCalls == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{{ID: "m", Title: "the coordinator", ParentID: task.ID}})
			return run.Report{Result: "split into a coordinator", Steps: 1}, err
		}
		return run.Report{Result: "root integrated: " + store.Task("m").Result, Steps: 1}, nil
	}
	seat.actions["m"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		midCalls++
		if midCalls == 1 {
			_, err := store.AddMany([]plandb.TaskSpec{
				{ID: "c1", Title: "child one", ParentID: task.ID},
				{ID: "c2", Title: "child two", ParentID: task.ID},
			})
			if err != nil {
				return run.Report{}, err
			}
			// THE COORDINATOR KEEPS WORKING while its children run, and they
			// finish first: not one of them is open when this turn ends.
			waitLanded(t, store, "c1", "c2")
			return run.Report{Result: "a first report, written before it saw anything", Steps: 1}, nil
		}
		clause = run.WakeClause(ctx)
		return run.Report{Result: "integrated both children", Steps: 1}, nil
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "split twice over",
		Slots:     4,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if midCalls != 2 {
		t.Fatalf("the coordinator ran %d times, want twice — its children landing first is not an ending", midCalls)
	}
	for _, want := range []string{"c1", "did c1", "c2", "did c2", "Integrate"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("wake clause = %q, want it to carry %q", clause, want)
		}
	}
	if mid := store.Task("m"); mid.Result != "integrated both children" {
		t.Fatalf("the coordinator's result = %q, want its woken report and not its first word", mid.Result)
	}
	if summary.Result != "root integrated: integrated both children" {
		t.Fatalf("summary result = %q, want the run to carry the integrated report", summary.Result)
	}
}

// TestSupervisorKeepsACappedParentsOwnReport holds the cap's other half. The cap
// stops the run from waking a parent that keeps spawning, and what it writes then
// is the store's own auto-completion — but a parent the run holds open has NO
// RESULT IN THE STORE, because every return of its worker was a wait that wrote
// nothing. So the ending carries the report the parent last gave: the cap is the
// run's decision to stop waking it, not a reason to throw away what it said.
func TestSupervisorKeepsACappedParentsOwnReport(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()

	var mu sync.Mutex
	midCalls := 0
	seat.actions["root"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		if store.Task("m") == nil {
			_, err := store.AddMany([]plandb.TaskSpec{{ID: "m", Title: "the coordinator", ParentID: task.ID}})
			return run.Report{Result: "split into a coordinator", Steps: 1}, err
		}
		return run.Report{Result: "root integrated: " + store.Task("m").Result, Steps: 1}, nil
	}
	seat.actions["m"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		mu.Lock()
		defer mu.Unlock()
		midCalls++
		// One more child every turn: without the cap this coordinator would
		// never stop being woken.
		id := fmt.Sprintf("c%d", midCalls)
		_, err := store.AddMany([]plandb.TaskSpec{{ID: id, Title: "child " + id, ParentID: task.ID}})
		return run.Report{Result: "the coordinator's report " + fmt.Sprint(midCalls), Steps: 1}, err
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "keep adding children",
		Slots:     8,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	// The first turn plus four wakes, and no fifth.
	if midCalls != 5 {
		t.Fatalf("the coordinator ran %d times, want the first turn and four wakes at the cap", midCalls)
	}
	mid := store.Task("m")
	if mid.Status != plandb.StatusDone {
		t.Fatalf("the coordinator's status = %s, want done once the cap closed it", mid.Status)
	}
	if mid.Result != "the coordinator's report 5" {
		t.Fatalf("the coordinator's result = %q, want the last report it gave and not an empty one", mid.Result)
	}
	if summary.Result != "root integrated: the coordinator's report 5" {
		t.Fatalf("summary result = %q, want the capped parent's own last report carried up to the run", summary.Result)
	}
}
