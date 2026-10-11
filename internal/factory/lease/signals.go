package lease

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
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
	// SignalRecheck asks the pulling worker to run the checks of a landed job
	// again instead of running its steps.
	SignalRecheck = "recheck"
	// SignalPublish carries a person's approval at the proof gate: Words is
	// the record's trailer (`Factory-Approval: evt_… @handle`), and the
	// worker that takes the job commits, pushes and opens its pull request.
	SignalPublish = "publish"
)

// Signal is one verb a person asked of a job, with the door's own arguments.
type Signal struct {
	Verb  string `json:"verb"`
	Yes   bool   `json:"yes,omitempty"`
	Words string `json:"words,omitempty"`
}

// Drain hands the holder of lease id every signal waiting for its job, oldest
// first, and forgets them. A lease that is gone answers [ErrNoLease], which is
// how a worker learns its job was stopped.
func (d *Desk) Drain(id, worker string) ([]Signal, error) {
	var out []Signal
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := identified(doc, id, worker)
		if err != nil {
			return false, err
		}
		out = doc.Signals[held.Job]
		if len(out) == 0 {
			return false, nil
		}
		delete(doc.Signals, held.Job)
		return true, nil
	})
	return out, err
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
		return nil
	})
}
