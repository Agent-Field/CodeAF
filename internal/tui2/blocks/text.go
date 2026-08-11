package blocks

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
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
		return "…"
	}
	return ansi.Truncate(s, width, "…")
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
	b.WriteString("…/")
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
	dashRun  = strings.Repeat("─", 256)
	spaceRun = strings.Repeat(" ", 256)
	dotRun   = strings.Repeat("·", 256)
)

// repeat returns n copies of r, slicing a preallocated run for the runes
// chrome actually repeats so a rule costs no allocation.
func repeat(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	switch r {
	case '─':
		if n*3 <= len(dashRun) {
			return dashRun[:n*3]
		}
	case ' ':
		if n <= len(spaceRun) {
			return spaceRun[:n]
		}
	case '·':
		if n*2 <= len(dotRun) {
			return dotRun[:n*2]
		}
	}
	return strings.Repeat(string(r), n)
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
		return s + repeat(' ', width-w)
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
		return repeat(' ', width-w) + s
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
