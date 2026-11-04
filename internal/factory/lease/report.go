package lease

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Renew uses the hold's identity rather than the job number so a delayed
// heartbeat cannot renew another run of the same job.
func (d *Desk) Renew(id, worker string) (Lease, error) {
	var renewed Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := receiptIdentity(doc, id, worker, now)
		if err != nil {
			return false, err
		}
		held.ExpiresAt = now.Add(d.TTL())
		held.HeartbeatAt = now
		renewed = *held
		return true, nil
	})
	return renewed, err
}

// Apply keeps ownership and the item's write in one lease critical section.
// Expiry cannot reassign the job between the holder check and its report.
// A completed job is written before release so a crash cannot offer it twice.
// capWait acknowledges a worker already parked on its reported budget question;
// it never admits a running step and retains the existing continue signal path.
func (d *Desk) Apply(id, worker string, complete, capWait bool, change func(*factory.Item) error) error {
	blocked := false
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			for i, receipt := range doc.Receipts {
				if receipt.Lease.ID != id || !receipt.Lease.live(now) {
					continue
				}
				if receipt.Lease.Worker != worker {
					return false, ErrWorker
				}
				if err := d.store.Update(receipt.Lease.Job, func(it *factory.Item) error {
					if !receipt.Restarted {
						return change(it)
					}
					// Validate the old report against an isolated stopped job. Only its
					// opened PR receipt may cross into the replacement run.
					raw, err := json.Marshal(receipt.Item)
					if err != nil {
						return err
					}
					var candidate factory.Item
					if err := json.Unmarshal(raw, &candidate); err != nil {
						return err
					}
					candidate.State = factory.StateNew
					if candidate.Stream == nil {
						candidate.Stream = &factory.Stream{}
					}
					candidate.Stream.Ended = now
					candidate.PullRequest = nil
					if err := change(&candidate); err != nil {
						return err
					}
					if candidate.PullRequest != nil && candidate.PublishStatus != nil && candidate.PublishStatus.State == "opened" {
						it.PullRequest = candidate.PullRequest
						it.Note(now, "PR opened by the stopped publisher: "+candidate.PullRequest.URL+"; not closed. Review it on GitHub.")
					}
					return nil
				}); err != nil {
					return false, err
				}
				if complete {
					doc.Receipts = append(doc.Receipts[:i], doc.Receipts[i+1:]...)
				}
				return complete, nil
			}
			return false, err
		}
		if err := d.store.Update(held.Job, func(it *factory.Item) error {
			if err := change(it); err != nil {
				return err
			}
			if !complete && holdAtJobBudget(it, now) {
				blocked = !capWait
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
		if blocked {
			withdraw(doc, *held)
			return true, nil
		}
		if !complete {
			return false, nil
		}
		return true, d.keepPublish(doc, held.Job)
	})
	if err == nil && blocked {
		return ErrWithdrawn
	}
	return err
}

// ThreadOnly checks the same lease identity as Apply before selecting the
// conversation report path. A replaced or expired hold cannot pass Apply.
func (d *Desk) ThreadOnly(id, worker string) (bool, error) {
	var only bool
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		held, err := receiptIdentity(doc, id, worker, d.now())
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
	if id != "" && doc.Withdrawn[worker] == id {
		return nil, ErrWithdrawn
	}
	return nil, ErrNoLease
}

// CheckPublish reads the current job under its row lock and the worker's lease.
// A worker must check again at each push and immediately before its PR request.
func (d *Desk) CheckPublish(id, worker string) error {
	return d.change(func(doc *document, _ time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		return false, d.store.Update(held.Job, func(it *factory.Item) error {
			if err := factory.CheckPersonStop(*it); err != nil {
				return err
			}
			return nil
		})
	})
}

func receiptIdentity(doc *document, id, worker string, now time.Time) (*Lease, error) {
	held, err := identified(doc, id, worker)
	if !errors.Is(err, ErrNoLease) {
		return held, err
	}
	for i := range doc.Receipts {
		held := &doc.Receipts[i].Lease
		if held.ID == id && id != "" && held.live(now) {
			if held.Worker != worker {
				return nil, ErrWorker
			}
			return held, nil
		}
	}
	return nil, err
}
