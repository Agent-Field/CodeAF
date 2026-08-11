package palette

import (
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Line assembly, the same shape [rail] uses for the same reason: a line is a
// list of (text, token) spans emitted once, so a row costs ONE allocation
// rather than one per painted fragment, and every add is clipped against the
// remaining budget so overflow is impossible by construction rather than by
// care.
//
// Two invariants hold for every line this file produces, and the width sweep in
// list_test.go walks 1 to 110 proving both: a line is never wider than the
// width it was asked for, and no input can make it panic.

// span is one painted run.
type span struct {
	text string
	tok  tokens.Token
}

// lineBuf accumulates spans against a cell budget.
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

// addMatched appends text into a column of cells cells, with the bytes at pos
// kept at base and everything around them dropped ONE TIER — the fzf highlight
// of 5.18/5.21, and the reason a user watching the list narrow can see WHY each
// survivor survived. A non-positive cells means "whatever is left on the line".
//
// The emphasis is made by demoting the rest rather than by promoting the match,
// and the difference is not cosmetic: the columns now carry the hierarchy
// ([rowTokens]), so a verb is already at the primary tier and has nothing above
// it to be promoted INTO — a highlight built on [tokens.Promote] would simply
// vanish on the one column the reader is typing at. Demoting the surround
// preserves the same one-tier relationship at every base, which is what
// internal/tui2/settings' own highlighter does for the same reason.
//
// pos holds byte offsets into text's lowercase form, which [lower] guarantees
// is the same length as text, so the offsets index text directly. Each matched
// offset is widened to its whole rune before painting: highlighting one byte of
// a multi-byte character would emit an escape sequence into the middle of it,
// which is a broken cell rather than a bright one.
//
// Truncation happens here rather than in the caller, so the offsets are always
// measured against the untruncated string and an offset past the cut is simply
// never reached. A truncated row highlights exactly the letters still on screen
// and never paints into the ellipsis.
func (l *lineBuf) addMatched(text string, base tokens.Token, pos []int32, cells int) {
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
	if len(pos) == 0 {
		l.add(blocks.Truncate(text, room), base)
		return
	}

	fit := text
	tail := ""
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

	bright, base := base, tokens.Demote(base)
	cursor := 0
	for _, p := range pos {
		at := int(p)
		if at < cursor {
			continue
		}
		if at >= len(fit) {
			break
		}
		if !utf8.RuneStart(fit[at]) {
			continue
		}
		_, size := utf8.DecodeRuneInString(fit[at:])
		if size <= 0 {
			continue
		}
		if at > cursor {
			l.add(fit[cursor:at], base)
		}
		l.add(fit[at:at+size], bright)
		cursor = at + size
	}
	if cursor < len(fit) {
		l.add(fit[cursor:], base)
	}
	if tail != "" {
		l.add(tail, base)
	}
}

// ellipsis is the mark [blocks.Truncate] leaves. It is matched, not assumed:
// see [lineBuf.addMatched].
const ellipsis = "…"

// sheetGround is the floor both surfaces stand on. They are floating dialogs
// (12.11), so their ground is the dialog's own — see [tokens.Sheet] for why an
// unpainted panel is a room with no floor, and [tokens.Profile.SheetGround] for
// where it degrades to the hairline alone.
const sheetGround = tokens.Sheet

// padSheet fills the rest of a pane's rectangle with the sheet's own ground.
//
// A pane is given a box and told to fill it (pane.go), and a floating dialog
// that returns fewer lines than its box is a dialog with a hole in the bottom of
// it: the cells it did not state belong to whatever is on the plane below, which
// is the transcript it is floating over. That is 12.13's ghost stated one layer
// up — there a frame that stopped early showed the last frame's cells, here a
// panel that stops early shows the room behind it — and the answer is the same
// one: state every cell you were given.
func padSheet(out []string, l *lineBuf, buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width, height int) []string {
	for len(out) < height {
		out = append(out, blankLine(l, buf, profile, focus, width))
	}
	return out
}

// blankLine is one empty row of the sheet's own ground. It is not the empty
// string: a blank line inside a painted plane that emitted no cells is a hole
// in that plane, and 5.13 spends this surface's structure budget on whitespace,
// which only works when the whitespace is part of the room.
func blankLine(l *lineBuf, buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width int) string {
	l.reset(width)
	return l.emit(buf, profile, focus, width, false, tokens.Band, sheetGround)
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

// emit renders the accumulated spans into buf and returns the line.
//
// The selection band (5.16) is a BACKGROUND: the row keeps its tier colours and
// gains a raised ground, so the band is written once at the head of the line
// and the spans inside it keep their own foregrounds. A profile with no
// trustworthy raised background falls back to SGR 7, the same choice
// [tokens.Styler.PaintOn] makes.
//
// GROUND is the second background, and it is the surface's own floor rather
// than one row's mark: [tokens.Sheet] paints the dialog as a plane (12.11's
// boundary, finally given the ground it was drawn around), and [tokens.Ground]
// means "paint nothing", which is what every pane on the base plane does — the
// transcript's floor is the terminal's, and a component that painted it would
// be claiming a room it does not own. A ground fills the whole width, because a
// plane that stops at the last letter is a highlight.
//
// The two never both apply: a banded row is already fully painted, and writing
// the sheet under it would spend bytes to be overdrawn.
//
// A dimmed pane draws no band at all. That is not a taste call: [tokens.Legal]
// forbids a dimmed foreground on a raised ground, because it is the one
// combination in this palette that cannot clear its contrast gate. An overlay
// is by definition the thing the user is watching, so this branch should never
// fire — it is here so that a caller who dims one anyway degrades to a legal
// frame instead of an illegible one.
func (l *lineBuf) emit(buf *strings.Builder, profile tokens.Profile, focus tokens.Focus, width int, banded bool, band, ground tokens.Token) string {
	colored := profile != tokens.NoColor
	banded = banded && colored && focus == tokens.FocusNormal
	grounded := !banded && colored && ground != tokens.Ground && profile.SheetGround()
	// A REVERSED band paints no foregrounds, and that is a correctness rule
	// rather than a saving. SGR 7 swaps the two colours the terminal is
	// currently using, so a tier colour written inside it lands on the row's
	// BACKGROUND: the old row came out striped — one inverted block per span,
	// separated by the padding runs that had no colour to invert. Reverse video
	// is "defined relative to whatever the terminal's own foreground and
	// background are" ([tokens.Profile.SelectionStyle]), so the honest reading
	// is one reversed run in the terminal's own two colours. The row loses its
	// column tiers on exactly the one row where the band already says
	// everything, at exactly the one profile that cannot draw a raised ground.
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
