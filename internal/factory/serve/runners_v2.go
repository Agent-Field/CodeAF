package serve

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (h *Handler) v2Routes() {
	h.route("/v1/settings", map[string]http.HandlerFunc{http.MethodGet: h.runnerPolicy, http.MethodPatch: h.runnerPolicy})
	h.read("/v1/runners", h.runnersDirectory)
	h.route("/v1/runners/attach", map[string]http.HandlerFunc{http.MethodPost: h.attachWorker})
	h.read("/v1/runners/pull", h.pullWorker)
	h.route("/v1/runners/presence", map[string]http.HandlerFunc{http.MethodPost: h.runnerPresence})
	h.read("/v1/runner/self", h.runnerSelf)
	h.route("/v1/runners/{runner}", map[string]http.HandlerFunc{http.MethodPut: h.runnerSettings})
	h.route("/v1/runners/leases/{lease}/heartbeat", map[string]http.HandlerFunc{http.MethodPost: h.heartbeatWorker})
	h.route("/v1/runners/leases/{lease}/report", map[string]http.HandlerFunc{http.MethodPost: h.reportWorker})
	h.route("/v1/runners/leases/{lease}/done", map[string]http.HandlerFunc{http.MethodPost: h.doneWorker})
	h.route("/v1/runners/recover/{id}", map[string]http.HandlerFunc{http.MethodPost: h.recoverWorkerReport, http.MethodGet: h.workerRecoveryStatus})
	h.read("/v1/people", h.peopleDirectory)
	h.route("/v1/people/{handle}/sessions", map[string]http.HandlerFunc{http.MethodDelete: h.operatorRevokePerson})
	h.route("/v1/access/links", map[string]http.HandlerFunc{http.MethodGet: h.accessLinks, http.MethodPost: h.createAccessLink})
	h.route("/v1/access/links/{id}", map[string]http.HandlerFunc{http.MethodDelete: h.revokeAccessLink})
	h.read("/v1/activity", h.audit)
	h.route("/v1/repos/{owner}/{repo}", map[string]http.HandlerFunc{http.MethodPut: h.repoPower})
}

// runnerList is the CALLER's own machines and nobody else's. A teammate's
// machine, checkout, slot count or heartbeat is never serialized to anyone,
// the operator included (FS0/FS15 privacy), so the owner filter runs before a
// row is built rather than being trimmed from the result. Repository
// visibility still bounds the job ids and checkouts of the caller's own rows.
func (h *Handler) runnerList(r *http.Request, owner string) ([]api.Runner, error) {
	caller := h.machineViewer(r)
	if caller == "" || (owner != "" && factory.NormalizeHandle(owner) != caller) {
		return []api.Runner{}, nil
	}
	held, err := h.leases()
	if err != nil {
		return nil, err
	}
	h.workers.mu.RLock()
	rows := []registeredWorker{}
	live := map[string]bool{}
	for _, row := range h.workers.credentials {
		rows = append(rows, row)
		key := row.Worker.Handle + "\x00" + row.Worker.Machine
		if _, done := live[key]; !done {
			_, live[key] = h.workers.newestAttestationLocked(row.Worker.Handle, row.Worker.Machine)
		}
	}
	h.workers.mu.RUnlock()
	by := map[string]*api.Runner{}
	for _, row := range rows {
		if factory.NormalizeHandle(row.Worker.Handle) != caller {
			continue
		}
		key := row.Worker.Handle + "\x00" + row.Worker.Machine
		got := by[key]
		if got == nil || row.Last.After(got.LastSeen) {
			got = &api.Runner{ID: row.Worker.Machine, Name: row.Name, Owner: row.Worker.Handle, Kind: row.Kind, Slots: row.Slots, LiveCapacity: live[key], State: api.RunnerIdle, Checkouts: []api.Checkout{}, Jobs: []int{}, LastSeen: row.Last}
			if got.Kind == "" {
				got.Kind = api.RunnerCI
			}
			if got.Slots <= 0 {
				got.Slots = api.DefaultRunnerSlots
			}
			if row.Asleep {
				got.State = api.RunnerAsleep
			} else if !h.clock().Before(row.Last.Add(h.runnerOfflineAfter())) {
				got.State = api.RunnerOffline
			}
			for _, checkout := range row.Checkouts {
				if h.repoVisible(r, checkout.Repo) {
					got.Checkouts = append(got.Checkouts, checkout)
				}
			}
			by[key] = got
		}
	}
	for _, l := range held {
		got := by[l.Handle+"\x00"+l.Machine]
		if got == nil {
			continue
		}
		if h.config.Store != nil {
			it, err := h.config.Store.Get(l.Job)
			if err != nil {
				return nil, err
			}
			if !h.repoVisible(r, itemRepo(it)) {
				continue
			}
		}
		got.Jobs = append(got.Jobs, l.Job)
		if got.State == api.RunnerIdle {
			got.State = api.RunnerBusy
		}
	}
	out := []api.Runner{}
	for _, got := range by {
		sort.Ints(got.Jobs)
		out = append(out, *got)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner == out[j].Owner {
			return out[i].ID < out[j].ID
		}
		return out[i].Owner < out[j].Owner
	})
	return out, nil
}

func (h *Handler) runnersDirectory(w http.ResponseWriter, r *http.Request) {
	rows, err := h.runnerList(r, "")
	if h.failed(w, err) || h.authFailed(w, r) {
		return
	}
	owners := make([]string, 0, len(rows))
	for _, row := range rows {
		owners = append(owners, row.Owner)
	}
	h.write(w, http.StatusOK, api.Runners{Factory: h.factory, Runners: rows, People: h.signedInPeople(owners), SlotsPolicy: api.CurrentRunnerSlotsPolicy()})
}

func (h *Handler) runnerPresence(w http.ResponseWriter, r *http.Request) {
	var body api.RunnerPresence
	if !h.decode(w, r, &body, false) || !h.workerReady(w) {
		return
	}
	worker := requestWorker(r)
	if h.failed(w, h.workers.desk.Sleep(worker.Machine, worker.Handle, body.Asleep)) {
		return
	}
	h.workers.mu.Lock()
	for key, row := range h.workers.credentials {
		if row.Worker.Machine == worker.Machine && row.Worker.Handle == worker.Handle {
			row.Asleep = body.Asleep
			row.Last = h.workers.now()
			h.workers.credentials[key] = row
		}
	}
	err := h.workers.saveRegistry()
	h.workers.mu.Unlock()
	h.finish(w, err)
}

func (h *Handler) runnerSettings(w http.ResponseWriter, r *http.Request) {
	var body api.RunnerSettings
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Slots < 1 || body.Slots > api.MaxRunnerSlots {
		h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("slots must be from 1 to %d", api.MaxRunnerSlots))
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	// Every refusal below, up to clearPhase, carries api.PhaseHeader (phase.go
	// marks the route): nothing has been changed. The marker is cleared
	// immediately before the first change, so a failure of the save or of the
	// event after it reads as uncertain to a client.
	h.workers.mu.Lock()
	found, live := h.workers.newestAttestationLocked(handle, r.PathValue("runner"))
	if found && body.RequireLiveCapacity && !live {
		// Judged under the lock that writes: a worker that restarted onto an
		// older binary between the caller's read and now is seen here.
		h.workers.mu.Unlock()
		h.fail(w, http.StatusConflict, "that runner's worker cannot follow a capacity change; update it first")
		return
	}
	if found {
		clearPhase(w)
		for key, row := range h.workers.credentials {
			if row.Worker.Machine == r.PathValue("runner") && row.Worker.Handle == handle {
				row.Slots = body.Slots
				row.Worker.Slots = body.Slots
				// ABSENT CHECKOUTS ARE UNCHANGED: a capacity change must never
				// erase the repositories this runner has. Only a list that was
				// sent, even an empty one, replaces them.
				if body.Checkouts != nil {
					row.Checkouts = body.Checkouts
				}
				h.workers.credentials[key] = row
			}
		}
		err = h.workers.saveRegistry()
	}
	h.workers.mu.Unlock()
	if !found {
		h.fail(w, http.StatusNotFound, "runner not found")
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.RunnerChanged, Handle: handle, Object: "runner:" + r.PathValue("runner"), Detail: "capacity changed"})
	}
	h.finish(w, err)
}

// runnerSelf answers (its path is /v1/runner/self, singular, so no machine
// named "self" can ever collide with PUT /v1/runners/{runner}) a runner's own credential with the capacity its owner
// saved. The worker polls it to follow a raise or a lowering without a
// restart. Person tokens never reach it (workerPath), and the body names
// nothing but this machine's own number and how many jobs started now may
// run above it.
func (h *Handler) runnerSelf(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	self := requestWorker(r)
	over := 0
	if h.workers != nil && h.workers.desk != nil {
		n, err := h.workers.desk.OverLimit(self)
		if h.failed(w, err) {
			return
		}
		over = n
	}
	h.write(w, http.StatusOK, api.RunnerSelf{Slots: self.Slots, OverLimit: over})
}

// saveRegistry persists only credential hashes and public runner facts under the service lock.
func (s *workerService) saveRegistry() error {
	if s.st == nil {
		return nil
	}
	rows := map[string]registeredWorker{}
	for key, row := range s.credentials {
		rows[hex.EncodeToString(key[:])] = row
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.st.Root(), ".runners-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.st.Root(), "runners.json"))
}

func (s *workerService) loadRegistry() error {
	if s.st == nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.st.Root(), "runners.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	rows := map[string]registeredWorker{}
	if err = json.Unmarshal(data, &rows); err != nil {
		return err
	}
	for key, row := range rows {
		raw, err := hex.DecodeString(key)
		if err != nil || len(raw) != 32 {
			return errors.New("invalid runner credential hash")
		}
		var hash [32]byte
		copy(hash[:], raw)
		// A row saved before the ceiling existed may hold more than it. It is
		// granted the ceiling from here on, so the roster, /v1/runner/self and
		// the worker agree. Held leases are untouched: the desk only counts
		// them against the cap, so nothing running is cancelled.
		row.Slots = grantedRunnerSlots(row.Slots)
		row.Worker.Slots = row.Slots
		s.credentials[hash] = row
	}
	return nil
}

// newestAttestationLocked reads whether a machine is owned by handle and
// whether its NEWEST attach said it follows capacity changes while running.
// The newest is by Attached; rows that tie (all saved before the field existed,
// or issued in the same instant) must ALL attest, so an ambiguity can only
// withhold a write and never allow one. The caller holds workers.mu.
func (s *workerService) newestAttestationLocked(handle, machine string) (found, live bool) {
	var newest time.Time
	for _, row := range s.credentials {
		if row.Worker.Machine != machine || row.Worker.Handle != handle {
			continue
		}
		switch {
		case !found || row.Attached.After(newest):
			found, live, newest = true, row.LiveCapacity, row.Attached
		case row.Attached.Equal(newest):
			live = live && row.LiveCapacity
		}
	}
	return found, live
}

// The saved runner setting is the one cap. Old rows with no setting use the
// same default the roster showed, and a legacy execution copy cannot override
// that setting or turn a missing value into unlimited leases.
func grantedRunnerSlots(saved int) int {
	if saved <= 0 {
		return api.DefaultRunnerSlots
	}
	return min(saved, api.MaxRunnerSlots)
}
