package serve

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// WHAT EVERY AGENT IS DOING NOW, ACROSS WORKSTREAMS (owner decisions
// 2026-11-17 §10; DESIGN §11.4). live2 WL found no view across workstreams of
// what agents were doing: a row said "Afi" and nothing else, so a run dead for
// nine minutes and one five seconds into its work looked the same. GET
// /v1/workstreams/running answers, for every workstream the reader can see,
// each task an agent is working on, waiting its turn for, or paused on a
// question about, each carrying its run's facts (task.run: what it is doing
// now, since when, its steps and its cost so far; task.queue: its place) and
// what it is blocked on (task.ask). `?scope=mine` keeps the tasks the reader
// owns or started. The operator also reads the organization's own count.

func (h *Handler) runningRoutes() {
	h.route("/v1/workstreams/running", map[string]http.HandlerFunc{http.MethodGet: h.runningWork})
}

func (h *Handler) runningWork(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	mine := r.URL.Query().Get("scope") == "mine"
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	out := api.RunningWork{Items: []api.RunningItem{}}
	starters := map[taskRunKey]string{}
	if leases, err := h.config.Store.TaskRuns(); err == nil {
		for _, l := range leases {
			starters[taskRunKey{l.WS, l.Task}] = l.Starter
		}
	}
	names := h.personNames()
	for _, ws := range all {
		if ws.ArchivedAt != nil || !v.canSee(ws) {
			continue
		}
		tasks, err := h.config.Store.WorkstreamTasks(ws.ID)
		if err != nil {
			continue
		}
		nameTasks(names, tasks)
		h.fillRuns(ws.ID, tasks)
		for _, t := range tasks {
			if !agentAtWork(t) {
				continue
			}
			if mine && !(t.Owner.Kind == factory.TaskOwnerPerson && factory.NormalizeHandle(t.Owner.ID) == v.Handle) &&
				starters[taskRunKey{ws.ID, t.ID}] != v.Handle {
				continue
			}
			out.Items = append(out.Items, api.RunningItem{WS: ws.ID, WSName: ws.Name, Task: t})
		}
	}
	// Paused on a question first, then at work, then waiting their turn;
	// the longest going first inside each.
	rank := func(t factory.WorkstreamTask) int {
		switch {
		case t.State == factory.TaskNeeds:
			return 0
		case t.Queue != nil:
			return 2
		}
		return 1
	}
	slices.SortStableFunc(out.Items, func(a, b api.RunningItem) int {
		if d := rank(a.Task) - rank(b.Task); d != 0 {
			return d
		}
		if a.Task.Queue != nil && b.Task.Queue != nil {
			return a.Task.Queue.Place - b.Task.Queue.Place
		}
		return strings.Compare(runSince(a.Task), runSince(b.Task))
	})
	if v.Operator {
		d := &h.agentDesk
		d.mu.Lock()
		out.Org = &api.RunningOrg{Running: d.runningInLocked(""), Queued: len(d.queue), Most: taskRunsPerOrg}
		d.mu.Unlock()
	}
	h.write(w, http.StatusOK, out)
}

// agentAtWork is whether t is an agent's task that is going, waiting its turn,
// or paused on a question the agent raised.
func agentAtWork(t factory.WorkstreamTask) bool {
	if t.DoneBy.Kind != factory.TaskByAfi {
		return false
	}
	switch t.State {
	case factory.TaskRunning:
		return true
	case factory.TaskNeeds:
		return true
	}
	return false
}

func runSince(t factory.WorkstreamTask) string {
	if t.Run != nil && t.Run.Since != "" {
		return t.Run.Since
	}
	return t.UpdatedAt
}
