package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The turn's activity: what it is doing, while it does it, and one row about it
// afterwards.
//
// THE DEFECT, reported as a feeling: the head reads the board, opens two
// results, opens a file and commissions work, and for the whole of it the
// person watches one pulsing `thinking` line. Between two tool calls there are
// no tokens, so a surface fed only by tokens is a surface that goes quiet
// exactly when the machine is busiest. A minute of that is indistinguishable
// from a hang.
//
// The head now narrates each belt call on the same feed its words ride
// (internal/head/activity.go), and this file is the two halves of drawing it.
//
//   - WHILE THE TURN RUNS: one row per call, pinned under the streamed text and
//     above the awaiting line, in the secondary and faint registers, at the body
//     indent. The gloss arrives person-readable and is drawn as it came.
//   - WHEN THE TURN LANDS: the rows collapse into ONE quiet row attached under
//     the durable reply — `▸ 5 steps — searched twice, read the plan, put work
//     in hand` — openable through the same per-block disclosure every other row
//     in this transcript uses (disclose.go).
//
// WHAT IT REFUSES TO SPEND. No motion of its own: §18.2 sanctions one moving
// glyph in this region and the awaiting line already holds it, so a running call
// wears the static [tokens.GlyphWorking]. No hue: every row here is TextSecondary
// or TextTertiary, which is what keeps it legible as CHROME beside an answer and
// keeps the whole feature honest at NoColor. No growth without bound: a turn may
// spend sixteen calls and the live region shows the last few with a count of what
// scrolled off, because the region sits above the composer and a live block that
// grows for a minute is a composer that walks off the bottom of the screen.

// activityStep is one belt call as this transcript saw it.
//
// A step is born from a begin and settled by the end that follows it. The two
// are strictly sequential — the head executes its calls one at a time and emits
// around each — so the end settles the newest unsettled step and needs no id.
// That is also what makes a window attached MID-TURN degrade correctly: it has
// no unsettled step to settle, so the orphan end lands on nothing.
type activityStep struct {
	// gloss is the head's own person-readable sentence about the call.
	gloss string
	// hint is the short thing worth saying about what came back, often empty.
	hint string
	// done says an end has arrived; failed says which end it was.
	done   bool
	failed bool
}

// activityLiveRows is how many calls the live region shows at once.
//
// The belt allows sixteen in a turn (internal/head/loop.go). Sixteen rows above
// the composer is not a transcript, it is a log — and the rows a reader cares
// about are the ones happening now, so the block keeps the newest few and says
// how many went past. The whole list is one keystroke away the moment the turn
// lands, which is the point of the collapse row.
const activityLiveRows = 5

// activityBlock is the live activity region: the rows between the streamed reply
// and the awaiting line.
//
// It is a live block in exactly the sense [awaitingBlock] is — never finalized,
// promising no settled rows, re-rendered every frame — so the committed
// transcript above it is never rebuilt on its account.
type activityBlock struct {
	style *tokens.Styler
	// linear freezes nothing here (no glyph on this block moves) but it is the
	// flag the collapse row reads for 10.1.5's plain rendering, and it is kept
	// beside the steps so both halves answer the same window.
	linear bool
	steps  []activityStep

	rows []string
}

var _ blocks.Block = (*activityBlock)(nil)

// ID is stable: there is at most one activity region in a room.
func (b *activityBlock) ID() string { return "activity" }

// IsFinalized is always false. This is the live region, with the awaiting line.
func (b *activityBlock) IsFinalized() bool { return false }

// SettledRows promises nothing: a step settles under the reader's eye.
func (b *activityBlock) SettledRows(int) int { return 0 }

// Version never moves; a live block is re-rendered rather than invalidated.
func (b *activityBlock) Version() uint64 { return 0 }

// End is live, always.
func (b *activityBlock) End() blocks.EndState { return blocks.EndLive }

// begin opens a step. An empty gloss is refused rather than drawn as a bare
// marker: §16's EMPTINESS says an absent figure renders as absence, and a row
// that says only `◐` says nothing at all.
func (b *activityBlock) begin(gloss string) bool {
	gloss = strings.TrimSpace(flattenLine(gloss))
	if b == nil || gloss == "" {
		return false
	}
	b.steps = append(b.steps, activityStep{gloss: gloss})
	return true
}

// settle closes the newest open step, and reports whether there was one.
//
// AN ORPHAN END CHANGES NOTHING. A window that attached mid-turn — a second
// terminal opened onto the same room, a promotion, a room switched into while
// the head was already reading — heard the end and never the begin, and it has
// no honest row to draw for it. So it draws none. Inventing a settled row for a
// call this window never saw begin would be the surface reporting work it has no
// account of.
func (b *activityBlock) settle(hint string, failed bool) bool {
	if b == nil {
		return false
	}
	for i := len(b.steps) - 1; i >= 0; i-- {
		if b.steps[i].done {
			break
		}
		b.steps[i].done = true
		b.steps[i].failed = failed
		b.steps[i].hint = strings.TrimSpace(flattenLine(hint))
		return true
	}
	return false
}

// live reports that this block has anything to draw. A turn that made no tool
// calls draws no region at all, which is the ordinary conversational turn and
// must cost it nothing.
func (b *activityBlock) live() bool { return b != nil && len(b.steps) > 0 }

// Rows draws the newest calls, one per row.
func (b *activityBlock) Rows(width int) []string {
	rows := b.rows[:0]
	if width < 1 || len(b.steps) == 0 {
		b.rows = rows
		return b.rows
	}
	shown := b.steps
	earlier := 0
	if len(shown) > activityLiveRows {
		earlier = len(shown) - activityLiveRows
		shown = shown[earlier:]
	}
	if earlier > 0 {
		rows = append(rows, b.paint(activityIndent()+
			blocks.Truncate(tokens.GlyphSeparator+" "+strconv.Itoa(earlier)+" earlier",
				width-bodyIndent), tokens.TextTertiary))
	}
	for _, step := range shown {
		rows = append(rows, b.stepRow(step, width))
	}
	b.rows = rows
	return b.rows
}

// stepRow draws one call: a marker, the gloss, and the hint behind a separator.
//
// The marker and the words are painted separately and measured separately, for
// [awaitingBlock.Rows]'s reason — slicing a painted string by bytes is how a row
// ends up carrying half an escape sequence.
func (b *activityBlock) stepRow(step activityStep, width int) string {
	glyph, tier := tokens.GlyphWorking, tokens.TextSecondary
	if step.done {
		glyph, tier = tokens.GlyphSettled, tokens.TextTertiary
		if step.failed {
			glyph = tokens.GlyphFailed
		}
	}
	text := step.gloss
	if step.hint != "" {
		text += " " + tokens.GlyphSeparator + " " + step.hint
	}
	room := width - bodyIndent - blocks.Width(glyph) - 1
	if room < activityFloor {
		// No measure worth a sentence beside the marker. The row is dropped
		// rather than reduced to a glyph: see [activityBlock.begin].
		return b.paint(blocks.Truncate(activityIndent()+glyph, width), tier)
	}
	return b.paint(activityIndent()+glyph+" ", tier) +
		b.paint(blocks.Truncate(text, room), tier)
}

// activityFloor is the measure below which an activity row says nothing worth a
// row. Same figure and same reasoning as [previewFloor].
const activityFloor = 12

func activityIndent() string { return strings.Repeat(" ", bodyIndent) }

func (b *activityBlock) paint(text string, tier tokens.Token) string {
	if b == nil || b.style == nil {
		return text
	}
	return b.style.PaintToken(text, tier)
}

// -- the app's seam ----------------------------------------------------------

// applyToolStream folds one activity boundary into the live turn and reports
// whether anything on screen moved.
//
// It is called from [App.applyStream]'s switch and nowhere else, so the session
// filter that guards every other boundary guards these too.
func (a *App) applyToolStream(event StreamEvent) bool {
	if !a.turn.active {
		// A tool boundary with no live turn is an event for a turn this window
		// is not watching. There is no region to put it in and opening one would
		// be a window claiming a turn it cannot see the end of.
		return false
	}
	if a.turn.activity == nil {
		a.turn.activity = &activityBlock{style: a.style, linear: a.linear}
	}
	moved := false
	switch event.Kind {
	case StreamToolBegin:
		moved = a.turn.activity.begin(event.Delta)
	case StreamToolEnd:
		moved = a.turn.activity.settle(event.Delta, false)
	case StreamToolFailed:
		moved = a.turn.activity.settle(event.Delta, true)
	}
	if !moved {
		return false
	}
	// The region has to be IN the transcript to be drawn, and the first step is
	// the moment it earns a place. Re-attaching is idempotent (attachLive), so
	// this costs an index lookup on every step after the first.
	a.detachLive()
	a.attachLive()
	return true
}

// collapseActivity folds the live region into one row under the message that
// ended the turn.
//
// THE STATE LIVES WITH THE BLOCK, which is the only place it can live and
// survive scrollback: the transcript keeps its blocks, the reader's fold
// decision is keyed by block id (disclose.go), and both come back unchanged when
// the row scrolls off and back on. A map on the app keyed by sequence would be a
// second truth about the same row.
//
// A turn that made no tool calls collapses into nothing at all — no row, no
// disclosure, no trace — which is the ordinary conversational turn and the one
// this surface may not make heavier.
func (a *App) collapseActivity(into *messageBlock) {
	steps := a.turn.activity
	if into == nil || !steps.live() {
		return
	}
	// ONE DOOR PER ROW. A block that already folds something of its own — its
	// own long body, a card's tail — keeps that door, and the activity settles
	// for the plain summary. Two chevrons on one block is a row with two answers
	// to "what does clicking me do".
	//
	// LINEAR MODE TAKES THE SAME BARGAIN for a different reason (10.1.5): the
	// accessible rendering is a single column of plain rows, and content behind
	// an interaction is content that surface cannot deliver.
	taken := into.foldsBody || into.tailFold || into.collapsible
	into.activity = append(into.activity[:0], steps.steps...)
	if !a.linear && !taken {
		into.collapsible = true
		a.applyFold(into)
		a.foldable = true
	}
	into.measured = false
	into.version++
}

// activityOwnsFold reports that this block's disclosure is the activity's own.
//
// It is the one question three places have to agree on — where the door is
// drawn, where a pointer resolves it, and whether the header may carry a hint —
// so it is asked once. See [App.collapseActivity] for who is allowed to take it.
func (b *messageBlock) activityOwnsFold() bool {
	return b != nil && len(b.activity) > 0 && b.collapsible &&
		!b.foldsBody && !b.tailFold
}

// -- the collapse row ---------------------------------------------------------

// activityRows draws the collapsed summary under a message, and the whole list
// when the reader has opened it.
//
// It is the LAST thing on the block, under the fold door of the body above it,
// because it is about the turn rather than about the words: what was said comes
// first, and how it was arrived at hangs underneath.
func (b *messageBlock) activityRows(rows []string, width int, pad string) []string {
	if len(b.activity) == 0 || width <= bodyIndent {
		return rows
	}
	summary := compressActivity(b.activity)
	if summary == "" {
		return rows
	}
	// A PLAIN LINE WHEN THE DOOR IS NOT THIS ROW'S — the accessible rendering,
	// and a block whose fold belongs to something else. A chevron drawn here
	// would be an affordance pointing at somebody else's fold, or at nothing.
	if !b.activityOwnsFold() {
		return append(rows, pad+b.foldRow(tokens.GlyphSeparator+" "+summary, width))
	}
	mark := blocks.CollapsedMark
	if b.expanded {
		mark = blocks.ExpandedMark
	}
	// The door's own row, recorded where it landed so the pointer resolves
	// against the frame on screen ([messageBlock.foldRowAt]).
	b.foldLine = len(rows)
	rows = append(rows, pad+b.foldRow(mark+" "+summary, width))
	if !b.expanded {
		return rows
	}
	for _, step := range b.activity {
		rows = append(rows, pad+b.activityReplayRow(step, width))
	}
	return rows
}

// activityReplayRow is one line of the opened list: the same marker grammar the
// live region drew, one step further in so it reads as belonging to the row that
// opened it.
func (b *messageBlock) activityReplayRow(step activityStep, width int) string {
	glyph := tokens.GlyphSettled
	switch {
	case !step.done:
		// A step with no ending is a call the turn was still making when it
		// ended — an interrupt, a lost stream. It is drawn as what it is.
		glyph = tokens.GlyphWorking
	case step.failed:
		glyph = tokens.GlyphFailed
	}
	text := step.gloss
	if step.hint != "" {
		text += " " + tokens.GlyphSeparator + " " + step.hint
	}
	indent := bodyIndent + partIndent
	row := strings.Repeat(" ", indent) + glyph + " " + text
	row = blocks.Truncate(row, width)
	if b.style == nil {
		return row
	}
	return b.style.PaintToken(row, tokens.TextTertiary)
}

// -- the compressor ------------------------------------------------------------

// compressActivity says a whole turn's activity in one line.
//
// `5 steps — searched twice, read the plan, put work in hand`
//
// THE COUNT IS EXACT AND THE PHRASES ARE NOT ALL OF THEM. That asymmetry is the
// honesty: the number says how much happened, and the clause says what KIND of
// thing happened, in the order it first happened, up to a bound. A summary that
// tried to list sixteen calls would be the log this row exists to replace.
//
// Every phrase is derived from the head's own gloss rather than from a second
// table here, so the words a reader saw scroll past and the words they read
// afterwards are the same vocabulary. What the derivation does is take the ACT
// off the front of the gloss (its subject is gone — «navctx» belongs to the row
// it happened on) and put it in the past tense, because the turn is over.
func compressActivity(steps []activityStep) string {
	if len(steps) == 0 {
		return ""
	}
	order := make([]string, 0, len(steps))
	counts := make(map[string]int, len(steps))
	failed := 0
	for _, step := range steps {
		if step.done && step.failed {
			failed++
		}
		act := pastAct(step.gloss)
		if act == "" {
			continue
		}
		if counts[act] == 0 {
			order = append(order, act)
		}
		counts[act]++
	}
	var line strings.Builder
	line.WriteString(plural(len(steps), "step", "steps"))
	phrases := make([]string, 0, activitySummaryActs)
	more := false
	for _, act := range order {
		if len(phrases) == activitySummaryActs {
			more = true
			break
		}
		phrases = append(phrases, act+timesSuffix(counts[act]))
	}
	if len(phrases) > 0 {
		line.WriteString(" — " + strings.Join(phrases, ", "))
		if more {
			// §16's ONE ELLIPSIS GRAMMAR, and NOT a fourth list item: what it
			// says is "the rest is off the edge", so it hangs off the last
			// phrase rather than being separated from it by a comma.
			line.WriteString(" " + tokens.GlyphEllipsis)
		}
	}
	if failed > 0 {
		line.WriteString(" " + tokens.GlyphSeparator + " " +
			strconv.Itoa(failed) + " didn't land")
	}
	return line.String()
}

// activitySummaryActs is how many kinds of act the row names before it stops.
// Three is what fits beside a count on a narrow terminal and still reads as a
// sentence rather than as a list.
const activitySummaryActs = 3

// timesSuffix says how often an act happened, and says nothing when it happened
// once — because "searched" already means "searched once" and a row that spelled
// it out would be §15's same-fact-twice.
func timesSuffix(n int) string {
	switch {
	case n <= 1:
		return ""
	case n == 2:
		return " twice"
	}
	return " " + strconv.Itoa(n) + " times"
}

// pastAct turns one gloss into the act it was, in the past tense.
//
// `searching for «navctx»`      → `searched`
// `reading the plan for «t-1»`  → `read the plan`
// `putting work in hand: «…»`   → `put work in hand`
// `looking at the work`         → `looked at the work`
//
// The subject is cut off at the quote or the colon that introduces it, then a
// dangling preposition is dropped, then the leading gerund is put in the past.
// A verb the table has never heard of keeps its gerund, which is a slightly
// awkward sentence and never a wrong one.
func pastAct(gloss string) string {
	act := strings.TrimSpace(gloss)
	if cut := strings.IndexAny(act, "«:"); cut >= 0 {
		act = strings.TrimSpace(act[:cut])
	}
	if act == "" {
		return ""
	}
	fields := strings.Fields(act)
	if last := len(fields) - 1; last > 0 && activityPrepositions[fields[last]] {
		fields = fields[:last]
	}
	if past, known := activityPastTense[fields[0]]; known {
		fields[0] = past
	}
	return strings.Join(fields, " ")
}

// activityPrepositions are the words a gloss ends on when its subject has been
// cut away. Dropping them is what turns "searching for" back into "searching".
var activityPrepositions = map[string]bool{
	"for": true, "at": true, "on": true, "to": true, "with": true,
	"through": true, "over": true, "of": true, "in": true, "from": true,
}

// activityPastTense is the whole irregular verb list this vocabulary needs. It
// is short because the gloss table it reads from is short (internal/head/
// activity.go), and it is a table rather than a rule because English is.
var activityPastTense = map[string]string{
	"searching":   "searched",
	"looking":     "looked",
	"reading":     "read",
	"opening":     "opened",
	"putting":     "put",
	"writing":     "wrote",
	"running":     "ran",
	"checking":    "checked",
	"changing":    "changed",
	"withdrawing": "withdrew",
	"saying":      "said",
	"asking":      "asked",
	"answering":   "answered",
	"doing":       "did",
	"letting":     "let",
	"stopping":    "stopped",
	"working":     "worked",
}
