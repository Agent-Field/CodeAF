package lease

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Renew uses the hold's identity rather than the job number so a delayed
// heartbeat cannot renew another run of the same job.
func (d *Desk) Renew(id, worker string) (Lease, error) {
	var renewed Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		held.ExpiresAt = now.Add(d.ttl)
		held.HeartbeatAt = now
		renewed = *held
		return true, nil
	})
	return renewed, err
}

// Apply keeps ownership and the item's write in one lease critical section.
// Expiry cannot reassign the job between the holder check and its report.
// A completed job is written before release so a crash cannot offer it twice.
func (d *Desk) Apply(id, worker string, complete bool, change func(*factory.Item) error) error {
	return d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		if err := d.store.Update(held.Job, func(it *factory.Item) error {
			if err := change(it); err != nil {
				return err
			}
			if complete && !held.ThreadOnly {
				it.Publishing = false
				if it.State == factory.StateShipped && it.PullRequest == nil && it.CommentURL != "" {
					it.DoneHow = "answered"
					it.DoneAt = now
				}
			}
			return nil
		}); err != nil {
			return false, err
		}
		if !complete {
			return false, nil
		}
		return true, d.keepPublish(doc, held.Job)
	})
}

// ThreadOnly checks the same lease identity as Apply before selecting the
// conversation report path. A replaced or expired hold cannot pass Apply.
func (d *Desk) ThreadOnly(id, worker string) (bool, error) {
	var only bool
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		only = held.ThreadOnly
		return false, nil
	})
	return only, err
}

func identified(doc *document, id, worker string) (*Lease, error) {
	for i := range doc.Leases {
		held := &doc.Leases[i]
		if held.ID != id || id == "" {
			continue
		}
		if held.Worker != worker {
			return nil, ErrWorker
		}
		return held, nil
	}
	return nil, ErrNoLease
}
