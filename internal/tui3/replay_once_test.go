package tui3

// THE RECORD IS DRAWN ONCE, HOWEVER MANY TIMES IT IS REPLAYED.
//
// [session.EarlierHistory] states the law this pins, and states it three times
// over: the file holds the same conversation twice, once as it happened and
// once as the pass rewrote it, and "drawing both would show the session to
// itself twice". replay.go says the same thing from the surface side, on
// roomRecord and on backfill: the region is drawn and the copy is never a row.
//
// [app.replayList] could break it on its own. It keeps everything already on
// screen, which is right for what it was written for, a person typing while the
// record is in flight. But rows a previous replay drew from this same record
// are not something said afterwards, and keeping them puts the conversation
// above itself.
//
// THE PRODUCTION ROAD TO A SECOND REPLAY IS UNPROVEN. Every caller of
// [app.attachConversation] clears the drawn conversation first, and the off-loop
// read folds with here=false for a conversation the person has left. What is
// asserted here is the property, which replayList owes whatever calls it.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// tellTwice counts how many times each line of a conversation is on the screen.
func tellTwice(t *testing.T, a *app, lines []string) {
	t.Helper()
	page := drawnText(a)
	for _, line := range lines {
		if n := strings.Count(page, line); n != 1 {
			t.Errorf("%q is on the screen %d times, want once", line, n)
		}
	}
	if n := strings.Count(page, strings.Fields(seamMark)[0]); n != 1 {
		t.Errorf("the seam is drawn %d times, want once", n)
	}
}

func TestAReplayOntoADrawnConversationDrawsItOnce(t *testing.T) {
	above, rewritten, since := namedPast("old", 6), shortened("old", 6), namedPast("kept", 6)
	record := append(append([]session.DisplayEntry(nil), rewritten...), since...)
	a := newTestApp(&fakeAgent{model: "m", past: record, earlier: above, earlierFloor: len(rewritten)})
	a.entries = nil
	a.replay()
	a.touch()
	scrollToTop(t, a)

	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, "old answer "+itoa(i), "kept answer "+itoa(i))
	}
	// The conversation is told once BEFORE the second replay, which is what
	// makes the count below a statement about the replay and not about the
	// fixture.
	tellTwice(t, a, lines)
	if t.Failed() {
		t.Fatalf("the fixture was already drawing the conversation twice:\n%s", drawnText(a))
	}

	// The record arrives again, as an attach hands it back.
	a.replayList(record)
	a.touch()
	scrollToTop(t, a)
	tellTwice(t, a, lines)
}

// AND THE SENTENCE SOMEBODY TYPED WHILE THE RECORD WAS IN FLIGHT IS STILL
// THERE, which is the whole reason the replay keeps anything at all. A fix that
// dropped it would pass the test above and lose the thing that test is guarding.
func TestASentenceTypedWhileTheRecordWasComingSurvivesTheReplay(t *testing.T) {
	record := namedPast("kept", 6)
	a := newTestApp(&fakeAgent{model: "m", past: record})
	a.entries = nil
	a.replay()
	a.touch()

	said := "typed while the record was still coming"
	a.entries = append(a.entries, entry{kind: entryUser, text: said, turn: a.turn + 1})
	a.touch()

	a.replayList(record)
	a.touch()

	page := drawnText(a)
	if !strings.Contains(page, said) {
		t.Fatalf("the replay buried a sentence the person had already typed:\n%s", page)
	}
	if n := strings.Count(page, "kept answer 0"); n != 1 {
		t.Fatalf("the conversation is on the screen %d times, want once", n)
	}
}
