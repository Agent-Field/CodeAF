package subharness

import (
	"strings"
	"testing"
	"time"
)

// flake is the shape the card and the runner are both tested against: a trigger
// that is hosted as a command, a worker, a branch with two arms, a bounded loop,
// a check and a gate. It is one harness rather than one per test so that a
// change to the model has one place to be felt.
func flake() Harness {
	return Harness{
		Id: Id{Name: "triage-flake", Desc: "chase a flaky test to a fix", Version: 2},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{"source": TriggerHosted, "args": "since, label"}},
				{Id: "name-it", Kind: KindAgentLoop, Fields: Fields{
					"brief": "name the test that failed and why", "tools": "read, grep", "max_turns": "8",
				}},
				{Id: "pick", Kind: KindBranch, Fields: Fields{"when": "contains flaky"}},
				{Id: "rerun", Kind: KindToolCall, Fields: Fields{"tool": "bash", "args": "go test -run TestFoo -count 20"}},
				{Id: "explain", Kind: KindAgentLoop, Fields: Fields{"brief": "say why it is not flaky"}},
				{Id: "tries", Kind: KindLoopUntil, Fields: Fields{"until": "ok", "max_rounds": "3"}},
				{Id: "check", Kind: KindVerify, Fields: Fields{"ladder": VerifyLoop, "check": "go test ./..."}},
				{Id: "land", Kind: KindHumanGate, Fields: Fields{"ask": "land the fix?"}},
			},
			Edges: []Edge{
				{"start", "name-it"}, {"name-it", "pick"},
				{"pick", "rerun"}, {"pick", "explain"},
				{"rerun", "tries"}, {"tries", "check"}, {"check", "land"},
			},
		},
		Whitelist: []string{"read", "grep", "bash"},
		Verify:    Verify{Ladder: VerifyLoop},
		Dyn:       Dyn{Ladder: DynBranch, Cap: 4},
	}
}

func TestTheCardNumbersTheStepsInTheOrderTheyRun(t *testing.T) {
	harness := flake()
	if err := Validate(harness); err != nil {
		t.Fatalf("the sample harness does not validate: %v", err)
	}
	card := Card(harness)
	lines := strings.Split(card, "\n")

	head := lines[0]
	for _, want := range []string{"triage-flake", "v2", "chase a flaky test to a fix"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the head line does not say %q: %q", want, head)
		}
	}

	// THE STEPS ARE THE WALK'S ORDER, numbered, one row each — so a person who
	// read the card top to bottom has read the run (run.go's topological walk).
	for at, id := range []string{"start", "name-it", "pick"} {
		if !hasRow(lines, itoaCard(at+1)) || !strings.Contains(card, id) {
			t.Fatalf("step %d (%s) is not a row of its own:\n%s", at+1, id, card)
		}
	}

	// THE BRANCH'S ARMS ARE LABELLED, because "it went down b" is a sentence
	// about a run and it needs a b on the card to be about.
	for _, want := range []string{"a → rerun", "b → explain"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the branch's arms are not on the card (%q):\n%s", want, card)
		}
	}

	// EVERY KIND SAYS THE FIELDS IT ACTUALLY USES.
	for _, want := range []string{
		"name the test that failed and why · read, grep · 8 turns",
		"when contains flaky",
		"bash · go test -run TestFoo -count 20",
		"until ok · up to 3",
		"loop · go test ./...",
		"land the fix?",
		"hosted · since, label",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}

	// AND THE FOOT IS THE BOUNDS, which is what approval is actually about.
	foot := lines[len(lines)-1]
	for _, want := range []string{"read · grep · bash", "verify  loop", "dynamism  branch (cap 4)"} {
		if !strings.Contains(foot, want) {
			t.Fatalf("the foot does not say %q: %q", want, foot)
		}
	}
}

func TestTheCardCallsAnUnregisteredHarnessADraft(t *testing.T) {
	harness := flake()
	harness.Id.Version = 0
	if card := Card(harness); !strings.Contains(card, "draft") {
		t.Fatalf("an unregistered harness does not say it is a draft:\n%s", card)
	}
}

func TestTheCardSaysWhenAHarnessHasNoTools(t *testing.T) {
	harness := Harness{
		Id:      Id{Name: "quiet", Version: 1},
		Program: Program{Nodes: []Node{{Id: "think", Kind: KindAgentLoop, Fields: Fields{"brief": "think"}}}},
	}
	if card := Card(harness); !strings.Contains(card, "no tools") {
		t.Fatalf("a harness with no whitelist does not say so:\n%s", card)
	}
}

// A card is what a person reads to find out a shape is wrong, so it has to
// survive the shapes Validate is about to refuse.
func TestTheCardDrawsAProgramThatCannotBeWalked(t *testing.T) {
	harness := flake()
	harness.Program.Edges = append(harness.Program.Edges, Edge{"land", "name-it"})
	if err := Validate(harness); err == nil {
		t.Fatal("a cycle was accepted, so this test is about nothing")
	}
	if card := Card(harness); !strings.Contains(card, "triage-flake") || !strings.Contains(card, "land") {
		t.Fatalf("a cyclic program drew no card:\n%s", card)
	}
}

func TestTheRunCardShowsThePathTheRunTook(t *testing.T) {
	card := RunCard(Trace{
		Id:      Id{Name: "triage-flake", Version: 2},
		Status:  StatusDeclined,
		Elapsed: 90 * time.Second,
		Spent:   1,
		Trail: []Trail{
			{Step: 1, Id: "name-it", Kind: KindAgentLoop, Out: "it is the reconciler", Elapsed: time.Second},
			{Step: 2, Id: "check", Kind: KindVerify, Err: "go test ./...: exit 1"},
			{Step: 3, Id: "land", Kind: KindHumanGate, Out: "declined"},
		},
	})
	for _, want := range []string{
		"triage-flake · v2 · declined", "it is the reconciler",
		"go test ./...: exit 1", "✗", "✓", "spent  1",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the run card does not say %q:\n%s", want, card)
		}
	}
}

// A trace written by a plain Run carries its own status; one hand-built from an
// older page does not, and is read rather than left blank.
func TestTheRunCardReadsAStatusOffATraceThatHasNone(t *testing.T) {
	card := RunCard(Trace{Id: Id{Name: "old", Version: 1}, Err: "node %q failed"})
	if !strings.Contains(card, "failed") {
		t.Fatalf("a trace with an error but no status does not read as failed:\n%s", card)
	}
}

func hasRow(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			return true
		}
	}
	return false
}

func itoaCard(n int) string { return string(rune('0' + n)) }
