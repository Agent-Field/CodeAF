package draft

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// State is the fold of the journal: every piece the worker and the owner have
// committed so far, and nothing else. Maps are never nil on a folded State.
type State struct {
	Name  string
	About string
	// Plan holds the plan card's lines. Name and Ask are filled by Render.
	Plan  spec.Plan
	Phase Phase
	// Done is the worker's one closing line, set when Phase is done.
	Done string

	Records map[string]*spec.RecordType
	// FieldOrder is the order each record's fields were first added in, for
	// owner-readable lines; Records holds them in a map.
	FieldOrder map[string][]string
	Screens    map[string]json.RawMessage
	Runs       map[string][]byte
	Agent      []byte
	Connectors map[string]spec.Connector
	Triggers   []spec.Trigger
	Commands   []spec.Command
}

func newState() State {
	return State{
		Phase:      PhasePlanning,
		Records:    map[string]*spec.RecordType{},
		FieldOrder: map[string][]string{},
		Screens:    map[string]json.RawMessage{},
		Runs:       map[string][]byte{},
		Connectors: map[string]spec.Connector{},
	}
}

// Fold replays entries in order. An entry whose verb is unknown to this
// binary is skipped, so an older reader still folds a newer journal.
func Fold(entries []Entry) State {
	st := newState()
	for _, e := range entries {
		applyEntry(&st, e)
	}
	return st
}

// applyEntry is the one place an entry changes a State, for Fold and for
// Append alike, so the check an append ran and the later fold cannot differ.
func applyEntry(st *State, e Entry) {
	v, ok := registry[e.Verb]
	if !ok || v.Apply == nil {
		return
	}
	v.Apply(st, e)
	if st.Phase == PhasePlanned && buildKinds[e.Piece.Kind] {
		st.Phase = PhaseBuilding
	}
}

// buildKinds are the pieces that only exist once the plan is accepted.
var buildKinds = map[string]bool{
	"record": true, "field": true, "constant": true, "screen": true, "run": true,
	"agent": true, "connect": true, "trigger": true, "command": true,
}
