package homes

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The self room. Row 0 is the day line and the eight routes; a route row is
// that route's list.
//
// 5.24's sentence is "no separate self page, no page-local key grammar", and
// the whole of the change is that the eight routes stopped being a list this
// pane navigates and became rows the RAIL navigates. What is left here is a
// renderer with no keys, no selection of its own and no esc ladder — the three
// things that made the old page a second product.

func (v *View) self(state State, rowID string, width, height int) {
	if rowID == "" {
		v.selfBrief(state, width, height)
		return
	}
	id, ok := ParseRowID(rowID)
	if !ok {
		v.gone(width, height)
		return
	}
	v.route(state, id, width, height)
}

// selfBrief is the room's page: the one line that is always true — what today
// cost, what it taught, how much of it was practice — and then the eight routes
// with their counts, so the room answers "what is in here" without being
// walked.
func (v *View) selfBrief(state State, width, height int) {
	if !v.heading(v.glyph(tokens.GQueued), tokens.TextTertiary, "self", width, height) {
		return
	}
	if !v.prose(HomeSelf.Blurb(), tokens.TextTertiary, width, height) {
		return
	}
	if line := today(state.Self.Today); line != "" {
		if !v.blank(height) {
			return
		}
		if !v.pair("today", line, width, height) {
			return
		}
	}
	if !v.blank(height) {
		return
	}
	for _, id := range Routes() {
		r := state.Self.Route(id)
		note := ""
		if r.HasCount {
			note = count(r.Count, r.AtCeiling)
		}
		glyph, tok := v.stateGlyph(LifeQueued, r.needs() > 0)
		if !v.row(glyph, tok, id.Word(), note, tokens.TextPrimary, false, width, height) {
			return
		}
		if !v.indented(id.Explain(), width, height) {
			return
		}
	}
}

// today assembles the day line. Each clause is dropped when it has nothing to
// say: a day with no spend, nothing learned and no practice draws no line at
// all, which is the honest rendering of a machine that was asleep.
func today(t Today) string {
	var c clause
	if t.HasSpend {
		c.add(tokens.Money(t.SpendUSD))
	}
	if t.Learned > 0 {
		c.addInt(t.Learned, "learned")
	}
	if t.Practiced > 0 {
		c.add("practiced " + tokens.Duration(t.Practiced))
	}
	return c.String()
}

// route is one route's list. The explainer leads, because a person who walked
// into "competence" deserves to be told what it claims before being shown
// numbers that claim it.
func (v *View) route(state State, id RouteID, width, height int) {
	r := state.Self.Route(id)
	glyph, tok := v.stateGlyph(LifeQueued, r.needs() > 0)
	title := id.Word()
	if r.HasCount {
		title += " " + tokens.GlyphSeparator + " " + count(r.Count, r.AtCeiling)
	}
	if !v.heading(glyph, tok, title, width, height) {
		return
	}
	if !v.prose(id.Explain(), tokens.TextTertiary, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if len(r.Items) == 0 {
		v.prose(id.Empty(), tokens.TextSecondary, width, height)
		return
	}
	for i := range r.Items {
		if !v.item(r.Items[i], width, height) {
			return
		}
	}
	if r.HasCount && r.Count > len(r.Items) {
		v.text(more(r.Count-len(r.Items)), tokens.TextTertiary, width, height)
	}
}

// item lays one row of a route: the state glyph, the name, the note flush
// right, then the detail line and the artifact path under it.
func (v *View) item(it Item, width, height int) bool {
	glyph, tok := v.stateGlyph(it.Attention(), it.Needs)
	if !v.row(glyph, tok, it.Name, it.Note, tokens.TextPrimary, false, width, height) {
		return false
	}
	if !v.indented(it.Detail, width, height) {
		return false
	}
	if it.Path == "" {
		return true
	}
	l := &v.line
	l.reset(width)
	l.padTo(indentStep)
	l.addPath(v.clean(it.Path), tokens.TextTertiary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), height)
}

// more is the overflow accounting under a windowed list. A window that does not
// say what it left out is a window pretending to be a list (8.1.7's rule, at a
// smaller scale).
func more(n int) string {
	return plural(n, "more, not shown", "more, not shown")
}
