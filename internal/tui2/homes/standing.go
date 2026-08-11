package homes

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The standing room: charters as cards, and a focused charter's affordances ARE
// the charter command verbs, rendered from the registry (5.24, 5.22).
//
// The second half of that sentence is the load-bearing one. This file draws a
// verb strip and knows the ORDER the four verbs go in ([CharterVerbs]); it does
// not know their keys, their descriptions, their journal mappings, or whether
// they apply. All of that arrives in [Charter.Verbs] from the one registry, so
// the charter room can never teach a key the palette does not have.

func (v *View) standing(state State, rowID string, width, height int) {
	if rowID == "" {
		v.standingBrief(state, width, height)
		return
	}
	id := strings.TrimPrefix(rowID, CharterRowPrefix)
	for i := range state.Standing.Charters {
		if state.Standing.Charters[i].ID == id {
			v.charter(state.Standing.Charters[i], state, width, height)
			return
		}
	}
	v.gone(width, height)
}

// standingBrief lists the charters with the one thing that decides whether a
// person needs to act: whether it is standing, or waiting to be stood up.
func (v *View) standingBrief(state State, width, height int) {
	if !v.heading(v.glyph(tokens.GQueued), tokens.TextTertiary, "standing", width, height) {
		return
	}
	if !v.prose(HomeStanding.Blurb(), tokens.TextTertiary, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	list := state.Standing.Charters
	if len(list) == 0 {
		v.prose(RouteWatches.Empty(), tokens.TextSecondary, width, height)
		return
	}
	for i := range list {
		c := list[i]
		glyph, tok := v.stateGlyph(c.life(), c.State == CharterProposed)
		if !v.row(glyph, tok, firstLine(c.Invariant), c.State.String(), tokens.TextPrimary, false, width, height) {
			return
		}
		if !v.indented(c.line(state.Standing.TenureAt), width, height) {
			return
		}
	}
}

// charter is one standing promise, opened: the promise itself, then the rails a
// person is being asked to trust, then the verbs.
func (v *View) charter(c Charter, state State, width, height int) {
	glyph, tok := v.stateGlyph(c.life(), c.State == CharterProposed)
	if !v.heading(glyph, tok, c.State.String(), width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if !v.prose(c.Invariant, tokens.TextPrimary, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if c.Cadence != "" && !v.pair("cadence", c.Cadence, width, height) {
		return
	}
	if c.Probation {
		reading := "earning tenure"
		if state.Standing.TenureAt > 0 {
			reading = ladder(c.Greens, state.Standing.TenureAt)
		}
		if !v.pair("autonomy", reading, width, height) {
			return
		}
	} else if c.State == CharterActive {
		if !v.pair("autonomy", "tenured", width, height) {
			return
		}
	}
	if !v.pair("costs", charterCost(c), width, height) {
		return
	}
	if a := age(c.LastFired, state.Now); a != "" {
		if !v.pair("last fired", a, width, height) {
			return
		}
	}
	if c.Today > 0 && !v.pair("today", count(c.Today, false), width, height) {
		return
	}
	if u := until(c.NextDue, state.Now); u != "" {
		if !v.pair("next check", u, width, height) {
			return
		}
	}
	if c.LastLine != "" {
		if !v.blank(height) {
			return
		}
		if !v.text("last look", tokens.TextTertiary, width, height) {
			return
		}
		if !v.prose(c.LastLine, tokens.TextSecondary, width, height) {
			return
		}
	}
	if len(c.Verbs) == 0 {
		return
	}
	if !v.blank(height) {
		return
	}
	v.verbs(c.Verbs, width, height)
}

// charterCost renders the per-firing figure, and says UNMEASURED when it is.
//
// 12.9.2 is the law here and it was written from a real failure: a model wrote
// "$20.00 a run" beside a measured $0.0017, and the surface let it stand. A
// figure this pane cannot compute is a figure this pane does not print — and it
// does not print $0.00 either, because two decimals turn a real fraction of a
// cent into something that reads as free.
func charterCost(c Charter) string {
	if !c.HasCost {
		return "not measured yet"
	}
	return tokens.Money(c.CostPerRun) + " a run"
}
