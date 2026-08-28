package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// AN INTERLEAVED STREAM IS ONE THOUGHT AND ONE ANSWER, NOT A SAW BLADE.
//
// The report this file is written from: a subharness design thread on a model
// that puts reasoning and answer on the wire interleaved, a few tokens of each
// at a time. The surface treated every hand-off as a new phase — each reasoning
// delta closed the streaming reply and opened a fresh thinking block, so a
// single sentence arrived as "thre", a `thought for 0s · 2 tok` row, then "ad
// gets saved…", all the way down the page. The rule these tests pin: a text
// delta settles the thought block but does not let go of it, so the next
// reasoning delta grows the same block and the answer streams on unbroken; only
// a real boundary — a tool call, the turn ending — seals the block so a
// genuinely new stretch of thinking earns a row of its own.

// interleaved is the wire shape that broke: reasoning and answer alternating
// mid-word, then a settle.
func interleaved() []session.Event {
	return []session.Event{
		text(session.EventReasoning, "the card should be dropped "),
		text(session.EventTextDelta, "Understood — nothing thre"),
		text(session.EventReasoning, "and the brief restarted "),
		text(session.EventTextDelta, "ad gets saved unless you approve a card."),
		text(session.EventReasoning, "so say that plainly"),
		text(session.EventTextDelta, " The new brief starts its own thread."),
		{Kind: session.EventTurnDone},
	}
}

func TestAnInterleavedStreamKeepsOneThoughtAndOneUnbrokenAnswer(t *testing.T) {
	_, a := wired(interleaved())
	typeLine(t, a, "stop this design")

	var thoughts, answers []string
	for _, e := range a.entries {
		switch e.kind {
		case entryThinking:
			thoughts = append(thoughts, e.text)
		case entryAssistant:
			answers = append(answers, e.text)
		}
	}
	if len(thoughts) != 1 {
		t.Fatalf("an interleaved turn left %d thinking blocks, want 1: %q", len(thoughts), thoughts)
	}
	if want := "the card should be dropped and the brief restarted so say that plainly"; thoughts[0] != want {
		t.Fatalf("the one block did not gather every reasoning delta:\n got %q\nwant %q", thoughts[0], want)
	}
	if len(answers) != 1 {
		t.Fatalf("the answer was sawed into %d blocks, want 1: %q", len(answers), answers)
	}
	if !strings.Contains(answers[0], "nothing thread gets saved") {
		t.Fatalf("the word the hand-off split is still split: %q", answers[0])
	}
}

func TestAToolCallSealsTheThoughtSoTheNextOneEarnsItsOwnRow(t *testing.T) {
	_, a := wired([]session.Event{
		text(session.EventReasoning, "read the loader first"),
		text(session.EventTextDelta, "Looking."),
		toolBegin("read", "read internal/config/load.go"),
		{Kind: session.EventToolEnd, Tool: "read", Output: "package config"},
		text(session.EventReasoning, "now the fix is clear"),
		text(session.EventTextDelta, " The map is never made."),
		{Kind: session.EventTurnDone},
	})
	typeLine(t, a, "fix the nil map")

	var thoughts int
	for _, e := range a.entries {
		if e.kind == entryThinking {
			thoughts++
		}
	}
	if thoughts != 2 {
		t.Fatalf("thinking on either side of a tool call left %d blocks, want 2", thoughts)
	}
}

func TestARoomsInterleavedStreamKeepsOneThoughtAndOneAnswer(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"design the subharness"}`,
	)
	a.openRoom(7, "designing a subharness")
	a.touch()

	for _, ev := range interleaved() {
		drive(t, a, roomEventMsg{gen: a.room.gen, ev: ev})
	}

	var thoughts, answers []string
	for _, e := range a.room.entries {
		switch e.kind {
		case entryThinking:
			thoughts = append(thoughts, e.text)
		case entryAssistant:
			answers = append(answers, e.text)
		}
	}
	if len(thoughts) != 1 {
		t.Fatalf("the room grew %d thinking blocks off one interleaved turn, want 1:\n%s",
			len(thoughts), roomText(a))
	}
	if len(answers) != 1 || !strings.Contains(answers[0], "nothing thread gets saved") {
		t.Fatalf("the room's answer did not stream unbroken: %q", answers)
	}
}
