package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE TASK STRIP: ONE ROW THAT ALWAYS HAS A DOOR IN IT.
//
// The roster is thirty columns on the right and it is the first thing a narrow
// frame gives up (task.go's railSlimFloor). That was the right trade for the
// conversation and the wrong one for the work: under a hundred columns — a
// phone over mosh, a split pane, a terminal somebody dragged half-shut — a
// running node had NO door at all. It was in the transcript as a card that
// scrolled away, and nowhere else. "Where is the thing that is running" is the
// question this surface exists to answer, and on the frames people actually
// carry around it could not answer it.
//
// So there is one row, under the pinned header, that says what is alive:
//
//	⠙ Fix nil-map · ◆ Auth tests · +2
//
// It is a TAB ROW and not a summary. Every chip is a door — click it and you
// are in that node's room — and the +N at its end is the door to the whole
// roster, which at a narrow width opens over the frame (task.go's [app.railFull]).
// It is drawn at EVERY width, because a row of tabs and a column of rows answer
// different questions: the strip says what is happening now, in one glance,
// above whatever a person is reading; the roster says what the session has done
// all day. The strip goes away the moment nothing is running, which is what
// keeps it from becoming another permanent bar.
//
// THE CHIP IS A GLYPH AND A NAME. The glyph is the node's STATE where it has
// one worth drawing — the spinner while it runs, ✓ and ✗ once it is over — and
// its IDENTITY otherwise (taskident.go's ◆, keyed on the id and stable for the
// node's whole life), which is the same pair of cells the roster opens its rows
// with. The name is the two-or-three-word title, cut to fit. Nothing else: a
// clock, a spend or a tool name on this row would make it a dashboard, and the
// room is one keystroke away.
//
// ── AND THE CHIP IS SHAPED LIKE A CHIP ──
//
// It used to be a bare run of text with a middot beside it, which is the shape
// of a LIST, and the row is not a list — it is the tab bar for the page below
// it. Read as a list, the one thing the row exists to say last of all was the
// thing hardest to see: which of these doors you are standing in. So a chip is
// padded, one cell each side, and the padding is what a band has to fill:
//
//	  ⠙ Fix nil-map    ◆ Auth tests   +2
//	 ^^^^^^^^^^^^^^^^
//	 the room you are in, banded
//
// TWO MARKS, BECAUSE THERE ARE TWO QUESTIONS. The OPEN chip — the room the body
// is drawing — takes the band and the accent, which is the emphasis this
// surface already spends on "the row you picked" (styles.go's [palette.band],
// one step above the pointer's own hover). The FOCUSED chip — wherever the
// roster's cursor is standing while it holds the keyboard (task.go's
// [app.railWhere]) — takes an underline instead, so that moving the cursor
// across the row never once looks like opening something. A chip can wear both,
// and when it does both are legible, which is the point of picking two channels
// rather than two shades of one.
//
// THE CURSOR IS THE ROSTER'S AND NOT A SECOND ONE. A tab row with a keyboard
// model of its own would be a third list to navigate on a surface that already
// has the column and the transcript; the strip is a VIEW of the roster's live
// set, so it shows the roster's cursor and adds no keys.
//
// The padding also belongs to the CHIP for the pointer: [stripSpan] covers it,
// so the cell beside a name opens the same room the name does. A one-cell gap
// separates two chips, which is what is left over once each one carries its own
// air — the middot went with the list it punctuated.

const (
	// stripFloor is the narrowest frame that gets a strip. Under it there is not
	// a chip's worth of room, and the row would be an ellipsis with a glyph in
	// front of it.
	stripFloor = 24
	// stripTitleCap is how much of a name one chip may spend. Eighteen is the
	// rail's own title budget at the full width — the same name, cut the same
	// way, so a node reads identically in both places.
	stripTitleCap = 18
	// stripGap is what separates two chips, and stripGapCols is its width in
	// CELLS. The two are stated apart because a budget measured in bytes would
	// fit fewer chips than the row can hold the moment the separator is not
	// ASCII.
	stripGap     = " "
	stripGapCols = 1
	// stripPad is the air inside a chip, one cell each side. It is what makes a
	// band read as a tab rather than as a highlighted word, and it is inside the
	// chip's own span so the pointer may land on it.
	stripPad     = " "
	stripPadCols = 2
)

// stripOrder is the order the chips come in, and it is not the roster's.
//
// The column leads with what is asking for a decision because a person reads it
// top to bottom looking for work to do. The strip leads with what is RUNNING
// because it is a presence row: it exists at all only while something is
// running, and the first chip is the thing a person is waiting on. What is
// parked and what is done are not on it — a strip is the live set, and the
// roster is where a session's history lives.
var stripOrder = [...]railGroup{railRunning, railAttention, railIdle}

// stripSpan is one chip's columns and the node behind them. It is written at
// LAYOUT and read by the click, which is the same bargain the status row's model
// segment makes (render.go's [app.identityParts]): the geometry is recorded
// where it is decided, because a hit-test that recomputed it would be measuring
// a row the frame has not drawn.
type stripSpan struct {
	span  hudSpan
	id    uint64
	title string
}

// stripShowing reports whether the frame carries a strip right now.
//
// ONE RUNNING NODE RAISES IT AND NOTHING ELSE DOES. A session whose work has all
// landed has nothing to keep a door open to — the cards are in the transcript
// and the roster still holds every one of them — and a permanent row that says
// "nothing is running" is a row of chrome bought with a row of conversation.
func (a *app) stripShowing() bool {
	// The fullscreen roster IS this row's destination, so drawing a tab row of
	// the same nodes above it would be the list with its own index on top.
	if a.railFull() {
		return false
	}
	width, height := a.size()
	// The same floor the pinned header stands on (view.go's [app.headHeight]): a
	// terminal too short for breathing room spends what it has on the
	// conversation and the box.
	if width < stripFloor || height < 6 {
		return false
	}
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && node.state == session.TaskRunning {
			return true
		}
	}
	return false
}

// stripHeight is what the strip costs the body region. It is one row or none,
// and it is subtracted in [app.topHeight] — the number every geometric question
// about the body resolves through.
func (a *app) stripHeight() int {
	if a.stripShowing() {
		return 1
	}
	return 0
}

// stripNodes is the live set, in the order the chips are drawn.
func (a *app) stripNodes() []*taskNode {
	members := a.railMembers()
	out := make([]*taskNode, 0, len(a.taskOrder))
	for _, g := range stripOrder {
		out = append(out, members[g]...)
	}
	return out
}

// stripRow is the row, and it is where the chips' columns are recorded.
//
// It returns "" when there is no strip, which is what the frame draws for a row
// it is not spending (view.go).
func (a *app) stripRow(width int) string {
	a.stripSpans, a.stripMore = nil, hudSpan{}
	if !a.stripShowing() || width <= 0 {
		return ""
	}
	nodes := a.stripNodes()
	if len(nodes) == 0 {
		return ""
	}
	// HOW MANY FIT IS DECIDED BEFORE ANYTHING IS PAINTED, because the +N at the
	// end is part of the budget: a row that laid chips down until it ran out
	// would have no room left to say how many it dropped, which is the one thing
	// the person who cannot see them needs.
	kept, used := 0, 0
	for i := range nodes {
		_, w := a.stripLabel(nodes[i])
		lead := 0
		if i > 0 {
			lead = stripGapCols
		}
		reserve := 0
		if rest := len(nodes) - i - 1; rest > 0 {
			reserve = stripGapCols + ansi.StringWidth(stripMoreWord(rest))
		}
		if used+lead+w+reserve > width {
			break
		}
		used += lead + w
		kept = i + 1
	}
	if kept == 0 {
		// A frame too narrow for one whole chip and its overflow mark still says
		// what is running: the first chip, cut to the row. The mark is dropped
		// rather than the name — "+3" with no name beside it is a count of things
		// a person cannot identify.
		text, _ := a.stripLabel(nodes[0])
		a.stripSpans = []stripSpan{{span: hudSpan{from: 0, to: width}, id: nodes[0].id, title: nodes[0].title}}
		return fit(text, width)
	}

	var out strings.Builder
	at := 0
	for i := 0; i < kept; i++ {
		if i > 0 {
			out.WriteString(stripGap)
			at += stripGapCols
		}
		text, w := a.stripLabel(nodes[i])
		out.WriteString(text)
		a.stripSpans = append(a.stripSpans, stripSpan{
			span:  hudSpan{from: at, to: at + w},
			id:    nodes[i].id,
			title: nodes[i].title,
		})
		at += w
	}
	if rest := len(nodes) - kept; rest > 0 {
		word := stripMoreWord(rest)
		out.WriteString(stripGap)
		at += stripGapCols
		out.WriteString(a.pal.dim(word))
		a.stripMore = hudSpan{from: at, to: at + ansi.StringWidth(word)}
	}
	return out.String()
}

// stripMoreWord is the overflow mark: how many chips the row could not hold.
func stripMoreWord(n int) string { return "+" + itoa(n) }

// stripLabel is one chip, painted, and the CELLS it occupies. The two are
// returned together because the budget is spent in cells and the label is
// carried in bytes: the width is taken through [ansi.StringWidth], which is what
// every other measurement on this surface goes through (task.go's [app.railJoin]).
//
// The band goes on LAST, over an already-painted chip, which is the same bargain
// [palette.hover] makes with the rows it lights: every foreground sequence on
// this surface closes with SGR 39, so a background wrapped round one leaves the
// ink underneath alone.
func (a *app) stripLabel(node *taskNode) (string, int) {
	glyph := a.stripGlyph(node)
	title := fit(node.title, stripTitleCap)
	cols := ansi.StringWidth(glyph) + 1 + ansi.StringWidth(title) + stripPadCols
	chip := stripPad + glyph + " " + a.stripTitle(node, title) + stripPad
	if a.room != nil && a.room.id == node.id {
		chip = a.pal.band(chip, cols)
	}
	if a.stripFocused(node) {
		chip = a.pal.underline(chip)
	}
	return chip, cols
}

// stripFocused reports whether the roster's cursor is standing on this node.
//
// It reads the column's state and keeps none of its own ([app.railHold] and
// [app.railWhere], task.go): the strip is a view of the same live set, and a
// second cursor over one list is two answers to "where am I". While the roster
// does not hold the keyboard there is no cursor to draw, which is the honest
// reading — the draft has the keys, and nothing on this row is being aimed at.
func (a *app) stripFocused(node *taskNode) bool {
	return a.railHold && a.railWhere.id != 0 && a.railWhere.id == node.id
}

// stripGlyph is the chip's one cell: the node's state where the state is worth
// a glyph, its identity otherwise.
//
// A QUEUED NODE WEARS ITS IDENTITY rather than the roster's hollow circle. The
// circle is a useful thing to say in a column where every row has one and the
// eye is scanning states; on a row of three chips the question is WHICH work,
// not what phase it is in, and the identity cell is the only mark on this
// surface that answers it the same way on every frame (taskident.go).
func (a *app) stripGlyph(node *taskNode) string {
	if node.state == session.TaskQueued {
		return a.taskMark(node.ident)
	}
	return a.railGlyph(node)
}

// stripTitle paints an already-cut name: the accent on the room a person is
// standing in, the ink on what is running, the muted voice on everything else —
// the rail's own law ([app.railTitle]) with one addition, because this row is
// drawn ABOVE the page it is a tab bar for and a tab bar that did not say which
// tab you are on would be a row of identical doors.
func (a *app) stripTitle(node *taskNode, title string) string {
	switch {
	case a.room != nil && a.room.id == node.id:
		return a.pal.bold(a.pal.accent(title))
	case node.state == session.TaskRunning:
		return a.pal.ink(title)
	}
	return a.pal.muted(title)
}

// stripPress resolves a click on the strip, and reports whether it took it.
//
// THE ROW IS RESOLVED BEFORE THE COLUMN, for the reason [app.statusPress] states
// about the model segment: laying the row out is what writes the spans, and
// reading them first would be reading where the chips were drawn on the frame
// before this one.
//
// A press ANYWHERE on the row is the row's, whether or not it landed on a chip.
// The strip sits above the conversation and beside nothing, so a click that fell
// through it would act on a transcript row the pointer was not over — and the
// gap between two chips is three cells wide, which is a place people miss.
func (a *app) stripPress(x, y int) (tea.Cmd, bool) {
	if a.sheet.open || a.copy.on || a.welcome.open {
		return nil, false
	}
	if a.stripHeight() == 0 || y != a.headHeight() {
		return nil, false
	}
	width, _ := a.size()
	if a.stripRow(width) == "" {
		return nil, false
	}
	for _, chip := range a.stripSpans {
		if chip.span.holds(x) {
			a.openRoomFor(chip.id, chip.title)
			return a.takeRoomPump(), true
		}
	}
	// THE OVERFLOW MARK IS THE DOOR TO THE WHOLE ROSTER, which is the roster
	// wherever this frame keeps it: the column on a wide one, the fullscreen
	// overlay on a narrow one (task.go's [app.railTake]). One gesture, one
	// destination, two shapes.
	if a.stripMore.holds(x) {
		a.railTake(true)
		return nil, true
	}
	return nil, true
}
