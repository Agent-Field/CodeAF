package run_test

// THE THREE ENDINGS A TASK HAS, AND THE ONE IT DOES NOT.
//
// A run worker's loop no longer ends on a reply: a reply with no tool call is
// something the worker says, not something the task does, and the harness
// answers it in its own voice and runs the loop again. Only `plandb done` on
// its own id ends a task with a result, `plandb wait` parks it for a wake, the
// step cap and the run's wall end it unfinished, and an errored turn ends it
// with its error. These tests pin the first two and the reply that is neither.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestBashWorkerATextOnlyReplyLoopsAndFourInARowFail: a reply that executed no
// action does not end the task. The harness answers it in its own voice and the
// loop runs again — and the fourth reply in a row with no action fails the
// task, the same cap the belt's invalid-action rejections use.
func TestBashWorkerATextOnlyReplyLoopsAndFourInARowFail(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("Now writing the taint engine:"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textReply("still writing it"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textReply("nearly there"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("I am blocked on my dependency"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	_, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID()))

	if err == nil {
		t.Fatal("a run of four text-only replies finished, want the task failed on the fourth")
	}
	if !strings.Contains(err.Error(), "carried no action") {
		t.Fatalf("the failure = %q, want the no-action reason", err)
	}
	// THE HARNESS'S OWN VOICE WENT OUT between the replies: the second request
	// carries the note the loop appends.
	seat.mu.Lock()
	requests := append([][]ai.Message(nil), seat.requests...)
	seat.mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("the provider answered %d calls, want the four the loop made before it failed", len(requests))
	}
	second := requests[1]
	if !strings.Contains(messageContent(second[len(second)-1]), "no action executed") {
		t.Fatalf("the second request's last message is %q, want the harness's no-action note", messageContent(second[len(second)-1]))
	}
	// AND THE STORE IS NOT TOUCHED: a task that never finished keeps no result.
	if task := store.Task(store.RootID()); task.Status == plandb.StatusDone {
		t.Fatalf("the root was marked %s, want it left open — words are not a completion", task.Status)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	end := endLine(t, lines)
	if !strings.Contains(end.Reason, "carried no action") {
		t.Fatalf("the trajectory's ending reason = %q, want the no-action reason", end.Reason)
	}
}

// TestBashWorkerDoneEndsTheLoopWithItsResult: `plandb done` on the worker's own
// id is the end of the loop, and the result it carried is the task's result.
func TestBashWorkerDoneEndsTheLoopWithItsResult(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	const answer = "the taint engine is written and tested"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", answer)), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID()))

	if err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
	if report.Result != answer {
		t.Fatalf("report result = %q, want the result the finish command wrote", report.Result)
	}
	if task := store.Task(store.RootID()); task.Status != plandb.StatusDone || task.Result != answer {
		t.Fatalf("the store's root = %s with %q, want done with the same result", task.Status, task.Result)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if end := endLine(t, lines); end.Result != answer || end.Reason != "finished in the store" {
		t.Fatalf("the ending = %q with reason %q, want the store's completion", end.Result, end.Reason)
	}
}

// TestSupervisorParksAWaitingTaskAndWakesItWhenASiblingLands: `plandb wait`
// leaves the task open and released, and the run launches it again — with the
// change in its clause — once the sibling it waited on lands.
func TestSupervisorParksAWaitingTaskAndWakesItWhenASiblingLands(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	// The root splits into two leaves. B waits on A through a `suggests`
	// dependency, which does not block readiness — so B's worker starts while A
	// is still open, finds something to wait on, and parks.
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "a", Title: "the dependency"},
		plandb.TaskSpec{ID: "b", Title: "the dependent", Dependencies: []plandb.Dependency{{TaskID: "a", Kind: plandb.DepSuggests}}},
	)
	// A holds its seat briefly, so B's worker parks while A is still open.
	seat.actions["a"] = holdSeat(60 * time.Millisecond)

	var (
		bRuns   int
		bClause string
	)
	seat.actions["b"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		bRuns++
		if bRuns == 1 {
			if _, err := store.Wait(task.ID, task.ID); err != nil {
				return run.Report{}, err
			}
			return run.Report{Waiting: true, Steps: 1, USD: 0.01}, nil
		}
		bClause = run.WakeClause(ctx)
		return run.Report{Result: "finished after the wake", Steps: 1, USD: 0.02}, nil
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	// B RAN TWICE: once to park, once woken.
	launched := 0
	for _, id := range seat.launches() {
		if id == "b" {
			launched++
		}
	}
	if launched != 2 {
		t.Fatalf("task b launched %d times, want the park and the wake", launched)
	}
	if bRuns != 2 {
		t.Fatalf("task b's worker ran %d times, want the park and the wake", bRuns)
	}
	if !strings.Contains(bClause, "a") || !strings.Contains(bClause, "the dependency") {
		t.Fatalf("the wake clause = %q, want it naming the sibling that moved", bClause)
	}
	b := store.Task("b")
	if b.Status != plandb.StatusDone || b.Result != "finished after the wake" {
		t.Fatalf("task b = %s with %q, want done with its woken result", b.Status, b.Result)
	}
	if b.Waiting {
		t.Fatalf("task b is still parked, want the wake to have cleared it")
	}
	// THE RUN'S RESULT IS THE ROOT'S OWN REPORT after the leaves landed.
	if root := store.Task(store.RootID()); root.Result != "integrated 2 leaves" {
		t.Fatalf("root result = %q, want the report the woken root gave", root.Result)
	}
}

// TestStartAnswersTheRootsDoneResult: the run's Result is the root's `done`
// result, read through the door a caller actually uses ([run.Start]). The root
// worker finishes its own task with `plandb done root` — the one rule c174
// changed — and the run's summary carries the result that command wrote.
func TestStartAnswersTheRootsDoneResult(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	const answer = "the run is done, and this is its result"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", answer)), nil
		},
	}}
	factory := func(task plandb.Task) run.Worker {
		return run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "finish yourself through the plan CLI",
		Slots:     1,
		Factory:   factory,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if summary.Result != answer {
		t.Fatalf("summary result = %q, want the root's done result %q", summary.Result, answer)
	}
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusDone || root.Result != answer {
		t.Fatalf("root = %s with %q, want done with the result its worker wrote", root.Status, root.Result)
	}
}

// lateWorker comes home after the supervisor has had time to look at the store:
// its task is written, and its return is still on the way.
type lateWorker struct {
	inner run.Worker
	late  time.Duration
}

func (w lateWorker) Run(ctx context.Context, task plandb.Task) (run.Report, error) {
	report, err := w.inner.Run(ctx, task)
	time.Sleep(w.late)
	return report, err
}

// TestStartAnswersTheRootsDoneResultWhenItsWorkerReturnsLate: THE STORE IS THE
// RECORD OF A RUN'S RESULT, AND THE WORKER'S RETURN IS ONLY ITS ECHO. The root's
// worker writes `plandb done root` and the store reads done at once; a pass on
// the supervisor's clock sees a finished run and ends it, and the worker's own
// return (the only place the result used to be read from) has not arrived. On
// a loaded box that lost the result about once in fifteen runs of the test
// above. The return is held back here for longer than a pass, so the late
// return is every run and not an unlucky one.
func TestStartAnswersTheRootsDoneResultWhenItsWorkerReturnsLate(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	const answer = "the run is done, and this is its result"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", answer)), nil
		},
	}}
	factory := func(task plandb.Task) run.Worker {
		return lateWorker{inner: run.NewBashWorker(store, t.TempDir(), "test/model", seat), late: 750 * time.Millisecond}
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "finish yourself through the plan CLI",
		Slots:     1,
		Factory:   factory,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if summary.Result != answer {
		t.Fatalf("summary result = %q, want the result the store holds %q", summary.Result, answer)
	}
}
