package serve

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

type machineCapacity struct {
	owner, machine string
	slots, used    int
	attached       time.Time
}

// Read once alongside presence. Registrations are per execution slot, while
// capacity belongs to a computer: never add a computer's limit once per credential.
func (h *Handler) executionCapacity(held []lease.Lease) []machineCapacity {
	if h.workers == nil {
		return nil
	}
	now := h.clock()
	offlineAfter := h.runnerOfflineAfter()
	by := map[string]machineCapacity{}
	h.workers.mu.RLock()
	for _, row := range h.workers.credentials {
		if row.Asleep || row.Last.IsZero() || !now.Before(row.Last.Add(offlineAfter)) {
			continue
		}
		key := row.Worker.Handle + "\x00" + row.Worker.Machine
		current, found := by[key]
		if !found || row.Attached.After(current.attached) {
			by[key] = machineCapacity{owner: row.Worker.Handle, machine: row.Worker.Machine, slots: grantedRunnerSlots(row.Slots), attached: row.Attached}
		}
	}
	h.workers.mu.RUnlock()
	for _, hold := range held {
		key := hold.Handle + "\x00" + hold.Machine
		if current, found := by[key]; found {
			current.used++
			by[key] = current
		}
	}
	out := make([]machineCapacity, 0, len(by))
	for _, current := range by {
		out = append(out, current)
	}
	return out
}

// Only the job owner's live computers can establish that capacity is full.
// Unknown ownership, offline machines and an available slot never claim a limit
// is the cause. Existing worktree affinity must keep the job on its computer.
func presentQueuedCapacity(job *api.Job, machines []machineCapacity) {
	if job.State != api.StateQueued || job.AssignedTo == "" {
		return
	}
	used, slots := 0, 0
	for _, machine := range machines {
		if machine.owner != job.AssignedTo || (job.Runner != "" && job.Runner != machine.machine) {
			continue
		}
		if machine.used < machine.slots {
			return
		}
		used += machine.used
		slots += machine.slots
	}
	if slots > 0 {
		job.StatePresentation = api.StatePresentation{Kind: "waiting_for_capacity", Title: "Waiting for capacity", Detail: fmt.Sprintf("Waiting for capacity · %d/%d execution slots in use. %s", used, slots, api.ConcurrencySettingsHint)}
	}
}
