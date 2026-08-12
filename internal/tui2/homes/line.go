package homes

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Line assembly, the same shape internal/tui2/rail, internal/tui2/palette and
// internal/tui2/modelui use, for the reason each of them states: a line is a
// list of (text, token) spans emitted once, so a row costs ONE allocation
// rather than one per painted fragment, and every add is clipped against the
// remaining budget so overflow is impossible by construction rather than by
// care.
//
// This is the FOURTH copy. modelui's header names the three that exist and says
// "if a shared painter is ever factored out, this is one of the three call
// sites that moves"; it is now one of four, and that is the number at which the
// factoring is worth doing. It is not done here because it would mean a public
// API in front of forty lines of span arithmetic that no consumer of this tree
// should ever call, and because a lane that both introduced a package and
// refactored three siblings would be a lane nobody could review.
//
// Two invariants hold for every line this file produces, and the width sweep in
// view_test.go walks 1 to 140 proving both: a line is never wider than the width
// it was asked for, and no input can make it panic.

type span struct {
	text string
	tok  tokens.Token
	// ruled marks a span that wears an underline, and rule is the underline's
	// OWN colour (SGR 58). Two fields rather than one sentinel token because the
	// zero value of a span has to be an unmarked one: every other line in this
	// package builds spans with a composite literal that names neither.
	ruled bool
	rule  tokens.Token
}

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

// addRuled appends text wearing an underline in its own colour — §12's quietest
// "this one, of several" mark: a rule UNDER the word rather than a ground behind
// it, so the word keeps its tier and the page keeps its three.
//
// The rule degrades in one step and never in two: a profile with no [SGR 58]
// form draws a plain underline in the text's colour, and [tokens.NoColor] draws
// no attribute at all, because that profile's promise is no escapes.
func (l *lineBuf) addRuled(text string, tok, rule tokens.Token) {
	before := len(l.spans)
	l.add(text, tok)
	for i := before; i < len(l.spans); i++ {
		l.spans[i].ruled = true
		l.spans[i].rule = rule
	}
}

// addPath appends a path with the MIDDLE ellipsis a path deserves (5.21):
// tail-truncating a path throws away the filename, which is the one part a
// reader was looking for.
func (l *lineBuf) addPath(path string, tok tokens.Token) {
	if path == "" {
		return
	}
	room := l.room()
	if room <= 0 {
		return
	}
	if blocks.Width(path) > room {
		path = blocks.TruncatePath(path, room)
	}
	l.add(path, tok)
}

// addStruck appends text with the strike-through a let-go belief wears. The
// combining long stroke overlay is applied per RUNE and adds no cells, so a
// struck line measures exactly what its unstruck form did — which is why the
// width sweep does not have to special-case it.
//
// A profile with no colour gets no strike either: the strike is a rendering of
// "this is no longer held", the status line already says it in words, and
// combining marks on a terminal that would not admit to supporting colour are
// the shape most likely to draw as garbage.
func (l *lineBuf) addStruck(text string, tok tokens.Token, colored bool) {
	if !colored {
		l.add(text, tok)
		return
	}
	room := l.room()
	if room <= 0 || text == "" {
		return
	}
	if blocks.Width(text) > room {
		text = blocks.Truncate(text, room)
	}
	if text == "" {
		return
	}
	var b strings.Builder
	b.Grow(len(text) * 2)
	for _, r := range text {
		b.WriteRune(r)
		b.WriteRune(strikeOverlay)
	}
	// Measured, not assumed: the struck form must occupy the same cells as the
	// plain one, or the pad that follows it lands in the wrong column.
	struck := b.String()
	if blocks.Width(struck) != blocks.Width(text) {
		l.add(text, tok)
		return
	}
	l.spans = append(l.spans, span{text: struck, tok: tok})
	l.w += blocks.Width(text)
}

// strikeOverlay is U+0336 COMBINING LONG STROKE OVERLAY: zero cells under every
// ruler, and the only strike a terminal renders without an SGR nobody's palette
// table owns.
const strikeOverlay = '̶'

// padTo advances to a column with spaces.
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

// emit renders the accumulated spans into buf and returns the line. The
// selection band is a BACKGROUND (5.16): the row keeps its tier colours and
// gains a raised ground, and a profile with no trustworthy raised background
// falls back to SGR 7 — the same choice [tokens.Styler.PaintOn] makes.
//
// A dimmed pane draws no band, because [tokens.Legal] forbids a dimmed
// foreground on a raised ground; it marks its selection with the accent rail
// instead, exactly as the rail does.
func (l *lineBuf) emit(buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width int, banded bool, band tokens.Token) string {
	colored := profile != tokens.NoColor
	banded = banded && colored && focus == tokens.FocusNormal
	// A REVERSED band paints no foregrounds (12.11.2, closed in palette and
	// left standing here): SGR 7 swaps the two colours in use, so a tier
	// colour written inside it lands on the row's BACKGROUND — one inverted
	// block per span, striped by the uncoloured padding runs between them.
	// Reverse video is one run in the terminal's own two colours.
	reversed := banded && profile.SelectionStyle() == tokens.SelectionReverse
	buf.Reset()
	buf.Grow(width * 2)
	wrote := false
	if banded {
		if reversed {
			buf.WriteString(tokens.Reverse(profile))
		} else {
			buf.WriteString(band.Bg(profile, focus))
		}
		wrote = true
	}
	for i := range l.spans {
		sp := &l.spans[i]
		ruled := colored && !reversed && sp.ruled
		if colored && !reversed {
			if seq := sp.tok.Fg(profile, focus); seq != "" {
				buf.WriteString(seq)
				wrote = true
			}
		}
		if ruled {
			buf.WriteString(tokens.Underline(profile))
			buf.WriteString(sp.rule.UnderlineColor(profile, focus))
			wrote = true
		}
		buf.WriteString(sp.text)
		if ruled {
			// Closed on the span rather than at the end of the line: a rule that
			// leaked would underline the separator after it, and the reader would
			// read the separator as part of the word.
			buf.WriteString(tokens.UnderlineColorOff(profile))
			buf.WriteString(tokens.UnderlineOff(profile))
		}
	}
	if banded && l.w < width {
		buf.WriteString(spaces(width - l.w))
	}
	if wrote {
		if banded {
			buf.WriteString(tokens.Reset(profile))
		} else {
			buf.WriteString(tokens.ResetFg(profile))
		}
	}
	out := buf.String()
	// Backstop. Nothing above can overflow, but a row that did would be a
	// broken frame rather than a visual truncation, and the compositor is not
	// the place to discover it.
	if blocks.Width(out) > width {
		out = blocks.Truncate(out, width)
	}
	return out
}

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

// sanitizeChokepoint is the same table the chat engine and the rail use.
// Everything that reaches this package is prose somebody else wrote — a belief
// a model authored, a charter's invariant, a service's own log — and it must not
// be able to move the cursor, clear the screen, or outrank the palette. Benign
// text, which is the overwhelming case, returns from the scan unallocated.
var sanitizeChokepoint = sanitize.Table(tokens.ANSI16Remap)

// clean is the one door prose takes on its way onto a line: sanitised, then
// newline-flattened so a body carrying a stray "\n" cannot smuggle a second row
// into a pane whose height has already been budgeted.
func clean(s string) string {
	return blocks.Flatten(sanitize.TextWithPalette(s, sanitizeChokepoint))
}

// cleanFor is [clean] with the one extra rule a no-colour profile needs.
//
// The sanitiser deliberately PRESERVES SGR and remaps it into the palette —
// that is the right trade for a colour terminal, where a service log's own red
// is information the log meant to carry. On a profile that has told us it has
// no colour it is the wrong trade twice over: the surface has promised to emit
// no escapes, and the likeliest consumers of that promise are a dumb pipe and a
// golden file, both of which read a preserved SGR as corruption.
//
// The strip runs only when a byte 0x1B survived the sanitiser, so benign
// text — the overwhelming case — pays one IndexByte.
func cleanFor(p tokens.Profile, s string) string {
	out := clean(s)
	if p == tokens.NoColor && strings.IndexByte(out, 0x1b) >= 0 {
		return ansi.Strip(out)
	}
	return out
}
