package serve

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The desk offers ten seconds; the server allows transport time for the person's Undo.
const taskDeleteUndo = 30 * time.Second

var errTaskDeleteUndoOver = errors.New("The time to undo this deletion has passed")

func (h *Handler) deleteWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	if _, agent := agentCallOf(r); agent {
		h.fail(w, 403, "Deleting a task is a person's action")
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	var d store.TaskDeletion
	var changed []factory.WorkstreamTask
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		t, found := findTask(all, r.PathValue("tid"))
		if !found {
			return store.ErrTaskGone
		}
		if !factory.MayStop(t, taskActor(v, ws)) {
			return factory.TaskRefused("Only the task's owner or whoever looks after this workstream can delete it")
		}
		if t.Rev != rev {
			return store.ErrTaskStale
		}
		if t.State == factory.TaskRunning || h.taskTurnRunning(ws.ID, t.ID) {
			return factory.TaskRefused("Stop this task before deleting it")
		}
		d = store.TaskDeletion{Task: t, By: v.Handle, Until: h.clock().Add(taskDeleteUndo).Format(time.RFC3339Nano)}
		for _, o := range all {
			if !slices.Contains(o.WaitingOn, t.ID) {
				continue
			}
			d.Waits = append(d.Waits, store.TaskWaitBefore{ID: o.ID, Rev: o.Rev, WaitingOn: slices.Clone(o.WaitingOn), Starts: o.Starts})
			before := o.Rev
			o.WaitingOn = slices.DeleteFunc(slices.Clone(o.WaitingOn), func(id string) bool { return id == t.ID })
			if len(o.WaitingOn) == 0 && factory.AgentDoes(o) {
				o.Starts = factory.TaskStartsByHand
			}
			o.Rev++
			o.UpdatedAt = h.clock().Format(time.RFC3339Nano)
			if err := keepTask(tw, o, before); err != nil {
				return err
			}
			changed = append(changed, o)
			if err := tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: o.ID, At: o.UpdatedAt, By: h.taskBy(r, v), Kind: factory.TaskEventState, Text: "No longer waiting: “" + t.Title + "” was deleted"}); err != nil {
				return err
			}
		}
		return tw.Hide(ws.ID, t.ID, d)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.publishWorkstreamPart(ws, api.PartTasks, d.Task.ID, h.orgActor(r), d.Task.Rev+1, true)
	h.publishTasks(ws, h.orgActor(r), changed...)
	h.write(w, 200, map[string]string{"undo_until": d.Until})
}

func (h *Handler) restoreWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	if _, agent := agentCallOf(r); agent {
		h.fail(w, 403, "Restoring a task is a person's action")
		return
	}
	var restored factory.WorkstreamTask
	var changed []factory.WorkstreamTask
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		d, err := tw.Deletion(ws.ID, r.PathValue("tid"))
		if err != nil {
			return err
		}
		if !factory.MayStop(d.Task, taskActor(v, ws)) {
			return factory.TaskRefused("Only the task's owner or whoever looks after this workstream can restore it")
		}
		until, err := time.Parse(time.RFC3339Nano, d.Until)
		if err != nil || !h.clock().Before(until) {
			return errTaskDeleteUndoOver
		}
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		if err := tw.Reveal(ws.ID, d.Task.ID); err != nil {
			return err
		}
		restored = d.Task
		restored.WaitingOn = slices.DeleteFunc(slices.Clone(restored.WaitingOn), func(id string) bool { _, ok := findTask(all, id); return !ok })
		restored.Rev++
		restored.UpdatedAt = h.clock().Format(time.RFC3339Nano)
		if err := keepTask(tw, restored, d.Task.Rev); err != nil {
			return err
		}
		for _, before := range d.Waits {
			cur, ok := findTask(all, before.ID)
			if !ok || cur.Rev != before.Rev+1 {
				continue
			}
			rev := cur.Rev
			cur.WaitingOn = slices.Clone(before.WaitingOn)
			cur.Starts = before.Starts
			cur.Rev++
			cur.UpdatedAt = restored.UpdatedAt
			if err := keepTask(tw, cur, rev); err != nil {
				return err
			}
			changed = append(changed, cur)
			if err := tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: cur.ID, At: cur.UpdatedAt, By: h.taskBy(r, v), Kind: factory.TaskEventState, Text: "Waiting restored: “" + restored.Title + "” was restored"}); err != nil {
				return err
			}
		}
		return nil
	})
	if h.taskRefused(w, err) {
		return
	}
	h.publishTasks(ws, h.orgActor(r), append(changed, restored)...)
	h.write(w, 200, restored)
}
