package lease

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
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
	// SignalShape asks the holding worker to give the job's manager one
	// shaping turn now. Nothing is launched. A job nobody holds is not
	// signaled: the factory runs that turn itself, where the worker lives.
	SignalShape = "shape"
)

// Signal is one verb a person asked of a job, with the door's own arguments.
type Signal struct {
	ID    string `json:"id,omitempty"`
	Verb  string `json:"verb"`
	Yes   bool   `json:"yes,omitempty"`
	Words string `json:"words,omitempty"`
}

// Drain hands the holder of lease id every signal waiting for its job, oldest
// first, and forgets them. A lease that is gone answers [ErrNoLease], which is
// how a worker learns its job was stopped.
func (d *Desk) Drain(id, worker string) ([]Signal, error) {
	return d.drain(id, worker, true)
}

// DrainLive carries interactive verbs through a heartbeat. Publishing stays
// queued until the finishing run releases its lease and a publish pull takes it.
func (d *Desk) DrainLive(id, worker string) ([]Signal, error) {
	return d.drain(id, worker, false)
}

func (d *Desk) drain(id, worker string, publishing bool) ([]Signal, error) {
	var out []Signal
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		it, err := d.store.Get(held.Job)
		if err != nil {
			return false, err
		}
		for _, m := range it.Thread {
			if m.State == "pending" {
				out = append(out, Signal{Verb: SignalReply, ID: m.ID, Words: m.Text})
			}
		}
		var kept []Signal
		for _, s := range doc.Signals[held.Job] {
			if s.Verb == SignalPublish && !publishing {
				kept = append(kept, s)
			} else {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return false, nil
		}
		for _, s := range out {
			if s.Verb == SignalPublish {
				held.Publish = s.Words
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

// keepPublish is release that leaves a publish waiting: the job goes back to
// the queue with its publish signal, so the next pull carries the approval
// out. An approval given while the job's worker was still finishing its run
// is never dropped with that worker's lease. Callers hold the desk's lock.
func (d *Desk) keepPublish(doc *document, job int) error {
	var kept []Signal
	for _, s := range doc.Signals[job] {
		if s.Verb == SignalPublish {
			kept = append(kept, s)
		}
	}
	release(doc, job)
	if len(kept) == 0 {
		return nil
	}
	for _, s := range kept {
		signal(doc, job, s)
	}
	return d.store.Update(job, func(it *factory.Item) error {
		it.State = factory.StateQueued
		it.Publishing = true
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
	return nil
}
