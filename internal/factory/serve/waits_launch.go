package serve

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// waitsLaunches makes retirement durable before any service launch enters its
// runner. Resetting a run later cannot restore the old waits between scans.
func (h *Handler) waitsLaunches(seam factory.Seam) factory.Seam {
	if h.config.Store == nil {
		return seam
	}
	if launch := seam.Launch; launch != nil {
		seam.Launch = func(id int) error {
			resumed := false
			err := h.config.Store.Update(id, func(it *factory.Item) error {
				if (it.WaitsReleased || len(it.WaitsOn) == 0) && it.PolicyRun == "" && (it.Blocked == nil || it.Blocked.By != factory.BlockPolicy) {
					return errWaitsUnchanged
				}
				resumed = h.resumePolicyStart(it)
				retireLaunchWaits(it, "")
				return nil
			})
			if err != nil && !errors.Is(err, errWaitsUnchanged) {
				return err
			}
			if resumed {
				return nil
			}
			return launch(id)
		}
	}
	if forJob := seam.ForJob; forJob != nil {
		seam.ForJob = func(id int) factory.Seam { return h.waitsLaunches(forJob(id)) }
	}
	return seam
}

// retireLaunchWaits binds automatic admission to the same write as retirement.
// Every other launch clears the allowance that belonged to the earlier run.
func retireLaunchWaits(it *factory.Item, allowance string) {
	if len(it.WaitsOn) != 0 {
		it.WaitsReleased = true
	}
	it.PolicyRun, it.WaitingOn, it.Blocked = allowance, nil, nil
}
