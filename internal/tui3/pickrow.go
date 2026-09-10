package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// THE WALKABLE ANSWER ROW — one row of answers a person can WALK, and one line
// under it saying what the answer they are on will do.
//
//	[ 1 yes, set it up ]  [ 2 change when or where ]  [ 3 just once ]  [ 0 no ]
//	I'll keep doing this every Monday at 9am, everywhere, until you stop it
//
// ── WHY IT IS A MECHANISM AND NOT A CARD'S PRIVATE LAYOUT ──
//
// Every question this surface asks is the same shape underneath: a short row of
// answers, one of which the keyboard is on, each with a key that takes it
// outright. The stand card is the first caller and deliberately not the last —
// the consent gate, the connect offer and the subharness offers all draw a row of
// answers of their own today, each with its own layout loop — so the arithmetic
// (what fits, what is dropped, where each answer landed in screen columns) lives
// here once.
//
// ── TWO WAYS IN, AND THE SLOW ONE IS NOT SECOND CLASS ──
//
// docs/DESIGN-LANGUAGE.md refuses keyboard speed bought at discoverability's
// expense: every chord keeps a visible, clickable, self-teaching door beside it.
// So the keys stay ON the chips for the hand that already knows them, AND the
// row is walked with ←/→ and taken with enter for the hand that does not — and
// the line under it reads out what the answer under the cursor will actually do,
// so walking the row is a way of READING the question rather than a way of
// guessing at it.
//
// ── THE PICKED ANSWER IS EMPHASIZED, NEVER OUTLINED ──
//
// THE EMPHASIS LAW: a thing is emphasized by raising its ground and turning its
// leading text accent, and nothing else ever changes. So the picked chip takes
// the ground ladder's selected step behind exactly its own cells and its KEY —
// the leading text inside it — turns accent. No new colour arrives, no ring is
// drawn round it, and the row does not reflow when the cursor moves, because
// every chip is drawn at exactly the same width picked or not.
//
// ── AND THE WAY OUT IS THE ONE CHIP THAT IS NEVER DROPPED ──
//
// A chip that does not fit is DROPPED rather than truncated — half an answer is
// an answer somebody presses by mistake — and that rule, left alone, drops from
// the RIGHT, which is exactly where a decline sits. A narrow window would then
// be the one place with no visible way to say no. So a caller may name one
// answer as the way out ([app.pickRow]'s `keep`), its room is taken off the top,
// and the answers before it compete for what is left.

// pickChoice is one answer on the row: the key that takes it outright, the word
// a person reads, and the SHORT spelling of that word for a row with no room for
// the long one.
//
// THE SHORT SPELLING IS NOT A TRUNCATION AND IT IS NOT OPTIONAL POLISH. A narrow
// window is where a person is most likely to be missing an answer they need, and
// the choice a row makes there is between saying every answer briefly and saying
// some of them fully — the consent gate settled that question first
// ([app.questionOffer]) and settled it the same way. A choice with no short
// spelling simply keeps its word at both lengths.
type pickChoice struct{ key, word, short string }

// spelling is this answer's word at one of the two lengths.
func (c pickChoice) spelling(long bool) string {
	if long || c.short == "" {
		return c.word
	}
	return c.short
}

// pickGap is the air between two chips. It is the clause step of the spacing
// ladder said sideways: two cells, everywhere on this surface a chip stands
// beside another one.
const pickGap = 2

// pickChipText is one chip's cells, brackets and key included. It is the one
// place the bracket idiom is spelled, so the width the layout reserves and the
// string the painter draws can never be two different chips.
func pickChipText(c pickChoice, long bool) string {
	return "[ " + c.key + " " + c.spelling(long) + " ]"
}

// pickRowCells is what a whole row of answers would occupy at one spelling,
// gaps included. It is what decides whether the long words fit.
func pickRowCells(choices []pickChoice, long bool) int {
	cells := 0
	for i, choice := range choices {
		if i > 0 {
			cells += pickGap
		}
		cells += ansi.StringWidth(pickChipText(choice, long))
	}
	return cells
}

// pickRow lays the answers out from column `left` inside `width` cells and
// reports the string and where each answer landed, so a click can be resolved
// to the answer under it.
//
// `picked` is the index the keyboard is on. `keep` is the index of the way out —
// the one answer that is never dropped for want of room — or -1 where the row
// has none; it must be the LAST answer, because a way out drawn anywhere else
// would be a chip that moved when the row narrowed.
func (a *app) pickRow(choices []pickChoice, picked, keep, left, width int) (string, []choiceSpan) {
	if len(choices) == 0 || width < 1 {
		return "", nil
	}
	// THE LONG WORDS IF THEY ALL FIT, AND THE SHORT ONES BEFORE ANY ANSWER IS
	// DROPPED. Losing a word off an answer costs a person a little reading;
	// losing the answer costs them the answer.
	long := pickRowCells(choices, true) <= width
	// THE WAY OUT'S ROOM IS TAKEN OFF THE TOP, so what the earlier answers
	// compete for is what is left after it.
	reserve := 0
	if keep >= 0 && keep < len(choices) {
		reserve = pickGap + ansi.StringWidth(pickChipText(choices[keep], long))
	}
	var line string
	var spans []choiceSpan
	at, end := left, left+width
	// The row is drawn LEFT TO RIGHT AND STOPS AT THE FIRST ANSWER THAT DOES NOT
	// FIT, rather than skipping it and taking a shorter one after it: a row
	// reading `1 … 3 …` with the two silently gone is a row whose keys have holes
	// in them, and a person counting chips would press the wrong digit.
	draw := func(i int, limit int) bool {
		chip := pickChipText(choices[i], long)
		gap := 0
		if len(spans) > 0 {
			gap = pickGap
		}
		if at+gap+ansi.StringWidth(chip) > limit {
			return false
		}
		if gap > 0 {
			line += strings.Repeat(" ", gap)
			at += gap
		}
		line += a.pickChip(choices[i], long, i == picked)
		spans = append(spans, choiceSpan{from: at, to: at + ansi.StringWidth(chip), at: i})
		at += ansi.StringWidth(chip)
		return true
	}
	for i := range choices {
		if i == keep {
			continue
		}
		if !draw(i, end-reserve) {
			break
		}
	}
	if keep >= 0 && keep < len(choices) {
		draw(keep, end)
	}
	return line, spans
}

// pickChip is one answer, painted.
//
// The picked one takes THE EMPHASIS LAW's two moves and no third: the ground
// ladder's selected step behind its own cells, and the accent on its key, which
// is the leading text inside the brackets. The others keep the question hue and
// spend their weight on the key that takes them — the digit rather than an
// initial, because two answers to one question rarely start with different
// letters a person would guess ([taskModelChip] made the same trade first).
//
// Below the 256-colour rung there is no ground to raise and what separates the
// picked chip is the accent on its key, which is the trade every other lifted
// run of cells on this surface already makes ([palette.background]).
func (a *app) pickChip(c pickChoice, long, picked bool) string {
	word := " " + c.spelling(long) + " ]"
	if !picked {
		return a.pal.dim("[ ") + a.pal.askBold(c.key) + a.pal.ask(word)
	}
	inner := a.pal.dim("[ ") + a.pal.bold(a.pal.accent(c.key)) + a.pal.ask(word)
	return a.pal.background(inner, 0, a.pal.ramp.selected)
}
