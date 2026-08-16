package blocks

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The marks this package draws that are not its own invention. The vocabulary
// authority for every glyph in this tree is internal/tui2/tokens; blocks cannot
// import it — the edge runs tokens → blocks so blocks stays a leaf — so each
// byte lives here twice, exactly as [CutMark] and [AccentEdge] already do, and
// a twin test pins the two spellings equal. A drift fails a test rather than
// shipping two marks for one meaning.
const (
	// OverflowMark is §16's ONE ELLIPSIS GRAMMAR: the mark text leaves behind
	// when it was too long for its column. Twin of tokens.GlyphEllipsis.
	//
	// It is deliberately NOT [CutMark] and not the clickable ⋯: an ellipsis
	// says "the rest is off the edge", a cut says "this stopped and should not
	// have". Before this constant the byte was spelled five times in this
	// package alone — in the two truncators, the path cut and the fold line —
	// which is four chances to disagree with the rest of the product.
	//
	// U+2026 is Ambiguous width and one cell under both shipping rulers.
	OverflowMark = "…"

	// The rule stroke is a twin too, and it is named in rule.go as [RuleMark] —
	// beside the renderer that draws it, so a surface reaching for a dash meets
	// the grammar in the same breath rather than a bare byte.

	// SeparatorMark is the telemetry separator: the dot between meta cells and
	// between a fold line's counts. Twin of tokens.GlyphSeparator. U+00B7 is
	// Ambiguous width and one cell under both shipping rulers.
	SeparatorMark = "·"

	// spaceMark is the third run [repeat] pools. It is not a vocabulary glyph
	// and needs no twin; it is named only so the switch below reads as three
	// marks rather than two marks and a literal.
	spaceMark = " "
)

// Width is the printable width of s, counting escape sequences as zero and
// wide runes as two. Every layout decision in this package goes through it.
func Width(s string) int { return stringWidth(s) }

func stringWidth(s string) int {
	if s == "" {
		return 0
	}
	// Fast path: pure ASCII with no escapes is its own length. This is the
	// common case for chrome and it costs one scan with no allocation.
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] == 0x1b {
			ascii = false
			break
		}
	}
	if ascii {
		return len(s)
	}
	return ansi.StringWidth(s)
}

// Truncate cuts s to width printable cells, marking the cut with an ellipsis.
// It never panics: width <= 0 yields "".
func Truncate(s string, width int) string { return truncate(s, width) }

func truncate(s string, width int) string {
	if width <= 0 || s == "" {
		return ""
	}
	if stringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return OverflowMark
	}
	return ansi.Truncate(s, width, OverflowMark)
}

// TruncatePath cuts a path in the middle, because the filename is the
// information (5.21): src/…/navigate.rs. It falls back to a tail cut when there
// is no separator to cut at.
func TruncatePath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if stringWidth(path) <= width {
		return path
	}
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 {
		return truncate(path, width)
	}
	base := path[slash+1:]
	baseWidth := stringWidth(base)
	// "…/" + base is the floor; below it the filename itself has to give.
	if baseWidth+2 >= width {
		return truncate(base, width)
	}
	head := width - baseWidth - 2
	var b builder
	b.grow(len(path) + 8)
	b.WriteString(ansi.Truncate(path[:slash], head, ""))
	b.WriteString(OverflowMark + "/")
	b.WriteString(base)
	return b.String()
}

// Flatten collapses every newline, carriage return and tab into single spaces,
// so a header can never be a multi-row surprise (8.1.5). A clean string is
// returned unchanged and unallocated.
func Flatten(s string) string { return flatten(s) }

func flatten(s string) string {
	if strings.IndexAny(s, "\n\r\t\v\f") < 0 {
		return s
	}
	var b builder
	b.grow(len(s))
	space := false
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t', '\v', '\f':
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = true
		default:
			b.WriteRune(r)
			space = false
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// cut returns the half-open printable cell range [left, right) of s.
func cut(s string, left, right int) string {
	if right <= left || s == "" {
		return ""
	}
	return ansi.Cut(s, left, right)
}

var (
	dashRun  = strings.Repeat(RuleMark, 256)
	spaceRun = strings.Repeat(spaceMark, 256)
	dotRun   = strings.Repeat(SeparatorMark, 256)
)

// repeat returns n copies of mark, slicing a preallocated run for the three
// marks chrome actually repeats so a rule costs no allocation. It takes a
// string rather than a rune so the pooled marks ARE the named constants —
// a rune switch would need a second spelling of each byte, which is the exact
// drift the twins exist to prevent.
//
// The three pooled marks have three different byte lengths, so the switch is a
// length test before it is a comparison.
func repeat(mark string, n int) string {
	if n <= 0 {
		return ""
	}
	switch mark {
	case RuleMark:
		if end := n * len(RuleMark); end <= len(dashRun) {
			return dashRun[:end]
		}
	case spaceMark:
		if n <= len(spaceRun) {
			return spaceRun[:n]
		}
	case SeparatorMark:
		if end := n * len(SeparatorMark); end <= len(dotRun) {
			return dotRun[:end]
		}
	}
	return strings.Repeat(mark, n)
}

// shrink takes an indent's cells out of a width, never leaving less than one
// cell of content. It is the one place this package clamps an indent, so a
// block's depth, its body's depth and a card's edge all give way at the same
// point: an indent with no room left renders something honest at one cell
// rather than refusing to render at all.
func shrink(width, indent int) int {
	if indent <= 0 {
		return width
	}
	if inner := width - indent; inner >= 1 {
		return inner
	}
	return 1
}

// shift moves a finished row right by a prebuilt pad. The empty-pad case is a
// branch and not a concatenation, so a block at depth zero — every block until
// a surface asks for one — hands back the exact string it built, with no copy
// and no allocation on a streaming frame.
func shift(pad, row string) string {
	if pad == "" {
		return row
	}
	return pad + row
}

// Pad right-fills s with spaces to exactly width cells, or cuts it if it is
// over. Used where a live cell must be width-stable so nothing to its right
// ever dances (5.21).
func Pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := stringWidth(s)
	switch {
	case w == width:
		return s
	case w > width:
		return truncate(s, width)
	default:
		return s + repeat(spaceMark, width-w)
	}
}

// PadLeft is [Pad] for right-aligned cells (telemetry, tabular numbers).
func PadLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := stringWidth(s)
	switch {
	case w == width:
		return s
	case w > width:
		return truncate(s, width)
	default:
		return repeat(spaceMark, width-w) + s
	}
}

// builder is a strings.Builder that knows about the Styler seam. It exists so
// row renderers share one growth policy and one place to reuse capacity.
type builder struct {
	strings.Builder
}

func (b *builder) grow(n int) {
	if n > 0 {
		b.Grow(n)
	}
}

func (b *builder) styled(s Styler, text string, state State, hue Hue) {
	if text == "" {
		return
	}
	b.WriteString(s.Paint(text, state, hue))
}

// identity paints a cell that may carry a task identity. A non-zero seed on a
// [HueIdentity] cell resolves through the token layer's 8-hue wheel when the
// Styler can do it; everything else — a zero seed, another hue, a Styler with
// no identity door — falls through to [builder.styled] and is byte-identical
// to what it drew before this existed.
//
// The type assertion is a pointer compare against an itab, not a lookup, and
// it is reached only by a cell that actually carries a seed, so the ordinary
// header pays nothing for it and nothing here allocates.
func (b *builder) identity(s Styler, text string, state State, hue Hue, seed uint64) {
	if text == "" {
		return
	}
	if seed != 0 && hue == HueIdentity {
		if id, ok := s.(IdentityStyler); ok {
			b.WriteString(id.PaintIdentity(text, seed, state))
			return
		}
	}
	b.WriteString(s.Paint(text, state, hue))
}
