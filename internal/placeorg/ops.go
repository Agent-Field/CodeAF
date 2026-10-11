package placeorg

import "time"

// OpKind is one reversible change to the tree. The set is closed: a stage
// cannot invent an op, and the Runner refuses an unknown kind.
type OpKind string

const (
	// OpBirth creates a place under Parent with Members.
	OpBirth OpKind = "birth"
	// OpCollapse folds a child that dominates its parent into the parent.
	OpCollapse OpKind = "collapse"
	// OpDissolve removes a place below quorum; its chats park at the parent
	// or, at the top level, go to Now.
	OpDissolve OpKind = "dissolve"
	// OpInterpose creates a parent over sibling places under fan-out
	// pressure. It passed the mega-bucket and separability guards.
	OpInterpose OpKind = "interpose"
	// OpDemote turns a place that is a kind of activity rather than a thing
	// into a lens on its parent. The chats park at the parent.
	OpDemote OpKind = "demote"
	// OpSplit subdivides a place into destination children that emerged.
	OpSplit OpKind = "split"
	// OpFile gives a chat its home at a place.
	OpFile OpKind = "file"
	// OpPark gives a chat its home at an ancestor level because no child
	// beat it by the margin. It is a file with a stated level.
	OpPark OpKind = "park"
	// OpInbox leaves a chat in Now because nothing reached the floor.
	OpInbox OpKind = "inbox"
	// OpLink adds a shortcut: a second place that lists the chat.
	OpLink OpKind = "link"
	// OpName sets a place's name.
	OpName OpKind = "name"
	// OpRename changes a name after the review pass.
	OpRename OpKind = "rename"
	// OpMerge proposes folding one place into another; never auto-applied.
	OpMerge OpKind = "merge"
	// OpLens records a view over a place's flat list. It changes no
	// structure and is journaled as nothing in the store.
	OpLens OpKind = "lens"
	// OpExemplar records a correction's lesson on a place.
	OpExemplar OpKind = "exemplar"
	// OpConstraint proposes a rule learned from repeated corrections.
	OpConstraint OpKind = "constraint"
	// OpAxis sets the steering axis of a place.
	OpAxis OpKind = "axis"
)

// Op is one change, proposed by a stage and applied by the Runner. Every
// field a person could read is derived from the tree or the corpus; the only
// model-written field is Name, and only after a Validator accepted it.
type Op struct {
	Kind OpKind `json:"kind"`
	// Stage is the stage that proposed it, for the journal.
	Stage string `json:"stage"`
	// Place is the subject; Parent, Members, Children and Into are what the
	// kind needs. Ids are placeorg node ids until the Plan is bound to the
	// store, when they become placegraph ids.
	Place    string   `json:"place,omitempty"`
	Parent   string   `json:"parent,omitempty"`
	Into     string   `json:"into,omitempty"`
	Members  []string `json:"members,omitempty"`
	Children []string `json:"children,omitempty"`
	Chat     string   `json:"chat,omitempty"`
	Name     string   `json:"name,omitempty"`
	// Level is the depth a park lands at; Margin and Scores are the evidence.
	Level  int       `json:"level,omitempty"`
	Scores []float32 `json:"scores,omitempty"`
	// Why is one line of evidence in a person's words, written by rules.
	Why string `json:"why"`
	// Proposed says the op waits for a yes. Applied ops are the ones policy
	// lets through: filing into an existing place when the person turned
	// that on, lenses, exemplars.
	Proposed bool `json:"proposed"`
}

// Plan is the result of one organising run: the ops in order, the evidence,
// and the invariants that passed. It is bound to the store in one commit by
// placegraph.Store.ApplyPlan, which yields one receipt, so one undo takes the
// whole run back. A Plan that fails any Invariant never leaves this package.
type Plan struct {
	ID      string    `json:"id"`
	Mode    Mode      `json:"mode"`
	Space   SpaceID   `json:"space"`
	Seed    uint64    `json:"seed"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	Ops     []Op      `json:"ops"`
	Spent   Spent     `json:"spent"`
	// Summary is the person's one-line account: "12 new places, 340 chats
	// filed, 2 renames offered". Written by rules from the ops.
	Summary string `json:"summary"`
	// Provisional marks a run that fell back to a local space or ran out of
	// budget before naming finished; its names may be candidates.
	Provisional bool `json:"provisional,omitempty"`
	// Invariants is the self-check result the person can read.
	Invariants []InvariantResult `json:"invariants"`
}
