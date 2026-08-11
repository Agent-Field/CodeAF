package composer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Render implements tui2.Pane. It is a pure function of the draft, the
// cursor and the given rectangle — nothing here mutates the model, per
// pane.go's contract, and every returned line is truncated to width as a
// last, unconditional step so the compositor never receives an over-run row
// regardless of how the layout above got there (see wrap.go and doc.go).
// usable is the draft's own text width: the rectangle less the prompt glyph and
// the space after it. It is a function rather than two lines inside Render
// because the pointer has to wrap the draft the same way the paint does, and
// two spellings of one number is how a click lands on the wrong row.
func usable(width int) int {
	prefixWidth := 2
	if width < 2 {
		prefixWidth = 1
	}
	return width - prefixWidth
}

func (m *Model) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	sty := m.activeStyler()

	prefixWidth := width - usable(width)
	usable := usable(width)

	// The `@` grammar's chrome (5.18, 5.22) borrows from the bottom of the
	// rectangle and never from the draft's last row — see hint.go. A composer
	// with no [Options.Targets] never enters this path at all, which is what
	// keeps its output byte-identical to the pre-`@` composer's.
	hints := m.hintRows(sty, width, height-1)
	drafted := height - len(hints)

	rows := layoutRows(m.value, usable)
	cursorRow := rowOf(rows, m.cursor)
	total := len(rows)

	visible := drafted
	if visible > total {
		visible = total
	}
	scrollTop := 0
	if total > drafted {
		scrollTop = cursorRow - (drafted - 1)
		if scrollTop < 0 {
			scrollTop = 0
		}
		if maxTop := total - drafted; scrollTop > maxTop {
			scrollTop = maxTop
		}
	}

	empty := len(m.value) == 0
	lines := make([]string, 0, visible+len(hints))
	for i := scrollTop; i < scrollTop+visible; i++ {
		lines = append(lines, m.renderRow(sty, rows[i], i, i == cursorRow, empty, width, prefixWidth))
	}
	lines = append(lines, hints...)
	return strings.Join(lines, "\n")
}

// renderRow draws one display row: the prompt (row 0 of the whole draft) or
// an aligned indent (every other row), the row's text, and — when this row
// holds the cursor and the pane is focused — one inverted cell marking it.
func (m *Model) renderRow(sty *tokens.Styler, sp span, rowIdx int, hasCursor, empty bool, width, prefixWidth int) string {
	var prefix string
	if rowIdx == 0 {
		prefix = padCells(tokens.GlyphPromptChat, prefixWidth)
		promptTok := tokens.TextTertiary
		if m.focused {
			promptTok = tokens.Cyan
		}
		prefix = paint(sty, prefix, promptTok)
	} else {
		prefix = strings.Repeat(" ", prefixWidth)
	}

	var body string
	switch {
	case empty && rowIdx == 0:
		body = m.renderEmptyBody(sty)
	case hasCursor && m.focused:
		body = m.renderCursorBody(sty, sp)
	default:
		body = paint(sty, string(m.value[sp.Start:sp.End]), tokens.TextPrimary)
	}

	return ansi.Truncate(prefix+body, width, "")
}

// renderEmptyBody draws the placeholder, with the caret in front of it when
// focused — the caret marks where typing will land even before anything has
// been typed.
func (m *Model) renderEmptyBody(sty *tokens.Styler) string {
	placeholder := paint(sty, placeholderText, tokens.TextTertiary)
	if !m.focused {
		return placeholder
	}
	return paintOn(sty, " ", tokens.TextPrimary, tokens.Band) + placeholder
}

// renderCursorBody draws a non-empty row that holds the cursor: the text
// before it, one inverted cell for the character at it (or a blank inverted
// cell when the cursor sits at the row's end), and the text after it.
func (m *Model) renderCursorBody(sty *tokens.Styler, sp span) string {
	col := m.cursor - sp.Start
	before := string(m.value[sp.Start : sp.Start+col])
	var at, after string
	if m.cursor < sp.End {
		at = string(m.value[m.cursor])
		after = string(m.value[m.cursor+1 : sp.End])
	} else {
		at = " "
	}
	return paint(sty, before, tokens.TextPrimary) +
		paintOn(sty, at, tokens.TextPrimary, tokens.Band) +
		paint(sty, after, tokens.TextPrimary)
}

// padCells right-pads s with spaces until it occupies n cells. Used only for
// the prompt glyph against a prefixWidth of 2 (glyph + one space) — at
// prefixWidth 1 (width == 1) it is a no-op, which is exactly the degraded
// "glyph only, no room for the separator" case doc.go describes.
func padCells(s string, n int) string {
	w := ansi.StringWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
