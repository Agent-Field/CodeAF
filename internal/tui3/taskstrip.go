package tui3

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
// surface already spends on "the row you picked" (styles.go's [palette.selected],
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
//
// ── AND THE ROSTER OWNS THE SHAPE ───────────────────────────────────────────
//
// An adaptive run is not a task. It is a task that keeps SPAWNING tasks, for as
// long as its planner has something left to want (internal/orchestrate), and a
// row of sibling chips is a lie about it: eight chips that read as eight jobs
// somebody asked for, when what happened is one job that grew seven. The shape
// is the news — what came out of what, and which limb is still moving.
//
// THAT TREE IS THE ROSTER'S AND NOT THIS ROW'S. It was drawn here for a while,
// growing downward out of a pinned row and folding itself to a budget the frame
// set, which made the one surface a person cannot scroll the one surface that
// decided for itself how many rows of conversation to eat. The column on the
// right is the shape a tree wants — it is already tall, it already scrolls, and
// a person can fold it themselves (task.go's [app.railEntries]) — so the tree
// lives there and this file kept the thing it was always good at.
//
// SO THE STRIP IS THE NARROW FRAME'S DOOR AND NOTHING ELSE. It stands DOWN
// wherever the roster is standing ([app.stripShowing]): a tab row above a column
// listing the same work is a row of conversation spent on an index to the thing
// beside it. Under [railSlimFloor] there is no column to lend, so this row is
// the only door there is, and it draws at every width down to [stripFloor] — one
// line, the live chips packed across it, the +N at its end.

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

// ── THE PARENT SEAM ─────────────────────────────────────────────────────────
//
// These two are the whole of what the roster's forest reads (task.go's
// [app.railKin]). The parent is filled from the
// engine's own updates (session's TaskNotice.Parent), and TWO KINDS OF WORK FILL
// IT: an adaptive run, which takes one row with a row under it for every node it
// cuts (session's orchestrate.go), and a TASK THAT SPLIT ITS OWN BRIEF, whose
// pieces are registered under it (session's task.go). Neither is anything to
// this package: a family is a family. A session that has run neither answers ""
// and false to both, which is the flat row, unchanged — and that is what keeps
// this a seam rather than a rewrite.
//
// Paused is filled the same way, from session's TaskNotice.Paused: an adaptive
// run that has spent its tank publishes its OWN row held at the gate until
// somebody tops it up, finishes it or stops it, and the workers under it keep
// publishing whatever they are actually doing. The run's page still asks the
// question; the paused mark is how the column says the run is standing still
// while it does.
//
// THE KEY IS A STRING AND THE ID IS NOT, on purpose. The thing that will fill
// it is an orchestrate node id (internal/orchestrate's [orchestrate.Node.ID] —
// "n3", not a number), so a uint64 here would be a conversion in the adapter
// and a lie in the type. And "" is an honest "nobody spawned this", where 0 is
// an id that could one day exist.

// ParentID is the key of the task this one was spawned under, or "" at a root.
func (n *taskNode) ParentID() string { return n.parent }

// Paused reports whether this task is HELD rather than working: an adaptive run
// stopped at its fuel gate, waiting for a person to top it up or finish it
// (session's EventOrchestratePause, and TaskNotice.Paused on the row). It is not
// a state the engine moves a node through, which is why it is a fact of its own
// — a paused node is still running as far as the run is concerned, and it is not
// moving as far as a person is concerned, and the second reading is the one a
// roster owes them. The reading that decides how the ROW is grouped and counted
// is [session.ProjectTask]'s, which takes the same fact through
// [app.taskStatus]: work that will not move until a person says something.
func (n *taskNode) Paused() bool { return n.paused }

// stripKey is a node's own key in the alphabet [taskNode.ParentID] speaks.
func stripKey(node *taskNode) string { return itoa(int(node.id)) }

// stripOrder is the order the chips come in, and it is not the roster's.
//
// The column leads with what is asking for a decision because a person reads it
// top to bottom looking for work to do. The strip leads with what is RUNNING
// because its expanded chips can name each item. The row stays while work is
// running, queued or needs attention; the compact phone summary puts attention first. What is
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
	if a.startingChat() {
		return false
	}
	width, height := a.size()
	// The same floor the pinned header stands on (view.go's [app.headHeight]): a
	// terminal too short for breathing room spends what it has on the
	// conversation and the box.
	if width < stripFloor || height < roomyFloor {
		return false
	}
	// A RUNNING SUB-HARNESS RAISES THE ROW even where the roster is standing
	// (harnesspanel.go). It is alive for minutes at a time and it is the only
	// thing on screen that would say so — the run happens inside one tool call,
	// so the transcript shows a single row that has not come back yet, and the
	// roster cannot carry it either: its rows are the session's task nodes, and
	// a harness run is not one. The stand-down below is about not indexing a
	// list next to the list; this chip is on no list to index.
	if _, running := a.runningHarness(); running {
		return true
	}
	// AND IT STANDS DOWN WHEREVER THE ROSTER IS STANDING, in either of the
	// roster's two shapes (task.go's [app.railStanding]). The column beside the
	// conversation and the overlay over it are both the whole list of this
	// session's work, with the shape of each run in them; a tab row above either
	// one is a row of conversation spent on an index to the thing next to it. What
	// is left for this row is the frame the roster cannot have — under
	// [railSlimFloor], with nobody asking for the overlay — a column somebody
	// closed with ctrl+g, and the harness chip above.
	if a.railStanding() {
		return false
	}
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && slices.Contains(stripOrder[:], a.railGroupOf(node)) {
			return true
		}
	}
	return false
}

// stripHeight is what the strip costs the body region, and it is subtracted in
// [app.topHeight] — the number every geometric question about the body resolves
// through.
//
// IT IS THE LAYOUT'S OWN COUNT AND NOT A SECOND OPINION. A cheaper answer
// computed a second way here is the one bug this arithmetic cannot survive: a
// frame that drew a row and budgeted for none puts the conversation's last line
// under the input box. So it lays the strip out and counts what came back, at
// the terminal's own width, the way [app.topHeight] requires of everything it
// adds up.
func (a *app) stripHeight() int {
	width, _ := a.size()
	return len(a.stripRows(width))
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

// stripRows is the strip, laid out, and it is where every chip's columns are
// recorded.
//
// It is ROWS rather than a row because the frame adds it row by row (view.go),
// and because a surface that returns "" for "there is no strip" and a string for
// "there is one" is a height nobody counted. There is at most one row of CHIPS —
// and one blank row under it, which is the strip's own separation from the
// conversation: the chips are chrome sitting directly on top of somebody's
// paragraph, and with nothing between them the top of the transcript read as
// the strip's second line. Whitespace is how this surface separates blocks —
// a rule or a border under the chips is exactly what the design law refuses —
// so the gap is a row of the strip and counted as one ([app.stripHeight]),
// never an off-by-one the body pays for.
func (a *app) stripRows(width int) []string {
	a.stripSpans = nil
	a.stripMore, a.stripHarn = hudSpan{}, hudSpan{}
	if !a.stripShowing() || width <= 0 {
		return nil
	}
	// phone lane: the strip is one full-width door into the roster page rather
	// than a row of chips a thumb cannot land between (taskphone.go). It sets no
	// spans, so [app.stripPress] falls through to the whole-row door below.
	if door, ok := a.stripPhoneDoor(width); ok {
		return []string{door, ""}
	}
	if row := a.stripRowText(gutterInner(width), a.stripNodes()); row != "" {
		// The chip keeps its own inner padding inside the reading gutter. Its
		// pointer targets move with the row, and every layout resets them above
		// so repeated hover reads cannot accumulate an extra indent.
		lead := textGutterCols(width)
		if lead > 0 {
			row = strings.Repeat(" ", lead) + row
			for i := range a.stripSpans {
				a.stripSpans[i].span = a.stripSpans[i].span.shift(lead)
			}
			a.stripMore = a.stripMore.shift(lead)
			a.stripHarn = a.stripHarn.shift(lead)
		}
		return []string{row, ""}
	}
	return nil
}

// stripRow is the strip as one string, and it is what a caller with a single
// line to fill wants.
func (a *app) stripRow(width int) string { return strings.Join(a.stripRows(width), "\n") }

// stripRowText is the live row: the harness chips, then a chip per live node,
// then the count of what would not fit.
func (a *app) stripRowText(width int, nodes []*taskNode) string {
	// THE HARNESS CHIPS GO FIRST AND ARE NEVER DROPPED. They lead for the reason
	// the running nodes lead the tasks: they are what a person is waiting on, and
	// unlike a node neither has a room of its own to be found in — a run's door is
	// the panel (harnesspanel.go) and a design has no door at all (harness.go), so
	// these chips are the only place on the surface either one exists.
	lead, leadCols := "", 0
	if name, running := a.runningHarness(); running {
		lead, leadCols = a.harnessChip(name)
		a.stripHarn = hudSpan{from: 0, to: leadCols}
		// THE POINTER LIGHTS THE CHIP AND NOT THE ROW, here and on every chip
		// beside it ([app.stripChip] says why). The band is drawn round exactly the
		// cells the chip occupies, which is what [palette.cursor] does when it is
		// given no width to pad to.
		if a.hoveringStripHarness() {
			lead = a.pal.cursor(lead, 0)
		}
	}
	if len(nodes) == 0 {
		return lead
	}
	// HOW MANY FIT IS DECIDED BEFORE ANYTHING IS PAINTED, because the +N at the
	// end is part of the budget: a row that laid chips down until it ran out
	// would have no room left to say how many it dropped, which is the one thing
	// the person who cannot see them needs.
	kept, used := 0, leadCols
	for i := range nodes {
		_, w := a.stripLabel(nodes[i], width)
		gap := 0
		if i > 0 || leadCols > 0 {
			gap = stripGapCols
		}
		reserve := 0
		if rest := len(nodes) - i - 1; rest > 0 {
			reserve = stripGapCols + ansi.StringWidth(stripMoreWord(rest))
		}
		if used+gap+w+reserve > width {
			break
		}
		used += gap + w
		kept = i + 1
	}
	if kept == 0 {
		// A frame too narrow for one whole chip and its overflow mark still says
		// what is running: the first chip, cut to the row. The mark is dropped
		// rather than the name — "+3" with no name beside it is a count of things
		// a person cannot identify.
		if leadCols > 0 {
			// The harness work has the row to itself: it is what is happening, and a
			// half-drawn node chip beside it would be a name nobody can act on.
			return fit(lead, width)
		}
		text, _ := a.stripLabel(nodes[0], width)
		a.stripSpans = []stripSpan{{span: hudSpan{from: 0, to: width}, id: nodes[0].id, title: nodes[0].title}}
		return fit(text, width)
	}

	var out strings.Builder
	out.WriteString(lead)
	at := leadCols
	for i := 0; i < kept; i++ {
		if i > 0 || leadCols > 0 {
			out.WriteString(stripGap)
			at += stripGapCols
		}
		text, w := a.stripLabel(nodes[i], width)
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
		// THE COUNT BRIGHTENS RATHER THAN BANDING, which is the model segment's own
		// answer to the same shape (render.go's [app.paintIdentity]): it is two
		// characters at the end of a line and not a chip, and a highlighted rectangle
		// round them would be the one boxed thing on a row of tabs. One step up from
		// the dim it rests in.
		if a.hoveringStripMore() {
			out.WriteString(a.pal.accent(word))
		} else {
			out.WriteString(a.pal.dim(word))
		}
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
// [palette.cursor] makes with the rows it lights: every foreground sequence on
// this surface closes with SGR 39, so a background wrapped round one leaves the
// ink underneath alone.
func (a *app) stripLabel(node *taskNode, width int) (string, int) {
	return a.stripChip(node, a.stripGlyph(node), fit(node.title, stripTitleCap))
}

// stripChip is a chip: a glyph, a name already cut, and the air around them. It
// answers with the chip and the CELLS it occupies.
//
// THE ROW WEARS ONE MARK AND NOT TWO ANY MORE. It used to carry the roster's
// cursor as an underline and a ✕ on whichever chip that cursor stood on, and
// both went with the roster: this row is only ever on screen where the roster is
// NOT ([app.stripShowing]), so a cursor drawn from [app.railHold] would be a
// cursor that can never be true. What is left is the one mark that is about the
// page under the row — the band on the room a person is standing in — because
// this is a tab bar, and a tab bar that did not say which tab you are on would
// be a row of identical doors.
func (a *app) stripChip(node *taskNode, glyph, title string) (string, int) {
	cols := ansi.StringWidth(glyph) + 1 + ansi.StringWidth(title) + stripPadCols
	chip := stripPad + glyph + " " + a.stripTitle(node, title) + stripPad
	// WHICH ROW IS THE PAGE YOU ARE ON IS ASKED IN ONE PLACE (room.go's
	// [app.roomStandingOn]), because the roster marks the same fact with the same
	// band and a tab bar that disagreed with the column beside it would be two
	// answers to a question with one.
	if a.roomStandingOn(node) {
		chip = a.pal.selected(chip, cols)
		return chip, cols
	}
	// AND THE POINTER LIGHTS ONE CHIP, NEVER THE ROW. Three doors share this line
	// and each goes somewhere else, so a band across all of it would say "you can
	// press here" about two rooms nobody is aiming at (hover.go's law, and
	// tasksettle.go's answers row before it). The band is drawn round exactly the
	// chip's own cells — padding included, because the padding is inside
	// [stripSpan] and a person may press it — which is what [palette.cursor] does
	// when it is given no width to pad to.
	//
	// THE OPEN CHIP IS LEFT ALONE, and that is the two marks not fighting rather
	// than the hover being forgotten: [palette.selected] is [palette.cursor] one step
	// louder, the two are backgrounds and backgrounds cannot nest, and of the two
	// facts "this is the page you are on" is the one still true when the pointer
	// moves away.
	if a.hoveringStrip(node) {
		chip = a.pal.cursor(chip, 0)
	}
	return chip, cols
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
	if node.Paused() {
		return a.stripPausedGlyph()
	}
	if node.state == session.TaskQueued {
		return a.taskMark(node.ident)
	}
	return a.railGlyph(node)
}

// stripPausedGlyph is work stopped at a gate, in the hue the roster gives every
// other state that is waiting on a person to say something (task.go's
// [app.railGlyph] paints the unverified question the same way).
func (a *app) stripPausedGlyph() string {
	return a.pal.warn(a.icon(tokens.GPaused))
}

// stripTitle paints an already-cut name: the accent on the room a person is
// standing in, the ink on what is running, the muted voice on everything else —
// the rail's own law ([app.railTitle]) with one addition, because this row is
// drawn ABOVE the page it is a tab bar for.
func (a *app) stripTitle(node *taskNode, title string) string {
	switch {
	case a.roomStandingOn(node):
		return a.pal.bold(a.pal.accent(title))
	case node.state == session.TaskRunning:
		return a.pal.ink(title)
	}
	return a.pal.muted(title)
}

// stripPress resolves a click on the strip, and reports whether it took it.
//
// A press ANYWHERE on the row is the row's, whether or not it landed on a chip.
// The strip sits above the conversation and beside nothing, so a click that fell
// through it would act on a transcript row the pointer was not over — and the
// gap between two chips is three cells wide, which is a place people miss.
//
// THE ROW IS LAID OUT BEFORE IT IS READ, for the reason [app.statusPress] states
// about the model segment: laying the row out is what writes the spans, and
// reading them first would be reading where the chips were drawn on the frame
// before this one.
func (a *app) stripPress(x, y int) (tea.Cmd, bool) {
	if a.at(pageSettings) || a.copy.on || a.welcome.open {
		return nil, false
	}
	width, _ := a.size()
	if len(a.stripRows(width)) == 0 || y != a.headHeight() {
		return nil, false
	}
	// THE HARNESS CHIP'S DOOR IS THE PANEL, which is the only place a run can be
	// looked at (harnesspanel.go).
	if a.stripHarn.holds(x) {
		a.openHarness()
		return nil, true
	}
	for _, chip := range a.stripSpans {
		if chip.span.holds(x) {
			a.openRoomFor(chip.id, chip.title)
			return a.takeRoomPump(), true
		}
	}
	// THE COUNT IS THE DOOR TO THE WHOLE ROSTER, which on a frame this narrow is
	// the roster over the body (task.go's [app.railTake]). The chips the row
	// dropped are the work a person came looking for, and this is where all of it
	// is.
	if a.stripMore.holds(x) {
		a.railTake(true)
		return nil, true
	}
	// phone lane: the whole row is one door into the roster PAGE at [tierPhone]
	// (taskphone.go). At this tier the row is not chips at all but a single door,
	// so every press that reaches here — which is every press on it — opens the
	// page and reads the record in behind it.
	if cmd, took := a.stripPhonePress(width); took {
		return cmd, true
	}
	return nil, true
}

// stripHoverAt is [app.stripPress]'s own hit-test with nothing done about it:
// what the pointer is over on this row, and false when it is not over the row at
// all.
//
// THE ROW IS ASKED BEFORE IT IS LAID OUT, which is the one place this parts
// company with the press. A press happens once and can afford to build the row
// to answer; a motion arrives once per CELL the pointer crosses, and laying the
// strip out on every one of them would be the whole session paying for a row
// that is three chips wide (hover.go's ceiling). The y test is a subtraction, and
// past it the layout is the same layout the press does — which is what keeps the
// set that lights and the set that answers one set.
//
// A PRESS BETWEEN TWO CHIPS IS SWALLOWED AND NOTHING LIGHTS THERE, and the two
// are not in disagreement: the row eats the miss so it cannot fall through to the
// conversation, and a gap that brightened would be claiming to be a door.
func (a *app) stripHoverAt(x, y int) (hoverAt, bool) {
	if a.at(pageSettings) || a.copy.on || a.welcome.open || y != a.headHeight() {
		return hoverAt{}, false
	}
	width, _ := a.size()
	if len(a.stripRows(width)) == 0 {
		return hoverAt{}, false
	}
	if a.stripHarn.holds(x) {
		return hoverAt{kind: hoverStripHarness}, true
	}
	for _, chip := range a.stripSpans {
		if chip.span.holds(x) {
			return hoverAt{kind: hoverStrip, id: chip.id}, true
		}
	}
	if a.stripMore.holds(x) {
		return hoverAt{kind: hoverStripMore}, true
	}
	return hoverAt{}, false
}
