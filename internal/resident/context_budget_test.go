package resident

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// What a sentinel is told about what just happened is sized from the window it
// reads through. Unknown keeps the pair this file was written with, which is the
// whole of the rollback.
func TestTheRevisionEventBudgetsFromTheSentinelsWindow(t *testing.T) {
	result, failure := revisionBytes(0)
	if result != revisionResultBytes || failure != revisionFailureBytes {
		t.Fatalf("an unknown window gave (%d, %d), want the old pair (%d, %d)",
			result, failure, revisionResultBytes, revisionFailureBytes)
	}
	wideResult, wideFailure := revisionBytes(200_000)
	if wideResult <= result*8 {
		t.Fatalf("a 200k-token sentinel is shown %d bytes of a landed result", wideResult)
	}
	if wideFailure <= failure {
		t.Fatalf("the failure line stayed at %d while the result grew to %d", wideFailure, wideResult)
	}

	// And the event itself carries it: the same landing, read by two models,
	// arrives clipped for one and whole for the other.
	node := store.Node{ID: "job-n2", Title: "Survey the API"}
	long := strings.Repeat("é", 4000)
	if bounded := RevisionEvent(node, long, nil, "", 0); len(bounded) > revisionResultBytes+512 {
		t.Fatalf("an unsized event ran to %d bytes", len(bounded))
	}
	whole := RevisionEvent(node, long, nil, "", 200_000)
	if len(whole) <= revisionResultBytes+512 {
		t.Fatalf("a sized event was clipped to %d bytes anyway", len(whole))
	}
	if !strings.Contains(whole, long) {
		t.Fatal("the result was clipped inside a budget with room for it")
	}
}

// A claim-time division reads what landed through the planning model's window,
// not through a literal that predates the question.
func TestTheJITDigestPotIsSizedFromThePlanningWindow(t *testing.T) {
	if pot := (JITExpander{}).digestPot(); pot != jitDigestBytes {
		t.Fatalf("an expander with no window took %d bytes, want the old %d", pot, jitDigestBytes)
	}
	wide := JITExpander{ContextTokens: 200_000}.digestPot()
	if wide <= jitDigestBytes*8 {
		t.Fatalf("a 200k-token sub-planner is shown %d bytes of what landed", wide)
	}
	// One division, one picture: the two reads inside an expansion must agree.
	if again := (JITExpander{ContextTokens: 200_000}).digestPot(); again != wide {
		t.Fatalf("the pot moved between reads: %d then %d", wide, again)
	}
}
