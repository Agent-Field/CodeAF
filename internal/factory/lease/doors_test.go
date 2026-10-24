package lease_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (f *fixture) running(t *testing.T) (factory.Item, lease.Lease) {
	t.Helper()
	it := f.add(t, factory.Item{State: factory.StateNew, Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}})
	doors := lease.NewDoors(f.desk)
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateQueued)
	held, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil {
		t.Fatal(err)
	}
	return it, held
}

// THE SIGNAL CHANNEL: a person's verb on a held job changes its state on the
// server and leaves the verb for the holder, which collects each once.
func TestDoorsSignalTheJobsHolder(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	doors := lease.NewDoors(f.desk)
	if err := doors.Hold(it.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := doors.Steer(it.ID, "use the other file"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(it.ID); got.Stream == nil || !got.Stream.Paused {
		t.Fatal("pause did not show on the job")
	}
	signals, err := f.desk.Drain(held.ID, "w1")
	want := []lease.Signal{{Verb: lease.SignalHold, Yes: true}, {Verb: lease.SignalSteer, Words: "use the other file"}}
	if err != nil || !reflect.DeepEqual(signals, want) {
		t.Fatalf("drained %+v, %v", signals, err)
	}
	if again, _ := f.desk.Drain(held.ID, "w1"); len(again) != 0 {
		t.Fatalf("a signal was handed twice: %+v", again)
	}
	if _, err := f.desk.Drain(held.ID, "w2"); !errors.Is(err, lease.ErrWorker) {
		t.Fatalf("another worker drained the job's signals: %v", err)
	}
}

// A SIGNAL REACHES ONLY A HELD JOB: a held job grows one signal, handed once,
// and a job nobody holds is not signaled at all.
func TestASignalReachesOnlyAHeldJob(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	heldNow, err := f.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalModel, Words: "m"})
	if err != nil || !heldNow {
		t.Fatalf("held job was not signaled: %v, %v", heldNow, err)
	}
	signals, err := f.desk.Drain(held.ID, "w1")
	want := []lease.Signal{{Verb: lease.SignalModel, Words: "m"}}
	if err != nil || !reflect.DeepEqual(signals, want) {
		t.Fatalf("drained %+v, %v", signals, err)
	}
	if again, _ := f.desk.Drain(held.ID, "w1"); len(again) != 0 {
		t.Fatalf("a signal was handed twice: %+v", again)
	}
	free := f.add(t, factory.Item{State: factory.StateNew})
	heldNow, err = f.desk.SignalIfHeld(free.ID, lease.Signal{Verb: lease.SignalModel, Words: "m"})
	if err != nil || heldNow {
		t.Fatalf("a job nobody holds was signaled: %v, %v", heldNow, err)
	}
}

// A STOP TAKES THE LEASE: the job is backlog at once, and the holder's next
// call learns the lease is gone, which is how it stops. No step of this job
// had reported running, so nothing was cut and it carries no stop mark.
func TestAStopReleasesTheLease(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateNew)
	if got, _ := f.store.Get(it.ID); got.Failure != nil || factory.JobState(got) != "backlog" {
		t.Fatalf("a stop that cut no step left %s %+v", factory.JobState(got), got.Failure)
	}
	if _, err := f.desk.Drain(held.ID, "w1"); !errors.Is(err, lease.ErrNoLease) {
		t.Fatalf("the stopped job's lease is still live: %v", err)
	}
	history, err := record.Open(f.store, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := history.Since(context.Background(), f.now.Add(-1))
	if err != nil || len(events) != 1 || events[0].Kind != record.Stopped || events[0].Job != it.ID {
		t.Fatalf("stop history = %+v; error = %v", events, err)
	}
}

// A RUN THAT ALREADY ENDED HAS NOTHING TO STOP: a person's stop of it is
// refused as not running and records nothing, and the reset a reassignment
// or a drop makes first leaves why it ended as it was. A start takes the stop
// down and the run begins again at its first step.
func TestAStoppedJobIsNotStoppedAgain(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{State: factory.StateRunning, Stages: []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
	}, Stream: &factory.Stream{Cur: 1, Spent: 0.5, Phases: []factory.Phase{
		{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone},
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1},
	}}})
	doors := lease.NewDoors(f.desk)
	if err := doors.Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	stopped, _ := f.store.Get(it.ID)
	if got := stopped.Failure; got == nil || got.Code != factory.CodeStopped || got.Step != "write" {
		t.Fatalf("stop = %+v", stopped.Failure)
	}
	if ph := stopped.Stream.Phases[1]; ph.State != factory.PhaseFailed || ph.Failure == nil || ph.Failure.Code != factory.CodeStopped {
		t.Fatalf("the cut step still reads %+v", ph)
	}
	if err := doors.Stop(it.ID); err == nil || !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("a second stop = %v", err)
	}
	if err := doors.Reset(it.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := f.store.Get(it.ID)
	if !reflect.DeepEqual(again.Failure, stopped.Failure) || again.State != factory.StateNew || !reflect.DeepEqual(again.Stream.Phases, stopped.Stream.Phases) {
		t.Fatalf("a reset rewrote the stopped job: %+v %+v", again.Failure, again.Stream.Phases)
	}
	history, err := record.Open(f.store, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if events, err := history.Since(context.Background(), f.now.Add(-1)); err != nil || len(events) != 1 || events[0].Kind != record.Stopped {
		t.Fatalf("stop history = %+v; error = %v", events, err)
	}
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	started, _ := f.store.Get(it.ID)
	if started.State != factory.StateQueued || started.Failure != nil || started.Stream.Spent != 0 || len(started.Stream.Phases) != 2 || started.Stream.Phases[0].State != factory.PhasePending || started.Stream.Phases[1].State != factory.PhasePending {
		t.Fatalf("start = %s %+v %+v", started.State, started.Failure, started.Stream)
	}
}

func TestStopRecordFailureKeepsTheJobAndLease(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if err := os.Mkdir(filepath.Join(f.store.Root(), "events.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).Stop(it.ID); err == nil {
		t.Fatal("stop succeeded without keeping the person's decision")
	}
	f.state(t, it.ID, factory.StateRunning)
	if _, err := f.desk.Drain(held.ID, "w1"); err != nil {
		t.Fatalf("a failed stop released the lease: %v", err)
	}
}

// A STEER OR A PAUSE NEEDS A HOLDER; a resume of a paused job nobody holds
// queues it for the next worker.
func TestVerbsOnAJobNobodyHolds(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	doors := lease.NewDoors(f.desk)
	if err := doors.Hold(it.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.desk.Apply(held.ID, "w1", true, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := doors.Steer(it.ID, "faster"); err == nil {
		t.Fatal("a steer reached a job nobody holds")
	}
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateQueued)
	if got, _ := f.store.Get(it.ID); got.Stream.Paused {
		t.Fatal("the resumed job is still paused")
	}
}

// APPROVING PUBLISHES THROUGH A WORKER, and only with the record's consent:
// an approve with no recorded approval leaves the job landed and signals
// nothing; with one, the trailer rides on a publish signal. An approval that
// arrives while the job's worker is still finishing survives that worker's
// done: the job is queued again and the next pull carries the publish.
func TestApprovingQueuesAPublishWithTheTrailer(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if err := f.store.Update(it.ID, func(x *factory.Item) error {
		x.State = factory.StateLanded
		x.Proof = []factory.Claim{{Text: "true passes", OK: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(f.desk)
	if _, err := doors.SignOff(it.ID, false); err == nil || !strings.Contains(err.Error(), "requires a recorded approval") {
		t.Fatalf("an unrecorded approve = %v", err)
	}
	f.state(t, it.ID, factory.StateLanded)
	history, err := record.Open(f.store, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	event, err := history.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doors.SignOff(it.ID, false); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateLanded)
	if err := f.desk.Apply(held.ID, "w1", true, func(x *factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateQueued)
	next, err := f.desk.Pull(lease.Worker{ID: "w2", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := f.desk.Drain(next.ID, "w2")
	want := []lease.Signal{{Verb: lease.SignalPublish, Words: "Factory-Approval: " + event.ID + " @sam"}}
	if err != nil || !reflect.DeepEqual(signals, want) {
		t.Fatalf("the pull carried %+v, %v", signals, err)
	}
}
