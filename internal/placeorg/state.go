package placeorg

import "time"

// The organiser's own persisted state: places-org.json beside the store. It
// holds what the store has no field for and what the next run must read
// back: the space the tree lives in, each place's axis, the lenses, the
// exemplars and confirmed constraints, each chat's kind and representative,
// and the last run's summary. It is one source of truth for these; the
// store stays the one source for places and memberships. A damaged file is
// set aside, not trusted and not deleted, the way the ledger does it.

// StateVersion is the on-disk format.
const StateVersion = 1

// Exemplar is one correction's lesson: the chat now weighs ExemplarWeight in
// the centroid of the place the person moved it to.
type Exemplar struct {
	Chat string    `json:"chat"`
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
}

// Constraint is a confirmed rule applied before park: chats carrying the
// anchor go to the place.
type Constraint struct {
	Anchor string    `json:"anchor"`
	Place  string    `json:"place"`
	At     time.Time `json:"at"`
}

// State is the document.
type State struct {
	Version int `json:"version"`
	// Space is the space the current tree was built in.
	Space SpaceID `json:"space"`
	// Thresholds are the per-space calibrated park figures (floor, margin,
	// delta) the stream path reads, so a filing uses the same rule as the
	// run that built the tree.
	Thresholds map[SpaceID]Thresholds `json:"thresholds"`
	Axes       map[string]Axis        `json:"axes,omitempty"`
	Lenses     map[string][]Lens      `json:"lenses,omitempty"`
	Exemplars  []Exemplar             `json:"exemplars,omitempty"`
	// Constraints are the confirmed ones; proposed ones are ledger offers.
	Constraints    []Constraint      `json:"constraints,omitempty"`
	Kinds          map[string]Kind   `json:"kinds,omitempty"`
	Representative map[string]string `json:"representative,omitempty"`
	LastRun        *RunSummary       `json:"lastRun,omitempty"`
}

// Thresholds are the park figures for one space.
type Thresholds struct {
	Floor  float32 `json:"floor"`
	Margin float32 `json:"margin"`
	Delta  float32 `json:"delta"`
}

// RunSummary is what Settings shows about the last organising run.
type RunSummary struct {
	Plan    string    `json:"plan"`
	Mode    Mode      `json:"mode"`
	Space   SpaceID   `json:"space"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	Chats   int       `json:"chats"`
	Places  int       `json:"places"`
	Spent   Spent     `json:"spent"`
	Summary string    `json:"summary"`
	// Outcome is "offered", "applied", "declined" or "cancelled".
	Outcome string `json:"outcome"`
}
