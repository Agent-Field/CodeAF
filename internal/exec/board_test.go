package exec

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The share tool is a channel to siblings, so it exists exactly when siblings
// do: a single-worker errand never pays the schema, and a worker holding the
// tool pays one line's rent, bounded, however much it wanted to say.
func TestShareExistsOnlyWhereSomebodyIsListening(t *testing.T) {
	alone := NewToolbox(workspace(t), 1, nil)
	for _, definition := range alone.Definitions() {
		if definition.Function.Name == "share" {
			t.Fatal("a worker with no siblings is carrying the share schema")
		}
	}
	if result := alone.Execute(t.Context(), "share", `{"line":"anyone there?"}`); !result.IsError {
		t.Fatal("sharing with nobody succeeded")
	}

	var heard string
	together := NewToolbox(workspace(t), 1, nil)
	together.share = func(line string) error { heard = line; return nil }
	found := false
	for _, definition := range together.Definitions() {
		found = found || definition.Function.Name == "share"
	}
	if !found {
		t.Fatal("a worker with siblings was not offered the share tool")
	}
	if result := together.Execute(t.Context(), "share", `{"line":"the export is semicolon-delimited"}`); result.IsError {
		t.Fatalf("share failed: %s", result.Content)
	}
	if heard != "the export is semicolon-delimited" {
		t.Fatalf("the line arrived as %q", heard)
	}

	// One line means one line: the bound clips, never rejects.
	long := strings.Repeat("x", shareLineBytes*3)
	if result := together.Execute(t.Context(), "share", `{"line":"`+long+`"}`); result.IsError {
		t.Fatalf("a long line was rejected instead of clipped: %s", result.Content)
	}
	if len(heard) > shareLineBytes+len("…") {
		t.Fatalf("a shared line kept %d bytes against a %d bound", len(heard), shareLineBytes)
	}
}

// A sibling's discovery is testimony, not instruction: it reaches the
// transcript in its own framing, before the user's own steering, and an empty
// board costs nothing.
func TestTheBoardArrivesAsTestimonyAndNeverAsTheUsersVoice(t *testing.T) {
	notes := []string{"parse_and_filter: the CSV has a duplicated header row"}
	task := Task{
		Board: func() []string { defer func() { notes = nil }(); return notes },
		Steer: func() []string { return []string{"make it about the sea"} },
	}
	var messages []ai.Message
	steered := readSteering(task, &messages, &tracer{})
	if steered != 1 {
		t.Fatalf("steered = %d, want the user's one line", steered)
	}
	if len(messages) != 2 {
		t.Fatalf("delivered %d messages, want board note + steering", len(messages))
	}
	board := messages[0].Content[0].Text
	if !strings.Contains(board, "another worker on this same job") || !strings.Contains(board, "duplicated header row") {
		t.Fatalf("the board note lost its framing:\n%s", board)
	}
	if strings.Contains(board, "Guidance from the user") {
		t.Fatal("a sibling's note was framed as the user's words")
	}
	if guidance := messages[1].Content[0].Text; !strings.Contains(guidance, "Guidance from the user") {
		t.Fatalf("the user's steering lost its framing:\n%s", guidance)
	}
}

// The brief tells a worker with siblings WHEN to share — at the moment of
// discovery — and says nothing about sharing to a worker who has nobody to
// tell. The measured miss: a sibling found the duplicated row while the
// revenue worker was still summing, and the total shipped wrong.
func TestOnlyAWorkerWithSiblingsIsToldToShareDiscoveries(t *testing.T) {
	linear := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute)
	withSiblings := linear.brief(Task{Brief: "sum the revenue", Share: func(string) error { return nil }})
	if !strings.Contains(withSiblings, "share it (the share tool) before you continue") {
		t.Fatalf("a worker with siblings was never told when to share:\n%s", withSiblings)
	}
	alone := linear.brief(Task{Brief: "sum the revenue"})
	if strings.Contains(alone, "share tool") {
		t.Fatalf("a worker with no siblings was told about a channel it does not have:\n%s", alone)
	}
}

// Measured live: asked to compute revenue FROM a CSV, a worker rewrote the CSV
// itself — cents became dollars, the duplicate row vanished, and the original
// evidence was gone. The board saved that run (the transformer's note warned
// the others), but the mutation is the sin: evidence is never edited.
func TestTheLeafIsToldEvidenceIsNeverEdited(t *testing.T) {
	for _, required := range []string{
		"material you were asked to read, analyse, or judge\nis evidence, and evidence is never edited",
		"Only what the ask\nitself asks you to change is yours to change",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Errorf("the leaf prompt lost the evidence law: %q", required)
		}
	}
}

// Measured live on the cross-file cell: the worker read the VAT note, restated
// it completely — "regionB and regionC amounts are pre-VAT (20% to add)" — and
// then summed all three files plain. The rule survived into prose and died
// before the arithmetic. Same model, same files, single competitor context:
// applied. The law targets exactly the restate-then-drop shape.
func TestTheLeafIsToldARestatedRuleIsAnAppliedRule(t *testing.T) {
	for _, required := range []string{
		"A rule you restated is a rule you apply",
		"in the arithmetic and not only in the\nprose",
		"read your\nown answer against every rule you noted along the way",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Errorf("the leaf prompt lost the restated-rule law: %q", required)
		}
	}
}
