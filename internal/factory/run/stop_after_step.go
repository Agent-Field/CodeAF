package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/client"
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
		for {
			on, err := readStopChoice(c.ctx, read.(func(context.Context) (bool, error)))
			if errors.Is(err, client.ErrLeaseGone) {
				c.cancel()
				return false
			}
			if err != nil {
				why := "could not confirm Stop before the next step; work held: " + loopFirstLine(err.Error())
				if err := lp.write(c, func(cur *factory.Item) error {
					// This is a boundary question, not a paid step's question.
					cur.Asking = &factory.Asked{Question: why, With: factory.AskedYou}
					return nil
				}); err != nil {
					return false
				}
				answer, ok := lp.ask(c, -1, qkindStep, qkindStep, why, factory.PhaseWaiting, "", "")
				if !ok {
					return false
				}
				if !answer.yes {
					lp.stop(c)
					return false
				}
				if err := lp.write(c, func(cur *factory.Item) error { cur.Asking = nil; return nil }); err != nil {
					return false
				}
				continue
			}
			if on != it.StopAfterStep {
				if err := lp.write(c, func(cur *factory.Item) error { cur.StopAfterStep = on; return nil }); err != nil {
					return false
				}
				it.StopAfterStep = on
			}
			break
		}
	}
	if !it.StopAfterStep {
		return true
	}
	next := nextPhase(it.Stream.Phases)
	// A recovered or retried round still belongs to the current step. Finish
	// it before honoring the hold, even when earlier steps are already done.
	if next >= 0 && it.Stream.Phases[next].Round > 0 {
		return true
	}
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
		if err := lp.write(c, func(cur *factory.Item) error { stopSpendingItem(cur, lp.r.now()); return nil }); err != nil {
			return false
		}
		lp.stop(c)
		return false
	}
	return lp.write(c, func(cur *factory.Item) error { cur.StopAfterStep = false; return nil }) == nil
}

// A boundary stop has no interrupted phase to supply the person's stop cause.
func stopSpendingItem(it *factory.Item, now time.Time) {
	factory.MarkPersonStopped(it)
	it.StopAfterStep = false
	endRun(it, now, stoppedByYou("", 0, now))
}

func (r *Runner) answerStoppedSpending(id int, yes bool, words string) error {
	err := r.opts.Store.Update(id, func(it *factory.Item) error {
		if it.QKind != factory.QKindSpendStopped || it.Stream == nil {
			return fmt.Errorf("%s is not waiting on a spending choice", it.Ref())
		}
		if err := AnswerUnheld(it, yes, words, r.now()); err != nil {
			return err
		}
		if yes {
			it.Stream.Paused = true
		}
		return nil
	})
	if err != nil || !yes {
		return err
	}
	return r.Hold(id, false)
}

// Read failures do not admit paid work; keep the retry window inside the lease.
func readStopChoice(ctx context.Context, read func(context.Context) (bool, error)) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var on bool
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		on, err = read(ctx)
		if err == nil || errors.Is(err, client.ErrLeaseGone) || ctx.Err() != nil {
			break
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return on, err
}
