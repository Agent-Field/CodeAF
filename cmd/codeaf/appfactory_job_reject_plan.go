package main

// ── codeaf app: SENDING A PLAN BACK ─────────────────────────────────────────
//
// The engine's half of appwire.FactoryPlanRejecter. It is one job write like
// every other (jobWrite, appfactory_actions.go): ONE POST to the factory's own
// typed route, /v1/jobs/{id}/reject_plan, with exactly {"words":…,"expect":…},
// then the receipt read. The route and the body are spelled here and never
// taken from the app.
//
// THE FACTORY DECIDES, UNDER ITS OWN LOCK. Whether the job is still the page
// the person reviewed, still a plan waiting with a finished step before it to
// send the work back to, and whether this caller may answer it are checked by
// the factory on the document held for the item write. A
// read made here first would decide nothing (the job can move after it), so
// none is made; the refusal comes back whole as stale, conflict or forbidden.
// Lease signals and receipts are written separately, as for an answer;
// a failure after the item changed can therefore have an uncertain outcome.
//
// NEVER THE ANSWER ROUTE. A factory from before the typed route answers it as
// an endpoint it does not have, and that refusal is passed on: nothing is
// retried on /answer, whose no would cancel a spending limit, decline a start
// or stop a plan with nowhere to go back to.
//
// There is no commit, no approval receipt and no Undo here: a plan sent back
// is an answer, not an approval.

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryPlanRejecter = (*appBackend)(nil)

// factoryVerbRejectPlan is the factory's typed route for a plan sent back.
const factoryVerbRejectPlan = "reject_plan"

func (b *appBackend) FactoryJobRejectPlan(ctx context.Context, p appwire.FactoryJobRejectPlanParams) (appwire.FactoryJobActed, error) {
	if out, handled, err := b.answerAgentField(ctx, p.Factory, p.Job, appwire.JobActRejectPlan, agentfield.DecisionRejected, p.Words, p.Expect); handled {
		return out, err
	}
	// A PLAN OF THIS FACTORY'S OWN IS SENT BACK WITH WORDS: a bare no would
	// only rewind the run. (An AgentField run's Reject, above, takes none.)
	// An AgentField run's Reject takes no words, on another machine's factory
	// too: that factory's job is read first, and blank words go only to a job
	// waiting on a run's pause.
	if strings.TrimSpace(p.Words) == "" && !b.waitsOnRunPause(ctx, p.Factory, p.Job) {
		return appwire.FactoryJobActed{}, appwire.Errorf(appwire.CodeBadParams, "words is required")
	}
	return b.jobWrite(ctx, p.Factory, appwire.JobActRejectPlan, p.Job, factoryVerbRejectPlan, api.RejectPlan{Words: p.Words, Expect: p.Expect})
}

// waitsOnRunPause says another machine's factory shows job waiting on an
// AgentField run's pause. This machine's own factory answered that above.
func (b *appBackend) waitsOnRunPause(ctx context.Context, id string, job int) bool {
	if host, _ := b.localHostOf(id); host != nil {
		return false
	}
	j, err := b.FactoryJob(ctx, id, job)
	return err == nil && j.NeedsYou != nil && j.NeedsYou.Question != nil && j.NeedsYou.Question.Kind == factory.QKindAgentField
}
