package decide

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	// MaxDecisions caps one place's ledger; the oldest fall off first.
	MaxDecisions = 2000
	// MaxAge is how long a decision is kept before it is dropped.
	MaxAge = 90 * 24 * time.Hour

	fileVersion = 1
)

var (
	// ErrInvalid is a malformed place id, kind or decision.
	ErrInvalid = errors.New("decide: invalid argument")
	// ErrNotFound is a decision id the ledger does not hold.
	ErrNotFound = errors.New("decide: decision not found")
)

// doc is the on-disk shape of one place's file.
type doc struct {
	Version   int                  `json:"version"`
	Decisions []Decision           `json:"decisions"`
	Modes     map[string]KindState `json:"modes"`
}

// Store is one place's ledger: a single JSON file, rewritten whole by rename
// and guarded by a flock so two processes never lose each other's write.
type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex // flock excludes processes; this excludes goroutines
	// recovered is the set-aside path of the last damaged file, if any.
	recovered string
}

// LedgerFile is the file Open uses for placeID. Callers that only want to
// know whether a ledger exists yet stat this path: Open creates the directory,
// and listing creates the lock, so a poll must not Open a place that has
// never decided anything.
func LedgerFile(dir, placeID string) (string, error) {
	if placeID == "" || placeID == "." || placeID == ".." || strings.ContainsAny(placeID, `/\`+"\x00") {
		return "", fmt.Errorf("%w: place id %q", ErrInvalid, placeID)
	}
	return filepath.Join(dir, placeID+".decisions.json"), nil
}

// Open returns the store for placeID under dir (the directory beside the
// placegraph state). The file is created on first write, not here.
func Open(dir, placeID string, now func() time.Time) (*Store, error) {
	path, err := LedgerFile(dir, placeID)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{path: path, now: now}, nil
}

// Recovered returns where the last damaged file was set aside, or "".
func (s *Store) Recovered() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recovered
}

// Append records a decision, assigning no id: callers own ids so an undo token
// can name them. It prunes by age and cap in the same write.
func (s *Store) Append(d Decision) error {
	if d.ID == "" || d.At.IsZero() {
		return fmt.Errorf("%w: decision needs id and time", ErrInvalid)
	}
	return s.update(func(c *doc) error {
		for _, have := range c.Decisions {
			if have.ID == d.ID {
				return fmt.Errorf("%w: duplicate decision %q", ErrInvalid, d.ID)
			}
		}
		c.Decisions = append(c.Decisions, d)
		return nil
	})
}

// List returns the ledger oldest first.
func (s *Store) List() ([]Decision, error) {
	var out []Decision
	err := s.view(func(c *doc) { out = append(out, c.Decisions...) })
	return out, err
}

// Overturn stamps a decision as reversed and records the outcome in its kind's
// ring. Overturning twice keeps the first time.
func (s *Store) Overturn(id string, at time.Time) error {
	return s.update(func(c *doc) error {
		for i := range c.Decisions {
			d := &c.Decisions[i]
			if d.ID != id {
				continue
			}
			if d.OverturnedAt == nil {
				t := at
				d.OverturnedAt = &t
				pushOutcome(c, d.AskKind, Outcome{DecisionID: id, Overturned: true, At: at})
			}
			return nil
		}
		return ErrNotFound
	})
}

// Mode returns the state for kindKey. An unknown kind is learning with no
// history: a place starts by watching.
func (s *Store) Mode(kindKey string) (KindState, error) {
	st := KindState{Mode: ModeLearning}
	err := s.view(func(c *doc) {
		if have, ok := c.Modes[kindKey]; ok {
			st = have
			st.Recent = append([]Outcome(nil), have.Recent...)
		}
	})
	return st, err
}

// Modes returns every kind the place has a state for, keyed by kind key. A
// kind never asked about is absent, not learning with an empty ring.
func (s *Store) Modes() (map[string]KindState, error) {
	out := map[string]KindState{}
	err := s.view(func(c *doc) {
		for key, st := range c.Modes {
			st.Recent = append([]Outcome(nil), st.Recent...)
			out[key] = st
		}
	})
	return out, err
}

// SetMode changes the mode for kindKey and keeps its ring.
func (s *Store) SetMode(kindKey string, m Mode) error {
	if kindKey == "" || !m.Valid() {
		return fmt.Errorf("%w: mode %q for kind %q", ErrInvalid, m, kindKey)
	}
	return s.update(func(c *doc) error {
		st := c.Modes[kindKey]
		st.Mode = m
		c.Modes[kindKey] = st
		return nil
	})
}

// RecordOutcome pushes an outcome onto kindKey's ring, dropping the oldest past
// RingSize. A kind never set keeps the learning mode.
func (s *Store) RecordOutcome(kindKey string, o Outcome) error {
	if kindKey == "" {
		return fmt.Errorf("%w: empty kind", ErrInvalid)
	}
	return s.update(func(c *doc) error {
		pushOutcome(c, kindKey, o)
		return nil
	})
}

func pushOutcome(c *doc, kindKey string, o Outcome) {
	st := c.Modes[kindKey]
	if st.Mode == "" {
		st.Mode = ModeLearning
	}
	st.Recent = append(st.Recent, o)
	if over := len(st.Recent) - RingSize; over > 0 {
		st.Recent = st.Recent[over:]
	}
	c.Modes[kindKey] = st
}

// lock takes the goroutine mutex and the cross-process flock.
func (s *Store) lock() (func(), error) {
	s.mu.Lock()
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := filelock.Lock(f, true, false); err != nil {
		_ = f.Close()
		s.mu.Unlock()
		return nil, err
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		s.mu.Unlock()
	}, nil
}

func (s *Store) view(fn func(*doc)) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	c, err := s.load()
	if err != nil {
		return err
	}
	fn(c)
	return nil
}

func (s *Store) update(fn func(*doc) error) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	c, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	s.prune(c)
	return s.write(c)
}

// load reads the file. A damaged file is renamed aside and the ledger starts
// empty: losing history beats refusing every future decision.
func (s *Store) load() (*doc, error) {
	empty := &doc{Version: fileVersion, Modes: map[string]KindState{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return nil, err
	}
	var c doc
	if err := json.Unmarshal(b, &c); err != nil || c.Version != fileVersion {
		aside := fmt.Sprintf("%s.corrupt-%s", s.path, s.now().UTC().Format("20060102T150405.000000000Z"))
		if rerr := os.Rename(s.path, aside); rerr != nil {
			return nil, rerr
		}
		s.recovered = aside
		return empty, nil
	}
	if c.Modes == nil {
		c.Modes = map[string]KindState{}
	}
	return &c, nil
}

// prune drops decisions past MaxAge, then the oldest beyond MaxDecisions.
func (s *Store) prune(c *doc) {
	cutoff := s.now().Add(-MaxAge)
	kept := c.Decisions[:0]
	for _, d := range c.Decisions {
		if !d.At.Before(cutoff) {
			kept = append(kept, d)
		}
	}
	if over := len(kept) - MaxDecisions; over > 0 {
		kept = kept[over:]
	}
	c.Decisions = kept
}

// write replaces the file whole: temp, fsync, rename. Any failure leaves the
// old file in place and removes the temp.
func (s *Store) write(c *doc) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".decide-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if n, err := tmp.Write(data); err != nil {
		return fail(err)
	} else if n != len(data) {
		return fail(io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
