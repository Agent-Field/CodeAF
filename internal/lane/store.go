package lane

import (
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── WHERE A BELIEF SLEEPS ───────────────────────────────────────────────────
//
// The strike ledger this package replaces was in memory on purpose, and the
// reasoning was sound as far as it went: endpoint speed is a fact about the
// last few minutes, and a ledger that survived a restart would open every
// session acting on a claim it could no longer see.
//
// A FILTER MAKES THAT ARGUMENT UNNECESSARY. A belief that is written down is
// also written down with the moment it was true, and [Posterior.Predict] widens
// it on the way back in until, some hours later, it is worth about as much as
// the public sheet. So what survives a restart is not yesterday's verdict; it
// is yesterday's evidence, correctly discounted. That is strictly more than
// starting from nothing and it cannot mislead the way a persisted strike could.
//
// THIS FILE WRITES NOTHING YET. Lane L-A replaces it with the atomic
// small-file writer this repo already uses elsewhere.

// ErrNoStore is what the empty store answers with, so that "nothing was kept"
// and "nothing can be kept" are never confused for each other.
var ErrNoStore = errors.New("lane: no belief store")

// StorePath is the file beliefs sleep in, `~/.aforge/v3/lanes.json` under the
// home this process was pointed at. It is stated once, here, because a path
// that appears twice is a path that drifts.
func StorePath() string { return home.Join("v3", "lanes.json") }

// store is the empty store: it keeps nothing and admits it.
type store struct{}

// newStore builds the empty store. It is called from the registry and nowhere
// else.
func newStore() *store { return &store{} }

// Load reads nothing.
func (s *store) Load() ([]Belief, error) { return nil, ErrNoStore }

// Save writes nothing, and says so rather than reporting a write it did not
// make.
func (s *store) Save([]Belief) error { return ErrNoStore }
