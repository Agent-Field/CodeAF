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
	wantDescription := "Acceptance: acceptance: the handler returns 200\n\nResult: did l1"
	if check.Description != wantDescription {
		t.Fatalf("check description = %q, want unchanged no-check description %q", check.Description, wantDescription)
	}
	if len(check.Checks) != 0 {
		t.Fatalf("check checks = %v, want none copied from a leaf with none", check.Checks)
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

// TestSupervisorCopiesDeclaredChecksOntoTheReviewTask proves declared proof stays
// machine-readable on the review node and is also appended to its worker brief.
func TestSupervisorCopiesDeclaredChecksOntoTheReviewTask(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	leafChecks := []string{"go test ./internal/widget", "go vet ./internal/widget"}
	seat.actions["root"] = splitRoot(t, store, plandb.TaskSpec{
		ID: "l1", Title: "the leaf", Description: "the handler returns 200", Checks: leafChecks,
	})
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 {
		t.Fatalf("check tasks = %d, want one", len(checks))
	}
	check := checks[0]
	if strings.Join(check.Checks, "\n") != strings.Join(leafChecks, "\n") {
		t.Fatalf("check node checks = %v, want %v", check.Checks, leafChecks)
	}
	wantDescription := "Acceptance: the handler returns 200\n\nResult: did l1\n\nChecks:\ngo test ./internal/widget\ngo vet ./internal/widget"
	if check.Description != wantDescription {
		t.Fatalf("check description = %q, want %q", check.Description, wantDescription)
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

// tasksTitled answers every task the plan holds that carries one title. It is
// how a fix task is found by the title the round mints it with.
func tasksTitled(store *plandb.Store, title string) []*plandb.Task {
	var out []*plandb.Task
	for _, task := range store.Tasks() {
		if task.Title == title {
			out = append(out, task)
		}
	}
	return out
}

// TestSupervisorChecksALeafThatCompletedItselfInTheStore proves the round fires
// on the ending real workers write. A leaf whose own `plandb done` already
// landed in the store is caught by the supervisor's store-ended road, and the
// review must read that road too — otherwise the round only ever fires on the
// scripted endings the supervisor writes itself and never on real work.
func TestSupervisorChecksALeafThatCompletedItselfInTheStore(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "the leaf", Description: "acceptance: the handler returns 200"})
	seat.actions["l1"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		// The worker writes its own completion, the way every real belt worker
		// finishes: `plandb done` against the store, then the turn ends.
		if _, err := store.Done(task.ID, task.ID, "the leaf wrote its own ending", nil, nil); err != nil {
			return run.Report{}, err
		}
		return run.Report{Result: "the leaf wrote its own ending", Steps: 1}, nil
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 {
		t.Fatalf("check tasks = %d, want one for the leaf that completed itself", len(checks))
	}
	if checks[0].ParentID != store.RootID() {
		t.Fatalf("check parent = %q, want the leaf's parent %q", checks[0].ParentID, store.RootID())
	}
	if checks[0].Status != plandb.StatusDone {
		t.Fatalf("check status = %s, want the check to have run and landed", checks[0].Status)
	}
}

// TestSupervisorTurnsADoesNotHoldFindingIntoAFixTask proves A FINDING IS WORK,
// NOT A REMARK: a check that does not hold leaves its note AND adds one `fix:`
// task under the checked leaf's parent, carrying the leaf's acceptance, the
// finding and the leaf's result, with no dependency so it is ready at once — and
// the run waits on it before the root completes.
func TestSupervisorTurnsADoesNotHoldFindingIntoAFixTask(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "the leaf", Description: "acceptance: the handler returns 200", Checks: []string{"go test ./internal/widget", "go vet ./internal/widget"}})
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
	fixes := tasksTitled(store, "fix: the leaf")
	if len(fixes) != 1 {
		t.Fatalf("fix tasks = %d, want exactly one for the finding", len(fixes))
	}
	fix := fixes[0]
	if fix.ParentID != store.RootID() {
		t.Fatalf("fix parent = %q, want the checked leaf's parent %q", fix.ParentID, store.RootID())
	}
	if fix.Role != plandb.RoleWork {
		t.Fatalf("fix role = %q, want the work seat", fix.Role)
	}
	if len(fix.Dependencies) != 0 {
		t.Fatalf("fix dependencies = %v, want none so it is ready at once", fix.Dependencies)
	}
	wantChecks := []string{"go test ./internal/widget", "go vet ./internal/widget"}
	if strings.Join(fix.Checks, "\n") != strings.Join(wantChecks, "\n") {
		t.Fatalf("fix checks = %v, want inherited %v", fix.Checks, wantChecks)
	}
	for _, want := range []string{"acceptance: the handler returns 200", "the handler still returns 500 under load.", "did l1"} {
		if !strings.Contains(fix.Description, want) {
			t.Fatalf("fix description = %q, want it to carry %q", fix.Description, want)
		}
	}
	// THE RUN WAITED ON THE FIX: it ended done, and the root completed no
	// earlier than the fix did.
	if fix.Status != plandb.StatusDone {
		t.Fatalf("fix status = %s, want the run to have waited on it", fix.Status)
	}
	root := store.Task(store.RootID())
	if root.CompletedAt.Before(fix.CompletedAt) {
		t.Fatalf("root completed at %v, before the fix at %s", root.CompletedAt, fix.CompletedAt)
	}
}

// TestSupervisorDoesNotAddASecondFixTask proves the round cannot loop: the fix
// task the finding made is checked in turn, that check does not hold too, and the
// second finding is left as a note on the fix task with no second fix behind it.
func TestSupervisorDoesNotAddASecondFixTask(t *testing.T) {
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
	fixes := tasksTitled(store, "fix: leaf l1")
	if len(fixes) != 1 {
		t.Fatalf("fix tasks = %d, want exactly one however many checks did not hold", len(fixes))
	}
	// The fix task's own check ran and did not hold; its finding is a note and no
	// second fix task, which is what keeps a run from looping.
	if checks := tasksWithRole(store, plandb.RoleCheck); len(checks) != 2 {
		t.Fatalf("check tasks = %d, want the leaf's check and the fix's own check", len(checks))
	}
	if notes := store.Notes(fixes[0].ID, 10); len(notes) != 1 {
		t.Fatalf("fix notes = %d, want the second finding left as a note on the fix", len(notes))
	}
}

// TestSupervisorChecksAChildlessRootBeforeCompletion proves the childless root is
// a leaf: it gets its own check UNDER ITSELF, and the root's completion waits on
// it the way it waits on any child — the root does not complete before the check
// lands.
func TestSupervisorChecksAChildlessRootBeforeCompletion(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = leafRoot
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 {
		t.Fatalf("check tasks = %d, want the one check for the childless root", len(checks))
	}
	check := checks[0]
	if check.ParentID != store.RootID() {
		t.Fatalf("check parent = %q, want it under the root itself %q", check.ParentID, store.RootID())
	}
	if check.Title != "check: The run" {
		t.Fatalf("check title = %q, want the root's title behind a check prefix", check.Title)
	}
	if !seat.launched(check.ID) || check.Status != plandb.StatusDone {
		t.Fatalf("the check did not run and land: launched=%v status=%s", seat.launched(check.ID), check.Status)
	}
	root := store.Task(store.RootID())
	if root.CompletedAt.Before(check.CompletedAt) {
		t.Fatalf("root completed at %v, before its check at %s", root.CompletedAt, check.CompletedAt)
	}
}

// TestSupervisorAddsNoCheckWithTheReviewRoundOff proves the default: with the
func rootDoneAction(store *plandb.Store, result string) func(context.Context, plandb.Task) (run.Report, error) {
	return func(_ context.Context, task plandb.Task) (run.Report, error) {
		if _, err := store.Done(task.ID, task.ID, result, nil, nil); err != nil {
			return run.Report{}, err
		}
		return run.Report{Result: result, Steps: 1}, nil
	}
}

func TestSupervisorChecksAChildlessRootThatCompletedItselfInTheStore(t *testing.T) {
	store := runOpenStore(t)
	seat := newFakeSeat()
	const result = "the root wrote its own ending"
	seat.actions["root"] = rootDoneAction(store, result)
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)
	if outcome := supervisor.Run(runContext(t)); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 {
		t.Fatalf("check tasks = %d, want exactly one for the root that completed itself", len(checks))
	}
	check := checks[0]
	if check.ParentID != store.RootID() || !seat.launched(check.ID) || check.Status != plandb.StatusDone {
		t.Fatalf("check = parent %q launched %v status %s, want one landed under root", check.ParentID, seat.launched(check.ID), check.Status)
	}
	if got := store.Task(store.RootID()).Result; got != result {
		t.Fatalf("root result = %q, want worker result %q", got, result)
	}
}

func TestSupervisorTurnsARootsDoesNotHoldFindingIntoAFix(t *testing.T) {
	store := runOpenStore(t)
	seat := newFakeSeat()
	seat.actions["root"] = rootDoneAction(store, "the root wrote its own ending")
	factory := func(task plandb.Task) run.Worker {
		if task.Role == plandb.RoleCheck {
			return funcWorker(func(context.Context, plandb.Task) (run.Report, error) {
				return run.Report{Result: "does not hold: the hidden case fails", Steps: 1}, nil
			})
		}
		return seat.workerFor(task)
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, factory)
	if outcome := supervisor.Run(runContext(t)); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	fixes := tasksTitled(store, "fix: The run")
	if len(fixes) != 1 {
		t.Fatalf("fix tasks = %d, want exactly one under the root", len(fixes))
	}
	fix := fixes[0]
	if fix.ParentID != store.RootID() || fix.Status != plandb.StatusDone || !seat.launched(fix.ID) {
		t.Fatalf("fix = parent %q launched %v status %s, want one landed under root", fix.ParentID, seat.launched(fix.ID), fix.Status)
	}
	if root := store.Task(store.RootID()); root.CompletedAt.Before(fix.CompletedAt) {
		t.Fatalf("root completed at %v, before fix at %v", root.CompletedAt, fix.CompletedAt)
	}
}

func TestSupervisorDoesNotCheckARootWithChildrenThatCompletedItself(t *testing.T) {
	store := runOpenStore(t)
	seat := newFakeSeat()
	turn := 0
	seat.actions["root"] = func(_ context.Context, task plandb.Task) (run.Report, error) {
		turn++
		if turn == 1 {
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "l1", Title: "the leaf", ParentID: task.ID}}); err != nil {
				return run.Report{}, err
			}
			return run.Report{Result: "split", Steps: 1}, nil
		}
		return rootDoneAction(store, "the coordinator integrated its child")(context.Background(), task)
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{ReviewRound: true}, seat.workerFor)
	if outcome := supervisor.Run(runContext(t)); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 || checks[0].Title == "check: The run" {
		t.Fatalf("checks = %#v, want only the child leaf's check and none for the root", checks)
	}
}

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
	if launches := seat.launches(); len(launches) != 3 {
		t.Fatalf("launches = %v, want the root, its one leaf, and the root's wake alone", launches)
	}
}
