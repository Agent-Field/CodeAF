package lease

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

const schema = 1

type document struct {
	Schema int     `json:"schema"`
	Leases []Lease `json:"leases"`
	// Signals are what people asked of a job that its worker has not yet
	// collected, keyed by job id. They live beside the leases, under the same
	// lock, so a verb and the hold it is meant for can never disagree.
	Signals map[int][]Signal `json:"signals,omitempty"`
}

// Path names the shared document beside the factory's numbered job files.
func (d *Desk) Path() string { return filepath.Join(d.store.Root(), "leases.json") }

func (d *Desk) locked(change func(*document, time.Time) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	lock, err := os.OpenFile(filepath.Join(d.store.Root(), "leases.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	doc, err := d.read()
	if err != nil {
		return err
	}
	return change(&doc, d.now())
}

func (d *Desk) read() (document, error) {
	data, err := os.ReadFile(d.Path())
	if os.IsNotExist(err) {
		return document{Schema: schema}, nil
	}
	if err != nil {
		return document{}, err
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("factory lease: cannot read leases.json: %w", err)
	}
	if doc.Schema != schema {
		return document{}, fmt.Errorf("factory lease: unsupported leases.json schema %d", doc.Schema)
	}
	return doc, validate(doc.Leases)
}

// validate refuses an unreadable desk rather than treating it as empty. Losing
// evidence of ownership is never permission to start the same work again.
func validate(leases []Lease) error {
	jobs, workers, ids := map[int]bool{}, map[string]bool{}, map[string]bool{}
	for _, held := range leases {
		if held.Job <= 0 || held.Worker == "" || factory.NormalizeHandle(held.Handle) == "" || held.ExpiresAt.IsZero() {
			return fmt.Errorf("factory lease: invalid lease for job %d", held.Job)
		}
		if jobs[held.Job] || workers[held.Worker] || (held.ID != "" && ids[held.ID]) {
			return fmt.Errorf("factory lease: duplicate job or worker in leases.json")
		}
		jobs[held.Job], workers[held.Worker], ids[held.ID] = true, true, true
	}
	return nil
}

func (d *Desk) write(doc document) error {
	doc.Schema = schema
	if doc.Leases == nil {
		doc.Leases = []Lease{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	write := d.persist
	if write == nil {
		write = writeAtomic
	}
	return write(d.Path(), append(data, '\n'))
}

// writeAtomic publishes a complete synced document; a partial write can never
// turn the live claims desk into an empty or undecodable file.
func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".leases.*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
