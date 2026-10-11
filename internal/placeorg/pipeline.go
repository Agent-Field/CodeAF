package placeorg

import (
	"context"
	"errors"
	"time"
)

// ErrNotBuilt is returned by every body in this package that a build lane
// has not written yet. The stubs compile so the lanes can start together.
var ErrNotBuilt = errors.New("placeorg: not built yet")

// Mode is how an organising run treats what is already there.
type Mode string

const (
	// ModeAddOn keeps every existing place as it is. New and unplaced chats
	// are filed or parked into the existing tree, Now is checked for
	// births, and only renames and merges are offered. This is the default
	// and what the idle pass runs.
	ModeAddOn Mode = "add-on"
	// ModeRefresh rebuilds the tree in a shadow and offers the difference.
	// Places the person made or confirmed are kept as must-link groups and
	// never dissolved; places this package created may be dissolved,
	// renamed or re-nested. Nothing is applied until the person says yes.
	ModeRefresh Mode = "refresh"
)

// Stage names, in order, for progress and the journal. A reader of the
// progress view sees the person-facing word; the journal uses these.
const (
	StageDedup       = "dedup"
	StageKind        = "kind"
	StageAnchors     = "anchors"
	StageVectors     = "vectors"
	StageNeighbours  = "neighbours"
	StageCommunities = "communities"
	StageNest        = "nest"
	StageAudit       = "audit"
	StagePark        = "park"
	StageName        = "name"
	StageReview      = "review"
	StageLink        = "link"
	StageCheck       = "check"
)

// Progress is one event on the way through a run, published to the desktop
// over the world feed. Done and Total are the stage's own units (chats,
// places, names); Spent is the running cost.
type Progress struct {
	Run   string    `json:"run"`
	Stage string    `json:"stage"`
	Done  int       `json:"done"`
	Total int       `json:"total"`
	Spent Spent     `json:"spent"`
	At    time.Time `json:"at"`
	// Cancellable is true from the first stage to the last; cancel is the
	// context, and a cancelled run applies nothing.
	Cancellable bool `json:"cancellable"`
}

// Resolutions are the three Leiden resolutions, fine to coarse. They are
// the reference implementation's and are calibrated per space by the park
// lane, not edited by hand.
var Resolutions = [3]float64{2.5, 1.0, 0.4}

// Consensus is how many seeded partitions vote and how many must agree on a
// pair before the final cut sees the edge.
const (
	ConsensusRuns  = 8
	ConsensusAgree = 0.4
)

// Options is everything one run needs besides the stages.
type Options struct {
	Mode   Mode
	Seed   uint64
	Budget Budget
	// Existing is the store's current snapshot as a Tree, built by the
	// bridge; add-on mode keeps it, refresh mode uses its person-made
	// places as must-link groups.
	Existing *Tree
	Progress func(Progress)
	Now      func() time.Time
}

// Runner drives the stages in order, applies every Op through the one
// journaling door, runs the self-check and returns a Plan. It is the only
// type that can change a Tree.
type Runner struct {
	Stages  Stages
	Judge   Judge
	Journal Journal
}

// Run performs one organising run. Not built yet.
func (r *Runner) Run(ctx context.Context, corpus *Corpus, opts Options) (*Plan, error) {
	return nil, ErrNotBuilt
}

// apply journals an Op and applies it to the tree. Every change to a Tree
// anywhere in this package goes through this function. Not built yet.
func (r *Runner) apply(t *Tree, op Op) error { return ErrNotBuilt }
