package tui3

import (
	"strings"
)

// THE READING GUTTER: TWO CELLS OF AIR DOWN THE LEFT OF EVERYTHING A PERSON READS.
//
// The transcript used to start on screen column zero. Every other block of prose
// this product draws is inset from the edge it sits against — the rail's own
// column, the home bands, a room's header — and the one surface a person
// actually reads sentences on was the one flush against the terminal's frame,
// with the first letter of every line touching the window's border. A paragraph
// with nothing to its left is a paragraph the eye has to find the start of on
// every wrap, and it is the commonest complaint about this screen.
//
// So the conversation, a task's page, and the transcript of a run's node are all
// laid out two columns narrower and then moved two columns right. It is
// [spacingConversationLead] and not a new number, because it is the same step of
// the same ladder the person's own message and a note's marker already take —
// one gutter width on this surface, not two.
//
// ── WHY IT IS ONE PASS, AFTER LAYOUT ──
//
// For [app.deckRows]'s indent law's reason, stated there in full: a rule applied
// once, to finished rows, is obeyed by every kind of machinery including the
// synthetic rows — the fold door, the "earlier" marker, the ellipsis, a room's
// foot — that no block owns. Thirty renderers that each remembered to add two
// spaces would be thirty places for one of them to forget.
//
// ── WHAT IT COSTS, AND THE ONE THING THAT MUST MOVE WITH IT ──
//
// The rows are laid out at [gutterInner] so that a line built to the full width
// and then shoved right is not two cells wider than the column it is drawn in —
// [app.railJoin] cuts that overhang back with an ellipsis, which is where a
// running call's spinner went when the indent law made the same mistake.
//
// And EVERY TARGET THIS SURFACE RESOLVES BY COLUMN MOVES WITH THE TEXT. There
// are seven of them and they arrive in two shapes:
//
//   - ON THE ROW ITSELF — a link's span, a cut table's foot, a bash row's
//     `click to background` clause (render.go's [row]). These are minted fresh
//     into a throwaway row on every pass, so [gutterPass] moves them outright.
//   - ON A CARD THAT OUTLIVES THE PASS — a proposal's answers and its models
//     (task.go's [taskCard]), a standing order's chips (standing.go), and the
//     answers of a node that needs a look (taskdone.go's [taskDone]). A card's
//     rows can come back from the entry cache with its spans untouched, so
//     [app.gutterCards] moves them by the DIFFERENCE between the gutter they
//     already carry and the one this frame wants — which is why each of the
//     three carries a `gut`.
//
// Every one of them is compared against the RAW screen x — [app.linkPress],
// [app.footPress], [app.keepPress], [app.choicePress], [app.standingPress],
// [app.settlePress] — because the transcript's left edge WAS screen column
// zero. Shift the text without shifting the spans and every task reference and
// every `[ yes ]` on the screen answers a click two columns to its left, which
// is the defect this file exists to not have.

// textGutterCols is how many columns the reading gutter takes at this width, and
// it is asked at layout and again at the pass that applies it for
// [workIndentCols]'s reason: the two must never be able to disagree.
//
// A PHONE IN A TERMINAL PAYS NOTHING. Below [tierPhone] the frame has no columns
// to spend on air — the indent law already gives its two back there — so the
// gutter goes rather than force another line wrap. The test is made
// against the width the rows would be laid out at rather than the width the
// frame has, so the gutter can never be the thing that pushes a frame down a
// tier: at sixty-one columns it would buy two cells of air and pay for them by
// collapsing every tool row's indent, which is a worse screen than the one it
// was mending.
func textGutterCols(width int) int {
	if layoutTier(width-spacingConversationLead) == tierPhone {
		return 0
	}
	return spacingConversationLead
}

// gutterInner is the width the rows are laid out at: what is left of the body's
// column once the gutter has taken its own. It never goes below one, because a
// wrap at zero is a wrap that never terminates.
func gutterInner(width int) int {
	if inner := width - textGutterCols(width); inner > 0 {
		return inner
	}
	return width
}

// gutterPass moves finished rows into the gutter — the text, and the three spans
// a click is resolved by.
//
// A BLANK ROW STAYS BLANK. Two spaces on a row with nothing on it is two cells of
// trailing whitespace on every gap in the transcript, which a copy takes with it
// and a terminal's own selection shows as a smear down the empty column.
func gutterPass(out []row, width int) {
	lead := textGutterCols(width)
	if lead == 0 {
		return
	}
	pad := strings.Repeat(" ", lead)
	for i := range out {
		if out[i].text == "" {
			continue
		}
		out[i].text = pad + out[i].text
		for j := range out[i].links {
			out[i].links[j].span = out[i].links[j].span.shift(lead)
		}
		out[i].foot.span = out[i].foot.span.shift(lead)
		out[i].keep = out[i].keep.shift(lead)
		out[i].pictureOpen = out[i].pictureOpen.shift(lead)
	}
}

// shift moves a span right by n columns, and leaves an empty one empty: a span
// with no cells in it is the absence of a target, and an absence at column two
// is still an absence.
func (s hudSpan) shift(n int) hudSpan {
	if !s.pressable() {
		return s
	}
	return hudSpan{from: s.from + n, to: s.to + n}
}

// gutterCards moves the answers of the three cards that keep their own geometry
// into the gutter the rows around them have just been moved into.
//
// IT IS A DIFFERENCE AND NOT AN ADDITION, which is the whole of why the cards
// carry a `gut`. A card whose rows came back from the entry cache did not
// re-mint its spans this pass — a settled proposal is cached exactly so its
// clock stops turning — and a pass that added two cells unconditionally would
// walk that card's chips two columns further right on every frame until they
// were off the end of the row. Each renderer sets its card's `gut` back to zero
// beside the line that clears the old spans, so "what this card already carries"
// is a fact the card states rather than one this pass has to infer.
func (a *app) gutterCards(d deck, width int) {
	lead := textGutterCols(width)
	for i := range d.entries {
		e := &d.entries[i]
		gutStandingCard(e.stand, lead)
	}
}

func gutStandingCard(card *standingCard, lead int) {
	if card == nil || card.gut == lead {
		return
	}
	shiftChoiceSpans(card.spans, lead-card.gut)
	card.gut = lead
}

// shiftChoiceSpans is [hudSpan.shift] for the other span shape this surface
// uses. The two stay two types because a choice carries WHICH answer it is
// (task.go's [choiceSpan]) and a span on a row carries nothing but its columns.
func shiftChoiceSpans(spans []choiceSpan, by int) {
	if by == 0 {
		return
	}
	for i := range spans {
		spans[i].from += by
		spans[i].to += by
	}
}
