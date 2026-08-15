package blocks

import "strconv"

// Header is THE header grammar (8.1.5), defined in exactly one place:
//
//	<glyph> <Title>: <desc> [badge] · meta · meta
//
// Every block renderer goes through it. Ad-hoc headers are a review reject —
// that is the point of the law, and the reason the badge, fold and expand-hint
// helpers live here too.
//
// The header is always exactly one row. Every field is newline-flattened, so a
// title carrying a stray "\n" cannot smuggle a second row into a block whose
// height the cache has already recorded.
//
// Under width pressure the header degrades in a fixed order — meta from the
// right, then the expand hint, then non-sticky badges, then the description,
// then the title. The truncation-law badge is sticky, so a cut turn still says
// it was cut on a 30-column terminal.
type Header struct {
	// Glyph is the state vocabulary of 5.17: ○ queued, ◐ working, ✓ settled,
	// ✕ failed, ? waiting on a human, ⚑ waiting on a sibling. Shape encodes
	// the state CATEGORY and changes only at a true transition; it never
	// animates on a long-lived row (8.1.6).
	Glyph string
	// GlyphHue tints the glyph — the identity pastel for a task, amber for a
	// question, coral for a failure.
	GlyphHue Hue
	// GlyphSeed is the task identity behind a [HueIdentity] glyph: the seed
	// the token layer runs through the 8-hue wheel of 5.16, so this header's
	// glyph is the same pastel as that task's rail card and breadcrumb dot.
	// Take it from [Seed] over the task id and the two agree by construction.
	//
	// It matters because HueIdentity is not a colour: resolved without a seed
	// it can only yield the wheel's first entry, so every task's header glyph
	// comes out identically painted and the peripheral answer to "which room
	// am I in" is wrong. A renderer holding a task id used to have to reach
	// around this grammar and pre-paint the glyph itself, which is an ad-hoc
	// header by another name (8.1.5 forbids exactly that). This field is the
	// door through the grammar instead of around it.
	//
	// Zero means no identity is carried, and then nothing changes: the glyph
	// is painted through [Styler.Paint] on the hue and state axes exactly as
	// before. The seed is only consulted when it is non-zero, the hue is
	// [HueIdentity], and the Styler implements [IdentityStyler].
	GlyphSeed uint64
	// State is the liveness of the row: accent while live, plain once settled.
	// It carries the glyph and the title.
	State State
	// Title is the primary text.
	Title string
	// Desc is the secondary clause after the colon.
	Desc string
	// Badges are the bracketed chips after the description.
	Badges []Badge
	// Meta are the dot-separated telemetry cells.
	Meta []string
	// Hint is the trailing expand/collapse affordance; see [Disclose].
	Hint string
	// Receipt is the row's telemetry, RIGHT-ALIGNED to the right edge instead
	// of trailing the title — §16's first rule ("the right edge is a column:
	// receipts right-align to one shared column per surface, so scanning down
	// reads like a table without being one").
	//
	// It is the ONE cell that does not degrade meta-first, and that inversion
	// is the whole reason the field exists. Everything else on this row sheds
	// from the right under width pressure, which is correct for telemetry that
	// merely accompanies a title — and wrong for a column, because a column
	// that moves is not a column. Measured: a batch row whose title ran to the
	// edge at 88 columns pushed its size cell off entirely while the row beside
	// it kept one, so a reader scanning for sizes read a ragged list with holes
	// in it and no way to tell an absent size from a dropped one.
	//
	// The reservation is [CardBlock.titleLine]'s, deliberately word for word:
	// the receipt is taken out of the row FIRST and the rest of the header
	// degrades into what is left, and below [minHeadRoom] cells for the header
	// it is DROPPED WHOLE rather than cut, because half a receipt is not a
	// smaller truth about a number, it is a false one.
	//
	// [Header.Hint] does NOT ride here, and that is a decision rather than an
	// omission. A receipt is a figure a reader SCANS — down a column, across
	// rows — and a door is something they AIM AT, which wants to sit where the
	// eye already is: immediately after the words it opens, not across a gulf of
	// empty cells. So the hint stays in the flow and the column stays telemetry.
	//
	// Empty is the zero value and changes NOTHING: the row is rendered by the
	// same code, at the same width, into the same bytes it produced before this
	// field existed.
	Receipt string
	// End marks how a finalized block ended. Anything other than
	// [EndCompleted] adds a sticky badge — the truncation law's first half
	// (12.5); [CutRule] is the second half, drawn under the body.
	End EndState
}

// Badge is a bracketed chip in the header.
type Badge struct {
	Text  string
	State State
	Hue   Hue
	// Sticky badges survive width pressure. Use it for facts a reader must
	// not lose on a narrow terminal — that a turn was cut, that a question is
	// open.
	Sticky bool
}

// CountBadge is the compact count chip of 5.21: ?2, ✕3.
func CountBadge(glyph string, n int, hue Hue) Badge {
	if n <= 0 {
		return Badge{}
	}
	var buf [24]byte
	out := append(buf[:0], glyph...)
	out = strconv.AppendInt(out, int64(n), 10)
	return Badge{Text: string(out), Hue: hue, State: StateSettled}
}

// The disclosure marks of §10's expand law: one chevron, two directions, and
// they mean nothing else anywhere in the product. Twins of
// tokens.GlyphExpanded and tokens.GlyphCollapsed, held equal by a pin the way
// [CutMark] and [OverflowMark] are — blocks stays a leaf, so the bytes live
// here as well as there.
//
// Both are Ambiguous width and one cell under both shipping rulers.
const (
	// ExpandedMark points down at rows that are already showing.
	ExpandedMark = "▾"
	// CollapsedMark points right at rows a click would reveal.
	CollapsedMark = "▸"
)

// ONE DISCLOSURE GRAMMAR, and these two functions are the whole of it.
//
// One mark, one count, one unit, and no verb — §15's delete test applied to a
// door. Take the mark away and the row stops saying which way it faces; take the
// count away and the reader cannot tell a fold over two lines from one over two
// hundred; take the unit away and the number is unattached. Take the VERB away
// and nothing is lost, which is exactly why there must not be one: `▸ view more
// (12 lines)` says "more" twice and spends a whole clause teaching a chevron
// every other row in the product already taught.
//
// The product had grown four spellings of one door — `▸ 12 lines`, `▸ view more
// (12 lines)`, `▾`, and a bare `▸` beside a word — which is four things for a
// reader to learn about one gesture, and 5.20 rule 3's affordance honesty asked
// of a fold: the door must look the same everywhere it is, or it is not one
// door.

// Disclose is the fold affordance: `▸ 12 lines` shut, `▾` open.
//
// The UNIT is the caller's because the unit is a fact about what is folded, not
// about folding: a transcript hides LINES, a subtree hides PARTS, a batch hides
// CALLS, and §14's vocabulary is a product law rather than this package's guess.
// Both spellings are taken so the singular is not assembled by trimming an `s`
// off a word that might not end in one.
//
// An OPEN door says only which way it faces. There is nothing to count once the
// rows are on screen, and a count beside them would be the surface narrating
// what the reader is already looking at (§15).
func Disclose(open bool, n int, unit, units string) string {
	if open {
		return ExpandedMark
	}
	if n <= 0 {
		return CollapsedMark
	}
	var buf [32]byte
	out := append(buf[:0], CollapsedMark+" "...)
	out = strconv.AppendInt(out, int64(n), 10)
	out = append(out, ' ')
	if n == 1 {
		out = append(out, unit...)
	} else {
		out = append(out, units...)
	}
	return string(out)
}

// DiscloseSection is [Disclose] for a NAMED band rather than a folded tail:
// `▸ history (18)`, `▾ history (18)`.
//
// The two differ in one thing and it is the thing that matters. A tail's door is
// drawn where the hidden rows would be, so the count IS the subject and stands
// bare. A section's door is drawn on the section's own heading, so the WORD is
// the subject and the count is a parenthetical about it — §20's hints grammar,
// which puts a figure that qualifies a phrase in dim parentheses on the end of
// it. Which is also why the count survives when the section is open: it is a
// fact about the band, not about the fold, and `history (18)` is as true with
// the rows showing as without them.
func DiscloseSection(open bool, word string, n int) string {
	mark := CollapsedMark
	if open {
		mark = ExpandedMark
	}
	if word == "" {
		return mark
	}
	if n <= 0 {
		return mark + " " + word
	}
	var buf [48]byte
	out := append(buf[:0], mark...)
	out = append(out, ' ')
	out = append(out, word...)
	out = append(out, " ("...)
	out = strconv.AppendInt(out, int64(n), 10)
	out = append(out, ')')
	return string(out)
}

const (
	sepDot     = " " + SeparatorMark + " "
	titleColon = ": "
	badgeLead  = " ["
	badgeTail  = "]"
	hintLead   = "  "

	// Cell widths, not byte lengths: the separator's middle dot is two bytes
	// and one cell, and measuring it in bytes would spend a column the row
	// actually has.
	sepDotWidth     = 3
	titleColonWidth = 2
	badgeLeadWidth  = 2
	badgeTailWidth  = 1
	hintLeadWidth   = 2

	minDescWide = 4
)

// receiptGulfMax is how many empty cells a right-aligned receipt may sit behind
// before it stops being a column and becomes §20's named defect.
//
// §20 permits the shared right column as ONE of exactly three receipt
// placements, and it permits it under a condition rather than generally: "ONLY
// inside dense same-shaped lists in a narrow pane (rail, palette), where every
// row has one AND THE COLUMN IS CLOSE." This constant is that last clause made
// enforceable. A rail row at 30 cells clears it and keeps its column; a trace
// row whose command is four bytes long at 140 cells does not, and its `~1.5k
// tok` comes back to the line it describes instead of being flung a hundred
// cells away from it — which is, in §20's own words, "the defect the user has
// now flagged twice".
//
// The fallback is placement 1 and not "drop it": the receipt rides inline in
// dim parentheses immediately after the row, `… ▸ 2 lines (~1.5k tok)`, so no
// figure is lost and the reader's eye never leaves the subject.
//
// Sixteen is a scan, not a taste: it is the width of the widest receipt this
// product composes (`4m · $0.27 · ~86K tok`) rounded down to the indent grid, so
// the gap may never exceed the thing it is separating.
const receiptGulfMax = 16

// receiptOpen and receiptClose wrap the inline form. §20 spells it with
// parentheses so an inline receipt cannot be misread as more of the sentence.
const (
	receiptOpen  = " ("
	receiptClose = ")"

	// receiptInlineCost is what the inline form costs beyond the receipt's own
	// cells: the leading space, both parentheses.
	receiptInlineCost = 3
)

// Render draws the header at width cells. It never panics and never returns a
// multi-row string; width <= 0 returns "".
//
// A [Header.Receipt] is reserved out of the row before anything else is
// measured and drawn hard against the right edge; the rest of the grammar then
// degrades inside what is left, exactly as it always did. A header with no
// receipt takes the same path it took before the field existed, at the same
// width, and is byte-identical.
func (h Header) Render(width int, s Styler) string {
	if width <= 0 {
		return ""
	}
	st := styler(s)

	receipt, region, gap := flatten(h.Receipt), width, 0
	if w := stringWidth(receipt); w > 0 && width-w-receiptGap >= minHeadRoom {
		region, gap = width-w-receiptGap, receiptGap
	} else {
		receipt = ""
	}
	row := h.flow(region, st)
	if receipt == "" {
		return row
	}
	var b builder
	b.grow(len(row) + len(receipt) + 32)
	b.WriteString(row)
	// §20's condition on the right column: it is a column only while it is
	// CLOSE. Past [receiptGulfMax] empty cells the figure comes back to its
	// subject in parentheses rather than being flung at the right edge.
	//
	// No re-flow is needed to do it, and that is arithmetic rather than luck:
	// the row already fits in `region`, and the branch only fires when it is
	// more than receiptGulfMax cells short of it, so the inline form's extra
	// receiptInlineCost cells cannot reach the right edge.
	if gulf := region - stringWidth(row); gulf > receiptGulfMax {
		// The brackets and the figure are painted SEPARATELY, so the receipt is
		// still one styled span of its own: a surface asserting what tier its
		// telemetry wears looks for the figure, and a figure glued to its
		// punctuation inside one span is a figure no such check can find.
		b.styled(st, receiptOpen, StateChrome, HueNone)
		b.styled(st, receipt, StateChrome, HueNone)
		b.styled(st, receiptClose, StateChrome, HueNone)
	} else {
		b.WriteString(repeat(spaceMark, gulf+gap))
		b.styled(st, receipt, StateChrome, HueNone)
	}
	out := b.String()
	if stringWidth(out) > width {
		out = truncate(out, width)
	}
	return out
}

// flow draws the header grammar itself — glyph, title, description, badges,
// meta, hint — into the cells [Header.Render] left it.
func (h Header) flow(width int, st Styler) string {
	if width <= 0 {
		return ""
	}
	glyph := flatten(h.Glyph)
	title := flatten(h.Title)
	desc := flatten(h.Desc)
	hint := flatten(h.Hint)

	glyphW := stringWidth(glyph)
	if glyphW > 0 {
		glyphW++ // the space after it
	}
	titleW := stringWidth(title)
	descW := 0
	if desc != "" {
		descW = titleColonWidth + stringWidth(desc)
	}

	// Badges: the truncation mark rides in front of the caller's own, sticky.
	badges := h.badgeList()
	badgeW := 0
	// keep is a bitmask rather than a []bool so a header costs no allocation
	// of its own; a row with more than 64 badges is not a row.
	var keep uint64 = ^uint64(0)
	if len(badges) > 64 {
		badges = badges[:64]
	}
	for _, b := range badges {
		badgeW += badgeLeadWidth + stringWidth(b.Text) + badgeTailWidth
	}

	metaKeep := len(h.Meta)
	metaW := 0
	for _, m := range h.Meta {
		metaW += sepDotWidth + stringWidth(flatten(m))
	}

	hintW := 0
	if hint != "" {
		hintW = hintLeadWidth + stringWidth(hint)
	}

	total := glyphW + titleW + descW + badgeW + metaW + hintW

	// Degrade in the fixed order.
	for total > width && metaKeep > 0 {
		metaKeep--
		w := sepDotWidth + stringWidth(flatten(h.Meta[metaKeep]))
		metaW -= w
		total -= w
	}
	if total > width && hintW > 0 {
		total -= hintW
		hintW, hint = 0, ""
	}
	for i := len(badges) - 1; i >= 0 && total > width; i-- {
		if badges[i].Sticky {
			continue
		}
		keep &^= 1 << uint(i)
		w := badgeLeadWidth + stringWidth(badges[i].Text) + badgeTailWidth
		badgeW -= w
		total -= w
	}
	if total > width && descW > 0 {
		room := width - (total - descW) - titleColonWidth
		if room < minDescWide {
			total -= descW
			descW, desc = 0, ""
		} else {
			desc = truncate(desc, room)
			newW := titleColonWidth + stringWidth(desc)
			total += newW - descW
			descW = newW
		}
	}
	if total > width && titleW > 0 {
		room := width - (total - titleW)
		if room < 1 {
			room = 1
		}
		title = truncate(title, room)
		total += stringWidth(title) - titleW
		titleW = stringWidth(title)
	}

	var b builder
	b.grow(width * 2)
	if glyph != "" {
		b.identity(st, glyph, h.State, h.GlyphHue, h.GlyphSeed)
		b.WriteByte(' ')
	}
	if title != "" {
		b.styled(st, title, h.State, HueNone)
	}
	if desc != "" {
		b.styled(st, titleColon, StateChrome, HueNone)
		b.styled(st, desc, StateChrome, HueNone)
	}
	for i, badge := range badges {
		if keep&(1<<uint(i)) == 0 || badge.Text == "" {
			continue
		}
		b.styled(st, badgeLead, StateChrome, HueNone)
		b.styled(st, badge.Text, badge.State, badge.Hue)
		b.styled(st, badgeTail, StateChrome, HueNone)
	}
	for i := 0; i < metaKeep; i++ {
		b.styled(st, sepDot, StateChrome, HueNone)
		b.styled(st, flatten(h.Meta[i]), StateChrome, HueNone)
	}
	if hint != "" {
		b.styled(st, hintLead, StateChrome, HueNone)
		b.styled(st, hint, StateChrome, HueNone)
	}
	// Backstop: a Styler that lied about widths, or a single glyph wider than
	// the terminal, still cannot produce an over-wide row.
	out := b.String()
	if stringWidth(out) > width {
		out = truncate(out, width)
	}
	return out
}

// badgeList prepends the truncation-law badge to the caller's badges. It is
// sticky, because "this answer stops short" is never the thing to drop.
func (h Header) badgeList() []Badge {
	mark := h.End.Mark()
	if mark == "" {
		return h.Badges
	}
	out := make([]Badge, 0, len(h.Badges)+1)
	out = append(out, Badge{Text: mark, State: StateChrome, Hue: h.End.Hue(), Sticky: true})
	return append(out, h.Badges...)
}
