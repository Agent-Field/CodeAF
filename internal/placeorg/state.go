package placeorg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

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
	// Organized is true once the person has accepted a plan from an organising
	// run. The first-run offer to organize existing chats never returns after.
	Organized bool `json:"organized,omitempty"`
	// OfferDismissedAt is how many chats were in no place when the person said
	// "Not now" to that offer; zero means it was never dismissed. The offer
	// returns only when the count has doubled. It lives here, with the rest of
	// the organiser's memory per profile, and never in renderer storage.
	OfferDismissedAt int `json:"offerDismissedAt,omitempty"`
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

// NewState is an organiser file with nothing in it yet. Every map is made, so
// a caller writes to it without checking.
func NewState() *State {
	return &State{
		Version:        StateVersion,
		Thresholds:     map[SpaceID]Thresholds{},
		Axes:           map[string]Axis{},
		Lenses:         map[string][]Lens{},
		Kinds:          map[string]Kind{},
		Representative: map[string]string{},
	}
}

// LoadState reads places-org.json. A missing file is a new organiser, not an
// error. A file that is damaged, or was written by a newer build, is moved
// aside and never deleted, and recovery says where it went so the settings
// page can tell the person; the organiser then starts empty rather than
// trust a file it cannot read.
func LoadState(path string, now func() time.Time) (st *State, recovery string, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewState(), "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("placeorg: reading %s: %w", path, err)
	}
	var doc State
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return setAside(path, "it is not readable", now)
	}
	if doc.Version > StateVersion {
		return setAside(path, "a newer version wrote it", now)
	}
	fresh := NewState()
	fresh.fill(doc)
	return fresh, "", nil
}

// fill copies a decoded document over a made one, keeping the made maps
// wherever the file had none.
func (s *State) fill(doc State) {
	s.Space, s.Exemplars, s.Constraints, s.LastRun = doc.Space, doc.Exemplars, doc.Constraints, doc.LastRun
	s.Organized, s.OfferDismissedAt = doc.Organized, doc.OfferDismissedAt
	if doc.Thresholds != nil {
		s.Thresholds = doc.Thresholds
	}
	if doc.Axes != nil {
		s.Axes = doc.Axes
	}
	if doc.Lenses != nil {
		s.Lenses = doc.Lenses
	}
	if doc.Kinds != nil {
		s.Kinds = doc.Kinds
	}
	if doc.Representative != nil {
		s.Representative = doc.Representative
	}
}

func setAside(path, reason string, now func() time.Time) (*State, string, error) {
	if now == nil {
		now = time.Now
	}
	aside := fmt.Sprintf("%s.corrupt-%s", path, now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(path, aside); err != nil {
		return nil, "", fmt.Errorf("placeorg: cannot set aside %s (%s): %w", path, reason, err)
	}
	return NewState(), reason + "; the old copy is at " + aside, nil
}

// Save writes the document whole to a sibling file and renames it over the
// old one, so a crash leaves either the old state or the new and never half.
func (s *State) Save(path string) error {
	s.Version = StateVersion
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("placeorg: encoding the organiser state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("placeorg: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("placeorg: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("placeorg: writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("placeorg: writing %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("placeorg: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}
