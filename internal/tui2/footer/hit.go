package footer

import "github.com/Agent-Field/aforge-v2/internal/tui2/blocks"

// The footer's words, as targets: chips are buttons, and so are places.
//
// Nothing on this row was added to make it clickable. The left zone already
// draws the homes as words and the trail as the way out; the middle already
// draws the live verbs with the keys that reach them. This file is the
// arithmetic that says which word a pointer landed on.
//
// # Why this is a function and not a table written during Render
//
// A click-target map written during View() forces a call order: the map is only
// correct if the last Render used the same context and the same width. Here the
// map is DERIVED, from the same [Model.layout] the paint runs, on demand. A
// click re-solves a one-row layout — three zones and a comparison — which is
// nothing next to the repaint it is about to cause, and in exchange the question
// "is the map stale?" cannot be asked.
//
// # What is a target and what is not
//
// A place word is: it is a door into a home. A trail is: it is the way out. A
// live verb chip is: it names an act and the host knows how to run it, and the
// WHOLE chip answers — a reader points at the words, not at the key.
//
// The answer chip is NOT, and that is not an oversight: `answer 1—3` names three
// acts, and a click cannot say which. The right zone is not either — a
// directory, a gauge and a day's spend are statements about this window, and
// there is nothing to open on a statement. A footer where half the words did
// something on click and half did nothing would be worse than one where none of
// them did: the reader would have to learn which.

// The target ids that are not host-supplied ids. They are namespaced so they can
// never collide with a registry entry id or a place id.
const (
	// HelpTarget named the `? help` door this row used to end with. The row does
	// not draw it any more — the standing legends moved to the `?` sheet, and
	// the middle zone's whole point is that it is silent when nothing is live —
	// so nothing here ever answers with this id. It stays exported only so the
	// shell's click switch keeps compiling until the assembly pass takes that
	// case out with it.
	HelpTarget = "footer:help"
	// ScopeTarget is the breadcrumb. Clicking anywhere on it is the same act as
	// the rail's ‹ and as esc: one step out.
	ScopeTarget = "footer:scope"
	// InterruptTarget is the `interrupt esc` chip. Clicking it is the same act
	// as pressing esc while a turn is streaming.
	InterruptTarget = "footer:interrupt"
	// ThreadsDoorTarget is the `threads` word in the left zone. Clicking it is
	// the same act the chord performs and the same act the title chip performs;
	// it exists because the chip is ABSENT until the scribe has named the
	// conversation, and a first-run window would otherwise show no door at all.
	ThreadsDoorTarget = "footer:threads-door"
	// NewThreadTarget is the `+` beside it. It mints a conversation and walks
	// into it, which is the same act the last row of the switcher performs.
	NewThreadTarget = "footer:new-thread"
	// ThreadTarget is the title chip: the name of the working conversation this
	// window is in (chat-simplify.md 5.3). Clicking it opens the thread
	// switcher, which is the same act the `t` key performs — one door, two
	// hands, exactly as the dock and its chord are one drawer.
	ThreadTarget = "footer:thread"
)

// Target is one clickable run of the footer, in pane-local columns.
//
// From is inclusive and To is exclusive, both measured in printable cells, so a
// caller compares a pointer's x against them directly and never has to know that
// the row it is looking at is full of escape sequences.
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
// Runs that share an id and touch are merged, which is what makes a chip one
// target: the verb and the key are drawn in two tiers and pressed as one word.
func (m *Model) Targets(ctx FocusContext, width int) []Target {
	runs, ok := m.layout(ctx, width)
	if !ok {
		return nil
	}
	out := make([]Target, 0, maxVerbs+4)
	for _, p := range runs {
		if p.id == "" || p.text == "" {
			continue
		}
		to := p.from + blocks.Width(p.text)
		if n := len(out); n > 0 && out[n-1].ID == p.id && out[n-1].To == p.from {
			out[n-1].To = to
			continue
		}
		out = append(out, Target{ID: p.id, From: p.from, To: to})
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
