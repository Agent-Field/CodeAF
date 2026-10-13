// Package live keeps the bounded notification record beside the factory's jobs.
// It has no store dependency so every writer, including another process, records
// transitions before the filesystem watch can coalesce their notifications.
package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Limit is the replay window shared by the record and the event stream.
const Limit = 200

// Event keeps routing facts outside Data so the SSE payload remains small.
type Event struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Job    int             `json:"job,omitempty"`
	Handle string          `json:"handle,omitempty"`
	Data   json.RawMessage `json:"data"`
}

type document struct {
	Next   uint64  `json:"next"`
	Events []Event `json:"events"`
}

// Append syncs and atomically replaces the bounded record under a shared flock.
func Append(root string, events ...Event) error {
	return Commit(root, events, func() error { return nil })
}

// Commit makes notifications durable before publishing a job snapshot. Holding
// the replay lock through the job write prevents a stream from seeing an event
// before the job exists. A refused write restores the previous record. There is
// no notification I/O after the snapshot becomes visible to a runner's next door.
func Commit(root string, events []Event, write func() error) error {
	if root == "" || len(events) == 0 {
		return write()
	}
	return locked(root, true, func(d *document) error {
		before := *d
		before.Events = append([]Event(nil), d.Events...)
		for _, e := range events {
			d.Next++
			e.ID = "live_" + strconv.FormatUint(d.Next, 10)
			d.Events = append(d.Events, e)
		}
		if len(d.Events) > Limit {
			d.Events = d.Events[len(d.Events)-Limit:]
		}
		if err := save(root, d); err != nil {
			return err
		}
		if err := write(); err != nil {
			return errors.Join(err, save(root, &before))
		}
		return nil
	})
}

func save(root string, d *document) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".live-events.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(root, "live-events.json"))
}

// Read returns append order and the latest cursor even when the record is empty.
func Read(root string) (events []Event, cursor string, err error) {
	if root == "" {
		return nil, "", nil
	}
	err = locked(root, false, func(d *document) error {
		events = d.Events
		if d.Next != 0 {
			cursor = "live_" + strconv.FormatUint(d.Next, 10)
		}
		return nil
	})
	return
}

func locked(root string, exclusive bool, fn func(*document) error) error {
	f, err := os.OpenFile(filepath.Join(root, "live-events.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = filelock.Lock(f, exclusive, false); err != nil {
		return err
	}
	defer filelock.Unlock(f)
	var d document
	data, err := os.ReadFile(filepath.Join(root, "live-events.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) != 0 {
		if err = json.Unmarshal(data, &d); err != nil {
			return fmt.Errorf("factory live record: %w", err)
		}
	}
	return fn(&d)
}

// New encodes a typed payload at the writer, rather than reconstructing it later.
func New(kind string, job int, payload any) Event {
	data, _ := json.Marshal(payload)
	return Event{Kind: kind, Job: job, Data: data}
}

// Payload is the additive union of the small SSE payloads. Fields are populated
// only for their event: needs_you, question, approved, taken, publishing,
// published, publish_failed or machine. A cleared need has kind empty.
type Payload struct {
	Job    int                  `json:"job,omitempty"`
	Kind   *string              `json:"kind,omitempty"`
	Since  *time.Time           `json:"since,omitempty"`
	By     string               `json:"by,omitempty"`
	State  string               `json:"state,omitempty"`
	PR     *factory.PublishedPR `json:"pr,omitempty"`
	Reason string               `json:"reason,omitempty"`
	Name   string               `json:"name,omitempty"`
	Status string               `json:"status,omitempty"`
}

// PublishedPayload keeps pr present even when the receipt is a proof comment.
type PublishedPayload struct {
	Job int                  `json:"job"`
	PR  *factory.PublishedPR `json:"pr"`
}

// FailedPayload keeps the reason field present for older reports without a code.
type FailedPayload struct {
	Job    int    `json:"job"`
	Reason string `json:"reason"`
}

// StatePayload names every projected job transition, including a cleared reason.
type StatePayload struct {
	Job    int    `json:"job"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}
