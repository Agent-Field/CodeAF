package intake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// goAheadFloor is a store with a fake clock and a launch door that queues the
// job and counts it, so a pass can be run as often as a test likes.
type goAheadFloor struct {
	t       *testing.T
	st      *store.Store
	history *record.Record
	in      *intake.Intake
	now     time.Time
	started []int
	refuse  bool
}

func newGoAheadFloor(t *testing.T) *goAheadFloor {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &goAheadFloor{t: t, st: st, now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return f.now }
	if f.history, err = record.Open(st, record.Options{Now: clock}); err != nil {
		t.Fatal(err)
	}
	f.in = intake.New(st, intake.Options{Record: f.history, Now: clock, Launch: func(id int) error {
		if f.refuse {
			return errors.New("no runner")
		}
		f.started = append(f.started, id)
		return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	return f
}

// job adds a Backlog job and answers its number.
func (f *goAheadFloor) job(change func(*factory.Item)) int {
	f.t.Helper()
	it := factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Origin: factory.OriginChat, State: factory.StateNew, Title: "work"}
	if change != nil {
		change(&it)
	}
	made, err := f.st.Create(it)
	if err != nil {
		f.t.Fatal(err)
	}
	return made.ID
}

func (f *goAheadFloor) policy(stopped bool, allowances ...factory.Allowance) {
	f.t.Helper()
	if _, err := f.st.ChangePolicy("ws", func(p *factory.GroupPolicy) error {
		p.Owner, p.Stopped, p.Allowances = "alice", stopped, allowances
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *goAheadFloor) pass() {
	f.t.Helper()
	if err := f.in.GoAheads(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *goAheadFloor) get(id int) factory.Item {
	f.t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}

func (f *goAheadFloor) merge(id int) {
	f.t.Helper()
	if err := f.st.Update(id, func(it *factory.Item) error { it.DoneHow = "merged"; return nil }); err != nil {
		f.t.Fatal(err)
	}
}

const ends = "2026-11-20T00:00:00Z"

// A COVERED JOB WAITS FOR WHAT IT WAITS ON TO MERGE, then starts by itself: it
// becomes the policy owner's, holds its budget, and its start is recorded as
// that allowance's, which is what lets the app say which go-ahead started it.
func TestAGoAheadStartsACoveredJobOnceItsWaitsMerge(t *testing.T) {
	f := newGoAheadFloor(t)
	parent := f.job(nil)
	child := f.job(func(it *factory.Item) { it.Cap = 3 })
	if err := f.st.Update(child, func(it *factory.Item) error { it.WaitsOn = []int{parent}; return nil }); err != nil {
		t.Fatal(err)
	}
	f.policy(false, factory.Allowance{ID: "ga_1", Jobs: []int{child}, CapUSD: 10, EndsAt: ends})

	f.pass()
	got := f.get(child)
	if len(f.started) != 0 || got.State != factory.StateNew {
		t.Fatalf("a job whose parent has not merged started: %v %+v", f.started, got)
	}
	if b := got.Blocked; b == nil || b.By != factory.BlockedByWaitsOn || b.Job != parent || !b.Since.Equal(f.now) {
		t.Fatalf("blocked = %+v", got.Blocked)
	}
	revision, _ := f.st.Revision(child)
	f.now = f.now.Add(time.Minute)
	f.pass()
	if again, _ := f.st.Revision(child); again != revision {
		t.Fatal("a pass that found nothing new rewrote the job")
	}
	if f.get(parent).State != factory.StateNew {
		t.Fatal("a job no allowance covers was started")
	}

	f.merge(parent)
	f.pass()
	got = f.get(child)
	if len(f.started) != 1 || f.started[0] != child || got.State != factory.StateQueued {
		t.Fatalf("started %v, state %s", f.started, got.State)
	}
	if got.PolicyRun != "ga_1" || got.PolicyGroup != "ws" || got.Blocked != nil || got.Starter != "alice" || got.Cap != 3 {
		t.Fatalf("the started job reads %+v", got)
	}
	events, err := f.history.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.AutoStarted || events[0].Detail != "allowance ga_1" || events[0].Job != child || events[0].Handle != "alice" {
		t.Fatalf("record = %+v %v", events, err)
	}
	f.pass()
	if len(f.started) != 1 {
		t.Fatal("a go-ahead started the same job twice")
	}
}

// THE CAP HOLDS ACROSS STARTS: each started job holds its budget against it,
// the last one that fits is given only what is left, and then nothing more
// starts and the waiting job says so.
func TestAGoAheadStartsNothingPastItsCap(t *testing.T) {
	f := newGoAheadFloor(t)
	budget := func(it *factory.Item) { it.Cap = 3 }
	a, b, c := f.job(budget), f.job(budget), f.job(budget)
	f.policy(false, factory.Allowance{ID: "ga_cap", Jobs: []int{a, b, c}, CapUSD: 5, EndsAt: ends})
	f.pass()
	if len(f.started) != 2 || f.get(a).Cap != 3 || f.get(b).Cap != 2 {
		t.Fatalf("started %v with budgets %v and %v", f.started, f.get(a).Cap, f.get(b).Cap)
	}
	if got := f.get(c); got.State != factory.StateNew || got.Blocked == nil || got.Blocked.By != factory.BlockedByPolicy || got.Blocked.Allowance != "ga_cap" {
		t.Fatalf("the job past the cap reads %+v", got)
	}
	// NOTHING IS SPENT YET, so the cap is held by the running jobs, not spent.
	if got := f.get(c).Blocked.Reason; got != intake.GoAheadCapHeld {
		t.Fatalf("with $0 spent the waiting job says %q, want %q", got, intake.GoAheadCapHeld)
	}
	// Once what they spent reaches the cap, it says the cap is spent.
	for id, spent := range map[int]float64{a: 3, b: 2} {
		if err := f.st.Update(id, func(it *factory.Item) error { it.SpentBefore = spent; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	f.pass()
	if got := f.get(c).Blocked.Reason; got != intake.GoAheadCapSpent {
		t.Fatalf("with the cap spent the waiting job says %q, want %q", got, intake.GoAheadCapSpent)
	}
}

// ENDING, STOPPING OR WITHDRAWING A GO-AHEAD STARTS NOTHING FURTHER, and a job
// that is someone else's waits for them.
func TestAnEndedStoppedOrWithdrawnGoAheadStartsNothing(t *testing.T) {
	f := newGoAheadFloor(t)
	job := f.job(nil)
	theirs := f.job(func(it *factory.Item) { it.Assignee = "bob" })
	f.policy(false, factory.Allowance{ID: "ga_end", Jobs: []int{job, theirs}, CapUSD: 10, EndsAt: ends})

	f.now = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	f.pass()
	if len(f.started) != 0 || f.get(job).Blocked == nil || f.get(job).Blocked.Reason != "its go-ahead ended" {
		t.Fatalf("an ended go-ahead: started %v, %+v", f.started, f.get(job).Blocked)
	}

	f.now = time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	f.policy(true, factory.Allowance{ID: "ga_end", Jobs: []int{job, theirs}, CapUSD: 10, EndsAt: ends})
	f.pass()
	if len(f.started) != 0 || f.get(job).Blocked.Reason != "its go-ahead was stopped" {
		t.Fatalf("a stopped policy: started %v, %+v", f.started, f.get(job).Blocked)
	}

	f.policy(false)
	f.pass()
	if len(f.started) != 0 || f.get(job).Blocked != nil {
		t.Fatalf("a withdrawn allowance: started %v, %+v", f.started, f.get(job).Blocked)
	}

	f.policy(false, factory.Allowance{ID: "ga_end", Jobs: []int{job, theirs}, CapUSD: 10, EndsAt: ends})
	f.pass()
	if len(f.started) != 1 || f.started[0] != job {
		t.Fatalf("started %v, want only job %d", f.started, job)
	}
	if got := f.get(theirs); got.State != factory.StateNew || got.Blocked == nil || got.Blocked.By != factory.BlockedByPolicy {
		t.Fatalf("someone else's job reads %+v", got)
	}
}

// A LAUNCH THAT REFUSES TAKES THE START BACK: the job is not counted as the
// allowance's, says it could not start, and the next pass tries again.
func TestARefusedLaunchIsTriedAgain(t *testing.T) {
	f := newGoAheadFloor(t)
	job := f.job(nil)
	f.policy(false, factory.Allowance{ID: "ga_1", Jobs: []int{job}, CapUSD: 10, EndsAt: ends})
	f.refuse = true
	if err := f.in.GoAheads(context.Background()); err == nil {
		t.Fatal("a refused launch was not reported")
	}
	if got := f.get(job); got.PolicyRun != "" || got.Blocked == nil || got.State != factory.StateNew {
		t.Fatalf("after a refused launch: %+v", got)
	}
	f.refuse = false
	f.pass()
	if got := f.get(job); len(f.started) != 1 || got.PolicyRun != "ga_1" || got.Blocked != nil {
		t.Fatalf("the next pass: started %v, %+v", f.started, got)
	}
}
