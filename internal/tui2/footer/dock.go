package footer

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE DOCK: §6's sidebar, hidden, compressed to one line.
//
// §6 gives `sidebar: right | left | hidden` and spells the hidden form out in
// full — `◐ 2 working · 1 question · $0.31 today`. This wave makes hidden the
// DEFAULT on every page, for the reason a reader gave twice: "still in chat I
// see side rail". With work and notebook as full pages and the palette
// searching every room, an always-open column of the same list is the clutter
// §15 exists to name.
//
// WHERE IT IS DRAWN, and why it is in this package rather than in a slot of its
// own. The dock stands where the rail's own column ends: the bar row runs the
// full width UNDER the rail, so the right end of this row IS the rail's edge,
// and a hidden rail collapsing onto it is the column becoming a line rather
// than a new element appearing somewhere else. A slot of its own would have
// been a second one-row pane welded to a corner, which is a rail-shaped hole
// with a different name.
//
// WHERE THE MONEY IS. The dock's tail — `$0.31 today` — is NOT a field here.
// This row has carried the day's total in [FocusContext.Spend] since §13 put it
// in the bottom bar, and §19's "never say a thing twice on one screen" is
// unambiguous about what a second copy would be. So the dock's counts are
// placed immediately BEFORE the spend fact and the row reads exactly as §6
// writes it, with one figure behind it instead of two. Absent spend stays
// absent (§16's EMPTINESS) and the dock simply ends at its last count.
//
// WHAT IT IS NOT: a summary of the work. It is a DOOR with a count on it, and
// the count exists so that hiding the rail never hides the fact that something
// is running — 10.3.15's law that no live lane goes silent, kept at one line.

// DockTarget is the whole dock as a click target: opening the rail is the one
// act it performs, and it is the same act the rail chord performs.
//
// It is the ONE door in the right zone, and that is a deliberate exception to
// hit.go's stance that the standing facts are statements rather than buttons.
// The distinction holds: a directory, a gauge and a day's total are things that
// are TRUE; the dock is a thing that was PUT AWAY, and a reader who can see the
// counts of hidden work and cannot click them is being shown a drawer with no
// handle.
const DockTarget = "footer:dock"

// Dock is the hidden sidebar's counts. Every field is honest-absent: a zero
// count draws no word at all rather than `0 working`, which is §16's EMPTINESS
// read at the one place a zero would look like news.
type Dock struct {
	// Shown says the rail is off the frame — the precondition, not the whole
	// question. A dock is drawn only when it is Shown AND has a count to carry:
	// see [dockFact] for why a shut drawer with nothing in it says nothing.
	Shown bool
	// Working is how many jobs are moving right now.
	Working int
	// Questions is how many of them are waiting on a human. It is drawn in its
	// own word rather than folded into the working count, because §18.3 spends
	// amber on exactly this fact and a number that meant two things could not
	// wear it.
	Questions int
}

// dockWords is the dock's text and whether the questions word is in it.
//
// The glyph is [tokens.GlyphWorking] — §6 writes the dock with `◐` and the
// vocabulary already owns that byte for "this is moving". It is STATIC here:
// §18.2 permits the spinner on work in flight and forbids it on a durable
// object, and the dock is a durable object — it is on the row whether anything
// is running or not.
//
// The words are §14's, which is why they are `working` and `question` and never
// `tasks`, `jobs running` or a fraction. Counts are only ever counts of things
// whose multiplicity is not already visible (§15), which a hidden list's is not.
func dockWords(d Dock) (text string, amber bool) {
	var b strings.Builder
	b.WriteString(tokens.GlyphWorking)
	if d.Working > 0 {
		b.WriteString(" ")
		b.WriteString(strconv.Itoa(d.Working))
		b.WriteString(" working")
	}
	if d.Questions > 0 {
		if b.Len() > len(tokens.GlyphWorking) {
			b.WriteString(sep)
		} else {
			b.WriteString(" ")
		}
		b.WriteString(strconv.Itoa(d.Questions))
		b.WriteString(plural(d.Questions, " question", " questions"))
		amber = true
	}
	return b.String(), amber
}

// dockFact is the dock as a right-zone column, or the zero fact when the rail
// is on the frame and there is nothing to collapse.
//
// The TIER is the chrome tier at rest — the dock is a door, and §16 puts
// interactive chips at rest on [tokens.TextTertiary] — and amber the moment a
// question is open, because §18.3 gives amber one meaning and this is it: a
// human is needed, and the list that would have said so is not on screen.
func dockFact(d Dock) (fact, bool) {
	// A SHUT DRAWER WITH NOTHING IN IT SAYS NOTHING.
	//
	// The first cut of this drew the glyph alone whenever the rail was hidden,
	// so that the door was always visible — and the golden caught what that
	// actually renders: a lone `◐` beside the day's money, on a window where
	// nothing was running. §18.2 is explicit that a working glyph on something
	// that is not working is a lie about liveness, and §16's EMPTINESS says an
	// absent value renders as absence rather than as a mark standing in for one.
	// A row that says "there is hidden work" when there is none has spent the
	// reader's trust to advertise a keystroke.
	//
	// So the chord is what teaches the drawer (the `?` sheet and the palette
	// both name it), and this row speaks only when it has a count to speak.
	if !d.Shown || (d.Working <= 0 && d.Questions <= 0) {
		return fact{}, false
	}
	text, amber := dockWords(d)
	tok := tokens.TextTertiary
	if amber {
		tok = tokens.Amber
	}
	return fact{zone: zoneDock, text: text, tok: tok, id: DockTarget}, true
}

// dockWidth is what the dock costs, so a caller sizing a row can ask without
// rendering one.
func dockWidth(d Dock) int {
	f, ok := dockFact(d)
	if !ok {
		return 0
	}
	return blocks.Width(f.text)
}

// plural picks the word for a count. It is here rather than shared because the
// only two words this package pluralises are the dock's.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
