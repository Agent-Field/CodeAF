package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// TASKS START BY THEMSELVES WHEN THEIR WAITS CLEAR, ON THE SERVER TOO (owner
// decisions 2026-11-17, 1; live use E2 #7, where every task a finished task
// released sat at "Ready to start" until somebody pressed its own Start).
// factory.StartsBySelf is the rule; this file applies it where the server can
// act: an Afi task is started in the same write that released it, in its own
// run, with the rights of whoever finished the work it waited on (that
// person's write, or the run working for them). A chat's task lives on its
// person's machine and stays "Ready to start" here. A start Afi cannot take
// (no model key, already being worked on) leaves the task up next with the
// "Ready to start" line, exactly as before, so a person can press Start.

// ownerStarts is the default a task follows when it has no word of its own:
// its owner's "Start tasks when what they wait on is done", on unless they
// turned it off. A task Everyone owns, or a file that cannot be read, has the
// default.
func (h *Handler) ownerStarts(t factory.WorkstreamTask) bool {
	if t.Owner.Kind != factory.TaskOwnerPerson {
		return true
	}
	on, _ := h.config.Store.StartWhenReady(t.Owner.ID)
	return on
}

// fillStarts writes onto each Afi and chat task whether it starts by itself,
// for its facts line. It is read-time only and never kept.
func (h *Handler) fillStarts(all []factory.WorkstreamTask) {
	for i := range all {
		t := &all[i]
		if t.DoneBy.Kind != factory.TaskByAfi && t.DoneBy.Kind != factory.TaskByChat {
			t.StartsWhenReady = nil
			continue
		}
		on := factory.StartsBySelf(*t, all, h.ownerStarts(*t))
		t.StartsWhenReady = &on
	}
}

// startReleased starts each released task that starts by itself and that Afi
// does, inside the releasing write (tw), as starter. It answers the tasks it
// started (now running, already kept) and the events for every released task:
// "Started: <title> is done" for those, "Ready to start: <title> is done" for
// the rest. The caller stops the runs it started if its write then fails.
func (h *Handler) startReleased(tw store.TaskWrite, ws factory.OrgWorkstream, finished factory.WorkstreamTask, released, all []factory.WorkstreamTask, by factory.TaskEventBy, starter string, newID func() string) (started []factory.WorkstreamTask, events []factory.WorkstreamTaskEvent, err error) {
	at := finished.UpdatedAt
	for _, rel := range released {
		line := "Ready to start: " + finished.Title + " is done"
		if !strings.HasPrefix(by.ID, botAgentPrefix) && rel.DoneBy.Kind == factory.TaskByAfi && factory.StartsBySelf(rel, all, h.ownerStarts(rel)) {
			running := rel
			running.State, running.StateReason, running.StoppedBy = factory.TaskRunning, "", nil
			running.Rev++
			running.UpdatedAt = at
			if werr := h.wakeTaskDoer(ws, running, starter); werr == nil {
				if err := keepTask(tw, running, rel.Rev); err != nil {
					h.stopTaskTurn(ws.ID, rel.ID)
					return started, events, err
				}
				started = append(started, running)
				line = "Started: " + finished.Title + " is done"
			} else if !wakeRefused(werr) {
				return started, events, werr
			}
		}
		events = append(events, factory.WorkstreamTaskEvent{ID: newID(), Task: rel.ID, At: at, By: by, Kind: factory.TaskEventState, Text: line})
	}
	return started, events, nil
}

// WorkPrefs is GET and PUT /v1/me/work: the signed-in person's own
// "Start tasks when what they wait on is done". The desk's Settings › Work
// writes it to every organization it is signed in to.
type WorkPrefs struct {
	StartWhenReady *bool `json:"start_when_ready"`
}

func (h *Handler) workPrefsRoutes() {
	h.route("/v1/me/work", map[string]http.HandlerFunc{http.MethodGet: h.workPrefs, http.MethodPut: h.workPrefs})
}

func (h *Handler) workPrefs(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "a setting belongs to a person: sign in first")
		return
	}
	if r.Method == http.MethodPut {
		var body WorkPrefs
		if !h.decode(w, r, &body, false) {
			return
		}
		if body.StartWhenReady == nil {
			h.fail(w, http.StatusUnprocessableEntity, "say start_when_ready, true or false")
			return
		}
		if h.failed(w, h.config.Store.SetStartWhenReady(v.Handle, *body.StartWhenReady)) {
			return
		}
	}
	on, err := h.config.Store.StartWhenReady(v.Handle)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, WorkPrefs{StartWhenReady: &on})
}
