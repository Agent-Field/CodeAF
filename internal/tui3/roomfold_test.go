package tui3

// ── WHAT A FINISHED ROOM READS LIKE ─────────────────────────────────────────
//
// roomfold.go moves ONE knob when a node lands: the fold style. These tests are
// about what that buys and what it may never cost.
//
// These tests open real journals, settle them through the lane's close message,
// and assert the rendered room deck, including its production reading lens.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// landedJournal is a node with an ordinary life behind it: the instruction, two
// stretches of narration each buying a call, and the markdown report it came
// home with.
//
// IT IS THE SHAPE THE READING POSTURE IS DESIGNED AGAINST. Everything between
// the instruction and the report is machinery the person delegated precisely so
// they would not have to watch it, and the report is the line they opened the
// page for.
func landedJournal(t *testing.T) string {
	t.Helper()
	return roomJournal(t,
		`{"type":"message","role":"user","content":"Draw two posters"}`,
		`{"type":"message","role":"assistant","content":"Reading the site first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"index.html\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"98 lines"}`,
		`{"type":"message","role":"assistant","content":"I have the aesthetic. Generating both.","toolCalls":[{"id":"c2","function":{"name":"generate_image","arguments":"{\"prompt\":\"poster\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c2","content":"wrote poster.png"}`,
		`{"type":"message","role":"assistant","content":"## Both posters\n\nThey are in **posters/**, at 1200 by 1600."}`,
	)
}

// openRoomOn opens a page on a journal with every chip at its default, and
// leaves the node RUNNING. The tests that want a landed one land it themselves,
// through the message the lane really ends with.
func openRoomOn(t *testing.T, journal string) *app {
	t.Helper()
	a, fake, _ := roomApp(t)
	fake.journal = journal
	a.workMode = config.WorkFold
	a.openRoom(7, "Draw two posters")
	a.touch()
	return a
}

// landRoom is what the lane closing does to the page (app.go's roomClosedMsg).
func landRoom(t *testing.T, a *app) {
	t.Helper()
	drive(t, a, roomClosedMsg{gen: a.room.gen})
	if !a.room.done {
		t.Fatal("the room did not take the lane's close as a landing")
	}
}

func readingDeck(a *app) deck { return a.room.deck() }

// readingPage is what a person reads on that page.
func readingPage(a *app) string {
	rows, _ := a.deckRows(readingDeck(a), 60)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// A LANDED ROOM IS THE INSTRUCTION, THE WORK BEHIND ONE CHIP, AND THE REPLY.
//
// Three claims in one page, because they are one claim: the machinery of a
// finished stretch collapses, and the two things the page exists to show — what
// was asked and what it came to — are still words on the screen.
func TestALandedRoomFoldsItsStretchOfWorkAndKeepsTheReply(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	page := readingPage(a)

	for _, want := range []string{"Draw two posters", "Both posters", "They are in posters/, at 1200 by 1600."} {
		if !strings.Contains(page, want) {
			t.Fatalf("a fold hid what the page is for (%q):\n%s", want, page)
		}
	}
	// ONE CHIP OVER THE WHOLE STRETCH, which is the difference from the posture
	// the page had while the node ran: two phases folded separately there.
	if n := strings.Count(page, "▸ worked"); n != 1 {
		t.Fatalf("want the finished work behind one chip, got %d:\n%s", n, page)
	}
	// AND IT STATES COUNTED FACTS AND NAMES ITS DOOR, in the conversation's own
	// grammar, because it IS the conversation's chip.
	if !strings.Contains(page, "2 tool calls · ctrl+e") {
		t.Fatalf("the chip does not count its calls or name its door:\n%s", page)
	}
	for _, gone := range []string{"Reading the site first", "I have the aesthetic", "index.html", "generate_image"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the settled machinery is still on the page (%q):\n%s", gone, page)
		}
	}
	// THE REPLY IS NEVER INSIDE THE CHIP. It is the stretch's last settled
	// paragraph, so the chip stops at it rather than covering it.
	report := strings.Index(page, "They are in posters/")
	if chip := strings.LastIndex(page, "▸ worked"); chip > report {
		t.Fatalf("a chip was drawn after the reply:\n%s", page)
	}
}

// AND EVERY DOOR STILL OPENS IT, because the disclosure ladder may never
// dead-end: what is behind the chip is the actual calls and the actual words the
// node wrote on its way, not a summary of them.
func TestOpeningALandedRoomsChipGivesBackTheCallsAndTheNarration(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)

	// The chip's own door, taken the way the click and `ctrl+e` take it
	// (workfold.go's [app.setWorkOpen]).
	d := readingDeck(a)
	folds := a.deckFolds(d)
	if len(folds) != 1 {
		t.Fatalf("want the one chip to open, got %d: %+v", len(folds), folds)
	}
	for _, f := range folds {
		a.setWorkOpen(d, f.key, true)
	}
	// Open is the OUTLINE — a caption per step — so the calls are one expand
	// further, exactly as they are out in the conversation.
	d = readingDeck(a)
	stampHierarchy(d.entries, a.deckFolds(d))
	for _, c := range deriveCaptions(d.entries, d.runningTurn) {
		a.setCapOpen(d, c.start, true)
	}

	page := readingPage(a)
	for _, want := range []string{"Reading the site first", "I have the aesthetic", "index.html", "generate_image"} {
		if !strings.Contains(page, want) {
			t.Fatalf("opening the chip did not give back %q:\n%s", want, page)
		}
	}
	// AND THE REPLY IS STILL THERE. Opening the work adds the machinery back; it
	// never trades the answer for it.
	if !strings.Contains(page, "They are in posters/") {
		t.Fatalf("opening the chip cost the page its reply:\n%s", page)
	}
}

// AND `ctrl+e` REACHES A CHIP ON A LANDED PAGE, through the room's own body deck
// rather than through a deck a test built.
//
// IT IS ASSERTED SEPARATELY FROM WHAT THE CHIP HOLDS, and lens-agnostically, on
// purpose: this is the claim that the KEYBOARD still finds something to open on
// a finished room, which has to be true of whichever posture room.go hands the
// body ([app.bodyDeck]), and it is the assertion that would catch a chip whose
// key nothing on the page can name.
func TestCtrlEStillOpensWorkOnALandedRoom(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	if strings.Contains(roomText(a), "index.html") {
		t.Fatalf("the landed page did not start with its work folded:\n%s", roomText(a))
	}
	if !a.toggleLatestWorkfold() {
		t.Fatalf("ctrl+e found no chip on a landed page:\n%s", roomText(a))
	}
	openFirstCaption(t, a)
	if page := roomText(a); !strings.Contains(page, "index.html") && !strings.Contains(page, "generate_image") {
		t.Fatalf("ctrl+e opened no work:\n%s", page)
	}
}

// A CORRECTION IS NEVER FOLDED AWAY, AND NEITHER IS THE REPLY ON EITHER SIDE OF
// IT. The overseer's primary act is saying something to running work; a page
// that filed those words behind a chip would be hiding the one thing on it the
// person themself wrote.
func TestACorrectionAndBothRepliesSurviveOnALandedRoom(t *testing.T) {
	a := openRoomOn(t, roomJournal(t,
		`{"type":"message","role":"user","content":"Draw two posters"}`,
		`{"type":"message","role":"assistant","content":"Reading the site first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"index.html\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"98 lines"}`,
		`{"type":"message","role":"assistant","content":"Both drawn in landscape."}`,
		`{"type":"message","role":"user","content":"portrait, not landscape","steer":{"at":"2026-09-05T18:00:00Z","consumed":true,"landing":"delivered"}}`,
		`{"type":"message","role":"assistant","content":"Redrawing them.","toolCalls":[{"id":"c2","function":{"name":"generate_image","arguments":"{\"prompt\":\"poster\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c2","content":"wrote poster.png"}`,
		`{"type":"message","role":"assistant","content":"## Both posters\n\nRedrawn in **portrait**."}`,
	))
	landRoom(t, a)

	page := readingPage(a)
	for _, want := range []string{
		"Draw two posters",         // the instruction
		"portrait, not landscape",  // the correction, in the person's own words
		"Both drawn in landscape.", // what the work had come to when they said it
		"Redrawn in portrait.",     // what it came to afterwards
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("a fold hid %q:\n%s", want, page)
		}
	}
	// THE CORRECTION ENDS THE STRETCH IT LANDED IN, so the work each side of it
	// folds separately and no chip spans it.
	elbow := -1
	for i := range a.room.entries {
		if a.room.entries[i].kind == entrySteer {
			elbow = i
		}
	}
	if elbow < 0 {
		t.Fatalf("the record's correction did not draw an elbow: %#v", a.room.entries)
	}
	folds := a.deckFolds(readingDeck(a))
	if len(folds) != 2 {
		t.Fatalf("want a chip each side of the correction, got %d: %+v", len(folds), folds)
	}
	for start, f := range folds {
		if start <= elbow && f.answer > elbow {
			t.Fatalf("a chip at %d covers the correction at %d: %+v\n%s", start, elbow, f, page)
		}
	}
}

// WORK THAT REACHED NO REPLY IS NOT FOLDED, AND `done` DOES NOT MAKE ONE.
//
// A node whose record ends in a call has said nothing to the person about how it
// went, and a landing is a fact about the lane rather than about the work. A
// page that promoted the last thing it happened to have into an answer — or
// swallowed a run of calls behind a chip on the strength of one — would be the
// surface inventing a report nobody wrote.
func TestTrailingWorkWithNoReplyStaysWholeOnALandedRoom(t *testing.T) {
	a := openRoomOn(t, roomJournal(t,
		`{"type":"message","role":"user","content":"Draw two posters"}`,
		`{"type":"message","role":"assistant","content":"Reading the site first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"index.html\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"98 lines"}`,
	))
	landRoom(t, a)

	if folds := a.deckFolds(readingDeck(a)); len(folds) != 0 {
		t.Fatalf("a stretch with no reply folded anyway: %+v", folds)
	}
	page := readingPage(a)
	if strings.Contains(page, "▸ worked") {
		t.Fatalf("a chip was drawn over work that reached no reply:\n%s", page)
	}
	for _, want := range []string{"Draw two posters", "Reading the site first", "index.html"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page lost %q, which is all the account there is:\n%s", want, page)
		}
	}
}

// AND A FAILURE AT THE TAIL KEEPS ITS ROW. ONLY FAILURE SPEAKS on this surface,
// so the one row allowed to raise its voice may not then be filed away — and it
// is after the last settled paragraph, so no chip reaches it.
func TestAFailedCallAtTheTailIsStillOnALandedPage(t *testing.T) {
	a := openRoomOn(t, roomJournal(t,
		`{"type":"message","role":"user","content":"Draw two posters"}`,
		`{"type":"message","role":"assistant","content":"Reading the site first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"index.html\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"98 lines"}`,
		`{"type":"message","role":"assistant","content":"Now generating both."}`,
	))
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "generate_image", CallID: "c9",
		Hint: "generate_image poster", Args: `{"prompt":"poster"}`,
	}})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolFailed, Tool: "generate_image", CallID: "c9",
		Err: errors.New("the image service refused"),
	}})
	landRoom(t, a)

	failed := false
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.status == toolFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("the fixture never drew a failed call: %#v", a.room.entries)
	}
	page := readingPage(a)
	if !strings.Contains(page, "generate_image") {
		t.Fatalf("the failed call was folded away:\n%s", page)
	}
	if !strings.Contains(page, "Now generating both.") {
		t.Fatalf("the last thing the node said is gone:\n%s", page)
	}
}

// A running room retains phase folding independently of compact live work.
// The live call remains available through its own disclosure.
func TestARunningRoomStillReadsByPhaseAndKeepsItsFrontier(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	if got := a.room.readingLens().foldPast; got != foldPhases {
		t.Fatalf("a running room folds by %v, want the phases it always folded by", got)
	}
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: "live",
		Hint: "bash convert", Args: `{"cmd":"convert"}`,
	}})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "Still working.",
	}})

	page := readingPage(a)
	if n := strings.Count(page, "▸ worked"); n != 2 {
		t.Fatalf("want a chip per settled phase behind the frontier, got %d:\n%s", n, page)
	}
	if !strings.Contains(page, "Still working.") || strings.Contains(page, "bash") {
		t.Fatalf("compact frontier lost prose or exposed the call:\n%s", page)
	}
	openRoomCompactWork(t, a)
	page = readingPage(a)
	for _, want := range []string{"Still working.", "bash"} {
		if !strings.Contains(page, want) {
			t.Fatalf("opened frontier lost %q:\n%s", want, page)
		}
	}
	if n := strings.Count(page, "▸ worked"); n != 2 {
		t.Fatalf("opening live work changed settled phases:\n%s", page)
	}
}

// AND ONLY THE FOLD MOVES. The other three knobs are answers about the PAGE —
// where its numbers live, whose clock runs over its turns, how many calls a
// folded cluster keeps — and none of them is a question about whether the work
// is still going, so a landing may not touch them.
func TestOnlyTheFoldStyleChangesWhenARoomLands(t *testing.T) {
	running := (&taskRoom{}).readingLens()
	landed := (&taskRoom{done: true}).readingLens()

	if running.foldPast != overseerLens.foldPast {
		t.Fatalf("a running room is no longer the overseer's page: %v", running.foldPast)
	}
	if landed.foldPast != foldTurns {
		t.Fatalf("a landed room folds by %v, want the conversation's own fold", landed.foldPast)
	}
	for name, got := range map[string]lens{"running": running, "landed": landed} {
		if got.clock != overseerLens.clock {
			t.Errorf("%s: the session's clock ran over a node's turns", name)
		}
		if got.receipts != overseerLens.receipts {
			t.Errorf("%s: the numbers left the header", name)
		}
		if got.spawnCards != overseerLens.spawnCards {
			t.Errorf("%s: a room started minting cards", name)
		}
		if reflect.ValueOf(got.toolTail).Pointer() != reflect.ValueOf(overseerLens.toolTail).Pointer() {
			t.Errorf("%s: the page stopped keeping the room's own screenful of calls", name)
		}
	}
}

// THE CHIPS A READER OPENED WHILE WATCHING DO NOT SURVIVE THE LANDING, because
// the two postures do not agree about what a key means: chip 1 is the first
// PHASE while the node runs and the whole of the completed WORK once it lands
// (setDone resets this keyspace). Left alone, somebody who opened one phase
// to watch a call go by would come back to a finished page with every call on it
// already spilled.
func TestOpenedChipsShutWhenTheWorkLands(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	live := readingDeck(a)
	folds := a.deckFolds(live)
	if len(folds) == 0 {
		t.Fatal("the running page folded nothing, so this proves nothing")
	}
	for _, f := range folds {
		a.setWorkOpen(live, f.key, true)
	}

	landRoom(t, a)

	for key, open := range a.room.workOpen {
		if open {
			t.Fatalf("chip %d was still open across the landing: %v", key, a.room.workOpen)
		}
	}
	if page := readingPage(a); strings.Contains(page, "index.html") {
		t.Fatalf("the landed page opened with its work spilled:\n%s", page)
	}
	if !a.room.dirty {
		t.Fatal("the page was not marked for a redraw after its chips were shut")
	}
}

// AND IT FIRES ON THE TRANSITION AND NEVER ON THE STATE. A hosted page
// re-answers `done` four times a second (room.go's [app.farRoomRead]); a reset
// that ran on "this room is finished" would shut every chip the reader opened, a
// quarter of a second after they opened it.
func TestSettlingTheFoldsLeavesAnAlreadyLandedReadersChipsAlone(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	d := readingDeck(a)
	for _, f := range a.deckFolds(d) {
		a.setWorkOpen(d, f.key, true)
	}

	a.room.setDone(true)
	open := false
	for _, v := range a.room.workOpen {
		open = open || v
	}
	if !open {
		t.Fatalf("a re-read of a finished room shut the chip its reader had opened: %v", a.room.workOpen)
	}

	// And a room that has not landed is not re-cut either: nothing changed, so
	// nothing may be dropped.
	b := openRoomOn(t, landedJournal(t))
	live := readingDeck(b)
	for _, f := range b.deckFolds(live) {
		b.setWorkOpen(live, f.key, true)
	}
	b.room.setDone(false)
	open = false
	for _, v := range b.room.workOpen {
		open = open || v
	}
	if !open {
		t.Fatalf("a running room's chips were shut by a landing that never happened: %v", b.room.workOpen)
	}
}

// A resumed room switches back to phase keys; an open completed-turn chip must
// not accidentally expand a different live phase with the same numeric key.
func TestResumingARoomClearsCompletedFoldKeys(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	d := readingDeck(a)
	folds := a.deckFolds(d)
	if len(folds) == 0 {
		t.Fatal("fixture has no completed fold")
	}
	for _, f := range folds {
		a.setWorkOpen(d, f.key, true)
	}
	a.room.dirty = false
	a.room.setDone(false)
	if len(a.room.workOpen) != 0 || !a.room.dirty {
		t.Fatalf("resuming retained completed keys or missed redraw: open=%v dirty=%v", a.room.workOpen, a.room.dirty)
	}
	if got := readingDeck(a).lens.foldPast; got != foldPhases {
		t.Fatalf("resumed room uses fold style %v, want phases", got)
	}
}
