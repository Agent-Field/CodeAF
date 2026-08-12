package exec

import (
	"strings"
	"testing"
)

// A single-leaf splice hands the worker a Brief that IS the Goal. Sending the
// goal again as a preamble doubles the prompt for zero information and, at the
// engine's size bands, priced one real job out of its own fast path (§14).
func TestTheGoalPreambleIsNotSentWhenTheBriefAlreadyCarriesIt(t *testing.T) {
	goal := "Fix issue #21: the normalizer drops diacritics on compound vowels."
	same := sweGoal(Task{Goal: goal, Brief: goal})
	if strings.Contains(same, "part of a larger goal") {
		t.Fatalf("brief == goal must not repeat the goal as a preamble:\n%s", same)
	}
	if !strings.Contains(same, goal) {
		t.Fatalf("the work itself went missing:\n%s", same)
	}
	distinct := sweGoal(Task{Goal: goal, Brief: "Write the vowel-table half of the fix."})
	if !strings.Contains(distinct, "part of a larger goal") {
		t.Fatalf("a brief narrower than the goal still deserves the goal:\n%s", distinct)
	}
}
