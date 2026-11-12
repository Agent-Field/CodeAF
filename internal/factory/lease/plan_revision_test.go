package lease_test

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A PLAN SENT BACK IS DECIDED ON THE LOCKED DOCUMENT. These drive
// lease.Doors.RejectPlan against a real store and desk: the revision, the plan
// and the caller's permission are all asked inside the same lock as the
// rewind, and a refusal leaves the item, its revision, the desk and its
// signals byte for byte as they were.

// planAt is a job waiting at its plan's approve step with phases ph and the
// run standing at cur.
func planAt(cur int, ph ...factory.Phase) factory.Item {
	return factory.Item{
		State: factory.StateNeedsYou, Assignee: "sam", Notes: []string{"an earlier note"},
		Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove,
		Stream: &factory.Stream{Cur: cur, Phases: ph},
	}
}

func planPhase(state factory.PhaseState) factory.Phase {
	return factory.Phase{Name: "plan", Kind: factory.StageChat, State: state, Round: 1}
}

func gatePhase(state factory.PhaseState) factory.Phase {
	return factory.Phase{Name: "approve", Kind: factory.StageGate, State: state}
}

func buildPhase() factory.Phase {
	return factory.Phase{Name: "build", Kind: factory.StageChat, State: factory.PhasePending}
}

// onDisk is every byte a refusal must not change: the item's document (and so
// its revision) and the desk's leases and signals.
func (f *fixture) onDisk(t *testing.T, id int) string {
	t.Helper()
	item, err := os.ReadFile(f.store.ItemPath(id))
	if err != nil {
		t.Fatal(err)
	}
	desk, err := os.ReadFile(f.desk.Path())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(item) + "\n--desk--\n" + string(desk)
}

func (f *fixture) revision(t *testing.T, id int) int {
	t.Helper()
	rev, err := f.store.Revision(id)
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func TestRejectPlanSendsAnUnheldPlanBackOnTheRevisionReviewed(t *testing.T) {
	for name, it := range map[string]factory.Item{
		"the gate waits":                     planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting), buildPhase()),
		"the shape older workers stored":     planAt(0, factory.Phase{Name: "plan", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1, Result: &factory.StageResult{Done: true}}, gatePhase(factory.PhasePending), buildPhase()),
		"a finished step further back":       planAt(2, planPhase(factory.PhaseDone), factory.Phase{Name: "approve0", Kind: factory.StageGate, State: factory.PhaseDone}, gatePhase(factory.PhaseWaiting), buildPhase()),
		"an old document's step has no name": planAt(1, factory.Phase{Kind: factory.StageChat, State: factory.PhaseDone, Round: 1}, gatePhase(factory.PhaseWaiting), buildPhase()),
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			job := f.add(t, it)
			rev := f.revision(t, job.ID)
			asked := 0
			before, err := lease.NewDoors(f.desk).RejectPlan(job.ID, rev, "use the other file", func(cur factory.Item) error {
				asked++
				if cur.Question == "" || cur.QKind != factory.QKindApprove {
					t.Errorf("permission was asked of a document already answered: %+v", cur)
				}
				return nil
			})
			if err != nil || asked != 1 {
				t.Fatalf("rejected: %v, permission asked %d times", err, asked)
			}
			if before.QKind != factory.QKindApprove || before.Stream.Cur != it.Stream.Cur {
				t.Fatalf("the copy handed back is not the job as it was asked: %+v", before)
			}
			got, _ := f.store.Get(job.ID)
			prev := 0
			if got.State != factory.StateQueued || got.Question != "" || got.QKind != "" || got.Stream.Cur != prev || got.Stream.Phases[prev].Round != 2 {
				t.Fatalf("not sent back: state %s cur %d phases %+v", got.State, got.Stream.Cur, got.Stream.Phases)
			}
			if strings.Join(got.Notes, "|") != "an earlier note|use the other file" {
				t.Fatalf("notes = %q", got.Notes)
			}
			if f.revision(t, job.ID) != rev+1 {
				t.Fatalf("revision %d after one write from %d", f.revision(t, job.ID), rev)
			}
		})
	}
}

// A NO THAT WOULD NOT REVISE A PLAN IS REFUSED WITH NOTHING CHANGED: the
// first gate (the same no would stop the job), no run at all, a run shape
// nobody recognises, and every question that is not a plan's.
func TestRejectPlanRefusesWithoutAnyChange(t *testing.T) {
	waiting := &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "build", Kind: factory.StageChat, State: factory.PhaseWaiting}}}
	cases := map[string]struct {
		it   factory.Item
		want string
	}{
		"the first gate":                  {planAt(0, gatePhase(factory.PhaseWaiting), planPhase(factory.PhasePending)), "This plan has no finished step before it to send back to."},
		"only unfinished steps before it": {planAt(1, planPhase(factory.PhaseFailed), gatePhase(factory.PhaseWaiting)), "This plan has no finished step before it to send back to."},
		"no run":                          {func() factory.Item { it := planAt(0); it.Stream = nil; return it }(), "This plan's run is not in a shape that can be sent back."},
		"an unrecognised shape":           {planAt(0, factory.Phase{Name: "plan", Kind: factory.StageChat, State: factory.PhaseWaiting}, gatePhase(factory.PhasePending)), "This plan's run is not in a shape that can be sent back."},
		"the run's step is out of range":  {planAt(5, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting)), "This plan's run is not in a shape that can be sent back."},
		"a spending limit":                {factory.Item{State: factory.StateNeedsYou, Question: "budget?", QKind: "cap", Cap: 5, Stream: waiting}, "Not a plan waiting for you."},
		"an exhausted round budget":       {factory.Item{State: factory.StateNeedsYou, Question: "try once more?", QKind: factory.QKindOutOfAttempts, Stream: waiting}, "Not a plan waiting for you."},
		"a start question":                {factory.Item{State: factory.StateNeedsYou, Question: "start?", QKind: factory.QKindAskFirst}, "Not a plan waiting for you."},
		"a free question":                 {factory.Item{State: factory.StateNeedsYou, Question: "which file?", QKind: "step", Stream: waiting}, "Not a plan waiting for you."},
		"a plan question already taken down": {func() factory.Item {
			it := planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting))
			it.Question = ""
			return it
		}(), "Not a plan waiting for you."},
		"running": {factory.Item{State: factory.StateRunning, Stream: waiting}, "Not a plan waiting for you."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			job := f.add(t, c.it)
			rev := f.revision(t, job.ID)
			was := f.onDisk(t, job.ID)
			_, err := lease.NewDoors(f.desk).RejectPlan(job.ID, rev, "redo", func(factory.Item) error {
				t.Error("permission was asked of a job that is not a plan to send back")
				return nil
			})
			var refusal run.PlanRevisionRefusal
			if !errors.As(err, &refusal) || err.Error() != c.want {
				t.Fatalf("err = %v, want the refusal %q", err, c.want)
			}
			if f.onDisk(t, job.ID) != was {
				t.Fatal("a refused plan revision changed the job, its revision or the desk")
			}
		})
	}
}

// THE REVISION IS COMPARED UNDER THE LOCK: a write that lands after the
// person's read (here a worker turning the plan into a spending limit, and a
// change of the job's person) refuses the revision as stale before the plan,
// the permission or anything else is looked at.
func TestRejectPlanRefusesARevisionTheJobHasMovedOn(t *testing.T) {
	for name, move := range map[string]func(*factory.Item){
		"a worker asks for more money": func(it *factory.Item) { it.Question, it.QKind, it.Cap = "budget?", "cap", 5 },
		"the job changes hands":        func(it *factory.Item) { it.Assignee = "kim" },
		"nothing a person would see":   func(*factory.Item) {},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			job := f.add(t, planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting), buildPhase()))
			read := f.revision(t, job.ID)
			if err := f.store.Update(job.ID, func(it *factory.Item) error { move(it); return nil }); err != nil {
				t.Fatal(err)
			}
			was := f.onDisk(t, job.ID)
			_, err := lease.NewDoors(f.desk).RejectPlan(job.ID, read, "redo", func(factory.Item) error {
				t.Error("permission was asked of a revision nobody reviewed")
				return nil
			})
			var stale *store.StaleError
			if !errors.As(err, &stale) || stale.Expected != read || stale.Revision != read+1 {
				t.Fatalf("err = %v, want stale at %d", err, read)
			}
			if f.onDisk(t, job.ID) != was {
				t.Fatal("a stale plan revision changed something")
			}
		})
	}
}

// THE CALLER'S PERMISSION IS ASKED OF THE LOCKED DOCUMENT, and its refusal is
// the door's answer with nothing written.
func TestRejectPlanPermissionRefusalWritesNothing(t *testing.T) {
	f := setup(t)
	job := f.add(t, planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting), buildPhase()))
	was := f.onDisk(t, job.ID)
	no := errors.New("only @sam can answer this")
	_, err := lease.NewDoors(f.desk).RejectPlan(job.ID, f.revision(t, job.ID), "redo", func(cur factory.Item) error {
		if cur.Person() != "sam" {
			t.Errorf("permission asked of %+v", cur)
		}
		return no
	})
	if !errors.Is(err, no) || f.onDisk(t, job.ID) != was {
		t.Fatalf("err = %v, or the refusal wrote something", err)
	}
}

// HELD: the question comes down, the approve step waits again, and the worker
// hears the no and the words through its lease, exactly as a no on the answer
// route; the worker's loop does the rewind. A refusal on a held job leaves the
// lease and its signals as they were.
func TestRejectPlanOnAHeldJobSignalsItsWorker(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	shape := planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting), buildPhase())
	put := func(s factory.Item) {
		t.Helper()
		if err := f.store.Update(it.ID, func(cur *factory.Item) error {
			cur.State, cur.Question, cur.QKind, cur.Stream, cur.Notes = s.State, s.Question, s.QKind, s.Stream, s.Notes
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	doors := lease.NewDoors(f.desk)

	put(planAt(0, gatePhase(factory.PhaseWaiting), planPhase(factory.PhasePending)))
	was := f.onDisk(t, it.ID)
	if _, err := doors.RejectPlan(it.ID, f.revision(t, it.ID), "redo", nil); err == nil || f.onDisk(t, it.ID) != was {
		t.Fatalf("a held first gate was answered: %v", err)
	}
	if signals, _ := f.desk.Drain(held.ID, "w1"); len(signals) != 0 {
		t.Fatalf("a refusal signaled the worker: %+v", signals)
	}

	put(shape)
	if _, err := doors.RejectPlan(it.ID, f.revision(t, it.ID), "split it in two", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if got.State != factory.StateRunning || got.Question != "" || got.Stream.Cur != 1 || got.Stream.Phases[1].State != factory.PhasePending || got.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("held job after: %s cur %d %+v", got.State, got.Stream.Cur, got.Stream.Phases)
	}
	if strings.Join(got.Notes, "|") != "an earlier note" {
		t.Fatalf("the door kept the words the worker's loop keeps: %q", got.Notes)
	}
	signals, err := f.desk.Drain(held.ID, "w1")
	if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalAnswer || signals[0].Yes || signals[0].Words != "split it in two" {
		t.Fatalf("signals = %+v %v", signals, err)
	}
	if live, _ := f.desk.Live(); len(live) != 1 || live[0].ID != held.ID {
		t.Fatalf("the lease moved: %+v", live)
	}
}

// TWO PEOPLE WHO REVIEWED THE SAME PAGE: one revision is written, and the other
// is stale, however the two interleave.
func TestRejectPlanTwiceOnOneRevisionWritesOnce(t *testing.T) {
	f := setup(t)
	job := f.add(t, planAt(1, planPhase(factory.PhaseDone), gatePhase(factory.PhaseWaiting), buildPhase()))
	rev := f.revision(t, job.ID)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = lease.NewDoors(f.open(t)).RejectPlan(job.ID, rev, "once", nil)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		var stale *store.StaleError
		switch {
		case err == nil:
			ok++
		case !errors.As(err, &stale):
			t.Errorf("a loser was not stale: %v", err)
		}
	}
	got, _ := f.store.Get(job.ID)
	if ok != 1 || f.revision(t, job.ID) != rev+1 || strings.Join(got.Notes, "|") != "an earlier note|once" || got.Stream.Phases[0].Round != 2 {
		t.Fatalf("%d writes; revision %d from %d; notes %q", ok, f.revision(t, job.ID), rev, got.Notes)
	}
}
