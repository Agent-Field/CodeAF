package blocks

// §16 admits exactly two ruled lines and this is the renderer for the second
// one: the label IS the rule, one row that separates and names at once — so a
// surface that wants a titled boundary gets this rather than growing an eighth
// spelling of a dash.
//
// The first ruled line is the bare hairline at a room boundary ([Rule], and
// [CutRule] under a severed block). The second is this one, [Ruled]:
//
//	─ execution ────────────────────────────────────────────────
//	─ wisp-parity-2 · 28m · $8.65 · 41k tok ─────────────────────
//
// WHY IT LIVES HERE rather than in the surface that first wanted it. It was
// written four times before it was written once — the record's `── execution ──`
// seam, the card's inset seam, the dialog ring's rule and the transcript's
// hairline were four spellings of one stroke, and two of them had already picked
// different lead widths. blocks is the leaf every renderer already depends on,
// so a stroke declared here is a stroke every consumer inherits.
//
// THE SECTION-WORD LAW, which is what a surface actually has to choose between,
// and it has exactly three answers. A band inside one list gets a FAINT
// LOWERCASE WORD at [ContentEdge] with [SectionAbove] blank above it and none
// below — no glyph, because a heading is not a state and §15's delete test takes
// the mark away without losing anything. A band the reader can OPEN gets that
// word promoted to a door ([DiscloseSection]), because the chevron is what makes
// it interactive and 5.22 forbids an interactive control living permanently in
// the dimmest tier. A boundary between two DIFFERENT KINDS of thing — an answer
// and the execution under it, a job and its record — gets [Ruled], because that
// is the one case where the eye needs a line and not just a label.

// RuleMark is the rule stroke of the two ruled lines §16 allows — the bare
// hairline, the cut rule under a severed block, and the word-in-line seam.
// Twin of tokens.GlyphTreeDash: blocks cannot import tokens (the edge runs
// tokens → blocks so blocks stays a leaf), so the byte lives here as well as
// there and a drift is a failed test rather than two strokes for one meaning.
//
// U+2500 is Neutral width: one cell everywhere.
const RuleMark = "─"

const (
	// RuleLead is how many marks stand in FRONT of a titled rule's word: ONE,
	// so the word itself lands at [ContentEdge] and lines up with every other
	// title, sentence and row name on the surface.
	//
	// The shipped `── execution ──` seam used two, which put its word at column
	// 3 — off §20's ladder entirely, and the one row on the record page that did
	// not line up with the rows above and below it. A rule's job is to separate,
	// and a separator that shifts the column its own label hangs in is doing the
	// opposite of the grid's work (§16 ALIGNMENT).
	RuleLead = 1

	// RuleFloor is the shortest tail of marks that still reads as a rule. Below
	// it the stroke stops being a boundary and becomes a smudge after a word, so
	// the title gives way instead — and if it cannot, the whole thing degrades
	// to the bare [Rule], which is honest at any width.
	RuleFloor = 4
)

// ruleGap is the one space on each side of a titled rule's words. It is named
// because both sides are written in separate calls and a gap that lived in each
// of them would be a gap that could differ between them.
const ruleGap = 1

// Rule is the bare hairline: `width` marks at the chrome tier, and nothing else.
// It never panics and never returns a multi-row string; width <= 0 returns "".
func Rule(width int, s Styler) string {
	if width <= 0 {
		return ""
	}
	return styler(s).Paint(repeat(RuleMark, width), StateChrome, HueNone)
}

// Ruled is the word-in-line seam: a hairline opened for the name of what it
// separates, and — where the boundary carries figures — that name's receipt.
//
// The whole row is ONE row and is drawn at ONE tier band: the marks and the meta
// are chrome, and only the title carries [Ruled.State], because the title is the
// only part of a rule that is content. There is no state glyph and there must
// not be: §15's delete test asks what the row would lose without the mark, and a
// boundary that also claimed a lifecycle would be claiming it for two different
// things at once — the section above the rule and the section below it.
type Ruled struct {
	// Title is the section's word, lowercase (§16's CASE) and short. It hangs at
	// [ContentEdge] by construction.
	Title string
	// Meta are the telemetry cells that follow the title, separated by
	// [SeparatorMark] and drawn at the chrome tier. They shed WHOLE from the
	// right under width pressure — half a receipt is not a smaller truth about a
	// number, it is a false one.
	Meta []string
	// State is the tier the title is painted at. Settled (the zero value) is a
	// named boundary the reader can read; [StateChrome] is a seam that is only
	// there for the eye that looks for it.
	State State
}

// Render draws the ruled line at width cells.
//
// The geometry is fixed and there is no second arrangement of it:
//
//	[RuleLead marks][space][Title][ · meta]*[space][marks to width]
//
// Degradation runs in one order — meta shed whole from the right, then the title
// truncated while [RuleFloor] trailing marks are kept, then the bare [Rule].
// A row is never wider than width and never carries a newline.
func (r Ruled) Render(width int, s Styler) string {
	if width <= 0 {
		return ""
	}
	st := styler(s)
	title := flatten(r.Title)
	if title == "" {
		return Rule(width, st)
	}
	// The floor: the lead marks, both gaps, one cell of title and a tail that
	// still reads as a rule. Under it there is no titled row to draw and the
	// bare hairline is the honest answer.
	lead := RuleLead + ruleGap
	if width < lead+1+ruleGap+RuleFloor {
		return Rule(width, st)
	}
	room := width - lead - ruleGap - RuleFloor

	keep := len(r.Meta)
	metaW := 0
	for _, cell := range r.Meta {
		metaW += sepDotWidth + stringWidth(flatten(cell))
	}
	titleW := stringWidth(title)
	for keep > 0 && titleW+metaW > room {
		keep--
		metaW -= sepDotWidth + stringWidth(flatten(r.Meta[keep]))
	}
	if titleW+metaW > room {
		title = truncate(title, room-metaW)
		titleW = stringWidth(title)
	}

	var b builder
	b.grow(width*2 + 32)
	b.styled(st, repeat(RuleMark, RuleLead), StateChrome, HueNone)
	b.styled(st, spaceMark, StateChrome, HueNone)
	b.styled(st, title, r.State, HueNone)
	for i := 0; i < keep; i++ {
		b.styled(st, sepDot, StateChrome, HueNone)
		b.styled(st, flatten(r.Meta[i]), StateChrome, HueNone)
	}
	b.styled(st, spaceMark, StateChrome, HueNone)
	if tail := width - lead - titleW - metaW - ruleGap; tail > 0 {
		b.styled(st, repeat(RuleMark, tail), StateChrome, HueNone)
	}
	// Backstop: a Styler that lied about widths still cannot produce an
	// over-wide row.
	out := b.String()
	if stringWidth(out) > width {
		out = truncate(out, width)
	}
	return out
}
