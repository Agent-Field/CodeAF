package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// WHEN THE PEOPLE OR THE FILES CHANGE UNDER A TASK (Workstreams v5, W2 on W4
// and W5): a person who leaves a workstream hands their open tasks to the
// rules' fallback, else the steward; a change to a file's contents stops the
// running tasks that read it on a question to their owner.

func init() { WorkstreamPeopleRemoved = handTasksOn }

// handTasksOn hands removed people's open tasks, in ws and every workstream
// inside it, to `to`. A Factory task's hand-off is kept as its overlay, as a
// hand-off by hand is. A failure here leaves the people saved and the tasks
// where they were: the save has already happened, and a person can still hand
// each one on by hand.
func handTasksOn(h *Handler, r *http.Request, wsIDs []string, removed []string, to string) {
	if h.config.Store == nil || to == "" {
		return
	}
	gone := map[string]bool{}
	for _, p := range removed {
		gone[factory.NormalizeHandle(p)] = true
	}
	names := h.personNames()
	by := factory.TaskEventBy{Kind: factory.TaskByPerson, ID: h.orgActor(r), Name: names(h.orgActor(r))}
	name := names(to)
	now := h.clock()
	for _, id := range wsIDs {
		ws, ok, err := h.config.Store.Workstream(id)
		if err != nil || !ok {
			continue
		}
		var handed []factory.WorkstreamTask
		err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
			kept, err := tw.Tasks(id)
			if err != nil {
				return err
			}
			for _, t := range kept {
				next, events, changed := factory.HandOn(t, gone, to, name, by, h.newTaskID("te_"), now)
				if !changed {
					continue
				}
				// A kept row is already a task or a Factory task's overlay: it is
				// put back as it was read, under its own rev.
				if err := tw.Put(next, t.Rev); err != nil {
					return err
				}
				if err := tw.Append(id, events...); err != nil {
					return err
				}
				handed = append(handed, next)
			}
			return nil
		})
		if err == nil {
			h.publishTasks(ws, by.ID, handed...)
		}
	}
}

// flagTasksOfFile flags the running tasks that read f (its used_by) after
// its contents changed. Read on the server's own authority: the person who
// changed the file need not see every task that read it.
func (h *Handler) flagTasksOfFile(r *http.Request, ws factory.OrgWorkstream, f factory.WorkstreamFile, by string) {
	if h.config.Store == nil || len(f.UsedBy) == 0 {
		return
	}
	used := map[string]bool{}
	for _, id := range f.UsedBy {
		used[id] = true
	}
	jobs, err := h.workstreamJobs(r, ws)
	if err != nil {
		return
	}
	who := factory.TaskEventBy{Kind: factory.TaskByPerson, ID: by, Name: by}
	now := h.clock()
	var flagged []factory.WorkstreamTask
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		for _, t := range all {
			if !used[t.ID] {
				continue
			}
			next, events, changed := factory.FlagForFile(t, f.Name, who, h.newTaskID("te_"), now)
			if !changed {
				continue
			}
			if next.Rev != t.Rev {
				if err := keepTask(tw, next, t.Rev); err != nil {
					return err
				}
				flagged = append(flagged, next)
			}
			if err := tw.Append(ws.ID, events...); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		h.publishTasks(ws, by, flagged...)
	}
}
