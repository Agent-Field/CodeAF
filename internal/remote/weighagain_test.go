package remote

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE WEIGHT IS STATED AGAIN WHEN THE NEXT REQUEST SPEAKS, once per batch. The
// facts already go out on every tool end, but the batch's results join the
// conversation only after the last end is published, so the reading taken there
// is one step early for a surface drawing the weight of the request in flight.
// The first word of the next request is the first moment the results are
// certainly in; after that, nothing more is owed until the next batch ends.
func TestTheWeightIsStatedAgainWhenTheNextRequestSpeaks(t *testing.T) {
	sess := &Session{}
	for i, step := range []struct {
		kind session.EventKind
		want bool
	}{
		{session.EventTextDelta, false},
		{session.EventToolEnd, false},
		{session.EventToolFailed, false},
		{session.EventReasoning, true},
		{session.EventTextDelta, false},
		{session.EventToolEnd, false},
		{session.EventTurnDone, false},
		{session.EventTextDelta, false},
	} {
		if got := sess.weighsAgainLocked(session.Event{Kind: step.kind}); got != step.want {
			t.Fatalf("step %d (%v): weighs again = %v, want %v", i, step.kind, got, step.want)
		}
	}
}

// A SCRIPT'S OWN CALLS END INSIDE ONE STEP. Their results never join the
// conversation, so their end owes no new weighing; only the script's does.
func TestAScriptsNestedEndOwesNoWeighing(t *testing.T) {
	sess := &Session{}
	sess.weighsAgainLocked(session.Event{Kind: session.EventToolEnd, Parent: "s"})
	if sess.weighsAgainLocked(session.Event{Kind: session.EventReasoning}) {
		t.Fatal("a script's nested call end owed the next request a weighing")
	}
}
