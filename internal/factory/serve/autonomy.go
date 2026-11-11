package serve

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) setAutonomy(w http.ResponseWriter, r *http.Request, it factory.Item, mode string) {
	if factory.JobState(it) == api.StateDone || factory.JobState(it) == api.StateDropped || it.Publishing {
		h.fail(w, 409, "Finished or publishing jobs cannot change automation mode.")
		return
	}
	unlock, err := h.lockConsent()
	if h.failed(w, err) {
		return
	}
	defer unlock()
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if factory.JobState(*cur) == api.StateDone || factory.JobState(*cur) == api.StateDropped || cur.Publishing {
			return fmt.Errorf("job finished or publishing began before the mode changed")
		}
		cur.Autonomy = mode
		if factory.ReadinessPending(*cur) {
			f := *cur.FollowUp
			f.Reason = factory.ReadinessApprovalWords(*cur)
			cur.FollowUp = &f
		}
		// Manual withdraws an automatic approval, never a person's explicit one.
		if mode == factory.AutonomyManual && cur.Approval != nil && cur.Approval.Automatic && cur.Approval.State == "approving" {
			cur.Approval = nil
		}
		cur.MergePolicy = nil
		return nil
	})
	if h.failed(w, err) {
		return
	}
	if h.workers != nil && h.workers.desk != nil {
		_, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalAutonomy, Words: mode})
	}
	h.finish(w, err)
}

// Critical questions, fixed gates, failed publication and exhausted spending
// stay visible. Only normal progression reaches the existing approval door.
func automaticApprovalAllowed(it factory.Item) bool {
	if !factory.Automatic(it) || factory.AutonomyBlockReason(it) != "" || !factory.ManagerMayPass(it) {
		return false
	}
	a := factory.JobApproval(it)
	if a == nil || a.Error != "" {
		return false
	}
	if it.State == factory.StateLanded {
		for i := len(it.Stages) - 1; i >= 0; i-- {
			if !it.Stages[i].On {
				continue
			}
			if factory.IsApprove(it.Stages[i]) && it.Stages[i].Fixed {
				return false
			}
			break
		}
		return a.Kind == "proof" && len(it.Proof) > 0 && run.ApprovalRefused(&it, false) == nil
	}
	if it.State != factory.StateNeedsYou || it.QKind != factory.QKindApprove || (a.Kind != "plan" && a.Kind != "step") {
		return false
	}
	if it.Stream == nil || it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) {
		return false
	}
	name := it.Stream.Phases[it.Stream.Cur].Name
	for _, st := range it.Stages {
		if st.Name == name {
			return !st.Fixed
		}
	}
	return false
}

// The service records automatic decisions, exactly as manual decisions; the
// worker never fabricates its own approval trailer. No visible UI is needed.
func (h *Handler) prepareAutomaticApprovals(items []factory.Item) []factory.Item {
	h.judgePasses(items)
	candidate := false
	for _, it := range items {
		if h.automaticApprovalAllowed(it) && factory.JobApproval(it).State == "waiting" {
			candidate = true
			break
		}
	}
	if !candidate {
		return items
	}
	unlock, err := h.lockConsent()
	if err != nil {
		return items
	}
	defer unlock()
	for i, candidate := range items {
		if !h.automaticApprovalAllowed(candidate) {
			continue
		}
		cur, e := h.config.Store.Get(candidate.ID)
		if e != nil || !h.automaticApprovalAllowed(cur) {
			continue
		}
		a := factory.JobApproval(cur)
		if a.State != "waiting" {
			continue
		}
		if a.Kind == "proof" {
			if h.seam.SignOff == nil {
				continue
			}
		} else if h.seam.Answer == nil {
			continue
		}
		a.State, a.Automatic, a.DecidedBy = "approving", true, ""
		a.At, a.UndoUntil, a.Commit = h.clock().UTC(), h.clock().UTC(), cur.HeadCommit
		if e = h.config.Store.Update(cur.ID, func(saved *factory.Item) error {
			if !h.automaticApprovalAllowed(*saved) || saved.HeadCommit != cur.HeadCommit {
				return fmt.Errorf("job changed before automatic approval")
			}
			saved.Approval = a
			saved.MergePolicy = nil
			return nil
		}); e == nil {
			items[i], _ = h.config.Store.Get(cur.ID)
		}
	}
	return items
}

func (h *Handler) automaticApprovalAllowed(it factory.Item) bool {
	if !automaticApprovalAllowed(it) {
		return false
	}
	sw, err := h.config.Store.RepoSwitch()
	return err == nil && !sw.Off(factory.RepoOf(it))
}

// judgePasses asks the manager's judgement of every waiting `manager when …`
// approval that Auto would otherwise pass and that has none yet, in the
// background: the answer is kept on the job ([factory.Item.PassJudge]) and
// the job timer runs again, which passes the approval on a yes and leaves it
// to a person, with the manager's reason, on a no. A JUDGE THAT CANNOT
// ANSWER IS A NO: no model, a refused call or an unreadable reply leaves the
// approval to a person.
func (h *Handler) judgePasses(items []factory.Item) {
	for _, it := range items {
		if !factory.Automatic(it) || factory.AutonomyBlockReason(it) != "" {
			continue
		}
		cond, ok := factory.NeedsPassJudgement(it)
		if !ok {
			continue
		}
		st, _, _ := factory.WaitingGateStage(it)
		key := factory.PassJudgeKey(it, st, cond)
		h.judgingMu.Lock()
		if h.judging == nil {
			h.judging = map[int]string{}
		}
		if h.judging[it.ID] == key {
			h.judgingMu.Unlock()
			continue
		}
		h.judging[it.ID] = key
		h.judgingMu.Unlock()
		h.jobsBusy.Add(1)
		go func(it factory.Item, cond, key string) {
			defer h.jobsBusy.Done()
			defer func() {
				h.judgingMu.Lock()
				if h.judging[it.ID] == key {
					delete(h.judging, it.ID)
				}
				h.judgingMu.Unlock()
			}()
			j := factory.PassJudgement{Key: key, Why: "there was no model to judge it with"}
			if h.config.PassJudge != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				a, err := h.config.PassJudge.Judge(ctx, cond, factory.PassEvidence(it))
				cancel()
				switch {
				case err != nil:
					j.Why = "it could not be judged: " + strings.TrimSuffix(err.Error(), ".")
				default:
					j.Yes, j.Why, j.Model = a.Yes, a.Why, a.Model
				}
			}
			j.At = h.clock().UTC()
			_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
				st, _, ok := factory.WaitingGateStage(*cur)
				if !ok {
					return nil
				}
				if k, c := factory.PassOf(st); k != factory.PassManagerWhen || factory.PassJudgeKey(*cur, st, c) != key {
					return nil
				}
				cp := j
				cur.PassJudge = &cp
				return nil
			})
			h.jobsMu.Lock()
			closed := h.jobsClosed
			h.jobsMu.Unlock()
			if !closed {
				h.settleJobs(true)
				h.scheduleJobTimer()
			}
		}(it, cond, key)
	}
}
