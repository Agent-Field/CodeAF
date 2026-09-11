package session

import (
	"strings"
	"testing"
	"time"
)

// A WAIT THAT RAN OUT SAYS SO, AND DOES NOT DIAGNOSE WHAT IT COULD NOT SEE.
//
// This is #967's whole lesson as a test. The helper it guards used to fail with
// `no node ran` — a claim about the scheduler — when all it observed was that
// nothing arrived before a deadline. On a box running another package's suite
// those are different facts, and the wrong one sent two lanes into the task
// graph and the wall law for an hour apiece before either of them looked at the
// clock. The words are pinned here rather than left to whoever next edits them,
// because the cost of this defect was entirely in the wording.
func TestAWaitThatRanOutSaysItTimedOutRatherThanNamingACause(t *testing.T) {
	said := awaitTimeoutWord("a node to run", 5*time.Second)
	for _, must := range []string{"waited 5s", "a node to run", "TIMEOUT", "Run it alone", "-timeout"} {
		if !strings.Contains(said, must) {
			t.Fatalf("a wait that ran out did not say %q · %s", must, said)
		}
	}
	// AND IT MAKES NO CLAIM ABOUT THE SEAM. These are the shapes the old message
	// was: a verdict on something the helper cannot observe.
	for _, never := range []string{"never started", "never ran", "no node ran", "never reached"} {
		if strings.Contains(said, never) {
			t.Fatalf("a wait that ran out diagnosed a cause it did not observe: %q · %s", never, said)
		}
	}
}

// AND THE PATIENCE COMES FROM THE RUN'S OWN BUDGET.
//
// PERF.md:24 bans a wall-clock threshold in a gate and gives the reason this
// file keeps proving: a stopwatch is a fact about the weather. A number typed
// into this package is the same stopwatch, so `-timeout` — which this repository
// already tells people to raise on a loaded box — is what buys room.
func TestTheWaitScalesWithTheRunsOwnTimeout(t *testing.T) {
	patience := awaitPatience(t)
	if patience < 5*time.Second {
		t.Fatalf("the wait fell under its floor: %s", patience)
	}
	if patience > time.Minute {
		t.Fatalf("the wait passed its ceiling, so one stuck seam could eat the run: %s", patience)
	}
	if deadline, ok := t.Deadline(); ok {
		if left := time.Until(deadline); left > 4*time.Minute && patience <= 5*time.Second {
			t.Fatalf("a generous -timeout bought no patience at all: %s left, %s given", left.Round(time.Second), patience)
		}
	}
}
