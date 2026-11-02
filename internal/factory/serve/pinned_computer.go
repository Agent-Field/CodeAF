package serve

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// pinnedDown is the computer a queued job's work is on, when that computer is
// known and not running: offline or asleep. A job with no pin, a computer the
// roster does not know, or one that is live claims nothing (ok false), so the
// generic waiting words stay. owner narrows the match to that person's
// computer when it is known; two people may name their laptops alike.
func pinnedDown(machine, owner string, roster []machineRow) (row machineRow, ok bool) {
	if machine == "" {
		return machineRow{}, false
	}
	for _, r := range roster {
		if r.Name != machine || (owner != "" && factory.NormalizeHandle(r.Handle) != factory.NormalizeHandle(owner)) {
			continue
		}
		if r.Status != api.MachineOffline && r.Status != api.RunnerAsleep {
			return machineRow{}, false
		}
		if !ok || r.LastHeartbeat.After(row.LastHeartbeat) {
			row, ok = r, true
		}
	}
	return row, ok
}

// downFor says how long a computer has been down, in the plainest unit.
func downFor(row machineRow, now time.Time) string {
	word := "offline"
	if row.Status == api.RunnerAsleep {
		word = "asleep"
	}
	if row.LastHeartbeat.IsZero() {
		return word
	}
	d := now.Sub(row.LastHeartbeat)
	switch {
	case d < time.Minute:
		return word + " since just now"
	case d < time.Hour:
		return fmt.Sprintf("%s for %d min", word, int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%s for %d h", word, int(d/time.Hour))
	default:
		return fmt.Sprintf("%s for %d days", word, int(d/(24*time.Hour)))
	}
}

// presentPinnedComputer names the computer a queued job waits for when that
// computer is down, instead of "waiting for an available runner": no slot or
// Jobs at once setting can start a job whose work is on a computer that is
// off. Presence decorates only this read; [Handler.redactMachines] withholds
// the name from anyone but that computer's person.
func presentPinnedComputer(job *api.Job, roster []machineRow, now time.Time) {
	if job.State != api.StateQueued || job.Held {
		return
	}
	row, ok := pinnedDown(job.Runner, job.AssignedTo, roster)
	if !ok {
		return
	}
	job.StatePresentation = api.StatePresentation{
		Kind:   api.WaitingForComputer,
		Title:  "Waiting for " + row.Name,
		Detail: fmt.Sprintf("Waiting for %s (%s). Its work is on that computer, so the job starts there once it is back.", row.Name, downFor(row, now)),
	}
}

// pinnedStartRefused refuses Start now on a queued job whose computer is down:
// a slot above Jobs at once cannot start a job whose work is on a computer
// that is off, so the press says why instead of doing nothing.
func (h *Handler) pinnedStartRefused(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if it.State != factory.StateQueued || it.WorktreeMachine == "" {
		return false
	}
	leases, err := h.leases()
	if err != nil {
		return false
	}
	now := h.clock()
	row, ok := pinnedDown(it.WorktreeMachine, it.Assignee, h.roster(now, leases))
	if !ok {
		return false
	}
	name := row.Name
	if h.mode() != api.FactorySolo && !h.callersOnlyMachine(h.machineViewer(r), name) {
		name = "its computer"
	}
	h.refuse(w, 409, fmt.Sprintf("This job can't start now: its work is on %s, which is %s. It starts there once that computer is back.", name, downFor(row, now)), "conflict", "", nil, factory.JobState(it))
	return true
}
