package homes

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The four home scopes, as a [rail.ScopeSource].
//
// Every one has the same anatomy the rail already draws: row 0 is the room's
// conversational surface, the members are its contents, and moving between them
// never leaves the scope (5.15). Nothing here is a page, nothing has a
// page-local key grammar, and esc pops the scope rather than closing a modal —
// which is the whole of 5.24's complaint against the surfaces these replace.
//
// Member row ids are namespaced by room. The rail hands a row id straight back
// to [ScopeSource.Scope] on enter, so an unprefixed belief seq and an
// unprefixed charter id would be one collision away from opening the wrong
// room — and worse, a belief whose body happened to spell a service name would
// be an id nobody could debug.

const (
	// BeliefRowPrefix namespaces a notebook row.
	BeliefRowPrefix = "belief:"
	// CharterRowPrefix namespaces a standing row.
	CharterRowPrefix = "charter:"
	// ServiceRowPrefix namespaces a services row.
	ServiceRowPrefix = "service:"
)

// Source answers the rail's scope question for the four homes. It holds one
// [State] and rebuilds nothing until the wiring hands over a new one, so a
// keystroke costs a map-free switch and a slice walk.
//
// The zero value is usable and answers every home with an honestly empty room.
type Source struct {
	state State
}

var _ rail.ScopeSource = (*Source)(nil)

// NewSource returns a Source over the given state.
func NewSource(state State) *Source { return &Source{state: state} }

// SetState replaces the facts. The caller's slices are NOT copied — this
// package never writes to them, and a copy per poll of a notebook window would
// be a copy nobody reads.
func (s *Source) SetState(state State) { s.state = state }

// State returns the facts the Source is answering from.
func (s *Source) State() State { return s.state }

// Scope implements [rail.ScopeSource] for the four homes. An id that is not a
// home's returns false, which the rail reads as "this row has no room to
// descend into" — exactly right for a task id that reached the wrong source.
func (s *Source) Scope(id string) (rail.Scope, bool) {
	h, ok := ParseScopeID(id)
	if !ok {
		return rail.Scope{}, false
	}
	return Scope(s.state, h), true
}

// Scope builds one home's scope. It is a pure function of the state so a caller
// with no Source — a test, or a narrow-width list rendering — can ask for a
// room directly.
//
// A home has no identity pastel: Seed stays empty, so the selection band is
// untinted. 5.16 spends identity on "which task am I in", and a notebook is not
// a task; borrowing a wheel entry for it would answer a question nobody asked.
func Scope(state State, h Home) rail.Scope {
	sc := rail.Scope{
		ID:    h.ScopeID(),
		Title: h.Word(),
		Rows: []rail.Row{{
			ID:       h.ScopeID(),
			Kind:     rail.RowSurface,
			Name:     h.Word(),
			Status:   h.Blurb(),
			Composer: h.Composer(),
		}},
	}
	switch h {
	case HomeNotebook:
		sc.Rows = append(sc.Rows, beliefRows(state)...)
	case HomeSelf:
		sc.Rows = append(sc.Rows, routeRows(state)...)
	case HomeStanding:
		sc.Rows = append(sc.Rows, charterRows(state)...)
	case HomeServices:
		sc.Rows = append(sc.Rows, serviceRows(state)...)
	}
	return sc
}

// beliefRows are the notebook's members.
//
// Every belief draws the dim ○ of [rail.LifeQueued], and that is the honest
// reading rather than a shortcut: a belief is not work, so it has no state in
// the rail's vocabulary, and [rail.AttnQueued] is the one entry that resolves
// to the chrome tier instead of claiming a colour. What separates an active
// belief from one that was let go is carried where a reader can act on it — the
// status line and the detail pane, both of which say it in words.
func beliefRows(state State) []rail.Row {
	rows := make([]rail.Row, 0, len(state.Notebook.Beliefs))
	for i := range state.Notebook.Beliefs {
		b := state.Notebook.Beliefs[i]
		rows = append(rows, rail.Row{
			ID:       BeliefRowPrefix + b.ID,
			Kind:     rail.RowStep,
			Name:     firstLine(b.Body),
			Status:   b.line(state.Now),
			Composer: rail.ComposerChat,
		})
	}
	return rows
}

// routeRows are the self room's eight routes. The count rides the NAME rather
// than a telemetry cell, because [rail.Telemetry] has no count that is not a
// worker count and `8w` beside "skills" would be a number wearing the wrong
// word.
func routeRows(state State) []rail.Row {
	rows := make([]rail.Row, 0, len(Routes()))
	for _, id := range Routes() {
		r := state.Self.Route(id)
		name := id.Word()
		if r.HasCount {
			name += " " + tokens.GlyphSeparator + " " + count(r.Count, r.AtCeiling)
		}
		status := id.Explain()
		if r.HasCount && r.Count == 0 {
			status = id.Empty()
		}
		rows = append(rows, rail.Row{
			ID:        id.RowID(),
			Kind:      rail.RowStep,
			Name:      name,
			Status:    status,
			Composer:  rail.ComposerChat,
			Questions: r.needs(),
		})
	}
	return rows
}

// charterRows are the standing room's members. A proposed charter carries one
// question, which is literally true — it is waiting for a person to stand it up
// — and it is what makes the amber ? reach the collapsed group above.
func charterRows(state State) []rail.Row {
	rows := make([]rail.Row, 0, len(state.Standing.Charters))
	for i := range state.Standing.Charters {
		c := state.Standing.Charters[i]
		row := rail.Row{
			ID:       CharterRowPrefix + c.ID,
			Kind:     rail.RowStep,
			Name:     firstLine(c.Invariant),
			Status:   c.line(state.Standing.TenureAt),
			Life:     c.life(),
			Composer: rail.ComposerChat,
		}
		if c.State == CharterProposed {
			row.Questions = 1
		}
		rows = append(rows, row)
	}
	return rows
}

// serviceRows are the services room's members. Every one binds
// [rail.ComposerNone]: a service is not a conversation, and 5.24 makes that the
// one place in the product where a room silences its composer outright.
func serviceRows(state State) []rail.Row {
	rows := make([]rail.Row, 0, len(state.Services.Services))
	for i := range state.Services.Services {
		sv := state.Services.Services[i]
		rows = append(rows, rail.Row{
			ID:       ServiceRowPrefix + sv.ID,
			Kind:     rail.RowStep,
			Name:     firstLine(sv.Name),
			Status:   sv.line(),
			Life:     sv.Life,
			Composer: rail.ComposerNone,
			Artifact: rail.Ref{Path: sv.LogPath, Label: "log"},
		})
	}
	return rows
}

// needs counts the route's rows waiting on a human.
func (r Route) needs() int {
	n := 0
	for i := range r.Items {
		if r.Items[i].Needs {
			n++
		}
	}
	return n
}

// count renders a collection size, with the ceiling mark when the number is a
// floor rather than a count (see [Notebook.AtCeiling]). `500+` is not a
// decoration: 10.2.8 says a number that has not arrived and a number that is
// what it says are different facts, and a scan ceiling reported as a count is
// the first kind wearing the second's clothes.
func count(n int, ceiling bool) string {
	var buf [24]byte
	out := strconv.AppendInt(buf[:0], int64(n), 10)
	if ceiling {
		out = append(out, '+')
	}
	return string(out)
}

// firstLine is the one-line form of prose that may carry newlines. The rail
// flattens too, but a name that arrives already flattened truncates against the
// right budget rather than against a string with a "\n" still counted in it.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
