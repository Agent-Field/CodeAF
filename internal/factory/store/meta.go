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
}

type metaDoc struct {
	Schema  int                   `json:"schema"`
	Sources map[string]SourceMeta `json:"sources"`
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
func (st *Store) TryPoller() (release func(), ok bool) {
	if st == nil {
		return func() {}, false
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return func() {}, false
	}
	lock, err := os.OpenFile(filepath.Join(st.root, "poll.lock"), os.O_CREATE|os.O_RDWR, 0o600)
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
