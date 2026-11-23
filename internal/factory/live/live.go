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

// File is the record's document under the factory root.
const File = "live-events.json"

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
	Next            uint64  `json:"next"`
	Events          []Event `json:"events"`
	OutcomesThrough uint64  `json:"outcomes_through,omitempty"`
	PolicyBatchEnd  uint64  `json:"policy_batch_end,omitempty"`
	PolicyBatchSize int     `json:"policy_batch_size,omitempty"`
	GrantBatchEnd   uint64  `json:"grant_batch_end,omitempty"`
	GrantBatchSize  int     `json:"grant_batch_size,omitempty"`
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
	return commit(root, events, write, ordinaryBatch)
}

// PolicyChangeLimit is the most individual audience changes sent with a policy update.
const PolicyChangeLimit = 500

// AppendPolicyChange keeps a policy update's related events together through the next replay window.
func AppendPolicyChange(root string, events ...Event) error {
	return commit(root, events, func() error { return nil }, policyBatch)
}

// AppendGrantLapses holds a sweep's full batch through the replay window
// because a roomful of expiries can pass between two filesystem notices.
func AppendGrantLapses(root string, events ...Event) error {
	return commit(root, events, func() error { return nil }, grantBatch)
}

type batchKind uint8

const (
	ordinaryBatch batchKind = iota
	policyBatch
	grantBatch
)

func commit(root string, events []Event, write func() error, batch batchKind) error {
	if root == "" || len(events) == 0 {
		return write()
	}
	return locked(root, true, func(d *document) error {
		before := *d
		before.Events = append([]Event(nil), d.Events...)
		outcomes, notifications, err := prepareOutcomes(root, d, events)
		if err != nil {
			return err
		}
		for _, e := range notifications {
			d.Next++
			e.ID = "live_" + strconv.FormatUint(d.Next, 10)
			d.Events = append(d.Events, e)
		}
		if batch == policyBatch && len(notifications) > 1 {
			d.PolicyBatchEnd = d.Next
			d.PolicyBatchSize = min(len(notifications), PolicyChangeLimit+1)
		}
		if batch == grantBatch && len(notifications) > Limit {
			d.GrantBatchEnd, d.GrantBatchSize = d.Next, len(notifications)
		}
		window := Limit
		if d.PolicyBatchEnd <= d.Next && d.Next-d.PolicyBatchEnd < Limit {
			window += min(max(d.PolicyBatchSize, 0), PolicyChangeLimit+1)
		} else {
			d.PolicyBatchEnd, d.PolicyBatchSize = 0, 0
		}
		if d.GrantBatchEnd <= d.Next && d.Next-d.GrantBatchEnd < Limit {
			window += max(d.GrantBatchSize, 0)
		} else {
			d.GrantBatchEnd, d.GrantBatchSize = 0, 0
		}
		if len(d.Events) > window {
			d.Events = d.Events[len(d.Events)-window:]
		}
		d.OutcomesThrough = d.Next
		if err := outcomes.save(root); err != nil {
			return err
		}
		if err := save(root, d); err != nil {
			return errors.Join(err, outcomes.restore(root))
		}
		if err := write(); err != nil {
			return errors.Join(err, save(root, &before), outcomes.restore(root))
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
	return os.Rename(f.Name(), filepath.Join(root, File))
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
	data, err := os.ReadFile(filepath.Join(root, File))
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

// ForgetJobs removes replay payloads for work items deliberately erased by
// the issue-only migration, preserving sequence IDs and other notifications.
func ForgetJobs(root string, ids map[int]bool) error {
	return locked(root, true, func(d *document) error {
		kept := d.Events[:0]
		for _, e := range d.Events {
			if !ids[e.Job] {
				kept = append(kept, e)
			}
		}
		d.Events = kept
		if err := forgetOutcomes(root, ids); err != nil {
			return err
		}
		return save(root, d)
	})
}
