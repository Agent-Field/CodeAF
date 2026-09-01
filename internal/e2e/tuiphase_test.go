//go:build e2e

package e2e

// tuiphase_test.go is the phase clock on a real screen: what this build says
// while it is waiting on a machine, and what it offers to do about it.
//
// WHY THIS IS A GLIMPSE AND NOT A WAIT, which is the whole shape of the file.
// Both sentences are about a machine that has GONE QUIET — one pinned lane that
// stopped answering, or every reachable lane believed slow at once — and a real
// router cannot be asked for that. A subtest that waited twenty seconds for a
// stall would be a subtest that fails on a good day, which is the one kind of
// red nobody chases twice. So the turn is real, the screen is real, and the
// sentences are caught when the run is slow enough to say them and written down
// as a FINDING when it is not. That is exactly what the live strip's three
// words already do next door in tui_e2e_test.go, and for the same reason.
//
// AND WHAT IS CAUGHT IS READ WHOLE. A glimpse that only logged would be a
// waiter in name: the moment either sentence IS on the screen, this file
// asserts the rest of it — the offer's key, the report's second half — because
// a question drawn without the key that answers it is the defect the surface's
// own arm states a law about (internal/tui3's phase.go).
//
// The needles come through [say] out of tuiwords_test.go's table like every
// other string this package waits for, and the untagged gate beside it reads
// this file back — so a row that stops being asked for here is a row that gate
// names, on the pull request that stopped asking.

import (
	"strings"
	"testing"
	"time"
)

// TestTUIPhaseClock drives one real turn and watches the clock over it.
//
// It is its own top-level test rather than a tenth subtest of [TestTUIE2E]
// because it is the only scenario here that asks nothing of the product and
// only reads what the product says about the wire underneath it: it passes on a
// run where nothing ever went slow, which no other subtest in this package
// does.
func TestTUIPhaseClock(t *testing.T) {
	requireTmuxAndKey(t)

	home := newHome(t, nil)
	seedProject(t, home, "waitseed", 21, 30*time.Minute)
	ws := newWorkspace(t, "waitws", false)
	r := start(t, "afe2e_phase", home, ws, tuiPlain, 40)

	r.waitFor(25*time.Second, say(t, "homeFootWord"))

	// A QUESTION WITH REAL WORK BEHIND IT, because the clock this file is about
	// only runs while an answer is owed. A one-word reply is a wait nobody has
	// time to read, let alone one the controller has time to weigh.
	ask := "plan a week of evening meals for four people and say why each night works"
	r.lit(ask)
	time.Sleep(600 * time.Millisecond)
	r.ctrlEnter()
	r.waitFor(30*time.Second, "plan a week of evening meals")

	// THE LIVE STRIP FIRST, so that a run which caught no wait can be told from
	// a run whose turn never started at all. A missed strip is a fast reply and
	// not a defect, so this only ever logs.
	if caught, ok := r.glimpse(20*time.Second,
		say(t, "homeAskThinkWord"), say(t, "homeAskWriteWord"), say(t, "homeAskRunWord")); ok {
		t.Logf("the turn is under way:\n%s", caught)
	} else {
		t.Logf("FINDING: no live strip caught — the phase clock had nothing to draw over")
	}

	// The two sentences are watched for together, because they are the two
	// answers to one question — is there anywhere better to be — and a run gets
	// at most one of them.
	waiting, caught := r.glimpse(modelPatience, say(t, "phaseSlowWord"), say(t, "phaseAllSlowWord"))
	switch {
	case !caught:
		t.Logf("FINDING: nothing went quiet on this run, so neither %q nor %q was drawn",
			say(t, "phaseSlowWord"), say(t, "phaseAllSlowWord"))
	case strings.Contains(waiting, say(t, "phaseSlowWord")):
		// A PIN THAT WENT QUIET IS ASKED ABOUT, and the key is the half a narrow
		// row keeps when it has spent everything else — so either spelling of
		// the question is the question, and neither is it missing.
		if !strings.Contains(waiting, say(t, "phaseOfferWord")) &&
			!strings.Contains(waiting, say(t, "phaseOfferKeyWord")) {
			t.Errorf("a machine went quiet and the row offered nothing to do about it — "+
				"no %q and no %q on:\n%s",
				say(t, "phaseOfferWord"), say(t, "phaseOfferKeyWord"), firstMatch(waiting, say(t, "phaseSlowWord")))
		}
		t.Logf("the question a pinned machine raised:\n%s", firstMatch(waiting, say(t, "phaseSlowWord")))
	default:
		// AND A WAIT NOTHING CAN END SAYS BOTH HALVES OR IT SAYS NOTHING USEFUL:
		// that every lane is slow is the reason, and that this build is still
		// waiting is what a person is being told.
		if !strings.Contains(waiting, say(t, "phaseWaitingWord")) {
			t.Errorf("the row said %q and then did not say what it was doing about it — no %q on:\n%s",
				say(t, "phaseAllSlowWord"), say(t, "phaseWaitingWord"),
				firstMatch(waiting, say(t, "phaseAllSlowWord")))
		}
		t.Logf("the wait nothing can end:\n%s", firstMatch(waiting, say(t, "phaseAllSlowWord")))
	}

	t.Logf("the screen the turn left behind:\n%s", r.capture())
}
