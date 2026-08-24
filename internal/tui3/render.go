package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The render core is two passes and one cache.
//
// PASS 1 — an entry renders ITSELF into rows and remembers them. It re-renders
// when its own text changes or the width does, and at no other time; a frame
// that touches a settled paragraph is a frame that wrapped text nobody
// re-typed.
//
// PASS 2 — [app.layout] joins those rows into the screen list, and it is the
// ONLY place a blank row is ever emitted. That is the whole fix for the gaps
// this surface used to grow: every entry politely left a line above itself, two
// polite entries left two, and nobody owned the result. Entries no longer get a
// vote.
//
// The frame joins [app.window]'s slice of that list and nothing else — no
// transcript rebuild, no re-wrap per keystroke.

// hitKind is what a visible row answers to a click.
type hitKind uint8

const (
	hitNone     hitKind = iota
	hitTool             // a tool call: click expands that call inline
	hitFold             // the "N earlier tool calls" line: click expands the turn
	hitWorkFold         // one completed turn's folded machinery
	hitMore             // the "… N more lines" foot of a capped expansion: click lifts the cap
	hitTask             // a task proposal (task.go): click opens its brief
	// hitDone is a landed task's card (taskdone.go): click opens its full
	// context, enter opens the node's room, ctrl+o is the key the card itself
	// names. It is a hit of its own rather than another hitTask because the two
	// blocks answer the same gestures with different things — one is a question
	// that can still be answered, the other is a record that cannot.
	hitDone
	// hitSettle is a landed card's ANSWERS row: the choices a task that finished
	// with nobody able to check it offers (tasksettle.go). It is a hit of its own
	// rather than another hitDone because the two rows answer the same gesture
	// with different things — one expands a record, the other decides about work
	// — and it needs the COLUMN as well as the row, the way hitChoice does.
	hitSettle
	hitHarness
	// hitChoice is the proposal's choices row, and it is the one hit on this
	// surface that needs the COLUMN as well as the row: three answers share one
	// line, so which of them was pressed is a question about x (app.go's
	// [app.choicePress], the same shape the rail's click has).
	hitChoice
	// hitModel is the proposal's MODELS row, on the rare card that has one:
	// one word fitted several models, so the card offers them (task.go). It is a
	// hit of its own rather than another hitChoice because the two rows answer
	// different questions with the same gesture — one settles which model, the
	// other settles whether the work goes at all — and [app.choicePress] must not
	// resolve a press on one against the other's columns.
	hitModel
	// hitRewind is the rewind mode's cut line (rewind.go): a click on it commits
	// the cut it is drawn at. It is the one hit on this surface that belongs to a
	// row nothing in the conversation produced — the line is drawn between two
	// blocks, and it exists only while the mode is up.
	hitRewind
	// hitStandChoice is the standing card's row of answers (standing.go). It is
	// a hit of its own rather than another [hitChoice] for that constant's own
	// reason: two blocks answer the same gesture with different questions, and
	// the press must not be resolved against the other card's columns.
	hitStandChoice
)

// row is one visible screen row and what it points at. It is the single
// mapping from screen geometry to the conversation: the frame joins row.text,
// the mouse hit-tests row.entry, and ↑/↓ walk the same list. Two sources of
// truth for "which entry is this row" is how a click lands on the wrong call.
type row struct {
	text  string
	entry int // index into app.entries; -1 for a blank or the fold line
	hit   hitKind
	turn  int // the turn a fold line folds
	// links are the task references drawn in this row's own columns
	// (markdown.go). They are the one thing on the transcript a click resolves
	// by COLUMN rather than by row, and they are recorded here for the reason
	// the strip's chips and the status row's model segment record theirs: the
	// geometry is written where it is decided, because a hit-test that
	// recomputed it would be measuring a row the frame has not drawn.
	links []taskLink
	// foot is the affordance under a markdown table that was cut, on the one row
	// that carries it (mdtable.go). It is the second target this surface resolves
	// by column, and it is recorded here for the reason the links above it are.
	foot tableFoot
}

// toolWindow is how many of a turn's tool calls stay on screen. Three is the
// number a person can hold without reading: the call that is running and the
// two it followed.
const toolWindow = 3

// deck is ONE LIST OF BLOCKS BEING LAID OUT, and the fold state that belongs to
// it. There are two of them on this surface and there is not going to be a
// third: the conversation ([app.conversation]) and a task's page, which is the
// same list built from a node's journal and its live lane (room.go).
//
// It exists because a room MUST render exactly like the conversation, and the
// only way to guarantee that is for both to go through one renderer. A room
// that drew its own lines was a second rendering of the same four kinds of
// block, and it diverged the way a second rendering always does: no pictures on
// a person's message, no reasoning, no expansion on a call, no markdown on an
// answer. Everything below takes the deck rather than reaching for [app.entries]
// so that a gap between the two cannot be reintroduced by a renderer that
// forgot which list it was drawing.
//
// The entries are a SLICE and the fold map is a POINTER, which is what makes a
// deck a view rather than a copy: [app.entryRows] writes each entry's row cache
// through it, and [app.unfold] writes the map.
// clock says whether this list carries THE SESSION'S CLOCK — the turn receipts
// and the seam marks (timestamps.go). Only the conversation does. A room's
// turns are its own numbering, and [app.stamps] is keyed by the session's, so a
// page that ran the clock would draw the conversation's receipts against a
// node's turns and report figures nobody measured.
//
// showsWork says this page IS the work rather than a conversation about it, so
// nothing on it collapses into a "worked" chip (workfold.go's [app.deckFolds]).
// Only a room sets it.
type deck struct {
	entries     []entry
	unfolded    map[int]bool
	workOpen    map[int]bool
	clock       bool
	showsWork   bool
	runningTurn int
}

// conversation is the deck the transcript draws.
func (a *app) conversation() deck {
	running := 0
	if a.state == stateWorking {
		running = a.turn
	}
	return deck{entries: a.entries, unfolded: a.unfolded, workOpen: a.workOpen, clock: true, runningTurn: running}
}

// bodyDeck is the deck the BODY REGION is drawing right now — the room's page
// while one is open, the conversation otherwise (view.go's [app.bodyRows] makes
// the same choice about the frame).
//
// Every gesture that names a row by index resolves through here: a click, the
// hover, the fold key, the thinking key. An index is only meaningful against the
// list it was taken from, and the list a person is pointing at is the one on
// screen.
func (a *app) bodyDeck() deck {
	// A NODE'S TRANSCRIPT INSIDE A RUN'S PAGE IS ITS OWN DECK: the rows on
	// screen are that journal's, so the doors that act on "the entry under the
	// click" — open a call, toggle a thought — must act on those entries and
	// not on the room's or the conversation's (roomorch.go's
	// [app.orchTranscriptDeck]).
	if run := a.orchOf(); run != nil && run.transcript != "" {
		return a.orchTranscriptDeck()
	}
	if a.room != nil {
		return a.room.deck()
	}
	return a.conversation()
}

// visible returns the row list, rebuilding it only when something changed.
//
// The dirty flag is the whole of the repaint discipline. A streamed delta
// mutates the entry's text but does NOT set it — the frame clock does, once per
// [frameInterval] — so a hundred deltas in a second cost one hundred string
// appends and thirty layouts, not a hundred layouts.
func (a *app) visible(width int) []row {
	if a.rows == nil || a.rowsWidth != width || a.dirty {
		a.rows = a.layout(width)
		a.rowsWidth = width
		a.dirty = false
		a.builds++
	}
	return a.rows
}

// layout is THE SPACING LAW (docs/CHAT-V3.md D11), and it is a law because it
// is enforced in exactly one place. Every blank row on this surface is emitted
// by the four rules below and by nothing else — no entry appends one, no
// renderer leaves one behind (see [trimBlanks]):
//
//   - ONE blank before a tool cluster that directly follows text. A cluster
//     that answers the person's own message gets none: the calls ARE the reply
//     starting, and a gap there would read as a pause that did not happen.
//   - ZERO between the lines of a cluster — a cluster is one thing.
//   - ONE blank after a cluster, before the text that follows it.
//   - ONE blank before each user message: the turn boundary, the only
//     structural silence this surface has.
//
// Two rules can ask for the same gap — a cluster ending a turn, then the next
// user message — and a gap asked for twice is still one gap, which is why each
// block asks once, before it draws. Nothing is ever emitted at the top of the
// transcript.
func (a *app) layout(width int) []row {
	out, closed := a.deckRows(a.conversation(), width)
	// THE ONE THING EVER EMITTED AT THE TOP OF THE TRANSCRIPT, and it is emitted
	// here rather than by any block because it is not one: it says that the
	// conversation on screen starts part-way through and that scrolling reaches
	// the rest (replay.go's [app.earlierRow]). It goes while the reader is still
	// above it — the moment the real beginning is drawn, there is nothing left
	// to promise and the marker is not laid out at all.
	if line := a.earlierRow(width); line != "" && len(out) > 0 {
		out = append([]row{{text: line, entry: -1}, {entry: -1}}, out...)
	}
	line, ok := a.harnessStepRow(width)
	if !ok {
		line, ok = a.ellipsis()
	}
	if ok {
		if closed && len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, row{text: line, entry: -1})
	}
	// THE CUT, SECOND TO LAST. A rewind being chosen is a property of the screen
	// too — the line between two blocks, and the wash over everything under it —
	// so it is applied to finished rows here for [app.hoverPass]'s reason, one
	// pass above it (rewind.go).
	out = a.rewindPass(out, width)
	// THE POINTER, LAST. Hover is a property of the screen and not of the
	// conversation, so it is applied to finished rows in one pass here rather
	// than threaded through six renderers (hover.go).
	a.hoverPass(out, width)
	return out
}

// deckRows is the pass itself, over whichever list is being drawn. It reports
// whether the LAST block it laid out closed something — a cluster, a proposal,
// a landed note — which is what a caller needs to decide the gap before a foot
// of its own: the conversation's ellipsis above, and the room's finished line
// (room.go). A foot wedged against the block above it is the same defect the
// four rules exist to prevent.
func (a *app) deckRows(d deck, width int) ([]row, bool) {
	es := d.entries
	folds := a.deckFolds(d)
	out := make([]row, 0, len(es)+8)
	// wasCluster says the block that just drew was a tool cluster, and wasBlock
	// that it was a CLOSED block — a proposal, or the note a node writes when it
	// lands (task.go). Together they are the whole of the state this pass
	// carries.
	wasCluster, wasBlock := false, false
	gap := func() {
		if len(out) > 0 {
			for range spacingBlockRows {
				out = append(out, row{entry: -1})
			}
		}
	}
	// THE CLOCK'S OWN ROWS (timestamps.go) are laid out from here for the reason
	// every blank on this surface is: they are spacing-bearing blocks, and a
	// block that emitted its own gap would be a second spacing law. `walk` is the
	// turn this pass is inside and the last moment it knows about — the two facts
	// a receipt and a seam mark are drawn from. It runs over the CONVERSATION and
	// over nothing else (see [deck.clock]).
	walk := stampWalk{}
	clock := func(i int) {
		if !d.clock {
			return
		}
		drew := false
		out, drew = a.stampBlock(d, out, &walk, i, width, gap)
		if drew {
			wasCluster, wasBlock = false, false
		}
	}
	for i := 0; i < len(es); i++ {
		e := &es[i]
		if f, ok := folds[i]; ok {
			open := a.workMode == config.WorkOpen || d.workOpen[f.turn]
			out = append(out, row{text: workIndent(width) + a.pal.dim(a.workfoldLabel(f)), entry: -1, hit: hitWorkFold, turn: f.turn})
			if !open {
				i = f.answer - 1
				wasCluster, wasBlock = false, false
				continue
			}
		}
		if e.turn != walk.turn {
			// The turn before this one is over: its receipt, and then the mark
			// that says how long ago that was. Both are drawn HERE — at the seam
			// between two turns — because that is where a person reads them.
			clock(i)
		}

		// A run of tool entries from one turn is a cluster, and a cluster is
		// laid out as a unit: it is the thing that folds.
		if e.kind == entryTool {
			end := i + 1
			for end < len(es) &&
				es[end].kind == entryTool &&
				es[end].turn == e.turn {
				end++
			}
			if wasBlock || (!wasCluster && !opensTurn(es, i)) {
				gap()
			}
			out = a.clusterRows(d, out, i, end, width)
			wasCluster, wasBlock = true, false
			i = end - 1
			continue
		}

		// A run of landed tasks is a BATCH, and a batch is laid out as a unit for
		// the reason a cluster is: whether it rolls up into one object is a
		// property of the run and not of any card in it (taskdone.go). It reads
		// the DECK's entries, like every other block in this pass: a room is a
		// list of its own, and an index is only meaningful against the list it
		// was taken from.
		if e.kind == entryDone {
			end := i + 1
			for end < len(es) && es[end].kind == entryDone {
				end++
			}
			gap()
			out = a.doneCluster(d, out, i, end, width)
			wasCluster, wasBlock = false, true
			i = end - 1
			continue
		}
		if e.kind == entryHarness {
			gap()
			for _, text := range a.harnessFeedRows(e.harness, width, a.selected(i)) {
				hit := hitNone
				if strings.Contains(text, "[enter] save") {
					hit = hitHarness
				}
				out = append(out, row{text: text, entry: i, hit: hit})
			}
			wasCluster, wasBlock = false, true
			continue
		}

		rows := a.entryRows(d, i, width)
		if len(rows) == 0 {
			continue
		}
		if wasCluster || wasBlock || e.kind == entryUser || e.kind == entryTask ||
			(e.kind == entryStanding && e.stand != nil && !e.stand.news()) {
			// A PROPOSAL TAKES A BLANK OF ITS OWN. It is the one block on this
			// surface that interrupts a reply to ask something, and a question
			// wedged against the sentence above it reads as part of that sentence.
			// AND ONE AFTER IT, which is what wasBlock buys: the block has a foot,
			// and a paragraph that started on the row under it would be a paragraph
			// inside the question.
			gap()
		}
		// The proposal is the only non-tool block a click acts on, so it is the
		// only one that carries a hit (task.go) — and its choices row carries a
		// different one, because that row answers the question rather than opening
		// the brief.
		hit := hitNone
		if e.kind == entryTask {
			hit = hitTask
		}
		// THE BLOCK'S REFERENCES ARE NUMBERED ACROSS ITS ROWS, and the count is
		// reset here because the pointer holds a link as (block, ordinal): a
		// paragraph re-wraps when the frame is dragged, so an ordinal counted per
		// screen row would name a different phrase after a resize (markdown.go's
		// [taskLink]).
		links := 0
		for n, text := range rows {
			at := hit
			if e.kind == entryTask && e.card != nil && !e.card.settled() {
				switch n {
				case e.card.choiceRow:
					at = hitChoice
				case e.card.modelRow:
					at = hitModel
				}
			}
			// AND THE STANDING CARD'S ANSWERS ROW, which is the only row of that
			// block a click acts on: the block itself has no fold to open, so
			// pressing anywhere else on it does nothing (standing.go).
			if e.kind == entryStanding && e.stand != nil && !e.stand.settled() && n == e.stand.choiceRow {
				at = hitStandChoice
			}
			drawn := row{text: text, entry: i, hit: at}
			// THE LINK PASS RUNS ON THE MODEL'S OWN ROWS AND ON NOTHING ELSE
			// (markdown.go). It is applied HERE — after the block was rendered and
			// wrapped, at the moment its rows become screen geometry — because a
			// link's columns are a fact about the row it landed on, and the row it
			// lands on is decided by a wrap this pass must not have an opinion
			// about.
			if e.kind == entryAssistant {
				// AND THE ONE THE POINTER IS ON IS INKED BY THE SAME PASS. It cannot be
				// done afterwards: the row that comes back is styled text, and a hue
				// spliced into it by column would have to redo the escape bookkeeping
				// [paintLinks] is already doing (markdown.go).
				hot := -1
				if at := a.hoveringLink(i); at >= 0 {
					hot = at - links
				}
				drawn.text, drawn.links = a.linkTasks(text, hot)
				for j := range drawn.links {
					drawn.links[j].ord = links + j
				}
				links += len(drawn.links)
				// AND THE TABLE FEET ARE READ BACK OFF THE BLOCK, keyed by the row
				// they landed on. They are not derived here for the reason the links
				// above them are derived here: a foot's columns are decided by the
				// render that drew it, and this pass is the one that turns that
				// block's rows into screen geometry (mdtable.go).
				drawn.foot = e.feet[n]
			}
			out = append(out, drawn)
		}
		wasCluster = false
		wasBlock = e.kind == entryTask || (e.kind == entryStanding && e.stand != nil && !e.stand.news())
	}
	// THE LAST TURN'S RECEIPT, which has no next turn to be drawn at the seam
	// with. A turn still running has no stamp yet, so this draws nothing until
	// the moment it settles — which is exactly when the figures become true.
	clock(-1)
	// THE INDENT LAW is applied after layout so every kind of machinery,
	// including expanded details and synthetic fold rows, obeys one rule.
	//
	// IT COSTS THE ROWS IT MOVES TWO CELLS, and the block that lays itself out
	// flush to the right edge is the one that has to know: a tool line built to
	// the frame's whole width and then shoved two columns right is two columns
	// wider than the column it is drawn in, and [app.railJoin] cuts the overhang
	// back with an ellipsis — which is where a running call's spinner and its
	// clock went, leaving `0…` at the frame's edge. [app.toolLine] and
	// [app.formingLine] subtract [workIndentCols] for exactly that reason, AFTER
	// they have chosen their tier from the frame's own width.
	if workIndent(width) != "" {
		for i := range out {
			if rowIsWork(out[i], es, folds) && !strings.HasPrefix(ansi.Strip(out[i].text), "  ") {
				out[i].text = "  " + out[i].text
			}
		}
	}
	return out, wasCluster || wasBlock
}

// hoverPass paints the row a person is on — under the pointer or under the
// keyboard cursor — and it is the last thing done to any row list on this
// surface, the conversation's and the room's alike.
//
// CURSOR AND HOVER ARE ONE STEP, NOT TWO, so they are one pass. The pointer's
// row has come up onto THE GROUND LADDER's cursor step here since hover was
// built; the keyboard's row said its position with an accent on the rail glyph
// and no ground at all, so ↑ and ↓ moved something a pointer crossing the same
// rows would have lit. That is the ladder's own refusal — the row a person is on
// does not change appearance depending on which hand they used — and mending it
// here rather than at the four renderers means the tool row, the phone row, the
// forming row, the done card, the rollup and the harness card all learn it at
// once, and none of them has to remember the pass exists.
func (a *app) hoverPass(out []row, width int) {
	for i := range out {
		if a.isHot(out[i]) || a.onCursorRow(out[i]) {
			out[i].text = a.hoverRow(out[i].text, width)
		}
	}
}

// onCursorRow reports whether the KEYBOARD cursor is on this row: ↑/↓ walk
// [app.sel] over the turn's calls and enter opens whichever one it stopped on.
//
// It is not folded into [app.isHot] because that predicate answers "is the
// POINTER here", and the linear tier has no pointer — see the note there. A
// keyboard cursor is a position in a list and a position is a fact for every
// reader, so the two questions stay two questions even though today's answer
// takes them to the same rung.
func (a *app) onCursorRow(r row) bool {
	return r.entry >= 0 && a.selected(r.entry)
}

// isHot reports whether the pointer is on this row. The linear tier has no
// pointer at all (Options.Linear), so it has no hot row.
func (a *app) isHot(r row) bool {
	if a.linear {
		return false
	}
	switch a.hot.kind {
	case hoverEntry:
		return r.entry >= 0 && r.entry == a.hot.entry
	case hoverFold:
		return (r.hit == hitFold || r.hit == hitWorkFold) && r.turn == a.hot.turn
	}
	return false
}

// opensTurn reports whether the entry at i is the first thing its turn drew.
// A cluster that opens a turn follows the person's own message and takes no
// blank of its own — the user message already brought one.
func opensTurn(es []entry, i int) bool {
	for at := i - 1; at >= 0; at-- {
		if es[at].turn != es[i].turn {
			return true
		}
		if es[at].kind != entryUser {
			return false
		}
	}
	return true
}

// entryRows is the per-entry cache for everything that is not a tool call.
//
// Tool lines never come through here — [app.clusterRows] draws them, because
// their marker depends on their position in the cluster — and they are
// deliberately NOT cached: one of them is animating, all of them are one line
// plus a bounded expansion, and a cache with an animation in it is a cache that
// has to be invalidated thirty times a second, which is not a cache but a bug
// with a field.
func (a *app) entryRows(d deck, i, width int) []string {
	e := &d.entries[i]
	// A RUNNING COMPACTION IS NOT CACHED, for the reason the tool lines are not:
	// its spinner and its count-up are functions of the frame, so a cached row
	// would be a still photograph of an animation. It rejoins the cache the
	// moment it settles, which is the moment it stops moving.
	if e.kind == entryCompact && e.ended.IsZero() {
		return a.renderEntry(i, e, width)
	}
	// AN OPEN PROPOSAL IS NOT CACHED EITHER, and for the same reason: its
	// countdown is a function of the frame (task.go). It rejoins the cache the
	// moment it is answered, which is the moment the clock stops.
	if e.kind == entryTask && e.card != nil && !e.card.settled() {
		return a.renderEntry(i, e, width)
	}
	// AND A SIGN-IN THAT IS STILL WAITING, for the reason both of those are not:
	// its spinner is a function of the frame (connect.go). It rejoins the cache
	// the moment it settles, which is the moment it stops moving.
	if e.kind == entryConnect && e.conn != nil && e.conn.state == connectWaiting {
		return a.renderEntry(i, e, width)
	}
	// AND AN OPEN STANDING CARD, for the reason the proposal above is not: its
	// meter drains toward the moment the engine declines it (standing.go). It
	// rejoins the cache the moment it is answered.
	if e.kind == entryStanding && e.stand != nil && !e.stand.settled() && !e.stand.news() {
		return a.renderEntry(i, e, width)
	}
	// AND A TASK COMMAND'S PRE-FLIGHT, for the reason all three of those are not:
	// `shaping the brief…` carries a spinner and a count-up while the call is out
	// (taskcommand.go's [app.preflightRows]), and both are functions of the frame.
	// It rejoins the cache the moment the wait ends, which is the moment the line
	// is taken away altogether.
	if a.waiting(e) {
		return a.renderEntry(i, e, width)
	}
	if e.built && e.width == width && !e.stale {
		return e.rows
	}
	e.rows = a.renderEntry(i, e, width)
	e.width, e.built, e.stale = width, true, false
	return e.rows
}

// renderEntry paints one block. Nothing here appends a blank row — see
// [app.layout].
//
// The index is carried in for one reason: hover is a fact about a POSITION in
// the conversation, and the only block that draws its own hover state — the
// thinking block, whose marker brightens — is also the only one whose rows are
// cached (hover.go marks it stale in exchange).
func (a *app) renderEntry(i int, e *entry, width int) []string {
	switch e.kind {
	case entryUser:
		// THE PERSON'S OWN WORDS, IN THE PERSON'S OWN HUE — the glyph and the
		// whole body in the accent, and every continuation line aligned under
		// the TEXT rather than under the glyph. The glyph marks the turn; the
		// column belongs to the sentence.
		//
		// The body was bold ink until this wave, and bold was the wrong marker
		// for one reason: MARKDOWN OWNS WEIGHT. An assistant answer with a bold
		// lead-in renders exactly like a person's message, and the two things a
		// reader must never confuse were separated by an attribute either of
		// them could wear. Hue is the one channel identity can hold alone —
		// nothing the model writes is ever painted in the accent — so identity
		// takes hue and markdown keeps weight, and neither can impersonate the
		// other. On a sixteen-colour terminal the accent degrades to bold
		// (styles.go's tier table), which is the old rendering and the right
		// one there: with no hue at all, weight is the only marker left.
		body := wrap(e.text, width-2)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := "  "
			if i == 0 {
				lead = a.pal.accent(a.pal.youGlyph())
			}
			// AND A RECOGNIZED SLASH COMMAND KEEPS ITS CHIP AFTER IT IS SENT
			// (slashchip.go). The box is where a person learns that this surface
			// knows the word, and a message that dropped the mark on its way into
			// the transcript would take the fact back the moment it mattered — a
			// conversation scrolled back through is the only record of what was
			// asked for. Every row of a wrapped message opens at a boundary: the
			// wrap breaks on spaces, and a word too long to break on one is not a
			// command either.
			// Sent tag ranges are stored on the unwrapped message. Wrapped rows
			// cannot reuse those offsets, so a line away from the head may chip a
			// send door only when this entry records that one acted.
			spans := transcriptCommandSpans([]rune(line), e.actedTags)
			if len(e.actedTags) > 0 && i > 0 {
				// Wrapping changes offsets; routed messages are ordinarily one line,
				// while the scanner still safely recognizes their door on this row.
				spans = commandSpans([]rune(line), true)
			}
			out = append(out, lead+paintCommandSpans(line, spans, a.pal, a.pal.accent))
		}
		// AND A PATH THE PERSON TYPED IS A DOOR TOO (pathlink.go). The commonest
		// one here is not typed at all: an `@task` mention leaves a footnote
		// block naming the node's transcript, and that block is the fastest way
		// into what a task actually did.
		//
		// AND THEN THE TURN SAYS WHAT IT WAS PART OF, when it was part of anything
		// (turncontext.go). It is added AFTER the paths are linked, and that order
		// is the point: the mark is this surface talking about the message, not a
		// word of the message, so nothing in it may become a door. On an ordinary
		// turn it adds nothing at all — which is nearly every turn, and is why this
		// line changes no frame most people will ever look at.
		return a.turnContextRows(a.linkPaths(out), e.context, width)

	case entryAssistant:
		return a.linkPaths(a.assistantRows(i, e, width))

	case entryThinking:
		return a.thoughtRows(e, width, a.hoveringEntry(i))

	case entryDivider:
		return []string{a.divider(e.text, width)}

	case entrySeam:
		// THE DIM "· " LANE, like every other line this surface says on its own
		// account, and WRAPPED rather than cut — the same two leads a note uses
		// above. The sentence states a limit, and a limit truncated at column 58
		// on a narrow frame is the half-sentence that invites exactly the wrong
		// conclusion: "above here the model keeps a shortened rec…".
		out := make([]string, 0, 2)
		for i, line := range wrap(e.text, width-2) {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			out = append(out, a.pal.dim(lead+line))
		}
		return out

	case entryCompact:
		return []string{a.compactRow(e, width)}

	case entryConnect:
		return a.connectRows(e, width)

	case entryTask:
		return a.taskCardRows(e.card, width, a.sel == i)

	case entryStanding:
		return StandingCardRows(a, e.stand, width, a.sel == i)

	case entryHarness:
		return a.harnessFeedRows(e.harness, width, a.sel == i)

	case entryNote:
		// A WAIT THAT IS STILL RUNNING IS NOT A NOTE YET. The lane's other lines
		// are facts about work that is over, and a command's pre-flight is the one
		// thing in it that is still happening — so while it is, it wears the
		// spinner and the clock every other live row on this surface wears
		// (taskcommand.go's [preflight]).
		if a.waiting(e) {
			return a.preflightRows(e, width)
		}
		// A LINE MAY BE QUIET; THE FACT IT CARRIES MAY NOT BE (payload.go). The
		// lane keeps its dim prose and its dim lead — a note is still the surface
		// talking about itself — while the words the person typed the command to
		// READ step up to ink, and any door the sentence names wears the chip it
		// wears in the box and in the message (slashchip.go). The walk is built
		// once for the whole note and spent across its wrapped rows, because the
		// data are in the order of the sentence and not of the rows the frame
		// happened to break it into.
		//
		// A NOTE WHOSE INDENTATION IS ITS MEANING IS CUT RATHER THAN RE-FLOWED
		// ([app.noteBlock]). The one shape that asks for it is a subharness card,
		// where the indent under a lane is what says the step belongs to that lane
		// — and [wrap] laid every one of those flat against the margin.
		body := wrap(e.text, width-2)
		if e.block {
			body = noteBlockLines(e.text, width-2)
		}
		out := make([]string, 0, len(body))
		walk := factWalk{words: e.facts}
		for i, line := range body {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			// The lead is painted WITH the line rather than beside it, so a note
			// with nothing to lift comes back as the one dim run it has always been
			// — [paintPayload] hands an unmarked line straight back to the prose
			// role. The spans are shifted by the lead's own two cells, which is what
			// [noteLead] is for.
			out = append(out, paintPayload(lead+line,
				shifted(walk.take([]rune(line)), noteLead), a.pal, a.pal.dim))
		}
		// A NOTE IS WHERE THIS SURFACE NAMES ITS OWN FILES, and it names them in
		// full on purpose (statusnote.go): /status's `file`, /help's `session ·`,
		// `exported · …`, `resumed …`, `new session · …`. Every one of them is a
		// path somebody was going to select and paste somewhere, which is the
		// same sentence as "somebody was going to open it".
		return a.linkPaths(out)
	}
	return nil
}

// assistantRows is the markdown swap.
//
// While a turn streams, the live tail is PLAIN wrapped text: markdown of a
// half-written sentence costs a parse per frame and re-flows under the
// reader's eye. Every [markdownThrottle] the settled PREFIX — everything up to
// the last newline — is promoted to rendered rows and remembered as promoted,
// so the formatting catches up without the tail flickering between two
// renderings. On EventTurnDone the whole block is rendered at once.
//
// A FOOT UNDER A TABLE IS DRAWN ON THE SETTLED RENDER AND ONLY THERE
// (mdtable.go). A table half-arrived has columns that will move when the rest of
// it lands, and an offer to open something that is still being written is a
// promise the surface cannot keep — so the streaming path below leaves the
// block's feet where it finds them, which is nowhere.
//
// The index is carried in for one reason, and it is [app.renderEntry]'s: a
// foot brightens under the pointer, and the pointer is a fact about a POSITION
// in the list being drawn.
func (a *app) assistantRows(at int, e *entry, width int) []string {
	if e.settled {
		return a.settledMarkdown(at, e, width)
	}
	e.feet = nil
	var out []string
	if e.mdCut > 0 {
		out = append(out, a.renderMarkdown(e.text[:e.mdCut], width)...)
	}
	out = append(out, wrap(e.text[e.mdCut:], width)...)
	return trimBlanks(out)
}

// stillWorking is how long a turn has to be silent before the indicator says
// so out loud.
//
// THE DEFECT THIS FIXES: the session's loop retries a failed request silently,
// with a backoff — a rate limit, a 529, a connection reset — and it says nothing
// to the surface while it does, because a retry that succeeds is not news. From
// the outside that is indistinguishable from a hang: three dots, pulsing, for
// forty seconds. The dots are the only thing on screen and they claim exactly
// as much at second one as at second forty.
//
// Ten seconds is chosen against the thing being waited on rather than against a
// person's patience: a first token from a large model on a cold cache can take
// six or seven, so below ten this would fire on ordinary turns and mean nothing.
// Past it, silence is either a retry or a very long tool-free think, and "still
// working" is true of both — which is why it says that and not "retrying". The
// surface does not know that it is retrying. It knows the stream has said
// nothing for ten seconds, and that is exactly what it claims.
const stillWorking = 10 * time.Second

// stillWorkingWord is the suffix.
const stillWorkingWord = " · still working"

// ── THE WAIT FOR THE FIRST BYTE ─────────────────────────────────────────────
//
//	···                                                    under four seconds
//	··· waiting for kimi-k3 · 12s                          past them
//	··· waiting for kimi-k3 · 47s · nothing has come back yet
//	··· trying again · 12s                                 after a cut stream
//
// THE DEFECT THIS FIXES: [stillWorking] above is measured from [app.lastDelta],
// which is the last thing the stream SAID — so it describes a turn that spoke
// and then stopped. The state a person actually complains about is the other
// one: a request went out and the provider has not yet produced a first byte,
// for twenty seconds, for a minute. Nothing is streaming, no call is spinning,
// and the pulse claims exactly as much at second one as at second fifty. From
// where the person sits that is indistinguishable from a hung program.
//
// WHAT THE LINE IS ALLOWED TO CLAIM is bounded by what the surface can see, and
// the surface cannot see the wire. It knows a request went out ([app.awaited]),
// it knows the stream has said nothing since, and it knows which model the
// conversation is pointed at. So it says exactly that and no more: not "the
// network is slow", not "the model is thinking" — the surface does not know
// either of those and both are frequently false.
//
// "TRYING AGAIN" IS THE ONE EXCEPTION, and only because it stopped being a
// guess. The engine cuts a request that has gone quiet or come apart and says so
// (session.EventRetrying, internal/provider's streamguard.go), so on that one
// event — and never by inference from a long wait — the words change. A wait
// nobody reported a retry for still reads "waiting for", however long it runs.
//
// IT NEVER RUNS UNDER A CALL. A tool that is executing has its own spinner and
// its own count-up (toolview.go), and [app.ellipsis] has already stood the
// pulse down for it — a second clock on the same wait would be the surface
// timing a `go test` and calling it a provider.
const (
	// waitingGrace is how long a request may be outstanding before the surface
	// puts a clock on it. A FAST PROVIDER MUST NEVER SHOW ONE: a first token
	// commonly lands in one or two seconds, and a figure that appears and
	// vanishes on every ordinary turn is chrome that trains the eye to ignore
	// it — so the grace sits above the ordinary case rather than at it.
	waitingGrace = 4 * time.Second
	// waitingLong is when the wait stops being ordinary. Past thirty seconds a
	// person is no longer waiting, they are deciding whether to interrupt, and
	// the honest thing to give them for that decision is the plain fact.
	waitingLong = 30 * time.Second
)

const (
	// waitForWord opens the line when the model is known, waitBareWord when it is
	// not — the emptiness law: an unknown name renders as nothing rather than as
	// an empty slot after "waiting for".
	waitForWord  = " waiting for "
	waitBareWord = " waiting"
	// waitLongWord is the escalation, and it is a statement of fact rather
	// than an alarm: the request is out, and the answer is that nothing has
	// arrived. It deliberately does not say whose fault that is.
	waitLongWord = " · nothing has come back yet"
	// retryWord replaces the whole "waiting for <model>" half while the request
	// on the wire is a SECOND attempt. The model is not named on it: the name was
	// on the line that was just cut, and naming it again would suggest the retry
	// went somewhere else. It waits out no grace, because the person has just
	// watched an answer disappear and is owed the reason immediately.
	retryWord = " trying again"
)

// waitingWords is the dim tail on the pulse while a model request is
// outstanding and the stream has said nothing at all against it, or "" when
// there is nothing to say — no turn running, a call spinning, the stream
// already speaking, or a wait still inside its grace.
//
// NOTHING NEW TICKS FOR IT. The frame clock already redraws while a turn runs —
// it is what turns the pulse — so the count-up is a function of the time at
// paint, in the spelling every other live clock on this surface uses
// ([countUpWord], toolview.go), and costs the surface no wakeup of its own.
func (a *app) waitingWords() string {
	if a.state != stateWorking || a.awaited.IsZero() || a.running() {
		return ""
	}
	waited := time.Since(a.awaited)
	if waited < waitingGrace && !a.retrying {
		return ""
	}
	word := waitBareWord
	switch {
	case a.retrying:
		word = retryWord
	case modelBase(a.model) != "":
		word = waitForWord + modelBase(a.model)
	}
	if clock := countUpWord(waited); clock != "" {
		word += " · " + clock
	}
	if waited >= waitingLong {
		word += waitLongWord
	}
	return word
}

// pulse is the ellipsis frame — or the still one, in the linear tier, where an
// animation is a word repeated forever.
func (a *app) pulse() string {
	if a.linear {
		return ellipsisFrames[len(ellipsisFrames)-1]
	}
	return ellipsisFrames[(a.paints/pulseStep)%len(ellipsisFrames)]
}

// ellipsis is the sign of life while a turn runs and nothing else on screen is
// moving. It is suppressed while text is actively streaming, and suppressed
// while any call is spinning: the text and the spinner each already answer "is
// this alive?", and two answers to one question is one too many.
func (a *app) ellipsis() (string, bool) {
	if a.state != stateWorking || a.running() {
		return "", false
	}
	if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].text != "" && !a.quiet() {
		return "", false
	}
	line := a.pal.accent("  " + a.pulse())
	// THE WAIT OUTRANKS THE SILENCE, and only one of the two is ever on the
	// line. They are two ways of saying the same thing — nothing is arriving —
	// and the wait is the more specific of them: it names what is being waited
	// on and how long for, where "still working" only says that something is.
	if tail := a.waitingWords(); tail != "" {
		return line + a.pal.dim(tail), true
	}
	if a.silentFor() >= stillWorking {
		line += a.pal.dim(stillWorkingWord)
	}
	return line, true
}

// harnessStepRow is the live row under a running sub-harness's announcement:
// the step it just finished (harness.go's [app.stepHarness]).
//
// IT TAKES THE ELLIPSIS'S PLACE RATHER THAN SITTING BESIDE IT. The pulse means
// "this is alive" and so does a step landing every few seconds — and two answers
// to one question is one too many, which is the rule [app.ellipsis] is already
// written to about streaming text and spinning calls.
//
// It is drawn only while the turn is working. A step that arrived on a run whose
// turn has since ended is a row about work that is over, and the report is on
// screen by then saying what all of it did.
func (a *app) harnessStepRow(width int) (string, bool) {
	if a.state != stateWorking || a.harnessStep == "" {
		return "", false
	}
	return a.pal.dim(fit("  "+a.harnessStep, width)), true
}

// silentFor is how long the stream has said nothing. Zero when nothing has ever
// arrived, which is a turn that has not started rather than one that has stopped.
func (a *app) silentFor() time.Duration {
	if a.lastDelta.IsZero() {
		return 0
	}
	return time.Since(a.lastDelta)
}

// divider is the compaction mark: a rule with the fact in it, because a
// conversation that silently lost its middle is a conversation the person
// cannot reason about.
func (a *app) divider(hint string, width int) string {
	label := " ⚭ " + hint + " "
	rest := width - ansi.StringWidth(label) - 2
	if rest < 0 {
		return a.pal.dim(ansi.Truncate("──"+label, width, glyphMore))
	}
	left := rest / 2
	return a.pal.dim(strings.Repeat("─", left+2) + label + strings.Repeat("─", rest-left))
}

// compactRow draws one compaction pass, in the two shapes it has.
//
// RUNNING, it is the braille spinner, the session's own hint and the clock:
//
//	⠙ compacting ~84k tokens · 6s
//
// It borrows the spinner and the count-up from the tool lines rather than
// inventing an animation, because a person who has learned that a spinner means
// "this is executing right now" has learned it here too — and it turns on the
// same [spinnerStep] grid, so two moving rows on one screen never beat against
// each other. The hue is the DIM the divider wears: this is the surface talking
// about its own housekeeping, and housekeeping is never the subject. (The ask
// violet is not available to it — that hue means a person is being asked
// something, and nobody is being asked anything here.)
//
// SETTLED, it is the rule it always was, with what it cost in time:
//
//	───── ⚭ compacted from ~84k tokens · took 6s ─────
//
// The duration is dropped under a second, by the same law the tool clock uses
// ([countUpWord]'s floor): "took 0s" is a column read for nothing.
func (a *app) compactRow(e *entry, width int) string {
	if !e.ended.IsZero() {
		label := e.text
		if !e.began.IsZero() {
			if word := countUpWord(e.ended.Sub(e.began)); word != "" {
				label += " · took " + word
			}
		}
		return a.divider(label, width)
	}
	// The linear tier's objection to a spinner is the one it makes on a tool
	// line: a claim repeated thirty times a second is heard thirty times a
	// second by a surface being read aloud. A still `*` makes it once.
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		mark = glyphRunASCII
	}
	line := mark + " " + e.text
	if word := countUpWord(a.now().Sub(e.began)); word != "" {
		line += " · " + word
	}
	return a.pal.dim(ansi.Truncate(line, width, glyphMore))
}

// ── THE BOTTOM HUD ──────────────────────────────────────────────────────────
//
// The bottom of this surface is TWO ROWS, and every element on them has exactly
// one job:
//
//	─ ~/s/aforge-v2 · chat-v3-task* ──────────────── @ files · / commands ─
//	porting the parser · gpt-4.1-mini    2 jobs · $0.14 · 12.4k/128k ▁▂▃▅ · ⠹ working · 4s
//
// LEFT IS IDENTITY — who you are talking to and where. RIGHT IS TELEMETRY AND
// ALIVENESS — how much it has cost, how full it is, how fast it is going, and
// whether it is going at all. Nothing crosses: a number never appears on the
// left, and a name never appears on the right, so a person learns ONE place to
// look for each question instead of scanning a line of alternating kinds.
//
// THE PAINT LAW: dim by default. Paint is spent on three things and nothing
// else — ALIVENESS (the spinner and its clock), DECISIONS (a question waiting,
// a context meter about to compact, a gate left open), and RECENCY (a number
// that just moved, fading back to furniture over ten seconds). A surface where
// everything is painted has told you nothing about what to look at.
//
// THE PRODUCT NAME IS GONE from this line. It was here for the screenshot —
// a terminal photograph that names everything except the program — and the
// identity cluster below carries the conversation's own name, which identifies
// a pane far better than a word that is the same in every one of them.
//
// The two rows are drawn by [app.legend] (the input's top border) and
// [app.statusRows] (the status row, which becomes two rows on a narrow frame).

const (
	// hudWide is where the frame is comfortable: everything is on it,
	// the session delta included.
	hudWide = 120
	// hudWrap is where the two clusters stop sharing a row. Below it the
	// telemetry takes a row of its own rather than eating the identity's — the
	// gap between the clusters is what separates them, and a gap of one cell is
	// not a gap.
	hudWrap = 100
	// hudTight is the narrow floor: the legend loses its branch and hints while
	// the status line keeps the conversation identity.
	hudTight = 70
)

// ── AGE-FADED TELEMETRY: PAINT FOLLOWS RECENCY ──────────────────────────────
//
// A number that changed a second ago and a number that has not moved in ten
// minutes are two different facts, and they were drawn identically. So every
// telemetry segment carries the moment it last CHANGED, and it is painted from
// that:
//
//	< 4s   ink     it just moved — this is the news on the line
//	< 10s  muted   it moved recently
//	else   dim     furniture, which is what a settled number is
//
// STALE NUMBERS STOP COMPETING. That is the whole point: at rest the entire
// right cluster is one quiet grey, so the one segment that is moving is the
// only thing on the line with any weight at all.
//
// A segment's FIRST appearance is not a change — there was nothing there to
// have changed, and a line that opens entirely in ink is a line with no
// hierarchy — so it starts at the bottom of the ramp and climbs only when it
// moves again.
//
// The clock behind it costs nothing while nothing is happening: during a turn
// the frame tick already runs at [frameInterval], and at turn settle exactly
// TWO one-shot ticks are scheduled ([fadeTicks]) so the fresh tier can expire
// on time. There is no idle ticker on this surface and there is not going to
// be one.
const (
	hudFresh = 4 * time.Second
	hudWarm  = 10 * time.Second
)

// ctxRingSize is how many turn-end context readings the sparkline holds, and
// how many the ETA estimator averages over. Six is the width a sparkline can
// carry without becoming a chart.
const ctxRingSize = 6

// hudSeg names one telemetry segment. The kind is what the fade clock is keyed
// by — the segment's POSITION is not, because a segment that disappears must
// not hand its age to the one that took its place.
type hudSeg uint8

const (
	// segCrew is the crew's preset word — `crew max` — the OTHER model dial,
	// drawn at the head of the telemetry so it stands beside the conversation's
	// model across the gap (crew.go's [app.crewSegment] says why it is one word).
	segCrew hudSeg = iota
	// segOpen is how many conversations this terminal is holding, and how many
	// of them want a person (keeper.go). It comes first among the run's own
	// facts — only the crew word, which belongs beside the model, stands ahead of
	// it — because it is the only segment that is not about the conversation in
	// front.
	segOpen
	segAmbient
	// segKeeping is the standing side's own presence: how many things are
	// keeping an eye on this project (homestanding.go). It sits beside segAmbient
	// because they answer one question — what is alive out there that nobody is
	// watching — and it is separate because they are two mechanisms with two
	// lifetimes: a background job dies with the window, and an item does not.
	segKeeping
	segDelta
	segCost
	segCtx
	segCache
	segBurn
	segETA
	segYolo
	segState
	segCount
)

// hudPart is one assembled segment: what it says, and which clock it is on.
type hudPart struct {
	kind hudSeg
	text string
}

// status is the HUD's status row, or both of its rows joined, which is what a
// caller that wants "the line" means. The frame draws the rows themselves —
// see [app.statusRows].
func (a *app) status(width int) string {
	return strings.Join(a.statusRows(width), "\n")
}

// statusRows is the two-cluster row: identity left, telemetry right, and the
// GAP BETWEEN THEM AS THE ONLY SEPARATOR. There is no pipe, no bracket and no
// rule between the clusters — space is what the eye reads as "these are two
// different subjects", and a glyph there would be furniture claiming to be
// structure.
//
// Below [hudWrap] the clusters stop sharing a row and the telemetry takes one
// of its own, still right-aligned. That is the only place this surface spends a
// row on chrome, and it spends it exactly where the alternative is truncating
// the numbers a person opened the terminal to read.
func (a *app) statusRows(width int) []string {
	// THE DOOR IS CLEARED BEFORE THE ROW IS LAID OUT AND WRITTEN ONLY WHERE IT
	// LANDED, so a span is its own answer to "was it drawn on this frame"
	// (standdoor.go). Every early return below is a row with no keeping segment
	// on it, and each of them leaves this cleared.
	a.keepSpan, a.keepRow = hudSpan{}, 0
	if width < 1 {
		a.modelSpan = hudSpan{}
		return []string{""}
	}
	// AND AT PHONE WIDTH IT IS A DECK, deterministically two rows, because the
	// ladder above has nothing left to give: at forty-four columns the identity
	// alone is wider than the frame and every number would be dropped before the
	// first segment is drawn. The deck keeps the two facts a phone can answer at
	// a glance and moves the rest into a sheet one tap away (statusdeck.go).
	if layoutTier(width) == tierPhone {
		return a.statusDeck(width)
	}
	left, parts, wrapped := a.statusLayout(width)
	right, plainRight := a.paintParts(parts)
	// THE CLUSTER GOES ACCENT IN A ROOM, and it is the one condition under which
	// it is painted at all: the chip is a statement about which page the keyboard
	// is pointed at, and it wears the accent at both ends of the frame — here and
	// in the pinned header (room.go).
	paint := a.pal.dim
	if a.roomOpen() {
		paint = a.pal.accent
	}
	// The right cluster is right-aligned in both shapes below, so where the
	// keeping segment landed is one piece of arithmetic said once.
	base := width - ansi.StringWidth(plainRight)
	if wrapped {
		a.markKeepingDoor(parts, base, 1)
		return []string{
			fit(a.paintIdentity(left, paint), width),
			rightAlign(right, plainRight, width),
		}
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(plainRight)
	if gap < 1 {
		// Nothing fits, even emptied: the telemetry is the half that survives,
		// because what is HAPPENING outranks what it is called. The identity is
		// not drawn, so nothing on this row is pressable either.
		a.modelSpan = hudSpan{}
		return []string{fit(right, width)}
	}
	a.markKeepingDoor(parts, base, 0)
	return []string{a.paintIdentity(left, paint) + strings.Repeat(" ", gap) + right}
}

// paintIdentity paints the left cluster, BRIGHTENING THE MODEL SEGMENT while the
// pointer is on it.
//
// Every interactive thing on this surface answers the pointer before it is
// clicked (hover.go), and this one never did: the model's name has been the door
// to the picker for a wave now, and it looked exactly like the telemetry it sits
// beside. A label that is also a control has to say so.
//
// IT IS A BRIGHTENING AND NOT A BACKGROUND BAND, which is the jump chip's own
// decision for the jump chip's own reason (jumpchip.go): the segment is three
// words at the end of a line, not a row of a list, and a highlighted rectangle
// around them would be the one boxed thing on a surface with no boxes. One step
// up from wherever the cluster already is — accent from the conversation's dim,
// ink from a room's accent — so the step reads the same in both.
//
// THE PIECES ARE PAINTED SEPARATELY RATHER THAN NESTED. These hues are raw SGR
// with an explicit reset (styles.go's [palette.paint]), so a colour inside a
// colour would end the outer one at the inner one's reset and leave the tail of
// the cluster unpainted. The cluster is plain text at this point and the span was
// measured against it, so cutting it in cells is exact.
func (a *app) paintIdentity(left string, paint func(string) string) string {
	span := a.modelSpan
	if !span.pressable() || !a.hoveringStatusModel() || span.to > ansi.StringWidth(left) {
		return paint(left)
	}
	lift := a.pal.accent
	if a.roomOpen() {
		lift = a.pal.ink
	}
	head := ansi.Cut(left, 0, span.from)
	segment := ansi.Cut(left, span.from, span.to)
	tail := ansi.Cut(left, span.to, ansi.StringWidth(left))
	return paint(head) + lift(segment) + paint(tail)
}

// hudGap is the smallest barrier the two clusters will stand next to each
// other across. Below it they are not two clusters, they are one line with a
// number in the middle of it.
const hudGap = 3

// statusLayout decides the row's shape: the identity, the segments that
// survived the frame's width, and whether the telemetry took a row of its own.
//
// THE NARROW LADDER, and it is one ladder because the frame, the chrome height
// and the hit-testing all resolve through here:
//
//	≥ 120   everything, the session delta included
//	≥ 100   the delta is gone; segments drop by [dropOrder] until it fits
//	< 100   the telemetry may take a row of its own — and it takes one only
//	        when the clusters would otherwise collide. A frame with a short
//	        name and a quiet session fits on one row at sixty columns, and
//	        spending a row of the conversation on a gap nobody needed is the
//	        cost this law exists to avoid.
func (a *app) statusLayout(width int) (string, []hudPart, bool) {
	// THE MODEL'S COLUMNS ARE RECORDED WHERE THE ROW IS LAID OUT, which is what
	// keeps the press and the paint in step: this function is what the frame, the
	// chrome height and the hit-testing all resolve through, so a segment drawn
	// here and a segment pressed there cannot be at two different offsets
	// (app.go's [app.statusPress]). A cluster the width pressure then drops
	// clears it again — see [app.statusRows].
	left, span := a.identityParts()
	a.modelSpan = span
	parts := a.telemetry(width)
	// The change clocks are stamped from the ASSEMBLED segments, before any
	// width pressure is applied: a number that moved has moved whether or not
	// this frame had room to say so.
	a.freshen(parts)

	if room := width - ansi.StringWidth(left) - hudGap; hudWidth(parts) <= room {
		return left, parts, false
	}
	if width < hudWrap {
		for hudWidth(parts) > width && dropSegment(&parts) {
		}
		return left, parts, true
	}
	for hudWidth(parts) > width-ansi.StringWidth(left)-hudGap && dropSegment(&parts) {
	}
	return left, parts, false
}

// statusHeight is how many rows the HUD's status takes: the frame, the chrome
// height and the pointer's hit-testing all have to agree about it (view.go).
func (a *app) statusHeight(width int) int {
	if width < 1 {
		return 1
	}
	// THE PHONE TIER ANSWERS FROM THE TIER, without laying anything out. The
	// wide row's second row is wrap-driven — it exists only when the clusters
	// would collide — and the deck's is not: it is two rows at every phone-width
	// frame, in every state, which is what lets this answer be a constant
	// (statusdeck.go).
	if layoutTier(width) == tierPhone {
		return deckHeight
	}
	if _, _, wrapped := a.statusLayout(width); wrapped {
		return 2
	}
	return 1
}

// identity is the left cluster: WHICH conversation, and WHAT is answering it.
//
//	porting the parser · gpt-4.1-mini:high
//
// The model is its BASENAME. "deepseek/deepseek-v4-flash" is a routing address
// and its first half is the same for every model a person is choosing between —
// nine cells that never vary, on the row where width is scarcest. The whole id
// stays wherever it is being CHOSEN or RECORDED: the picker's rows, the /model
// note, the session file. The reasoning rider is kept because it is not part of
// the address — it is how this model is being run (view.go's [app.statusRow]).
//
// The name falls back to the workspace's base name until the session has named
// itself (session's title.go), so the cluster is never empty.
func (a *app) identity() string {
	text, _ := a.identityParts()
	return text
}

// identityParts is that cluster and the COLUMNS ITS MODEL SEGMENT OCCUPIES on
// the row, because the model segment is a thing you can press: the whole point
// of a name on screen is that it is where a person already looks when they want
// to change it, and until this wave the only door was typing /model.
//
// The span is [from, to) in cells from the row's left edge, which is where this
// cluster is drawn. An empty span (to == 0) means there is nothing to press — a
// session with no model yet, or a room whose node is past being moved: the press
// always acts on WHAT THE ROW NAMES, so in a room it is the node's model and out
// here it is the conversation's, and neither can ever be mistaken for the other.
func (a *app) identityParts() (string, hudSpan) {
	// A ROOM RENAMES THIS CLUSTER AND NOTHING ELSE ON THE LINE. The identity is
	// WHERE YOU ARE, and while a room is open where you are is a task — but the
	// telemetry beside it is still the session's, because a room is a view over
	// one body region and not a second session (room.go). A status line that
	// re-pointed the cost and the context meter at a node would be quoting
	// figures nobody is measuring.
	//
	// AND THE MODEL SEGMENT NAMES THE ROOM'S NODE. The status row is ABOUT THE
	// WINDOW, and while a room is open the window IS that task — so the law above
	// argues FOR this and not against it. What the law forbids is re-pointing the
	// TELEMETRY, which measures the session and would be quoting figures nobody
	// took; the node's model is not a measurement, it is a fact the node
	// published. It is said here because this is the only ALWAYS-VISIBLE model
	// name on the screen, and a person who launched a task on one model, opened
	// its room, and read the conversation's model at the foot of the frame was
	// told the wrong thing by the one line they could not look away from.
	//
	// AND WHILE A ROOM IS OPEN THE SEGMENT IS A DOOR ONTO THAT NODE'S OWN MODEL —
	// never onto the conversation's. The two are one gesture over two subjects,
	// which is the only reading of "press the name to change it" that stays true
	// wherever the name is: what the row names is what the press moves. A picker
	// opened from in here retargets THIS node from its next turn on and touches
	// neither the conversation nor any other task (room.go's [app.retargetTask],
	// internal/session's [Agent.RetargetTask]).
	//
	// THE SPAN IS EMPTY WHENEVER THE PICK COULD NOT LAND, which is the design law
	// rather than a special case: a capability that cannot work is absent, not
	// broken. A node that has finished, failed, been stopped or needs a look has a
	// model that is a FACT about what happened — nothing can move it and the
	// engine refuses to try — so the name is still drawn and simply cannot be
	// pressed ([app.roomModelMovable] holds the whole of that list). An
	// affordance that lit up and then apologised would be worse than none.
	if a.roomOpen() {
		cluster := a.roomChip()
		word := a.roomModelWord()
		if word == "" {
			return cluster, hudSpan{}
		}
		// The LEAD WORD IS PART OF THE TARGET, exactly as the served rider is part
		// of the conversation's: "task glm-5.2" is one fact said in three words, and
		// a person pressing any of them means the same thing (room.go's
		// [roomModelLead]).
		from := ansi.StringWidth(cluster + " · ")
		cluster += " · " + word
		if !a.roomModelMovable() {
			return cluster, hudSpan{}
		}
		return cluster, hudSpan{from: from, to: from + ansi.StringWidth(word)}
	}
	name := a.sessionName()
	if name == "" {
		name = a.place
	}
	model := modelBase(a.model)
	if model == "" {
		return name, hudSpan{}
	}
	// The RIDER IS PART OF THE TARGET. "via deepinfra · 92 tok/s" is a fact
	// about the model that is answering, so a person pressing it means the same
	// thing they mean by pressing the id.
	segment := model + a.servedRider()
	from := ansi.StringWidth(name + " · ")
	return name + " · " + segment, hudSpan{from: from, to: from + ansi.StringWidth(segment)}
}

// hudSpan is a pressable stretch of the status row: [from, to) cells on it.
// It is the same shape a proposal's choices row carries for its three options
// (task.go's [choiceSpan]) and it is written and read the same way — by the
// render, then by the hit-testing — so a click can never land on a segment the
// frame drew somewhere else.
type hudSpan struct{ from, to int }

// pressable reports whether this span has any columns in it.
func (s hudSpan) pressable() bool { return s.to > s.from }

// holds reports whether a column is inside the span.
func (s hudSpan) holds(x int) bool { return s.pressable() && x >= s.from && x < s.to }

// servedSighting is the HUD's window onto the adapter's velocity ledger. It is
// a var so a test can state one sighting without a live endpoint; nothing else
// ever reassigns it.
var servedSighting = provider.LastServed

// servedWindow is how long a sighting still describes the present. Past it the
// rider goes quiet rather than keeping a rate from a conversation that has
// since gone to sleep on the line.
const servedWindow = 10 * time.Minute

// servedRider is the other half of the model's name: WHO ACTUALLY ANSWERED, and
// how fast they were writing.
//
//	deepseek-v4-flash · via deepinfra · 92 tok/s
//
// A model id is an address, not a machine. One id is fanned over many endpoints
// that answer at very different speeds for the same price, and the adapter both
// asks for the fastest of them and times what it got (internal/provider's
// velocity.go). The id alone therefore names a decision the session did not
// make — this is the part of it that is a fact.
//
// It is drawn only when the server's name is not already the model's own: an
// endpoint that IS the vendor adds nothing to "deepseek-v4-flash", and a line
// that reads "gpt-4.1 · via openai" is a cell of chrome per frame for a word
// the reader already has.
func (a *app) servedRider() string {
	sighting, ok := servedSighting(a.model)
	if !ok || sighting.Provider == "" {
		return ""
	}
	if a.now().Sub(sighting.At) > servedWindow {
		return ""
	}
	served := strings.ToLower(sighting.Provider)
	if strings.Contains(strings.ToLower(a.model), served) {
		return ""
	}
	rider := " · via " + served
	// THE RATE RIDES ONLY WHILE A TURN IS RUNNING. Who served is attribution
	// and stays; how fast they were writing is a claim about NOW, and a rate
	// from the last turn standing on an idle status line read as a live figure
	// nobody was producing — a person sat looking at "92 tok/s" over a chat
	// that was doing nothing.
	if sighting.Rate > 0 && a.state == stateWorking {
		rider += " · " + tokenWord(int(sighting.Rate)) + " tok/s"
	}
	return rider
}

// modelBase strips the vendor from a model id, and nothing else: everything
// after the last slash, which leaves a bare id alone and keeps a ":level" rider
// (the rider is appended after the id, and the slash is before it).
func modelBase(id string) string {
	if at := strings.LastIndexByte(id, '/'); at >= 0 {
		return id[at+1:]
	}
	return id
}

// telemetry assembles the right cluster IN ORDER, and the order is the question
// each segment answers about the run:
//
//	2 open · 1 waiting   how many conversations this terminal is holding
//	2 jobs · 1 watch     what is still alive out there
//	Σ +128 −14           what this session has written
//	$0.14                what it has cost
//	12.4k/128k · 10% ▁▂▃ what it is carrying, and where that has been going
//	⟲ saved $0.02 · 89%  what the cache gave back
//	1.2k tok/s           how fast it is writing right now
//	compaction in ~3     what is about to happen to it
//	YOLO                 the gate is open (and nothing when it is not)
//	⠹ working · 4s       what it is DOING — always last, because it is the one
//	                     segment that is true of the whole line
func (a *app) telemetry(width int) []hudPart {
	var parts []hudPart
	add := func(kind hudSeg, text string) {
		if text != "" {
			parts = append(parts, hudPart{kind: kind, text: text})
		}
	}
	// THE TWO DIALS READ AS A PAIR. The identity cluster ends with the model the
	// conversation talks to, and the telemetry begins with the crew word, so a
	// frame wide enough for both shows `… · deepseek-v4-flash      crew max · …`:
	// two facts, one gap, and no way to read `/crew max` as having moved the
	// model beside it. It is the first thing after the delta to go when the row
	// is short ([dropOrder]) — /status and the model picker's hint slot both say
	// it in full — and it is nothing at all on a door with no profile.
	add(segCrew, a.crewSegment())
	add(segOpen, a.openSegment())
	add(segAmbient, a.ambientSegment())
	add(segKeeping, a.keepingSegment())
	// THE DELTA IS THE LOWEST PRIORITY ON THE LINE and it says so twice: it is
	// drawn only on a comfortable frame, and it is the first thing [dropSegment]
	// takes when even that frame turns out to be full.
	if width >= hudWide {
		add(segDelta, a.deltaSegment())
	}
	add(segCost, dollars(a.cost))
	if context, _ := a.contextSegment(); context != "" {
		if spark := a.ctxSpark(); spark != "" && width >= hudTight {
			context += " " + spark
		}
		add(segCtx, context)
	}
	add(segCache, a.warmSegment())
	add(segBurn, a.burnSegment())
	add(segETA, a.etaSegment())
	add(segYolo, a.yoloSegment())
	word, _ := a.stateSegment()
	add(segState, word)
	return parts
}

// openSegment is how many conversations this terminal holds and how many of
// them want somebody:
//
//	2 open · 1 waiting
//
// IT IS ABSENT WHENEVER ONLY ONE IS OPEN, which is the ordinary case and the
// emptiness law's plainest application — a permanent `1 open` would be a
// permanent reminder of the absence of a feature. The `· N waiting` clause is
// absent when nothing is waiting, on the same terms.
//
// The deliberate $0.00 exception on this line is NOT extended here. That
// exception exists so a cost segment does not jump sideways as its width
// changes; this one appears and disappears with a real change in what is true,
// and a placeholder would be a lie about how many conversations are open.
//
// THE COUNT IS ASKED OF THE AGENTS AND NOT OF THE PRESENCE FILE, for the reason
// home's own rows are ([app.homeTrue]): the file lags by up to five seconds, and
// this is a pointer we are holding.
func (a *app) openSegment() string {
	open := a.openCount()
	if open < 2 {
		return ""
	}
	word := itoa(open) + " " + homeOpenWord
	if waiting := a.waitingCount(); waiting > 0 {
		word += " · " + itoa(waiting) + " waiting"
	}
	return word
}

// dropOrder is what the line gives up, first to last, when it does not fit,
// and it is ordered by how ACTIONABLE each segment is:
//
//	delta    what the session wrote — the only fact here about the past
//	crew     which preset aforge's own calls are on — a setting, not a
//	         measurement; it changes only when the person changes it, and
//	         /status, the picker's hint and bare /crew all say it in full
//	cache    an accounting nicety; the cost segment already carries the bill
//	eta      a forecast, and the meter beside it is already painted the warning
//	burn     nice to watch, but the clock on the state word says it is alive
//	ambient  a server holding a port is a thing a person acts on
//	cost     the bill
//	ctx      what the conversation is carrying, which is the decision it forces
//
// The state word and the safety posture are not in this list at all: one is why
// a person is looking at the line, and the other is why they should be.
//
//	open     how many other conversations this terminal holds — true, and about
//	         somewhere else; at forty columns what a person needs is what THIS
//	         conversation is doing
var dropOrder = []hudSeg{segDelta, segCrew, segOpen, segCache, segETA, segBurn, segKeeping, segAmbient, segCost, segCtx}

// dropSegment removes the least important segment still present, and reports
// whether it found one to remove.
func dropSegment(parts *[]hudPart) bool {
	for _, kind := range dropOrder {
		for i, part := range *parts {
			if part.kind == kind {
				*parts = append((*parts)[:i], (*parts)[i+1:]...)
				return true
			}
		}
	}
	return false
}

// hudWidth is what a set of segments measures, joined, unpainted.
func hudWidth(parts []hudPart) int {
	if len(parts) == 0 {
		return 0
	}
	width := 3 * (len(parts) - 1)
	for _, part := range parts {
		width += ansi.StringWidth(part.text)
	}
	return width
}

// paintParts joins the cluster, painted and plain. The plain string is what
// every width decision above is made from: measuring a painted string is
// measuring escape sequences.
func (a *app) paintParts(parts []hudPart) (string, string) {
	var painted, plain string
	for i, part := range parts {
		if i > 0 {
			painted += a.pal.dim(" · ")
			plain += " · "
		}
		painted += a.paintPart(part)
		plain += part.text
	}
	return painted, plain
}

// paintPart is where the hue budget is spent, and the order of these branches
// IS the priority of the three things paint is allowed to mean.
func (a *app) paintPart(part hudPart) string {
	switch part.kind {
	case segState:
		// ALIVENESS AND THE DECISION, both of which the state word owns.
		_, painted := a.stateSegment()
		return painted
	case segKeeping:
		// DIM, ALWAYS, AND THE ONE SEGMENT THAT MOVES WITHOUT CHANGING. It is
		// re-derived here rather than taken from part.text because its glyph
		// breathes while a firing is in flight and its TEXT must not, or the fade
		// ramp would paint it bright forever (homestanding.go's [app.keepingWord]
		// says the whole of it).
		//
		// AND IT BRIGHTENS UNDER THE POINTER, because it is a door: pressing it
		// opens /standing, and a label that is also a control has to say so
		// (standdoor.go, and [app.paintIdentity] for the same decision about the
		// model's name).
		if a.hoveringKeeping() {
			return a.pal.accent(a.keepingWord())
		}
		return a.pal.dim(a.keepingWord())
	case segYolo:
		// The one segment that is loud because of what it MEANS rather than
		// because of when it changed.
		return a.pal.bad(part.text)
	case segCtx:
		// The meter's three-rung ramp outranks its age: a conversation about to
		// compact is a decision a person can still act on, and "this number is
		// four seconds old" is not.
		switch a.ctxHeat() {
		case ctxDue:
			return a.pal.bad(part.text)
		case ctxNear:
			return a.pal.accent(part.text)
		}
	}
	return a.fadeSeg(part.kind, part.text)
}

// freshen stamps the change clocks. A segment that vanished loses its clock
// rather than keeping it: the next thing to appear under that kind is new, and
// new is not the same as recently changed.
func (a *app) freshen(parts []hudPart) {
	var seen [segCount]bool
	for _, part := range parts {
		if part.kind >= segCount {
			continue
		}
		seen[part.kind] = true
		if a.segText[part.kind] == part.text {
			continue
		}
		if a.segText[part.kind] != "" {
			a.segAt[part.kind] = a.now()
		}
		a.segText[part.kind] = part.text
	}
	for kind := hudSeg(0); kind < segCount; kind++ {
		if !seen[kind] {
			a.segText[kind], a.segAt[kind] = "", time.Time{}
		}
	}
}

// fadeSeg paints one segment at its age (see the ramp above).
//
// WHILE A PERSON IS BEING ASKED SOMETHING, the whole ramp collapses to dim. The
// consent question owns the screen's attention for as long as it is up, and a
// cost figure glowing beside it is a number competing with a decision.
func (a *app) fadeSeg(kind hudSeg, text string) string {
	if a.asking() || kind >= segCount {
		return a.pal.dim(text)
	}
	at := a.segAt[kind]
	if at.IsZero() {
		return a.pal.dim(text)
	}
	switch age := a.now().Sub(at); {
	case age < hudFresh:
		return a.pal.ink(text)
	case age < hudWarm:
		return a.pal.muted(text)
	default:
		return a.pal.dim(text)
	}
}

// rightAlign pushes a painted cluster to the right edge of the frame.
func rightAlign(painted, plain string, width int) string {
	if gap := width - ansi.StringWidth(plain); gap > 0 {
		return strings.Repeat(" ", gap) + painted
	}
	return fit(painted, width)
}

// ── THE TELEMETRY SEGMENTS ──────────────────────────────────────────────────

// ambientSegment is what is still alive that nobody is watching:
//
//	2 jobs · 1 watch
//
// It exists because background work is the one thing on this surface that
// happens OFF the transcript. A server started twenty turns ago is not on
// screen, is not in the reply, and is still holding a port; a watch is still
// firing into the conversation. Zero of both is the ordinary case and it draws
// NOTHING — an ambient count that reads "0 jobs" is a permanent reminder of the
// absence of a thing.
func (a *app) ambientSegment() string {
	stats := a.hudStats()
	var parts []string
	if stats.jobs > 0 {
		parts = append(parts, itoa(stats.jobs)+plural(" job", stats.jobs))
	}
	if stats.watches > 0 {
		parts = append(parts, itoa(stats.watches)+plural(" watch", stats.watches, "es"))
	}
	return strings.Join(parts, " · ")
}

// deltaSegment is what this SESSION has written, summed from the same tool
// arguments the per-turn "what changed" line is derived from (app.go's
// [app.computeStats]):
//
//	Σ +128 −14
//
// The per-turn line answers "what did that do"; this answers "what has this
// conversation done", which is the question a person asks before they decide
// whether to keep it. It is the first segment sacrificed to width because it is
// the only one on the line that is about the PAST rather than about now.
func (a *app) deltaSegment() string {
	stats := a.hudStats()
	if stats.adds == 0 && stats.dels == 0 {
		return ""
	}
	return "Σ " + glyphAdd + itoa(stats.adds) + " " + glyphDel + itoa(stats.dels)
}

// ctxSpark is the last few turn-end context readings, as one glyph each:
//
//	12.4k/128k · 10% ▁▂▂▃▅▆
//
// It answers the question the number cannot: a conversation at 60% that has sat
// at 60% for six turns and one that arrived there from 20% are the same figure
// and completely different situations. The bars are measured against the
// COMPACTION THRESHOLD rather than the window, for the reason the heat ramp is
// ([accentAtThresholdPercent]): the threshold is the thing that actually
// happens to you.
//
// Fewer than two readings draws nothing. One bar is not a trend, it is a bar.
func (a *app) ctxSpark() string {
	// A SPARKLINE IS SHAPE, and the two tiers that cannot read shape do not get
	// one: a terminal that cannot be trusted with box drawing would render six
	// replacement characters, and a surface being read aloud would announce
	// them one by one. Both keep the number, which is the fact.
	if a.pal.ascii || a.linear {
		return ""
	}
	threshold := session.CompactThreshold(a.ctxWindow)
	if threshold <= 0 || len(a.ctxRing) < 2 {
		return ""
	}
	// THE SHAPE IS THE SPARK MACHINERY'S AND NOT THIS FUNCTION'S (spark.go). The
	// machine card's `hands` chart draws in the same alphabet off the same
	// quantizer, and one rounding rule is what keeps two sparks on one screen
	// from disagreeing about the same reading. The ceiling is STATED here —
	// a context bar means the same thing from one turn to the next — where the
	// chart on the card has no scale but its own window.
	return barSpark(a.ctxRing, threshold, len(a.ctxRing))
}

// burnSegment is how fast the model is writing, right now:
//
//	1.2k tok/s
//
// It is output tokens over the wall time of THIS turn, and it exists because
// "working" is a boolean and a person watching a long turn wants a rate. It is
// drawn only while a turn is running — a rate over a finished turn is a fact
// about the past wearing the clothes of a live one — and only after a second,
// because a rate computed over 200ms is a rate computed over the first packet.
func (a *app) burnSegment() string {
	if a.state != stateWorking || a.turnBegan.IsZero() {
		return a.holdBurn("")
	}
	elapsed := a.now().Sub(a.turnBegan)
	if elapsed < time.Second {
		return a.holdBurn("")
	}
	written := a.outputTokens - a.turnOutStart
	if written <= 0 {
		return a.holdBurn("")
	}
	return a.holdBurn(tokenWord(burnStep(int(float64(written)/elapsed.Seconds()))) + " tok/s")
}

// ── THE STEADY FIGURE ───────────────────────────────────────────────────────
//
// A NUMBER THAT MOVES FASTER THAN IT CAN BE READ IS NOT INFORMATION, IT IS
// MOTION. The burn rate is recomputed every frame, and unheld it lands on a
// different figure nearly every one of them: thirty times a second the status
// line becomes a line the terminal has to be told about again, to show a person
// digits their eye never resolved. The second and third digits of a token rate
// are not a fact anybody acts on — "about ninety" is the whole of what the
// figure says, and it says it whether it was drawn from 88 or from 91.
//
// So the DISPLAY is damped and the accounting is not. Every meter behind this
// line keeps its exact figure; what is held is the string, and it is held in
// two ways at once: the rate is rounded to a step worth reading, and the shown
// value is only replaced twice a second. Between replacements the segment is
// character for character the line it already was, which costs the wire
// nothing, and the person still watches a live rate — just one that stands
// still long enough to be read.

// burnHoldFor is how long a shown rate stands before it is allowed to move.
// Half a second is two updates a second: fast enough that a turn slowing down
// says so while it is still happening, slow enough that the figure is legible.
const burnHoldFor = 500 * time.Millisecond

// burnStep rounds a rate to a step a person reads as one number.
//
// The two tiers are the same rule stated for two magnitudes: keep about two
// digits of it. Under a hundred that is steps of five — the difference between
// 62 and 64 tok/s is a difference nobody is deciding anything on — and above it
// two significant figures, which is what [tokenWord] would draw anyway by the
// time the figure reaches thousands.
func burnStep(rate int) int {
	if rate <= 0 {
		return 0
	}
	if rate < 100 {
		return (rate + 2) / 5 * 5
	}
	step := 1
	for left := rate; left >= 100; left /= 10 {
		step *= 10
	}
	return (rate + step/2) / step * step
}

// holdBurn is the half-second hold: it returns the figure currently on the
// line, and adopts the new one only when the hold has run out. An empty
// candidate — the turn ended, or has not earned a rate yet — takes effect at
// once and clears the hold with it: a rate held past the turn it describes
// would be the surface reporting on work that has stopped.
func (a *app) holdBurn(text string) string {
	if text == "" {
		a.burnShown, a.burnAt = "", time.Time{}
		return ""
	}
	now := a.now()
	if a.burnShown == "" || now.Sub(a.burnAt) >= burnHoldFor {
		a.burnShown, a.burnAt = text, now
	}
	return a.burnShown
}

// etaSegment is the compaction forecast, and it only ever speaks when the
// answer is SOON:
//
//	compaction in ~3 turns
//
// The estimate is the average growth over the turn-end ring, against what is
// left before the threshold. It is deliberately silent in three cases: when the
// conversation is not growing (a session of reads and replies can sit flat for
// twenty turns, and "compaction in ~400 turns" is a number nobody will ever
// use), when the answer is more than [etaHorizon] turns away, and when
// compaction is already due — the meter is painted the bad hue by then, and a
// forecast of a thing that is happening is not a forecast.
func (a *app) etaSegment() string {
	turns, ok := a.compactionETA()
	if !ok {
		return ""
	}
	return "compaction in ~" + itoa(turns) + plural(" turn", turns)
}

// etaHorizon is how far ahead the forecast is worth making.
const etaHorizon = 5

func (a *app) compactionETA() (int, bool) {
	threshold := session.CompactThreshold(a.ctxWindow)
	if threshold <= 0 || len(a.ctxRing) < 2 || a.ctxTokens <= 0 {
		return 0, false
	}
	growth := (a.ctxRing[len(a.ctxRing)-1] - a.ctxRing[0]) / (len(a.ctxRing) - 1)
	if growth <= 0 {
		return 0, false
	}
	remaining := threshold - a.ctxTokens
	if remaining <= 0 {
		return 0, false
	}
	turns := (remaining + growth - 1) / growth
	if turns > etaHorizon {
		return 0, false
	}
	return turns, true
}

// ── NEGATIVE-SPACE SAFETY ───────────────────────────────────────────────────
//
// The approval posture is the one fact on this line that is drawn ONLY when it
// is unsafe. In the default posture — the gate asks before it runs anything —
// the segment does not exist, because a permanent "SAFE" badge is a badge
// nobody reads and therefore a badge that says nothing on the day it changes.
//
// ABSENCE IS THE SAFE STATE, and its presence is the whole message: "allow"
// means every tool call this session makes runs without asking, and a person
// who has forgotten they turned that on must be reminded by the screen rather
// than by the outcome.
func (a *app) yoloSegment() string {
	if a.approval == "allow" {
		return "YOLO"
	}
	return ""
}

// stateSegment is the last segment: what this surface is DOING, plain and
// painted.
//
//	idle                      dim       nothing is happening
//	⠹ working · 1m 4s         accent    the model has the turn, and for how long
//	waiting · your call       violet    IT HAS THE TURN AND IT IS YOURS
//	interrupted               soft red  the last turn was stopped by hand
//
// THE CLOCK IS THE ALIVENESS. A spinner says "something is happening" and says
// exactly as much at second one as at second ninety; the count-up is the only
// thing on the line that answers "should I still be waiting for this?". It is
// the tool rows' own count-up ([countUpWord]) and it turns on the tool rows'
// own grid ([spinnerStep]), so nothing on this screen beats against anything
// else.
func (a *app) stateSegment() (string, string) {
	word, painted := a.stateWord()
	if a.state != stateWorking || a.asking() || a.copy.on {
		return word, painted
	}
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		// A spinner read aloud is a word repeated forever (styles.go's linear
		// tier); the clock beside it is the fact it was standing in for.
		mark = glyphRunASCII
	}
	plain, line := mark+" "+word, a.pal.accent(mark)+" "+painted
	if clock := countUpWord(a.now().Sub(a.turnBegan)); clock != "" && !a.turnBegan.IsZero() {
		plain += " · " + clock
		line += a.pal.dim(" · ") + a.pal.accent(clock)
	}
	return plain, line
}

// plural spells a count's unit. The plural form is "s" unless a caller says
// otherwise, which "watch" does.
func plural(unit string, n int, form ...string) string {
	if n == 1 {
		return unit
	}
	if len(form) > 0 {
		return unit + form[0]
	}
	return unit + "s"
}

// contextSegment is what the conversation is CARRYING, and it reports whether
// that has got close enough to compaction to be painted.
//
//	12.4k/128k · 10%      the ordinary reading
//	842/128k              under one percent: the figure without a percentage
//	                      (empty)  nobody has said what the window is
//
// It leads with the tokens rather than the percentage because the two answer
// different questions and only one of them is answerable without the other. "How
// much am I carrying" is a fact about the conversation; "how much of the window
// is that" is a fact about the model, and it changes under a person's feet when
// they switch models without a single word being added. Both are on the line, in
// that order.
//
// The percentage is dropped entirely below 1% rather than shown as "0%" or "1%".
// A meter that reads 1% for the first twenty turns of a session is not a meter —
// it is what the byte-counting estimator this replaced actually did, and the
// figure it parked at was the only thing anybody ever read off it.
func (a *app) contextSegment() (string, bool) {
	if a.ctxTokens <= 0 || a.ctxWindow <= 0 {
		return "", false
	}
	segment := tokenWord(a.ctxTokens) + "/" + tokenWord(a.ctxWindow)
	if pct, ok := a.ctxPercent(); ok && pct >= 1 {
		segment += " · " + itoa(pct) + "%"
	}
	return segment, a.ctxCrowded()
}

// warmSegment is the session's cached share of everything it has sent, and what
// that share was WORTH — and it is empty until there is one.
//
//	⟲ saved $0.02 · 89% cached    a priced session: the cash, then the hit rate
//	⟲ 89% cached                  nobody published a price: the rate alone
//
// THE LAW IN ONE LINE: the percentage is the hit RATE, and the cash is what it
// MEANT. "⟲ 89%" alone was a number nobody could act on — a person reading it
// could not tell whether it was a good thing that had happened to them or a
// statistic about a mechanism they never asked about. The dollars are the
// answer, and they lead because money is the part a person recognizes on sight.
//
// The cash appears only when it is real (app.go's cacheSaved, which grows only
// where a prompt price AND a cache-read price were both published): a session on
// a model that publishes neither, or publishes only the first, keeps exactly the
// segment it had, rather than learning to say "saved $0.00" — or, worse, to
// count the whole prompt price as a saving the cache never made.
//
// It is a share rather than a count because a count of cached tokens says
// nothing on its own: 40k cached is excellent against 50k sent and a rounding
// error against 4M. The glyph is the same one the per-turn savings note opens
// with, so the running total and the line that explains one turn of it are
// visibly the same subject.
//
// Rounding is toward the honest side: 0% is shown when the share is real but
// tiny, because "there is a cache and it is barely hitting" is a different fact
// from the empty segment's "there is no cache accounting here at all".
func (a *app) warmSegment() string {
	share, ok := session.Usage{Input: a.inputTokens, CacheRead: a.cacheRead}.CachedShare()
	if !ok {
		return ""
	}
	// The word rides with the rate because the rate alone was the owner's own
	// stumble: two percentages share this line, and the one that means "of the
	// window" and the one that means "served from the cache" are told apart by
	// a word, not by position. It is the same word the per-turn note spends on
	// the same subject ("⟲ 9.8k cached"), so the running share and the turn
	// that explains it stay one vocabulary.
	rate := itoa(int(share*100)) + "% cached"
	if a.cacheSaved > 0 {
		return "⟲ saved " + savedWord(a.cacheSaved) + " · " + rate
	}
	return "⟲ " + rate
}

// stateWord is the state itself, plain and painted — what [app.stateSegment]
// wraps with the spinner and the clock.
//
// A pending question OUTRANKS the run state, and says so in words as well as in
// colour: the turn is technically still working — the tool call is parked
// inside the batch — but what is true about it that a person can act on is that
// it is waiting for them. "your call" rather than "your answer" because it is
// shorter and because it is what it is.
func (a *app) stateWord() (string, string) {
	// COPY OUTRANKS EVERYTHING, because it is the only state on this line that is
	// about the KEYBOARD rather than about the turn. While the viewport is frozen
	// the keys do something else entirely (copymode.go), and a status line that
	// said "idle" would be describing the session correctly and the screen
	// wrongly. The turn underneath keeps running; the row it would have claimed
	// is back the moment esc is pressed.
	if a.copy.on {
		word := a.copyWord()
		return word, a.pal.accent(word)
	}
	// A sweep's receipt outranks the run state for the seconds it stands: the
	// person's eye is on the status line asking exactly one question — did the
	// copy land — and the turn's own word is back the moment it expires
	// (dragselect.go).
	if word := a.dragWord(); word != "" {
		return word, a.pal.accent(word)
	}
	// A PROPOSAL IS THE SAME MOMENT AS A CONSENT QUESTION from this line's point
	// of view: the turn is technically working — the propose_task call is parked
	// inside it — and what is true about it that a person can act on is that it
	// is waiting for them (task.go).
	if a.asking() || a.awaitingTask() || a.awaitingStanding() || a.awaitingSubharness() {
		return waitingWord, a.pal.askBold(waitingWord)
	}
	word := a.state.String()
	switch a.state {
	case stateWorking:
		return word, a.pal.accent(word)
	case stateInterrupted:
		return word, a.pal.bad(word)
	default:
		return word, a.pal.dim(word)
	}
}

// waitingWord is the state a person has to answer.
const waitingWord = "waiting · your call"

// ── THE LEGEND: THE INPUT'S TOP BORDER, WITH THE CONVERSATION IN IT ─────────
//
// The rule above the input was one unbroken line whose only job was to say
// "below this is your business". It still says that, and it now carries the two
// facts that identify the conversation you are typing into, in the shape a form
// has used for fifty years — a fieldset legend, the label sitting in the border
// itself:
//
//	─ porting the parser · chat-v3-task* ─────────── / commands ─
//
// LEFT IS WHICH CONVERSATION THIS IS. It was the workspace path for a long
// time, and a path is the one fact on this frame a person already has: they
// typed the cd that got them here, the shell prompt behind this pane still says
// it, and every other pane they have open says the same abbreviated three
// letters. What they cannot recover from anywhere is WHICH of their
// conversations this pane is — so the session's own name leads, the name it
// gave itself from its first exchange (internal/session's title.go), read back
// as words by [app.sessionName]. The branch follows it with a "*" when the tree
// is dirty, which is the one bit of git state a person acts on without asking
// for more.
//
// THE PATH IS MOVED, NOT LOST. /status prints it in full, and the phone tier's
// sheet carries it under "place" with the branch beside it (statusdeck.go's
// [app.deckItems]). A path is a thing a person copies into another program,
// which is something you do from a note and not from a border.
//
// AN UNNAMED SESSION SAYS NOTHING WHERE THE NAME WOULD GO. The name lands one
// turn in, and THE EMPTINESS LAW governs the gap before it: the legend reads
// `─ chat-v3-task* ───` and never "untitled", never a placeholder. A session
// with neither a name nor a branch leaves the left end empty and the line is
// the plain rule it always was, with the hints still on its right.
//
// RIGHT IS WHAT THIS BOX ANSWERS TO. The affordance of the input line itself —
// "/ commands" — and it is here rather than in the status line for the reason it
// exists at all: it is about the thing directly below it.
// While a state has keys of its own the hints REPLACE them (see [app.hintWord]),
// because the two are the same slot answering the same question, and a
// cheatsheet beside a live prompt is a cheatsheet nobody reads.
//
// The narrow ladder drops in the order of what a person can recover elsewhere:
// the microcopy first (the keys still work whether or not they are printed),
// then the branch (the shell prompt behind this one says it) — and THE NAME IS
// CUT RATHER THAN DROPPED. It is the only thing on this line that can be eighty
// cells long (session's titleLimit), so it is what gives cells back, one
// ellipsis at a time, while the branch and the hints keep theirs. The last thing
// standing is the rule it always was.

// microcopy is the input's own affordance, and the legend's default right.
//
// IT NAMES ONE KEY AND IT USED TO NAME TWO. "@ files" was true — the completion
// still opens on "@" and always will (files.go, taskmention.go) — and it was
// still the wrong half to print, because the two keys are not the same KIND of
// thing. "/" opens a list of everything this surface can be told to do, which is
// the door a person who does not know what to press is looking for; "@" is a
// shortcut inside a sentence somebody is already writing, and a person writing a
// sentence about a file discovers it by typing the character that is already in
// their head. Printing both made the slot a two-item menu, and a two-item menu
// beside a live prompt is read once and then never again.
const microcopy = "/ commands"

// legend draws that border. It replaces the plain rule at every width, and
// degrades back into it when there is no room for anything else.
func (a *app) legend(width int) string {
	if width < 1 {
		return ""
	}
	// THE ONE MOMENT THE LEGEND MAY SHOUT: while a person is being asked
	// something, the place goes violet along with the state word. The question
	// is bottom-anchored and so is this border — the two of them framing the
	// question is the surface pointing at it with both hands (consent.go).
	paint := a.pal.dim
	if a.asking() || a.awaitingTask() || a.awaitingStanding() {
		paint = a.pal.ask
	}
	// THE LADDER, in the order of what a person can recover elsewhere. Each rung
	// is told its own room, because the left label is now BUILT to fit rather
	// than measured and rejected: the name is cut to whatever the frame leaves
	// it, and only when there is not even [legendNameFloor] worth of cells for it
	// is the hint slot spent instead — the keys it names keep working unprinted,
	// and which conversation this is is not written anywhere else on a frame
	// this narrow. A rung whose name did not survive is skipped rather than
	// drawn, which is what puts the hints on the block before the name.
	right := a.legendRight(width)
	attempts := make([]struct{ left, right string }, 0, 2)
	if left, named := a.legendLeft(width, legendRoom(width, right)); named {
		attempts = append(attempts, struct{ left, right string }{left, right})
	}
	bare, _ := a.legendLeft(width, legendRoom(width, ""))
	attempts = append(attempts, struct{ left, right string }{bare, ""})
	for _, attempt := range attempts {
		if line, ok := a.legendLine(attempt.left, attempt.right, width, paint); ok {
			return line
		}
	}
	return a.rule(width)
}

// legendGap is the shortest run of rule the two labels will leave between them.
//
// It is three rather than one because a name can now fill this line on its own:
// a label cut to the last cell leaves `… · chat-v3-task* ─ / commands ─`, where
// the single dash reads as two labels that collided rather than as a border
// with two labels set into it. Three cells is the least that still reads as a
// rule, and it is bought from the name, which is the thing that had too much to
// say in the first place.
const legendGap = 3

// legendRoom is how many cells one attempt leaves for its left label: the
// width, less the border's own two cells at the head, the space that separates
// the label from the fill, the gap, and the right label's tail.
//
// It is the arithmetic of [app.legendLine] read forwards instead of backwards,
// and it lives here because the left label now has to be BUILT to a budget
// rather than merely measured against one. Two functions computing the same
// number would drift; this is the one that computes it.
func legendRoom(width int, right string) int {
	tail := 0
	if right != "" {
		tail = ansi.StringWidth(right) + 3
	}
	return width - 3 - legendGap - tail
}

// legendLine lays one attempt out, and reports whether it fitted. The label
// sits one cell inside the border on each side, which is what makes it read as
// a legend rather than as text that collided with a rule.
//
// AN EMPTY LEFT IS A LINE, NOT A FAILURE. A session that has not named itself
// in a directory that is not a repository has nothing true to put at that end
// (the emptiness law), and the hint slot at the other end is the newcomer's
// only pointer at "/" — so the border draws from the frame's edge and the keys
// keep their place. Only an attempt with nothing at EITHER end is refused, and
// what answers that is the plain rule.
func (a *app) legendLine(left, right string, width int, paint func(string) string) (string, bool) {
	head := "─"
	if left != "" {
		head = "─ " + left + " "
	}
	tail := ""
	if right != "" {
		tail = " " + right + " ─"
	}
	fill := width - ansi.StringWidth(head) - ansi.StringWidth(tail)
	if (left == "" && right == "") || fill < 1 {
		return "", false
	}
	// WHERE THE DOOR LANDED, for the press that may follow. It is written HERE,
	// as the line is laid out, for the reason [app.statusPress] gives about the
	// model segment: a column read from anywhere else is a column from the
	// frame before this one. A right end that is not the door records nothing,
	// which is what makes the span its own answer to "was it drawn".
	a.homeDoor = hudSpan{}
	if strings.HasPrefix(right, homeDoorWord) {
		at := ansi.StringWidth(head) + fill + 1
		a.homeDoor = hudSpan{from: at, to: at + ansi.StringWidth(homeDoorWord)}
	}
	line := a.pal.dim("─")
	if left != "" {
		line = a.pal.dim("─ ") + paint(left) + a.pal.dim(" ")
	}
	line += a.pal.dim(strings.Repeat("─", fill))
	if tail != "" {
		// THE KEY IS THE PAYLOAD AND THE VERB IS THE PROSE (payload.go). This slot
		// is written in one grammar at every call site that fills it — `esc
		// interrupt`, `y allow · n deny · a always` — and until this wave both
		// halves were drawn at the dim value the border itself wears, so the chord
		// a person had to press was exactly as loud as the word explaining it. The
		// chord steps to ink; nothing else on the line moves, and the line is the
		// same number of cells it was.
		line += a.pal.dim(" ") + paintHint(right, a.pal, a.pal.dim) + a.pal.dim(" ─")
	}
	return line, true
}

// legendNameFloor is the fewest cells worth spending on a cut name. Below it
// the name is dropped entirely and the branch stands alone, because "po…" names
// no conversation — it is an ellipsis wearing two letters, and the cells it took
// said less than the branch they were taken from.
const legendNameFloor = 12

// branchWord is the branch as every surface writes it: its name, and a "*" when
// the tree has uncommitted work. An unknown branch is the empty string, which
// the emptiness law then draws as nothing wherever this is spent.
//
// It is one function because it is printed twice — here in the border and in the
// status sheet's "place" row (statusdeck.go) — and a dirty mark that appeared in
// one of those and not the other would be a person's answer to "is this tree
// clean" depending on which line they happened to read.
func (a *app) branchWord() string {
	if a.branch == "" {
		return ""
	}
	if a.branchDirty {
		return a.branch + "*"
	}
	return a.branch
}

// legendLeft is where this conversation is running: its branch and, on a remote
// session, its machine. The status line owns session identity, so the adjacent
// legend does not repeat the conversation name.
//
// THE MACHINE KEEPS ITS PLACE NOW THAT THE PATH HAS LOST ITS OWN. host.go's law
// is that a connection is shown as the place and nowhere else, and this end of
// the legend is that place: `devbox · porting the parser`. It is written as a
// SEGMENT rather than with the path's colon, because `devbox:` in front of a
// sentence of English is scp syntax pointed at something nobody can copy. It is
// never cut, for the reason the path never cut it either — which machine is the
// half of the answer a person cannot reconstruct from anything else on screen.
//
// THE TIGHT FRAME DROPS THE BRANCH. The status line below keeps identity, and a
// branch a person can recover from the shell prompt does not outrank it.
func (a *app) legendLeft(width, room int) (string, bool) {
	// THE PLACE IS THE ROOM while one is open, and the name and branch go with
	// the path: none of them is a fact about the page on screen, and the one
	// thing a person in here needs from this slot is the key that gets them out
	// (room.go). The task's own title is on the status row two lines down, where
	// a room renames the identity cluster ([app.identityParts]).
	if a.roomOpen() {
		// AND WHILE A HISTORY WALK IS ON IT SAYS WHAT ESC ACTUALLY DOES, which for
		// those few keystrokes is not "main": the walk is dismissed first and the
		// person's own draft comes back (room.go's [app.roomKey], recall.go). The
		// slot is here to promise the NEXT keystroke, so it has to move with it.
		if a.recalling() {
			return roomLegendRecallWord, true
		}
		return roomLegendWord, true
	}
	if room < 1 {
		return "", true
	}
	branch := a.branchWord()
	if width < hudTight {
		branch = ""
	}
	return fit(dotted(a.host, branch), room), true
}

// legendJoin is the separator between the legend's facts, and dotted threads any
// number of them onto it while skipping the ones that are not there — which is
// the emptiness law spelled as a function, since a missing branch must leave no
// dangling "·" behind it.
const legendJoin = " · "

func dotted(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, legendJoin)
}

// placePath is the workspace, abbreviated at one of three strengths — and, on
// a session that is running on another machine, the machine's name in front of
// it: `devbox:~/code/app`.
//
// IT IS NO LONGER ON THE LEGEND. The path is on the status sheet's
// "place" row and in /status (statusdeck.go, statusnote.go), which is where a
// path a person copies belongs. The three strengths are kept because the sheet
// is a forty-four-column page.
//
// THE HOST IS NOT CUT WITH THE PATH. On a remote session WHICH MACHINE is the
// half of the answer a person cannot reconstruct from anything else — the path
// they might recognize, the host they would have to remember. So the
// abbreviation eats the path and leaves the name. The home abbreviation is still
// this machine's home, which is why a remote path rarely collapses to `~`: it is
// the far machine's home and nobody here knows it.
func (a *app) placePath(hard int) string {
	return a.hostedPath(a.placeWord(shortPath(a.workspace, a.tilde, hard)))
}

// legendRight is the hint slot: the state's own keys when it has any, the
// input's two affordances when it does not, and nothing at all on a tight
// frame — where the cells are worth more to the conversation's name than to a
// reminder.
func (a *app) legendRight(width int) string {
	if width < hudTight {
		return ""
	}
	if hint := a.hintWord(); hint != "" {
		return hint
	}
	// THE IDLE SLOT CARRIES BOTH DOORS. `/ commands` is recoverable a dozen
	// other ways — the manual, /help, typing a slash — and home, until this
	// line existed, was recoverable only by knowing it was there. So the rest
	// state of the slot names them both, and neither costs a row: this is the
	// legend, which is on the frame either way (home.go).
	if a.homeDoorShowing() {
		// AND THE WAY BACK, when there is one. `tab last` is absent whenever this
		// terminal holds only one conversation, which is the emptiness law again:
		// a key that cannot act says so by not being advertised (keeper.go's
		// [app.lastConversation]). It sits between the two doors because it is
		// the same kind of thing — somewhere else to be — and it is dropped first
		// when the slot is tight, by [app.homeDoorShowing]'s own rule about the
		// box being empty.
		if _, ok := a.lastBehind(); ok {
			return homeDoorWord + " · " + lastDoorWord + " · " + microcopy
		}
		return homeDoorWord + " · " + microcopy
	}
	return microcopy
}

// lastDoorWord advertises the key back to the conversation before this one. It
// is the shortest true sentence about it: `tab` is the key, and `last` is what
// it goes to — the conversation you were in last, which is the same promise
// `cd -` makes.
const lastDoorWord = "tab last"

// ── CONTEXTUAL KEY HINTS ────────────────────────────────────────────────────
//
// THE HINT SLOT IS STATE-DRIVEN, AND IDLE IT IS EMPTY. This surface used to
// carry "/help · ctrl+o" permanently, which is the definition of a static
// cheatsheet: two keys that were true in every state, drawn in the one place a
// person looks when they do not know what to do, and therefore never read after
// the first session.
//
// What replaces it is a slot that only ever names the keys that WORK RIGHT NOW:
//
//	the pointer is theirs drag to select · any key ends it
//	the door is armed     ctrl+c again to quit · a task will stop  (quitarm.go)
//	the picker is open    enter switch · esc · crew max
//	the sessions are up   enter open · esc
//	copy mode is on       v select · a block · y yank · esc
//	rewind is armed       esc again to rewind        (rewind.go's double esc)
//	rewind mode is up     nothing — the mode bar prints its own keys
//	the welcome box is up ↑↓ recent · enter open
//	a path is completing  tab take · enter run · esc
//	a list is open        ↑↓ · enter · esc
//	a proposal is up      y yes · r redirect · n no    (task.go's own keys)
//	a question is up      a allow · t always · d deny  (consent.go's own keys)
//	a room is open        x stop, or ↑↓ history mid-walk  (room.go's [app.roomHint])
//	a turn is running     esc interrupt
//	the column is away    ctrl+g tasks               (task.go's [railBackHint])
//	idle                  nothing
//
// The keys are quoted from the handlers rather than authored here — a hint that
// disagrees with input.go is worse than no hint, because it is a hint somebody
// will act on.
//
// AND THE ORDER IS input.go's OWN ROUTING ORDER, top to bottom, because that is
// the only thing that makes the slot true: what a key does is decided by which
// handler reads it first, so a hint ranked any other way is a hint that names
// the keys of a state the keyboard has already been taken away from. THE ARMED
// DOOR LEADS, under only the pointer handover, because ctrl+c is read above
// every modal on this surface — leaving is never modal — so while it is warm the
// next keystroke's meaning is settled before any picker or panel gets a say.
// Copy mode is the rung under those; the typed lists come last of the modal
// ones, since they take only the four keys that move and commit a list and give
// every other one back to the draft.
//
// COPY MODE IS THE ONE THIS SLOT WAS MOST WRONG ABOUT. While the viewport is
// frozen every key on this surface means something else, and the slot was
// drawing "@ files · / commands" — two affordances of a box the keyboard is not
// currently pointed at. A hint naming keys that do nothing is the failure mode
// this slot exists to prevent, and it was the default state of it.
func (a *app) hintWord() string {
	switch {
	case a.released:
		// IT LEADS BECAUSE THE HANDOVER IS READ FIRST ([app.key] takes the pointer
		// back above every overlay), and because it is the one state on this slot
		// that a person cannot see any other way: the pointer being somewhere else
		// looks exactly like the pointer being broken until a line says otherwise.
		return "drag to select · any key ends it"
	case a.quitArmed():
		// AND THE ARMED DOOR RANKS DIRECTLY UNDER IT, above every modal on this
		// slot, because that is where ctrl+c is READ: the routing order is this
		// slot's ordering law, and ctrl+c is excepted from every picker, panel,
		// room and paste bracket in input.go — leaving is never modal. So while
		// the door is warm, the next keystroke's meaning is settled before any
		// of the states below get a say in it, and a slot that named their keys
		// instead would be promising the wrong thing about the one key the
		// person has already pressed once.
		//
		// The sentence names what leaving would STOP when anything is running
		// (quitarm.go's [app.quitHint]), and nothing at all when nothing is.
		return a.quitHint()
	case a.pick.open:
		// AND THE CREW IS NAMED BESIDE THE KEYS, because this list is where a
		// person lands when the crew they just set did not change anything they
		// can see. The status line's model readout is the conversation's model,
		// which /crew never touches by design — so somebody who typed `/crew max`
		// opens /model hunting for the change, and the one word this slot can
		// afford tells them the crew is a separate thing that is already set.
		// The picker's rows are the list itself and are reused whole inside the
		// settings panel ([picker.rowsOwned]), so it has no header or foot of its
		// own to spend on a sentence; this slot is the line that is already there.
		// It says nothing on a door with no profile ([app.crewHint]).
		if crew := a.crewHint(); crew != "" {
			return "enter switch · esc · " + crew
		}
		return "enter switch · esc"
	case a.crewPick.open:
		return "↑↓ · enter apply · esc"
	case a.roster.open:
		return "enter open · esc"
	case a.shelf.open:
		// The deliverables picker names its verbs here as well as in its filter
		// box, because they are the half of this list nobody can guess: two of
		// the three are chords (deliverables.go).
		if a.shelf.dest != nil {
			return filesCopyVerbs
		}
		return filesVerbs
	case a.standPage.open:
		// The standing page names its verbs here because they are the half of it
		// nobody can guess: three of the four are bare letters, and one of them
		// stops a thing for good (standingpage.go).
		return standPageVerbs
	case a.subPage.open:
		// /subharness names its verbs here PER ROW, because enter means two
		// things on the intake card — fill this field in, or start the run — and
		// one line saying "enter" for both would be teaching nobody
		// (subharness.go).
		return a.subVerbs()
	case a.copy.on:
		return "v select · a block · y yank · esc"
	case a.rew.on:
		// The rewind mode prints its own keys in the bar that replaced the draft
		// box (rewind.go), and a slot repeating them would be the surface saying
		// the same thing twice on one screen.
		return ""
	case a.rewindArmed() && a.rewindReady():
		// The first esc has landed and the second one means something else for
		// half a second. This outranks "esc interrupt" below for exactly that
		// reason: while the window is open, that is no longer what the key does.
		//
		// It asks [app.rewindReady] as well as the clock, because the two can come
		// apart: a question can be raised in the half second the window is open,
		// and from that moment esc belongs to the question. The slot promises what
		// the NEXT esc does, so it has to ask the same thing that key will.
		return rewindArmWord
	case a.rewindSaying():
		return a.rewSay
	case a.welcome.open:
		// The box reads two keys and hands back the rest (welcome.go), and the
		// arrows only mean the list while the draft is empty — which is exactly
		// when this hint is worth drawing.
		if !a.input.empty() || len(a.welcome.recent) == 0 {
			return ""
		}
		return "↑↓ recent · enter open"
	case a.comp.open && a.comp.arg:
		// A path completing under a command argument: tab takes the row, and
		// enter belongs to the LINE rather than to the list (input.go).
		return "tab take · enter run · esc"
	case a.menu.open || a.comp.open:
		return "↑↓ · enter · esc"
	case a.awaitingTask():
		// The proposal owns these keys while it is up, and it owns them ahead of
		// the consent letters below: a card and a consent question cannot be open
		// at once, and the keys a person needs are the ones on screen (task.go).
		return taskProposalHint
	case a.awaitingStanding():
		// And the standing card owns the digits it drew — three, or two on a
		// one-off reminder, and the follow-up's two the moment the yes is given
		// (standing.go).
		return standAskHint(a.stand)
	case a.shaping():
		// The always is part-way answered and the block is on its second beat
		// (consent.go): the numbers bank a shape and esc puts the question back.
		return "1-3 shape · esc never mind"
	case a.asking() || a.awaitingDecision():
		// THE KEYS THE BLOCK ACTUALLY DRAWS. This line said "a allow · t always"
		// for a year after the answers took their own first letters, so the hint
		// under the box named `a` as allow while the block above it named `a` as
		// always — one keystroke, two readings, and the wrong one widens a
		// permission.
		return "y allow · n deny · a always"
	case a.railHold:
		// The roster has the keyboard (ctrl+t, task.go) — the one state on this
		// surface where the arrows have left the box entirely. It ranks HERE, under
		// every overlay and both questions, because that is exactly where
		// [app.railKey]'s guard stands down; and above the running turn, because
		// while it is held esc gives the keyboard back rather than interrupting.
		return railHoldHint
	case a.roomOpen():
		// A ROOM IS READ HERE because that is where [app.roomKey] stands in the
		// program loop (app.go): under the stop card and the roster, over
		// everything the draft would have got. And it ranks ABOVE the two lines
		// below for the reason this whole slot is ordered the way it is — while a
		// room is open, esc leaves the page and does not touch the conversation's
		// turn, so "esc interrupt" would be naming a key that is spoken for.
		return a.roomHint()
	case len(a.parks) > 0:
		// A MESSAGE IS WAITING FOR THIS ANSWER, and while it is, esc does one
		// more thing than it did: it stops the turn AND sends what is parked
		// (park.go). It outranks the plain interrupt below for the reason the
		// armed rewind outranks it — the slot promises what the NEXT esc does,
		// and that is no longer only a stop. It is spelled exactly as the block's
		// own dim line spells it, so the two lines on one screen agree.
		return parkedHint[1]
	case a.state == stateWorking:
		return "esc interrupt"
	case a.spell.asking:
		// THE EXPANSION IS OUT. The slot the chord was named in is where the
		// spinner for it belongs — the person pressed a key at the end of this
		// line and this is the line answering (spellout.go). It ranks above the two
		// offers below because it is not an offer: it is something happening.
		return a.spellWorkingWord()
	case a.slashTagHint() != "":
		// A LIVE TAG OWNS ENTER, so its line outranks the two optional chords
		// below. A HINT MAY ONLY NAME A KEY THAT WORKS, and exactly one tag is the
		// only state where enter has the promised alternate meaning.
		return a.slashTagHint()
	case a.standMarkOffered():
		// THE DRAFT LOOKS LIKE A CONDITION, so the slot says the chord that makes
		// it one (standmark.go). It ranks HERE — under the running turn, over the
		// column's own line — for this slot's ordering law: the chord is read at
		// the bottom of [app.key]'s plain switch, so every state above has already
		// taken the keyboard, and esc while an answer is streaming is a key the
		// person is far more likely to want next.
		//
		// It costs no rows. The legend is on the frame in every state, and this is
		// the slot it already carries — so a draft that starts looking like a rule
		// changes one word at the end of a line and moves nothing.
		return standMarkHint
	case a.spellOffered():
		// AND THE DRAFT LOOKS LIKE SOMETHING TO BUILD, with room left to say what
		// it means, so the slot offers to spell it out (spellout.go). It ranks
		// directly UNDER the standing hint because these two are the only lines
		// here that are about the sentence being typed rather than about a state
		// the surface is in — but the sharing itself is decided in
		// [app.spellOffered], which answers no while the standing hint is up, so
		// this ordering is a statement of the same rule and never a second one.
		//
		// It costs no rows, for the reason the line above it costs none: this is
		// the legend, which is on the frame in every state.
		return spellOutHint
	case a.railAway && a.railAvail():
		// THE COLUMN IS AWAY AND THIS SESSION HAS RUN SOMETHING (task.go's
		// [app.railStow]). It ranks LAST, under every state above it, because it is
		// the only line here that is not about the next keystroke — it is where the
		// work went, said in the one slot a person looks at when they cannot see
		// something they know exists.
		//
		// AND IT IS THE HALF THE STRIP CANNOT SAY. The chips above the conversation
		// draw what is RUNNING and nothing else, so a session whose work has all
		// landed has a roster full of results and no sign on the frame that it is
		// there. This is that sign. With nothing run at all it stays quiet — the
		// column a person closed was empty, ctrl+g still brings it back, and a
		// standing hint about a roster of nothing is the emptiness law broken in
		// the one slot a person reads most.
		return railBackHint
	}
	return ""
}

// awaitingDecision reports whether a call is parked on a person. It is the same
// hint as an open question because it is the same moment: the request may not
// have reached this surface yet, and the row is already showing the "?".
func (a *app) awaitingDecision() bool {
	for i := range a.entries {
		if a.entries[i].kind == entryTool && a.entries[i].status == toolConsent {
			return true
		}
	}
	return false
}

// ── THE PATH, FISH-STYLE ────────────────────────────────────────────────────
//
// shortPath abbreviates a directory the way fish's prompt does, at one of three
// strengths:
//
//	hard 0   ~/s/aforge-v2     home to "~", every parent to its initial
//	hard 1   …/aforge-v2       the parents dropped entirely
//	hard 2   aforge-v2         the place, alone
//
// The LAST SEGMENT IS ALWAYS WHOLE, at every strength. It is the only part of
// the path that answers the question the legend is for — which project is this
// pane — and a rule that abbreviated it would be a rule that saved cells by
// deleting the message. A leading dot is kept with the letter after it (".c"
// for ".claude"), because a lone "." is not a name.
func shortPath(dir, home string, hard int) string {
	dir = strings.TrimRight(strings.TrimSpace(dir), "/")
	if dir == "" {
		return ""
	}
	if home = strings.TrimRight(home, "/"); home != "" {
		if dir == home {
			return "~"
		}
		if strings.HasPrefix(dir, home+"/") {
			dir = "~" + strings.TrimPrefix(dir, home)
		}
	}
	parts := strings.Split(dir, "/")
	last := parts[len(parts)-1]
	switch {
	case hard >= 2 || len(parts) == 1:
		return last
	case hard == 1:
		return glyphMore + "/" + last
	}
	for i, part := range parts[:len(parts)-1] {
		parts[i] = initialOf(part)
	}
	return strings.Join(parts, "/")
}

// initialOf is one path segment, cut to what identifies it: its first rune, or
// the first two when the first is a dot.
func initialOf(segment string) string {
	runes := []rune(segment)
	switch {
	case len(runes) == 0:
		return ""
	case runes[0] == '.' && len(runes) > 1:
		return string(runes[:2])
	default:
		return string(runes[:1])
	}
}

// wrap breaks a block of plain text to width, keeping its own newlines. The
// text is unstyled at this point: styling after wrapping is what keeps every
// width measurement honest.
func wrap(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	text = strings.ReplaceAll(text, "\t", "    ")
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	return out
}

// noteBlockLines is [wrap]'s opposite number for a block whose own line
// structure is the meaning: the text's own lines, each one FITTED to the width
// and none of them re-flowed.
//
// It is the same shape a fenced block already gets (markdown.go says why in the
// same words: indentation is content, and a paragraph filler does not know it).
// The difference is only which door the text came through — a card printed into
// the conversation by a slash command arrives as a note rather than as prose
// from the model.
func noteBlockLines(text string, width int) []string {
	text = strings.ReplaceAll(text, "\t", "    ")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, fit(line, width))
	}
	return out
}

// fit truncates to a printable width, or returns nothing at all when there is
// no room — a one-cell ellipsis in a one-cell gap says less than a space.
func fit(s string, width int) string {
	fitted, _ := fitWidth(s, width)
	return fitted
}

// fitWidth is fit, and what the result measures.
//
// Callers that lay a row out column by column need both, and measuring a
// string is a grapheme walk — the most expensive thing this surface does per
// character. fit already measures the string to decide whether it fits, so a
// caller that then measures the answer itself pays for the same walk twice.
// Only the truncating branch has to measure again, because ansi.Truncate
// stops at the last cluster that fits and can land under the budget.
func fitWidth(s string, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	if measured := ansi.StringWidth(s); measured <= width {
		return s, measured
	}
	cut := ansi.Truncate(s, width, glyphMore)
	return cut, ansi.StringWidth(cut)
}

// The meter's alphabet: what is left, and what has been spent. Both are from
// the block family, so the bar reads as one object rather than as a row of
// glyphs — and both have an ASCII stand-in, because a terminal that cannot draw
// them would otherwise render a countdown as replacement characters.
const (
	meterFull       = "█"
	meterSpent      = "░"
	meterFullASCII  = "#"
	meterSpentASCII = "-"
)

// progress draws a meter of exactly cells glyphs, of which frac is still full.
//
// IT IS A PROPORTION AND NEVER A COUNT: the caller passes what is LEFT, between
// 0 and 1, and the bar says how much of the whole that is. The paint follows the
// same split — the remainder in the question hue, the spent part dim — so what
// is left is the part of the row that is lit.
//
// The last sliver survives rounding: any fraction above zero keeps one cell,
// because an empty bar is the meter saying the thing has already happened, and
// it must not say that a tenth of a second early.
func (a *app) progress(frac float64, cells int) string {
	if cells < 1 {
		return ""
	}
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	full, spent := meterFull, meterSpent
	if a.pal.ascii {
		full, spent = meterFullASCII, meterSpentASCII
	}
	left := int(frac*float64(cells) + 0.5)
	if left > cells {
		left = cells
	}
	if left == 0 && frac > 0 {
		left = 1
	}
	return a.pal.ask(strings.Repeat(full, left)) + a.pal.dim(strings.Repeat(spent, cells-left))
}

// trimBlanks drops leading and trailing empty rows from a block. It is the
// other half of the spacing law: a block that ends in a newline must not hand
// the layout a gap it did not ask for, because the layout would keep it.
func trimBlanks(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(rows[0]) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
