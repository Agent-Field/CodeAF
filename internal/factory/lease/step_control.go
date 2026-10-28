package lease

import (
	"reflect"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
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
	return d.updateAt(id, revision, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if err := run.StepControl(it, name, rerun, words, now); err != nil {
			return false, err
		}
		delete(doc.Signals, id)
		if held != nil {
			release(doc, id)
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
// copy writes them back; a running job is queued again for that pull. It
// answers whether the desk changed.
func withdrawForSteps(doc *document, held *Lease, id int, it *factory.Item, changed bool) bool {
	if held == nil || !changed {
		return false
	}
	if it.State == factory.StateRunning && it.Stream != nil && !it.Stream.Paused {
		it.State = factory.StateQueued
	}
	release(doc, id)
	delete(doc.Signals, id)
	return true
}
