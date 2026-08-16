package settings

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// One line, built as plain segments and painted once at the end.
//
// The order matters and it is the reason this type exists at all: width is
// measured on the plain text, before any escape sequence is added, so the
// clipping is exact and a painted cell can never smuggle an escape into the
// cell count. Painting is the last thing that happens to a line, and nothing
// downstream ever has to measure a string that has already been painted.
//
// The clip is a hard promise to the compositor's contract (pane.go): a line
// never exceeds the width it was built for. Over-run would be clipped anyway,
// but a pane that relies on that is a pane whose right-hand column is a lie.

type segment struct {
	text  string
	fg    tokens.Token
	bg    tokens.Token
	hasBG bool
}

type lineBuf struct {
	segments []segment
	used     int
	max      int

	// tail is the right-aligned end of the line. It is held back until render
	// so the padding between the two halves is whatever is actually left, and
	// so a line too narrow to hold both drops the tail rather than wrapping it.
	tail      []segment
	tailWidth int
}

func newLine(width int) *lineBuf { return &lineBuf{max: max(0, width)} }

// add appends text in one token, clipped to whatever room is left.
func (b *lineBuf) add(text string, token tokens.Token) {
	b.append(segment{text: text, fg: token})
}

// addOn appends text over a background — the selection band inside a line
// (5.16), used for the selected tab and the picked choice.
func (b *lineBuf) addOn(text string, fg, bg tokens.Token) {
	b.append(segment{text: text, fg: fg, bg: bg, hasBG: true})
}

func (b *lineBuf) append(s segment) {
	if s.text == "" || b.used >= b.max {
		return
	}
	room := b.max - b.used
	width := ansi.StringWidth(s.text)
	if width > room {
		s.text = ansi.Truncate(s.text, room, "")
		width = ansi.StringWidth(s.text)
		if width == 0 {
			return
		}
	}
	b.segments = append(b.segments, s)
	b.used += width
}

// addHighlighted draws a label with the query's letters one tier brighter than
// the rest — fzf's idiom (5.18), in our tiers rather than in a colour, because
// a highlight is emphasis and emphasis is not one of the five words colour is
// allowed to say.
func (b *lineBuf) addHighlighted(label string, hits []int, width int) {
	if len(hits) == 0 || !isASCII(label) {
		b.add(pad(label, width), tokens.TextPrimary)
		return
	}
	lit := make([]bool, len(label))
	for _, at := range hits {
		if at >= 0 && at < len(label) {
			lit[at] = true
		}
	}
	start := 0
	for index := 1; index <= len(label); index++ {
		if index < len(label) && lit[index] == lit[start] {
			continue
		}
		token := tokens.TextTertiary
		if lit[start] {
			token = tokens.TextPrimary
		}
		b.add(label[start:index], token)
		start = index
	}
	if gap := width - ansi.StringWidth(label); gap > 0 {
		b.add(strings.Repeat(" ", gap), tokens.TextPrimary)
	}
}

// addCursor draws the inline editor's text with one cell raised to the band —
// a cursor without a glyph, so nothing in the vocabulary has to be invented
// for it and the text keeps its own colour underneath.
func (b *lineBuf) addCursor(text string, cursor int) {
	runes := []rune(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	b.add(string(runes[:cursor]), tokens.TextPrimary)
	if cursor < len(runes) {
		b.addOn(string(runes[cursor]), tokens.TextPrimary, tokens.Band)
		b.add(string(runes[cursor+1:]), tokens.TextPrimary)
		return
	}
	b.addOn(" ", tokens.TextPrimary, tokens.Band)
}

// right sets the right-aligned tail. Only one is kept: a line with two
// competing tails is a line whose columns are decided by call order.
func (b *lineBuf) right(text string, token tokens.Token) {
	if text == "" {
		return
	}
	b.tail = []segment{{text: text, fg: token}}
	b.tailWidth = ansi.StringWidth(text)
}

// render paints the line. banded raises the whole row onto the selection band
// and pads it to full width, because a band that stops at the last letter is a
// highlight rather than a selection (5.16).
//
// ground is the sheet's own floor under everything that is not banded, and it
// pads for the same reason the band does: this surface is a floating dialog
// (12.11), and a plane with unpainted gaps in it is not a plane. Pass
// [tokens.Ground] to paint no floor at all — the shape linear mode takes, and
// the shape every pane on the base plane takes, where the floor is the
// terminal's and no component owns it.
func (b *lineBuf) render(styler *tokens.Styler, banded bool, ground tokens.Token) string {
	grounded := !banded && styler != nil && ground != tokens.Ground &&
		styler.Profile().SheetGround()
	segments := b.segments
	if b.tailWidth > 0 {
		// Two spaces of breathing room, or the tail is dropped — a right-hand
		// chip crushed against the value is worse than no chip.
		if gap := b.max - b.used - b.tailWidth; gap >= 2 {
			segments = append(append([]segment{}, segments...),
				segment{text: strings.Repeat(" ", gap), fg: tokens.TextTertiary})
			segments = append(segments, b.tail...)
		}
	}
	if banded || grounded {
		used := 0
		for _, s := range segments {
			used += ansi.StringWidth(s.text)
		}
		if gap := b.max - used; gap > 0 {
			segments = append(append([]segment{}, segments...),
				segment{text: strings.Repeat(" ", gap), fg: tokens.TextTertiary})
		}
	}

	var out strings.Builder
	for _, s := range segments {
		switch {
		case styler == nil:
			out.WriteString(s.text)
		case s.hasBG:
			out.WriteString(styler.PaintOn(s.text, s.fg, s.bg))
		case banded:
			out.WriteString(styler.PaintOn(s.text, s.fg, tokens.Band))
		case grounded:
			out.WriteString(styler.PaintOn(s.text, s.fg, ground))
		default:
			out.WriteString(styler.PaintToken(s.text, s.fg))
		}
	}
	return out.String()
}

// blank is one row of the sheet's own ground and nothing else. A blank line
// inside a painted plane that emitted no cells is a hole in that plane, and
// this surface spends its whole structure budget on blank lines (5.13:
// "separated by whitespace not boxes") — so the whitespace has to be part of
// the room rather than a gap in it.
func blank(styler *tokens.Styler, width int, ground tokens.Token) string {
	return newLine(width).render(styler, false, ground)
}

func isASCII(s string) bool {
	for index := 0; index < len(s); index++ {
		if s[index] >= 0x80 {
			return false
		}
	}
	return true
}
