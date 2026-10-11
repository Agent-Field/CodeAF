package placeorg

import (
	"context"
	"time"
)

// The stream path: one chat at a time, after the tree exists. The same park
// rule as the full pass, one embedding, and a Judge only on a cross-branch
// contest. Arrivals never birth a place; a place is born when Now gathers a
// quorum of similar chats.

// Arrival is one new or newly settled chat with its vector in the tree's
// space, and the folder prior the engine already knows.
type Arrival struct {
	Chat Chat
	// Vector is in the tree's primary space; Local is in SpaceLocal and is
	// only read when the tree itself lives there.
	Vector []float32
	// FolderPlace is the store place whose folder source matches the chat's
	// workspace, when one does. It is a hard prior: no call is made.
	FolderPlace string
}

// Filing is where an arrival went and why. Judged says a model was asked;
// Contest lists the two places that contested across branches.
type Filing struct {
	Op      Op
	Judged  bool
	Contest []string
	Pick    *Pick
}

// Filer files one arrival. It is the stage behind placegraph's filing offer
// and the auto-file policy. Its one judgment point, the cross-branch
// contest, falls back to the shallower park. Births are separate: Births
// looks at Now and returns one OpBirth per connected group of at least
// Quorum distinct chats, as a proposal.
type Filer interface {
	Judged
	File(ctx context.Context, t *Tree, a Arrival, judge Judge) (Filing, error)
	Births(ctx context.Context, t *Tree, inbox []Arrival) ([]Op, error)
}

// Guess is the live answer while a draft is typed. It is computed in
// SpaceLocal from the draft text, the continuation prior (the place opened
// within ContinuationWindow) and the folder prior, and it never asks a
// model. The bridge serves it in under GuessBudget; the desktop asks for it
// no more often than every GuessDebounce of typing.
type Guess struct {
	Place      string   `json:"place"`
	Name       string   `json:"name"`
	Confidence int      `json:"confidence"`
	Others     []string `json:"others,omitempty"`
}

const (
	ContinuationWindow = time.Hour
	GuessBudget        = 20 * time.Millisecond
	GuessDebounce      = 300 * time.Millisecond
	// FileBudget bounds File excluding the one embedding call.
	FileBudget = 50 * time.Millisecond
)

// Guesser answers the live guess. It is a rule stage: no Judge.
type Guesser interface {
	Guess(ctx context.Context, t *Tree, draft string, workspace string, recentPlace string) (Guess, error)
}

// Correction is a move the person made by hand: a chat left From for To.
type Correction struct {
	Chat string
	From string
	To   string
	At   time.Time
}

// Corrector absorbs a correction: the chat becomes an exemplar of To with
// ExemplarWeight in every space, neighbours that would now follow are
// counted for the journal, and when ConstraintRepeats corrections share a
// pattern (same From and To, or a shared anchor) an OpConstraint is proposed
// for the person to confirm. Nothing moves but the chat the person moved.
type Corrector interface {
	Correct(ctx context.Context, t *Tree, corpus *Corpus, c Correction) ([]Op, error)
}

const (
	ExemplarWeight    = 2.0
	ConstraintRepeats = 3
)
