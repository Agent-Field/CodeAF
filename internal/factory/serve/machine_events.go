package serve

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// machineEvents uses the same roster as /v1/me. Heartbeats reschedule the next
// offline deadline; a timer delivers silence as a transition without app polling.
func (h *Handler) machineEvents() {
	if h.eventRoot() == "" {
		return
	}
	h.machinesMu.Lock()
	defer h.machinesMu.Unlock()
	if h.machinesClosed {
		return
	}
	held, err := h.leases()
	if err != nil {
		return
	}
	now := h.clock()
	rows := h.roster(now, held)
	if h.machines == nil {
		h.machines = map[string]machineRow{}
	}
	var events []live.Event
	var deadline time.Time
	for _, row := range rows {
		key := row.Handle + "\x00" + row.Name
		old := h.machines[key]
		if old.Status != row.Status || old.Job != row.Job {
			e := live.New("machine", row.Job, api.LiveEvent{Name: row.Name, Status: row.Status})
			e.Handle = row.Handle
			// Keep the prior job for filtering the busy-to-idle or offline transition.
			if e.Job == 0 {
				e.Job = old.Job
			}
			events = append(events, e)
		}
		h.machines[key] = row
		if row.Status != api.MachineOffline {
			due := row.LastHeartbeat.Add(time.Duration(lease.OfflineMisses) * lease.HeartbeatEvery)
			if deadline.IsZero() || due.Before(deadline) {
				deadline = due
			}
		}
	}
	if err := live.Append(h.eventRoot(), events...); err != nil {
		return
	}
	if h.machinesTimer != nil {
		h.machinesTimer.Stop()
	}
	if !deadline.IsZero() {
		h.machinesTimer = time.AfterFunc(deadline.Sub(now), h.machineEvents)
	}
}
