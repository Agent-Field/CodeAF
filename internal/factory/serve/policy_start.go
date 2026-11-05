package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func policyStartable(it factory.Item) bool {
	return it.Group != "" && len(it.WaitsOn) != 0 && it.WaitsReleased && it.PolicyRun == "" && it.Stream == nil && len(it.Adapted) == 0 && it.SpentBefore == 0 && !factory.WaitsOverridden(it)
}

// scanPolicyStarts also visits releases from earlier scans. A missing grant or
// stopped group can therefore become startable without changing the waits.
func (h *Handler) scanPolicyStarts(jobs []factory.Item) error {
	var spent map[int]float64
	var errs []error
	for _, job := range jobs {
		if job.Group == "" || len(job.WaitsOn) == 0 {
			continue
		}
		if job.Stream != nil || factory.WaitsOverridden(job) || job.PolicyRun != "" {
			continue
		}
		if spent == nil {
			spent = make(map[int]float64, len(jobs))
			for _, job := range jobs {
				spent[job.ID] = factory.JobLifetimeSpent(job)
			}
		}
		r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("/v1/jobs/%d/run", job.ID), nil)
		budget, budgetErr := h.startBudget(r, job)
		err := h.startPolicyJob(r, job.ID, budget, budgetErr, spent)
		if err != nil && !errors.Is(err, errWaitsUnchanged) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// startPolicyJob claims only the locked, still released job. A durable
// PolicyRun prevents a second handler from entering Launch for the same run.
func (h *Handler) startPolicyJob(r *http.Request, id int, budget float64, budgetErr error, spent map[int]float64) error {
	var started factory.Item
	var allowance, owner string
	claimed := false
	err := h.config.Store.Update(id, func(it *factory.Item) error {
		if !policyStartable(*it) {
			return errWaitsUnchanged
		}
		p, err := h.config.Store.Policy(it.Group)
		reason := ""
		if err != nil {
			reason = "its group's policy cannot be read"
		} else if p.Stopped {
			reason = "its group is stopped"
		} else if budgetErr != nil {
			reason = budgetErr.Error()
		}
		sw, switchErr := h.config.Store.RepoSwitch()
		if reason == "" && switchErr != nil {
			reason = "the factory's repository settings cannot be read"
		}
		if reason == "" && sw.OffJob(*it) {
			reason = "its repository is off"
		}
		if reason == "" && h.waits.launch == nil {
			reason = "this factory cannot start jobs"
		}
		candidate := *it
		if candidate.Cap <= 0 {
			candidate.Cap = budget
		}
		if reason == "" {
			allowance, reason, claimed = factory.StartDecision(p, candidate, func(id int) float64 {
				if id == it.ID {
					return factory.JobLifetimeSpent(*it)
				}
				return spent[id]
			}, h.clock())
		}
		if claimed && h.mode() != api.FactorySolo && (p.Owner == "" || !h.teamAllows(p.Owner) || it.Person() != "" && it.Person() != p.Owner) {
			reason, claimed = "the policy's owner cannot start this job", false
		}
		if !claimed {
			block := &factory.JobBlock{By: factory.BlockPolicy, Reason: reason, Since: h.clock().UTC()}
			if sameWaitsBlock(it.Blocked, block) {
				return errWaitsUnchanged
			}
			it.Blocked = block
			return nil
		}
		it.Cap = candidate.Cap
		owner = p.Owner
		if h.mode() != api.FactorySolo && it.Person() == "" {
			it.Starter = owner
		}
		retireLaunchWaits(it, allowance)
		started = *it
		return nil
	})
	if err != nil || !claimed {
		return err
	}
	r = r.WithContext(context.WithValue(r.Context(), workerKey{}, lease.Worker{Handle: owner}))
	return h.recordAction(r, started, record.AutoStarted, "allowance "+allowance, func() error { return h.waits.launch(id) })
}
