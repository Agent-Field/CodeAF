package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// Reads and the durable job timer both advance recovery; no open desktop is
// required. The desk rechecks each candidate under its own item and lease locks.
func (h *Handler) prepareStepRecoveries(items []factory.Item) ([]factory.Item, error) {
	if h.workers == nil || h.workers.desk == nil {
		return items, nil
	}
	doors := lease.NewDoors(h.workers.desk)
	updated := false
	for i, it := range items {
		if it.Recovery == nil && !factory.TransientStepFailure(it.Failure) {
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
