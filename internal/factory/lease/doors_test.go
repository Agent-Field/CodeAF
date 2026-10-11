package lease_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
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

// A STOP TAKES THE LEASE: the job is backlog at once, and the holder's next
// call learns the lease is gone, which is how it stops.
func TestAStopReleasesTheLease(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	f.state(t, it.ID, factory.StateNew)
	if _, err := f.desk.Drain(held.ID, "w1"); !errors.Is(err, lease.ErrNoLease) {
		t.Fatalf("the stopped job's lease is still live: %v", err)
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
