package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// ── THE LIVE TOKEN COLUMN ───────────────────────────────────────────────────
//
//	▸ Working · ctrl+e                                  +3.4k ↑ 63.6k  ↓ 2,531
//	  reading 2 files in internal/tui3
//	· running go test ./internal/session · 41s                 ↑ 67k  ↓ 2,531
//
// THE DEFECT THIS FIXES: a running turn draws one still line, and that line
// claims exactly as much at second one as at second forty. The shimmer says
// "alive"; the caption says what the work is about; neither says whether
// anything is MOVING — and a person watching a slow endpoint, a long think or a
// stalled stream cannot tell the three apart. The pulse already learned to name
// what it is waiting for (render.go's [app.waitingWords]); this is the other
// half of the same question, and it is the half a number answers better than a
// sentence: how big is what is going up, how much has come back, right now.
//
// WHAT THE TWO FIGURES MEAN, AND WHERE EACH OF THEM COMES FROM.
//
//	↑  the weight of the request    The conversation's context — what the next
//	   IN FLIGHT.                   request carries whole: the system prompt,
//	                                every message, every tool result. It is
//	                                [session.Agent.ContextTokens] read through
//	                                [app.measureContext] and the usage beat, so
//	                                it steps up when a step's usage lands and
//	                                again when a tool result joins.
//	↓  what this work has           The books' output, plus an estimate of what
//	   RECEIVED.                    has arrived on the page SINCE those books
//	                                last moved — prose, reasoning, and the
//	                                arguments of calls still streaming in.
//
// ↑ USED TO BE THE BILL, AND THE BILL IS THE WRONG FIGURE FOR THIS COLUMN. It
// was the input the provider charged, summed over every request of the turn, so
// three steps on a 50k conversation read 150k — a number that answers "what
// have I paid for" (the status line's question, and /cost's) with a figure
// shaped like "how big is this", and which nobody could reconcile with anything
// else on the screen. The weight of the request is the figure a person reads
// the column for: it is what is being SENT, and it grows exactly when the
// conversation does. Spend stays where it always was.
//
// ↓ IS THE BOOKS PLUS WHAT IS NEW SINCE THEM, NEVER THE LARGER OF THE TWO. The
// first cut drew max(books, estimate of the whole turn), and the moment a bill
// counted tokens the page never showed — a call's arguments, hidden reasoning,
// the error in a four-bytes-to-the-token guess — the estimate sat under the
// books and ↓ STOOD STILL until the prose caught up, which on a turn of tool
// calls is never. So every page remembers how much of its writing it had when
// the books last moved ([tokenCol.bill]), and draws the books plus an estimate
// of only what arrived after. The whole-turn estimate stays as the floor for a
// page whose books have not said anything yet.
//
// A CALL'S ARGUMENTS ARE THE MODEL'S WRITING TOO. A `write` or a `propose_task`
// streams its body for seconds, and the provider bills every byte of it as
// output; a count of prose alone froze ↓ for exactly as long as the model was
// writing hardest, then leapt when the bill landed. The forming row already
// carries the size of what has arrived (feed.go's [feed.formTool]), and
// [modelWrote] counts it.
//
// ONE COLUMN, EVERY PAGE THAT STREAMS. The state lives on [feed] — the ONE
// reducer both the conversation and a task room grow their transcripts with
// (feed.go, and docs/design/lens/DESIGN.md's Decision 1 on why there is not a
// second one) — so a node's page draws the same column by the same code, from
// its own sources:
//
//   - the conversation's books are the session's, turn-scoped (app.go's
//     [app.take]), and its weight is the conversation's own;
//   - a task room on THIS machine takes its books from the node's lane — the
//     sum of the steps the lane has heard, as its bill is counted
//     ([taskNode.liveCost]) — and its weight from the node's own worker
//     ([taskWeightDoor]), asked on the same beat as the conversation's;
//   - a task room on ANOTHER machine has no lane at all (room.go's
//     [app.openFarRoom]), so it reads both off the node's journal, which banks
//     one line per request ([session.RequestBooks]): ↑ is the newest request as
//     it was sent, ↓ the output of every request in the tail the page read.
//
// In a room ↑ is ALWAYS ONE REQUEST, NEVER A SUM, for the conversation's
// reason. Where a page knows nothing yet — a node that was already running when
// the room opened and has not asked anything since — the figure is absent,
// which is the emptiness law and not a gap. Every page reaches the drawing
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
// and the whole column goes with the turn — this is the owner's ruling, and the
// session's totals are the status line's, which never go away.
//
// IT IS SPARE CELLS AND NEVER A RESERVATION. The words are the point of every
// row it lands on, so the column takes what is left at the right edge after the
// sentence has taken what it needs, exactly as the step clock does beside it
// (steptime.go) — no rewrap, no ellipsis, no fourth row. On a frame too narrow
// to hold all of it, the column sheds whole pieces in a fixed order (see
// [app.tokenColumn]), and on one too narrow for any of it the sentence wins.
//
// IT IS THE MARGIN'S INK, AND IT LIGHTS UP ONLY WHERE IT MOVED. The first cut
// painted the figures in the datum hue and they shouted; the second painted
// them dim and still, and a person could not tell a figure that had just moved
// from one that had been standing for a minute. So the RESTING column is the
// margin's tier every other right-edge fact wears (`41s`, `189 lines`), with the
// arrow one stop under its figure — and the side that has just moved GLOWS: its
// figure steps up to the reading ink and its arrow up one stop, then decays back
// through the palette's own ramp over [tokenGlowSpan] ([tokenGlow]). The two
// halves glow independently, so a person sees WHICH way the traffic is going.
// While a tool runs nothing moves, both halves rest dim, and that is the whole
// reading of "waiting, not dead": the shimmer says alive, the figures say quiet.
// A FIGURE THAT MOVES IS ALREADY SALIENT; the glow says only that it moved just
// now, and it is gone again before it can compete with the sentence.
//
// UNDER TEN THOUSAND EVERY TOKEN IS VISIBLE. A figure spelled `2.5k` changes
// once per hundred tokens, which on a slow stream is once every several seconds
// — exactly the stillness the column exists to rule out. So a column figure
// under 10,000 is spelled whole ([tokenColWord]); from there up it keeps
// [tokenWord]'s one decimal, where a hundred tokens is a rounding error and the
// glow carries the motion instead.
//
// AND A JUMP IN ↑ LEAVES A RECEIPT. When the request's weight steps up — a tool
// result joining, a step's usage landing — a faint `+3.4k` stands just left of
// the figure for [tokenReceiptSpan] and goes: the eased walk makes the jump
// legible, and the receipt says how big it was without anybody subtracting.
// It is the first thing a narrow row gives up.
//
// AND THE FIGURES EASE, on reveal.go's clock and curve, because THE ARRIVAL
// SHAPE IS NOT THE DRAWING SHAPE: output arrives in lumps and the books land a
// step at a time, and a figure that jumped four thousand tokens between two
// frames reads as a glitch where the same four thousand walked reads as work.
// The books stay exact — nothing here is rounded into them — and the drawn pair
// snaps the instant the work stops, along with every other meter on the surface.
// THE LINEAR TIER NEVER PACES ONE AND NEVER GLOWS ONE (tui3.go's
// Options.Linear): a reader hears the exact figure, not its animation.

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
	// tokenExactBelow is where a column figure stops being spelled whole. Below
	// it every token is its own digit; at and above it the figure takes
	// [tokenWord]'s one decimal (see the header's UNDER TEN THOUSAND).
	tokenExactBelow = 10_000
	// tokenGlowSpan is how long a figure that has just moved takes to come back
	// to its resting ink, and tokenGlowStops is how many steps the decay takes
	// to get there. About a second and a third: long enough to be seen out of
	// the corner of an eye after the words have been read, short enough that a
	// figure which has stopped moving has visibly stopped before anybody wonders.
	tokenGlowSpan  = 1300 * time.Millisecond
	tokenGlowStops = 2
	// tokenReceiptSpan is how long the `+3.4k` beside ↑ stands. A little longer
	// than the glow, so the receipt is still readable when the walk it explains
	// has arrived.
	tokenReceiptSpan = 1500 * time.Millisecond
)

// tokenCol is one page's column: its books, its weight, the pair in motion, and
// when each half last moved. It lives on [feed] so that every page grown by the
// reducer has one.
type tokenCol struct {
	// down is THE BOOKS for what this page's work has received — what its source
	// has been told was billed as output — and nothing here ever moves it.
	down int
	// billedAt and billedTurn are how much of the model's writing was on the
	// page, and in which turn, the last time `down` moved. What has arrived
	// SINCE is the only part of the page the books have not already counted
	// ([tokenCol.reading]).
	billedAt, billedTurn int
	// weight is ↑: what the request in flight weighs. The conversation's is its
	// context ([app.measureContext]); a room's is its node's newest request.
	// Zero is "nobody has said", and draws nothing.
	weight int
	// shownUp and shownDown are the pair in motion (the walk below).
	shownUp, shownDown int
	// upMoved and downMoved are when each drawn figure last changed, which is
	// all the glow is measured from ([tokenGlow]).
	upMoved, downMoved time.Time
	// readUp is ↑ as the last tick read it, so the next can tell a jump; receipt
	// is the size of the jumps inside the receipt's window and receiptAt when
	// the newest of them landed.
	readUp    int
	receipt   int
	receiptAt time.Time
}

// open is the column at the start of a piece of work: every figure at nothing.
// A pair left standing at the last turn's totals would spend the first second
// of this one walking DOWN in front of somebody.
func (c *tokenCol) open() { *c = tokenCol{} }

// bill takes a new reading of the books, remembering how much of the page's
// writing — `written` bytes, in `turn` — they already account for. A reading
// that has not moved changes nothing, so a beat that re-asks the same books
// thirty times does not keep pushing the mark forward past bytes that arrived
// in between and were never billed.
func (c *tokenCol) bill(down, written, turn int) {
	if down == c.down {
		return
	}
	c.down, c.billedAt, c.billedTurn = down, written, turn
}

// reading is the exact pair, given how many bytes of the model's own writing
// the running turn has on the page.
//
// Zero is "nothing is known", not "nothing happened", and the emptiness law
// takes it from there: a figure that has not been earned yet is not drawn.
func (c *tokenCol) reading(written, turn int) (up, down int) {
	up = c.weight
	since := written
	if c.down > 0 && turn == c.billedTurn {
		since = written - c.billedAt
	}
	if since < 0 {
		since = 0
	}
	down = c.down + session.EstimateTokens(since)
	// The whole-turn estimate is the floor for a page whose books have said
	// nothing yet, and the honest one: bytes on the page were certainly written.
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
//
// IT IS ALSO WHERE A MOVE IS TIMED AND A JUMP IS RECEIPTED, because it is the
// one place that sees both the reading and the figure on the screen change.
// A snapped pair marks nothing: a column that is not running draws nothing, and
// the linear tier neither glows nor receipts.
func (c *tokenCol) tick(written, turn, slots int, snap bool, now time.Time) bool {
	up, down := c.reading(written, turn)
	if !snap {
		c.receiptOf(up, now)
	}
	c.readUp = up
	if snap {
		moved := c.shownUp != up || c.shownDown != down
		c.shownUp, c.shownDown = up, down
		return moved
	}
	nextUp, nextDown := easeInt(c.shownUp, up, slots), easeInt(c.shownDown, down, slots)
	if nextUp != c.shownUp {
		c.upMoved = now
	}
	if nextDown != c.shownDown {
		c.downMoved = now
	}
	moved := nextUp != c.shownUp || nextDown != c.shownDown
	c.shownUp, c.shownDown = nextUp, nextDown
	return moved
}

// receiptOf notes a jump in ↑. ONLY A RISE FROM A KNOWN FIGURE IS A JUMP: the
// first reading of a turn is ↑ arriving, not ↑ growing, and a receipt reading
// `+63.6k` on every turn's first frame would be the whole figure said twice. A
// fall — a compaction — takes the receipt away rather than receipting a loss,
// because the meter on the status line says that event at once and in its own
// words. Jumps inside one window add up, so the receipt says how far the figure
// moved since it last stood still.
func (c *tokenCol) receiptOf(up int, now time.Time) {
	switch {
	case up < c.readUp:
		c.receipt, c.receiptAt = 0, time.Time{}
	case up > c.readUp && c.readUp > 0:
		if c.receipt > 0 && now.Sub(c.receiptAt) < tokenReceiptSpan {
			c.receipt += up - c.readUp
		} else {
			c.receipt = up - c.readUp
		}
		c.receiptAt = now
	}
}

// drawn is the pair the frame paints: the eased readings while the work runs
// on a tier that paces, the exact ones everywhere else. A restore, a settle and
// the screen-reader tier take the exact figure for reveal.go's stated reason —
// nobody is watching those numbers grow.
func (c *tokenCol) drawn(written, turn int, running, linear bool) (up, down int) {
	if !running {
		return 0, 0
	}
	if linear {
		return c.reading(written, turn)
	}
	return c.shownUp, c.shownDown
}

// tokenGlow is how brightly a figure that last moved at `moved` is lit at
// `now`: [tokenGlowStops] just after the move, one fewer at each even step of
// [tokenGlowSpan], and zero — the resting ink — from then on. A figure that has
// never moved is at rest.
func tokenGlow(moved, now time.Time) int {
	if moved.IsZero() {
		return 0
	}
	since := now.Sub(moved)
	if since < 0 {
		since = 0
	}
	if since >= tokenGlowSpan {
		return 0
	}
	return tokenGlowStops - int(since*tokenGlowStops/tokenGlowSpan)
}

// tokenLit is one half's glow as this frame should paint it — none at all on a
// terminal that cannot fade, which is the same answer [palette.fade] gives: the
// sixteen-colour profiles, NO_COLOR, and the screen-reader tier.
func (a *app) tokenLit(moved time.Time) int {
	if !a.pal.fading() || a.linear {
		return 0
	}
	return tokenGlow(moved, a.now())
}

// tokenColWord is a column figure spelled for the column: whole below
// [tokenExactBelow], with the thousands grouped the one way this surface
// groups them ([groupedInt]); [tokenWord] from there up.
func tokenColWord(n int) string {
	if n < tokenExactBelow {
		return groupedInt(n)
	}
	return tokenWord(n)
}

// modelWrote is how many bytes of THE MODEL'S OWN WRITING one entry holds: the
// prose and the reasoning, which the provider bills as one thing, and the
// arguments of a call — the bytes that have arrived while it forms
// ([entry.bytes]) or the payload once it is whole, whichever is larger, since
// the second is clipped for display and the first is not.
func modelWrote(e *entry) int {
	switch e.kind {
	case entryAssistant, entryThinking:
		return len(e.text)
	case entryTool:
		if n := len(e.detail.Args); n > e.bytes {
			return n
		}
		return e.bytes
	}
	return 0
}

// turnWritten is how many bytes of the model's own writing the RUNNING TURN
// has put on this page ([modelWrote]).
//
// It walks BACK from the end and stops at the first entry belonging to another
// turn, so it costs the length of the running turn rather than the length of
// the transcript, and it is a sum of lengths rather than of strings: nothing
// here copies a byte.
func (f *feed) turnWritten() int {
	return writtenIn(f.entries, f.turn)
}

// writtenIn is [feed.turnWritten] over any page's entries and turn.
func writtenIn(es []entry, turn int) int {
	total := 0
	for at := len(es) - 1; at >= 0; at-- {
		e := &es[at]
		if e.turn != turn {
			break
		}
		total += modelWrote(e)
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
		total += modelWrote(&es[at])
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
	now := a.now()
	// THE CONVERSATION'S WEIGHT IS TAKEN HERE, ON THE CLOCK, rather than at the
	// weight's own door: [app.measureContext] and the usage beat run where the
	// weight changes and the column is a reader of it, not a second owner.
	a.col.weight = a.ctxTokens
	if a.col.tick(a.turnWritten(), a.turn, slots, snap || a.linear || !working, now) {
		a.dirty = true
	}
	if a.room == nil {
		return
	}
	if a.room.col.tick(a.room.turnWritten(), a.room.turn, slots, snap || a.linear || !a.room.running(), now) {
		a.room.dirty = true
	}
}

// tokenColumnOn reports whether this deck may draw the column at all: it has
// one to carry, and the work it shows is still running.
func (a *app) tokenColumnOn(d deck) bool {
	return d.col != nil && d.runningTurn != 0
}

// tokenPair is the column as one frame paints it: the two figures, how brightly
// each is lit ([tokenGlow]), and the receipt beside ↑ — zero where there is
// none to draw.
type tokenPair struct {
	up, down       int
	upLit, downLit int
	receipt        int
}

// tokenFigure is one half of the column — the arrow and its figure — painted
// at its glow, and the cells it takes.
//
// AT REST THE FIGURE IS DIM AND THE ARROW IS DIMMER. The number is what the row
// came to say and it wears the margin's tier ([palette.dim]), like every other
// fact at the right edge of a row on this surface; the arrow is a label on it
// and takes the fade ramp's faintest stop ([palette.fade]). LIT, both step up
// the SAME ramp the surface already paints with and nothing else: the figure to
// the narration rung and then to the reading ink, the arrow one stop up the
// fade ramp. No new colour is minted for it, and nothing lit is ever brighter
// than the sentence the column sits beside.
func (a *app) tokenFigure(glyph, plain string, n, lit int) (string, int) {
	if n <= 0 {
		return "", 0
	}
	mark := glyph
	if a.pal.ascii || a.linear {
		mark = plain
	}
	word := " " + tokenColWord(n)
	var figure, arrow string
	switch {
	case lit >= tokenGlowStops:
		figure, arrow = a.pal.ink(word), a.pal.fade(mark, 1)
	case lit > 0:
		figure, arrow = a.pal.narr(word), a.pal.fade(mark, 1)
	default:
		figure, arrow = a.pal.dim(word), a.pal.fade(mark, 0)
	}
	return arrow + figure, ansi.StringWidth(mark) + ansi.StringWidth(word)
}

// tokenReceipt is the `+3.4k` beside ↑, painted, and the cells it takes. It is
// the fade ramp's middle stop: fainter than the figure it explains, legible
// against the ground, and back at the dim tier on a terminal that cannot fade.
func (a *app) tokenReceipt(n int) (string, int) {
	if n <= 0 {
		return "", 0
	}
	word := "+" + tokenWord(n)
	return a.pal.fade(word, 1), ansi.StringWidth(word)
}

// tokenColumn is the pair as a person reads it, and the cells it takes.
//
// IT DROPS WHOLE PIECES RATHER THAN CLIPPING ONE, IN A FIXED ORDER: the receipt
// first, then ↑, and ↓ last. Half a number is a number a person has to
// distrust — the rule the tool row's column is written to (toolview.go's
// [app.tailOf]) — and the order is the order of what the column is FOR: ↓
// answers the question it exists for, something is coming back; ↑ is the
// context around it; and the receipt is an annotation on ↑ that means nothing
// once ↑ is gone.
func (a *app) tokenColumn(p tokenPair, budget int) (string, int) {
	upText, upCells := a.tokenFigure(tokenUpGlyph, tokenUpPlain, p.up, p.upLit)
	downText, downCells := a.tokenFigure(tokenDownGlyph, tokenDownPlain, p.down, p.downLit)
	gap := len(tokenColGap)
	if upCells > 0 && downCells > 0 {
		if rText, rCells := a.tokenReceipt(p.receipt); rCells > 0 {
			if cells := rCells + 1 + upCells + gap + downCells; cells <= budget {
				return rText + " " + upText + tokenColGap + downText, cells
			}
		}
	}
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
func (a *app) tokenSuffix(p tokenPair, used, room int) string {
	budget := room - used - tokenColClear
	if budget < 1 {
		return ""
	}
	text, cells := a.tokenColumn(p, budget)
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
	return a.tokenSuffix(a.tokenPairOf(d), used, room)
}

// tokenPairOf is everything the column paints for one deck on this frame.
func (a *app) tokenPairOf(d deck) tokenPair {
	c := d.col
	up, down := c.drawn(writtenIn(d.entries, d.runningTurn), d.runningTurn, true, a.linear)
	p := tokenPair{up: up, down: down, upLit: a.tokenLit(c.upMoved), downLit: a.tokenLit(c.downMoved)}
	if !a.linear && c.receipt > 0 && a.now().Sub(c.receiptAt) < tokenReceiptSpan {
		p.receipt = c.receipt
	}
	return p
}

// stepTokenWord is what the model has written inside ONE RUNNING STEP, spelled
// for that step's own caption row — "↓ 486", or "" when the step has written
// nothing yet.
//
// IT COMES BACK PLAIN, because the row it joins is a list of dim facts about the
// step and paints the whole list at once (caption.go). A painted figure handed
// into that would be painted twice, which is one escape sequence too many and a
// width nobody can measure.
//
// It is deliberately not eased and not lit. The turn's pair is the figure a
// person watches; this one is a fact beside a step title in an opened outline,
// it moves in the same lumps the text does, and a second walking, glowing figure
// on the same frame would be two things moving where one is the signal.
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
	return mark + " " + tokenColWord(written)
}
