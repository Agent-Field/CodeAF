package tui3

// A CLICK ON NOTHING IS NOTHING, AND THE ROOM SAYS WHERE YOU ARE.
//
// Two defects off one session, both about a person standing inside a node's
// page. The first: a click anywhere in the body — a blank row, the gap beside a
// paragraph, the slack under a short transcript — threw them back to the
// conversation. Leaving was never meant to be a miss; it is esc, it is ←, and it
// is the pinned header that names both. The second: nothing in the roster beside
// the page said WHICH of those rows they had walked through, so the one list of
// the work was a list of what exists rather than a map of where you are.
//
// These tests hold both ends of that: what is inert now, what is still pressable
// (the header, the rail, the column's own door, the approval row, a parked
// message), and the mark the roster wears while a room is open.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// bandSeq is the selected row's background as a terminal receives it — the same
// sequence the strip's chip and the overlay's cursor row are pinned by.
func bandSeq() string { return "\x1b[48;5;" + itoa(int(hueSelected.idx)) + "m" }

// clickRailNode presses the roster row of ONE named node — the door into that
// node's page. [clickRail] counts rows and these tests care which node they
// walked into, not which row it happened to be on.
func clickRailNode(t *testing.T, a *app, id uint64) {
	t.Helper()
	top := a.bodyTop()
	for y := top; y < top+a.viewHeight(); y++ {
		if node := a.railNodeAt(y); node != nil && node.id == id {
			// Past the seam and past the state cell, on the title. The two cells
			// before it are the column's handle at this width, and the one after
			// them is the fold while the pointer is on it (task.go's
			// [app.railLead]) — this press wants neither.
			drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam) + 6,
				Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam) + 6,
				Y: y, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("the roster has no row for node %d", id)
}

// deadRowInRoom is a screen row inside the open page's body that answers to
// nothing: no tool call, no card, no link — the empty space a person's miss
// lands on. It prefers the slack under a short transcript, and falls back to the
// first row the layout marked [hitNone].
func deadRowInRoom(t *testing.T, a *app) int {
	t.Helper()
	top := a.bodyTop()
	if top < 0 {
		t.Fatal("the frame has no body region to miss in")
	}
	for y := top + a.viewHeight() - 1; y >= top; y-- {
		r, ok := a.rowAt(y)
		if !ok || r.hit == hitNone {
			return y
		}
	}
	t.Fatal("every row of the page is a target; there is no miss to make")
	return -1
}

// ── WHAT IS INERT ───────────────────────────────────────────────────────────

// THE DEFECT ITSELF. A press on the empty part of a node's page used to close
// the page; now it does nothing at all, and the person is still reading what
// they were reading.
func TestAPressOnTheEmptyPartOfARoomStaysInTheRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail did not open a room to stand in")
	}
	id := a.room.id

	y := deadRowInRoom(t, a)
	drive(t, a, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 1, Y: y, Button: tea.MouseLeft})
	if !a.roomOpen() {
		t.Fatalf("a press on row %d of the page threw the reader out of the room", y)
	}
	if a.room.id != id {
		t.Fatalf("the press moved the reader to another room: %d", a.room.id)
	}

	// AND THE SAME PRESS THROUGH THE HIT-TESTING'S OTHER ANSWER: the row list
	// runs out under a short page, so the pointer resolves to no row at all.
	// That used to be the same exit and it is the same nothing now.
	drive(t, a, tea.MouseClickMsg{X: 1, Y: a.bodyTop() + a.viewHeight() - 1, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 1, Y: a.bodyTop() + a.viewHeight() - 1, Button: tea.MouseLeft})
	if !a.roomOpen() {
		t.Fatal("a press on the slack under the page closed it")
	}

	// AND THE WAY OUT IS UNTOUCHED.
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc no longer leaves the room")
	}
}

// THE SAME LAW IN THE CONVERSATION, which is where it always held: a press on a
// paragraph, or on the blank under the last message, does nothing. This is the
// guard against the fix being written as a special case for rooms.
func TestAPressOnNothingInTheConversationDoesNothing(t *testing.T) {
	a, _, _ := roomApp(t)
	a.dismissWelcome()
	typeLine(t, a, "what is in this repository")

	before := frame(a)
	sel, stick := a.sel, a.stick
	y := a.bodyTop() + a.viewHeight() - 1
	drive(t, a, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 1, Y: y, Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("a press on the conversation's empty space opened a room")
	}
	if a.sel != sel || a.stick != stick {
		t.Fatalf("a press on nothing moved the cursor: sel=%d stick=%v", a.sel, a.stick)
	}
	if after := frame(a); after != before {
		t.Fatalf("a press on nothing redrew the window differently:\n%s\n---\n%s", before, after)
	}
}

// ── WHAT IS STILL PRESSABLE ─────────────────────────────────────────────────

// THE HEADER IS THE POINTER'S WAY OUT, AND IT IS PRESSABLE ALL THE WAY ACROSS —
// its right end especially, because that is where "esc/← main" is printed and
// that end is inside the roster's own columns. The rail claims every press in
// those columns, so a header read after it would be dead at exactly the cells
// carrying the words.
func TestTheRoomHeaderBackLabelOpensAndWhitespaceIsInert(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)
	_ = a.roomHeadRows(a.width)
	if !a.roomBackSpan.pressable() {
		t.Fatal("header omitted its Back control")
	}
	for _, x := range []int{0, a.width / 2, a.width - 1} {
		drive(t, a, tea.MouseClickMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
		if !a.roomOpen() {
			t.Fatalf("blank header space at %d navigated", x)
		}
	}
	x := a.roomBackSpan.from + 1
	drive(t, a, tea.MouseClickMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("Back label failed to return to the conversation")
	}
}

// THE ROSTER IS STILL THE DOOR BETWEEN ROOMS. The whole point of making a miss
// inert is that the hits keep working: a rail row still walks into that node's
// page from inside another one.
func TestTheRailStillOpensAnotherRoomFromInsideOne(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the rail did not open node 1's room: open=%v", a.roomOpen())
	}
	clickRailNode(t, a, 3)
	if !a.roomOpen() || a.room.id != 3 {
		t.Fatalf("a rail press from inside a room did not walk into node 3: open=%v id=%d",
			a.roomOpen(), roomID(a))
	}
}

// roomID is the open room's node, or zero when there is no room.
func roomID(a *app) uint64 {
	if a.room == nil {
		return 0
	}
	return a.room.id
}

// AND THE COLUMN'S OWN DOOR IS STILL PRESSABLE FROM INSIDE A ROOM — the last
// line of the roster, which names ctrl+g (task.go's [railStowHint]).
func TestTheColumnsStowLineStillAnswersFromInsideARoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	clickRailNode(t, a, 1)
	if !a.roomOpen() {
		t.Fatal("the rail did not open a room")
	}
	at := -1
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if line, ok := a.railLineAt(y); ok && line.stow {
			at = y
			break
		}
	}
	if at < 0 {
		t.Fatal("the column drew no door out of itself")
	}
	drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + 2, Y: at, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + 2, Y: at, Button: tea.MouseLeft})
	if a.railShowing() {
		t.Fatal("a press on the column's own door did not put it away")
	}
	if !a.roomOpen() {
		t.Fatal("putting the column away left the room as well")
	}
}

// THE IN-ROOM APPROVAL ROW STILL ANSWERS A PRESS (roomapproval.go). It is read
// above the body, so nothing about the body's miss can reach it — this is the
// test that keeps the ladder in that order.
func TestTheRoomApprovalRowStillAnswersAPress(t *testing.T) {
	a, agent := awaitingDesign(t)
	at := -1
	for y := 0; y < a.height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeRoomApproval && mark.index == 1 {
			at = y
			break
		}
	}
	if at < 0 {
		t.Fatal("the design's room drew no approval row to press")
	}
	if len(a.roomApprovalTaps) == 0 {
		t.Fatal("the approval row recorded no answers to press")
	}
	drive(t, a, tea.MouseClickMsg{X: a.roomApprovalTaps[0].span.from, Y: at, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.roomApprovalTaps[0].span.from, Y: at, Button: tea.MouseLeft})
	if len(agent.answers) == 0 {
		t.Fatal("a press on the approval row answered nothing")
	}
	if !a.roomOpen() {
		t.Fatal("answering the design's question walked out of its room")
	}
}

// ── THE ROSTER SAYS WHICH ROOM YOU ARE IN ───────────────────────────────────

// railRowsFor is the drawn lines the roster gives one node, at this height.
func railRowsFor(a *app, height int, title string) []string {
	var out []string
	for _, row := range a.railRows(height) {
		if strings.Contains(plain(row), title) {
			out = append(out, row)
		}
	}
	return out
}

// THE ROW YOU WALKED THROUGH WEARS THE SELECTION BAND, and it stops wearing it
// the moment you leave. Both halves of the mark are checked: the band, which is
// what a colour terminal shows, and the accent title under it, which is what is
// left where there is no background to draw.
func TestTheRosterMarksTheRoomAPersonIsStandingIn(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	height := a.viewHeight()

	rows := railRowsFor(a, height, "Ship the port")
	if len(rows) == 0 {
		t.Fatal("the roster has no row for the node to walk into")
	}
	for _, row := range rows {
		if strings.Contains(row, bandSeq()) {
			t.Fatalf("a roster with no room open already marks a row:\n%q", row)
		}
	}

	clickRailNode(t, a, 1)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the rail did not open node 1: open=%v id=%d", a.roomOpen(), roomID(a))
	}
	rows = railRowsFor(a, height, "Ship the port")
	if len(rows) == 0 {
		t.Fatal("the node's row left the roster when its room opened")
	}
	// EVERY LINE OF THE ENTRY, not just its head: a node's row is two lines tall
	// when it has something to say under its title.
	for _, row := range rows {
		if !strings.Contains(row, bandSeq()) {
			t.Fatalf("the open room's roster row wears no selection band:\n%q", row)
		}
	}
	if !strings.Contains(rows[0], sgr256(hueAccent)) {
		t.Fatalf("the open room's title is not picked out in the accent:\n%q", rows[0])
	}
	// AND NO OTHER ROW WEARS IT. A map with two "you are here" marks is a map.
	for _, row := range a.railRows(height) {
		if strings.Contains(row, bandSeq()) && !strings.Contains(plain(row), "Ship the port") {
			t.Fatalf("a row that is not the open room is marked as well:\n%q", row)
		}
	}

	// THE MARK FOLLOWS NAVIGATION.
	clickRailNode(t, a, 3)
	if !a.roomOpen() || a.room.id != 3 {
		t.Fatalf("the rail did not move rooms: id=%d", roomID(a))
	}
	for _, row := range railRowsFor(a, height, "Ship the port") {
		if strings.Contains(row, bandSeq()) {
			t.Fatalf("the room a person left is still marked:\n%q", row)
		}
	}

	// AND IT CLEARS WHEN THERE IS NO ROOM — the emptiness law, said about a
	// highlight.
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	for _, row := range a.railRows(height) {
		if strings.Contains(row, bandSeq()) {
			t.Fatalf("the roster still marks a room nobody is in:\n%q", row)
		}
	}
}

// THE ROSTER'S TWO MARKS ARE TWO STEPS OF THE LADDER, AND BOTH ARE READABLE AT
// ONCE. This column is the one surface on this screen that carries a persistent
// fact and a transient one side by side — the room you walked into, and the row
// ↑/↓ has reached — and that pair is the whole reason THE GROUND LADDER has a
// cursor step AND a selected step rather than one highlight.
//
// The column used to draw the room on a ground and the keyboard's row with a
// marker and no ground at all, so the two facts were said in two different
// vocabularies and only one of them was a rung. Now the room takes the louder
// step, the cursor takes the quieter one under it, and neither can be mistaken
// for the other.
func TestTheRosterTellsItsCursorRowFromTheRoomYouAreIn(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	height := a.viewHeight()

	clickRailNode(t, a, 1)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the rail did not open node 1: open=%v id=%d", a.roomOpen(), roomID(a))
	}
	a.railTake(true)
	if !a.railHold {
		t.Fatal("the roster did not take the keyboard it was handed")
	}
	// Walk the cursor off the room's own row. A cursor resting on the room you
	// are in is a real state and it is checked at the foot of this test, but the
	// pair only becomes two rows once they part.
	for i := 0; i < len(a.railEntries()) && sameRailRow(a); i++ {
		drive(t, a, key("down"))
	}
	if sameRailRow(a) {
		t.Fatal("the cursor never left the row of the room it opened in")
	}

	var onChosen, onCursor int
	for _, row := range a.railRows(height) {
		chosen, cursor := strings.Contains(row, bandSeq()), strings.Contains(row, hoverBg())
		if chosen && cursor {
			t.Fatalf("one row wears both steps at once:\n%q", row)
		}
		if chosen {
			onChosen++
			if !strings.Contains(plain(row), "Ship the port") {
				t.Fatalf("a row that is not the open room wears the chosen step:\n%q", row)
			}
		}
		if cursor {
			onCursor++
			if strings.Contains(plain(row), "Ship the port") {
				t.Fatalf("the open room's row wears the cursor step:\n%q", row)
			}
		}
	}
	if onChosen == 0 {
		t.Fatalf("the room a person is in wears no ground:\n%s", strings.Join(railText(a, height), "\n"))
	}
	if onCursor == 0 {
		t.Fatalf("the row the keyboard is on wears no ground:\n%s", strings.Join(railText(a, height), "\n"))
	}
	if hueCursor.idx == hueSelected.idx {
		t.Fatal("the two steps resolve to one colour, so the pair says nothing")
	}
	// AND THE MARKER STAYS IN THE LEAD. The ground is the secondary cue; the
	// accent on the leading glyph is the one a person reads first, so the cursor
	// keeps saying where enter is aimed as well as where it is.
	if !strings.Contains(rosterText(a, height), railMark) {
		t.Fatalf("the cursor's row lost its marker to its ground:\n%s", rosterText(a, height))
	}
}

// sameRailRow reports whether the roster's keyboard cursor is standing on the
// row of the room that is open.
func sameRailRow(a *app) bool {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 || at >= len(entries) {
		return false
	}
	return a.roomStandingOn(entries[at].node)
}

// THE MARK SURVIVES BOTH COLUMN WIDTHS AND THE OVERLAY. The roster is drawn at
// three sizes on this surface — the narrow column, the wide one, and the whole
// body on a frame with no columns to lend — and a "you are here" that only one of
// them showed would be a mark a person cannot rely on.
func TestTheRoomsMarkIsOnTheRosterAtEveryWidth(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	clickRailNode(t, a, 1)
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("the rail did not open node 1: id=%d", roomID(a))
	}

	marked := func(t *testing.T, where string) {
		t.Helper()
		rows := railRowsFor(a, a.viewHeight(), "Ship the port")
		if len(rows) == 0 {
			t.Fatalf("%s: the open room's node has no row", where)
		}
		for _, row := range rows {
			if !strings.Contains(row, bandSeq()) {
				t.Fatalf("%s: the open room's row wears no band:\n%q", where, row)
			}
			if ansi.StringWidth(plain(row)) < 4 {
				t.Fatalf("%s: the marked row is not a row:\n%q", where, row)
			}
		}
	}
	marked(t, "the resting column")
	a.railWiden(true)
	marked(t, "the wide column")
	a.railWiden(false)

	// AND OVER THE BODY, which is the roster's third shape.
	drive(t, a, ctrlT())
	if !a.railStanding() {
		t.Fatal("ctrl+t raised no roster")
	}
	marked(t, "the roster over the body")
}

// A RUN'S PAGE IS NOT A NODE'S, and it carries no node id at all
// ([app.openOrchRoom] builds its room with zero on purpose). Nothing in the
// roster may light up off that zero.
func TestARunsPageMarksNoNodeRowOffItsZeroId(t *testing.T) {
	a, _, _ := roomApp(t)
	railRun(a)
	a.room = a.newRoom(0, "a run")
	a.room.orch = &orchRun{id: "run-1", goal: "a run", seen: map[string]bool{}, fresh: map[string]bool{}}
	for _, row := range a.railRows(a.viewHeight()) {
		if strings.Contains(row, bandSeq()) {
			t.Fatalf("a run's page marked a roster row that is not its door:\n%q", row)
		}
	}
	if a.roomStandingOn(a.tasks[1]) {
		t.Fatal("a run's page claims a node row it has nothing to do with")
	}
}

// A node the engine has not published a state for is still a node; nothing here
// may panic on one. It is the cheapest guard on the new predicate and it costs a
// line.
func TestTheRoomsMarkAnswersForANodeThatIsNotThere(t *testing.T) {
	a, _, _ := roomApp(t)
	if a.roomStandingOn(nil) {
		t.Fatal("a nil node is standing in a room")
	}
	a.taskUpdate(update(3, "Write the tree", session.TaskRunning, session.TaskNotice{}))
	if a.roomStandingOn(a.tasks[3]) {
		t.Fatal("a node is marked while no room is open at all")
	}
}
