package blocks

import "strings"

// The delivery card: the ONE template a finished answer wears.
//
// This is the only element in the transcript that gets a ground and a left
// accent edge of its own, and the exclusivity IS the information. 4.3 says
// "settled deliverable cards stay inline at birth position", and that sentence
// only reads as a card if nothing else on screen is drawn like one: the moment
// a second kind of row wears the edge, the edge stops meaning "a finished
// answer" and becomes decoration. So this type exists to make the treatment
// UNREPEATABLE, not to make it available — a surface that wants a row grouped
// but not delivered has [TextBlock] and the header grammar, and a review that
// finds an ad-hoc accent edge should reject it the way 8.1.5 rejects an ad-hoc
// header.

// AccentEdge is the left accent rail of 5.21: a hue-tinted edge that groups a
// card's rows without drawing a box around them (the lazygit/delta idiom, and
// the reason 5.13's "cards separated by whitespace not boxes" costs nothing in
// legibility).
//
// The vocabulary authority for every glyph in this tree is
// internal/tui2/tokens (tokens.GlyphAccentRail). blocks cannot import it — the
// edge runs tokens → blocks so blocks stays a leaf — so, exactly as [CutMark]
// does, the byte lives here twice and the pin belongs in tokens' own glyph_test
// beside the CutMark pin. A drift should fail a test rather than ship two rails
// for one meaning.
//
// U+258E is Narrow width: one cell under every ruler.
const AccentEdge = "▎"

// BodyIndent is 5.13's spacing rhythm as a number: two cells per depth, which
// is where a room's WORDS begin, with column 0 left as the gutter the markers
// hang in — a header's glyph, a composer's prompt, a card's accent edge.
//
// Same arrangement as [AccentEdge]: the authority is tokens.LensIndent, blocks
// cannot import it, and the two are the same number by pin rather than by luck.
const BodyIndent = 2

// edgeWidth is the accent edge plus the space after it — the cells a card
// spends on its own gutter before its content starts.
const edgeWidth = 2

// receiptGap is the smallest space between the title and the receipt. Two
// cells, because one lets a long title and a receipt read as a single phrase.
const receiptGap = 2

// minHeadRoom is how much of the title line the header must keep for the
// receipt to be allowed on the row at all.
const minHeadRoom = 8

// CardTone is the outcome a card claims, and the only thing that colours it.
//
// It is a CLAIM and not a decoration (8.2.20): a card drawn green says the work
// succeeded, and a surface that does not know had better not say. That is why
// the zero value makes no claim — a card built from a record whose lifecycle
// has not been read renders in the grey ramp, which is the honest answer.
type CardTone uint8

const (
	// ToneNeutral makes no claim about the outcome: grey edge, grey glyph.
	// It is the zero value, so a card only ever becomes coloured on purpose.
	ToneNeutral CardTone = iota
	// ToneDelivered is the settled answer: soft green, which is 5.16's one
	// word for money AND success.
	ToneDelivered
	// ToneFailed is the failure: soft coral on the glyph and on the edge, so
	// the card reads as broken from the periphery without reading a word.
	ToneFailed
)

// Hue is the tone's colour on the 5.16 vocabulary.
func (t CardTone) Hue() Hue {
	switch t {
	case ToneDelivered:
		return HueMoney
	case ToneFailed:
		return HueBroken
	default:
		return HueNone
	}
}

// GroundStyler is the optional extension a token layer implements to raise a
// [CardBlock] onto its own ground. A Styler without it paints a card exactly as
// it paints everything else, and the card is then carried by its edge alone —
// which is the load-bearing half of the treatment and the half 5.21 actually
// specifies.
//
// It asks for a SPAN and not for a finished row, and that is forced rather than
// chosen: a background wrapped around already-painted text is undone by the
// first inner reset the painted spans carry, so the only form that survives is
// foreground and background written together, span by span. A card that has a
// ground pads every row to its full width for the same reason — a ground behind
// a ragged row draws a torn rectangle.
//
// The card never carries [HueIdentity], so there is no identity door to pass
// through here: a delivery is coloured by its outcome, not by whose task it
// was.
type GroundStyler interface {
	Styler
	// PaintGround paints text on the same state and hue axes [Styler.Paint]
	// uses, over the card's raised ground.
	PaintGround(text string, state State, hue Hue) string
}

// CardBlock is the delivery template of 4.3, drawn to 5.9's card anatomy
// through 5.21's accent rail. Measured at 60 columns:
//
//	▎ ✓ rivers haiku          4 workers · 2m · $0.14
//	▎   three haiku in 5-7-5, one blank line between them
//	▎   ▸ /…/workspace/task-8/rivers.txt
//	▎   ctrl+r open · r rerun
//
// The four parts, in the order they are drawn:
//
//   - The TITLE LINE is the state glyph, the title, and the receipt — a
//     caller-composed telemetry string, right-aligned. The card does not
//     compose the receipt because it does not know what the job spent its time
//     on; it only guarantees the cells.
//   - The BODY is pre-rendered lines. The card takes lines rather than a blob
//     so the prose renderer that lands later can hand over what it decided,
//     and wraps what overflows, because the width the lines will be read at is
//     not known until the frame.
//   - The ARTIFACT ROWS are references (12.5.1's artifact law): a deliverable
//     is born on disk and named by its PATH, shortened from the middle because
//     the filename is the information (5.21).
//   - The VERB ROW is 5.22's rule that no action is typed-only: the words the
//     caller supplies, in the same dim tier and the same "key verb · key verb"
//     grammar the footer draws.
//
// Every row of the card carries the [AccentEdge]; the content sits at
// [BodyIndent] inside it, so the glyph hangs in the card's own gutter exactly
// as a header's glyph hangs in the room's, and the body lines up under the
// title rather than under the glyph. One blank row opens and closes the card:
// unlike a message, which inherits the blank the turn above it left, a card's
// ground must not touch the text on either side of it.
//
// A card is FINALIZED FROM BIRTH. A delivery is a settled thing — that is what
// the treatment means — so it is rendered once per (width, version) and never
// rebuilt. Set its fields before appending it; change them afterwards through
// [CardBlock.Mutate], which is the only coherent way to move committed bytes
// (8.1.1).
type CardBlock struct {
	// Glyph is the card's state glyph, from the 5.17 vocabulary the token
	// layer owns: ✓ settled, ✕ failed. It is the caller's because blocks does
	// not hold that vocabulary; [CardTone] holds the colour.
	Glyph string
	// Title is the job's human name — never its id (5.14).
	Title string
	// Receipt is the right-aligned telemetry of the title line, composed by
	// the caller: "4 workers · 2m · $0.14". A receipt that cannot fit whole is
	// DROPPED rather than cut, because half a receipt is a lie about a number.
	Receipt string
	// Tone is the outcome claim: it colours the glyph and the edge.
	Tone CardTone
	// Body are the finding lines, already rendered — what the prose renderer
	// hands over, one entry per line.
	//
	// An entry that already fits the card's room is drawn EXACTLY as given,
	// escapes and all, because a renderer that laid out a table or highlighted
	// a code row has already made the decisions this type would be overriding.
	// Only an entry that overflows is wrapped, because the width it will be
	// read at is not known until the frame. An empty entry is a paragraph
	// break, and still carries the edge.
	Body []string
	// BodyState is the tier the body is painted at. Settled (the zero value)
	// is the primary grey the findings deserve.
	BodyState State
	// ArtifactGlyph leads every artifact row — the caller's glyph again, and
	// empty draws the path alone.
	ArtifactGlyph string
	// Artifacts are the paths this delivery handed over.
	Artifacts []string
	// Verbs are the clickable words: "open ctrl+r", "rerun r". Verbs that do
	// not fit the row are dropped from the end, whole — a half-drawn verb
	// names a key that does not exist.
	//
	// THE ORDER IN THOSE EXAMPLES IS THE LAW AND NOT A HABIT (§16's verb·key
	// chip): the word that names the act comes first and the key reads as
	// annotation after it, which is why `esc close` was a bug rather than a
	// style. This field takes finished strings because blocks is a leaf and
	// cannot see the catalog, so a caller composing them BY HAND is composing
	// them against that law — and a caller that does not want to should not:
	// internal/tui2/keychip is the renderer, and it spells the order and both
	// tiers once for every surface in the product.
	Verbs []string
	// Styler paints the card. Nil means [Plain]; a Styler that also implements
	// [GroundStyler] gets the ground.
	Styler Styler

	id      string
	end     EndState
	version uint64

	rows     []string
	wrapped  []string
	width    int
	measured bool
}

var _ Block = (*CardBlock)(nil)

// NewCard returns a finalized delivery card. Fill in the rest of its fields
// before appending it to a transcript, or through [CardBlock.Mutate] after.
func NewCard(id, title string) *CardBlock {
	return &CardBlock{id: id, Title: title, end: EndCompleted, width: -1}
}

// ID is the block's stable identity: the anchor key and the cache key.
func (c *CardBlock) ID() string { return c.id }

// IsFinalized is always true: a delivery is a settled thing.
func (c *CardBlock) IsFinalized() bool { return true }

// SettledRows is every row.
func (c *CardBlock) SettledRows(width int) int { return len(c.Rows(width)) }

// Version increments on every change.
func (c *CardBlock) Version() uint64 { return c.version }

// End is how the delivery ended. A card whose job was interrupted or cut draws
// the truncation law's rule under its body like any other finalized block
// (12.5) — the treatment says "a finished answer", and an answer that stopped
// short has to say so inside it.
func (c *CardBlock) End() EndState { return c.end }

// SetEnd marks how the card ended and bumps the version.
func (c *CardBlock) SetEnd(end EndState) {
	if end == EndLive {
		end = EndCompleted
	}
	c.end = end
	c.invalidate()
}

// Mutate applies a change to the card and bumps its version, which is the only
// coherent way to change committed bytes (8.1.1). Setting an exported field
// without it leaves the transcript holding rows nothing will rebuild.
func (c *CardBlock) Mutate(apply func(*CardBlock)) {
	apply(c)
	c.invalidate()
}

func (c *CardBlock) invalidate() {
	c.measured, c.width = false, -1
	c.version++
}

// Rows renders the card at width, reusing the last render when the width has
// not moved. Every row is exactly as wide as the terminal allows and never
// wider, at every width down to one cell.
func (c *CardBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if c.measured && c.width == width {
		return c.rows
	}
	c.rows = c.render(c.rows[:0], width)
	c.width, c.measured = width, true
	return c.rows
}

// render lays the card out. It is written as one pass because the card is
// finalized from birth: it runs once per (width, version) and never on a
// streaming frame, so it buys clarity with allocations the hot path never pays.
func (c *CardBlock) render(dst []string, width int) []string {
	st := styler(c.Styler)
	paint := st.Paint
	head := st
	grounded := false
	if g, ok := st.(GroundStyler); ok {
		paint, head, grounded = g.PaintGround, groundPaint{g}, true
	}
	hue := c.Tone.Hue()

	// A terminal too narrow to hold the edge AND a cell of content drops the
	// edge rather than the content: something honest beats nothing at all, and
	// a card whose every row is one bar says less than a truncated title.
	edge := edgeWidth
	if width-edge < 1 {
		edge = 0
	}
	inner := width - edge
	room := shrink(inner, BodyIndent)

	// fill is n cells of the card's ground, or plain spaces when it has none.
	fill := func(n int) string {
		if n <= 0 {
			return ""
		}
		if !grounded {
			return repeat(spaceMark, n)
		}
		return paint(repeat(spaceMark, n), StateChrome, HueNone)
	}
	lead := ""
	if edge > 0 {
		// The edge is painted as its own one-rune span, which is also what
		// lets the glyph tier upgrade it (12.7 D.2) without this package
		// knowing the tier exists.
		lead = paint(AccentEdge, StateChrome, hue) + fill(1)
	}
	// line closes one card row: the edge, the content, and — only on a
	// grounded card — the fill that makes the row the rectangle a ground needs.
	line := func(content string) string {
		pad := inner - stringWidth(content)
		if pad < 0 {
			content, pad = truncate(content, inner), 0
		}
		if !grounded {
			pad = 0
		}
		return lead + content + fill(pad)
	}

	dst = append(dst, "")
	dst = append(dst, line(c.titleLine(inner, head, paint, fill)))

	for _, text := range c.Body {
		// A line that already fits is a line whose renderer has already
		// decided where it breaks, and it goes through untouched — no wrap, no
		// cut, escapes and all. That is what makes "pre-rendered" true: a
		// syntax-highlighted code row or a laid-out table row re-wrapped here
		// would come out as soup, and [Wrap] measures plain text by contract.
		if stringWidth(text) <= room && strings.IndexByte(text, '\n') < 0 {
			dst = append(dst, line(fill(BodyIndent)+paint(text, c.BodyState, HueNone)))
			continue
		}
		c.wrapped, _ = Wrap(c.wrapped[:0], text, room)
		for _, row := range c.wrapped {
			dst = append(dst, line(fill(BodyIndent)+
				paint(truncate(row, room), c.BodyState, HueNone)))
		}
	}
	for _, path := range c.Artifacts {
		if row := c.artifactRow(path, room, hue, paint, fill); row != "" {
			dst = append(dst, line(row))
		}
	}
	if row := c.verbRow(room, paint, fill); row != "" {
		dst = append(dst, line(row))
	}
	if rule := CutRule(c.end, inner, head); rule != "" {
		dst = append(dst, line(rule))
	}
	return append(dst, "")
}

// titleLine draws the glyph, the title and the receipt.
//
// The receipt is reserved out of the row FIRST and the header degrades into
// what is left, which is the opposite of the header's own order (8.1.5 sheds
// meta first) and deliberately so: on a card, the receipt IS the telemetry 5.9
// says must always be visible, and a title that ate its money would be the card
// hiding the one number a reader came for. What it may never do is arrive half
// drawn — below [minHeadRoom] cells for the header the receipt is dropped
// whole, because "$0.1" is not a smaller truth than "$0.14", it is a different
// and false one.
func (c *CardBlock) titleLine(inner int, head Styler, paint painter, fill filler) string {
	h := Header{
		Glyph:    c.Glyph,
		GlyphHue: c.Tone.Hue(),
		Title:    c.Title,
		State:    StateSettled,
	}
	receipt := flatten(c.Receipt)
	region, gap := inner, 0
	if w := stringWidth(receipt); w > 0 && inner-w-receiptGap >= minHeadRoom {
		region, gap = inner-w-receiptGap, receiptGap
	} else {
		receipt = ""
	}
	title := h.Render(region, head)
	if receipt == "" {
		return title
	}
	var b builder
	b.grow(len(title) + len(receipt) + 32)
	b.WriteString(title)
	// §20's right-column condition, word for word with [Header.Render]: a
	// receipt is a column only while the column is close. A card whose title is
	// three words wide on a 140-cell terminal used to throw its money at the
	// right edge across most of a screen; now it wears it.
	if gulf := region - stringWidth(title); gulf > receiptGulfMax {
		// Painted as three spans for the reason [Header.Render] gives: the
		// figure stays findable as a run of its own.
		b.WriteString(paint(receiptOpen, StateChrome, HueNone))
		b.WriteString(paint(receipt, StateChrome, HueNone))
		b.WriteString(paint(receiptClose, StateChrome, HueNone))
	} else {
		b.WriteString(fill(gulf + gap))
		b.WriteString(paint(receipt, StateChrome, HueNone))
	}
	return b.String()
}

// artifactRow draws one handed-over path: the caller's glyph in the card's hue,
// then the path itself, cut from the MIDDLE because the tail of a path is the
// part that names the file (5.21).
func (c *CardBlock) artifactRow(path string, room int, hue Hue, paint painter, fill filler) string {
	path = flatten(path)
	if path == "" {
		return ""
	}
	lead := ""
	if g := flatten(c.ArtifactGlyph); g != "" {
		lead = g + " "
	}
	var b builder
	b.grow(len(path) + 32)
	b.WriteString(fill(BodyIndent))
	if w := stringWidth(lead); w >= room {
		b.WriteString(paint(truncate(lead, room), StateChrome, hue))
		return b.String()
	} else if lead != "" {
		b.WriteString(paint(lead, StateChrome, hue))
		room -= w
	}
	b.WriteString(paint(TruncatePath(path, room), StateSettled, HueNone))
	return b.String()
}

// verbRow draws the card's affordances in the footer's grammar: dim words,
// dot-separated, in the order the caller gave them.
//
// Verbs are kept WHOLE. A row with room for two and a half of them draws two,
// because the half is a key nobody can press and 5.22's whole point is that an
// action a reader can see is an action they can take.
func (c *CardBlock) verbRow(room int, paint painter, fill filler) string {
	var b builder
	used, drawn := 0, 0
	for _, verb := range c.Verbs {
		verb = flatten(verb)
		if verb == "" {
			continue
		}
		cost := stringWidth(verb)
		if drawn > 0 {
			cost += sepDotWidth
		}
		if used+cost > room {
			break
		}
		if drawn == 0 {
			b.grow(room + 32)
			b.WriteString(fill(BodyIndent))
		} else {
			b.WriteString(paint(sepDot, StateChrome, HueNone))
		}
		b.WriteString(paint(verb, StateChrome, HueNone))
		used, drawn = used+cost, drawn+1
	}
	if drawn == 0 {
		return ""
	}
	return b.String()
}

// painter and filler name the two closures render passes down, so the helpers
// read as the row grammar they are rather than as a signature.
type (
	painter func(text string, state State, hue Hue) string
	filler  func(n int) string
)

// groundPaint adapts a [GroundStyler] to the plain [Styler] seam, so the header
// grammar and the cut rule — which know nothing about grounds — paint onto the
// card's ground through the one door they already have.
type groundPaint struct{ ground GroundStyler }

func (g groundPaint) Paint(text string, state State, hue Hue) string {
	return g.ground.PaintGround(text, state, hue)
}
