package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// ── THE LIVE TOKEN COLUMN ───────────────────────────────────────────────────
//
//	▸ Working · ctrl+e                                       ↑ 63.6k  ↓ 12
//	  reading 2 files in internal/tui3
//	· running go test ./internal/session · 41s              ↑ 78.2k  ↓ 486
//
// THE DEFECT THIS FIXES: a running turn draws one still line, and that line
// claims exactly as much at second one as at second forty. The shimmer says
// "alive"; the caption says what the work is about; neither says whether
// anything is MOVING — and a person watching a slow endpoint, a long think or a
// stalled stream cannot tell the three apart. The pulse already learned to name
// what it is waiting for (render.go's [app.waitingWords]); this is the other
// half of the same question, and it is the half a number answers better than a
// sentence: how much has gone up, how much has come back, right now.
//
// WHAT THE TWO FIGURES MEAN, AND WHERE EACH OF THEM COMES FROM.
//
//	↑  what this work has SENT.     The books' input, floored by what one
//	                                request weighs where the page knows it — so
//	                                the conversation has a figure from the first
//	                                frame rather than from the first step that
//	                                lands, and it steps up as each tool result
//	                                joins what goes back out.
//	↓  what this work has RECEIVED. The books' output, floored by an estimate of
//	                                the bytes already on the page — so it moves
//	                                while the model writes rather than once a
//	                                step, and the exact figure takes over the
//	                                moment a step reports one.
//
// BOTH ARE THE MAX OF A BOOK AND AN ESTIMATE, which is not a hedge — it is the
// shape [session.Agent.ContextTokens] already uses for the same reason: the
// provider's count is the honest number for what has been SETTLED and knows
// nothing about what has happened since, and the content estimate is the honest
// floor for the part nobody has been billed for yet. The books are never moved
// by anything here; the drawn figure is the larger of the two readings.
//
// ONE COLUMN, EVERY PAGE THAT STREAMS. The state lives on [feed] — the ONE
// reducer both the conversation and a task room grow their transcripts with
// (feed.go, and docs/design/lens/DESIGN.md's Decision 1 on why there is not a
// second one) — so a node's page draws the same column by the same code, with
// its own books off its own lane. The conversation's books are the session's,
// turn-scoped (app.go's [app.take]); a room's are the sum of the turns its lane
// has heard (room.go's [app.roomEvent]), which is exactly how its bill is
// counted ([taskNode.liveCost]). Where a page's lane has heard nothing — a node
// that was already running when the room opened — the column runs on the page's
// own bytes alone, which is the emptiness law and not a gap: ↑ is absent until
// it is known, and ↓ is the floor that is true. Every page reaches the drawing
// through [deck.col]; a page with no column to carry (a run's read-only
// transcript, roomorch.go) leaves it nil and draws nothing.
//
// A ROW CARRIES THE FIGURES OF THE WORK IT STANDS FOR. That is the whole of the
// hierarchy and there is no other rule in it:
//
//   - the compact block's live row, and the door that replaces it when the
//     block is open, stand for THE WHOLE RUNNING TURN, and carry the pair
//     (livesteps.go);
//   - a step's own caption row, drawn inside an opened block, stands for THAT
//     STEP, and carries only ↓ — what the model wrote inside it (caption.go).
//     A step's share of what went UP is not attributable: a request carries the
//     whole conversation, not the step it happens to be in, and a figure that
//     divided it between steps would be arithmetic nobody performed.
//
// A ROW THAT HAS FINISHED CARRIES NOTHING. The column is a sign of motion, so
// it lives exactly as long as the motion does: a settled step drops its figure,
// and the whole column goes with the turn — this is the owner's ruling and it is
// why the pair is drawn from turn-scoped figures rather than from the session's
// totals, which the status line already carries and which never go away.
//
// IT IS SPARE CELLS AND NEVER A RESERVATION. The words are the point of every
// row it lands on, so the column takes what is left at the right edge after the
// sentence has taken what it needs, exactly as the step clock does beside it
// (steptime.go) — no rewrap, no ellipsis, no fourth row. On a frame too narrow
// to hold both, the sentence wins and the column is simply absent.
//
// IT IS THE MARGIN'S INK, NOT THE PAYLOAD'S. The first cut painted the figures
// in the datum hue and they shouted: a cyan number at the right edge outranked
// the sentence it sat beside, which is the hierarchy upside down — the words
// are what the row is for and the figures are telemetry about them. A FIGURE
// THAT MOVES DOES NOT ALSO NEED TO BE BRIGHT. Motion is salience; spending
// brightness on top of it is what made the column loud. So the pair wears the
// dim tier every other right-edge fact on this surface wears (`41s`, `189
// lines`, `⠋ 2s / 30s`), and the arrow sits one stop under its figure so the eye
// lands on the number and not on the symbol. Scope is not carried by brightness
// either: a turn's row has two arrows and a step's row has one, which is a
// difference in what is said rather than in how loudly.
//
// AND THE FIGURES EASE, on reveal.go's clock and curve, because THE ARRIVAL
// SHAPE IS NOT THE DRAWING SHAPE: output arrives in lumps and the books land a
// step at a time, and a figure that jumped four thousand tokens between two
// frames reads as a glitch where the same four thousand walked reads as work.
// The books stay exact — nothing here is rounded into them — and the drawn pair
// snaps the instant the work stops, along with every other meter on the surface.
// The linear tier never paces one (tui3.go's Options.Linear).

const (
	// tokenUpGlyph and tokenDownGlyph are the two directions, and they are the
	// PLAIN arrows deliberately. ⇡ is spoken for on this surface — it is the
	// boosted mark (internal/tui2/tokens' GlyphBoosted) — and a second meaning
	// for one glyph is a glyph that has stopped meaning either. The plain pair
	// is also one cell wide in every font a terminal is likely to have, which
	// the barbed and doubled spellings are not.
	tokenUpGlyph   = "↑"
	tokenDownGlyph = "↓"
	// tokenUpPlain and tokenDownPlain are the same two directions where a glyph
	// cannot be trusted — the ascii floor and the screen-reader tier.
	tokenUpPlain   = "^"
	tokenDownPlain = "v"
	// tokenColGap separates the two halves of the column, and it is SPACE rather
	// than this surface's " · ". The dot joins facts in a list (toolview.go's
	// [app.joinTail] says so where the list is); these two are one fact said in
	// two directions, and a dot between them would read as a third figure.
	tokenColGap = "  "
	// tokenColClear is the least space between the sentence and the column. Two
	// cells, because a figure run up against a word reads as part of the word.
	tokenColClear = 2
)

// tokenCol is one page's column: its books, its floor, and the pair in motion.
// It lives on [feed] so that every page grown by the reducer has one.
type tokenCol struct {
	// up and down are THE BOOKS for the work this page is showing — what its
	// lane has been told was billed — and nothing here ever moves them.
	up, down int
	// weight is the floor under ↑ where the page knows what one request
	// weighs: the conversation's context (app.go's [app.measureContext]). A
	// room knows no such thing and leaves it at zero.
	weight int
	// shownUp and shownDown are the pair in motion (the walk below).
	shownUp, shownDown int
}

// open is the column at the start of a piece of work: every figure at nothing.
// A pair left standing at the last turn's totals would spend the first second
// of this one walking DOWN in front of somebody.
func (c *tokenCol) open() { *c = tokenCol{} }

// reading is the exact pair — books floored by what the page can see — given
// how many bytes of the model's own writing are on the page.
//
// Zero is "nothing is known", not "nothing happened", and the emptiness law
// takes it from there: a figure that has not been earned yet is not drawn.
func (c *tokenCol) reading(written int) (up, down int) {
	up, down = c.up, c.down
	if c.weight > up {
		up = c.weight
	}
	if est := session.EstimateTokens(written); est > down {
		down = est
	}
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	return up, down
}

// tick walks the drawn pair toward the reading, on the slots [app.tickReveal]
// measured — the same clock and the same curve as every other figure in motion
// on this surface — and reports whether anything moved.
//
// IT CHASES WITHOUT BEING ARMED, and that is the difference between this pair
// and the status line's meters. Those move when a reading LANDS, so something
// has to arm them at the landing ([app.armMeters]); ↓ moves as bytes arrive,
// which is most frames of a streaming turn, and a flag raised and lowered thirty
// times a second would be bookkeeping in place of a rule. The rule is simply
// that the pair follows the figures for as long as the work runs, and snaps to
// them the moment it does not.
func (c *tokenCol) tick(written, slots int, snap bool) bool {
	up, down := c.reading(written)
	if snap {
		moved := c.shownUp != up || c.shownDown != down
		c.shownUp, c.shownDown = up, down
		return moved
	}
	nextUp, nextDown := easeInt(c.shownUp, up, slots), easeInt(c.shownDown, down, slots)
	moved := nextUp != c.shownUp || nextDown != c.shownDown
	c.shownUp, c.shownDown = nextUp, nextDown
	return moved
}

// drawn is the pair the frame paints: the eased readings while the work runs
// on a tier that paces, the exact ones everywhere else. A restore, a settle and
// the screen-reader tier take the exact figure for reveal.go's stated reason —
// nobody is watching those numbers grow.
func (c *tokenCol) drawn(written int, running, linear bool) (up, down int) {
	if !running {
		return 0, 0
	}
	if linear {
		return c.reading(written)
	}
	return c.shownUp, c.shownDown
}

// turnWritten is how many bytes of the model's own writing the RUNNING TURN
// has put on this page — its prose and its reasoning, which the provider bills
// as one thing.
//
// It walks BACK from the end and stops at the first entry belonging to another
// turn, so it costs the length of the running turn rather than the length of
// the transcript, and it is a sum of lengths rather than of strings: nothing
// here copies a byte.
func (f *feed) turnWritten() int {
	total := 0
	for at := len(f.entries) - 1; at >= 0; at-- {
		e := &f.entries[at]
		if e.turn != f.turn {
			break
		}
		if e.kind == entryAssistant || e.kind == entryThinking {
			total += len(e.text)
		}
	}
	return total
}

// stepWritten is the same count over ONE STEP's span — what the model wrote
// inside a single caption (caption.go's [caption] carries the bounds).
func stepWritten(c caption, es []entry) int {
	from, to := c.start, c.end
	if from < 0 {
		from = 0
	}
	if to > len(es) {
		to = len(es)
	}
	total := 0
	for at := from; at < to; at++ {
		if es[at].kind == entryAssistant || es[at].kind == entryThinking {
			total += len(es[at].text)
		}
	}
	return total
}

// tickTokenCol is one turn of every column on the surface, from the reveal
// clock (reveal.go's [app.tickReveal]). The conversation's column follows the
// session's turn while the surface is working; a room's follows its node while
// the node runs. Each marks its own page dirty when a figure moved, because the
// two pages keep separate row caches.
func (a *app) tickTokenCol(slots int, snap bool) {
	working := a.state == stateWorking
	// THE CONVERSATION'S FLOOR IS TAKEN HERE, ON THE CLOCK, rather than at the
	// weight's own door: [app.measureContext] runs where the weight changes and
	// the column is a reader of it, not a second owner.
	a.col.weight = a.ctxTokens
	if a.col.tick(a.turnWritten(), slots, snap || a.linear || !working) {
		a.dirty = true
	}
	if a.room == nil {
		return
	}
	if a.room.col.tick(a.room.turnWritten(), slots, snap || a.linear || !a.room.running()) {
		a.room.dirty = true
	}
}

// tokenColumnOn reports whether this deck may draw the column at all: it has
// one to carry, and the work it shows is still running.
func (a *app) tokenColumnOn(d deck) bool {
	return d.col != nil && d.runningTurn != 0
}

// tokenFigure is one half of the column — the arrow and its figure — painted,
// and the cells it takes.
//
// THE FIGURE IS DIM AND THE ARROW IS DIMMER. The number is what the row came to
// say and it wears the margin's tier ([palette.dim]), like every other fact at
// the right edge of a row on this surface; the arrow is a label on it and takes
// the fade ramp's faintest stop ([palette.fade]), which comes back dim on the
// terminals that cannot fade. Nothing in the column is ever brighter than the
// sentence it sits beside.
func (a *app) tokenFigure(glyph, plain string, n int) (string, int) {
	if n <= 0 {
		return "", 0
	}
	mark := glyph
	if a.pal.ascii || a.linear {
		mark = plain
	}
	word := tokenWord(n)
	return a.pal.fade(mark, 0) + a.pal.dim(" "+word), ansi.StringWidth(mark) + 1 + ansi.StringWidth(word)
}

// tokenColumn is the pair as a person reads it, and the cells it takes.
//
// IT DROPS A WHOLE FIGURE RATHER THAN CLIPPING ONE, and ↑ goes first. Both are
// the same rule the tool row's column is written to (toolview.go's [app.tailOf]):
// half a number is a number a person has to distrust, and of these two ↓ is the
// one that answers the question the column exists for — something is coming
// back — while ↑ is the context around it.
func (a *app) tokenColumn(up, down, budget int) (string, int) {
	upText, upCells := a.tokenFigure(tokenUpGlyph, tokenUpPlain, up)
	downText, downCells := a.tokenFigure(tokenDownGlyph, tokenDownPlain, down)
	gap := len(tokenColGap)
	switch {
	case upCells > 0 && downCells > 0 && upCells+gap+downCells <= budget:
		return upText + tokenColGap + downText, upCells + gap + downCells
	case downCells > 0 && downCells <= budget:
		return downText, downCells
	case upCells > 0 && upCells <= budget:
		return upText, upCells
	}
	return "", 0
}

// tokenSuffix is the column laid against the RIGHT EDGE of a row: the space
// between the sentence and the figures, and the figures — ready to append to a
// line already `used` cells wide inside `room`. It is "" when the row cannot
// afford the column, which is the only thing that ever happens on a narrow
// frame: the sentence is never shortened for it.
func (a *app) tokenSuffix(up, down, used, room int) string {
	budget := room - used - tokenColClear
	if budget < 1 {
		return ""
	}
	text, cells := a.tokenColumn(up, down, budget)
	if cells == 0 {
		return ""
	}
	pad := room - used - cells
	if pad < tokenColClear {
		return ""
	}
	return strings.Repeat(" ", pad) + text
}

// turnTokenSuffix is the pair for a row that stands for the WHOLE RUNNING TURN.
// Every caller hands in the room it is laying out in and the cells its own
// sentence has already spent.
func (a *app) turnTokenSuffix(d deck, used, room int) string {
	if !a.tokenColumnOn(d) {
		return ""
	}
	up, down := d.col.drawn(turnWrittenOf(d), true, a.linear)
	return a.tokenSuffix(up, down, used, room)
}

// turnWrittenOf is [feed.turnWritten] asked of a deck, which knows its own
// entries and which turn is running but not the feed they came from.
func turnWrittenOf(d deck) int {
	total := 0
	for at := len(d.entries) - 1; at >= 0; at-- {
		e := &d.entries[at]
		if e.turn != d.runningTurn {
			break
		}
		if e.kind == entryAssistant || e.kind == entryThinking {
			total += len(e.text)
		}
	}
	return total
}

// stepTokenWord is what the model has written inside ONE RUNNING STEP, spelled
// for that step's own caption row — "↓ 486", or "" when the step has written
// nothing yet, which is every step that is only a tool call.
//
// IT COMES BACK PLAIN, because the row it joins is a list of dim facts about the
// step and paints the whole list at once (caption.go). A painted figure handed
// into that would be painted twice, which is one escape sequence too many and a
// width nobody can measure.
//
// It is deliberately not eased. The turn's pair is the figure a person watches;
// this one is a fact beside a step title in an opened outline, it moves in the
// same lumps the text does, and a second walking figure on the same frame would
// be two things moving where one is the signal.
func (a *app) stepTokenWord(c caption, d deck) string {
	if !a.tokenColumnOn(d) || !c.ended.IsZero() {
		return ""
	}
	written := session.EstimateTokens(stepWritten(c, d.entries))
	if written <= 0 {
		return ""
	}
	mark := tokenDownGlyph
	if a.pal.ascii || a.linear {
		mark = tokenDownPlain
	}
	return mark + " " + tokenWord(written)
}
