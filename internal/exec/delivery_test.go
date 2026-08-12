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

// The second thing the two documents have to agree on, and the one they used to
// make impossible between them: the law demanded the whole finished thing
// written out in the final message while the leaf's own budget capped that
// message at about three hundred words. A worker that had built and run a
// 288-line script could satisfy neither, stalled three times trying, and
// delivered a transcription while the output it had rendered went unmentioned.
//
// So both texts now say that where the work produced the thing, the produced
// thing is the answer — named, with its substance summarised — and that the
// demand for full text is what applies when nothing was produced that carries
// it. Whether a run produced anything is a fact about that run, so it is stated
// as something the worker judges rather than switched on from outside: the leaf
// system message is a shared prompt prefix and its bytes may not vary per run.
func TestNeitherLawDemandsFullTextOfSomethingTheRunProduced(t *testing.T) {
	flat := func(text string) string {
		return strings.ToLower(strings.Join(strings.Fields(text), " "))
	}
	for name, clause := range map[string]string{
		"a produced thing is itself the answer": "that produced thing is the answer",
		"an answer need not be made of words":   "some answers are not made of sentences",
	} {
		if law := flat(plan.DeliverInMessage); !strings.Contains(law, flat(clause)) {
			t.Errorf("the shared law no longer states that %s", name)
		}
		if leaf := flat(systemPrompt); !strings.Contains(leaf, flat(clause)) {
			t.Errorf("the leaf's own prompt no longer states that %s", name)
		}
	}
	if law := flat(plan.DeliverInMessage); !strings.Contains(law,
		"the demand for full text stands only where nothing was produced that carries the answer") {
		t.Error("the law no longer bounds its own full-text demand; it can contradict the leaf's message budget again")
	}
	if leaf := flat(systemPrompt); !strings.Contains(leaf, "do not retype it into the message") {
		t.Error("the leaf prompt no longer forbids transcribing a produced file into the capped message")
	}

	// The prompt-cache half of the same fix. A per-run fact in the system
	// message costs every leaf of a run its warm prefix, which is why the
	// artifact clause is phrased for the worker to apply rather than compiled
	// in by a caller that knows what the run produced.
	first, second := (&Linear{}).system(Task{}), (&Linear{}).system(Task{})
	if first != second {
		t.Error("the leaf system message is no longer byte-identical across leaves of a run")
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
