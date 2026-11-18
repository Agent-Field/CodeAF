package lease

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// Task sends a delegate into the job's checkout with plain instructions, as
// an extra step (internal/factory's task.go): by is who asked, the job's main
// agent or its person, and now says the task runs at once instead of when
// the round running now ends. The rules are applied under the job's lock, so
// a refusal changes nothing, and it answers the step the task runs as.
//
// THE RUNNER LEARNS OF THE TASK THE WAY IT LEARNS OF A STEP EDIT OR AN ANSWER.
// A task that goes in front of the step the job is on lets the runner go, as
// a step edit does: the lease is withdrawn, never merely released, so the
// runner ends its drive without recording a stop and the queued job is
// pulled again with the task first and every finished step kept. A task that
// waits for the round running now rides the lease as a signal, as an answer
// does. A task for an open pull request waits on the job itself for the
// round running now to end.
func (d *Doors) Task(id int, by, words string, now bool) (factory.TaskAdded, error) {
	var added factory.TaskAdded
	err := d.update(id, func(doc *document, held *Lease, it *factory.Item, at time.Time) (bool, error) {
		// A lease that only lends the manager to a reply runs no step.
		running := held != nil && !held.ThreadOnly
		var err error
		if added, err = run.AddTask(it, by, words, running, now, at); err != nil {
			return false, err
		}
		if !running {
			return false, nil
		}
		switch added.Road {
		case factory.TaskRunsAfter:
			signal(doc, id, Signal{Verb: SignalTask, ID: added.Step, Words: words, By: added.By})
			return true, nil
		case factory.TaskRunsFollowNext, factory.TaskRunsLanded:
			// The runner is handing its round in; its done lands the job, and
			// the task is started from there.
			return false, nil
		}
		withdraw(doc, *held)
		delete(doc.Signals, id)
		return true, nil
	})
	return added, err
}

// StartHeldTask starts the Follow PR round of the tasks a job with an open
// pull request kept while a round was running, when no runner holds the job
// any more ([factory.StartHeldFollowTask]). It says whether a round was
// started, and writes nothing when none was.
func (d *Doors) StartHeldTask(id int) (bool, error) {
	err := d.update(id, func(_ *document, held *Lease, it *factory.Item, at time.Time) (bool, error) {
		if held != nil && !held.ThreadOnly {
			return false, errNoHeldTask
		}
		if !factory.StartHeldFollowTask(it, at) {
			return false, errNoHeldTask
		}
		return false, nil
	})
	if errors.Is(err, errNoHeldTask) {
		return false, nil
	}
	return err == nil, err
}

// errNoHeldTask leaves a job with no task to start as it is.
var errNoHeldTask = errors.New("no task waits for a Follow PR round")
