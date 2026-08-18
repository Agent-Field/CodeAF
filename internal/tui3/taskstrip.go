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
//
// ── AND THEN WORK GREW CHILDREN ─────────────────────────────────────────────
//
// An adaptive run is not a task. It is a task that keeps SPAWNING tasks, for as
// long as its planner has something left to want (internal/orchestrate), and a
// row of sibling chips is a lie about it: eight chips that read as eight jobs
// somebody asked for, when what happened is one job that grew seven. The shape
// is the news — what came out of what, and which limb is still moving — and a
// flat row is the one rendering that cannot carry a shape.
//
// So a FAMILY is drawn as a family, one node per row, with the connectors
// drawn in the chip row itself:
//
//	 ⠋ Ship the port
//	├── ✓ Read the law
//	├── ⠋ Write the tree
//	│   └── ◌ Cut the goldens
//	└── ◌ Wire the seam   ▸ +3
//
// IT IS A TREE AND NOT AN ACCORDION, and that is the whole design. An accordion
// answers "is this open" — a question about the widget — and it answers it by
// hiding rows behind a heading somebody has to think to press. A tree answers
// "what came out of what", which is the question the run poses, and it answers
// it at rest: every node the strip is spending a row on is on screen, indented
// under the thing that asked for it. Nothing here folds because a person did
// not press it.
//
// FOLDING IS THE ONE COLLAPSE AND IT IS THE FRAME'S, NOT THE PERSON'S. A tree
// that outgrows the rows the strip may spend ([app.stripBudget]) gives up its
// DEEPEST leaves first — the finest-grained work, the part a shape is least
// hurt by losing — and what it gave up is counted on the parent's own row as a
// trailing ▸ +N chip, which is a door to the roster like every other count on
// this surface. And the fold NEVER TOUCHES LIVE WORK: a node that is running,
// that failed, or that is held at the fuel gate keeps its row, and so does
// every ancestor it hangs from, because the one question this row exists to
// answer is where the thing that is moving is. A tree with nothing foldable
// left in it simply stands at the height it needs, and the frame's own +N —
// the same one a too-narrow row has always drawn — is what catches the rest.
//
// THE PHONE KEEPS THE CONNECTORS AND SPENDS ON THE NAMES. Four cells of indent
// per level is the cheapest thing on the row, and it is the only thing carrying
// the structure; a name is expensive and it is recoverable one keystroke away
// in the room. So [tierPhone] cuts to [stripPhoneCap] and keeps every stem.
//
// A NODE IN A TREE WEARS ITS STATE AND NOT ITS IDENTITY, which is the one place
// the tree parts company with the flat row above it ([app.stripGlyph] says why
// the flat row does the opposite). On a row of three sibling chips the question
// is WHICH work, and the identity cell is the only mark that answers it the
// same way every frame. Down a tree the neighbours are already named by the
// connectors they hang from, and the question left over is which limb is still
// moving — so the column of glyphs is a column of STATES, and it can be read
// down.

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
	// stripPhoneCap is a name's budget inside a tree at [tierPhone]. The indent
	// is what a phone cannot afford to lose and a name is what it can: the stems
	// carry the shape, and the whole name is one keystroke away in the room.
	stripPhoneCap = 12
)

// The tree's connectors, and the four cells each level of it costs.
//
// THEY ARE NOT [railMid] AND [railLast], and the difference is what the arrow
// means. The transcript's fold hangs DETAIL off a row — "here is what is inside
// this call" — and an arrow pointing into it is the right mark for that. A
// roster tree hangs WORK off work, sibling beside sibling, and the plain elbow
// is what every tree a person has ever read is drawn with. Two structures, two
// vocabularies, and neither one borrowed for the other.
//
// The stand-ins are the linear tier's law (styles.go): a shape that means
// something gets a spelling that means the same thing out loud.
// THE ELBOW IS THREE CELLS AND THE CHIP PAYS THE FOURTH. A chip already opens
// with one cell of its own air ([stripPad]), and an elbow that carried its own
// trailing space would put two cells between the stem and the glyph — a tree
// with a gutter down it. So the elbow stops at the corner, the chip's left pad
// is the gap, and every level lands on the same four-cell grid: a glyph at
// column 4×depth, which is what makes a column of states readable downward.
const (
	stripBranch      = "├──"
	stripLast        = "└──"
	stripStem        = "│   "
	stripVoid        = "    "
	stripBranchASCII = "|--"
	stripLastASCII   = "`--"
	stripStemASCII   = "|   "
	stripIndentCols  = 4
	stripElbowCols   = 3
	// stripFoldMark opens the count of what a fold took away. It is the same
	// closed disclosure this surface points rightward with everywhere else, and
	// what follows it is [stripMoreWord] — one overflow vocabulary, two places.
	stripFoldMark      = "▸ "
	stripFoldMarkASCII = "> "
)

// How many rows the strip may spend on a session, before the frame's own
// height has its say ([app.stripBudget]).
const (
	// stripRowCap is the ceiling anywhere the frame has columns to think with.
	// Six rows is a root and five limbs — enough that a real run's shape is on
	// screen — and it is a CEILING rather than a target: a session with one node
	// still draws one row.
	stripRowCap = 6
	// stripPhoneRowCap is that ceiling at [tierPhone], where every row is a
	// bigger share of the screen and the conversation is the thing being read.
	stripPhoneRowCap = 4
	// stripRowShare is the most of a terminal's height the strip may take,
	// whatever the caps above say. A pinned row that ate half a short window
	// would be answering "what is running" by hiding what it is running for.
	stripRowShare = 3
)

// ── THE PARENT SEAM ─────────────────────────────────────────────────────────
//
// These two are the whole of what the tree reads. The parent is filled from the
// engine's own updates (session's TaskNotice.Parent), and TWO KINDS OF WORK FILL
// IT: an adaptive run, which takes one row with a row under it for every node it
// cuts (session's orchestrate.go), and a TASK THAT SPLIT ITS OWN BRIEF, whose
// pieces are registered under it (session's task.go). Neither is anything to
// this package: a family is a family. A session that has run neither answers ""
// and false to both, which is the flat row, unchanged — and that is what keeps
// this a seam rather than a rewrite.
//
// Paused has no publisher yet: nothing on TaskNotice says a run is standing at
// its fuel gate, so the ⏸ is drawn from a fact this surface cannot currently be
// told. The run's own page is where a paused run says so today.
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
// (session's EventOrchestratePause). It is not a state the engine moves a node
// through, which is why it is a fact of its own — a paused node is still
// running as far as the run is concerned, and it is not moving as far as a
// person is concerned, and the second reading is the one a roster owes them.
func (n *taskNode) Paused() bool { return n.paused }

// stripKey is a node's own key in the alphabet [taskNode.ParentID] speaks.
func stripKey(node *taskNode) string { return itoa(int(node.id)) }

// glyphPaused marks work held at a gate. It is the transport bar every device a
// person owns pauses with, and it is deliberately NOT the queued circle: a
// queued node has not started and this one has, and the difference is the whole
// of what the gate is asking about.
const (
	glyphPaused      = "⏸"
	glyphPausedASCII = "="
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
	span hudSpan
	// row is which of the strip's rows the chip landed on, counted from the
	// strip's own top. It is zero for every chip on a flat session's single row,
	// which is why it costs that session nothing to carry.
	row   int
	id    uint64
	title string
	// stop is the ✕'s own columns inside this chip, or the empty span on a chip
	// that is not carrying one (stop.go). It is read BEFORE the chip's own span,
	// because the ✕ sits inside the door and pressing it must not also walk
	// through it.
	stop hudSpan
}

// stripFold is a ▸ +N chip: what a fold took off the tree, and where the count
// is standing. Its door is the roster, which is where the whole tree lives —
// the same destination [app.stripMore] opens, for the same reason.
type stripFold struct {
	span hudSpan
	row  int
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
	if width < stripFloor || height < roomyFloor {
		return false
	}
	// A RUNNING SUB-HARNESS RAISES THE ROW TOO (harnesspanel.go). It is alive
	// for minutes at a time and it is the only thing on screen that would
	// otherwise say so — the run happens inside one tool call, so the transcript
	// shows a single row that has not come back yet.
	if _, running := a.runningHarness(); running {
		return true
	}
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && node.state == session.TaskRunning {
			return true
		}
	}
	return false
}

// stripHeight is what the strip costs the body region, and it is subtracted in
// [app.topHeight] — the number every geometric question about the body resolves
// through.
//
// IT IS THE LAYOUT'S OWN COUNT AND NOT A SECOND OPINION. A tree's height is the
// tree's business — how many nodes, how many folds it took to fit — and a
// cheaper answer computed a second way here is the one bug this arithmetic
// cannot survive: a frame that drew five rows and budgeted for one puts the
// conversation's last line under the input box. So it lays the strip out and
// counts what came back, at the terminal's own width, the way [app.topHeight]
// requires of everything it adds up.
func (a *app) stripHeight() int {
	width, _ := a.size()
	return len(a.stripRows(width))
}

// stripBudget is how many rows this frame will let the strip spend. See the
// header: it is what a tree folds itself down to, not a promise about a
// session with one node in it.
func (a *app) stripBudget() int {
	width, height := a.size()
	rows := stripRowCap
	if layoutTier(width) == tierPhone {
		rows = stripPhoneRowCap
	}
	if share := height / stripRowShare; share < rows {
		rows = share
	}
	if rows < 1 {
		rows = 1
	}
	return rows
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

// stripRows is the strip, laid out, and it is where every chip's columns and
// row are recorded.
//
// THE LIVE ROW LEADS AND THE FAMILIES HANG UNDER IT. A session that has never
// spawned a tree gets exactly the row it has always got — one line, the chips
// packed across it, the +N at its end — because that row is what the tab-bar
// reading of this surface is built on and a tree above it would push the thing
// a person is watching down the screen. Under it, one row per node, the shape
// of each run that is alive.
//
// A NODE IS ON ONE OF THE TWO AND NEVER BOTH. Anything that belongs to a family
// is drawn in its family and taken out of the flat row, because a chip up top
// and the same chip down the tree is one node claiming to be two.
//
// It returns nothing when there is no strip, which is what the frame draws for
// rows it is not spending (view.go).
func (a *app) stripRows(width int) []string {
	a.stripSpans, a.stripFolds = nil, nil
	a.stripMore, a.stripMoreRow, a.stripHarn = hudSpan{}, 0, hudSpan{}
	if !a.stripShowing() || width <= 0 {
		return nil
	}
	trees, flat := a.stripTrees()
	var rows []string
	if lead := a.stripFlatRow(width, flat); lead != "" {
		rows = append(rows, lead)
	}
	if len(trees) == 0 {
		return rows
	}
	// THE FOLD RUNS BEFORE A SINGLE ROW IS PAINTED, for the reason the flat row
	// budgets its own +N before laying a chip down: what a tree gives up changes
	// where everything under it is drawn, and a paint that discovered its ceiling
	// halfway would have to unpaint.
	budget := a.stripBudget()
	for {
		rest := budget - len(rows)
		if stripRowsOf(trees) <= rest || !stripFoldOnce(trees) {
			break
		}
	}
	for _, tree := range trees {
		rows = a.stripPaint(rows, tree, width, nil)
	}
	return a.stripClamp(rows, budget, width)
}

// stripRow is the strip as one string, and it is what a caller with a single
// line to fill wants. The frame does not — it appends [app.stripRows] row by
// row, because a row is a row and a string with newlines in it is a frame whose
// height nobody counted.
func (a *app) stripRow(width int) string { return strings.Join(a.stripRows(width), "\n") }

// stripClamp is the frame's last word, and it is NOT the fold.
//
// The fold is a judgement about the tree — give up the finest work first, never
// give up what is moving — and it can run out of things it is allowed to take.
// This is the arithmetic underneath it: rows the terminal does not have. So the
// last row the strip may spend is spent saying how many it could not draw, in
// the same mark and with the same door as the row that ran out of columns. It
// is the crude cut, it is meant to be, and a tree that reaches it is a session
// with more live work on it than a pinned strip was ever the surface for — the
// roster is one press away and it has every row.
func (a *app) stripClamp(rows []string, budget, width int) []string {
	if len(rows) <= budget {
		return rows
	}
	keep := budget - 1
	word := stripMoreWord(len(rows) - keep)
	rows = rows[:keep]
	a.stripSpans = stripSpansWithin(a.stripSpans, keep)
	a.stripFolds = stripFoldsWithin(a.stripFolds, keep)
	a.stripMore = hudSpan{from: 0, to: ansi.StringWidth(word)}
	a.stripMoreRow = keep
	return append(rows, fit(a.pal.dim(word), width))
}

// stripSpansWithin and stripFoldsWithin drop what was recorded for rows the
// clamp above took away. A span that outlives its row is a click delivered to a
// chip nobody can see.
func stripSpansWithin(spans []stripSpan, rows int) []stripSpan {
	out := spans[:0]
	for _, s := range spans {
		if s.row < rows {
			out = append(out, s)
		}
	}
	return out
}

func stripFoldsWithin(folds []stripFold, rows int) []stripFold {
	out := folds[:0]
	for _, f := range folds {
		if f.row < rows {
			out = append(out, f)
		}
	}
	return out
}

// stripFlatRow is the live row: the harness chips, then a chip per node that
// belongs to no family, then the count of what would not fit.
//
// It is the row this surface has always drawn and the arithmetic is unchanged —
// what moved is only where the nodes come from, because a node in a tree is
// drawn in its tree.
func (a *app) stripFlatRow(width int, nodes []*taskNode) string {
	// THE HARNESS CHIPS GO FIRST AND ARE NEVER DROPPED. They lead for the reason
	// the running nodes lead the tasks: they are what a person is waiting on, and
	// unlike a node neither has a room of its own to be found in — a run's door is
	// the panel (harnesspanel.go) and a design has no door at all (harness.go), so
	// these chips are the only place on the surface either one exists.
	lead, leadCols := "", 0
	if name, running := a.runningHarness(); running {
		lead, leadCols = a.harnessChip(name)
		a.stripHarn = hudSpan{from: 0, to: leadCols}
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
		_, w, _ := a.stripLabel(nodes[i], width)
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
		text, _, _ := a.stripLabel(nodes[0], width)
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
		text, w, markCols := a.stripLabel(nodes[i], width)
		out.WriteString(text)
		a.stripSpans = append(a.stripSpans, stripSpan{
			span:  hudSpan{from: at, to: at + w},
			id:    nodes[i].id,
			title: nodes[i].title,
			stop:  stripStopSpan(at, w, markCols),
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

// ── THE TREE ────────────────────────────────────────────────────────────────

// stripTwig is one node of a family as the strip holds it: the node, the
// children still being drawn, and how many rows were folded away under it.
//
// It is a shape of its own rather than a flat list of (node, depth) pairs
// because the fold is a question about SUBTREES — may this limb go, is anything
// under it moving — and a depth-tagged list answers that by scanning forward
// for the next row at the same depth, which is a tree with its structure taken
// out and then guessed back.
type stripTwig struct {
	node   *taskNode
	kids   []*stripTwig
	folded int
}

// rows is what this twig is asking the strip for right now.
func (t *stripTwig) rows() int {
	n := 1
	for _, kid := range t.kids {
		n += kid.rows()
	}
	return n
}

// count is every node this twig stands for, drawn or folded away — what a
// parent has to add to its own ▸ +N when it gives this limb up.
func (t *stripTwig) count() int {
	n := 1 + t.folded
	for _, kid := range t.kids {
		n += kid.count()
	}
	return n
}

// live reports whether this twig or anything under it is work the fold may not
// hide: running, failed, or held at a gate. See the header — an ancestor of a
// live node is live, because a row hanging from nothing is not a tree.
func (t *stripTwig) live() bool {
	switch {
	case t.node.state == session.TaskRunning, t.node.state == session.TaskFailed, t.node.Paused():
		return true
	}
	for _, kid := range t.kids {
		if kid.live() {
			return true
		}
	}
	return false
}

func stripRowsOf(trees []*stripTwig) int {
	n := 0
	for _, tree := range trees {
		n += tree.rows()
	}
	return n
}

// stripFoldOnce gives up ONE limb — the deepest one the law allows, and the
// last of those where several sit at the same depth — and reports whether it
// found one.
//
// ONE AT A TIME, rather than every foldable child of the deepest parent at
// once, because the strip needs a row and not a clearance: folding a parent's
// whole brood to win back the single row it was over is a shape thrown away for
// nothing. The caller loops until it fits, so the tree gives up exactly what the
// frame charged it.
//
// DEEPEST FIRST is the judgement. What is deepest is the finest-grained work the
// planner asked for, it is the part of a shape a person can most afford to read
// as a count, and taking it never orphans anything — the row above it is still
// there to carry the number.
func stripFoldOnce(trees []*stripTwig) bool {
	var parent *stripTwig
	at, deepest := -1, -1
	var walk func(t *stripTwig, depth int)
	walk = func(t *stripTwig, depth int) {
		for i, kid := range t.kids {
			if !kid.live() && depth >= deepest {
				parent, at, deepest = t, i, depth
			}
		}
		for _, kid := range t.kids {
			walk(kid, depth+1)
		}
	}
	for _, tree := range trees {
		walk(tree, 0)
	}
	if parent == nil {
		return false
	}
	gone := parent.kids[at]
	parent.folded += gone.count()
	parent.kids = append(parent.kids[:at], parent.kids[at+1:]...)
	return true
}

// stripKin buckets every node this session has admitted by its parent's key,
// and indexes them all by their own.
//
// It walks [app.taskOrder], so a parent's children come out in the order the
// session met them — the one order a roster is allowed to use, because any
// other one moves a row a person is watching for a reason they cannot see
// (task.go's [app.railMembers] keeps the same law).
func (a *app) stripKin() (kids map[string][]*taskNode, byKey map[string]*taskNode) {
	byKey = make(map[string]*taskNode, len(a.taskOrder))
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil {
			byKey[stripKey(node)] = node
		}
	}
	for _, id := range a.taskOrder {
		node := a.tasks[id]
		if node == nil {
			continue
		}
		up := node.ParentID()
		if up == "" || up == stripKey(node) || byKey[up] == nil {
			continue
		}
		if kids == nil {
			kids = map[string][]*taskNode{}
		}
		kids[up] = append(kids[up], node)
	}
	return kids, byKey
}

// stripTrees splits the live set into the families it belongs to and the nodes
// that belong to none.
//
// A FAMILY IS DRAWN WHOLE, and that is the one place the tree takes rows the
// flat row would not have. The live set is running, waiting-on-a-person and
// idle work (stripOrder) — what is parked and what has landed are the roster's
// — but a tree of only the live members of a run is a tree with holes in it,
// and a hole in a tree is a claim that nothing was there. So once a live node
// turns out to have a family, the family comes with it: the ✓ rows are what
// make the ◌ rows legible, and the fold is what pays for them.
//
// The families come in the order their live members do, which is the strip's
// own order and not the roster's: what is running leads, because that is what
// this row exists to point at.
func (a *app) stripTrees() (trees []*stripTwig, flat []*taskNode) {
	live := a.stripNodes()
	kids, byKey := a.stripKin()
	if kids == nil {
		return nil, live
	}
	seen := map[string]bool{}
	for _, node := range live {
		root := stripRootOf(node, byKey)
		key := stripKey(root)
		if len(kids[key]) == 0 {
			flat = append(flat, node)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		trees = append(trees, stripGrow(root, kids, map[string]bool{}))
	}
	return trees, flat
}

// stripRootOf walks up to the head of a node's family.
//
// The visited set is not defensive tidiness: the parent is written by an
// adapter this package does not own ([taskNode.ParentID]), and a cycle in it
// would be a frame that never returns rather than a frame that looks wrong.
func stripRootOf(node *taskNode, byKey map[string]*taskNode) *taskNode {
	seen := map[string]bool{}
	for {
		key := stripKey(node)
		if seen[key] {
			return node
		}
		seen[key] = true
		up := byKey[node.ParentID()]
		if up == nil {
			return node
		}
		node = up
	}
}

// stripGrow builds one family, depth first, in the order the session met it.
func stripGrow(node *taskNode, kids map[string][]*taskNode, seen map[string]bool) *stripTwig {
	key := stripKey(node)
	twig := &stripTwig{node: node}
	if seen[key] {
		return twig
	}
	seen[key] = true
	for _, kid := range kids[key] {
		twig.kids = append(twig.kids, stripGrow(kid, kids, seen))
	}
	return twig
}

// stripPaint draws one twig and everything under it, and records where each
// chip landed.
//
// stems is the ancestry as the connectors need it: one entry per level, true
// where that level's node still has siblings to come. Every entry but the last
// picks between a stem and blank air; the last one picks between the ├ and the
// └, which is the same question asked about this node rather than its parents.
func (a *app) stripPaint(rows []string, twig *stripTwig, width int, stems []bool) []string {
	prefix, at := a.stripPrefix(stems)
	text, cols, markCols := a.stripTreeLabel(twig.node, width)
	line := prefix + text
	a.stripRecord(stripSpan{
		span:  hudSpan{from: at, to: at + cols},
		row:   len(rows),
		id:    twig.node.id,
		title: twig.node.title,
		stop:  stripStopSpan(at, cols, markCols),
	}, width)
	at += cols
	if twig.folded > 0 {
		word := a.linearMark(stripFoldMark, stripFoldMarkASCII) + stripMoreWord(twig.folded)
		line += stripGap + a.pal.dim(word)
		fold := stripFold{
			span: hudSpan{from: at + stripGapCols, to: at + stripGapCols + ansi.StringWidth(word)},
			row:  len(rows),
		}
		if fold.span.from < width {
			if fold.span.to > width {
				fold.span.to = width
			}
			a.stripFolds = append(a.stripFolds, fold)
		}
	}
	rows = append(rows, fit(line, width))
	for i, kid := range twig.kids {
		// The stack is COPIED down rather than appended to in place: one backing
		// array shared between two siblings is the second sibling drawing the
		// first one's stems.
		next := make([]bool, len(stems), len(stems)+1)
		copy(next, stems)
		rows = a.stripPaint(rows, kid, width, append(next, i < len(twig.kids)-1))
	}
	return rows
}

// stripRecord keeps a chip's columns, cut to the row it was drawn on. A span
// that runs past the frame is a click delivered to a name the cut took away.
func (a *app) stripRecord(chip stripSpan, width int) {
	if chip.span.from >= width {
		return
	}
	if chip.span.to > width {
		chip.span.to = width
	}
	if chip.stop.to > width {
		// The cut took the ✕ away. A button a person cannot see must not still
		// answer to the column it would have been drawn in.
		chip.stop = hudSpan{}
	}
	a.stripSpans = append(a.stripSpans, chip)
}

// stripPrefix is the connectors for one row, painted, and the CELLS they cost.
// A root has none — it is the thing everything else hangs from.
func (a *app) stripPrefix(stems []bool) (string, int) {
	if len(stems) == 0 {
		return "", 0
	}
	var out strings.Builder
	for _, more := range stems[:len(stems)-1] {
		if more {
			out.WriteString(a.linearMark(stripStem, stripStemASCII))
			continue
		}
		out.WriteString(stripVoid)
	}
	if stems[len(stems)-1] {
		out.WriteString(a.linearMark(stripBranch, stripBranchASCII))
	} else {
		out.WriteString(a.linearMark(stripLast, stripLastASCII))
	}
	return a.pal.dim(out.String()), (len(stems)-1)*stripIndentCols + stripElbowCols
}

// stripLabel is one chip, painted, and the CELLS it occupies. The two are
// returned together because the budget is spent in cells and the label is
// carried in bytes: the width is taken through [ansi.StringWidth], which is what
// every other measurement on this surface goes through (task.go's [app.railJoin]).
//
// The band goes on LAST, over an already-painted chip, which is the same bargain
// [palette.hover] makes with the rows it lights: every foreground sequence on
// this surface closes with SGR 39, so a background wrapped round one leaves the
// ink underneath alone.
func (a *app) stripLabel(node *taskNode, width int) (string, int, int) {
	return a.stripChip(node, a.stripGlyph(node), fit(node.title, stripTitleCap), width)
}

// stripTreeLabel is the same chip on a tree row: the node's STATE in the glyph
// cell (the header says why the two rows differ on this), and a name cut to
// what the tier can afford.
func (a *app) stripTreeLabel(node *taskNode, width int) (string, int, int) {
	limit := stripTitleCap
	if layoutTier(width) == tierPhone {
		limit = stripPhoneCap
	}
	return a.stripChip(node, a.stripTreeGlyph(node), fit(node.title, limit), width)
}

// stripChip is a chip: a glyph, a name already cut, the air around them, and
// whichever of the marks this node has earned. It answers with the chip, the
// CELLS it occupies, and the cells the ✕ at its end took — zero on a chip that
// is not carrying one.
//
// THE ✕ IS ON THE FOCUSED CHIP AT THE WIDE TIER AND NOWHERE ELSE (stop.go).
// This row is the roster's live set drawn as chips, so "the focused chip" is
// the roster's own cursor (see [app.stripFocused]) — the button follows the
// keyboard rather than appearing on every chip, because a row of doors that all
// carry a stop button is a row where the wrong one is one cell away. Below the
// wide tier the cells are not there to spend and the room's own header carries
// the same button at every width.
func (a *app) stripChip(node *taskNode, glyph, title string, width int) (string, int, int) {
	mark, markCols := "", 0
	if a.stripStopMark(node, width) {
		mark = a.linearMark(roomStopMark, roomStopMarkASCII)
		markCols = ansi.StringWidth(mark) + 1 // the space that separates it from the name
	}
	cols := ansi.StringWidth(glyph) + 1 + ansi.StringWidth(title) + markCols + stripPadCols
	chip := stripPad + glyph + " " + a.stripTitle(node, title)
	if mark != "" {
		chip += " " + mark
	}
	chip += stripPad
	if a.room != nil && a.room.id == node.id {
		chip = a.pal.band(chip, cols)
	}
	if a.stripFocused(node) {
		chip = a.pal.underline(chip)
	}
	return chip, cols, markCols
}

// stripStopMark reports whether this chip carries the ✕.
func (a *app) stripStopMark(node *taskNode, width int) bool {
	return a.stripFocused(node) && layoutTier(width) == tierWide &&
		!a.stopTaskTarget(node).empty()
}

// stripStopSpan is where a chip's ✕ landed, given where the chip did. The mark
// sits one cell in from the chip's trailing pad, which is where [app.stripChip]
// put it.
func stripStopSpan(at, cols, markCols int) hudSpan {
	if markCols <= 0 {
		return hudSpan{}
	}
	end := at + cols - stripPadCols/2
	return hudSpan{from: end - markCols, to: end}
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
	if node.Paused() {
		return a.stripPausedGlyph()
	}
	if node.state == session.TaskQueued {
		return a.taskMark(node.ident)
	}
	return a.railGlyph(node)
}

// stripTreeGlyph is the same cell down a tree, where it is the node's STATE and
// never its identity: a queued node wears the roster's own hollow circle, so
// the column of glyphs can be read down as the shape of the run.
func (a *app) stripTreeGlyph(node *taskNode) string {
	if node.Paused() {
		return a.stripPausedGlyph()
	}
	return a.railGlyph(node)
}

// stripPausedGlyph is work stopped at a gate, in the hue the roster gives every
// other state that is waiting on a person to say something (task.go's
// [app.railGlyph] paints the unverified question the same way).
func (a *app) stripPausedGlyph() string {
	return a.pal.warn(a.linearMark(glyphPaused, glyphPausedASCII))
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
	width, _ := a.size()
	// The rows are laid out first and the ROW is resolved before the columns: a
	// tree is as many rows as it needs, so "which chip" is two coordinates now
	// and the first of them is the one that used to be a constant.
	rows := a.stripRows(width)
	row := y - a.headHeight()
	if len(rows) == 0 || row < 0 || row >= len(rows) {
		return nil, false
	}
	// THE HARNESS CHIP'S DOOR IS THE PANEL, which is the only place a run can be
	// looked at (harnesspanel.go). It rides the live row and nothing else.
	if row == 0 && a.stripHarn.holds(x) {
		a.openHarness()
		return nil, true
	}
	for _, chip := range a.stripSpans {
		if chip.row != row || !chip.span.holds(x) {
			continue
		}
		// THE ✕ IS RESOLVED INSIDE THE DOOR IT SITS IN (stop.go). It is drawn
		// within the chip's own span, so a press on it that fell through to the
		// chip would walk into the room of the very work it was aimed at ending.
		if chip.stop.holds(x) {
			if node := a.tasks[chip.id]; node != nil {
				a.raiseStop(a.stopTaskTarget(node))
			}
			return nil, true
		}
		a.openRoomFor(chip.id, chip.title)
		return a.takeRoomPump(), true
	}
	// EVERY COUNT ON THIS ROW IS THE DOOR TO THE WHOLE ROSTER, which is the
	// roster wherever this frame keeps it: the column on a wide one, the
	// fullscreen overlay on a narrow one (task.go's [app.railTake]). The chips a
	// narrow row dropped and the limbs a short frame folded are the same missing
	// work asked about twice — one gesture, one destination, two shapes.
	for _, fold := range a.stripFolds {
		if fold.row == row && fold.span.holds(x) {
			a.railTake(true)
			return nil, true
		}
	}
	if row == a.stripMoreRow && a.stripMore.holds(x) {
		a.railTake(true)
		return nil, true
	}
	return nil, true
}
