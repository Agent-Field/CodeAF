package serve

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
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

// computersDown is the computer a queued job with no pin waits for, when every
// computer that could take it is asleep or offline: nothing else can start it,
// so no slot is what it waits for. An empty owner counts every computer. A
// computer whose registration is quiet past offline is offline; a zero offline
// judges only asleep, for a reader of the saved registry, where the last-heard
// time is not kept current. Any computer up, or none known, claims nothing.
func computersDown(rows []registeredWorker, owner string, offline time.Duration, now time.Time) (machineRow, bool) {
	by := map[string]*machineRow{}
	for _, row := range rows {
		if owner != "" && factory.NormalizeHandle(row.Worker.Handle) != factory.NormalizeHandle(owner) {
			continue
		}
		name := strings.TrimSpace(row.Worker.Machine)
		if name == "" {
			name = row.Name
		}
		got := by[row.Worker.Handle+"\x00"+name]
		if got == nil {
			got = &machineRow{Handle: row.Worker.Handle, Machine: api.Machine{Name: name, Status: api.MachineOffline}}
			by[row.Worker.Handle+"\x00"+name] = got
		}
		if row.Last.After(got.LastHeartbeat) {
			got.LastHeartbeat = row.Last
		}
		if row.Asleep {
			got.Status = api.RunnerAsleep
		}
	}
	var down machineRow
	for _, got := range by {
		quiet := offline > 0 && !now.Before(got.LastHeartbeat.Add(offline))
		if got.Status != api.RunnerAsleep && !quiet {
			return machineRow{}, false
		}
		if down.Name == "" || got.LastHeartbeat.After(down.LastHeartbeat) {
			down = *got
		}
	}
	return down, down.Name != ""
}

// presentComputersDown says what a queued job with no pin waits for when every
// computer that could take it is down. A team's unassigned job can go to anyone,
// so it names no one's computer.
func (h *Handler) presentComputersDown(job *api.Job, rows []registeredWorker, now time.Time) {
	if job.State != api.StateQueued || job.Held || job.Runner != "" || (job.AssignedTo == "" && h.mode() != api.FactorySolo) {
		return
	}
	row, ok := computersDown(rows, job.AssignedTo, h.runnerOfflineAfter(), now)
	if !ok {
		return
	}
	job.StatePresentation = api.StatePresentation{
		Kind:   api.WaitingForComputer,
		Title:  "Waiting for " + row.Name,
		Detail: fmt.Sprintf("Waiting for %s (%s). The job starts once it is back.", row.Name, downFor(row, now)),
	}
}

// QueuedWait is the same reason for the job's manager, which reads the floor and
// not this service: what it says a queued job waits for, or "" when it cannot
// say. It names no computer, because a manager's words reach whoever reads the job.
func QueuedWait(st *store.Store, it factory.Item, now time.Time) string {
	if it.State != factory.StateQueued || st == nil {
		return ""
	}
	saved, err := readRegistry(st.Root())
	if err != nil {
		return ""
	}
	rows := make([]registeredWorker, 0, len(saved))
	for _, row := range saved {
		if it.WorktreeMachine == "" || row.Worker.Machine == it.WorktreeMachine {
			rows = append(rows, row)
		}
	}
	if _, ok := computersDown(rows, it.Person(), 0, now); !ok {
		return ""
	}
	return "It is waiting for its computer, which is asleep, and starts once that computer is back. Nothing else is holding it."
}
