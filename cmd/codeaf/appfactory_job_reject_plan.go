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

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryPlanRejecter = (*appBackend)(nil)

// factoryVerbRejectPlan is the factory's typed route for a plan sent back.
const factoryVerbRejectPlan = "reject_plan"

func (b *appBackend) FactoryJobRejectPlan(ctx context.Context, p appwire.FactoryJobRejectPlanParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActRejectPlan, p.Job, factoryVerbRejectPlan, api.RejectPlan{Words: p.Words, Expect: p.Expect})
}
