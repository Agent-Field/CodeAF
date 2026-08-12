package homes

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The notebook room. The brief is what the surface row shows; a belief row
// shows the belief.
//
// The old chat drew this as an overlay with its own numbered list, its own
// filter and its own retract confirmation, on top of whatever was behind it.
// Here it is a room: the rail lists the beliefs, this pane shows the one under
// the cursor, and the composer beneath is still the one that was there before —
// so "why do you think that" is a sentence, not a mode.

func (v *View) notebook(state State, rowID string, width, height int) {
	if rowID == "" {
		v.notebookBrief(state, width, height)
		return
	}
	id := strings.TrimPrefix(rowID, BeliefRowPrefix)
	for i := range state.Notebook.Beliefs {
		if state.Notebook.Beliefs[i].ID == id {
			v.belief(state.Notebook.Beliefs[i], state, width, height)
			return
		}
	}
	v.gone(width, height)
}

// notebookBrief is the room's own page: what it holds, how much of it there is,
// and — when there is none — the sentence that says what would put something
// here (5.22 rule 6).
func (v *View) notebookBrief(state State, width, height int) {
	if !v.sectionWord("notebook", width, height) {
		return
	}
	if !v.prose(HomeNotebook.Blurb(), tokens.TextTertiary, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	n := state.Notebook
	if len(n.Beliefs) == 0 {
		if n.Query != "" {
			v.text("nothing learned about \""+n.Query+"\"", tokens.TextSecondary, width, height)
			return
		}
		v.prose(RouteBeliefs.Empty(), tokens.TextSecondary, width, height)
		return
	}
	if !v.pair("held", count(n.Total, n.AtCeiling), width, height) {
		return
	}
	if n.Query != "" && !v.pair("filtered by", n.Query, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	for i := range n.Beliefs {
		b := n.Beliefs[i]
		glyph, tok := v.stateGlyph(LifeQueued, false)
		body := tokens.TextPrimary
		if b.Retired || b.Provisional {
			body = tokens.TextTertiary
		}
		if !v.row(glyph, tok, firstLine(b.Body), b.state(), body, b.Retired, width, height) {
			return
		}
	}
}

// belief is one belief, opened. Body first and in full — it is the thing the
// person came for — then the provenance a correction needs to be aimed.
func (v *View) belief(b Belief, state State, width, height int) {
	glyph, tok := v.stateGlyph(LifeQueued, false)
	if !v.heading(glyph, tok, b.state(), width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	body := tokens.TextPrimary
	if b.Retired || b.Provisional {
		body = tokens.TextSecondary
	}
	if !v.prose(b.Body, body, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if b.Scope != "" && !v.pair("about", b.Scope, width, height) {
		return
	}
	if b.Kind != "" && !v.pair("kind", b.Kind, width, height) {
		return
	}
	if b.Trust != "" && !v.pair("trust", b.Trust, width, height) {
		return
	}
	if a := age(b.Learned, state.Now); a != "" {
		if !v.pair("learned", a, width, height) {
			return
		}
	}
	if b.HasUses && !v.pair("recalled", count(b.Uses, false), width, height) {
		return
	}
	if len(b.Evidence) == 0 {
		return
	}
	if !v.blank(height) {
		return
	}
	if !v.text("taught by", tokens.TextTertiary, width, height) {
		return
	}
	for _, ref := range b.Evidence {
		// BY NAME, never by handle (5.14): the rail room draws the same
		// resolved reference the page's detail does, and a reference the wiring
		// could not name says so in words rather than falling back to an id.
		name := firstLine(ref.Name)
		if name == "" {
			name = "a past task"
			if ref.Room == "" {
				name = "a past note"
			}
		}
		if !v.indented(name, width, height) {
			return
		}
	}
}

// gone is what a detail pane says when the row under the cursor is no longer in
// the state — a belief retracted from another window, a service that finished
// dying between the poll and the frame. It is a sentence rather than an empty
// pane because a pane that goes blank looks like a bug in the surface, and this
// is not one.
func (v *View) gone(width, height int) {
	v.text("that row is gone — the list moved", tokens.TextTertiary, width, height)
}
