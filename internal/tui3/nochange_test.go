package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CARRY-ON ANSWERED [session.NoChangeReply] LEAVES THE ANSWER BEFORE IT
// STANDING (#1065). The measured turn folded down to its last words, and its
// last words were the model arguing with the completion check — so the table
// the person asked for sat inside the chip, and only the argument stood.
//
// The events are the session's own order on that road: the answer and its
// boundary, the carry-on's notice, the token and its boundary. It drives the
// reducer directly, as the caption tests do, because what is pinned is what a
// confirmed response leaves in the transcript and what the fold then finds.
func TestANoChangeReplyLeavesThePriorAnswerAsTheFoldsAnswer(t *testing.T) {
	const table = "| seat | model |\n| --- | --- |\n| one | deepseek/deepseek-v4.1-flash |\n"
	f := &feed{live: -1, think: -1, turn: 1}
	f.entries = append(f.entries, entry{kind: entryUser, text: "compare the models per seat", turn: 1})
	runBatch(f, "read", "c1")
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: table})
	f.ingest(session.Event{Kind: session.EventAssistantDone})
	f.ingest(session.Event{Kind: session.EventNotice, Text: "the ask is not finished · carrying on rather than stopping here"})
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: session.NoChangeReply})
	f.ingest(session.Event{Kind: session.EventAssistantDone})

	answer := -1
	for i, e := range f.entries {
		if e.kind != entryAssistant || strings.TrimSpace(e.text) == "" {
			continue
		}
		if session.IsNoChangeReply(e.text) {
			t.Fatalf("the %q reply is still a block the person reads", session.NoChangeReply)
		}
		answer = i
	}
	if answer < 0 || f.entries[answer].text != table {
		t.Fatalf("the last words left standing are not the table: %+v", f.entries)
	}
	folds := deriveWorkfolds(f.entries, 0)
	if len(folds) != 1 {
		t.Fatalf("the carried-on turn derived %d folds, want 1", len(folds))
	}
	for _, fold := range folds {
		if fold.answer != answer {
			t.Fatalf("the fold answers with block %d, want the table at %d", fold.answer, answer)
		}
	}
	if workEntry(f.entries, folds, answer) {
		t.Fatal("the table was folded away as work")
	}
}
