package lease

import (
	"crypto/rand"
	"errors"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE SIGNAL CHANNEL. A person's verb reaches the server, never the machine
// running the job. The server changes the job's state itself and, while a
// worker holds the job, leaves a signal beside the lease; the worker collects
// it on its next heartbeat (or, for a job queued with one, on the pull that
// takes it) and carries it through its own runner's door. Nothing else rides
// the channel: results travel the other way as reports.

// The verbs a signal carries.
const (
	// SignalHold carries Seam.Hold: Yes is on (pause), false goes on.
	SignalHold = "hold"
	// SignalBudget carries the exact selected limit to the assignee's runner.
	SignalBudget = "budget"
	// SignalAnswer carries the person's answer to the question the run waits on.
	SignalAnswer = "answer"
	// SignalSteer carries words for the round running now.
	SignalSteer = "steer"
	// SignalReply carries a pending manager reply; ID is acknowledged by report.
	SignalReply = "reply"
	// SignalRecheck asks the pulling worker to run the checks of a landed job
	// again instead of running its steps.
	SignalRecheck = "recheck"
	// SignalPublish carries a person's approval at the proof gate: Words is
	// the record's trailer (`Factory-Approval: evt_… @handle`), and the
	// worker that takes the job commits, pushes and opens its pull request.
	SignalPublish = "publish"
	// SignalMerge carries a merge the factory's gate passed: Words is the
	// exact commit to merge, and the worker that pulls the job reads the pull
	// request again, runs the same gate and merges only that commit.
	SignalMerge = "merge"
	// SignalStop tells the holding worker to stop. Drop sends it. Stop keeps
	// the branch; the signal does not delete it.
	SignalStop = "stop"
	// SignalModel carries the model a person chose for the job (Words, ""
	// for the default) to the worker running it, whose next step opens on it
	// and whose running step switches at its next turn.
	SignalModel     = "model"
	SignalAutonomy  = "autonomy"
	SignalReconcile = "reconcile_results"
)

// Signal is one verb a person asked of a job, with the door's own arguments.
type Signal struct {
	// Delivery identifies transport separately from a question or message ID.
	Delivery  string  `json:"delivery,omitempty"`
	Automatic *bool   `json:"automatic,omitempty"`
	Budget    float64 `json:"budget,omitempty"`
	ID        string  `json:"id,omitempty"`
	Verb      string  `json:"verb"`
	Yes       bool    `json:"yes,omitempty"`
	Words     string  `json:"words,omitempty"`
}

// Drain hands the holder of lease id every signal waiting for its job, oldest
// first, and forgets them. A lease that is gone answers [ErrNoLease], which is
// how a worker learns its job was stopped.
func (d *Desk) Drain(id, worker string) ([]Signal, error) {
	return d.drain(id, worker, true)
}

// DrainLive carries interactive verbs through a heartbeat. Publishing stays
// queued until the finishing run releases its lease and a publish pull takes it.
// Acknowledgements opt into retained delivery; older workers keep the hold fallback.
func (d *Desk) DrainLive(id, worker string, acknowledged ...[]string) ([]Signal, error) {
	return d.drain(id, worker, false, acknowledged...)
}

func (d *Desk) drain(id, worker string, publishing bool, acknowledged ...[]string) ([]Signal, error) {
	reliable := len(acknowledged) > 0
	received := func(id string) bool { return reliable && id != "" && slices.Contains(acknowledged[0], id) }
	var out []Signal
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			if _, receiptErr := receiptIdentity(doc, id, worker, now); receiptErr == nil {
				s := Signal{Verb: SignalStop}
				if reliable {
					s.Delivery = id + "/stop"
				}
				if !received(s.Delivery) {
					out = []Signal{s}
				}
				return false, nil
			}
			return false, err
		}
		it, err := d.store.Get(held.Job)
		if err != nil {
			return false, err
		}
		for _, m := range it.Thread {
			if m.State == "pending" {
				s := Signal{Verb: SignalReply, ID: m.ID, Words: m.Text}
				if reliable {
					s.Delivery = held.ID + "/reply/" + m.ID
				}
				if !received(s.Delivery) {
					out = append(out, s)
				}
			}
		}
		var kept []Signal
		holdSent := false
		stopped := factory.StoppedByPerson(it)
		for _, s := range doc.Signals[held.Job] {
			if received(s.Delivery) {
				continue
			}
			if stopped && s.Verb == SignalHold {
				continue
			}
			if reliable && !publishing && s.Verb == SignalHold && !run.StoredPaused(it) {
				continue
			}
			if !reliable && !publishing && s.Verb == SignalHold {
				// Resume queues the next lease. A queued older hold must not undo it.
				if !holdSent && run.StoredPaused(it) {
					out = append(out, Signal{Verb: SignalHold, Yes: true})
					holdSent = true
				}
				continue
			}
			if onPull(s.Verb) && !publishing {
				kept = append(kept, s)
			} else {
				if reliable {
					if s.Delivery == "" {
						s.Delivery = held.ID + "/" + rand.Text()
					}
					kept = append(kept, s)
				}
				out = append(out, s)
			}
		}
		// Hold is desired state, not a one-shot notification. Reuse the existing
		// signal vocabulary so older workers also retry a lost heartbeat reply.
		if !stopped && !reliable && !publishing && !holdSent && run.StoredPaused(it) {
			out = append(out, Signal{Verb: SignalHold, Yes: true})
		}
		if len(out) == 0 && len(doc.Signals[held.Job]) == len(kept) {
			return false, nil
		}
		for _, s := range out {
			switch s.Verb {
			case SignalPublish:
				held.Publish = s.Words
			case SignalMerge:
				held.Merge = s.Words
			}
		}
		if len(kept) == 0 {
			delete(doc.Signals, held.Job)
		} else {
			doc.Signals[held.Job] = kept
		}
		return true, nil
	})
	return out, err
}

// SignalIfHeld leaves s for the worker holding job and answers whether one
// does. A job nobody holds is unchanged: the caller runs the turn itself.
func (d *Desk) SignalIfHeld(job int, s Signal) (bool, error) {
	held := false
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		if find(doc, job) == nil {
			return false, nil
		}
		signal(doc, job, s)
		held = true
		return true, nil
	})
	return held, err
}

// signal leaves s for job's worker. Callers hold the desk's lock.
func signal(doc *document, job int, s Signal) {
	if doc.Signals == nil {
		doc.Signals = map[int][]Signal{}
	}
	doc.Signals[job] = append(doc.Signals[job], s)
}

// release takes job's hold and its uncollected signals off the desk. Callers
// hold the desk's lock.
func release(doc *document, job int) {
	for i := range doc.Leases {
		if doc.Leases[i].Job == job {
			doc.Leases = append(doc.Leases[:i], doc.Leases[i+1:]...)
			break
		}
	}
	delete(doc.Signals, job)
}

// keepPublish is release that leaves startup work waiting: the job goes back to
// the queue with its publish signal, so the next pull carries the approval
// out. An approval given while the job's worker was still finishing its run
// is never dropped with that worker's lease. Callers hold the desk's lock.
func (d *Desk) keepPublish(doc *document, job int) error {
	var kept []Signal
	publishing, merging := false, false
	for _, s := range doc.Signals[job] {
		if onPull(s.Verb) {
			kept = append(kept, s)
			publishing = publishing || s.Verb == SignalPublish
			merging = merging || s.Verb == SignalMerge
		}
	}
	release(doc, job)
	if len(kept) == 0 {
		return nil
	}
	for _, s := range kept {
		signal(doc, job, s)
	}
	if !publishing {
		// A merge or recheck queued while the worker finishes belongs to the
		// next pull, without turning the job into a publish.
		return d.store.Update(job, func(it *factory.Item) error {
			if merging && (it.Merging == nil || it.Merging.State != factory.MergeQueued) {
				return nil
			}
			it.State = factory.StateQueued
			return nil
		})
	}
	return d.store.Update(job, func(it *factory.Item) error {
		// An interrupted publish stays visible until its person chooses to
		// resume; retaining consent is not permission to retry a lost write.
		if it.State == factory.StateNeedsYou && it.QKind == factory.NeedsRunnerOffline {
			return nil
		}
		it.State = factory.StateQueued
		it.Publishing = true
		if it.PublishStatus != nil {
			copy := *it.PublishStatus
			copy.State = "queued"
			it.PublishStatus = &copy
		}
		return nil
	})
}

// restorePublish returns a collected approval to the queue only when its
// holder disconnects; normal completion consumes it permanently.
func (d *Desk) restorePublish(doc *document, job int) error {
	if held := find(doc, job); held != nil && held.Publish != "" {
		it, err := d.store.Get(job)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if it.Publishing {
			signal(doc, job, Signal{Verb: SignalPublish, Words: held.Publish})
		}
	}
	if held := find(doc, job); held != nil && held.Merge != "" {
		it, err := d.store.Get(job)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		// A merge whose runner vanished before reporting goes back as it was
		// asked; the next runner reads the pull request first, so a merge that
		// did happen is seen as merged and never sent twice.
		if m := it.Merging; m != nil && (m.State == factory.MergeQueued || m.State == factory.MergeMerging) {
			signal(doc, job, Signal{Verb: SignalMerge, Words: held.Merge})
			_ = d.store.Update(job, func(it *factory.Item) error {
				if it.Merging != nil && it.Merging.State == factory.MergeMerging {
					cp := *it.Merging
					cp.State = factory.MergeQueued
					it.Merging = &cp
				}
				return nil
			})
		}
	}
	return nil
}

// carriedOut is a verb whose work happens on the pulling worker after the
// run, never through a heartbeat: a publish and a merge.
func carriedOut(verb string) bool { return verb == SignalPublish || verb == SignalMerge }

// onPull keeps startup work out of a finishing worker's heartbeat.
func onPull(verb string) bool { return carriedOut(verb) || verb == SignalRecheck }

// CarriedMerge says the hold named by id was given a merge to carry out, so
// its done is the merge's and not a publish.
func (d *Desk) CarriedMerge(id, worker string) (bool, error) {
	var merge bool
	err := d.change(func(doc *document, _ time.Time) (bool, error) {
		held, err := receiptIdentity(doc, id, worker, d.now())
		if err != nil {
			return false, err
		}
		merge = held.Merge != ""
		return false, nil
	})
	return merge, err
}
