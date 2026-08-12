package footer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The breadcrumb column, and the one column on this row that SHORTENS.
//
// [tokens.FitFooter]'s law is that a column is dropped whole and never
// truncated, and it is the right law for every other column here: half a verb
// is worse than no verb, and a health note cut in the middle is a sentence the
// reader has to guess the end of. The breadcrumb is the exception, and it earns
// the exception by being the one column that answers a question about the
// reader rather than about the room — WHERE AM I. Dropping it whole answers
// that question with silence.
//
// THE DEFECT, measured: at 120 columns `‹ untitled room ‹ Higher-order
// investment angles` is wider than the cells left after the verbs, the hint and
// the help door have taken theirs, so the whole trail left the row — and a
// reader standing three levels inside a task had nothing on screen naming where
// they were standing. The cells it needed were not missing; they were spent on
// ancestors.
//
// So the trail elides, in the one order that keeps the answer: THE CURRENT NAME
// SURVIVES LONGEST. Ancestors collapse into a single `…` first, oldest first,
// because "which room is this inside of" is a question a reader can answer by
// pressing esc and "what am I looking at" is not. Only when the deepest name
// alone still does not fit does the name itself give, and it gives in the
// middle — the head and the tail of a task title are what make it recognizable,
// and a title cut at the end reads as a different, shorter title.
//
// The elision happens inside [Model.fit] so the paint and the hit test are
// handed the same already-shortened text; a breadcrumb that measured one way
// for the pointer and another for the eye would be a click landing on a word
// that had moved.

// crumbSep is how the wiring joins the names of a trail, and therefore how this
// file takes one apart. It is [tokens.GlyphScopeUp] because the breadcrumb is
// the way OUT (5.15): every separator on it is a step a reader can take.
var crumbSep = " " + tokens.GlyphScopeUp + " "

// scopeElided stands for every ancestor the width could not afford. One mark
// for any number of them, because the count is not the information — the fact
// that there is more above you is.
var scopeElided = "…" + crumbSep

// scopeNameFloor is the fewest cells of the current name worth keeping. Below
// it the column stops being an answer and becomes a shape: `‹ … ‹ Hi…` names
// nothing, and the cells are better spent on the row's other columns. The
// column drops whole at that point, which is the ordinary [tokens.FitFooter]
// outcome and the honest one — the surface says nothing rather than something
// unreadable.
const scopeNameFloor = 8

// scopeTrail is one breadcrumb tail taken apart: the way-out glyph it opens
// with, and the names it was joined from, ancestors first and the reader's own
// place last.
type scopeTrail struct {
	lead  string
	names []string
}

// parseScopeTail splits a tail into its names. A tail with no separator in it
// is one name, which is the shape a room at home has and is handled by the same
// code as a trail three deep.
func parseScopeTail(tail string) scopeTrail {
	trail := scopeTrail{}
	rest := tail
	if lead := tokens.GlyphScopeUp + " "; strings.HasPrefix(rest, lead) {
		trail.lead, rest = lead, rest[len(lead):]
	}
	if rest == "" {
		return trail
	}
	trail.names = strings.Split(rest, crumbSep)
	return trail
}

// text renders the trail with its first drop names elided and the current name
// cut to nameWidth cells (0 keeps it whole).
func (t scopeTrail) text(drop, nameWidth int) string {
	if len(t.names) == 0 {
		return ""
	}
	if drop < 0 {
		drop = 0
	}
	if drop > len(t.names)-1 {
		drop = len(t.names) - 1
	}
	kept := t.names[drop:]
	var b strings.Builder
	b.WriteString(t.lead)
	if drop > 0 {
		b.WriteString(scopeElided)
	}
	for i, name := range kept {
		if i > 0 {
			b.WriteString(crumbSep)
		}
		if i == len(kept)-1 && nameWidth > 0 {
			name = middleCut(name, nameWidth)
		}
		b.WriteString(name)
	}
	return b.String()
}

// shortest is the least this column can say and still be worth its cells: the
// way out, the mark for everything above, and [scopeNameFloor] cells of the
// name the reader is standing on. It is what [Model.fit] hands to the fitter as
// the column's MinWidth, so the trail survives on rows it used to leave.
func (t scopeTrail) shortest() string { return t.text(len(t.names)-1, scopeNameFloor) }

// fitScopeTail is the elision order this file exists for, run against a budget.
//
// It walks the forms from longest to shortest and takes the first that fits, so
// a wide terminal pays nothing for the machinery and shows the whole trail.
func fitScopeTail(tail string, width int) string {
	trail := parseScopeTail(tail)
	if width <= 0 || len(trail.names) == 0 {
		return ""
	}
	for drop := range trail.names {
		if text := trail.text(drop, 0); blocks.Width(text) <= width {
			return text
		}
	}
	// The deepest name alone is still too wide, so the name itself gives. The
	// budget is what is left after the way out and the elision mark, both of
	// which are the reader's handles and neither of which is a name.
	last := len(trail.names) - 1
	frame := blocks.Width(trail.text(last, 1)) - 1
	if budget := width - frame; budget >= 1 {
		return trail.text(last, budget)
	}
	return ""
}

// middleCut keeps both ends of a name and marks the missing middle.
//
// A title is recognized by its head and disambiguated by its tail — `Higher-
// order investment angles` and `Higher-order investment risks` are one cut at
// the end away from being the same string — which is [blocks.TruncatePath]'s
// reasoning for a path, applied to the other kind of name this surface draws.
func middleCut(s string, width int) string {
	w := blocks.Width(s)
	if width <= 0 {
		return ""
	}
	if w <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	keep := width - 1
	head := (keep + 1) / 2
	return ansi.Cut(s, 0, head) + "…" + ansi.Cut(s, w-(keep-head), w)
}
