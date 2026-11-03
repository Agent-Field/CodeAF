package run

import (
	"reflect"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func (lp *floorLoop) verificationProgress(c *loopCtl, index, round int) func(*factory.StepVerification) {
	return func(status *factory.StepVerification) {
		// Own the snapshot: the producer may reuse its buffer on the next tick.
		var saved *factory.StepVerification
		if status != nil {
			cp := *status
			cp.Checks = append([]factory.RunningCheck(nil), status.Checks...)
			saved = &cp
		}
		it, err := lp.r.opts.Store.Get(c.id)
		if err != nil || it.Stream == nil || index >= len(it.Stream.Phases) || reflect.DeepEqual(it.Stream.Phases[index].Verification, saved) {
			return
		}
		_ = lp.write(c, func(it *factory.Item) error {
			if it.Stream == nil || index >= len(it.Stream.Phases) {
				return nil
			}
			ph := &it.Stream.Phases[index]
			if ph.Round != round || ph.State != factory.PhaseRunning {
				return nil
			}
			ph.Verification = saved
			return nil
		})
	}
}
