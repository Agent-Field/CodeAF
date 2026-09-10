package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// EVERYTHING PRESSABLE ANSWERS THE POINTER, AND AT THE SIZE OF THE THING.
//
// hover.go's law is that the set which lights is the set [app.press] acts on,
// and for a wave it was only half kept: a dozen targets took a click and never
// once looked like they would. These pin the other half, one target at a time,
// and they are written the way the defect was found — move the pointer, then
// read what the frame actually says.
//
// TWO ASSERTIONS RECUR AND BOTH MATTER. That the thing under the pointer lights
// is the feature; that NOTHING ELSE ON ITS ROW DOES is the reason most of these
// were not one-line fixes. A row with four doors on it that bands as one is a row
// promising three doors the hand is not on.

// hoverBg is the hover background exactly as the terminal receives it.
func hoverBg() string { return "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m" }

// motionTo is a pointer moved to one cell, column included: every target below
// is narrower than its row, or shares the row with one that is.
func motionTo(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg{X: x, Y: y} }

// ── the task strip ──────────────────────────────────────────────────────────

// A CHIP LIGHTS ALONE. Three doors share that line and each goes somewhere
// else, so a band across the row would offer two rooms nobody is aiming at.
func TestAStripChipLightsWithoutLightingTheRow(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = 80 // no rail at all: the strip is the only door
	a.touch()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Write the auth tests",
		session.TaskRunning, session.TaskNotice{})})
	// Laying the row out is what records the columns (taskstrip.go says why).
	_ = stripText(a)
	if len(a.stripSpans) < 2 {
		t.Fatalf("the strip drew %d chips, want two to tell apart:\n%q",
			len(a.stripSpans), stripText(a))
	}
	first, second := a.stripSpans[0], a.stripSpans[1]

	drive(t, a, motionTo(first.span.from+1, a.headHeight()))
	if !a.hoveringStrip(a.tasks[first.id]) {
		t.Fatalf("the pointer on the first chip recorded %+v", a.hot)
	}
	row := a.stripRow(a.width)
	if got := strings.Count(row, hoverBg()); got != 1 {
		t.Fatalf("hovering one chip lit %d things on the row:\n%q", got, row)
	}

	// The chip beside it is a different door, so it is a different hover.
	drive(t, a, motionTo(second.span.from+1, a.headHeight()))
	if !a.hoveringStrip(a.tasks[second.id]) {
		t.Fatalf("the pointer on the second chip recorded %+v", a.hot)
	}
	if got := strings.Count(a.stripRow(a.width), hoverBg()); got != 1 {
		t.Fatalf("moving to the neighbour lit %d things on the row", got)
	}

	// AND THE GAP BETWEEN TWO CHIPS LIGHTS NOTHING. The press is swallowed there
	// so it cannot fall through, and a gap that brightened would be claiming to be
	// a door.
	if gap := first.span.to; gap < second.span.from {
		drive(t, a, motionTo(gap, a.headHeight()))
		if strings.Contains(a.stripRow(a.width), hoverBg()) {
			t.Fatalf("the gap between two chips lit up:\n%q", a.stripRow(a.width))
		}
	}
}

// THE +N BRIGHTENS RATHER THAN BANDING. It is two characters at the end of a
// row of tabs, and a rectangle round them would be the one boxed thing here.
func TestTheStripsOverflowMarkBrightensUnderThePointer(t *testing.T) {
	a, _, _ := taskApp(t)
	// WIDE ENOUGH THAT THE STRIP IS STILL A STRIP. Under sixty columns the row
	// stops drawing chips at all and becomes the one-line rollup `▸ 5 tasks ·
	// 5 running` (taskstrip.go), which has no +N to point at.
	a.width = 60
	for i := 1; i <= 5; i++ {
		a.taskUpdate(update(uint64(i), "node number "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	_ = stripText(a)
	if !a.stripMore.pressable() {
		t.Fatal("five nodes in sixty columns dropped none of them")
	}
	word := stripMoreWord(5 - len(a.stripSpans))

	drive(t, a, motionTo(a.stripMore.from, a.headHeight()))
	if !a.hoveringStripMore() {
		t.Fatalf("the pointer on the overflow mark recorded %+v", a.hot)
	}
	row := a.stripRow(a.width)
	if !strings.Contains(row, a.pal.accent(word)) {
		t.Fatalf("the overflow mark did not brighten:\n%q", row)
	}
	if strings.Contains(row, hoverBg()) {
		t.Fatalf("the overflow mark took a background band:\n%q", row)
	}
}

// ── a room's pinned header, and the `Stop` under it ─────────────────────────

// THE TRAIL ROW AND `Stop` ARE OPPOSITE GESTURES AND NEVER LIGHT TOGETHER. One
// leaves the page, the other ends the work it is about — and they are on two
// rows now, so the separation is geometric rather than an arbitration.
func TestTheRoomHeaderAndItsMarkLightSeparately(t *testing.T) {
	a, _ := stopApp(t)
	a.openRoomFor(7, "Fix the nil-map crash")
	a.touch()
	width, _ := a.size()
	if _ = strings.Join(a.roomHeadRows(width), "\n"); !a.roomStop.pressable() {
		t.Fatal("the header drew no ✕ to aim at")
	}

	drive(t, a, motionTo(a.roomStop.from, a.roomFactsRow()))
	if !a.hoveringRoomStop() {
		t.Fatalf("the pointer on Stop recorded %+v", a.hot)
	}
	head := strings.Join(a.roomHeadRows(width), "\n")
	if !strings.Contains(head, a.pal.ink(a.linearMark(roomStopMark, roomStopMarkASCII))) {
		t.Fatalf("the ✕ did not brighten under the pointer:\n%q", head)
	}
	if strings.Contains(head, hoverBg()) {
		t.Fatalf("the ✕ banded the whole way-out row:\n%q", head)
	}

	// Anywhere else along the row is the way out, and the way out is the row. The
	// probe is the middle of the rule rather than column two: the trail starts at
	// the label's own column now that the state glyph has moved down a row, so
	// column two is the root crumb and answers as itself (roomcrumbs.go).
	drive(t, a, motionTo(a.roomBackSpan.from+1, a.roomHeadRow()))
	if !a.hoveringRoomBack() {
		t.Fatalf("the pointer on the header recorded %+v", a.hot)
	}
	if head = strings.Join(a.roomHeadRows(width), "\n"); !strings.Contains(head, hoverBg()) {
		t.Fatalf("the header did not light as the way out:\n%q", head)
	}
}

// ── the stop card ───────────────────────────────────────────────────────────

// THE ANSWER UNDER THE POINTER LIGHTS AND NOTHING ELSE DOES. The card is the
// question block's now (question.go), which puts each answer on a row of its own
// — so exactly one row lights, and a band across the whole card would promise
// "stop it" under a hand reaching for "keep going".
func TestTheStopCardsAnswersLightOneAtATime(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, key("x"))
	width, _ := a.size()
	rows := a.questionRows(width) // the layout is what writes the bands
	if len(a.questionBands) != len(stopAnswers) {
		t.Fatalf("the card drew %d pressable answers, want %d", len(a.questionBands), len(stopAnswers))
	}
	for _, band := range a.questionBands {
		y := chromeRowY(t, a, band.row)
		drive(t, a, motionTo(band.span.from+1, y))
		rows = a.questionRows(width)
		if !strings.Contains(rows[band.row], hoverBg()) {
			t.Fatalf("hovering answer %d did not light its row:\n%s", band.at, plain(strings.Join(rows, "\n")))
		}
		// AND THE QUESTION ABOVE THE ANSWERS IS A SENTENCE, which answers to
		// nothing and never lights.
		if strings.Contains(rows[0], hoverBg()) {
			t.Fatalf("hovering answer %d lit the question itself:\n%s", band.at, plain(strings.Join(rows, "\n")))
		}
	}
}

// ── a message parked above the box ──────────────────────────────────────────

// THE WHOLE MESSAGE LIGHTS, because the press pulls the whole message back into
// the box. The dim line under the block belongs to no message and stays dark.
func TestAParkedMessageLightsWholeAndItsFootDoesNot(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	parkLine(t, a, "no, the other file")
	drive(t, a, frameMsg{})
	y := parkedRowY(t, a)

	drive(t, a, motionTo(2, y))
	if !a.hoveringParked(0) {
		t.Fatalf("the pointer on a parked message recorded %+v", a.hot)
	}
	lines := strings.Split(frame(a), "\n")
	if !strings.Contains(lines[y], hoverBg()) {
		t.Fatalf("the parked message did not light:\n%q", lines[y])
	}
	if strings.Contains(lines[y+1], hoverBg()) {
		t.Fatalf("the line under the block lit up:\n%q", lines[y+1])
	}

	drive(t, a, motionTo(2, y+1))
	if a.hot.kind != hoverNothing {
		t.Fatalf("the dim line answered the pointer: %+v", a.hot)
	}
}

// ── the tray above the box ──────────────────────────────────────────────────

// A PICTURE ON THE TRAY LIGHTS ON ITS OWN CELLS. Each chip takes a different
// thing off the message being written, so hovering one must not offer the other.
func TestATrayChipLightsOnItsOwnCells(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 12, "two.png": 12})
	a.attach(filepath.Join(dir, "one.png"))
	a.attach(filepath.Join(dir, "two.png"))

	// The tray is the input block's FIRST row, read off the layout's own marks
	// rather than counted back from the foot of the chrome: the breathing blank
	// moved under the box on 2026-09-09 and a count would be a row out
	// (attach.go's [app.chipTrayTarget] says the whole of it).
	y := trayRow(a)
	labels := chipLabels(a.chips, a.pal)
	x := len(inputPad) + ansi.StringWidth(labels[0]) + len(chipGap) + 1

	drive(t, a, motionTo(x, y))
	if !a.hoveringChip(1) {
		t.Fatalf("the pointer on the second chip recorded %+v", a.hot)
	}
	strip := a.chipStrip(a.width - len(inputPad))
	if got := strings.Count(strip, hoverBg()); got != 1 {
		t.Fatalf("hovering one picture lit %d things on the tray:\n%q", got, strip)
	}
	if !strings.Contains(strip, a.pal.cursor(a.pal.dim(labels[1]), 0)) {
		t.Fatalf("the wrong chip lit:\n%q", strip)
	}
}

// ── an adaptive run's page ──────────────────────────────────────────────────

// roomRowY is the SCREEN row one row of the open page landed on. It is
// [app.roomRowAt]'s arithmetic run backwards, which is how a test builds a
// pointer a person could actually have.
func roomRowY(a *app, at int) int {
	rows := a.roomRows(a.bodyWidth())
	return a.bodyTop() + at - a.roomOffsetFor(len(rows), a.viewHeight())
}

// A NODE IS A ROW AND THE ROW LIGHTS ALONE. The old wide tier put a whole
// layer of chips on one line — ids without goals, strokes without meaning —
// and this test used to guard that arrangement's hover. The page draws one
// node per row now, its goal beside its id and its needs on the row's dim
// tail, so hover is the ordinary full-width band and can never light a
// neighbour.
func TestARunPagesNodeRowsCarryGoalsAndLightAlone(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	rows := a.roomRows(a.bodyWidth())
	run := a.orchOf()

	for i := range rows {
		if i < len(run.spots) && len(run.spots[i]) > 1 {
			t.Fatalf("row %d carries %d nodes; every node has its own row now", i, len(run.spots[i]))
		}
	}
	page := roomText(a)
	for _, want := range []string{orchWorkHead, "read the three RFCs", "needs rfcs client"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page never says %q:\n%s", want, page)
		}
	}

	at := -1
	for i := range rows {
		if i < len(run.spots) && len(run.spots[i]) == 1 && run.spots[i][0].node == "write" {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no row carries the write node:\n%s", page)
	}
	y := roomRowY(a, at)
	drive(t, a, motionTo(4, y))
	if !a.hoveringOrch(run.spots[at][0].key()) {
		t.Fatalf("the pointer on the write row recorded %+v", a.hot)
	}
	if got := strings.Count(a.roomRows(a.bodyWidth())[at].text, hoverBg()); got != 1 {
		t.Fatalf("hovering one node lit %d things on its row", got)
	}
}

// ── the task record card ────────────────────────────────────────────────────

// THE CARD'S EDGES ARE THE WAY BACK AND THEY SAY SO NOW. Its body is read and
// stays dark, and the two edges are two hovers — they are at opposite ends of
// the screen, and one of them lighting the other would be a card with no answer
// to "which of these am I on".
func TestTheTaskRecordCardLightsTheEdgeUnderThePointer(t *testing.T) {
	a, _, _ := taskApp(t)
	a.comp.tasks = []session.TaskIndexEntry{pastTask("9", "port-the-parser", "Port the parser", 0)}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open on a project with only a record")
	}
	drive(t, a, key("enter"))
	if !a.taskSheet.detailOn {
		t.Fatal("enter did not go inside the card")
	}

	drive(t, a, motionTo(2, 0))
	if !a.hoveringTaskCard(int(taskCardHitHead)) {
		t.Fatalf("the pointer on the card's title recorded %+v", a.hot)
	}
	lines := strings.Split(frame(a), "\n")
	if !strings.Contains(lines[0], hoverBg()) {
		t.Fatalf("the card's title did not light:\n%q", lines[0])
	}
	if !strings.Contains(lines[1], hoverBg()) {
		t.Fatalf("the blank under the title is part of the target and did not light")
	}
	if strings.Contains(lines[len(lines)-1], hoverBg()) {
		t.Fatalf("hovering the title lit the foot at the other end of the screen")
	}

	drive(t, a, motionTo(2, len(lines)-1))
	if !a.hoveringTaskCard(int(taskCardHitFoot)) {
		t.Fatalf("the pointer on the card's foot recorded %+v", a.hot)
	}
	lines = strings.Split(frame(a), "\n")
	if !strings.Contains(lines[len(lines)-1], hoverBg()) || strings.Contains(lines[0], hoverBg()) {
		t.Fatal("the foot and the title did not swap the highlight")
	}

	// The report between them is read, not pressed.
	drive(t, a, motionTo(2, 5))
	if a.hot.kind != hoverNothing {
		t.Fatalf("the card's body answered the pointer: %+v", a.hot)
	}
}

// ── a task reference in somebody's sentence ─────────────────────────────────

// THE WORDS BRIGHTEN AND THE PARAGRAPH DOES NOT. A reply can name four nodes,
// so a band across the sentence would be four doors offered at once — and a
// rectangle mid-paragraph would be the one boxed thing on the screen.
func TestATaskLinkBrightensUnderThePointerAndItsSentenceDoesNot(t *testing.T) {
	a := linkLab(t, "I split this into task 7 and task 8, and that is all.")
	r, y, ok := linkedRow(a)
	if !ok || len(r.links) != 2 {
		t.Fatalf("the answer grew %d links, want two:\n%s", len(r.links), strings.Join(plainRows(a), "\n"))
	}

	drive(t, a, motionTo(r.links[1].span.from+1, y))
	if got := a.hoveringLink(r.entry); got != r.links[1].ord {
		t.Fatalf("the pointer on the second link recorded link %d, want %d", got, r.links[1].ord)
	}
	again, _, _ := linkedRow(a)
	if strings.Contains(again.text, hoverBg()) {
		t.Fatalf("a task link took a background band:\n%q", again.text)
	}
	if !strings.Contains(again.text, a.pal.underline(a.pal.ink("task 8"))) {
		t.Fatalf("the hovered link did not brighten to ink:\n%q", again.text)
	}
	if !strings.Contains(again.text, a.pal.underline(a.pal.accent("task 7"))) {
		t.Fatalf("the link beside it moved when the pointer was not on it:\n%q", again.text)
	}

	// The prose between them is not a door and does not react.
	drive(t, a, motionTo(r.links[0].span.from-2, y))
	if a.hoveringLink(r.entry) >= 0 {
		t.Fatalf("the sentence between two links answered the pointer: %+v", a.hot)
	}
}

// ── a sign-in still waiting ─────────────────────────────────────────────────

// THE WHOLE CARD LIGHTS, because the whole card is the target: it has one thing
// to do — copy the address — and it does it wherever it is pressed.
func TestAWaitingSignInLightsAsOneBlock(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
	}))

	at := -1
	for i := range a.entries {
		if a.entries[i].kind == entryConnect {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no sign-in block on the screen:\n%s", strings.Join(plainRows(a), "\n"))
	}
	y := screenRowOf(t, a, func(r row) bool { return r.entry == at })

	drive(t, a, motionTo(2, y))
	if !a.hoveringEntry(at) {
		t.Fatalf("the pointer on a waiting sign-in recorded %+v", a.hot)
	}
	lit := 0
	for _, r := range a.visible(a.bodyWidth()) {
		if r.entry == at && strings.Contains(r.text, hoverBg()) {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("the waiting sign-in did not light under the pointer")
	}
}

// ── the phone status deck ───────────────────────────────────────────────────

// EITHER ROW LIGHTS WHOLE, because every cell of both opens something: the chip
// its picker, everything else the sheet. There is no part of them a band would
// be promising a door it does not have.
func TestThePhoneDecksRowsLightUnderThePointer(t *testing.T) {
	a, _ := deckApp(t)
	_, height := a.size()

	for row := 0; row < deckHeight; row++ {
		y := height - deckHeight + row
		drive(t, a, motionTo(2, y))
		if !a.hoveringDeck(row) {
			t.Fatalf("the pointer on deck row %d recorded %+v", row, a.hot)
		}
		lines := strings.Split(frame(a), "\n")
		if !strings.Contains(lines[y], hoverBg()) {
			t.Fatalf("deck row %d did not light:\n%q", row, lines[y])
		}
		other := height - deckHeight + (1 - row)
		if strings.Contains(lines[other], hoverBg()) {
			t.Fatalf("hovering deck row %d lit the other one too", row)
		}
	}
}
