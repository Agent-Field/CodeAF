package automation

import (
	"testing"
	"time"
)

// NOTHING RUNS BEFORE RATIFY: a proposed routine is kept and never due, and
// ratifying it makes it wake from now.
func TestAProposedAutomationWakesOnlyAfterRatify(t *testing.T) {
	s, now := openStore(t)
	a := routine("15m")
	a.Status = StatusProposed
	saved, err := s.Create(a)
	if err != nil || saved.Status != StatusProposed {
		t.Fatalf("created %+v, %v", saved, err)
	}
	if due, _ := s.Due(now.Add(48 * time.Hour)); len(due) != 0 {
		t.Fatalf("a proposed routine was due: %+v", due)
	}
	if _, err := s.SetStatus(saved.ID, StatusActive); err == nil {
		t.Fatal("a proposed routine was resumed without being agreed to")
	}
	ratified, err := s.Ratify(saved.ID)
	if err != nil || ratified.Status != StatusActive || !ratified.Next.After(*now) {
		t.Fatalf("ratified %+v, %v", ratified, err)
	}
	if _, err := s.Ratify(saved.ID); err == nil {
		t.Fatal("an active routine was ratified twice")
	}
}

// A STOPPED AUTOMATION STAYS LISTED AND IS NOT RESUMED.
func TestAStoppedAutomationIsNotResumed(t *testing.T) {
	s, _ := openStore(t)
	saved, err := s.Create(routine("15m"))
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := s.Stop(saved.ID)
	if err != nil || stopped.Status != StatusStopped || !stopped.Next.IsZero() {
		t.Fatalf("stopped %+v, %v", stopped, err)
	}
	if _, err := s.SetStatus(saved.ID, StatusActive); err == nil {
		t.Fatal("a stopped routine was resumed")
	}
	if all, _ := s.List(); len(all) != 1 {
		t.Fatalf("listed %d", len(all))
	}
}

// THE LADDER: work not yet trusted parks each run until the person's go; five
// go's make it trusted, and one not right puts it back to the bottom.
func TestUntrustedWorkAsksAndTheLadderClimbs(t *testing.T) {
	r := startClock(t, &fakeRunner{report: Report{Outcome: OutcomeDone, Line: "drafted"}})
	a := routine("1h")
	a.Trust = TrustAsksFirst
	saved, err := r.store.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	waitAsking := func() Run {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			asking, _ := r.store.Asking()
			if len(asking) == 1 {
				return asking[0]
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("no run asked first")
		return Run{}
	}
	for i := 1; i <= TrustAfter; i++ {
		r.now(t, saved.ID)
		run := waitAsking()
		if r.runner.worked.Load() != int32(i-1) {
			t.Fatalf("work ran before the person's go (%d)", r.runner.worked.Load())
		}
		got, err := r.store.Answer(run.ID, ReplyGo)
		if err != nil || got.Approved != i {
			t.Fatalf("go %d = %+v, %v", i, got, err)
		}
		r.settle(t, saved.ID, i)
	}
	got, _ := r.store.Get(saved.ID)
	if got.Trust != TrustTrusted || got.NeedsAsk() {
		t.Fatalf("after %d go's the trust is %q", TrustAfter, got.Trust)
	}
	r.now(t, saved.ID)
	r.settle(t, saved.ID, TrustAfter+1)
	if r.runner.worked.Load() != TrustAfter+1 {
		t.Fatalf("a trusted run did not run alone: %d", r.runner.worked.Load())
	}

	if _, err := r.store.SetTrust(saved.ID, TrustAsksFirst); err != nil {
		t.Fatal(err)
	}
	r.now(t, saved.ID)
	run := waitAsking()
	got, err = r.store.Answer(run.ID, ReplyNotRight)
	if err != nil || got.Trust != TrustAsksFirst || got.Approved != 0 {
		t.Fatalf("not right = %+v, %v", got, err)
	}
	runs := r.settle(t, saved.ID, TrustAfter+2)
	if runs[0].Outcome != OutcomeSkipped || runs[0].Line != LineNotRight || runs[0].News() {
		t.Fatalf("the passed-over run = %+v", runs[0])
	}
}

// A RUN ASKING FIRST HOLDS ITS AUTOMATION: the slots that come meanwhile are
// passed over, never stacked behind it.
func TestAnAskingRunHoldsItsAutomation(t *testing.T) {
	s, now := openStore(t)
	a := routine("15m")
	a.Trust = TrustAsksFirst
	saved, err := s.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.QueueNow(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AskRun(run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueNow(saved.ID); err == nil {
		t.Fatal("a second run queued behind one that is asking")
	}
	if _, err := s.Answer(run.ID, ReplySkip); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(saved.ID); got.Trust != TrustAsksFirst || got.Last == nil || got.Last.Outcome != OutcomeSkipped {
		t.Fatalf("after a skip = %+v", got)
	}
	if _, err := s.Answer(run.ID, ReplyGo); err == nil {
		t.Fatal("a run already passed over took a go")
	}
	_ = now
}

// AN EDIT NEVER MOVES THE LADDER.
func TestAnEditKeepsTheTrustItHad(t *testing.T) {
	s, _ := openStore(t)
	a := routine("15m")
	a.Trust, a.Approved = TrustEarning, 3
	saved, err := s.Create(a)
	if err != nil {
		t.Fatal(err)
	}
	saved.Action.Do = "draft it shorter"
	saved.Trust, saved.Approved = TrustTrusted, 9
	saved.AnswersIn = ScopeEverywhere
	got, err := s.Update(saved)
	if err != nil || got.Trust != TrustEarning || got.Approved != 3 || got.AnswersIn != ScopeEverywhere {
		t.Fatalf("after the edit = %+v, %v", got, err)
	}
}
