package run_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

func TestFinishOnTheCappedStepKeepsTheStoreEnding(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	id := store.RootID()
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo one"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand(id, "finished on the last step")), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 2), *store.Task(id))
	if err != nil || report.Result != "finished on the last step" {
		t.Fatalf("capped finish returned report=%+v, err=%v; want the store's done result", report, err)
	}
	if row := store.Task(id); row.Status != plandb.StatusDone {
		t.Fatalf("store = %s, want done", row.Status)
	}
	end := endLine(t, rawTrajectory(t, filepath.Dir(store.Path()), id))
	if end.Reason != "finished in the store" {
		t.Fatalf("trajectory ending = %q", end.Reason)
	}
}

func TestOpenTaskStillStopsAtItsStepCap(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo one"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo two"}`), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	_, err := worker.Run(run.WithStepsPerTask(runContext(t), 2), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "stopped at its step cap after 2 steps") {
		t.Fatalf("open task cap = %v", err)
	}
}

func TestCappedLeafFinishStillSeatsItsReview(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo one"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("leaf", "leaf finished")), nil
		},
	}}
	split := splitRoot(t, store, plandb.TaskSpec{ID: "leaf", Title: "leaf work"})
	factory := func(task plandb.Task) run.Worker {
		switch {
		case task.ID == "leaf":
			return run.NewBashWorker(store, t.TempDir(), "test/model", seat)
		case task.Role == plandb.RoleCheck:
			return funcWorker(func(context.Context, plandb.Task) (run.Report, error) {
				return run.Report{Result: "holds: checked"}, nil
			})
		default:
			return funcWorker(split)
		}
	}
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Limits: run.Limits{StepsPerTask: 2, ReviewRound: true}, Factory: factory,
	})
	if outcome != run.OutcomeDone {
		t.Fatalf("capped leaf run = %q, summary=%+v", outcome, summary)
	}
	checks := tasksWithRole(store, plandb.RoleCheck)
	if len(checks) != 1 || checks[0].Status != plandb.StatusDone {
		t.Fatalf("review after capped finish = %+v", checks)
	}
}

func TestWaitOnTheCappedStepKeepsTheStorePark(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", ParentID: store.RootID(), Title: "child"}}); err != nil {
		t.Fatal(err)
	}
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"plandb wait root --agent root"}`), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 1), *store.Task(store.RootID()))
	if err != nil || !report.Waiting {
		t.Fatalf("capped wait returned report=%+v err=%v", report, err)
	}
	if task := store.Task(store.RootID()); !task.Waiting {
		t.Fatalf("store did not park root: %+v", task)
	}
}
