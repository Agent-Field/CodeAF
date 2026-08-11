package rail

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Line assembly.
//
// A row is built as a list of (text, token) spans and emitted once, so a line
// costs ONE allocation rather than one per painted fragment. The escape
// sequences come from [tokens.Token.Fg], which the token layer precomputed at
// package initialisation — a frame appends a constant string and never formats
// one, which is the use that package documents.
//
// Two invariants hold for every line this file produces, and render_test walks
// widths 1 to 110 proving both: a line is never wider than the width it was
// asked for, and no input can make it panic.

// span is one painted run.
type span struct {
	text string
	tok  tokens.Token
}

// lineBuf accumulates spans against a cell budget. Every add is clipped, so
// overflow is impossible by construction rather than by care.
type lineBuf struct {
	spans []span
	w     int
	max   int
}

func (l *lineBuf) reset(max int) {
	if max < 0 {
		max = 0
	}
	l.spans = l.spans[:0]
	l.w = 0
	l.max = max
}

// room is how many cells are left.
func (l *lineBuf) room() int {
	if l.w >= l.max {
		return 0
	}
	return l.max - l.w
}

// add appends text, truncating it into whatever room is left.
func (l *lineBuf) add(text string, tok tokens.Token) {
	if text == "" {
		return
	}
	room := l.room()
	if room <= 0 {
		return
	}
	w := blocks.Width(text)
	if w > room {
		text = blocks.Truncate(text, room)
		w = blocks.Width(text)
		if text == "" {
			return
		}
	}
	l.spans = append(l.spans, span{text: text, tok: tok})
	l.w += w
}

// padTo advances to a column with spaces. It is how a right-aligned cell finds
// its edge without a Sprintf.
func (l *lineBuf) padTo(target int) {
	if target > l.max {
		target = l.max
	}
	if target <= l.w {
		return
	}
	l.spans = append(l.spans, span{text: spaces(target - l.w), tok: tokens.TextTertiary})
	l.w = target
}

// emit renders the accumulated spans.
//
// The selection band (5.16) is a BACKGROUND: the row keeps its tier colours and
// gains a raised ground, which is why the band is written once at the head of
// the line and the spans inside it keep their own foregrounds. A profile with
// no trustworthy raised background falls back to SGR 7, the same choice
// [tokens.Styler.PaintOn] makes.
func (v *View) emit(width int, banded bool, band tokens.Token) string {
	l := &v.line
	colored := v.profile != tokens.NoColor
	v.buf.Reset()
	v.buf.Grow(width * 2)
	wrote := false
	if colored && banded {
		if v.profile.SelectionStyle() == tokens.SelectionReverse {
			v.buf.WriteString(tokens.Reverse(v.profile))
		} else {
			v.buf.WriteString(band.Bg(v.profile, v.focus))
		}
		wrote = true
	}
	for i := range l.spans {
		if colored {
			tok := l.spans[i].tok
			if v.hovered {
				// The pointer preview (hit.go): one tier brighter, nothing else.
				// 5.22's own rule for a control that is also telemetry — "dim at
				// rest, secondary on focus" — and it cannot be confused with the
				// cursor, because the cursor is a BACKGROUND band (5.16) and this
				// is a foreground. Two axes, two meanings, one screen.
				tok = tokens.Promote(tok)
			}
			if seq := tok.Fg(v.profile, v.focus); seq != "" {
				v.buf.WriteString(seq)
				wrote = true
			}
		}
		v.buf.WriteString(l.spans[i].text)
	}
	if banded && l.w < width {
		v.buf.WriteString(spaces(width - l.w))
	}
	if wrote {
		if banded {
			v.buf.WriteString(tokens.Reset(v.profile))
		} else {
			v.buf.WriteString(tokens.ResetFg(v.profile))
		}
	}
	out := v.buf.String()
	// Backstop. Nothing above can overflow, but a row that did would be a
	// broken frame rather than a visual truncation, and the compositor is not
	// the place to discover it.
	if blocks.Width(out) > width {
		out = blocks.Truncate(out, width)
	}
	return out
}

// spaceRun is sliced for padding, so a pad costs no allocation.
const spaceRun = "                                                                                                                                "

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(spaceRun) {
		return spaceRun[:n]
	}
	return strings.Repeat(" ", n)
}

// sanitizeChokepoint is the same table the chat engine uses: model-written
// prose reaches this surface as row names and status lines, and it must not be
// able to move the cursor, clear the screen, or outrank the palette. Benign
// text — the overwhelming case — returns from the scan unallocated.
var sanitizeChokepoint = sanitize.Table(tokens.ANSI16Remap)

// clean is the one door prose takes on its way onto a row: sanitised,
// newline-flattened so a status carrying a stray "\n" cannot smuggle a second
// row into a card whose height the fold has already budgeted, and — at a
// profile that has told us it has no colour — stripped of the SGR the sanitiser
// deliberately kept.
//
// That last clause is the fix for 12.10.6's second finding. The sanitiser
// PRESERVES SGR and remaps it into the palette, which is the right trade for a
// colour terminal: a worker's own red is information the worker meant to carry,
// and [ANSI16Remap] exists to make it legible rather than to erase it. At
// [tokens.NoColor] it is the wrong trade twice over — the surface has promised
// to emit no escapes, and the likeliest readers of that promise are a dumb pipe
// and a golden file, both of which read a surviving SGR as corruption. The rail
// is where a model's own words land as row names and status lines, so it is a
// door the promise has to hold at.
//
// It is a method rather than a package function because the profile is the
// View's, and one door that knows the profile is better than two doors that
// disagree about it. The strip runs only when an ESC survived the sanitiser, so
// benign text — the overwhelming case — pays one IndexByte.
//
// STILL OWED (12.10.6.2 asked for one shared decision, and this is one of
// three): internal/tui2/homes made the same call locally in cleanFor, and the
// chat engine's chokepoint still preserves SGR at NoColor. The shared home
// cannot be tokens — remap.go is explicit that the package stays a leaf and its
// test file is "the only place tokens touches internal/sanitize" — so the
// factoring needs a home of its own and an owner who holds all three call
// sites. Fixed here, where the defect is; named here, so the other two are not
// left to be rediscovered.
func (v *View) clean(s string) string {
	out := blocks.Flatten(sanitize.TextWithPalette(s, sanitizeChokepoint))
	if v.profile == tokens.NoColor && strings.IndexByte(out, 0x1b) >= 0 {
		return ansi.Strip(out)
	}
	return out
}
