package exec

import (
	"strings"
	"testing"

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
