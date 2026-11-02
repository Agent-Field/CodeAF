package run

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// SetStopAfterStepReader binds a leased job's authoritative spending choice.
// The runner reads it before each step and landing, without cutting a round.
// Nil removes the binding when the worker releases the job.
func (r *Runner) SetStopAfterStepReader(id int, read func(context.Context) (bool, error)) {
	if read == nil {
		r.stopReaders.Delete(id)
		return
	}
	r.stopReaders.Store(id, read)
}

func (lp *floorLoop) stopSpending(c *loopCtl, it factory.Item) bool {
	if read, ok := lp.r.stopReaders.Load(c.id); ok {
		on, err := read.(func(context.Context) (bool, error))(c.ctx)
		if err != nil {
			lp.failStop(c, "", fmt.Errorf("could not read spending stop: %w", err))
			return false
		}
		if on != it.StopAfterStep {
			if err := lp.write(c, func(cur *factory.Item) error { cur.StopAfterStep = on; return nil }); err != nil {
				return false
			}
			it.StopAfterStep = on
		}
	}
	if !it.StopAfterStep {
		return true
	}
	next := nextPhase(it.Stream.Phases)
	prev := next - 1
	if next < 0 {
		prev = len(it.Stream.Phases) - 1
	}
	if prev < 0 {
		return true
	}
	name := it.Stream.Phases[prev].Name
	q := fmt.Sprintf("Stopped spending after %s as you asked · $%.2f so far. Carry on?", name, factory.JobSpent(it))
	a, ok := lp.ask(c, -1, factory.QKindSpendStopped, factory.QKindSpendStopped, q, factory.PhaseWaiting, "", "")
	if !ok {
		return false
	}
	if !a.yes {
		lp.stop(c)
		return false
	}
	return lp.write(c, func(cur *factory.Item) error { cur.StopAfterStep = false; return nil }) == nil
}
