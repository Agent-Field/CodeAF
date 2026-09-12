package provider

import (
	"strings"
	"testing"
	"time"
)

// ── A CONTROLLER THAT SAYS NOTHING (#971) ───────────────────────────────────
//
// Every scenario in this package has a controller that speaks, so the bound
// [theControllersWord] arms is the one part of that fixture nothing exercises:
// it only ever loses the race to the word. The tests here are its regression
// net, and they hold the hold on its own, with no wire and no scenario — the
// bound is armed, the channel it guards is waited on, and what it says on
// expiry is read straight back. Nothing is slept for and nothing is polled: the
// only wait is on the channel the bound itself closes, and on the watcher
// behind it.

// TestAHoldThatNeverOpensEndsAtTheScenariosOwnBound is #971 itself: a
// controller that never speaks used to leave the lane holding its first word
// until the package's deadline, so the reader was handed a timeout naming
// whichever test happened to be running rather than the silence that caused it.
// The hold now opens itself at the ceiling's own slack multiple and carries the
// sentence that says who was silent, through what bound, and what the last
// thing a person was told was.
func TestAHoldThatNeverOpensEndsAtTheScenariosOwnBound(t *testing.T) {
	word := newControllersWord()
	ceiling := 10 * time.Millisecond
	word.arm(ceiling)

	// THE ONLY WAIT HERE IS ON THE HOLD ITSELF. Nothing else can close this
	// channel in this test — no rung is ever posted and nobody speaks — so a
	// receive that returns is the bound having fired, and a bound that never
	// fires is the package timeout this whole fixture exists to replace.
	<-word.spoken
	<-word.watched

	if !word.expired.Load() {
		t.Fatal("the hold opened without its bound having expired, so something other than the scenario's patience closed it")
	}
	// The bound is the stated ceiling times [heldWordSlack] and no figure of
	// its own, so it is stated here the way the arm states it rather than as a
	// second number that could drift away from it.
	if got, want := word.bound, ceiling*heldWordSlack; got != want {
		t.Fatalf("the hold was bounded at %s, want the scenario's own ceiling times its slack (%s)", got, want)
	}
	said := word.silence()
	if !strings.Contains(said, "the controller never spoke") {
		t.Fatalf("the hold expired without naming the silence: %q", said)
	}
	if !strings.Contains(said, "reported before "+word.bound.String()) {
		t.Fatalf("the sentence did not carry the bound it expired at: %q", said)
	}
	// A controller that reported nothing leaves the last rung unknown, and
	// unknown is said as the fact it is rather than left as a blank in the
	// middle of the sentence.
	if !strings.Contains(said, `the last thing a person was told was "nothing"`) {
		t.Fatalf("a controller that said nothing at all was not reported as nothing: %q", said)
	}
}

// TestASilenceNamesTheLastRungAPersonWasTold is the other half of #971's ask: a
// controller that reported and then went quiet is a different defect from one
// that never reported at all, and the sentence has to tell them apart. The rung
// a person was last told about is carried through the silence in the words a
// person gets, detail and all.
func TestASilenceNamesTheLastRungAPersonWasTold(t *testing.T) {
	word := newControllersWord()
	word.heard(PhaseNews{Phase: PhaseAllSlow, Detail: "both lanes below pace"})
	word.arm(5 * time.Millisecond)

	<-word.spoken
	<-word.watched

	said := word.silence()
	if !strings.Contains(said, string(PhaseAllSlow)) || !strings.Contains(said, "both lanes below pace") {
		t.Fatalf("the silence did not name the last rung a person was told about: %q", said)
	}
}

// TestAWordSpokenInTimeIsNoSilence is the ordinary case every scenario in this
// package really runs, pinned so the bound can never call a working controller
// silent: the hold is armed at a ceiling no test would ever reach, the word is
// spoken, and there is nothing to report.
func TestAWordSpokenInTimeIsNoSilence(t *testing.T) {
	word := newControllersWord()
	word.arm(time.Hour)

	word.speak()
	<-word.spoken
	// The watcher goes when the hold opens, whichever of the two opened it, so
	// joining it is what makes the claim below a reading rather than a guess.
	<-word.watched

	if word.expired.Load() {
		t.Fatal("a hold the controller opened in time was reported as a silence")
	}
}
