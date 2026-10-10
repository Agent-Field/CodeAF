// Package decide holds the desktop's decision ledger: what the app decided on a
// person's behalf in a place, why, and whether the person later overturned it.
// It is the persistence half only; deciding policy lives with the callers.
package decide

import "time"

// RingSize is how many recent outcomes a Mode remembers per kind. One constant
// so the store, the tests and any caller reading the ring agree on the figure.
const RingSize = 20

// Mode is how much the app may decide alone for one kind of question.
type Mode string

const (
	ModeLearning Mode = "learning" // watching what the person picks; decides nothing
	ModeDeciding Mode = "deciding" // decides alone and records each decision
	ModeAsk      Mode = "ask"      // always asks, whatever it has learned
)

// Valid reports whether m is one of the three modes.
func (m Mode) Valid() bool {
	return m == ModeLearning || m == ModeDeciding || m == ModeAsk
}

// QuestionRef points at the question in a session that a decision answered.
type QuestionRef struct {
	Session string `json:"session"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
}

// Undo is the handle that reverses a decision. The token is opaque to this
// package; whoever made the decision knows how to honour it.
type Undo struct {
	Token string `json:"token"`
}

// Decision is one thing the app decided for a person. By is the place whose
// rule decided it, which may differ from PlaceID when a rule is inherited.
type Decision struct {
	ID          string      `json:"id"`
	PlaceID     string      `json:"placeId"`
	QuestionRef QuestionRef `json:"questionRef"`
	AskKind     string      `json:"askKind"`
	Subject     string      `json:"subject"`
	Action      string      `json:"action"`
	By          string      `json:"by"`
	Because     string      `json:"because"`
	Percent     int         `json:"percent"`
	Stakes      string      `json:"stakes"`
	Reversible  bool        `json:"reversible"`
	At          time.Time   `json:"at"`
	Undo        Undo        `json:"undo"`
	// OverturnedAt is nil until the person reverses the decision.
	OverturnedAt *time.Time `json:"overturnedAt,omitempty"`
	Dependents   []string   `json:"dependents,omitempty"`
}

// Outcome is what became of one decision: left alone, or overturned.
type Outcome struct {
	DecisionID string    `json:"decisionId"`
	Overturned bool      `json:"overturned"`
	At         time.Time `json:"at"`
}

// KindState is the mode for one (place, kind) and the ring of its last
// RingSize outcomes, oldest first.
type KindState struct {
	Mode   Mode      `json:"mode"`
	Recent []Outcome `json:"recent,omitempty"`
}
