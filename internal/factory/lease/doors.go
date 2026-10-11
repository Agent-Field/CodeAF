package lease

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// Doors is the runner's doors on a factory that runs nothing itself
// ([factory.RunnerDoors] and [factory.HoldDoor]). A person's verb becomes a
// change of the job's state in the store and, while a worker holds the job, a
// [Signal] on its lease; the worker's own Runner carries the verb out and its
// writes come back as reports. Every process with the store can hang these
// doors, because they only write files under the desk's lock: no process owns
// the floor.
type Doors struct {
	desk *Desk
}

// NewDoors hangs the doors over desk's store.
func NewDoors(desk *Desk) *Doors { return &Doors{desk: desk} }

// update changes job id under the desk's lock: held is its live lease, nil
// when no worker holds it. change answers whether the desk itself changed.
func (d *Doors) update(id int, change func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error)) error {
	return d.desk.change(func(doc *document, now time.Time) (bool, error) {
		held := find(doc, id)
		dirty := false
		err := d.desk.store.Update(id, func(it *factory.Item) error {
			var err error
			dirty, err = change(doc, held, it, now)
			return err
		})
		return dirty && err == nil, err
	})
}

// Launch queues the job for a worker; a paused job goes on instead.
func (d *Doors) Launch(id int) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	if run.StoredPaused(it) {
		return d.Hold(id, false)
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if held != nil {
			return false, run.AlreadyRunning(it.Ref())
		}
		if err := run.QueueItem(it, now); err != nil {
			return false, err
		}
		_, stale := doc.Signals[id]
		delete(doc.Signals, id)
		return stale, nil
	})
}

// Stop puts the job back to new and takes its lease away: the worker learns
// it on its next heartbeat or report, which the server no longer accepts, and
// stops its runner. A dead worker needs to learn nothing.
func (d *Doors) Stop(id int) error { return d.stop(id, nil) }

// StopAs records the caller when the HTTP door has authenticated one.
// An unknown caller stays blank; only the local Stop door uses the job's person.
func (d *Doors) StopAs(id int, handle string) error { return d.stop(id, &handle) }

func (d *Doors) stop(id int, caller *string) error {
	history, err := record.Open(d.desk.store, record.Options{Now: d.desk.now})
	if err != nil {
		return err
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if err := run.StopItem(it, now); err != nil {
			return false, err
		}
		// THE PERSON'S STOP WINS: keep it durable while the job is locked,
		// before intake can see the job back in the backlog.
		if _, err := history.Append(context.Background(), record.Event{Kind: record.Stopped, Job: id, Handle: stopHandle(caller, it.Person())}); err != nil {
			return false, err
		}
		it.Publishing = false
		if held == nil {
			return false, nil
		}
		release(doc, id)
		return true, nil
	})
}

// Pause holds a running job, or lets a held one go on.
func (d *Doors) Pause(id int) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	return d.Hold(id, !run.StoredPaused(it))
}

// Hold says on (pause) or off (go on). An ask already true changes nothing.
// A paused job nobody holds is queued again, and its worker resumes it.
func (d *Doors) Hold(id int, on bool) error {
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		paused := run.StoredPaused(*it)
		switch {
		case paused == on:
			return false, nil
		case held == nil && on:
			return false, fmt.Errorf("%s is not running", it.Ref())
		case held == nil:
			return false, run.ResumeItem(it, now)
		}
		it.Stream.Paused = on
		signal(doc, id, Signal{Verb: SignalHold, Yes: on})
		return true, nil
	})
}

// Answer answers the question the job waits on. A job that asked before it
// starts is launched by a yes; a held job hears the answer through its lease;
// an answer to a job no worker holds is kept for the step's next round.
func (d *Doors) Answer(id int, yes bool, words string) error {
	it, err := d.desk.store.Get(id)
	if err != nil {
		return err
	}
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindAskFirst {
		if yes {
			return d.Launch(id)
		}
		return d.desk.store.Update(id, func(it *factory.Item) error { return run.DeclineAskFirst(it, words) })
	}
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if held == nil {
			return false, run.KeepAnswer(it, yes, words, now)
		}
		if err := run.AnswerHeld(it, yes, now); err != nil {
			return false, err
		}
		signal(doc, id, Signal{Verb: SignalAnswer, Yes: yes, Words: words})
		return true, nil
	})
}

// Steer hands words to the round running now, through the lease.
func (d *Doors) Steer(id int, words string) error {
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, _ time.Time) (bool, error) {
		if held == nil {
			return false, fmt.Errorf("%s is not running", it.Ref())
		}
		signal(doc, id, Signal{Verb: SignalSteer, Words: words})
		return true, nil
	})
}

// SignOff is the person's approval at the proof gate, and APPROVING
// PUBLISHES. The approval must already be in the factory's record (the
// service writes it before it opens this door); its trailer rides to the
// worker as a publish signal: to the job's holder when a worker still holds
// it, else on the job queued again for the next pull. That worker commits,
// pushes and opens the pull request, and the job is done when it reports the
// pr. A job with no recorded approval publishes nothing.
func (d *Doors) SignOff(id int, edited bool) (bool, error) {
	trailer, err := d.trailer(id)
	if err != nil {
		return false, err
	}
	var repo string
	err = d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if run.TrailerCommit(trailer) != it.HeadCommit {
			return false, publish.ErrCommitChanged
		}
		if it.PublishStatus != nil && it.PublishStatus.State == "failed" {
			it.State = factory.StateQueued
			it.MarkPublishing("queued", "", "", now)
		} else if err := run.PublishItem(it, edited, now); err != nil {
			return false, err
		}
		if held != nil {
			// The holder is finishing its run; its done releases the lease
			// and keeps this signal, which queues the job again.
			it.State = factory.StateLanded
		}
		it.Publishing = true
		repo = it.Repo
		signal(doc, id, Signal{Verb: SignalPublish, Words: trailer})
		return true, nil
	})
	if err != nil || edited {
		return false, err
	}
	return run.HabitDue(d.desk.store, repo), nil
}

// trailer is the job's recorded approval as its trailer line, read from the
// record beside the desk's store: the one consent a publish may cite.
func (d *Doors) trailer(id int) (string, error) {
	history, err := record.Open(d.desk.store, record.Options{})
	if err != nil {
		return "", err
	}
	trailer, err := history.Trailer(context.Background(), id)
	if err != nil {
		return "", err
	}
	if !run.ValidTrailer(trailer) {
		return "", run.ErrNoApproval
	}
	return trailer, nil
}

// SendBack queues a landed job again with one more step, prove.
func (d *Doors) SendBack(id int, words string) error {
	return d.update(id, func(_ *document, _ *Lease, it *factory.Item, now time.Time) (bool, error) {
		return false, run.SendBackItem(it, words, now)
	})
}

// Reverify queues a landed job with a recheck signal: the worker that pulls
// it runs its checks again rather than its steps.
func (d *Doors) Reverify(id int) error {
	return d.update(id, func(doc *document, _ *Lease, it *factory.Item, _ time.Time) (bool, error) {
		if err := run.RecheckItem(it); err != nil {
			return false, err
		}
		signal(doc, id, Signal{Verb: SignalRecheck})
		return true, nil
	})
}

func stopHandle(caller *string, person string) string {
	if caller != nil {
		return *caller
	}
	return person
}
