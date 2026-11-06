package run

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// L-LOOP2 C8a: a root worker returning after the stop cannot reopen its done
// root for review. The stopped tree still accounts for its paid receipt.
func TestStoppedRunRejectsALateRootWorkersReview(t *testing.T) {
	store := boundedRunStore(t, "Single task")
	if err := store.CompleteRoot("already finished"); err != nil {
		t.Fatal(err)
	}
	if err := store.StopRoot("stopped"); err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{ReviewRound: true}, nil)
	supervisor.absorb(workerReturn{task: *store.Task(store.RootID()), report: Report{Result: "late result", USD: .01}, err: context.Canceled})
	if len(store.Tasks()) != 1 || store.Task(store.RootID()).Status != plandb.StatusDone || store.Task(store.RootID()).Result != "already finished" || supervisor.spent != .01 {
		t.Fatalf("late return reopened or rewrote stopped work: %+v spent=%v", store.Tasks(), supervisor.spent)
	}
}

// L-LOOP2 C8, C6: unabsorbed work retained for continuation is queued once
// its worker is home; its report is still dropped and its receipt is counted.
func TestDrainedWorkerReleasesItsClaimWithoutDiscardingRetainedWork(t *testing.T) {
	store := boundedRunStore(t, "Run")
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "part", Title: "Retained work"}}); err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{}, nil)
	if _, err := store.Claim("part", "part", supervisor.Owner); err != nil {
		t.Fatal(err)
	}
	supervisor.inFlight = 1
	supervisor.finished <- workerReturn{task: *store.Task("part"), report: Report{Result: "dropped result", USD: .01}, err: context.Canceled}
	supervisor.drain()
	if task := store.Task("part"); task.Status != plandb.StatusReady || task.ClaimedBy != "" || task.Owner != "" || task.Result != "" || supervisor.spent != .01 {
		t.Fatalf("drain kept a running claim or discarded retained work: %+v spent=%v", task, supervisor.spent)
	}
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", filepath.Join(t.TempDir(), "stub-codeaf"))
	agent, err := session.NewBeltWorker(session.Config{Workspace: t.TempDir(), Model: "test/model"}, &crewCallProbe{}, store.Task("part"), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	page, ok := agent.PlanTaskPage("t-part")
	if !ok || page.Row.Status != "ready" || page.Row.StateWord() != "queued" {
		t.Fatalf("drained task's page still reads running: %+v, found=%v", page.Row, ok)
	}
	views, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range views {
		if view.ID == "t-part" {
			found = true
			if view.State != "waiting" {
				t.Fatalf("drained task's app row reads %s, want waiting", view.State)
			}
		}
	}
	if !found {
		t.Fatal("drained task disappeared from the app listing")
	}
}
