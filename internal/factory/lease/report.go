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
		if err := d.store.Update(held.Job, change); err != nil {
			return false, err
		}
		if !complete {
			return false, nil
		}
		release(doc, held.Job)
		return true, nil
	})
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
