package automation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// These tests are written from the validation contract of "one waiting run per
// order, Go and stop asking, trust releases the run waiting" and not from the
// code: each is named for the behaviour a person sees.

// steppedTime is a clock a test moves by hand, safe to read from a run's own
// goroutine.
type steppedTime struct {
	mu  sync.Mutex
	now time.Time
}

func (t *steppedTime) Now() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now
}

func (t *steppedTime) Add(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = t.now.Add(d)
}

// stateRunner says what it was asked and when, so a test can tell a look made
// after the person's go from one made before it.
type stateRunner struct {
	at     func() time.Time
	mu     sync.Mutex
	looks  []time.Time
	works  []time.Time
	handed []string
}

func (r *stateRunner) Look(context.Context, Automation) (Sight, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.looks = append(r.looks, r.at())
	return Sight{Text: fmt.Sprintf("the state at look %d", len(r.looks))}, nil
}

func (r *stateRunner) Judge(context.Context, Automation, Sight) (Judgment, error) {
	return Judgment{Sure: true, Met: true, Line: "yes"}, nil
}

func (r *stateRunner) Work(_ context.Context, _ Automation, _ Run, evidence string) (Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.works = append(r.works, r.at())
	r.handed = append(r.handed, evidence)
	return Report{Outcome: OutcomeDone, Line: "done"}, nil
}

func (r *stateRunner) counts() (looks, works int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.looks), len(r.works)
}

// steppedClock is a store and a clock that move only when a test says so.
func steppedClock(t *testing.T) (*Store, *Clock, *steppedTime, *stateRunner) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	at := &steppedTime{now: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
	s.now = at.Now
	runner := &stateRunner{at: at.Now}
	c := &Clock{Store: s, Runner: runner, Now: at.Now, Log: func(line string) { t.Log("clock: " + line) }}
	c.inflight = map[int64]*flight{}
	return s, c, at, runner
}

// look is one pass of the clock, with the runs it started settled.
func look(c *Clock, at *steppedTime) {
	c.pass(context.Background(), at.Now())
	c.wg.Wait()
}

// untrustedHourly is an hourly piece of work that has been given no go yet,
// anchored at the stepped time so its slots fall on the hour.
func untrustedHourly(t *testing.T, s *Store, at *steppedTime) Automation {
	t.Helper()
	a := routine("1h")
	a.Trust = TrustAsksFirst
	a.Schedule.Anchor = at.Now()
	saved, err := s.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func mustAsking(t *testing.T, s *Store) []Run {
	t.Helper()
	asking, err := s.Asking()
	if err != nil {
		t.Fatal(err)
	}
	return asking
}

// Contract C1: an order left alone for hours has ONE run waiting for the
// person's go, the same run, with the time it began to wait.
func TestAnOrderLeftAloneForHoursKeepsOneRunWaiting(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)

	var first Run
	for hour := 1; hour <= 6; hour++ {
		at.Add(time.Hour)
		look(c, at)
		asking := mustAsking(t, s)
		if len(asking) != 1 {
			t.Fatalf("after %d hour(s) %d runs wait for the person", hour, len(asking))
		}
		if hour == 1 {
			first = asking[0]
			continue
		}
		if asking[0].ID != first.ID || !asking[0].Asked.Equal(first.Asked) {
			t.Fatalf("the waiting run changed: was %d asked %v, now %d asked %v", first.ID, first.Asked, asking[0].ID, asking[0].Asked)
		}
	}
	if runs, _ := s.Runs(saved.ID, 50); len(runs) != 1 {
		t.Fatalf("six hours left %d runs in the history, want the one waiting", len(runs))
	}
	if _, err := s.Answer(first.ID, ReplyGo); err != nil {
		t.Fatal(err)
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("a go ran the work %d times", works)
	}
}

// Contract C1: a run that comes to wait beside one already waiting is folded
// into it: no second ask, the waiting run keeps its id and its asked-at time,
// and an answer already in flight to it still lands.
func TestARunThatComesToWaitBesideAWaitingOneIsFoldedIntoIt(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	waiting, err := s.QueueAsking(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	waiting = mustAsking(t, s)[0]

	// A slot's run that got past the clock's own look at what is busy: the
	// person's "run now" and the slot landing in the same moment.
	at.Add(2 * time.Hour)
	a, err := s.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	beside, err := s.Take(a, at.Now())
	if err != nil {
		t.Fatal(err)
	}
	look(c, at)

	asking := mustAsking(t, s)
	if len(asking) != 1 || asking[0].ID != waiting.ID || !asking[0].Asked.Equal(waiting.Asked) {
		t.Fatalf("the person is asked about %+v, want only run %d asked %v", asking, waiting.ID, waiting.Asked)
	}
	folded, err := s.Run(beside.ID)
	if err != nil {
		t.Fatal(err)
	}
	if folded.Phase != PhaseOver || folded.Outcome != OutcomeSkipped || folded.Line != LineFolded || folded.News() {
		t.Fatalf("the run that came to wait beside it = %+v", folded)
	}
	if _, works := runner.counts(); works != 0 {
		t.Fatal("work ran with nobody's go")
	}
	if _, err := s.Answer(waiting.ID, ReplyGo); err != nil {
		t.Fatalf("an answer to the run that was waiting was refused: %v", err)
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("a go ran the work %d times", works)
	}
}

// Contract C1: runs an older build left waiting side by side are one question
// now: a go to one runs the work once and folds the rest.
func TestAGoToOneOfSeveralWaitingRunsRunsTheWorkOnce(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	first, err := s.QueueAsking(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	var second, third int64
	for i, into := range []*int64{&second, &third} {
		// A SLOT IS UNIQUE, so each leftover is one of its own.
		res, err := s.db.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase, seq) VALUES (?, ?, ?, ?, 1)`,
			saved.ID, ms(at.Now().Add(time.Duration(i+1)*time.Minute)), string(WhyOnTime), string(PhaseAsking))
		if err != nil {
			t.Fatal(err)
		}
		*into, _ = res.LastInsertId()
	}
	if n := len(mustAsking(t, s)); n != 3 {
		t.Fatalf("the fixture has %d waiting runs", n)
	}
	if _, err := s.Answer(second, ReplyGo); err != nil {
		t.Fatal(err)
	}
	if asking := mustAsking(t, s); len(asking) != 0 {
		t.Fatalf("after a go %d runs still wait: %+v", len(asking), asking)
	}
	for _, id := range []int64{first.ID, third} {
		run, _ := s.Run(id)
		if run.Phase != PhaseOver || run.Line != LineFolded {
			t.Fatalf("run %d = %+v, want folded", id, run)
		}
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("a go ran the work %d times", works)
	}
}

// Contract C1: a run that was granted and is queued or running is not
// waiting; a slot that comes while it is going is passed over as it always was.
func TestASlotThatComesWhileAGrantedRunIsGoingIsPassedOverAsBefore(t *testing.T) {
	s, c, at, _ := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	run, err := s.QueueAsking(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(run.ID, ReplyGo); err != nil {
		t.Fatal(err)
	}
	at.Add(2 * time.Hour)
	c.pass(context.Background(), at.Now())
	c.wg.Wait()
	runs, _ := s.Runs(saved.ID, 50)
	if len(runs) != 1 {
		t.Fatalf("a slot beside a granted run left %d runs, want the one", len(runs))
	}
}

// Contract C2: a go given hours after the run began to wait works on what is
// true when it is given, not on what was true when it was asked.
func TestALateGoWorksOnNowAndNotOnWhenItWasAsked(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	untrustedHourly(t, s, at)
	asked := at.Now().Add(time.Hour)
	at.Add(time.Hour)
	look(c, at)
	waiting := mustAsking(t, s)
	if len(waiting) != 1 {
		t.Fatalf("%d runs wait", len(waiting))
	}
	if _, works := runner.counts(); works != 0 {
		t.Fatal("work began before the go")
	}

	at.Add(3 * time.Hour)
	if _, err := s.Answer(waiting[0].ID, ReplyGo); err != nil {
		t.Fatal(err)
	}
	gone := at.Now()
	look(c, at)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.works) != 1 || !runner.works[0].Equal(gone) || !runner.works[0].After(asked) {
		t.Fatalf("the work began at %v; the run was asked at %v and the go was given at %v", runner.works, asked, gone)
	}
}

// Contract C2: a watch that does work looks AFTER the go, and the work is
// handed what that look saw.
func TestALateGoOnAWatchLooksAfterTheGoAndHandsTheWorkWhatItSaw(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	a := watchOn("", false)
	a.Action = Action{Do: "find out why CI failed"}
	a.Trust = TrustAsksFirst
	a.Schedule.Anchor = at.Now()
	if _, err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	at.Add(time.Hour)
	look(c, at)
	waiting := mustAsking(t, s)
	if len(waiting) != 1 {
		t.Fatalf("%d runs wait", len(waiting))
	}
	if looks, works := runner.counts(); looks != 0 || works != 0 {
		t.Fatalf("it looked %d and worked %d times before the person's go", looks, works)
	}

	at.Add(3 * time.Hour)
	if _, err := s.Answer(waiting[0].ID, ReplyGo); err != nil {
		t.Fatal(err)
	}
	gone := at.Now()
	look(c, at)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.looks) != 1 || !runner.looks[0].Equal(gone) {
		t.Fatalf("the look was made at %v, the go was given at %v", runner.looks, gone)
	}
	if len(runner.handed) != 1 || runner.handed[0] != "the state at look 1" {
		t.Fatalf("the work was handed %q", runner.handed)
	}
}

// Contract C3: "Go, and stop asking" runs the waiting run once, counts as a
// go, and trusts the order, so the next slot runs without asking.
func TestGoAndStopAskingRunsTheWaitingRunAndTrustsTheOrder(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	at.Add(time.Hour)
	look(c, at)
	waiting := mustAsking(t, s)
	if len(waiting) != 1 {
		t.Fatalf("%d runs wait", len(waiting))
	}

	got, err := s.Answer(waiting[0].ID, ReplyAlways)
	if err != nil {
		t.Fatal(err)
	}
	if got.Trust != TrustTrusted || got.NeedsAsk() || got.Approved != 1 {
		t.Fatalf("after go-and-stop-asking the order is %q with %d go's", got.Trust, got.Approved)
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("the run it answered ran %d times", works)
	}

	at.Add(time.Hour)
	look(c, at)
	if asking := mustAsking(t, s); len(asking) != 0 {
		t.Fatalf("a trusted order asked again: %+v", asking)
	}
	if _, works := runner.counts(); works != 2 {
		t.Fatalf("the next slot did not run alone: %d runs of work", works)
	}
	if runs, _ := s.Runs(saved.ID, 50); len(runs) != 2 || runs[0].Phase != PhaseOver {
		t.Fatalf("history = %+v", runs)
	}
}

// Contract C3: it is an answer to a run that is waiting, like the others.
func TestGoAndStopAskingNeedsARunThatIsWaiting(t *testing.T) {
	s, _, at, _ := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	if _, err := s.Answer(999, ReplyAlways); err == nil {
		t.Fatal("an answer to a run that does not exist was taken")
	}
	run, err := s.QueueNow(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(run.ID, ReplyAlways); err == nil {
		t.Fatal("go-and-stop-asking was taken for a run that was not waiting")
	}
	if got, _ := s.Get(saved.ID); got.Trust != TrustAsksFirst || got.Approved != 0 {
		t.Fatalf("a refused answer moved the ladder: %q, %d", got.Trust, got.Approved)
	}
}

// Contract C4: trusting an order starts the run waiting on it, and is not a
// go: the ladder's count does not move.
func TestTrustingAnOrderStartsTheRunWaitingOnIt(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	at.Add(time.Hour)
	look(c, at)
	if n := len(mustAsking(t, s)); n != 1 {
		t.Fatalf("%d runs wait", n)
	}

	got, err := s.SetTrust(saved.ID, TrustTrusted)
	if err != nil {
		t.Fatal(err)
	}
	if got.Approved != 0 {
		t.Fatalf("trusting counted %d go's", got.Approved)
	}
	if asking := mustAsking(t, s); len(asking) != 0 {
		t.Fatalf("a trusted order is still asking: %+v", asking)
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("the released run ran %d times", works)
	}
}

// Contract C4: trusting leaves no question open however many were waiting, and
// runs the work once.
func TestTrustingAnOrderFoldsEverythingElseThatWasWaiting(t *testing.T) {
	s, c, at, runner := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	if _, err := s.QueueAsking(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase, seq) VALUES (?, ?, ?, ?, 1)`,
		saved.ID, ms(at.Now()), string(WhyOnTime), string(PhaseAsking)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTrust(saved.ID, TrustTrusted); err != nil {
		t.Fatal(err)
	}
	if asking := mustAsking(t, s); len(asking) != 0 {
		t.Fatalf("a trusted order is still asking: %+v", asking)
	}
	look(c, at)
	if _, works := runner.counts(); works != 1 {
		t.Fatalf("trusting ran the work %d times", works)
	}
}

// Contract C4: only trust releases a run. Asking first again leaves it
// waiting, and a paused order keeps its run waiting for the person's go.
func TestOnlyTrustingAnActiveOrderReleasesItsWaitingRun(t *testing.T) {
	s, _, at, _ := steppedClock(t)
	saved := untrustedHourly(t, s, at)
	waiting, err := s.QueueAsking(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTrust(saved.ID, TrustAsksFirst); err != nil {
		t.Fatal(err)
	}
	if asking := mustAsking(t, s); len(asking) != 1 || asking[0].ID != waiting.ID {
		t.Fatalf("asking first again released the run: %+v", asking)
	}
	if _, err := s.SetStatus(saved.ID, StatusPaused); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTrust(saved.ID, TrustTrusted); err != nil {
		t.Fatal(err)
	}
	if asking := mustAsking(t, s); len(asking) != 1 || asking[0].ID != waiting.ID {
		t.Fatalf("trusting a paused order started its run: %+v", asking)
	}
}

// Contract C7: nothing about the defaults moved. A new order asks first, the
// ladder is still five go's, and an unfolded ask is parked as before.
func TestTheLadderAndItsDefaultsAreWhereTheyWere(t *testing.T) {
	if TrustAfter != 5 {
		t.Fatalf("TrustAfter = %d", TrustAfter)
	}
	s, c, at, _ := steppedClock(t)
	a := routine("1h")
	a.Trust = TrustAsksFirst
	a.Schedule.Anchor = at.Now()
	if _, err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	at.Add(time.Hour)
	look(c, at)
	asking := mustAsking(t, s)
	if len(asking) != 1 || !asking[0].Asked.Equal(at.Now()) {
		t.Fatalf("a run of an order that asks first = %+v", asking)
	}
	got, err := s.Answer(asking[0].ID, ReplyGo)
	if err != nil || got.Trust != TrustEarning || got.Approved != 1 {
		t.Fatalf("a first go = %+v, %v", got, err)
	}
}
