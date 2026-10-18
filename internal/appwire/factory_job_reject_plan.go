package appwire

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── SENDING A PLAN BACK, ON AN ATTACHED FACTORY ─────────────────────────────
//
// factory.job_reject_plan is the app's "Request changes" on a PLAN: the
// person's words go back to the finished step before the plan's approve step,
// which plans again with them. It is carried to the factory's own typed route,
// POST /v1/jobs/{id}/reject_plan, and NEVER to its answer route: the answer
// route takes a no for every question a job can wait on, and a no to a
// spending limit or to an exhausted round budget cancels the job, to a start
// declines it, and to a plan with no finished step before it stops it. It is
// not factory.job_request_changes either, which is the PROOF send-back of a
// landed job and can never act on a plan.
//
// THE FACTORY DECIDES, ON ITS LOCKED DOCUMENT. Whether the job is still the
// version the person reviewed, still a plan waiting with a finished step to
// send the work back to, and whether the caller may answer it are checked by
// the factory inside the same lock as the write; this wire only refuses what
// can never be sent (blank or too many words, no exact version) and carries
// the factory's answer back whole. A factory that does not offer the revision
// says so in the job's can.reject_plan, and one from before the route refuses
// it as an endpoint it does not have, with nothing written.
//
// IT IS NOT A DECISION TO PUBLISH. There is no commit, no approval receipt and
// no Undo window: a plan sent back leaves an `answered` receipt and the run
// starts over from an earlier step. It is therefore never sent twice, and an
// outcome the engine cannot confirm is [CodeUncertain].
//
// THE VERSION THE PERSON READ IS THE GUARD. `expect.version` and
// `expect.state` (which must be needs_you) are required and travel with the
// write; a job that moved since is refused by the factory as [CodeStale].

// FeatureFactoryJobRejectPlan is announced in hello when the engine serves
// factory.job_reject_plan. It is independent of [FeatureFactoryJobActions]: an
// engine without it answers the method as unknown_method.
const FeatureFactoryJobRejectPlan = "factory.job_reject_plan"

// JobActRejectPlan is the action a [FactoryJobActed] names for a plan sent back.
const JobActRejectPlan = "reject_plan"

// FactoryJobRejectPlanParams sends one plan back with what to change. Words are
// required (a bare no would only rewind the run) and at most
// [api.MaxRejectPlanWords] characters. Expect.Version and Expect.State
// (needs_you) are required.
type FactoryJobRejectPlanParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Words   string      `json:"words"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryPlanRejecter is the engine's half of factory.job_reject_plan. It is
// optional on top of [Factory] and independent of [FactoryActor]. It makes at
// most one write to the factory and never sends it twice.
type FactoryPlanRejecter interface {
	FactoryJobRejectPlan(ctx context.Context, p FactoryJobRejectPlanParams) (FactoryJobActed, error)
}

func withFactoryJobRejectPlanFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPlanRejecter); ok {
		hello = withFeature(hello, FeatureFactoryJobRejectPlan)
	}
	return hello
}

func prepRejectPlan(p *FactoryJobRejectPlanParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	if p.Expect.State != api.StateNeedsYou {
		return Errorf(CodeBadParams, "expect.state must be %s: a plan is sent back only while it waits for its person", api.StateNeedsYou)
	}
	var err error
	p.Words, err = words("words", p.Words, api.MaxRejectPlanWords, true)
	return err
}

// factoryJobRejectPlanMethods is the table of the one method, for the tests
// that walk it.
var factoryJobRejectPlanMethods = map[string]method{}

func init() {
	m := shaped[FactoryJobRejectPlanParams, FactoryJobActed](rejectPlan)
	methods["factory.job_reject_plan"] = m
	factoryJobRejectPlanMethods["factory.job_reject_plan"] = m
}

func rejectPlan(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	const name = "factory.job_reject_plan"
	r, ok := e.(FactoryPlanRejecter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobRejectPlanParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := prepRejectPlan(&p); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryJobRejectPlan(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
