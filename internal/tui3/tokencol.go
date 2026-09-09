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
//	↑  what this turn has SENT.     The books' input for the turn, floored by
//	                                what one request weighs — so it is a figure
//	                                from the first frame rather than from the
//	                                first step that lands, and it steps up as
//	                                each tool result joins what goes back out.
//	↓  what this turn has RECEIVED. The books' output for the turn, floored by
//	                                an estimate of the bytes already on the page
//	                                — so it moves while the model writes rather
//	                                than once a step, and the exact figure takes
//	                                over the moment a step reports one.
//
// BOTH ARE THE MAX OF A BOOK AND AN ESTIMATE, which is not a hedge — it is the
// shape [session.Agent.ContextTokens] already uses for the same reason: the
// provider's count is the honest number for what has been SETTLED and knows
// nothing about what has happened since, and the content estimate is the honest
// floor for the part nobody has been billed for yet. The books are never moved
// by anything here; the drawn figure is the larger of the two readings.
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
// AND THE FIGURES EASE, on reveal.go's clock and curve, because THE ARRIVAL
// SHAPE IS NOT THE DRAWING SHAPE: output arrives in lumps and the books land a
// step at a time, and a figure that jumped four thousand tokens between two
// frames reads as a glitch where the same four thousand walked reads as work.
// The books stay exact — nothing here is rounded into them — and the drawn pair
// snaps the instant the turn stops, along with every other meter on the surface.
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

// tokenColumnOn reports whether this page may draw the column at all.
//
// THE SESSION'S FIGURES ARE THE SESSION'S. Only the page that carries the
// conversation's own receipts may quote them (lens.go's [receiptsInline], and
// workfold.go's chip refuses the same reading for the same reason): a task
// room draws somebody else's work, and the turn totals held on this surface are
// not that node's — they are this conversation's, and drawing them over a
// node's live step would be a figure about the wrong worker, said confidently.
func (a *app) tokenColumnOn(d deck) bool {
	return a.state == stateWorking && d.runningTurn != 0 && d.lens.receipts == receiptsInline
}

// turnTokens is what the running turn has sent and received, as the BOOKS and
// the page between them know it. It is the exact pair; [app.turnTokensDrawn] is
// the pair in motion.
//
// Zero is "nothing is known", not "nothing happened", and the emptiness law
// takes it from there: a figure that has not been earned yet is not drawn.
func (a *app) turnTokens() (up, down int) {
	if a.state != stateWorking {
		return 0, 0
	}
	// The books first, turn-scoped: what the session has spent since this turn
	// opened its clock (app.go's [app.startClock] takes both marks).
	up = a.inputTokens - a.turnInStart
	down = a.outputTokens - a.turnOutStart
	// THE FLOOR UNDER ↑ IS WHAT ONE REQUEST WEIGHS. Before the first step
	// reports, the books say nothing at all about a turn — and the first ten
	// seconds of a turn are exactly the stretch this column exists for. The
	// conversation's weight is the honest answer to "how much went up" for a
	// request that is out and unanswered, and it is already measured
	// ([app.measureContext]).
	if a.ctxTokens > up {
		up = a.ctxTokens
	}
	// AND THE FLOOR UNDER ↓ IS WHAT IS ALREADY ON THE PAGE. Between two book
	// readings the only evidence of output is the text the surface has drawn, so
	// the bytes of it are estimated with the engine's own estimator
	// ([session.EstimateTokens] — one divisor for the whole program) and the
	// larger reading wins.
	if est := session.EstimateTokens(a.turnWritten()); est > down {
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

// turnWritten is how many bytes of the model's own writing this turn has put on
// the page — its prose and its reasoning, which the provider bills as one thing.
//
// It walks BACK from the end and stops at the first entry belonging to another
// turn, so it costs the length of the running turn rather than the length of the
// conversation, and it is a sum of lengths rather than of strings: nothing here
// copies a byte.
func (a *app) turnWritten() int {
	total := 0
	for at := len(a.entries) - 1; at >= 0; at-- {
		e := &a.entries[at]
		if e.turn != a.turn {
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

// turnTokensDrawn is the pair the frame paints: the eased readings while the
// turn runs, the exact ones everywhere else.
//
// The two are the same figure at rest. A restore, a settle and the screen-reader
// tier take the exact one for reveal.go's stated reason — nobody is watching
// those numbers grow.
func (a *app) turnTokensDrawn() (up, down int) {
	up, down = a.turnTokens()
	if a.linear || a.state != stateWorking {
		return up, down
	}
	return a.shownUp, a.shownDown
}

// tickTokenCol walks the drawn pair toward the readings, on the slots
// [app.tickReveal] measured — the same clock and the same curve as every other
// figure in motion on this surface.
//
// IT CHASES WITHOUT BEING ARMED, and that is the difference between this pair
// and the status line's meters. Those move when a reading LANDS, so something
// has to arm them at the landing ([app.armMeters]); ↓ moves as bytes arrive,
// which is most frames of a streaming turn, and a flag raised and lowered thirty
// times a second would be bookkeeping in place of a rule. The rule is simply
// that the pair follows the figures for as long as the turn runs.
func (a *app) tickTokenCol(slots int, snap bool) {
	up, down := a.turnTokens()
	if snap || a.linear || a.state != stateWorking {
		a.shownUp, a.shownDown = up, down
		return
	}
	nextUp, nextDown := easeInt(a.shownUp, up, slots), easeInt(a.shownDown, down, slots)
	if nextUp != a.shownUp || nextDown != a.shownDown {
		a.dirty = true
	}
	a.shownUp, a.shownDown = nextUp, nextDown
}

// tokenFigure is one half of the column — the arrow and its figure — painted,
// and the cells it takes.
//
// THE ARROW IS DIM AND THE NUMBER IS THE DATUM. The glyph is a label and the
// figure is what the row came to say, so the figure takes the payload ink
// (styles.go's [palette.data]) on a row standing for the turn, and stays dim one
// tier down on a row standing for a step. That is the whole typographic
// hierarchy here: brightness says which scope you are reading.
func (a *app) tokenFigure(glyph, plain string, n int, bright bool) (string, int) {
	if n <= 0 {
		return "", 0
	}
	mark := glyph
	if a.pal.ascii || a.linear {
		mark = plain
	}
	word := tokenWord(n)
	figure := a.pal.dim(word)
	if bright {
		figure = a.pal.data(word)
	}
	return a.pal.dim(mark+" ") + figure, ansi.StringWidth(mark) + 1 + ansi.StringWidth(word)
}

// tokenColumn is the pair as a person reads it, and the cells it takes.
//
// IT DROPS A WHOLE FIGURE RATHER THAN CLIPPING ONE, and ↑ goes first. Both are
// the same rule the tool row's column is written to (toolview.go's [app.tailOf]):
// half a number is a number a person has to distrust, and of these two ↓ is the
// one that answers the question the column exists for — something is coming
// back — while ↑ is the context around it.
func (a *app) tokenColumn(up, down, budget int, bright bool) (string, int) {
	upText, upCells := a.tokenFigure(tokenUpGlyph, tokenUpPlain, up, bright)
	downText, downCells := a.tokenFigure(tokenDownGlyph, tokenDownPlain, down, bright)
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
func (a *app) tokenSuffix(up, down, used, room int, bright bool) string {
	budget := room - used - tokenColClear
	if budget < 1 {
		return ""
	}
	text, cells := a.tokenColumn(up, down, budget, bright)
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
	up, down := a.turnTokensDrawn()
	return a.tokenSuffix(up, down, used, room, true)
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
