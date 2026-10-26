package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A computer running its Jobs at once takes a job a person said to start now,
// ONE slot above its number, and still no other job; the pass is spent once taken.
func TestAFullComputerTakesOnlyAJobStartedNow(t *testing.T) {
	f := setup(t)
	f.add(t, factory.Item{Assignee: "sam"})
	waiting := f.add(t, factory.Item{Assignee: "sam"})
	now := f.add(t, factory.Item{Assignee: "sam", OverLimit: true})
	slot := func(id string) lease.Worker {
		return lease.Worker{ID: id, Machine: "mbp", Handle: "sam", Slots: 1}
	}
	if _, err := f.desk.Pull(slot("a")); err != nil {
		t.Fatal(err)
	}
	if n, err := f.desk.OverLimit(slot("b")); err != nil || n != 1 {
		t.Fatalf("over limit = %d, %v; want 1", n, err)
	}
	held, err := f.desk.Pull(slot("b"))
	if err != nil || held.Job != now.ID {
		t.Fatalf("full computer pulled job %d, %v; want the job started now, %d", held.Job, err, now.ID)
	}
	it, err := f.store.Get(now.ID)
	if err != nil || it.OverLimit || it.State != factory.StateRunning {
		t.Fatalf("started-now job after its pull: over=%v state=%s err=%v", it.OverLimit, it.State, err)
	}
	if n, err := f.desk.OverLimit(slot("c")); err != nil || n != 0 {
		t.Fatalf("over limit after the pull = %d, %v; want 0", n, err)
	}
	if _, err := f.desk.Pull(slot("c")); !errors.Is(err, lease.ErrWorkerBusy) {
		t.Fatalf("a third slot pulled past Jobs at once: %v", err)
	}
	f.state(t, waiting.ID, factory.StateQueued)
}

// Hold keeps a queued job that no worker holds where it is: no worker takes
// it, and Resume puts it back in the queue to be taken.
func TestAHeldQueuedJobIsNotTakenUntilResumed(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam", OverLimit: true})
	doors := lease.NewDoors(f.desk)
	if err := doors.Hold(it.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Get(it.ID)
	if err != nil || got.State != factory.StateQueued || got.Stream == nil || !got.Stream.Paused || got.OverLimit {
		t.Fatalf("held job: state=%s stream=%+v over=%v err=%v", got.State, got.Stream, got.OverLimit, err)
	}
	if _, err := f.desk.Pull(worker("laptop", "sam")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("a worker took a held job: %v", err)
	}
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatal(err)
	}
	if held, err := f.desk.Pull(worker("laptop", "sam")); err != nil || held.Job != it.ID {
		t.Fatalf("resumed job not taken: %+v, %v", held, err)
	}
}
