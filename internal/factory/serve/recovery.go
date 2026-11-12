package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// Reads and the durable job timer both advance recovery; no open desktop is
// required. The desk rechecks each candidate under its own item and lease locks.
func (h *Handler) prepareStepRecoveries(items []factory.Item, leases []lease.Lease) ([]factory.Item, error) {
	if h.workers == nil || h.workers.desk == nil {
		return items, nil
	}
	desk := h.workers.desk
	doors := lease.NewDoors(desk)
	held := make(map[int]bool, len(leases))
	for _, row := range leases {
		held[row.Job] = true
	}
	updated := false
	for i, it := range items {
		if factory.RecoveryPaused(it) {
			continue
		}
		mayBeLost := !held[it.ID] && desk.MayHaveLostRunner(it)
		if it.Recovery == nil && !factory.TransientStepFailure(it.Failure) && !mayBeLost {
			continue
		}
		changed, err := doors.RecoverAutomatic(it.ID)
		if err != nil {
			return items, err
		}
		if changed {
			cur, err := h.config.Store.Get(it.ID)
			if err != nil {
				return items, err
			}
			items[i], updated = cur, true
		}
	}
	if updated {
		h.scheduleJobTimer()
	}
	return items, nil
}
