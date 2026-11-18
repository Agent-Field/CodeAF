package serve

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// prepareAutomaticRepairs sends every landed Auto job whose proof or policy
// checks failed back through its own repair loop (factory's auto_repair.go),
// before the automatic approvals are looked at: a finding is Auto's to fix
// first, not a pause for the person. A job whose repairs are used up, or
// whose repository is switched off, keeps today's pause on the finding.
//
// The attempt is counted under the job's lock before the send-back, so a
// send-back that fails is still an attempt and the repairs stay bounded.
//
// ONE SETTLE AT A TIME. settleJobs runs on every request; the attempt is
// counted before the send-back moves the job off landed, so a second settle
// in between would read it still due and count a second attempt for the same
// send-back. A settle that comes while another repairs waits for it, then
// reads the job again under its lock and finds it queued, not due. (Nothing a
// send-back calls settles again: settles come from requests, the job timer and
// the manager-when judge's goroutine.)
func (h *Handler) prepareAutomaticRepairs(items []factory.Item) ([]factory.Item, error) {
	h.repairing.Lock()
	defer h.repairing.Unlock()
	for i, candidate := range items {
		if _, ok := factory.AutoRepairDue(candidate); !ok || !h.repoOn(candidate) {
			continue
		}
		var r factory.AutoRepair
		err := h.config.Store.Update(candidate.ID, func(cur *factory.Item) error {
			due, ok := factory.AutoRepairDue(*cur)
			if !ok {
				return errNoRepair
			}
			// A SEND-BACK NEEDS THE RUNNER'S DOOR: without one the job keeps
			// its pause rather than counting a repair nobody can run.
			if !due.OnPR && h.seam.SendBack == nil {
				return errNoRepair
			}
			var e error
			r, e = factory.BeginAutoRepair(cur, h.clock())
			return e
		})
		if err != nil {
			continue
		}
		if !r.OnPR {
			// The runner's own door puts the prove step after the last
			// finished one and queues the job, as Request changes does. A
			// send-back that fails is still the attempt counted above, and it
			// is said on the job, never dropped.
			if err := h.seam.SendBack(candidate.ID, r.Words()); err != nil {
				now := h.clock().UTC()
				_ = h.config.Store.Update(candidate.ID, func(cur *factory.Item) error {
					logAct(cur, now, "fail", autoRepairFailed+err.Error())
					return nil
				})
			}
		}
		if fresh, e := h.config.Store.Get(candidate.ID); e == nil {
			items[i] = fresh
		}
	}
	return items, nil
}

// autoRepairFailed leads the line a failed Auto repair send-back leaves on the
// job's log and activity; the door's error follows it.
const autoRepairFailed = "Auto could not send the work back for its repair: "

// errNoRepair leaves a job its read said was due as it is: it moved, or no
// door can run the repair.
var errNoRepair = errors.New("no Auto repair is due")

// repoOn says the job's repository is not switched off.
func (h *Handler) repoOn(it factory.Item) bool {
	sw, err := h.config.Store.RepoSwitch()
	return err == nil && !sw.Off(factory.RepoOf(it))
}
