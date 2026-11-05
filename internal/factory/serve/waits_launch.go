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
			err := h.config.Store.Update(id, func(it *factory.Item) error {
				if it.WaitsReleased || len(it.WaitsOn) == 0 {
					return errWaitsUnchanged
				}
				it.WaitsReleased, it.WaitingOn, it.Blocked = true, nil, nil
				return nil
			})
			if err != nil && !errors.Is(err, errWaitsUnchanged) {
				return err
			}
			return launch(id)
		}
	}
	if forJob := seam.ForJob; forJob != nil {
		seam.ForJob = func(id int) factory.Seam { return h.waitsLaunches(forJob(id)) }
	}
	return seam
}
