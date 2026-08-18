package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A REPLAYED CONVERSATION MUST NOT PUT WORDS IN A PERSON'S MOUTH.
//
// The line that wakes a turn — "task 7 could not be verified…" — is written by
// the SESSION and rides the user role, because that is the only role a model can
// be told something in. Live, this surface never draws it as the person's words:
// a woken turn writes no user line at all (followup.go's [app.startFollow]).
// Replayed, it did, because the journal had nothing but the role. session marks
// it now and hands it back as "aside" (its agent.go), and it lands in the lane
// this surface says everything of its own in.
func TestAReplayedAsideIsTheSurfacesOwnLaneAndNotThePersons(t *testing.T) {
	const note = "task 7 could not be verified: finished, but needs your look · transcript file:///t/7.jsonl"
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{
		{Role: "user", Text: "port the parser"},
		{Role: "aside", Text: note},
		{Role: "assistant", Text: "task 7 stopped short of the key table."},
	}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 80, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.workMode = config.WorkOpen
	a.touch()

	got := plain(frame(a))
	// The head of the line, because the row wraps at this width — what is being
	// asserted is that it is on screen and how it is drawn, not where it breaks.
	head := "task 7 could not be verified"
	if !strings.Contains(got, head) {
		t.Fatalf("the replayed conversation lost the line that started the turn:\n%s", got)
	}
	// THE PERSON'S GLYPH IS THE ASSERTION. It belongs to what they typed, and
	// exactly one line here was typed.
	you := plain(a.pal.youGlyph())
	if strings.Contains(got, you+head) {
		t.Fatalf("a line the session wrote is drawn as the person's:\n%s", got)
	}
	users := 0
	for i := range a.entries {
		if a.entries[i].kind == entryUser {
			users++
		}
		if a.entries[i].kind == entryUser && strings.Contains(a.entries[i].text, "task 7 could not be verified") {
			t.Fatalf("the wake note replayed as a user message: %q", a.entries[i].text)
		}
	}
	if users != 1 {
		t.Fatalf("%d user lines replayed, want 1 — the one that was typed", users)
	}
	// It is the surface's own lane: the dim "· " row [app.note] writes.
	aside := false
	for i := range a.entries {
		if a.entries[i].kind == entryNote && strings.Contains(a.entries[i].text, "task 7 could not be verified") {
			aside = true
		}
	}
	if !aside {
		t.Fatal("the session's own line is not in the session's own lane")
	}
}

// AND THE COMPACTION MARKER IS STILL A RULE. "note" is a different shape from
// "aside" — a marker the surface draws across the conversation, not a line
// somebody said — and the two must not be collapsed into one row.
func TestAReplayedNoteIsStillDrawnAsARule(t *testing.T) {
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{
		{Role: "note", Text: "compacted 12 messages"},
	}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 60, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.touch()

	if len(a.entries) == 0 || a.entries[0].kind != entryDivider {
		t.Fatalf("a compaction marker replayed as kind %d", a.entries[0].kind)
	}
}
