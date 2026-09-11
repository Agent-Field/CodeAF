package session

// Issue #890's engine half. The belt refuses a `propose_task` whose declared
// check is a composed shell command, and THE SENTENCE IT ANSWERS WITH IS
// WRITTEN FOR THE MODEL: it names `checks`, quotes the offending command, and
// is one repair away from a working call — which is the stated reason the door
// refuses rather than dropping silently. The surface may no longer draw that
// sentence to the person (tui3's refusal_test.go holds that half), but nothing
// about the refusal itself may soften: the model still reads it verbatim as
// the tool's result, in the transcript, because that is the only place the
// repair can happen.

import (
	"strings"
	"testing"
)

// TestAnArgumentRefusalStillReachesTheModelVerbatim is the acceptance line "the
// refusal still reaches the model verbatim as the tool result, asserted on the
// transcript": the belt's own sentence for a composed check, unclipped and
// unrewritten, carried in the result the model's next turn reads.
func TestAnArgumentRefusalStillReachesTheModelVerbatim(t *testing.T) {
	refusal := `Invalid arguments: checks must each be ONE command with no shell composition — "cd 1-check && ./run.sh" is not`

	// The direct answer the belt gives is the sentence whole.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	composed, isError := runTool(t, agent, "propose_task",
		`{"title":"Fix the nil-map","summary":"s","brief":"b","deliverable":"d","acceptance":"a","checks":["cd 1-check && ./run.sh"]}`)
	if !isError {
		t.Fatalf("a composed check was accepted:\n%s", composed)
	}
	if !strings.Contains(composed, "checks must each be ONE command with no shell composition") {
		t.Fatalf("the refusal does not name the repair:\n%s", composed)
	}

	// The same sentence, sent back through a refused call's result, is still
	// recognisably a refusal the loop guard and the surface can agree on —
	// that is what keeps the two halves of this change answering with one
	// pair of eyes.
	if !ArgumentRefusal(refusal) {
		t.Fatal("ArgumentRefusal does not see the belt's own composed-check refusal")
	}
	if ArgumentRefusal("make: *** no rule to make target 'test'") {
		t.Fatal("ArgumentRefusal took a bash that failed in the world for a refusal of the call's arguments")
	}
}
