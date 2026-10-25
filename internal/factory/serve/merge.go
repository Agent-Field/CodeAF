package serve

// ── MERGE: THE PERSON'S PRESS, AND THE PIPELINE'S AUTO-MERGE ─────────────
//
// POST /v1/jobs/{id}/merge is a person pressing Merge. The factory runs
// [factory.MergeGate] on the job as its last follow read it and refuses with
// the gate's words; a merge the gate passes waits [factory.MergeUndo] with
// Undo, then settles: the gate runs again and the lease carries the merge to
// the runner that holds the assignee's forge account (lease.Doors.Merge).
//
// AUTO-MERGE IS THE PIPELINE'S, AND NOTHING ELSE'S. The sweep below looks only
// at jobs whose pinned policy turned auto-merge on when their proof was
// approved, and sends a merge only when the gate passes for the pipeline's
// own ask. A job whose policy is off, or absent, is never merged by it. A
// merge that failed is never retried by the sweep: a person presses Merge.

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// mergeJob is POST /v1/jobs/{id}/merge.
func (h *Handler) mergeJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
		// Commit is the head the person was shown; a different one now is
		// refused rather than merged.
		Commit string `json:"commit,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "merge", body.Expect) || !h.owns(w, r, it) {
		return
	}
	if h.workers == nil || h.workers.desk == nil {
		h.fail(w, http.StatusServiceUnavailable, "this factory has no runner to merge with")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	who = factory.NormalizeHandle(who)
	if who == "" {
		h.fail(w, http.StatusUnprocessableEntity, "Sign in to GitHub before merging this job")
		return
	}
	v := factory.MergeGate(it, factory.MergeAsk{By: who})
	if !v.OK {
		h.refuse(w, http.StatusConflict, v.Why, "merge_refused", "", nil, factory.JobState(it))
		return
	}
	if c := strings.TrimSpace(body.Commit); c != "" && !strings.EqualFold(c, v.Commit) {
		h.refuse(w, http.StatusConflict, "the pull request has commits you did not approve", "merge_refused", "", nil, factory.JobState(it))
		return
	}
	now := h.clock().UTC()
	m := factory.MergeRun{State: factory.MergeAsked, By: who, Rule: v.Rule, Commit: v.Commit, Method: mergeMethod(it), At: now, UndoUntil: now.Add(factory.MergeUndo)}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cp := m
		cur.Merging = &cp
		return nil
	})) {
		return
	}
	if h.config.Record != nil {
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.MergeAsked, Job: it.ID, Handle: who, Detail: v.Rule})
	}
	h.scheduleJobTimer()
	h.write(w, http.StatusOK, struct {
		Factory   api.Factory   `json:"factory"`
		Merge     *api.JobMerge `json:"merge"`
		UndoUntil time.Time     `json:"undo_until"`
	}{h.factory, api.FromItem(withMerging(it, m)).Merge, m.UndoUntil})
}

func withMerging(it factory.Item, m factory.MergeRun) factory.Item {
	it.Merging = &m
	return it
}

// mergeMethod is the method the job's pinned pipeline names, "" for the
// repository's own choice.
func mergeMethod(it factory.Item) string {
	if it.MergePolicy != nil {
		return it.MergePolicy.Method
	}
	return ""
}

// settleMerges sends every pressed Merge whose Undo window has passed, and,
// when sweep is set, every merge the pipeline's auto-merge may make now.
// Both run the gate again first: facts read since the press win.
func (h *Handler) settleMerges(sweep bool, items []factory.Item) error {
	if h.config.Store == nil || h.workers == nil || h.workers.desk == nil {
		return nil
	}
	now := h.clock().UTC()
	var due []factory.Item
	for _, it := range items {
		if m := it.Merging; m != nil && m.State == factory.MergeAsked && !now.Before(m.UndoUntil) {
			due = append(due, it)
			continue
		}
		if sweep && it.Merging == nil && it.MergePolicy != nil && it.MergePolicy.Auto && factory.MergeGate(it, factory.MergeAsk{Auto: true}).OK {
			due = append(due, it)
		}
	}
	if len(due) == 0 {
		return nil
	}
	unlock, err := h.lockConsent()
	if err != nil {
		return err
	}
	defer unlock()
	doors := lease.NewDoors(h.workers.desk)
	for _, stale := range due {
		it, err := h.config.Store.Get(stale.ID)
		if err != nil {
			return err
		}
		if err := h.sendMerge(doors, it, now); err != nil {
			return err
		}
	}
	return nil
}

// sendMerge runs the gate once more and hands a passing merge to the lease.
func (h *Handler) sendMerge(doors *lease.Doors, it factory.Item, now time.Time) error {
	ctx := context.Background()
	var ask factory.MergeAsk
	var run factory.MergeRun
	switch m := it.Merging; {
	case m != nil && m.State == factory.MergeAsked && !now.Before(m.UndoUntil):
		ask = factory.MergeAsk{By: m.By}
		run = *m
	case m == nil && it.MergePolicy != nil && it.MergePolicy.Auto:
		ask = factory.MergeAsk{Auto: true}
		run = factory.MergeRun{Auto: true, Method: mergeMethod(it), At: now}
	default:
		return nil
	}
	check := it
	check.Merging = nil
	v := factory.MergeGate(check, ask)
	if !v.OK {
		if run.By == "" {
			// An auto-merge the gate does not pass yet simply waits: nothing
			// was asked, so nothing is refused.
			return nil
		}
		err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if cur.Merging == nil || cur.Merging.State != factory.MergeAsked {
				return nil
			}
			cp := *cur.Merging
			cp.State, cp.Why, cp.UndoUntil = factory.MergeFailed, v.Why, time.Time{}
			cur.Merging = &cp
			return nil
		})
		if err != nil {
			return err
		}
		if h.config.Record != nil {
			_, _ = h.config.Record.Append(ctx, record.Event{Kind: record.MergeRefused, Job: it.ID, Handle: run.By, Detail: v.Why})
		}
		return nil
	}
	run.Rule, run.Commit = v.Rule, v.Commit
	if err := doors.Merge(it.ID, run); err != nil {
		return err
	}
	if h.config.Record != nil {
		_, _ = h.config.Record.Append(ctx, record.Event{Kind: record.MergeQueued, Job: it.ID, Handle: run.By, Detail: v.Rule})
	}
	return nil
}

// pinnedMerge is the job's pipeline merge settings as they stand now, pinned
// on the job when its proof is approved. A pipeline that cannot be read pins
// nothing, which is auto-merge off.
func (h *Handler) pinnedMerge(r *http.Request, it factory.Item) *factory.MergePolicy {
	if it.Autonomy == factory.AutonomyAuto || it.Autonomy == factory.AutonomyManual {
		return nil
	}
	if h.seam.RecipeAt == nil {
		return nil
	}
	recipe, _, dir, err := h.seam.RecipeAt(it.Repo)
	if err != nil {
		return nil
	}
	if h.config.Pipeline != nil {
		f, err := h.config.Pipeline(r.Context(), it.Repo, dir)
		if err != nil {
			return nil
		}
		recipe = f.Recipe()
	}
	if recipe.Merge == (factory.MergePolicy{}) {
		return nil
	}
	p := recipe.Merge
	return &p
}
