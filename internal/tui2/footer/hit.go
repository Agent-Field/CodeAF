package footer

import (
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The footer's words, as targets (5.22 rule 5: "chips are buttons").
//
// The footer already draws the verbs this room can do, with their
// accelerators, from the registry. 5.21 forbids adding an icon bar to make
// them pressable and 5.22 says the affordance must be a thing already on
// screen — so the words themselves are the buttons, and this file is the
// arithmetic that says which word a column landed on.
//
// # Why this is a function and not a table written during Render
//
// Part 2's anti-pattern 14 is "render functions mutate the model (click-target
// row maps written during View())", and it is on the list because it forces a
// call order: the click map is only correct if the last Render used the same
// context and the same width. Here the map is DERIVED, from the same
// buildParts and the same tokens.FitFooter the paint runs, on demand. A click
// re-solves a one-row layout — eight columns and a comparison — which is
// nothing next to the repaint it is about to cause, and in exchange the
// question "is the map stale?" cannot be asked.
//
// # What is a target and what is not
//
// A verb is: it names an action and the registry knows how to run it. The help
// door is: it is a door. The scope tail is: 5.15 makes the breadcrumb the way
// out and 5.22 rule 5 says breadcrumb segments are buttons.
//
// The attention badge, the key-mode note, the health cell and the toast are
// NOT. They are statements about the room, not verbs on it, and a footer where
// half the words did something on click and half did nothing would be worse
// than one where none of them did — the reader would have to learn which.

// The two target ids that are not registry entry ids. They are namespaced so
// they can never collide with one.
const (
	// HelpTarget is the `? help` door at the right end of the row (5.22's
	// checklist: "the `?` surface gets a permanent visible door").
	HelpTarget = "footer:help"
	// ScopeTarget is the breadcrumb tail. Clicking it is the same act as the
	// rail's ‹ and as esc: one step out (5.15).
	ScopeTarget = "footer:scope"
)

// Target is one clickable run of the footer, in pane-local columns.
//
// From is inclusive and To is exclusive, both measured in printable cells, so
// a caller compares a pointer's x against them directly and never has to know
// that the row it is looking at is full of escape sequences.
type Target struct {
	ID   string
	From int
	To   int
}

// Contains reports that a column is inside this run.
func (t Target) Contains(x int) bool { return x >= t.From && x < t.To }

// Targets is every clickable run in the row the same ctx and width would draw,
// left to right.
//
// It walks the survivors in display order, tracking the column each one starts
// at, and splits the verb column — which is fitted as a UNIT, deliberately (see
// the pane's offeredVerbs) — back into the entries it was joined from. The
// split is done from the same verbLabel the paint uses, so a verb whose label
// changes changes its own target with it.
func (m *Model) Targets(ctx FocusContext, width int) []Target {
	survivors, ok := m.fit(ctx, width)
	if !ok {
		return nil
	}
	out := make([]Target, 0, maxVerbs+2)
	x := 0
	for i, p := range survivors {
		if i > 0 {
			x += sepWidth
		}
		w := blocks.Width(p.text)
		switch p.id {
		case "verbs":
			out = appendVerbTargets(out, ctx.Verbs, x)
		case "help":
			out = append(out, Target{ID: HelpTarget, From: x, To: x + w})
		case "scope":
			out = append(out, Target{ID: ScopeTarget, From: x, To: x + w})
		}
		x += w
	}
	return out
}

// TargetAt answers which run a column landed on.
func (m *Model) TargetAt(ctx FocusContext, width, x int) (string, bool) {
	for _, t := range m.Targets(ctx, width) {
		if t.Contains(x) {
			return t.ID, true
		}
	}
	return "", false
}

// appendVerbTargets splits the verb column at the separators it was joined at.
func appendVerbTargets(dst []Target, entries []registry.Entry, from int) []Target {
	n := len(entries)
	if n > maxVerbs {
		n = maxVerbs
	}
	x := from
	for i, e := range entries[:n] {
		if i > 0 {
			x += sepWidth
		}
		w := blocks.Width(verbLabel(e))
		dst = append(dst, Target{ID: e.ID, From: x, To: x + w})
		x += w
	}
	return dst
}

// fit is the survivor computation Render performs, lifted out so the paint and
// the hit test cannot disagree about which columns are on the row. Both call
// it; neither reimplements it.
func (m *Model) fit(ctx FocusContext, width int) ([]part, bool) {
	if width <= 0 {
		return nil, false
	}
	parts := buildParts(ctx)
	if len(parts) == 0 {
		return nil, false
	}
	cols := make([]tokens.FooterColumn, len(parts))
	for i, p := range parts {
		w := blocks.Width(p.text)
		if i > 0 {
			w += sepWidth
		}
		cols[i] = tokens.FooterColumn{ID: p.id, MinWidth: w, Priority: priorityOf(p.id)}
	}
	kept := tokens.FitFooter(cols, width)
	if len(kept) == 0 {
		return nil, false
	}
	keepAt := make(map[string]bool, len(kept))
	for _, c := range kept {
		keepAt[c.ID] = true
	}
	survivors := make([]part, 0, len(parts))
	for _, p := range parts {
		if keepAt[p.id] {
			survivors = append(survivors, p)
		}
	}
	return survivors, true
}
