package lane

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
// is yesterday's evidence, correctly discounted.
//
// THE AGEING HAPPENS AT READ TIME AND NOT HERE. This file loads what was
// written, with the moment it was written attached; nothing in it looks at a
// clock. Whoever asks the ledger a question knows what time it is and ages the
// answer with [Posterior.Predict] — which is the same arithmetic a belief that
// has merely been sitting in memory for ten minutes needs, so there is exactly
// one place that does it rather than one for each way a belief got old.
//
// ── WHY THE WHOLE SET, EVERY TIME ───────────────────────────────────────────
//
// A few hundred lanes of a few dozen models is tens of kilobytes. Writing all
// of it on every update is one small atomic write, and it buys the property
// that matters more than the bytes: there is no partial state, no append log to
// compact, and no window in which the file describes a ledger that never
// existed. When it stops being small — the day this build is talking to
// thousands of lanes — the fix is a debounce, not a format.

// ErrNoStore is what a store with nowhere to write answers with, so that
// "nothing was kept" and "nothing can be kept" are never confused for each
// other. A file that is simply not there yet is the first of those and reads
// back as no beliefs and no error.
var ErrNoStore = errors.New("lane: no belief store")

// StorePath is the file beliefs sleep in, `~/.aforge/v3/lanes.json` under the
// home this process was pointed at. It is stated once, here, because a path
// that appears twice is a path that drifts.
func StorePath() string { return home.Join("v3", "lanes.json") }

// store is the belief file.
//
// The path is resolved on every call rather than captured at construction
// because AFORGE_HOME is allowed to move the whole state root under a running
// process — a disposable run does exactly that — and a store holding the path
// it was born with would keep writing into the home it was pointed at first.
type store struct {
	mu sync.Mutex
	// path overrides the file, for a test that must not write into the state
	// root of whoever is running it. Empty is [StorePath].
	path string
}

// newStore builds the store on the real file. It is called from the registry
// and nowhere else.
func newStore() *store { return &store{} }

// at points a store at another file. It exists for tests.
func (s *store) at(path string) *store {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	return s
}

// file is where this store writes.
func (s *store) file() string {
	if path := strings.TrimSpace(s.path); path != "" {
		return path
	}
	return StorePath()
}

// Load reads yesterday's beliefs.
//
// A missing file is no beliefs and no error: it is the normal state of a
// machine that has not routed anything yet, and a caller that had to tell that
// apart from a real failure would end up treating both as neither.
func (s *store) Load() ([]Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.file()
	if path == "" {
		return nil, ErrNoStore
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var beliefs []Belief
	if err := json.Unmarshal(data, &beliefs); err != nil {
		return nil, err
	}
	kept := beliefs[:0]
	for _, belief := range beliefs {
		// A row that names no lane is a fact about a machine that was never
		// involved, whether it got into the file by hand or by a version of
		// this code that no longer exists.
		if belief.ID.Zero() {
			continue
		}
		kept = append(kept, belief)
	}
	return kept, nil
}

// Save writes the whole set, atomically.
func (s *store) Save(beliefs []Belief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.file()
	if path == "" {
		return ErrNoStore
	}
	data, err := json.Marshal(beliefs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// writeAtomic writes data at path through a temporary file in the same
// directory and a rename, which is the one write this repo makes to a small
// state file everywhere it makes one.
//
// A reader of either of these files is a cold process deciding where to send
// its first request, and a half-written one would be a belief nobody ever held.
func writeAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
