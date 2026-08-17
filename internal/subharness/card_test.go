package subharness

import (
	"strings"
	"testing"
	"time"
)

func TestCardNumbersTheStepsAndWritesTheShapeInline(t *testing.T) {
	harness := sample()
	Clamp(&harness)
	harness.Version = 2
	card := Card(harness)
	lines := strings.Split(card, "\n")

	head := lines[0]
	for _, want := range []string{"triage-flake", "v2", "chase a flaky test to a fix"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the head line does not say %q: %q", want, head)
		}
	}

	// THE TOP-LEVEL STEPS ARE NUMBERED, in order, one row each.
	for at, want := range []string{"1", "2", "3", "4", "5"} {
		if !hasRowStarting(lines, want+"   ") {
			t.Fatalf("step %d is not numbered on a row of its own:\n%s", at+1, card)
		}
	}

	// THE BRANCH'S ARMS ARE LABELLED and its condition is on the arm, so a
	// person can point at the path a run took.
	if !strings.Contains(card, "a   contains flaky") {
		t.Fatalf("the branch's arm is not on the card:\n%s", card)
	}
	if !strings.Contains(card, "else") {
		t.Fatalf("the branch's else is not on the card:\n%s", card)
	}

	// THE LOOP SAYS ITS CONDITION AND ITS BOUND, and its body is under it.
	if !strings.Contains(card, "until ok · up to 3") {
		t.Fatalf("the loop's bound is not on the card:\n%s", card)
	}

	// THE VERIFY SAYS ITS RUNG AND ITS CHECK.
	if !strings.Contains(card, "verify") || !strings.Contains(card, "loop · go test ./...") {
		t.Fatalf("the check is not on the card:\n%s", card)
	}

	// THE GATE SAYS IT OFFERS THE ESCALATION.
	if !strings.Contains(card, "land the fix? · intervene") {
		t.Fatalf("the gate does not offer to be taken over:\n%s", card)
	}

	// AND THE FOOT IS THE BOUNDS, which is what approval is actually about.
	foot := lines[len(lines)-1]
	for _, want := range []string{"read · grep · bash", "verify  loop", "dynamism  width (cap 4)"} {
		if !strings.Contains(foot, want) {
			t.Fatalf("the foot does not say %q: %q", want, foot)
		}
	}
}

func TestCardCallsAnUnregisteredHarnessADraft(t *testing.T) {
	harness := sample()
	harness.Version = 0
	if !strings.Contains(Card(harness), "draft") {
		t.Fatalf("an unregistered harness does not say it is a draft:\n%s", Card(harness))
	}
}

func TestCardSaysWhenAHarnessHasNoTools(t *testing.T) {
	harness := Harness{Name: "quiet", Program: []Node{{ID: "think", Kind: KindAgentLoop, Prompt: "think"}}}
	Clamp(&harness)
	if !strings.Contains(Card(harness), "no tools") {
		t.Fatalf("a harness with no whitelist does not say so:\n%s", Card(harness))
	}
}

func TestRunCardShowsThePathTheRunTook(t *testing.T) {
	start := time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC)
	card := RunCard(Run{
		Harness: "triage-flake", Version: 2, Status: StatusDeclined,
		Started: start, Finished: start.Add(90 * time.Second),
		Nodes: []Trace{
			{ID: "look", Node: "look", Kind: KindAgentLoop, OK: true, Output: "it is the reconciler"},
			{ID: "check#2", Node: "check", Kind: KindVerify, Round: 2, OK: false, Note: "loop"},
			{ID: "land", Node: "land", Kind: KindHumanGate, OK: true, Answer: "declined"},
		},
	})
	for _, want := range []string{"triage-flake · v2 · declined", "round 2", "declined", "✗", "✓"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the run card does not say %q:\n%s", want, card)
		}
	}
}

// hasRowStarting reports whether any line begins with the prefix, ignoring the
// indent — the card's rows are padded, and a test that asserted the padding
// would be a test of the column widths.
func hasRowStarting(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			return true
		}
	}
	return false
}
