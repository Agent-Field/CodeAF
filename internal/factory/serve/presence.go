package serve

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// machineRow is one machine after its slots are folded together. Handle
// stays off the wire: /v1/me already names the caller, and another person's
// handle is not a roster entry.
type machineRow struct {
	Handle string
	api.Machine
}

// leases is the live holds, or none when this handler has no claims desk.
// A read reaps expired holds the same way a pull does.
func (h *Handler) leases() ([]lease.Lease, error) {
	if h.workers == nil || h.workers.desk == nil {
		return nil, nil
	}
	return h.workers.desk.Live()
}

func (h *Handler) clock() time.Time {
	if h.config.Now != nil {
		return h.config.Now()
	}
	if h.workers != nil && h.workers.now != nil {
		return h.workers.now()
	}
	return time.Now()
}

// beat is when the hold began and when it was last renewed. A lease written
// before those fields existed uses expiry minus the TTL, which is where the
// renewal clock already lived.
func (h *Handler) beat(held lease.Lease) (at, since time.Time) {
	at = held.HeartbeatAt
	if at.IsZero() {
		ttl := lease.DefaultTTL
		if h.workers != nil && h.workers.desk != nil {
			ttl = h.workers.desk.TTL()
		}
		at = held.ExpiresAt.Add(-ttl)
	}
	since = held.Since
	if since.IsZero() {
		since = at
	}
	return at, since
}

// roster folds attached slots into one row per handle and machine. Status
// is offline when the last heartbeat is sixty seconds old or older; a
// live lease does not keep a quiet machine busy.
func (h *Handler) roster(now time.Time, leases []lease.Lease) []machineRow {
	if h.workers == nil {
		return nil
	}
	h.workers.mu.RLock()
	rows := make([]registeredWorker, 0, len(h.workers.credentials))
	for _, row := range h.workers.credentials {
		rows = append(rows, row)
	}
	h.workers.mu.RUnlock()

	type acc struct {
		handle, name, os, version string
		last                      time.Time
		job                       int
		jobAt                     time.Time
		asleep                    bool
	}
	by := map[string]*acc{}
	var keys []string
	machineOf := func(row registeredWorker) string {
		name := strings.TrimSpace(row.Worker.Machine)
		if name == "" {
			name = row.Name
		}
		return name
	}
	for _, row := range rows {
		name := machineOf(row)
		key := row.Worker.Handle + "\x00" + name
		got := by[key]
		if got == nil {
			got = &acc{handle: row.Worker.Handle, name: name, os: row.OS, version: row.Version, last: row.Last, asleep: row.Asleep}
			by[key] = got
			keys = append(keys, key)
		}
		if row.Last.After(got.last) {
			got.last = row.Last
			got.asleep = row.Asleep
			if row.OS != "" {
				got.os = row.OS
			}
			if row.Version != "" {
				got.version = row.Version
			}
		}
	}
	heldByWorker := map[string]lease.Lease{}
	for _, held := range leases {
		heldByWorker[held.Worker] = held
	}
	for _, row := range rows {
		held, ok := heldByWorker[row.Worker.ID]
		if !ok {
			continue
		}
		got := by[row.Worker.Handle+"\x00"+machineOf(row)]
		at, _ := h.beat(held)
		if got.job == 0 || at.After(got.jobAt) || (at.Equal(got.jobAt) && (got.job == 0 || held.Job < got.job)) {
			got.job = held.Job
			got.jobAt = at
		}
	}
	limit := lease.OfflineAfter
	out := make([]machineRow, 0, len(keys))
	for _, key := range keys {
		got := by[key]
		status := api.MachineIdle
		if got.asleep {
			status = api.RunnerAsleep
		} else if got.last.IsZero() || !now.Before(got.last.Add(limit)) {
			status = api.MachineOffline
		} else if got.job != 0 {
			status = api.MachineBusy
		}
		out = append(out, machineRow{
			Handle: got.handle,
			Machine: api.Machine{
				Name: got.name, OS: got.os, CodeafVersion: got.version,
				Status: status, Job: got.job, LastHeartbeat: got.last,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Handle < out[j].Handle
	})
	return out
}

// viewerMachines is the caller's own roster. A different handle's machines
// are left out; those appear only as held_by on a job this caller can see.
func (h *Handler) viewerMachines(handle string) ([]api.Machine, error) {
	leases, err := h.leases()
	if err != nil {
		return nil, err
	}
	handle = factory.NormalizeHandle(handle)
	out := []api.Machine{}
	roster := h.roster(h.clock(), leases)
	for _, row := range roster {
		if row.Handle != handle {
			continue
		}
		out = append(out, row.Machine)
	}
	return out, nil
}

// applyPresence stamps live holds onto jobs and counts every machine, not
// only the caller's. The counts are the floor's; the names of other people
// stay on the job they are already allowed to see.
func (h *Handler) applyPresence(jobs []api.Job) (online, busy int, err error) {
	leases, err := h.leases()
	if err != nil {
		return 0, 0, err
	}
	roster := h.roster(h.clock(), leases)
	for _, row := range roster {
		switch row.Status {
		case api.MachineBusy:
			busy++
			online++
		case api.MachineIdle:
			online++
		}
	}
	byJob := map[int]lease.Lease{}
	for _, held := range leases {
		byJob[held.Job] = held
	}
	names := map[string]string{}
	if h.workers != nil {
		h.workers.mu.RLock()
		for _, row := range h.workers.credentials {
			name := strings.TrimSpace(row.Worker.Machine)
			if name == "" {
				name = row.Name
			}
			names[row.Worker.ID] = name
		}
		h.workers.mu.RUnlock()
	}
	for i := range jobs {
		if p := jobs[i].Publishing; p != nil && p.State == "queued" && jobs[i].Desk != nil && jobs[i].Desk.WorktreeMachine != "" {
			online := false
			for _, row := range roster {
				if row.Name == jobs[i].Desk.WorktreeMachine && row.Status != api.MachineOffline {
					online = true
				}
			}
			copy := *p
			copy.Code, copy.Detail = "", ""
			if !online {
				copy.Code, copy.Detail = "machine_offline", "Publishing waits for "+jobs[i].Desk.WorktreeMachine
			}
			jobs[i].Publishing = &copy
		}
		held, ok := byJob[jobs[i].ID]
		if !ok {
			continue
		}
		at, since := h.beat(held)
		machine := held.Machine
		if machine == "" {
			machine = names[held.Worker]
		}
		jobs[i].Runner = machine
		jobs[i].HeldBy = &api.HeldBy{Machine: machine, Handle: held.Handle, HeartbeatAt: at, Since: since}
	}
	return online, busy, nil
}

// savedTakes is the caller's persisted toggle. No store and no saved map
// are both an empty object: the field has always been present.
func (h *Handler) savedTakes(handle string) (map[string]bool, error) {
	if h.config.Store == nil {
		return map[string]bool{}, nil
	}
	saved, set, err := h.config.Store.Takes(handle)
	if err != nil || !set || saved == nil {
		if err != nil {
			return nil, err
		}
		return map[string]bool{}, nil
	}
	return saved, nil
}

// consent applies a saved toggle over the consent the worker sent at attach.
// Until this handle has been saved, the attach map stands, so a loopback
// worker still takes every unassigned job.
func (h *Handler) consent(worker lease.Worker) (lease.Worker, error) {
	worker.Team = h.mode() == api.FactoryTeam
	worker.Solo = h.mode() == api.FactorySolo
	if worker.Team {
		worker.TakesUnassigned = nil
		return worker, nil
	}
	if h.config.Store == nil {
		return worker, nil
	}
	saved, set, err := h.config.Store.Takes(worker.Handle)
	if err != nil || !set {
		return worker, err
	}
	worker.TakesUnassigned = saved
	return worker, nil
}

func (h *Handler) setTakes(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	var body map[string]bool
	if !h.decode(w, r, &body, false) {
		return
	}
	// The saved map belongs to the same handle GET /v1/me names: the signed-in
	// caller when there is one, otherwise the factory's connected login.
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	for repo := range body {
		if strings.TrimSpace(repo) == "" {
			h.fail(w, http.StatusBadRequest, "takes_unassigned repo must not be empty")
			return
		}
	}
	if h.failed(w, h.config.Store.SetTakes(handle, body)) {
		return
	}
	h.finish(w, nil)
}
