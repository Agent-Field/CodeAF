package serve

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// taskRoutes is the door of an extra task (api/task.go).
func (h *Handler) taskRoutes() {
	h.postJob("task", h.config.Store != nil, h.taskJob)
}

// taskJob is POST /v1/jobs/{id}/task: the job's main agent, or its person,
// sends a delegate into the job's checkout with plain instructions. The rules
// that take or refuse it are factory.TaskRefusal's, applied by the lease
// desk's door under the job's lock, so this handler decides nothing about the
// job's state: it reads the body, holds the consent lock so an approval and a
// task never cross, and records what was asked.
//
// WHO ASKED IS THE CALLER'S WORD. The local factory runs with one credential,
// so the service cannot tell the main agent from the person; the trusted
// caller in this process says which (the wire contract's by), and the rules
// are the same for both. Only the job's own person may send one either way.
func (h *Handler) taskJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.Task
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "task", body.Expect) || !h.owns(w, r, it) || !h.workerReady(w) {
		return
	}
	words := strings.TrimSpace(body.Words)
	if len(words) > api.MaxTaskWords {
		h.fail(w, http.StatusUnprocessableEntity, "The task's instructions are too long.")
		return
	}
	by := strings.TrimSpace(body.By)
	switch by {
	case "":
		by = api.TaskByYou
	case api.TaskByYou, api.TaskByMainAgent:
	default:
		h.fail(w, http.StatusUnprocessableEntity, "by must be main_agent or you")
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	unlock, err := h.lockConsent()
	if h.failed(w, err) {
		return
	}
	defer unlock()
	added, err := lease.NewDoors(h.workers.desk).Task(it.ID, by, words, body.Now)
	var refused factory.TaskRefused
	if errors.As(err, &refused) {
		status, reason := http.StatusConflict, "conflict"
		if words == "" {
			status, reason = http.StatusUnprocessableEntity, "bad_input"
		}
		h.refuse(w, status, refused.Error(), reason, "", nil, factory.JobState(it))
		return
	}
	// THE MAIN AGENT'S LIMIT IS A REFUSAL, NOT A FAULT. A task it sends to a
	// stop it has already put back to work as often as it may is refused by
	// the same rule as its other decisions (factory's task_handoff.go). That
	// error is not a [factory.TaskRefused], so it used to leave here as a 500,
	// which the app reads as "this may have happened".
	if errors.Is(err, factory.ErrManagerRoundsUsed) {
		h.refuse(w, http.StatusConflict, err.Error(), "conflict", "", nil, factory.JobState(it))
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.config.Record != nil {
		event := record.Event{Kind: record.Tasked, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: words}
		if added.By == factory.TaskByMainAgent {
			event.Actor = record.Actor{Kind: "agent", Name: "main agent"}
		}
		if _, err := h.config.Record.Append(r.Context(), event); h.failed(w, err) {
			return
		}
	}
	h.scheduleJobTimer()
	// A pull request not read since it was opened queues no round until it
	// is read (factory.QueueCIFollowUp); the poll is woken to read it now.
	if added.Road == factory.TaskRunsFollow {
		_ = h.config.Store.NudgePoll()
		factorygithub.Nudge(h.config.Store)
	}
	runs := api.TaskRunsNow
	if added.Road == factory.TaskRunsAfter || added.Road == factory.TaskRunsFollowNext {
		runs = api.TaskRunsAfterRound
	}
	h.write(w, http.StatusOK, api.TaskSent{Factory: h.factory, Step: added.Step, Runs: runs})
}

// prepareHeldTasks starts the Follow PR round of every task that was asked
// for while a round was running on an open pull request, once no runner
// holds the job ([factory.StartHeldFollowTask]). It runs before the automatic
// approvals are looked at: a repair the round just landed is not approved
// and pushed ahead of a task that will change its commit.
func (h *Handler) prepareHeldTasks(items []factory.Item, leases []lease.Lease) []factory.Item {
	if h.workers == nil || h.workers.desk == nil {
		return items
	}
	held := make(map[int]bool, len(leases))
	for _, row := range leases {
		held[row.Job] = true
	}
	for i, it := range items {
		if it.FollowUp == nil || strings.TrimSpace(it.FollowUp.Next) == "" || held[it.ID] {
			continue
		}
		started, err := lease.NewDoors(h.workers.desk).StartHeldTask(it.ID)
		if err != nil || !started {
			continue
		}
		if fresh, e := h.config.Store.Get(it.ID); e == nil {
			items[i] = fresh
		}
	}
	return items
}
