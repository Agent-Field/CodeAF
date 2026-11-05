package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

var errPolicyMoved = errors.New("the group's policy changed since it was read; read it again, then make the change")

func (h *Handler) policyRoutes() {
	h.route("/v1/groups/{group}/policy", map[string]http.HandlerFunc{http.MethodGet: h.groupPolicy, http.MethodPut: h.groupPolicy})
}

func (h *Handler) groupPolicy(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	group, err := factory.CheckGroup(r.PathValue("group"))
	if err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	if group == "" {
		h.fail(w, 422, "a policy requires a non-empty group")
		return
	}
	if r.Method == http.MethodGet {
		p, err := h.config.Store.Policy(group)
		if h.failed(w, err) {
			return
		}
		h.writePolicy(w, p)
		return
	}
	var body struct {
		Stopped    bool                `json:"stopped"`
		Allowances []factory.Allowance `json:"allowances"`
		Version    *int                `json:"version,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	next := factory.GroupPolicy{Group: group, Stopped: body.Stopped, Allowances: body.Allowances}
	if err := next.Check(); err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	if body.Version != nil && *body.Version < 0 {
		h.fail(w, 422, "policy version must not be negative")
		return
	}
	caller, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	caller = factory.NormalizeHandle(caller)
	operator := h.isOperator(r)
	if caller == "" && operator {
		caller = h.operatorLogin(r)
	}
	stopped := false
	clearPhase(w)
	saved, err := h.config.Store.ChangePolicy(group, func(cur *factory.GroupPolicy) error {
		if h.mode() != api.FactorySolo && (caller == "" || cur.Owner != "" && caller != cur.Owner && !operator) {
			return fs.ErrPermission
		}
		if body.Version != nil && *body.Version != cur.Version {
			return errPolicyMoved
		}
		stopped = !cur.Stopped && next.Stopped
		next.Owner = cur.Owner
		if next.Owner == "" {
			next.Owner = caller
		}
		now := h.clock().UTC()
		next.UpdatedAt = &now
		*cur = next
		return nil
	})
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, errPolicyMoved) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		if errors.Is(err, fs.ErrPermission) {
			h.fail(w, 403, "only the policy's owner or the operator can change it")
		} else {
			h.refuse(w, 409, err.Error(), "stale", "", nil, "")
		}
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.PolicyChanged, Verb: "changed policy", Handle: caller, Object: "group:" + group, Detail: fmt.Sprintf("policy version %d; stopped=%t", saved.Version, saved.Stopped)})
		if h.failed(w, err) {
			return
		}
	}
	if stopped && h.failed(w, h.stopPolicyJobs(r, group)) {
		return
	}
	h.writePolicy(w, saved)
}

func (h *Handler) writePolicy(w http.ResponseWriter, p factory.GroupPolicy) {
	jobs, err := h.config.Store.List()
	if h.failed(w, err) {
		return
	}
	h.write(w, 200, api.PolicyFrom(p, jobs, h.clock()))
}

func (h *Handler) stopPolicyJobs(r *http.Request, group string) error {
	jobs, err := h.config.Store.List()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Group != group || !factory.MayStopAfterStep(job) || job.StopAfterStep {
			continue
		}
		err := h.recordAction(r, job, record.StopAfterStepChanged, "stop_after_step=true; its group is stopped", func() error {
			return h.config.Store.Update(job.ID, func(cur *factory.Item) error {
				if cur.Group != group || !factory.MayStopAfterStep(*cur) || cur.StopAfterStep {
					return errWaitsUnchanged
				}
				cur.StopAfterStep = true
				cur.Note(h.clock(), "stop after this step: its group is stopped")
				return nil
			})
		})
		if err != nil && !errors.Is(err, errWaitsUnchanged) {
			return err
		}
	}
	return nil
}

// continuePolicy releases the same cap question as a budget hold. Its worker
// lease was withdrawn, so the saved step returns to the queue for admission.
func (h *Handler) continuePolicy(r *http.Request, it factory.Item, words string) (bool, error) {
	if h.config.Store != nil && it.PolicyRun != "" && it.QKind == "cap" {
		if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error { cur.PolicyRun = ""; return nil }); err != nil {
			return true, err
		}
	}
	if it.Blocked == nil || it.Blocked.By != factory.BlockPolicy || it.QKind != "cap" || h.config.Store == nil {
		return false, nil
	}
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if cur.Blocked == nil || cur.Blocked.By != factory.BlockPolicy || cur.QKind != "cap" {
			return fmt.Errorf("this job's policy hold changed; read it again")
		}
		factory.ClearPolicyRun(cur)
		run.ReleaseCap(cur, h.clock())
		return nil
	})
	if err == nil {
		err = h.recordContinued(r, it, words)
	}
	return true, err
}

// resumePolicyStart keeps the saved work when Start answers a policy hold.
// No worker holds this run, so queuing it needs no second Launch.
func (h *Handler) resumePolicyStart(it *factory.Item) bool {
	if it.State != factory.StateNeedsYou || it.QKind != "cap" || it.Blocked == nil || it.Blocked.By != factory.BlockPolicy {
		return false
	}
	run.ReleaseCap(it, h.clock())
	return true
}
