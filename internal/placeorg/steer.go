package placeorg

import "context"

// Steering: a sentence from the person becomes one validated action over one
// place, and the action re-renders that place deterministically. The model
// only parses; the grammar below is the whole option space, and a sentence
// that does not parse changes nothing and says "I did not understand".

// SteerAction is what a steer does.
type SteerAction string

const (
	// SteerAxis organises a place's children along an axis.
	SteerAxis SteerAction = "axis"
	// SteerSeparate keeps two named groups apart inside a place
	// ("keep papers separate from code").
	SteerSeparate SteerAction = "separate"
	// SteerFlat removes a place's axis and children; chats park at it.
	SteerFlat SteerAction = "flat"
	// SteerNone is "did not understand": nothing changes.
	SteerNone SteerAction = "none"
)

// AxisKind is what a place can be organised by.
type AxisKind string

const (
	// AxisProject uses the derived anchors: workspace family, repo key,
	// issue references. Children are anchor values at or above Quorum.
	AxisProject AxisKind = "project"
	// AxisKindOfWork uses a kind-of-work label per chat from a closed list,
	// labelled in batches by the Judge with a verb heuristic as fallback.
	AxisKindOfWork AxisKind = "kind"
	// AxisMonth is a lens, never a split: a by-month view over the flat list.
	AxisMonth AxisKind = "month"
	// AxisWorkspace is the folder the chat ran in.
	AxisWorkspace AxisKind = "workspace"
	// AxisCustom is the person's own values from a SteerSeparate.
	AxisCustom AxisKind = "custom"
)

// Axis is the stored steering of one place: future arrivals in it sub-file
// by the same axis.
type Axis struct {
	Kind   AxisKind `json:"kind"`
	Values []string `json:"values,omitempty"`
}

// Steer is the validated action.
type Steer struct {
	Action SteerAction `json:"action"`
	Scope  string      `json:"scope"`
	Axis   AxisKind    `json:"axis,omitempty"`
	Values []string    `json:"values,omitempty"`
}

// KindsOfWork is the closed list a kind-of-work label comes from.
var KindsOfWork = []string{"build", "fix", "review", "research", "write", "plan", "ask", "set up", "check"}

// Steerer parses a sentence against the places the person can see (the
// Judge chooses scope and action among shown options; fallback SteerNone)
// and renders a validated Steer as Ops: OpAxis plus OpLens for a month axis,
// OpAxis plus a proposed OpSplit for project, workspace, kind and custom.
type Steerer interface {
	Judged
	Parse(ctx context.Context, t *Tree, sentence string, judge Judge) (Steer, Pick, error)
	Render(ctx context.Context, t *Tree, corpus *Corpus, s Steer, judge Judge) ([]Op, error)
}
