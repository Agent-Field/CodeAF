package lease

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
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
		var dirty bool
		renewed, dirty = d.renew(held, now)
		return dirty, nil
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
	// THE TWO MOMENTS A JOB'S SOURCE IS TOLD OF — its pull request opened, it
	// merged — and a stop that needs a person, all arrive here, in a worker's
	// report. They are told after the write is kept, never inside it
	// ([inbox.Notify]), so an outside system can neither slow nor fail a job.
	job, moments := 0, []inbox.Event(nil)
	observed := func(it *factory.Item, write func(*factory.Item) error) error {
		mark := inbox.MarkOf(*it)
		if err := write(it); err != nil {
			return err
		}
		job, moments = it.ID, mark.Moments(*it)
		return nil
	}
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
						return observed(it, change)
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
		rejected := false
		if err := d.store.Update(held.Job, func(it *factory.Item) error {
			if err := observed(it, change); err != nil {
				return err
			}
			// A RUN THAT ENDED ON THE PERSON'S NO ENDS ITS JOB REJECTED, here
			// as when the runner acknowledges carrying the Reject: the
			// control plane cancels a rejected run at once, and the runner's
			// reading of that cancel may arrive first. It is the person's
			// decision, never the failed step the cancel reads as.
			if it.Rejection == nil && it.State != factory.StateShipped {
				if no, ok := factory.AgentFieldRejectedEnd(*it); ok {
					history, err := record.Open(d.store, record.Options{Now: d.now})
					if err != nil {
						return err
					}
					if f := it.Failure; f != nil && f.Code != factory.CodeStopped {
						it.Failure = nil
					}
					// The step it answered carries no failure either, as a
					// native Reject leaves its step: an older runner stamped
					// the cancel on it as a tool_error.
					if it.Stream != nil {
						for i := range it.Stream.Phases {
							if f := it.Stream.Phases[i].Failure; f != nil && f.Code != factory.CodeStopped {
								it.Stream.Phases[i].Failure = nil
							}
						}
					}
					rejected = true
					_, err = endRejectedLocked(history, doc, held, held.Job, it, now, no.By, no.Words)
					return err
				}
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
				// A job an AgentField run carried ends when its run does, with
				// no pull request or comment to stamp it: its end is the run's.
				if it.State == factory.StateShipped && strings.TrimSpace(it.RunOn) != "" && it.DoneAt.IsZero() {
					it.DoneAt = factory.RunOnEnded(*it, now)
				}
			}
			return nil
		}); err != nil {
			return false, err
		}
		if rejected {
			return true, nil
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
	if err == nil {
		inbox.Tell(context.Background(), d.store, job, moments)
	}
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
	if id != "" && doc.Stopped[worker] == id {
		return nil, ErrStopped
	}
	return nil, ErrNoLease
}

// CheckPublish reads the current job under its row lock and the worker's lease.
// A worker must check again at each push and immediately before its PR request.
func (d *Desk) CheckPublish(id, worker string) error {
	var refusal error
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		return false, d.store.Update(held.Job, func(it *factory.Item) error {
			if err := factory.CheckPersonStop(*it); err != nil {
				return err
			}
			refusal = run.RefuseWrite(it, "write", now)
			return nil
		})
	})
	if err != nil {
		return err
	}
	return refusal
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
