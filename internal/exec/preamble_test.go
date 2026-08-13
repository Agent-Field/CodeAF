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
