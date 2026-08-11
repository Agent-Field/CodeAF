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
	// Hint is the trailing expand/collapse affordance; see [ExpandHint].
	Hint string
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

// ExpandHint is the shared fold affordance: "▸ 12 lines" collapsed,
// "▾" expanded. It is the only place those glyphs are chosen.
func ExpandHint(expanded bool, hidden int) string {
	if expanded {
		return "▾"
	}
	if hidden <= 0 {
		return "▸"
	}
	var buf [24]byte
	out := append(buf[:0], "▸ "...)
	out = strconv.AppendInt(out, int64(hidden), 10)
	if hidden == 1 {
		out = append(out, " line"...)
	} else {
		out = append(out, " lines"...)
	}
	return string(out)
}

const (
	sepDot     = " · "
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

// Render draws the header at width cells. It never panics and never returns a
// multi-row string; width <= 0 returns "".
func (h Header) Render(width int, s Styler) string {
	if width <= 0 {
		return ""
	}
	st := styler(s)

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
