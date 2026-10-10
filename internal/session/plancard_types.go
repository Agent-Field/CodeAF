package session

// Plan is a proposed sequence of actions, rather than the task database's work
// plan. ReachesBeyond is the confirmation boundary: actions inside the current
// chat run directly; actions beyond it wait for Go, Edit or Cancel on the card.
// Undo belongs to the receipts of actions that ran, not to this proposal.
type Plan struct {
	ID            string         `json:"id"`
	Steps         []PlanCardStep `json:"steps"`
	ReachesBeyond bool           `json:"reachesBeyond"`
}

// PlanCardStep keeps the action and its destination separate from the words
// drawn on the card, so execution never has to parse a display sentence.
// The name differs from PlanStep, which already describes recorded task work.
type PlanCardStep struct {
	Kind   PlanStepKind `json:"kind"`
	Target PlanTarget   `json:"target"`
	Text   string       `json:"text"`
}

// PlanStepKind names the six actions a coordinating chat may propose.
type PlanStepKind string

const (
	PlanStepHold     PlanStepKind = "hold"
	PlanStepSteer    PlanStepKind = "steer"
	PlanStepStart    PlanStepKind = "start"
	PlanStepStop     PlanStepKind = "stop"
	PlanStepAskPlace PlanStepKind = "ask-place"
	PlanStepRemember PlanStepKind = "remember"
)

// PlanTarget carries exactly one canonical destination ID. Empty alternatives
// stay absent on the wire, because they do not identify additional targets.
type PlanTarget struct {
	Chat  string `json:"chat,omitempty"`
	Task  string `json:"task,omitempty"`
	Place string `json:"place,omitempty"`
}
