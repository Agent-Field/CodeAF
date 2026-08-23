package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── A LONG LIST'S TAIL FADES WITH DEPTH — NEVER STRIPES ─────────────────────
//
// THIS IS THE LAW, and it is stated here because this is where it is built.
//
// A list long enough to scroll has to answer two questions at once: which row am
// I on, and how do I tell one row from the next. The answer this surface will
// NOT give is the alternating background — the zebra stripe. A stripe is noise
// spread evenly over every row: it costs the whole list a second colour to solve
// a problem only the crowded part of the list has, and it says nothing at all
// about where the reader is. Nothing on this surface stripes, and nothing on it
// ever will.
//
// What is drawn instead is DEPTH. The head of the window is fully inked. The
// last few rows of a window with more underneath step down the fade ladder the
// thinking window already uses ([thoughtFade], styles.go) — three stops, oldest
// and faintest last — so a long list reads as SHARP WHERE YOU ARE AND QUIET
// WHERE YOU ARE NOT. The fade is the same fact the scrollbar this surface does
// not draw would have been carrying: there is more below, and it is further from
// you than what you are looking at.
//
// Three things follow from stating it that way, and each is enforced below.
//
//	A LIST THAT FITS DOES NOT FADE. Depth is a fact about a window that has
//	cut a list off. A list with its last row on screen has no tail to fade —
//	the bottom of the window IS the bottom of the list — so a short list draws
//	exactly as it drew before this law existed, byte for byte.
//
//	THE CURSOR ROW IS NEVER FADED, wherever it is. The cursor is the answer to
//	"where am I", and a window scrolled so that the cursor sits in the last
//	three rows would otherwise dim the one row the whole list is about.
//	Selection and hover are drawn by whoever owns them; this file only ever
//	declines to fade a row that already wears one.
//
//	A WINDOW WITH NO HEAD DOES NOT FADE EITHER. Three faded rows out of four
//	is not a gradient, it is a dimmed list. The window has to be at least twice
//	the ladder before there is a head to fade away FROM ([fadeFloor]).
//
// And the law is about NAVIGATION lists, which is a judgement each call site
// makes rather than something this file can check. A roster, a project index,
// the record of what a directory has run — those are lists whose tail is
// context, scanned from the top down for the one row you came for. A transcript
// is not: it is read linearly, every line of it is the content, and fading the
// end of what somebody is reading is fading the sentence they are on. The
// conversation and copy mode are therefore untouched, deliberately.

const (
	// fadeSteps is how many rows at the foot of a cut-off window step down, and
	// it is len(ramp.fade) by construction: the ladder has three stops because
	// the thinking window found three was the most a reader can tell apart, and
	// a fourth here would be a step nobody could see.
	fadeSteps = 3
	// fadeFloor is the shortest window that fades at all — twice the ladder, so
	// that the sharp head is never outweighed by the quiet tail.
	fadeFloor = 2 * fadeSteps
)

// tailStop is the whole of the geometry: which stop of the fade ladder the row
// at index `at` of a window `rows` lines tall takes, or -1 for full ink.
//
// `more` is the caller's answer to "did this window cut the list off" and it is
// the gate the emptiness of the law hangs on — false means every row comes back
// -1 and the window draws as it always did.
//
// The stop IS the depth, which is a coincidence worth naming so nobody
// "corrects" it: [thoughtFade] is ordered oldest-first, so stop 0 is the
// faintest of the three, and the faintest row is the deepest one — the last
// line of the window. Depth 1 and 2 walk back up toward the ink.
func tailStop(at, rows int, more bool) int {
	if !more || rows < fadeFloor || at < 0 || at >= rows {
		return -1
	}
	depth := rows - 1 - at
	if depth >= fadeSteps {
		return -1
	}
	return depth
}

// fadeRow paints one already-drawn row at one stop of the ladder.
//
// IT REPAINTS RATHER THAN OVERLAYS, and it has to: a row of these lists arrives
// with its own colours already in it — a state glyph, a title in the ink, a tail
// in the dim — and a colour wrapped around the outside of that would be
// overridden by the first sequence inside it. So the row's own SGR is taken off
// ([unpaint]) and the plain text is painted once in the fade hue. A row this
// deep in a window is not saying six things any more; it is saying "there is
// more down here", in one voice.
//
// A stop of -1 is the ordinary case and returns the row untouched. So does a
// terminal with no hue to fade toward: [palette.fade] answers the sixteen and
// NO_COLOR with the dim tier, which is right for three lines of thinking and
// wrong here — it would drop the colour off a list's tail on a terminal that
// cannot show the gradient that was the point of dropping it.
func (p palette) fadeRow(s string, stop int) string {
	if stop < 0 || s == "" {
		return s
	}
	switch p.profile {
	case tokens.TrueColor, tokens.ANSI256:
	default:
		return s
	}
	// The linear tier takes the same answer it takes for the thinking window: a
	// gradient is an animation held still, and it says nothing to a reader
	// (styles.go's [palette.fade]).
	if p.linear {
		return s
	}
	return p.fade(unpaint(s), stop)
}

// unpaint strips every SGR sequence from a row and leaves everything else
// standing — notably an OSC 8 hyperlink, which occupies no cells and is a fact
// about where a word POINTS rather than about how loud it is. `ansi.Strip` would
// take that with it, which is why this is written out rather than borrowed.
//
// An SGR is a CSI whose final byte is `m`; every other CSI (a cursor move, an
// erase) is left alone, because a row that carried one and lost it would move
// the rest of the frame.
func unpaint(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
			out.WriteByte(s[i])
			i++
			continue
		}
		// Walk the parameter and intermediate bytes to the final one, which is
		// anything in 0x40–0x7E. An unterminated sequence at the end of the row is
		// copied through as it stands: it is malformed either way, and eating it
		// would be this function inventing a repair.
		end := i + 2
		for end < len(s) && (s[end] < 0x40 || s[end] > 0x7E) {
			end++
		}
		if end >= len(s) {
			out.WriteString(s[i:])
			break
		}
		if s[end] != 'm' {
			out.WriteString(s[i : end+1])
		}
		i = end + 1
	}
	return out.String()
}
