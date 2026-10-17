package serve

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (h *Handler) v2Routes() {
	h.route("/v1/settings", map[string]http.HandlerFunc{http.MethodGet: h.runnerPolicy, http.MethodPatch: h.runnerPolicy})
	h.read("/v1/runners", h.runnersDirectory)
	h.route("/v1/runners/attach", map[string]http.HandlerFunc{http.MethodPost: h.attachWorker})
	h.read("/v1/runners/pull", h.pullWorker)
	h.route("/v1/runners/presence", map[string]http.HandlerFunc{http.MethodPost: h.runnerPresence})
	h.route("/v1/runners/{runner}", map[string]http.HandlerFunc{http.MethodPut: h.runnerSettings})
	h.route("/v1/runners/leases/{lease}/heartbeat", map[string]http.HandlerFunc{http.MethodPost: h.heartbeatWorker})
	h.route("/v1/runners/leases/{lease}/report", map[string]http.HandlerFunc{http.MethodPost: h.reportWorker})
	h.route("/v1/runners/leases/{lease}/done", map[string]http.HandlerFunc{http.MethodPost: h.doneWorker})
	h.read("/v1/people", h.peopleDirectory)
	h.route("/v1/people/{handle}/sessions", map[string]http.HandlerFunc{http.MethodDelete: h.operatorRevokePerson})
	h.route("/v1/access/links", map[string]http.HandlerFunc{http.MethodGet: h.accessLinks, http.MethodPost: h.createAccessLink})
	h.route("/v1/access/links/{id}", map[string]http.HandlerFunc{http.MethodDelete: h.revokeAccessLink})
	h.read("/v1/activity", h.audit)
	h.route("/v1/repos/{owner}/{repo}", map[string]http.HandlerFunc{http.MethodPut: h.repoPower})
}

// runnerList applies repository visibility before exposing another person's job ids or checkouts.
func (h *Handler) runnerList(r *http.Request, owner string) ([]api.Runner, error) {
	held, err := h.leases()
	if err != nil {
		return nil, err
	}
	h.workers.mu.RLock()
	rows := []registeredWorker{}
	for _, row := range h.workers.credentials {
		rows = append(rows, row)
	}
	h.workers.mu.RUnlock()
	by := map[string]*api.Runner{}
	for _, row := range rows {
		if owner != "" && row.Worker.Handle != owner {
			continue
		}
		key := row.Worker.Handle + "\x00" + row.Worker.Machine
		got := by[key]
		if got == nil || row.Last.After(got.LastSeen) {
			got = &api.Runner{ID: row.Worker.Machine, Name: row.Name, Owner: row.Worker.Handle, Kind: row.Kind, Slots: row.Slots, State: api.RunnerIdle, Checkouts: []api.Checkout{}, Jobs: []int{}, LastSeen: row.Last}
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
	caller, _ := h.callerHandle(r)
	_, partial := h.repoAccess(r, nil)
	out := []api.Runner{}
	for _, got := range by {
		if partial && got.Owner != caller && len(got.Jobs) == 0 {
			continue
		}
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
	h.write(w, http.StatusOK, api.Runners{Factory: h.factory, Runners: rows, People: h.signedInPeople(owners)})
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
	if body.Slots < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "slots must be positive")
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	h.workers.mu.Lock()
	found := false
	for key, row := range h.workers.credentials {
		if row.Worker.Machine == r.PathValue("runner") && row.Worker.Handle == handle {
			found = true
			row.Slots = body.Slots
			row.Worker.Slots = body.Slots
			row.Checkouts = body.Checkouts
			h.workers.credentials[key] = row
		}
	}
	if found {
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
		s.credentials[hash] = row
	}
	return nil
}
