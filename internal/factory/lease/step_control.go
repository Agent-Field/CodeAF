package lease

import (
	"reflect"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// StepControl rewinds under the desk lock and revokes the old lease in the
// same operation. A late heartbeat or report cannot restore the previous run.
func (d *Doors) StepControl(id int, name string, rerun bool, words string) error {
	return d.StepControlExpected(id, name, rerun, words, nil)
}

// StepControlExpected compares a human decision inside the item lock; a read
// followed by a later write must not rewind a step that has already moved on.
func (d *Doors) StepControlExpected(id int, name string, rerun bool, words string, revision *int) error {
	return d.rewind(id, revision, rerun, func(it *factory.Item, now time.Time) error {
		if rerun {
			factory.ClearPersonStop(it)
		}
		return run.StepControl(it, name, rerun, words, now)
	})
}

// StepRerunExpected is [Doors.StepControlExpected]'s rerun that may also name
// the model the step runs on ([run.StepRerun]): run again from here, with
// another model.
func (d *Doors) StepRerunExpected(id int, name, words, model string, revision *int) error {
	return d.rewind(id, revision, true, func(it *factory.Item, now time.Time) error {
		factory.ClearPersonStop(it)
		return run.StepRerun(it, name, words, model, now)
	})
}

// rewind applies one step control under the item lock and revokes the old
// lease with it.
func (d *Doors) rewind(id int, revision *int, rerun bool, control func(it *factory.Item, now time.Time) error) error {
	return d.updateAt(id, revision, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if err := control(it, now); err != nil {
			return false, err
		}
		delete(doc.Signals, id)
		if held != nil {
			if rerun {
				withdraw(doc, *held)
			} else {
				release(doc, id)
			}
		}
		return true, nil
	})
}

// EditSteps applies the edit and withdraws the previous worker in one locked
// change. A resumed worker reads the edited tail instead of restoring stale steps.
func (d *Doors) EditSteps(id int, edit factory.RunEdit, recipe factory.Recipe) (factory.Item, []string, error) {
	var out factory.Item
	var lines []string
	err := d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if factory.JobState(*it) == "done" || it.Publishing {
			return false, run.StepControlRefusal("finished or publishing jobs cannot edit steps")
		}
		if err := factory.CheckPersonStop(*it); err != nil {
			return false, err
		}
		// A job that never ran is first given the steps its repository runs,
		// so the edit lands on them ([pipeline.StampRecipe]).
		pipeline.StampRecipe(it, recipe)
		var err error
		lines, err = run.EditStepsItem(it, edit, recipe)
		if err != nil {
			return false, err
		}
		withdrawn := withdrawForSteps(doc, held, id, it, len(lines) > 0)
		out = *it
		return withdrawn, nil
	})
	return out, lines, err
}

// ChangeSteps is a change to one job made by a door other than the step edit
// (the factory_item card's: its steps, budget, thinking and notes at once),
// held to what EditSteps holds the steps to. change edits the job in place.
// WHEN IT CHANGED THE STEPS, the phases not begun are laid again from them
// ([run.RelayTail]), so a step removed never runs and a step moved runs in
// its new place, and a worker running the job from its own copy is withdrawn
// as EditSteps withdraws it; a finished or publishing job's steps are refused
// and nothing is written. A change that leaves the steps alone writes only
// itself. It answers the job as saved.
func (d *Doors) ChangeSteps(id int, change func(it *factory.Item) error) (factory.Item, error) {
	var out factory.Item
	err := d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		before := factory.CopyStages(it.Stages)
		if err := change(it); err != nil {
			return false, err
		}
		changed := !sameStages(before, it.Stages)
		if changed {
			if factory.JobState(*it) == "done" || it.Publishing {
				return false, run.StepControlRefusal("finished or publishing jobs cannot edit steps")
			}
			run.RelayTail(it)
		}
		withdrawn := withdrawForSteps(doc, held, id, it, changed)
		out = *it
		return withdrawn, nil
	})
	return out, err
}

// sameStages compares two step lists, an empty proof list and a missing one
// alike.
func sameStages(a, b []factory.Stage) bool {
	return slices.EqualFunc(a, b, func(x, y factory.Stage) bool {
		px, py := x.Proof, y.Proof
		x.Proof, y.Proof = nil, nil
		return reflect.DeepEqual(x, y) && slices.Equal(px, py)
	})
}

// withdrawForSteps takes job id from the worker holding it when its steps
// changed, so the worker pulls the changed steps and no report from its old
// copy writes them back; a running job is queued again for that pull, at the
// step it was on. It answers whether the desk changed.
//
// THE LEASE IS WITHDRAWN, NEVER MERELY RELEASED. A worker whose lease is simply
// gone reads that as the factory stopping the job and runs a person's Stop,
// which ends the run and leaves the job new, in Backlog, to start again from
// its first step; the live audit's "add a security step" did exactly that to a
// running Write. A withdrawn lease is a handover: the worker cancels its drive
// without writing a stop, and the queued job is pulled again with its finished
// steps kept.
func withdrawForSteps(doc *document, held *Lease, id int, it *factory.Item, changed bool) bool {
	if held == nil || !changed {
		return false
	}
	if it.State == factory.StateRunning && it.Stream != nil && !it.Stream.Paused {
		it.State = factory.StateQueued
	}
	withdraw(doc, *held)
	delete(doc.Signals, id)
	return true
}

// Withdraw revokes automatic execution before manual control takes over. It
// leaves the recorded results intact and does not record a person's Stop.
func (d *Doors) Withdraw(id int) error {
	return d.update(id, func(doc *document, held *Lease, _ *factory.Item, _ time.Time) (bool, error) {
		if held == nil {
			return false, nil
		}
		withdraw(doc, *held)
		return true, nil
	})
}

// withdraw retains the old lease's meaning until its worker pulls again.
func withdraw(doc *document, held Lease) {
	if doc.Withdrawn == nil {
		doc.Withdrawn = map[string]string{}
	}
	doc.Withdrawn[held.Worker] = held.ID
	release(doc, held.Job)
}
