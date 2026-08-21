package tui3

// THE DEFECT THESE TESTS PIN: scrolling up in a resumed conversation stopped at
// the last compaction and pretended that was the beginning of the chat.
//
// The journal had every line of it. [app.backfill] paged through the SESSION's
// transcript, which starts after the latest marker, so the moment replayFrom hit
// zero the `· earlier · keep scrolling` marker went out and an hour of
// conversation was unreachable on a screen that looked complete.
//
// What is asserted here is the whole of the fix: the scroll runs through the
// boundary into the region above it, the marker stays honest until the real
// beginning, and the boundary itself is DRAWN — one dim line saying what it is,
// staying where it happened while the reader scrolls on past it.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// compactedApp is a surface that opened on a journal with a compaction in it.
//
// It is built the way a real one is: `above` is the conversation as it stood
// when the pass ran, `rewritten` is the pass's shortened copy of it — the head
// of the live transcript, drawn by nobody — and `since` is what has been said
// since. The floor is where the copy ends, which is what keeps the conversation
// drawn once.
func compactedApp(t *testing.T, above, rewritten, since []session.DisplayEntry) *app {
	t.Helper()
	past := append(append([]session.DisplayEntry(nil), rewritten...), since...)
	a := newTestApp(&fakeAgent{
		model: "m", past: past, earlier: above, earlierFloor: len(rewritten),
	})
	a.entries = nil
	a.replay()
	a.touch()
	return a
}

// shortened is what a pass leaves where a run of conversation was: the person's
// own words kept, everything else standing in for itself.
func shortened(name string, turns int) []session.DisplayEntry {
	out := make([]session.DisplayEntry, 0, turns*2)
	for i := 0; i < turns; i++ {
		out = append(out,
			session.DisplayEntry{Role: "user", Text: name + " question " + itoa(i)},
			session.DisplayEntry{Role: "assistant", Text: "[folded]"},
		)
	}
	return out
}

// namedPast is longPast with its own words, so a test can tell which side of the
// seam a line came from.
func namedPast(name string, turns int) []session.DisplayEntry {
	out := make([]session.DisplayEntry, 0, turns*2)
	for i := 0; i < turns; i++ {
		out = append(out,
			session.DisplayEntry{Role: "user", Text: name + " question " + itoa(i)},
			session.DisplayEntry{Role: "assistant", Text: name + " answer " + itoa(i)},
		)
	}
	return out
}

// ── S1: the scroll runs through the seam ────────────────────────────────────

// SCROLLING UP REACHES THE CONVERSATION'S REAL FIRST MESSAGE, straight through
// the compaction. This is the bug: the first message of the whole chat is above
// the marker, and it was unreachable.
func TestScrollingUpReachesTheRealBeginningThroughACompaction(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	scrollToTop(t, a)

	drawn := drawnText(a)
	for _, want := range []string{
		"old question 0", "old question 49", "kept question 0", "kept question 49",
	} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q is missing from the scrolled-back conversation:\n%s", want, drawn)
		}
	}
	// The top of the transcript is the first thing anybody said, not the seam
	// and not the first line the model still holds.
	if !strings.Contains(strings.Join(plainRows(a)[:4], "\n"), "old question 0") {
		t.Fatalf("the top of the transcript is not the conversation's first message:\n%s",
			strings.Join(plainRows(a)[:4], "\n"))
	}
}

// THE MARKER STAYS UP UNTIL THE REAL BEGINNING. It used to go out at the
// boundary, which is the half of the defect a person actually saw: the promise
// of more conversation was withdrawn while there was an hour of it left.
func TestTheEarlierMarkerSurvivesTheCompactionBoundary(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	if !a.moreHistory() {
		t.Fatal("a resumed compacted session does not know there is more above it")
	}

	// Walk down to the boundary a helping at a time, and check the promise is
	// still standing every step of the way until the first message is drawn.
	for i := 0; i < 200; i++ {
		if strings.Contains(drawnText(a), "old question 0") {
			break
		}
		if !a.moreHistory() {
			t.Fatalf("the surface stopped promising history with the beginning still unreached:\n%s",
				strings.Join(plainRows(a)[:6], "\n"))
		}
		if !a.backfill() {
			t.Fatal("the backfill gave up before the conversation's first message")
		}
	}
	scrollToTop(t, a)
	if a.moreHistory() {
		t.Fatal("the surface still claims there is conversation above the first message")
	}
	if got := plainRows(a)[0]; strings.Contains(got, "earlier") {
		t.Fatalf("the marker is still up at the real beginning: %q", got)
	}
}

// ── S2: the seam itself ─────────────────────────────────────────────────────

// THE BOUNDARY IS DRAWN, in the surface's own dim lane, and it says both true
// things: the model's copy of what is above is shorter, and the person's is not.
func TestTheSeamIsDrawnAtTheCompactionBoundaryInThePersonsWords(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	scrollToTop(t, a)

	rows := plainRows(a)
	seam, end := seamRows(rows)
	if seam < 0 {
		t.Fatalf("the compaction boundary is not drawn anywhere:\n%s", strings.Join(rows, "\n"))
	}
	// THE ROW WEARS THE EXACT WORDS, in the dim lane, whole — wrapped on a narrow
	// frame and never cut, because a limit half-stated is the wrong limit.
	said := strings.TrimPrefix(strings.TrimSpace(strings.Join(rows[seam:end], " ")), "· ")
	if strings.Join(strings.Fields(said), " ") != seamMark {
		t.Fatalf("the seam is drawn as %q, want %q", said, seamMark)
	}
	// NO MACHINERY VOCABULARY. A person reads this line; "compaction" is a word
	// about the program's insides.
	for _, banned := range []string{"compact", "marker", "transcript", "token"} {
		if strings.Contains(strings.ToLower(seamMark), banned) {
			t.Fatalf("the seam says %q, which is machinery vocabulary: %q", banned, seamMark)
		}
	}

	// AND IT IS AT THE BOUNDARY. Everything above it is the region the journal
	// kept; everything below is what the model still carries.
	above := strings.Join(rows[:seam], "\n")
	below := strings.Join(rows[seam+1:], "\n")
	if !strings.Contains(above, "old question 49") || strings.Contains(above, "kept question") {
		t.Fatalf("what is above the seam is not the conversation above the boundary:\n%s", above)
	}
	if !strings.Contains(below, "kept question 0") || strings.Contains(below, "old question") {
		t.Fatalf("what is below the seam is not the conversation the model kept:\n%s", below)
	}
}

// THE SEAM IS DRAWN ONCE. It is earned on the crossing and every helping after
// it comes up without one — a line per backfill would turn the history into a
// ladder of the same sentence.
func TestTheSeamIsDrawnOnlyOnce(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	scrollToTop(t, a)

	seams := 0
	for _, e := range a.entries {
		if e.kind == entrySeam {
			seams++
		}
	}
	if seams != 1 {
		t.Fatalf("the conversation holds %d seams, want exactly one", seams)
	}
}

// A CONVERSATION THAT WAS NEVER COMPACTED HAS NO SEAM AND NOTHING ABOVE IT. This
// is the byte-for-byte claim: the surface a person without a compaction sees is
// the one they saw before this wave.
func TestAnUncompactedConversationDrawsNoSeam(t *testing.T) {
	a := resumedApp(t, longPast(80)...)
	scrollToTop(t, a)
	if seam, _ := seamRows(plainRows(a)); seam >= 0 {
		t.Fatalf("a conversation with no compaction drew a seam:\n%s", drawnText(a))
	}
	if a.moreHistory() {
		t.Fatal("a conversation with no compaction claims history above its first message")
	}
}

// A SEAM IS NOT SWALLOWED BY A WORK FOLD. The chip that hides a turn's machinery
// picks its rows by POSITION, so a seam that landed inside one would disappear
// with it — and the fold would be quietly claiming the conversation above it is
// unbroken.
func TestTheSeamSurvivesTheWorkFold(t *testing.T) {
	above := namedPast("old", 50)
	// A turn whose tail is machinery followed by a settled answer, which is
	// exactly the shape [deriveWorkfolds] collapses.
	kept := []session.DisplayEntry{
		{Role: "tool", Tool: "read", Hint: "read a.go", Args: `{"path":"a.go"}`, Output: "package main"},
		{Role: "assistant", Text: "here is what it says"},
	}
	kept = append(kept, namedPast("kept", 20)...)
	a := compactedApp(t, above, shortened("old", 50), kept)
	scrollToTop(t, a)

	if seam, _ := seamRows(plainRows(a)); seam < 0 {
		t.Fatalf("a fold swallowed the seam:\n%s", drawnText(a))
	}
}

// seamRows is where the seam is drawn and where it ends — a half-open range,
// because the sentence wraps on a narrow frame. (-1, -1) when it is not drawn.
func seamRows(rows []string) (int, int) {
	head := strings.Fields(seamMark)[0]
	for i, row := range rows {
		if !strings.HasPrefix(strings.TrimSpace(row), "· "+head+" ") {
			continue
		}
		end := i + 1
		for end < len(rows) && strings.HasPrefix(rows[end], "  ") && strings.TrimSpace(rows[end]) != "" {
			end++
		}
		return i, end
	}
	return -1, -1
}

// ── S3: a pass that fires while somebody is reading ─────────────────────────

// A COMPACTION LANDING MID-SESSION DOES NOT CORRUPT THE SCROLLBACK. The mark
// this surface holds was taken against a transcript the pass has just replaced,
// and left alone it would hand up somebody else's blocks. It is carried over
// into the region the pass created instead, so the history stays reachable and
// stays in order.
func TestACompactionWhileScrolledUpKeepsTheHistoryReachableAndInOrder(t *testing.T) {
	past := namedPast("kept", 60)
	agent := &fakeAgent{model: "m", past: past}
	a := newTestApp(agent)
	a.entries = nil
	a.replay()
	a.touch()

	// The reader walks up one helping, so the surface is holding a place in the
	// middle of the transcript when the pass fires.
	if !a.backfill() {
		t.Fatal("the first backfill drew nothing")
	}
	top := strings.Join(plainRows(a)[:4], "\n")

	// The pass: what it edited becomes the region above, and the transcript
	// becomes its own shortened copy of that — every entry of it inside the new
	// floor, because at that instant nothing has been said since.
	agent.earlier = past
	agent.past = shortened("kept", 60)
	agent.earlierFloor = len(agent.past)
	a.event(session.Event{Kind: session.EventCompacted, Hint: "compacted · stubbed 3 tool results"})

	if !a.moreHistory() {
		t.Fatal("the pass took the history above the reader with it")
	}
	// The row they were reading has not moved out from under them, and nothing
	// from the rebuilt window has been spliced into the middle of the history.
	if got := strings.Join(plainRows(a)[:4], "\n"); got != top {
		t.Fatalf("the top of the drawn conversation changed across the pass:\n%s\n---\n%s", top, got)
	}

	scrollToTop(t, a)
	drawn := drawnText(a)
	if !strings.Contains(drawn, "kept question 0") {
		t.Fatalf("the conversation's first message is unreachable after the pass:\n%s", drawn)
	}
	if strings.Contains(drawn, "[folded]") {
		t.Fatalf("the pass's own shortened copy was drawn into the history:\n%s", drawn)
	}
	// The pass draws its OWN row at that boundary; a seam two rows above it would
	// be the surface saying the same thing twice.
	if seam, _ := seamRows(plainRows(a)); seam >= 0 {
		t.Fatalf("a seam was drawn beside the pass's own row:\n%s", drawn)
	}
	// And no message is drawn twice, which is what a mark left pointing into the
	// replaced transcript would have caused.
	if n := strings.Count(drawn, "kept question 30"); n != 1 {
		t.Fatalf("a message from the history is drawn %d times after the pass", n)
	}
}

// A REWIND RE-EARNS THE HISTORY FROM SCRATCH. [app.rebuildTranscript] throws the
// drawn blocks away and replays; the region above the seam is dropped unread
// with them, so nothing that was fetched before the cut is trusted after it.
func TestARewindDropsTheHistoryTheSurfaceWasHolding(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	scrollToTop(t, a)
	if a.earlierFrom != 0 || !a.earlierSeam {
		t.Fatalf("the surface did not scroll through the history it was holding: from=%d seam=%v",
			a.earlierFrom, a.earlierSeam)
	}

	a.rebuildTranscript()
	if a.earlierFrom != len(a.earlier) || a.earlierSeam {
		t.Fatalf("a rebuild kept the place it had walked to: from=%d of %d, seam=%v",
			a.earlierFrom, len(a.earlier), a.earlierSeam)
	}
	// And it is all reachable again, from the session, exactly as on a resume.
	scrollToTop(t, a)
	if !strings.Contains(drawnText(a), "old question 0") {
		t.Fatalf("the rebuilt conversation cannot be scrolled to its beginning:\n%s", drawnText(a))
	}
}

// ── S4: the conversation is told once ───────────────────────────────────────

// THE PASS'S OWN SHORTENED COPY IS NEVER DRAWN. This is the regression that
// makes the whole feature worth having or not: the file holds the conversation
// twice — once as it happened, above the marker, and once rewritten below it —
// so a surface that drew the region ABOVE the transcript instead of INSTEAD of
// its head would show a person their entire session twice over.
func TestTheConversationIsDrawnOnceAcrossTheSeam(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), namedPast("kept", 50))
	scrollToTop(t, a)
	drawn := drawnText(a)

	for _, once := range []string{"old question 0", "old question 49", "kept question 0"} {
		if n := strings.Count(drawn, once); n != 1 {
			t.Fatalf("%q is drawn %d times, want once:\n%s", once, n, drawn)
		}
	}
	// The shortened copy stands in for the region and is never a row.
	if strings.Contains(drawn, "[folded]") {
		t.Fatalf("the pass's shortened copy of the history was drawn:\n%s", drawn)
	}
	// And the original words are, which is the point of reaching for the region
	// at all rather than leaving the transcript alone.
	if !strings.Contains(drawn, "old answer 7") {
		t.Fatalf("the history is drawn in the pass's words, not the ones that were said:\n%s", drawn)
	}
}

// A SESSION PUT DOWN THE MOMENT IT COMPACTED STILL OPENS ON SOMETHING. The pass
// leaves no conversation below its floor, so the opening helping has to come out
// of the region — and a surface that opened on an empty screen would be the
// defect the resume replay exists to prevent.
func TestASessionResumedRightAfterAPassOpensOnItsHistory(t *testing.T) {
	a := compactedApp(t, namedPast("old", 50), shortened("old", 50), nil)
	drawn := drawnText(a)
	if strings.TrimSpace(drawn) == "" {
		t.Fatal("a session resumed straight after a pass opened on an empty screen")
	}
	if !strings.Contains(drawn, "old question 49") {
		t.Fatalf("the opening helping is not the end of the conversation:\n%s", drawn)
	}
	if !a.moreHistory() {
		t.Fatal("the surface does not know the rest of the conversation is above")
	}
	scrollToTop(t, a)
	if !strings.Contains(drawnText(a), "old question 0") {
		t.Fatalf("the beginning is unreachable:\n%s", drawnText(a))
	}
}
