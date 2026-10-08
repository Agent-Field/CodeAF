package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// SourceMeta is what a polling source last said about itself: when a read
// last succeeded, when one was last tried, and the trouble when the last try
// failed. It lives on disk beside the items, at <root>/sources.json, because
// THE PROCESS THAT POLLS IS NOT ALWAYS THE ONE THAT DRAWS: on the ordinary
// launch the engine polls and the window reads the floor, and both read this
// one file.
type SourceMeta struct {
	Polled  time.Time `json:"polled,omitzero"`
	Tried   time.Time `json:"tried,omitzero"`
	Trouble string    `json:"trouble,omitempty"`
	// Polling is true while a read is in flight. The poll writes it at the
	// start and the end of each read, and [Store.ClearBusy] takes it off when
	// a process starts, so a poll a crash cut short never reads as running.
	Polling bool `json:"polling,omitempty"`
}

type metaDoc struct {
	Schema  int                   `json:"schema"`
	Sources map[string]SourceMeta `json:"sources"`
	// Rail is the day's rail in dollars, the most the floor may spend in a
	// day, and 0 when none is set. It lives in this one record beside the
	// sources because it is the floor's, not an item's, and a second file for
	// one number is a second lock for one number.
	Rail float64 `json:"rail,omitempty"`
	// ReadCost is what the last triage read cost in dollars, and ReadSum and
	// Reads the running total and count, whose quotient is the average a
	// whole-floor refresh is estimated at. They are the same dollars the read
	// put on the spend ledger, kept here so the floor never scans the ledger
	// to draw one figure.
	ReadCost float64 `json:"read_cost,omitempty"`
	ReadSum  float64 `json:"read_sum,omitempty"`
	Reads    int     `json:"reads,omitempty"`
}

// MetaPath is <root>/sources.json.
func (st *Store) MetaPath() string { return filepath.Join(st.root, "sources.json") }

// SourceMeta reads one source's record; a source that never polled answers
// the zero record and no error.
func (st *Store) SourceMeta(name string) (SourceMeta, error) {
	if st == nil {
		return SourceMeta{}, nil
	}
	d, err := st.readMeta()
	if err != nil {
		return SourceMeta{}, err
	}
	return d.Sources[name], nil
}

// SetSourceMeta writes one source's record, keeping the others, as one
// read-modify-write under the file's flock.
func (st *Store) SetSourceMeta(name string, m SourceMeta) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "sources.lock"), func() error {
		d, err := st.readMeta()
		if err != nil {
			return err
		}
		if d.Sources == nil {
			d.Sources = map[string]SourceMeta{}
		}
		d.Sources[name] = m
		d.Schema = Schema
		data, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.MetaPath(), append(data, '\n'))
	})
}

// Rail reads the day's rail; no rail is 0 and no error.
func (st *Store) Rail() (float64, error) {
	if st == nil {
		return 0, nil
	}
	d, err := st.readMeta()
	return d.Rail, err
}

// SetRail writes the day's rail, keeping every source's record, as one
// read-modify-write under the file's flock. 0 takes the rail off; A RAIL IS
// NEVER BELOW NOTHING.
func (st *Store) SetRail(usd float64) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	if usd < 0 {
		return errors.New("factory store: a rail is never below nothing")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "sources.lock"), func() error {
		d, err := st.readMeta()
		if err != nil {
			return err
		}
		d.Rail = usd
		d.Schema = Schema
		data, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.MetaPath(), append(data, '\n'))
	})
}

func (st *Store) readMeta() (metaDoc, error) {
	data, err := os.ReadFile(st.MetaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return metaDoc{}, nil
		}
		return metaDoc{}, err
	}
	var d metaDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return metaDoc{}, fmt.Errorf("factory store: cannot read sources.json: %w", err)
	}
	if d.Schema > Schema {
		return metaDoc{}, fmt.Errorf("%w: sources.json", ErrNewer)
	}
	return d, nil
}

// TryPoller takes the floor's one poller lock without waiting: true and a
// release when this process may poll now, false when another process on this
// machine already is. TWO WINDOWS MUST NOT BOTH POLL ONE FLOOR: they would
// race to create the same forge item twice, and spend the rate limit twice.
func (st *Store) TryPoller() (release func(), ok bool) { return st.tryLock("poll.lock") }

// TryTriager takes the floor's one triage lock without waiting, as
// [Store.TryPoller] takes the poll's: TWO WINDOWS MUST NOT BOTH READ ONE NEW
// ITEM, because each read is a model call somebody pays for. It is a lock of
// its own rather than the poller's, so the process that triages is free to be
// a different one from the process that polls.
func (st *Store) TryTriager() (release func(), ok bool) { return st.tryLock("triage.lock") }

func (st *Store) tryLock(name string) (release func(), ok bool) {
	if st == nil {
		return func() {}, false
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return func() {}, false
	}
	lock, err := os.OpenFile(filepath.Join(st.root, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		lock.Close()
		return func() {}, false
	}
	return func() {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
	}, true
}

// changeMeta is one read-modify-write of the meta record under its flock.
func (st *Store) changeMeta(change func(*metaDoc)) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "sources.lock"), func() error {
		d, err := st.readMeta()
		if err != nil {
			return err
		}
		change(&d)
		d.Schema = Schema
		data, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.MetaPath(), append(data, '\n'))
	})
}

// SetPolling marks one source's read as in flight, or no longer, keeping the
// rest of its record.
func (st *Store) SetPolling(name string, on bool) error {
	return st.changeMeta(func(d *metaDoc) {
		if d.Sources == nil {
			d.Sources = map[string]SourceMeta{}
		}
		m := d.Sources[name]
		m.Polling = on
		d.Sources[name] = m
	})
}

// NoteReadCost remembers one triage read's cost: the last, and the running
// total and count. A read nobody priced (0 or less) is not counted, so the
// average is of real figures only.
func (st *Store) NoteReadCost(usd float64) error {
	if usd <= 0 {
		return nil
	}
	return st.changeMeta(func(d *metaDoc) {
		d.ReadCost = usd
		d.ReadSum += usd
		d.Reads++
	})
}

// ReadCost is the last triage read's cost and the average of every one
// noted; both are 0 when none was.
func (st *Store) ReadCost() (last, avg float64, err error) {
	if st == nil {
		return 0, 0, nil
	}
	d, err := st.readMeta()
	if err != nil || d.Reads == 0 {
		return d.ReadCost, 0, err
	}
	return d.ReadCost, d.ReadSum / float64(d.Reads), nil
}
