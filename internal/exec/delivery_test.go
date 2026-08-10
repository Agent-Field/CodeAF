package exec

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// The delivery law is stated once, in internal/plan, and rendered here in the
// leaf's own voice. Different words are allowed and a different rule is not:
// the contract written for the deliverable owner demanded the whole thing in
// the final message and never filed, while this prompt demanded under about
// three hundred words with the long version filed, and a run died over four
// gate rounds in the gap between them.
//
// What reconciles them is the split, not a length — the answer is never what
// gets filed, and the working always may be — so the split is the clause both
// documents have to state.
func TestTheLeafsDeliveryLinesStateTheSameLawAsThePlanners(t *testing.T) {
	flat := func(text string) string {
		return strings.ToLower(strings.Join(strings.Fields(text), " "))
	}
	for name, clause := range map[string]string{
		"the split between the answer and its working": "the split is between the answer and its working, " +
			"never between the answer and a pointer to the answer",
		"a pointer is not delivery": "a message that says where the answer lives instead of carrying it has delivered nothing",
	} {
		law := flat(plan.DeliverInMessage)
		if !strings.Contains(law, clause) {
			t.Fatalf("the shared law no longer states %s — this test is reading the wrong clause", name)
		}
		leaf := flat(systemPrompt + outputClause(Task{}))
		if !strings.Contains(leaf, clause) {
			t.Errorf("the leaf's own prompt contradicts the shared law on %s", name)
		}
	}
}

// The carve-out reaching the leaf. The offered address is the one place here
// that knows the shape of the ask, so it is the one place that may say the file
// is the deliverable — and it must never say what the law's other half forbids,
// which is that a file the person asked for is a place the answer was hidden.
func TestTheOfferedAddressAgreesWithTheCarveOut(t *testing.T) {
	clause := outputClause(Task{OutputHint: "07-review.md"})
	for name, phrase := range map[string]string{
		"the instruction's own address wins":  "If your instructions already say where the deliverable goes, that wins.",
		"an asked-for file is legitimate":     "only when they asked for a file",
		"the file never replaces the message": "never in place of it",
	} {
		if !strings.Contains(clause, phrase) {
			t.Errorf("the offered address no longer states that %s: %q missing", name, phrase)
		}
	}
	if !strings.Contains(plan.DeliverToNamedFile, "so the file IS the deliverable") {
		t.Fatal("the shared carve-out moved; the clause above is being read against nothing")
	}
}
