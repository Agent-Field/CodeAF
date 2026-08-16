package exec

import (
	"strings"
	"testing"
)

// flatten is the reading a model does: whitespace is not meaning.
func flatten(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// The leak this pins, in the words it was measured in. A validation battery ran
// six real tasks through the product and three of the deliverables opened on
// the leaf's own checking — "487 words — within reasonable tolerance of 500…",
// "Files n3, n7, n8 are empty…", "I now have comprehensive information. Let me
// compile the deliverable." — with the researched answer underneath.
//
// The prompt asked for the checking in three places and never said where it
// goes, so it went into the delivery. The paragraph this test guards gives it a
// destination and names what may cross over, which is the part that stops the
// rule from contradicting the verification clauses above it: evidence for a
// claim is still substance, and a gap is still worth a sentence.
func TestTheLeafIsToldWhereItsOwnCheckingGoes(t *testing.T) {
	prompt := flatten(systemPrompt)
	for name, clause := range map[string]string{
		"the working has a destination that is not the delivery": "working is for the record rather than for the delivery",
		"a corrected answer crosses over":                        "the changed answer is what you hand over",
		"a real gap crosses over":                                "one sentence names the gap",
		"evidence for a claim crosses over":                      "it goes beside that claim",
		"narrated verification is not proof":                     "the deliverable arriving second",
	} {
		if !strings.Contains(prompt, flatten(clause)) {
			t.Errorf("the leaf prompt no longer states that %s: %q missing", name, clause)
		}
	}

	// The paragraph must not undo the three clauses that ask for the checking in
	// the first place; a leaf told not to check is a worse leaf than one that
	// narrates.
	for name, clause := range map[string]string{
		"a check is still run before finishing": "run that check before you finish",
		"verified is still earned":              "verified is a word you earn",
		"a noted rule is still re-read":         "read your own answer against every rule you noted",
	} {
		if !strings.Contains(prompt, flatten(clause)) {
			t.Errorf("the routing rule was written over the instruction to %s", name)
		}
	}
}

// THE OTHER SIDE OF THE SCALE. Verification is asked for in three separate
// paragraphs of this prompt and again in every model-authored method; the bound
// on it — that a green check is a finished question — was nine words on the end
// of one sentence, and a leaf reading the two weighed them the way they were
// weighed. Measured against the harness we benchmark on, that imbalance is a
// large part of a 2-4× turn gap at equal quality: the same check run twice, the
// same file read again to confirm what the first read said.
//
// The repair is weight rather than volume. The stop keeps its paragraph and gets
// its own sentences inside it, and it says WHY in the terms that make it stick —
// a fact already in hand, bought a second time with the person's money.
func TestTheStopAfterGreenCarriesItsOwnSentenceWeight(t *testing.T) {
	prompt := flatten(systemPrompt)
	for name, clause := range map[string]string{
		"a green check is a finished question":  "a check that came back green is a finished question",
		"asking again is spending their money":  "asking it again spends the person's money to re-learn a fact you already have",
		"the second answer was never worth it":  "nothing you learn the second time was worth the first",
		"and the instruction itself is present": "once a check passes, move on",
	} {
		if !strings.Contains(prompt, flatten(clause)) {
			t.Errorf("the anti-spiral clause no longer states that %s: %q missing", name, clause)
		}
	}
	// In the same paragraph as the demand it bounds. A stop that has drifted into
	// a paragraph of its own is a rule a leaf reads separately from the rule it
	// qualifies, which is how the imbalance is read back in.
	demand := strings.Index(prompt, flatten("run that check before you finish"))
	stop := strings.Index(prompt, flatten("once a check passes, move on"))
	verified := strings.Index(prompt, flatten("verified is a word you earn"))
	if demand < 0 || stop < demand || stop > verified {
		t.Error("the stop left the paragraph whose demand it bounds")
	}
}

// The cache law, restated against this edit. The paragraph is a property of
// every leaf, so it belongs in the shared prefix and may not be compiled in per
// run — TestEveryLeafOfARunSharesOneSystemMessage owns that property, and this
// only pins that the new text is in the message that test compares.
func TestTheCheckingRuleLivesInTheSharedPrefix(t *testing.T) {
	shared := (&Linear{}).system(Task{}, nil)
	if !strings.Contains(flatten(shared), flatten("working is for the record rather than for the delivery")) {
		t.Fatal("the checking rule is not in the assembled system message, so no leaf ever reads it")
	}
}

// The output contract, and the inconsistency that made it worth strengthening:
// of twelve leaves of one job given an address each, nine wrote their file and
// three kept the result only in the message. The reconciler downstream read
// files, found three missing, and narrated instead of delivering.
//
// The repair is not to force the file — an offered address that becomes an
// order is the litter this clause was written to stop. It is to make the
// message sufficient either way, so a sibling's choice cannot starve a reader,
// and to fix the address when a file IS written so the name is never invented.
func TestTheOfferedAddressCannotStarveTheReaderDownstream(t *testing.T) {
	clause := outputClause(Task{OutputHint: "07-review.md"})
	for name, phrase := range map[string]string{
		"the message stands alone whether or not the file is written": "Whether or not you write that file, the message carries the whole answer on its own",
		"a later reader is never required to open a file":             "nobody reading after you can be required to open one",
		"the offered address is the only address":                     "it is that one address and no other",
	} {
		if !strings.Contains(clause, phrase) {
			t.Errorf("the offered address no longer states that %s: %q missing", name, phrase)
		}
	}
	if !strings.Contains(clause, "07-review.md") {
		t.Error("the offered address stopped naming the path it offers")
	}
}
