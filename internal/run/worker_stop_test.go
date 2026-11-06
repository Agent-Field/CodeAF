package run

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
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
}
