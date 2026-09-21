package session

import "testing"

// THE DEFAULT IS A FACT WORTH PINNING WHICHEVER WAY IT POINTS.
//
// The bash belt was made the default in #1335 and the default was moved back
// here, on a measured comparison rather than on anybody's reading. What that
// episode showed is that the default had never been asserted anywhere: it was
// carried only by the absence of a value in other tests, so it could move, and
// did, without one test in the tree saying a word about it.
//
// So the switch's whole answer is written out. An unset or blank variable is
// the older node belt; the exact word `bash` is the harness; nothing else
// reaches the harness, because the predicate is an equality and not a list.
func TestTheBeltIsTheNodeBeltUnlessTheWordIsExactlyBash(t *testing.T) {
	for _, row := range []struct {
		value string
		want  bool
		why   string
	}{
		{"", false, "unset or blank is the default, and the default is the node belt"},
		{"bash", true, "the one word that asks for the harness"},
		{"node", false, "a word naming the older belt is not the word for the harness"},
		{"legacy", false, "and neither is this one"},
		{"off", false, "nor this"},
		{"BASH", false, "the match is exact: an upper-case spelling does not reach the harness"},
		{"bashx", false, "and neither does a longer word that starts the same way"},
		{"true", false, "a word that means nothing here moves nobody"},
	} {
		t.Run(row.value, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", row.value)
			if got := bashBeltAsked(); got != row.want {
				t.Fatalf("CODEAF_TASK_BELT=%q: harness on = %v, want %v — %s", row.value, got, row.want, row.why)
			}
			// The exported door and the package's own reader are two halves of
			// one fact. A change that flipped one and not the other would leave
			// a run and the landing that judges it on different belts, which is
			// the exact thing the single-reader rule above bashBeltAsked exists
			// to prevent.
			if BashBeltAsked() != bashBeltAsked() {
				t.Fatalf("CODEAF_TASK_BELT=%q: the exported door and the package reader disagree", row.value)
			}
		})
	}
}

// SURROUNDING SPACE IS A PERSON'S TYPING. The predicate trims, so a value a
// shell carried a space into still asks for the harness; this is separate from
// the table above because it is a property of the reader rather than a row of
// the switch.
func TestSpaceAroundTheWordStillAsksForTheHarness(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "  bash  ")
	if !bashBeltAsked() {
		t.Fatal("a value with surrounding space did not reach the harness, but the predicate trims")
	}
}
