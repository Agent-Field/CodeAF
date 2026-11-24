package serve

import (
	"context"
	"slices"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ClaimFactoryJobs brings each new issue into the workstream whose Factory
// switch takes it ([factory.FactoryWorkstreamFor]), so it becomes a task
// there with no person adding it. It runs after every poll tick, before the
// go-ahead pass, so the job then starts within that workstream's "start
// without asking" rule ([Handler.syncStartWithoutAsking]) and above it waits
// for its owner, rather than starting under the repository's own budget.
//
// A job is claimed only while it is new: in Backlog, never run, in no
// workstream yet. Taking one out of a workstream again is a person's choice
// the claim never undoes, because it is already in none and its issue's open
// time is long past the next time the switch goes on.
func (h *Handler) ClaimFactoryJobs(ctx context.Context) error {
	st := h.config.Store
	if st == nil {
		return nil
	}
	all, err := st.Workstreams()
	if err != nil || len(all) == 0 {
		return err
	}
	taking := false
	for _, w := range all {
		if r := w.RulesOrDefault(); w.ArchivedAt == nil && r.KeepsRunning.Factory {
			taking = true
			break
		}
	}
	if !taking {
		return nil
	}
	member, err := st.JobWorkstreams()
	if err != nil {
		return err
	}
	items, err := st.List()
	if err != nil {
		return err
	}
	claims := map[string][]int{}
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, in := member[it.ID]; in || it.State != factory.StateNew || factory.EverRan(it) || factory.StoppedByPerson(it) || factory.JobState(it) == "done" {
			continue
		}
		if id, ok := factory.FactoryWorkstreamFor(all, it); ok {
			claims[id] = append(claims[id], it.ID)
		}
	}
	if len(claims) == 0 {
		return nil
	}
	now := h.clock().UTC()
	added := map[string][]int{}
	saved, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		var changed []string
		held := map[int]bool{}
		for _, w := range all {
			for _, job := range w.Jobs {
				held[job] = true
			}
		}
		for id, jobs := range claims {
			cur, ok := all[id]
			if !ok || cur.ArchivedAt != nil || !cur.RulesOrDefault().KeepsRunning.Factory {
				continue
			}
			next := slices.Clone(cur.Jobs)
			for _, job := range jobs {
				if len(next) >= workstreamListMost {
					break
				}
				// A person may have put it somewhere since the read.
				if !held[job] {
					held[job] = true
					next = append(next, job)
					added[id] = append(added[id], job)
				}
			}
			if len(added[id]) == 0 {
				continue
			}
			cur.Jobs, cur.UpdatedAt = next, now
			all[id] = cur
			changed = append(changed, id)
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(added))
	for id, jobs := range added {
		ids = append(ids, id)
		for _, job := range jobs {
			h.publishWorkstreamPart(saved[id], api.PartJobs, strconv.Itoa(job), "", saved[id].Rev, false)
		}
	}
	slices.Sort(ids)
	h.syncStartWithoutAsking(ids...)
	return nil
}
