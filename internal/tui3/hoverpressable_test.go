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

// ── the top bar's crumb ─────────────────────────────────────────────────────

// EACH STEP OF THE CRUMB IS ITS OWN DOOR, AND WHERE YOU ARE IS NONE. The
// project goes home, the chat's name climbs out of the room, and the current
// segment answers to nothing — a door to nowhere must not light. Three spans
// share one row, so any of them answering for a neighbour's cells would be the
// bar offering a page the hand is not on.
func TestTheCrumbLightsOneStepAtATimeAndTheCurrentStepNever(t *testing.T) {
	a, _ := stopApp(t)
	// The chat's step exists only where the chat has a name of its own
	// (topbar.go's [app.crumbSegments]); this is the door under test.
	a.title = "port the lexer"
	a.openRoomFor(7, "Fix the nil-map crash")
	a.touch()
	width, _ := a.size()
	// Laying the bar out is what records the columns (topbar.go says why).
	if _ = a.topBarWord(width); !a.crumbHomeSpan.pressable() || !a.crumbChatSpan.pressable() {
		t.Fatalf("the bar drew no crumb to aim at: home=%+v chat=%+v\n%q",
			a.crumbHomeSpan, a.crumbChatSpan, a.topBarWord(width))
	}

	// The project step is space space parity: home.
	drive(t, a, motionTo(a.crumbHomeSpan.from, 0))
	if a.hot.kind != hoverHome {
		t.Fatalf("the pointer on the project step recorded %+v", a.hot)
	}

	// The chat's name leaves the room however deep the trail is, and it is a
	// different door from home's — AND FROM THE BACK WORD'S beside it, which
	// climbs exactly one level. At one level down the two land in the same
	// place and at two they do not, so they light separately (hover.go's
	// [hoverCrumbChat] says why they are two kinds).
	drive(t, a, motionTo(a.crumbChatSpan.from, 0))
	if !a.hoveringCrumbChat() {
		t.Fatalf("the pointer on the chat's name recorded %+v", a.hot)
	}
	if a.hoveringRoomBack() {
		t.Fatal("the chat's step lit the back word beside it")
	}

	// THE CURRENT SEGMENT IS WHERE YOU ALREADY ARE, so it carries no span: the
	// cells between the chat's name and the right cluster's doors answer to
	// nothing, and a crumb that lit there would be promising a press that does
	// nothing.
	drive(t, a, motionTo(a.crumbChatSpan.to+1, 0))
	if a.hot.kind != hoverNothing {
		t.Fatalf("the current segment answered the pointer: %+v", a.hot)
	}
}

// ── the top bar's right end: the way out and the ✕ ──────────────────────────

// THE BACK WORD AND THE ✕ ARE OPPOSITE GESTURES AND NEVER LIGHT TOGETHER. One
// leaves the page, the other ends the work it is about, and the expensive one
// wins the cells it is drawn on.
func TestTheTopBarsWayOutAndItsMarkLightSeparately(t *testing.T) {
	a, _ := stopApp(t)
	a.openRoomFor(7, "Fix the nil-map crash")
	a.touch()
	width, _ := a.size()
	if _ = a.topBarWord(width); !a.roomStop.pressable() {
		t.Fatal("the bar drew no ✕ to aim at")
	}
	if !a.backSpan.pressable() {
		t.Fatal("the bar drew no back word to aim at")
	}

	drive(t, a, motionTo(a.roomStop.from, 0))
	if !a.hoveringRoomStop() {
		t.Fatalf("the pointer on the ✕ recorded %+v", a.hot)
	}

	// The back word beside it is the way out, and a different hover: a hand
	// reaching for "leave" must never be offered "end it".
	drive(t, a, motionTo(a.backSpan.from, 0))
	if !a.hoveringRoomBack() {
		t.Fatalf("the pointer on the back word recorded %+v", a.hot)
	}
	if a.hoveringRoomStop() {
		t.Fatal("the back word lit the ✕'s hover")
	}
}

// ── the top bar's YOLO term ─────────────────────────────────────────────────

// THE OPEN GATE LIGHTS AT THE SIZE OF THE WORD. YOLO is a safety affordance —
// seeing the gate open has to offer the way to close it — and it is never
// dropped by the width ladder, so the span is on the bar at every width the
// bar is drawn at. A hover that answered for the whole right cluster would be
// offering the Settings page under a hand reaching for the model.
func TestTheYoloTermLightsAtTheSizeOfTheWord(t *testing.T) {
	a, _ := stopApp(t)
	a.approval = "allow"
	a.touch()
	width, _ := a.size()
	if _ = a.topBarWord(width); !a.topYoloSpan.pressable() {
		t.Fatalf("an open gate drew no YOLO to aim at:\n%q", a.topBarWord(width))
	}

	drive(t, a, motionTo(a.topYoloSpan.from, 0))
	if a.hot.kind != hoverYolo {
		t.Fatalf("the pointer on YOLO recorded %+v", a.hot)
	}

	// The model beside it is a different door, so it is a different hover.
	if a.modelSpan.pressable() {
		drive(t, a, motionTo(a.modelSpan.from, 0))
		if !a.hoveringStatusModel() {
			t.Fatalf("the pointer on the model recorded %+v", a.hot)
		}
	}

	// AND A CLOSED GATE IS NOTHING: the capability that cannot work is absent,
	// so there is no span and the cells answer to nothing.
	a.approval = ""
	a.hot = hoverAt{}
	if _ = a.topBarWord(width); a.topYoloSpan.pressable() {
		t.Fatal("a closed gate kept its span on the bar")
	}
}

// ── the stop card ───────────────────────────────────────────────────────────

// THE TWO ANSWERS ARE THE TWO ENDS OF ONE DECISION, so exactly one of them
// lights: a band across the row would promise "stop it" under a hand reaching
// for "keep going".
func TestTheStopCardsAnswersLightOneAtATime(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, key("x"))
	width, _ := a.size()
	a.guardRows(width) // the layout is what writes the spans
	row, ok := stopCardRow(a)
	if !ok || len(a.stop.spans) != len(stopAnswers) {
		t.Fatalf("the card is not on the frame with its targets: row=%v spans=%d", ok, len(a.stop.spans))
	}

	for at := range stopAnswers {
		drive(t, a, motionTo(a.stop.spans[at].from+1, row))
		if !a.hoveringStopAnswer(at) {
			t.Fatalf("the pointer on answer %d recorded %+v", at, a.hot)
		}
		line := a.stopRows(width)[1]
		if got := strings.Count(line, hoverBg()); got != 1 {
			t.Fatalf("hovering answer %d lit %d things on the row:\n%q", at, got, line)
		}
	}

	// The question above the answers is a sentence and answers to nothing.
	drive(t, a, motionTo(2, row-1))
	if a.hot.kind != hoverNothing {
		t.Fatalf("the card's question answered the pointer: %+v", a.hot)
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

	width, height := a.size()
	rows, _, _, _ := a.chrome(width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	y := height - len(rows) + at
	labels := chipLabels(a.chips, a.pal)
	x := len(inputPad) + ansi.StringWidth(labels[0]) + len(chipGap) + 1

	drive(t, a, motionTo(x, y))
	if !a.hoveringChip(1) {
		t.Fatalf("the pointer on the second chip recorded %+v", a.hot)
	}
	strip := a.chipStrip(width - len(inputPad))
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
	a := deckApp(t)
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
