package run_test

// The review round's tests: a work-seat leaf that lands done spawns one check
// task under its parent, a check never spawns another, and a check whose result
// does not hold leaves its sentence as a note on the leaf it read. They are
// scripted the way the supervisor's other tests are — a fake seat, a fresh
// store, a wall on the run — and the check worker is scripted by its seat word,
// because the check task's id is minted by the supervisor and the test never
// knows it in advance.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// funcWorker is a worker built from a function, for a test that scripts one
// seat — the check — without the recording the fakeSeat does.
type funcWorker func(context.Context, plandb.Task) (run.Report, error)

func (f funcWorker) Run(ctx context.Context, task plandb.Task) (run.Report, error) {
	return f(ctx, task)
}

// tasksWithRole answers every task the plan holds that carries one seat word.
// It reads the stored role, which is how a check task is told apart from the
// leaves whose completions it reviews.
func tasksWithRole(store *plandb.Store, role string) []*plandb.Task {
	var out []*plandb.Task
	for _, task := range store.Tasks() {
		if task.Role == role {
			out = append(out, task)
		}
	}
	return out
}

// TestSupervisorAddsOneCheckForAFinishedLeafUnderTheReviewRound proves the
// round's first half: with the review round on, a leaf's done adds exactly one
// check task — under the leaf's parent, on the check seat, titled for the leaf
// and carrying the leaf's acceptance and result — and the root waits on it.
func TestSupervisorAddsOneCheckForAFinishedLeafUnderTheReviewRound(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "the leaf", Description: "acceptance: the handler returns 200"})
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	// The check's own done adds no further check, so the round's one check is
	// the only check-seat task the finished plan holds.
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 {
		t.Fatalf("check tasks = %d, want exactly one for the finished leaf", len(checks))
	}
	check := checks[0]
	if check.Title != "check: the leaf" {
		t.Fatalf("check title = %q, want the leaf's title behind a check prefix", check.Title)
	}
	if check.ParentID != store.RootID() {
		t.Fatalf("check parent = %q, want the leaf's parent %q", check.ParentID, store.RootID())
	}
	if len(check.Dependencies) != 0 {
		t.Fatalf("check dependencies = %v, want none so it is ready at once", check.Dependencies)
	}
	if !strings.Contains(check.Description, "acceptance: the handler returns 200") {
		t.Fatalf("check description = %q, want the leaf's acceptance", check.Description)
	}
	if !strings.Contains(check.Description, "did l1") {
		t.Fatalf("check description = %q, want the leaf's own result", check.Description)
	}
	// The check ran, and the leaf's own completion did not carry the root with
	// it: the root completed no earlier than the check.
	if !seat.launched(check.ID) {
		t.Fatalf("launches = %v, want the check launched", seat.launches())
	}
	if check.Status != plandb.StatusDone {
		t.Fatalf("check status = %s, want done", check.Status)
	}
	root := store.Task(store.RootID())
	if root.CompletedAt.Before(check.CompletedAt) {
		t.Fatalf("root completed at %v, before the check at %s", root.CompletedAt, check.CompletedAt)
	}
}

// TestSupervisorAddsNoCheckForACheckTask proves the round's second half: a task
// already on the check seat is never itself checked, so a run whose only child
// is a check finishes with that one check task alone.
func TestSupervisorAddsNoCheckForACheckTask(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "c1", Title: "check: the leaf", Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatalf("seed the check: %v", err)
	}
	seat := newFakeSeat()
	seat.actions["root"] = leafRoot
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if checks := tasksWithRole(store, plandb.RoleCheck); len(checks) != 1 {
		t.Fatalf("check tasks = %d, want the one seeded check alone", len(checks))
	}
}

// TestSupervisorLeavesADoesNotHoldFindingAsANoteOnTheLeaf proves the finding:
// a check whose result begins "does not hold" leaves its sentence as a note on
// the leaf it read, in the check's own voice, and the leaf keeps its done
// ending — nothing is reopened.
func TestSupervisorLeavesADoesNotHoldFindingAsANoteOnTheLeaf(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	finding := "does not hold: the handler still returns 500 under load."
	factory := func(task plandb.Task) run.Worker {
		if task.Role == plandb.RoleCheck {
			return funcWorker(func(context.Context, plandb.Task) (run.Report, error) {
				return run.Report{Result: finding, Steps: 2}, nil
			})
		}
		return seat.workerFor(task)
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, factory)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	leaf := store.Task("l1")
	if leaf.Status != plandb.StatusDone {
		t.Fatalf("leaf status = %s, want the done ending left in place", leaf.Status)
	}
	notes := store.Notes("l1", 10)
	if len(notes) != 1 {
		t.Fatalf("leaf notes = %d, want one finding", len(notes))
	}
	if notes[0].Agent != "check" {
		t.Fatalf("note author = %q, want check", notes[0].Agent)
	}
	if notes[0].Body != "the handler still returns 500 under load." {
		t.Fatalf("note body = %q, want the finding's sentence", notes[0].Body)
	}
}

// TestSupervisorRootWaitsOnAnOpenCheckTask proves the waiting the round relies
// on: the store's own CanFinish refuses the root while a check stands open,
// naming the check, which is the shape completeTree reads to hold the root's
// completion back. The check worker asks the question while it holds the seat.
func TestSupervisorRootWaitsOnAnOpenCheckTask(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	canFinish := make(chan bool, 1)
	reason := make(chan string, 1)
	factory := func(task plandb.Task) run.Worker {
		if task.Role == plandb.RoleCheck {
			return funcWorker(func(context.Context, plandb.Task) (run.Report, error) {
				ok, why := store.CanFinish(store.RootID())
				canFinish <- ok
				reason <- why
				return run.Report{Result: "holds: the acceptance is met", Steps: 1}, nil
			})
		}
		return seat.workerFor(task)
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, factory)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if ok := <-canFinish; ok {
		t.Fatalf("CanFinish on the root with an open check = true, want refused")
	}
	if why := <-reason; !strings.Contains(why, "has not finished") {
		t.Fatalf("CanFinish reason = %q, want the open check named", why)
	}
}

// TestSupervisorAddsNoCheckWithTheReviewRoundOff proves the default: with the
// review round unset — the zero Limits every existing caller builds — a leaf
// that lands done adds nothing, and the run is the root and its one leaf.
func TestSupervisorAddsNoCheckWithTheReviewRoundOff(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if checks := tasksWithRole(store, plandb.RoleCheck); len(checks) != 0 {
		t.Fatalf("check tasks = %d, want none with the review round off", len(checks))
	}
	if launches := seat.launches(); len(launches) != 2 {
		t.Fatalf("launches = %v, want the root and its one leaf alone", launches)
	}
}
