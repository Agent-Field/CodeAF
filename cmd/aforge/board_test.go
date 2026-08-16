package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A board note must survive the round trip and nothing else may read as one:
// questions, receipts, briefs and progress posts all anchor to nodes too, and
// a board that mistook any of them for a worker's line would inject a
// question card into a sibling's transcript as testimony.
func TestOnlyAWorkersSharedLineReadsBackAsABoardNote(t *testing.T) {
	body := jobNoteBody("the totals column is EUR, not USD")
	note, ok := jobNoteLine(store.Message{Role: store.RoleAgent, Body: body})
	if !ok || note != "the totals column is EUR, not USD" {
		t.Fatalf("round trip = %q, %v", note, ok)
	}

	for name, message := range map[string]store.Message{
		"a user steering line": {Role: store.RoleUser, Body: body},
		"an anchored question": {Role: store.RoleAgent, Body: body, QuestionSeq: 7},
		"a command receipt":    {Role: store.RoleAgent, Body: body, CommandSeq: 7},
		"a progress post":      {Role: store.RoleAgent, Body: body, Progress: &store.MessageProgress{Phase: "compile"}},
		"an arrival brief":     {Role: store.RoleAgent, Body: body, Brief: &store.Brief{}},
		"plain agent prose":    {Role: store.RoleAgent, Body: "just talking, no marker"},
	} {
		if _, ok := jobNoteLine(message); ok {
			t.Errorf("%s read back as a board note", name)
		}
	}
}
