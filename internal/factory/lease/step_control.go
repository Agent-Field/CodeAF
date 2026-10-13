package lease

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// StepControl rewinds under the desk lock and revokes the old lease in the
// same operation. A late heartbeat or report cannot restore the previous run.
func (d *Doors) StepControl(id int, name string, rerun bool) error {
	return d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		if err := run.StepControl(it, name, rerun, now); err != nil {
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
		if held != nil && len(lines) > 0 {
			if it.State == factory.StateRunning && it.Stream != nil && !it.Stream.Paused {
				it.State = factory.StateQueued
			}
			release(doc, id)
			delete(doc.Signals, id)
		}
		out = *it
		return held != nil && len(lines) > 0, nil
	})
	return out, lines, err
}
