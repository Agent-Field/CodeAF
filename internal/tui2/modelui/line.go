package modelui

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Line assembly, the same shape internal/tui2/rail and internal/tui2/palette
// use for the same reason: a line is a list of (text, token) spans emitted
// once, so a row costs ONE allocation rather than one per painted fragment, and
// every add is clipped against the remaining budget so overflow is impossible
// by construction rather than by care.
//
// It is copied rather than imported because both siblings keep theirs
// unexported, and the alternative — a fourth package existing only to hold
// forty lines of span arithmetic — would put a public API in front of something
// no consumer of this tree should ever call. If a shared painter is ever
// factored out, this is one of the three call sites that moves.
//
// Two invariants hold for every line this file produces, and the width sweep in
// picker_test.go walks 1 to 120 proving both: a line is never wider than the
// width it was asked for, and no input can make it panic.

type span struct {
	text string
	tok  tokens.Token
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

// addMatched appends text into a column of cells cells, painting the matched
// run one tier brighter than base — the same highlight the palette draws, and
// the reason a user watching the list narrow can see WHY each survivor
// survived. A non-positive cells means "whatever is left on the line".
//
// at is a byte offset into text's lowered form, which [lower] guarantees is the
// same length as text, and n is the match's byte length. A negative at paints
// nothing bright. The match is clipped against the truncation rather than
// measured after it, so a truncated row highlights exactly the letters still on
// screen and never paints into the ellipsis.
func (l *lineBuf) addMatched(text string, base tokens.Token, at, n, cells int) {
	if text == "" {
		return
	}
	room := l.room()
	if cells > 0 && cells < room {
		room = cells
	}
	if room <= 0 {
		return
	}
	if at < 0 || n <= 0 || at+n > len(text) {
		l.add(blocks.Truncate(text, room), base)
		return
	}

	fit, tail := text, ""
	if blocks.Width(text) > room {
		fit = blocks.Truncate(text, room)
		if fit == "" {
			return
		}
		if strings.HasSuffix(fit, ellipsis) {
			tail = ellipsis
			fit = fit[:len(fit)-len(ellipsis)]
		}
	}
	if at >= len(fit) {
		l.add(fit, base)
		l.add(tail, base)
		return
	}
	if at+n > len(fit) {
		n = len(fit) - at
	}
	l.add(fit[:at], base)
	l.add(fit[at:at+n], tokens.Promote(base))
	l.add(fit[at+n:], base)
	l.add(tail, base)
}

// ellipsis is the mark [blocks.Truncate] leaves. It is matched, not assumed —
// and it is read off [tokens.GlyphEllipsis] rather than spelled here, so the
// match is against the vocabulary's own byte instead of against a copy of it.
const ellipsis = tokens.GlyphEllipsis

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
// foreground on a raised ground. An overlay is by definition the thing the user
// is watching, so that branch should never fire; it is here so a caller who
// dims one anyway degrades to a legal frame instead of an illegible one.
func (l *lineBuf) emit(buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width int, banded bool, band, ground tokens.Token) string {
	colored := profile != tokens.NoColor
	banded = banded && colored && focus == tokens.FocusNormal
	grounded := !banded && colored && ground != tokens.Ground && profile.SheetGround()
	// See internal/tui2/palette's copy of this function: SGR 7 swaps the two
	// colours in use, so a tier colour written inside a reversed band lands on
	// the row's background and the row comes out striped. A reversed row is one
	// run in the terminal's own two colours.
	reversed := banded && profile.SelectionStyle() == tokens.SelectionReverse
	buf.Reset()
	buf.Grow(width * 2)
	wrote := false
	switch {
	case reversed:
		buf.WriteString(tokens.Reverse(profile))
		wrote = true
	case banded:
		buf.WriteString(band.Bg(profile, focus))
		wrote = true
	case grounded:
		buf.WriteString(ground.Bg(profile, focus))
		wrote = true
	}
	for i := range l.spans {
		if colored && !reversed {
			if seq := l.spans[i].tok.Fg(profile, focus); seq != "" {
				buf.WriteString(seq)
				wrote = true
			}
		}
		buf.WriteString(l.spans[i].text)
	}
	if (banded || grounded) && l.w < width {
		buf.WriteString(spaces(width - l.w))
	}
	if wrote {
		if banded || grounded {
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

// sheetGround is the floor the picker stands on: it is a floating dialog
// (12.11), and [tokens.Sheet] is the ground the boundary was drawn around. The
// chip is NOT — it lives in the meta strip on the base plane, where the floor is
// the terminal's and no component owns it — so it emits [tokens.Ground] and
// paints none. [Picker.ground] is the mode that gives this one up.
const sheetGround = tokens.Sheet

// blankLine is one empty row of the sheet's own ground. A blank line inside a
// painted plane that emitted no cells is a hole in that plane.
func blankLine(l *lineBuf, buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width int, ground tokens.Token) string {
	l.reset(width)
	return l.emit(buf, profile, focus, width, false, tokens.Band, ground)
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

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
