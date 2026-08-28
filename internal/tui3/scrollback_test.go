package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DEFECT THESE TESTS PIN: a resumed conversation could not be scrolled back
// through. Two separate things were suspected of it and only one turned out to
// be true, so both are written down here — the one that was broken so it stays
// fixed, and the one that was not so nobody has to suspect it twice.
//
//   - The replay drew the last [replayTail] blocks and nothing else, and there
//     was no way to ask for more. Scrolling up reached the top of those forty
//     and stopped, with an hour of conversation sitting in the journal
//     underneath it. That is what [app.backfill] answers.
//   - Following the live edge was suspected of yanking a reader back down when
//     a turn streamed or when they typed. It does not, and the tests at the
//     bottom of this file are what keep it that way.

// longPast is a journal with `turns` of the person's messages in it, each one
// answered. The first message names itself so a test can ask whether the
// beginning of the conversation is on screen.
func longPast(turns int) []session.DisplayEntry {
	out := make([]session.DisplayEntry, 0, turns*2)
	for i := 0; i < turns; i++ {
		out = append(out,
			session.DisplayEntry{Role: "user", Text: "question " + itoa(i)},
			session.DisplayEntry{Role: "assistant", Text: "answer " + itoa(i)},
		)
	}
	return out
}

// drawnText is everything the transcript is currently drawing, whether it is on
// the frame or above it. It is the row list rather than the window because the
// question these tests ask is what has been MATERIALIZED, not what fits.
func drawnText(a *app) string { return strings.Join(plainRows(a), "\n") }

// scrollToTop scrolls up a page at a time until the window stops moving, which
// is what a person's hand does. It is bounded so a backfill that never ended
// fails as a test rather than as a hang.
func scrollToTop(t *testing.T, a *app) {
	t.Helper()
	for i := 0; i < 400; i++ {
		before := len(a.visible(a.bodyWidth()))
		beforeOffset := a.offsetFor(before, a.viewHeight())
		scrollBy(t, a, -a.scrollPage())
		after := len(a.visible(a.bodyWidth()))
		if after == before && a.offsetFor(after, a.viewHeight()) == beforeOffset {
			return
		}
	}
	t.Fatal("scrolling up never reached a top")
}

// scrollBy follows the page command the same way Bubble Tea does. History is
// deliberately not materialized inside [app.scroll], so a test that stops at
// the returned command would be testing the input handler rather than scrolling.
func scrollBy(t *testing.T, a *app, delta int) {
	t.Helper()
	for _, msg := range runCmd(a.scroll(delta)) {
		drive(t, a, msg)
	}
}

// ── S1: the whole conversation is reachable ─────────────────────────────────

// A RESUMED CONVERSATION OPENS ON ITS TAIL, which is the bargain [replayTail]
// makes and is not the defect. This states the starting position the test below
// scrolls out of.
func TestAResumedConversationOpensOnItsTail(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	if !strings.Contains(drawnText(a), "question 79") {
		t.Fatalf("the newest message is not drawn:\n%s", drawnText(a))
	}
	if strings.Contains(drawnText(a), "question 0") {
		t.Fatal("the whole conversation was drawn on open — the tail cap is gone")
	}
	if !a.moreHistory() {
		t.Fatal("the surface does not know there is more conversation above")
	}
}

// SCROLLING UP REACHES THE BEGINNING. This is the bug Santosh reported: he
// resumed a chat, scrolled up, and hit a wall a long way short of where the
// conversation actually started.
func TestScrollingUpReachesTheBeginningOfAResumedConversation(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	scrollToTop(t, a)

	drawn := drawnText(a)
	if !strings.Contains(drawn, "question 0") {
		t.Fatalf("scrolling up never reached the first message:\n%s", drawn)
	}
	// And the whole of it is there, not just the two ends.
	for _, want := range []string{"question 0", "question 40", "question 79"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q is missing from the scrolled-back conversation", want)
		}
	}
	// At the real beginning there is nothing left to promise.
	if a.moreHistory() {
		t.Fatal("the surface still claims there is conversation above the first message")
	}
	if !strings.Contains(strings.Join(plainRows(a)[:4], "\n"), "question 0") {
		t.Fatalf("the top of the transcript is not the first message:\n%s",
			strings.Join(plainRows(a)[:4], "\n"))
	}
}

// THE MARKER IS DRAWN WHILE THERE IS MORE, and it goes when there is not. It is
// the only thing that makes the seam honest: a top row with an hour of
// conversation behind it looks exactly like the beginning without it.
func TestTheEarlierMarkerStandsOnlyWhileThereIsMoreToReach(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	if got := plainRows(a)[0]; !strings.Contains(got, "earlier") {
		t.Fatalf("the top row does not say there is more above it: %q", got)
	}
	scrollToTop(t, a)
	if got := plainRows(a)[0]; strings.Contains(got, "earlier") {
		t.Fatalf("the marker is still up at the real beginning: %q", got)
	}
	// A conversation short enough to draw whole never had a seam to mark.
	short := resumedApp(t, longPast(3)...)
	if got := plainRows(short)[0]; strings.Contains(got, "earlier") {
		t.Fatalf("a whole conversation drew the marker anyway: %q", got)
	}
}

// THE READER DOES NOT MOVE WHEN THE HISTORY ARRIVES. Materializing rows above
// the window is only usable if the words under the reader's eye stay where they
// are — a jump at the moment of the backfill is the whole gesture undone.
func TestBackfillKeepsTheReaderOnTheSameLine(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	a.height = 24
	a.touch()
	// Park just short of the top, where the next page up is the one that
	// triggers the backfill.
	for a.offsetFor(len(a.visible(a.bodyWidth())), a.viewHeight()) > 0 {
		scrollBy(t, a, -1)
	}
	before, _ := a.window(a.bodyWidth(), a.viewHeight())
	if len(before) == 0 {
		t.Fatal("the window is empty")
	}
	top := plain(before[0].text)

	scrollBy(t, a, -1) // the gesture that walks into the prefetched page
	after, _ := a.window(a.bodyWidth(), a.viewHeight())
	if len(after) == 0 {
		t.Fatal("the window went empty across the backfill")
	}
	// One row of movement is the scroll the person asked for; the transcript
	// jumping to somebody else's paragraph is the defect.
	var found int = -1
	for i, r := range after {
		if plain(r.text) == top {
			found = i
			break
		}
	}
	if found < 0 || found > 2 {
		t.Fatalf("the line under the reader moved to row %d across the backfill (was 0): %q",
			found, top)
	}
}

// A BACKFILLED TURN FOLDS LIKE ANY OTHER. The blocks that arrive above carry
// turn numbers of their own, and a cluster whose calls shared a number with the
// turn already on screen would fold two unrelated turns together.
func TestBackfilledTurnsKeepTheirOwnGrouping(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	scrollToTop(t, a)

	seen := map[int]bool{}
	var last int
	first := true
	for _, e := range a.entries {
		if e.kind != entryUser {
			continue
		}
		if seen[e.turn] {
			t.Fatalf("two of the person's messages share turn %d", e.turn)
		}
		seen[e.turn] = true
		if !first && e.turn <= last {
			t.Fatalf("turn numbers do not run forward: %d after %d", e.turn, last)
		}
		last, first = e.turn, false
	}
	if len(seen) != 80 {
		t.Fatalf("the scrolled-back conversation holds %d of the person's messages, want 80", len(seen))
	}
}

// ── S2: a deliberate scroll wins ────────────────────────────────────────────

// TYPING DOES NOT MOVE THE CONVERSATION. A person who scrolled up to read
// something and then started writing about it keeps the thing they are reading
// on screen.
func TestTypingDoesNotPullTheConversationBackToTheBottom(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-9)
	if a.stick {
		t.Fatal("the transcript never left its live edge")
	}
	offset := a.offset
	typeInto(t, a, "about this line here")
	if a.stick {
		t.Fatal("typing re-armed the follow and pulled the reader to the bottom")
	}
	if a.offset != offset {
		t.Fatalf("typing moved the scroll: %d→%d", offset, a.offset)
	}
}

// NEW OUTPUT DOES NOT YANK THE VIEW DOWN. That is the whole reason the jump
// chip exists (jumpchip.go) — the surface offers the way back rather than
// taking it.
func TestNewOutputDoesNotYankAScrolledReaderDown(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-9)
	offset := a.offset
	before, _ := a.window(a.bodyWidth(), a.viewHeight())
	top := plain(before[0].text)

	for i := 0; i < 12; i++ {
		a.note("something landed while the reader was reading")
	}
	if a.stick {
		t.Fatal("output arriving re-armed the follow")
	}
	if a.offset != offset {
		t.Fatalf("output arriving moved the scroll: %d→%d", offset, a.offset)
	}
	after, _ := a.window(a.bodyWidth(), a.viewHeight())
	if plain(after[0].text) != top {
		t.Fatalf("the top of the window changed under the reader: %q → %q",
			top, plain(after[0].text))
	}
	// And the chip is up, which is the surface's whole answer to this state.
	if !a.jumpShowing() {
		t.Fatal("the conversation is parked off its live edge with no way back offered")
	}
}

// A REPLY STREAMING IN DOES NOT MOVE THE READER EITHER, which is the same claim
// one layer down: the note above is an append, and this is the live block
// growing a sentence at a time under an eye that is somewhere else entirely.
func TestAStreamingReplyDoesNotMoveAScrolledReader(t *testing.T) {
	a := scrolledApp(t, 24)
	a.scroll(-9)
	offset := a.offset
	before, _ := a.window(a.bodyWidth(), a.viewHeight())
	top := plain(before[0].text)

	a.state = stateWorking
	for i := 0; i < 20; i++ {
		a.event(text(session.EventTextDelta, "another sentence of the reply\n"))
	}
	a.touch()
	if a.stick {
		t.Fatal("a streaming reply re-armed the follow")
	}
	if a.offset != offset {
		t.Fatalf("a streaming reply moved the scroll: %d→%d", offset, a.offset)
	}
	after, _ := a.window(a.bodyWidth(), a.viewHeight())
	if plain(after[0].text) != top {
		t.Fatalf("the top of the window changed while the reply streamed: %q → %q",
			top, plain(after[0].text))
	}
}

// AND A BACKFILL DOES NOT HAND THE STREAM SOMEBODY ELSE'S BLOCK. The live reply
// is held as a POSITION in the block list, and forty blocks arriving in front of
// it would leave that position pointing at a finished answer forty turns back —
// which is the one way this could have gone quietly wrong.
func TestBackfillDoesNotDetachTheStreamingBlock(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	a.state = stateWorking
	a.event(text(session.EventTextDelta, "the answer being written right now"))
	live := a.live
	if live < 0 {
		t.Fatal("the stream opened no block to write into")
	}
	said := a.entries[live].text

	scrollToTop(t, a)
	if a.live < 0 || a.live >= len(a.entries) {
		t.Fatalf("the live block is now index %d of %d", a.live, len(a.entries))
	}
	if a.entries[a.live].text != said {
		t.Fatalf("the stream is now writing into %q, not %q",
			a.entries[a.live].text, said)
	}
	a.event(text(session.EventTextDelta, " and its next words"))
	if got := a.entries[a.live].text; got != said+" and its next words" {
		t.Fatalf("the delta after the backfill landed as %q", got)
	}
}

// THE NEXT PAGE IS ASKED FOR WHILE A SCREEN STILL REMAINS. The gesture changes
// only the offset it can already see; materializing older blocks is the command
// that comes back, so neither a key nor a wheel can wait on history work.
func TestEarlierHistoryPrefetchesBeforeTheViewportReachesTheTop(t *testing.T) {
	agent := &fakeAgent{model: "m", past: longPast(80)}
	a := newTestApp(agent)
	a.entries = nil
	a.replay()
	a.height = 24
	a.touch()
	rows := len(a.visible(a.bodyWidth()))
	height := a.viewHeight()
	a.stick = false
	a.offset = height + 1
	before := len(a.entries)

	cmd := a.scroll(-1)
	if cmd == nil || !a.historyLoading {
		t.Fatal("entering the one-screen prefetch margin scheduled no history page")
	}
	if a.offsetFor(rows, height) == 0 {
		t.Fatal("the viewport reached the top before prefetch began")
	}
	if len(a.entries) != before {
		t.Fatal("the scroll gesture materialized history synchronously")
	}

	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("the prefetch command returned %d messages, want one", len(msgs))
	}
	drive(t, a, msgs[0])
	if len(a.entries) <= before {
		t.Fatal("the returned history page added no entries")
	}
}

// A HOSTED TRANSCRIPT IS READ ONCE AND THEN SCROLLED LOCALLY. The fake's read
// count stands in for MethodTranscript crossing ssh: every page after replay is
// cut from the surface's mirror and no upward gesture calls the agent again.
func TestScrollingHistoryReadsTheMirroredTranscriptOnly(t *testing.T) {
	agent := &fakeAgent{model: "m", past: longPast(100)}
	a := newTestApp(agent)
	reads := agent.transcriptReads
	if agent.transcriptReads != 1 {
		t.Fatalf("opening read the transcript %d times, want one", agent.transcriptReads)
	}

	scrollToTop(t, a)
	if agent.transcriptReads != reads {
		t.Fatalf("scrolling crossed the transcript door %d extra times", agent.transcriptReads-reads)
	}
}
