package lease

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"time"
)

// RecoverAutomatic plans or advances one interrupted step under the same locks
// as manual recovery. A held worker is never replaced and a no-op writes nothing.
func (d *Doors) RecoverAutomatic(id int) (bool, error) {
	unchanged := errors.New("recovery unchanged")
	changed, restarted := false, false
	var detail string
	err := d.update(id, func(doc *document, held *Lease, it *factory.Item, now time.Time) (bool, error) {
		sw, err := d.desk.store.RepoSwitch()
		if err != nil {
			return false, err
		}
		if sw.Off(factory.RepoOf(*it)) {
			if it.Recovery == nil || (it.Recovery.State != "waiting" && it.Recovery.State != "queued") {
				return false, unchanged
			}
			cp := *it.Recovery
			cp.State, cp.Reason = "exhausted", "Repository is turned off. Turn it on before trying again."
			it.Recovery, changed = &cp, true
			if it.State == factory.StateQueued {
				it.State = factory.StateNeedsYou
				factory.StampFailure(it, factory.NewFailure(cp.Step, factory.CodeMachineLost, cp.Reason, it.WorktreeMachine, factory.FailureStopped, cp.Attempts, now))
			}
			return false, nil
		}
		changed = factory.PlanStepRecovery(it, now)
		rec := it.Recovery
		// Backoff belongs in Queued, rather than asking the person to approve
		// a transient wait. Do not change a held worker's reporting state.
		if rec != nil && rec.State == "waiting" && held == nil && it.State != factory.StateQueued {
			it.State, changed = factory.StateQueued, true
		}
		if rec != nil && rec.State == "waiting" && !now.Before(rec.NextAt) {
			cp := *rec
			if why := factory.RecoveryBlockReason(*it); why != "" {
				cp.State, cp.Reason = "exhausted", why
			} else if cp.Attempts >= cp.Limit || (held != nil && !now.Before(cp.Deadline)) {
				// Out of attempts; or a worker that still holds the job never
				// let a retry start within the runner-return window.
				cp.State, cp.Reason = "exhausted", "Automatic recovery reached its limit. Try again when ready."
			} else if held != nil {
				// Keep the wait visible and bounded, but do not supersede a
				// worker whose hold still belongs to this job.
				if !changed {
					return false, unchanged
				}
				return false, nil
			} else {
				// A new attempt gets a fresh stage conversation, while earlier results,
				// the worktree, PR and total spend are retained. Old lease reports cannot
				// revive the interrupted round because no old lease remains.
				if err := run.StepControl(it, cp.Step, true, "", now); err != nil {
					return false, err
				}
				// The wait for the runner to take this retry starts now.
				cp.State, cp.NextAt, cp.Deadline = "queued", time.Time{}, now.Add(factory.RunnerReturnWindow)
				cp.Attempts++
				cp.Reason = fmt.Sprintf("Automatic retry %d of %d queued; waiting for the runner.", cp.Attempts, cp.Limit)
				restarted, detail = true, cp.Reason
			}
			it.Recovery, changed = &cp, true
			if cp.State == "exhausted" && it.State == factory.StateQueued {
				it.State = factory.StateNeedsYou
			}
		}
		if !changed {
			return false, unchanged
		}
		return false, nil
	})
	if errors.Is(err, unchanged) {
		return false, nil
	}
	if err == nil && restarted && d.desk.record != nil {
		_, _ = d.desk.record.Append(context.Background(), record.Event{Kind: record.StepRerun, Job: id, Detail: detail})
	}
	return changed, err
}
