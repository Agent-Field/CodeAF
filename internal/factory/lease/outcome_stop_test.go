package lease_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// atGate is a job whose plan finished and which waits at its approve gate,
// as its runner's report leaves the service's copy.
func atGate(it *factory.Item, at time.Time) {
	it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindApprove, "plan is ready · continue, or send it back?"
	it.Asking = &factory.Asked{Stage: factory.ApproveName, Question: it.Question, At: at, With: factory.AskedYou}
	it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{
		{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, At: at},
		{Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting},
		{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
	}}
}

// A PERSON'S DECISION TO END A RUN IS NO FAILED JOB, on every door of the
// Factory service that ends one: Stop (factory.job_stop and `codeaf factory
// stop` reach it), Close, Reject, the reset before a reassignment, and a
// cancel of a step out of attempts. Each cuts the step it finds under way and
// writes the person's stop on the job as a failure coded stopped; that write
// recorded a failed outcome beside whatever the door really did.
func TestServiceStopDoorsRecordNoFailedOutcome(t *testing.T) {
	running := func(it *factory.Item, at time.Time) {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, At: at},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}}
	}
	outOfAttempts := func(it *factory.Item, at time.Time) {
		running(it, at)
		it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindOutOfAttempts, "plan is out of attempts · try once more?"
		it.Stream.Phases[0].State = factory.PhaseWaiting
	}
	tests := []struct {
		name  string
		held  bool
		shape func(*factory.Item, time.Time)
		door  func(*lease.Doors, int) error
		want  []string
	}{
		{"Stop at the plan gate, held by a runner", true, atGate, func(d *lease.Doors, id int) error { return d.StopAs(id, "sam") }, nil},
		{"Stop in a running step, held by a runner", true, running, func(d *lease.Doors, id int) error { return d.StopAs(id, "sam") }, nil},
		{"Stop at the plan gate, held by nobody", false, atGate, func(d *lease.Doors, id int) error { return d.Stop(id) }, nil},
		{"Close at the plan gate", true, atGate, func(d *lease.Doors, id int) error { return d.Close(id, nil, "sam") }, []string{"finished:closed_locally"}},
		{"Reject at the plan gate", true, atGate, func(d *lease.Doors, id int) error { return d.Reject(id, nil, "sam", "not this plan") }, []string{"finished:closed_locally"}},
		{"reset before a reassignment", true, atGate, func(d *lease.Doors, id int) error { return d.Reset(id) }, nil},
		{"cancel a step out of attempts", false, outOfAttempts, func(d *lease.Doors, id int) error { return d.Answer(id, false, factory.ChoiceCancel) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			it := f.add(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Title: "A stopped job"})
			if tt.held {
				if _, err := f.desk.Pull(worker("w1", "sam")); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.Update(it.ID, func(cur *factory.Item) error { tt.shape(cur, f.now.Add(-time.Minute)); return nil }); err != nil {
				t.Fatal(err)
			}
			zero := int64(0)
			before, err := live.ReadOutcomes(f.store.Root(), &zero, 100)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.door(lease.NewDoors(f.desk), it.ID); err != nil {
				t.Fatal(err)
			}
			stored, err := f.store.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Failure == nil || stored.Failure.Code != factory.CodeStopped {
				t.Fatalf("the door cut no step: failure %+v", stored.Failure)
			}
			cursor := before.Cursor
			after, err := live.ReadOutcomes(f.store.Root(), &cursor, 100)
			if err != nil || after.Reset {
				t.Fatalf("outcomes: %+v %v", after, err)
			}
			var got []string
			for _, e := range after.Events {
				got = append(got, e.Kind+":"+e.How+e.Code)
			}
			if len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
				t.Fatalf("the door recorded %v, want %v: %+v", got, tt.want, after.Events)
			}
		})
	}
}
