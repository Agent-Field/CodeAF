package session

// The laws of a clock that waits for its question to be seen (askclock.go).

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// clockedAsk is one open question nobody is parked on, on an hour's clock, so
// nothing here races the wall: every clock is run by hand.
func clockedAsk(t *testing.T) (*Agent, Question) {
	t.Helper()
	a := askTestAgent(t, true)
	if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if q.Deadline.IsZero() {
		t.Fatal("the question carries no clock")
	}
	return a, q
}

// A DEFERRED CLOCK TAKES NOTHING, AND STARTS FROM WHEN IT IS SHOWN. The
// suggestion is never taken for somebody who has not seen the question, and
// the person who finally sees it gets the policy's whole length.
func TestADeferredClockTakesNothingAndStartsWholeWhenShown(t *testing.T) {
	a, q := clockedAsk(t)
	if !a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not deferred")
	}
	if got := waitForOneQuestion(t, a); !got.Deadline.IsZero() {
		t.Fatalf("a deferred question still says when its clock ends: %v", got.Deadline)
	}
	// A timer that fired as the clock was deferred lost that race.
	a.askClockRanOut(q.ID)
	if notes := askDelivered(a); len(notes) > 0 {
		t.Fatalf("a deferred clock answered a question nobody had seen: %v", notes)
	}
	if a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("a clock already deferred was deferred again")
	}
	shown := time.Now()
	if !a.StartQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the deferred clock did not start")
	}
	got := waitForOneQuestion(t, a)
	if got.Deadline.Before(shown.Add(time.Hour)) {
		t.Fatalf("the clock did not get its whole length from being shown: %v, shown at %v", got.Deadline, shown)
	}
	if a.StartQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("a running clock was started a second time")
	}
	a.askClockRanOut(q.ID)
	if notes := askDelivered(a); len(notes) == 0 {
		t.Fatal("the started clock never took the pick")
	}
}

// THE DEFERRAL IS BOUNDED: a surface that deferred and went away cannot keep a
// suggestion unclocked. The bound is the same one timer, so it is run here by
// shortening it rather than by sleeping five minutes.
func TestADeferredClockStartsByItselfAtItsBound(t *testing.T) {
	saved := askClockDeferLimit
	askClockDeferLimit = time.Millisecond
	t.Cleanup(func() { askClockDeferLimit = saved })
	a, q := clockedAsk(t)
	if !a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not deferred")
	}
	deadline := time.After(2 * time.Second)
	for {
		if open := a.OpenQuestions(); len(open) == 1 && !open[0].Deadline.IsZero() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the deferred clock never started at its bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// A PERSON'S HOLD WINS AND IS NEVER UNDONE: showing the question later does
// not start a clock somebody stopped.
func TestShowingAHeldQuestionDoesNotStartItsClock(t *testing.T) {
	a, q := clockedAsk(t)
	if !a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not deferred")
	}
	a.HoldQuestion(QuestionAsk, q.Token())
	if a.StartQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("showing a held question started its clock")
	}
	if a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("a held question's clock was deferred")
	}
	if got := waitForOneQuestion(t, a); !got.Deadline.IsZero() {
		t.Fatalf("a held question carries a deadline: %v", got.Deadline)
	}
}

// ONLY THE LANE'S OWN CLOCK IS ITS TO DEFER: a parked question has no clock
// here to stop, and another lane's token is not read as this one's.
func TestOnlyAClockedAskOfThisLaneIsDeferred(t *testing.T) {
	a, q := clockedAsk(t)
	if a.DeferQuestionClock(QuestionConsent, q.Token()) {
		t.Fatal("another lane's token deferred the ask's clock")
	}
	if a.DeferQuestionClock(QuestionAsk, "not a number") {
		t.Fatal("an unreadable token deferred a clock")
	}
	plain := askTestAgent(t, true)
	if _, _, err := plain.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	unclocked := waitForOneQuestion(t, plain)
	if plain.DeferQuestionClock(QuestionAsk, unclocked.Token()) {
		t.Fatal("a question with no clock was deferred")
	}
}

// clockGen is the live arming of one question's timer.
func clockGen(a *Agent, id uint64) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asked.atLocked(id).clockGen
}

// A TIMER STOPPED TOO LATE DECIDES NOTHING. Its callback had begun and was
// waiting on the lock while the clock was deferred and shown again; that
// callback is exactly a call carrying the arming it was made with, so it is
// made here by hand — no sleep, no race — and it must not take the pick
// against the new deadline. The live arming still does.
func TestAStaleClockCallbackCannotAnswerAfterDeferAndShow(t *testing.T) {
	a, q := clockedAsk(t)
	stale := clockGen(a, q.ID)
	if !a.DeferQuestionClock(QuestionAsk, q.Token()) || !a.StartQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not deferred and shown")
	}
	shown := waitForOneQuestion(t, a).Deadline
	a.askClockEnded(q.ID, stale, true)
	if notes := askDelivered(a); len(notes) > 0 {
		t.Fatalf("the old clock's callback took the pick before the new deadline: %v", notes)
	}
	if got := waitForOneQuestion(t, a); !got.Deadline.Equal(shown) {
		t.Fatalf("the old callback moved the new deadline: %v, want %v", got.Deadline, shown)
	}
	a.askClockEnded(q.ID, clockGen(a, q.ID), true)
	if notes := askDelivered(a); len(notes) == 0 {
		t.Fatal("the live clock never took the pick")
	}
}

// A BOUND FROM AN EARLIER DEFERRAL DOES NOT START A LATER ONE: deferred,
// shown, deferred again, the first bound's callback finds itself stale.
func TestAStaleBoundCannotStartALaterDeferral(t *testing.T) {
	a, q := clockedAsk(t)
	if !a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not deferred")
	}
	firstBound := clockGen(a, q.ID)
	if !a.StartQuestionClock(QuestionAsk, q.Token()) || !a.DeferQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("the clock was not shown and deferred again")
	}
	if a.startDeferredClock(q.ID, firstBound, true) {
		t.Fatal("an earlier deferral's bound started the clock")
	}
	if got := waitForOneQuestion(t, a); !got.Deadline.IsZero() {
		t.Fatalf("the question is clocked while deferred: %v", got.Deadline)
	}
	if !a.startDeferredClock(q.ID, clockGen(a, q.ID), true) {
		t.Fatal("the live bound did not start the clock")
	}
}

// A QUESTION THAT BLOCKS IS NEVER DEFERRED, whatever a surface believed: the
// engine reads its own Blocking and not a hint.
func TestABlockingQuestionsClockIsNeverDeferred(t *testing.T) {
	for name, raw := range map[string]string{
		"the turn parks on it":  askWaiting,
		"it names waiting work": `{"head":"Which storage shape?","kind":"choice","reason":"two fit","stakes":"reversible","blocking":{"turn":false,"tasks":["port the parser"]},"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}],"pick":{"key":"1","reason":"sqlite readers"}}`,
	} {
		a := askTestAgent(t, true)
		if err := a.SetAutonomy(AskChoice, Policy{Kind: PolicyRecommendThenAuto, After: time.Hour}); err != nil {
			t.Fatal(err)
		}
		go func() { _, _, _ = a.executeAsk(context.Background(), json.RawMessage(raw)) }()
		q := waitForOneQuestion(t, a)
		if !q.Blocking.Blocks() || q.Deadline.IsZero() {
			t.Fatalf("%s: the question blocks %v with deadline %v; this test needs a clocked blocking question", name, q.Blocking.Blocks(), q.Deadline)
		}
		if a.DeferQuestionClock(QuestionAsk, q.Token()) {
			t.Fatalf("%s: a blocking question's clock was deferred", name)
		}
	}
}

// WHAT IS BLOCKING IS THE ASK TOOL'S OWN DERIVATION: a turn that carries on
// and names no work is the one shape nothing waits on.
func TestOnlyACarriedOnAskNamingNoWorkBlocksNothing(t *testing.T) {
	a, q := clockedAsk(t)
	_ = a
	if q.Blocking.Blocks() {
		t.Fatal("an ask whose turn carried on and names no work says it blocks")
	}
	parked := askTestAgent(t, true)
	go func() { _, _, _ = parked.executeAsk(context.Background(), json.RawMessage(askWaiting)) }()
	if got := waitForOneQuestion(t, parked); !got.Blocking.Blocks() {
		t.Fatal("an ask the turn parked on says it blocks nothing")
	}
}
