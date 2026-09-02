package main

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── THE PROCESS'S LANE-SHEET BEAT ───────────────────────────────────────────
//
// The router publishes, per model, one row per machine serving it, and that is
// a free prior for every lane a request might be sent to (docs/ARCHITECTURE.md,
// Decision 10). Fetching it was a SESSION'S errand, and three doors of this
// binary build no session at all — `do`, `run`, and a subharness — so a
// headless install wrote sightings forever and never once read a sheet: no
// cache under `v3/lanes/`, no facts in the belief, and a chooser ranking the
// one machine the last run happened to be served by (issue #318).
//
// SO THE BEAT IS THE PROCESS'S, seated at [installMeasuredRulers] — the one
// function every surface that records anything already calls before it reads or
// writes a profile. A beat started per door would be the same fetch loop
// written four times, and the fourth would drift.
//
// A SESSION STILL STARTS ITS OWN and nothing about it changed. Whichever asks
// first runs the loop and the other hands its models to it, because [lanes.Beat]
// admits ONE BEAT PER SHEET and joins the rest through the queue it already had.

// laneBeatCtx is the lifetime of every beat this process starts. It is the
// process's own, cancelled at the one exit every command shares ([execute]),
// beside the belief writer that runs for exactly as long.
//
// It is a variable rather than an argument because the seam it is read at —
// [installMeasuredRulers] — is called from six surfaces that have no context
// worth threading: the beat outlives every one of their requests and belongs to
// the process instead. Background is what a test gets until it says otherwise.
var laneBeatCtx = context.Background()

// startLaneBeat begins this process's lane-sheet beat, or does nothing at all.
//
// THE THREE REFUSALS ARE THE SESSION'S, WORD FOR WORD (internal/session's
// agent.go). Routing off is a person saying they do not want their endpoints
// chosen for them, and a background fetch would be work nobody asked for on
// somebody who asked for the opposite. A base that is not a router has no sheet
// to publish, whatever the model is spelled — and the gate is the provider's
// own, so that the wire point and this one cannot drift apart. A door with no
// model slot filled has nothing to fetch a sheet about.
//
// The interval is [lanes.Beat]'s own and is not restated here: a second
// spelling of that number is a number that would drift.
func startLaneBeat(settings config.Config, model string) {
	if config.RoutingAt(settings.ProfileDir) == config.RoutingOff {
		return
	}
	if !provider.LaneSheetAvailable(settings.BaseURL) {
		return
	}
	models := laneBeatModels(settings, model)
	if len(models) == 0 {
		return
	}
	// Under the guard, like every fire-and-forget goroutine in this binary: a
	// fault in a background fetch may not take the surface down with it.
	ctx, sheet := laneBeatCtx, lanes.Default().Sheet()
	guard.Go("lanes/beat", func() { lanes.Beat(ctx, sheet, models, 0) })
}

// laneBeatModels is the models this process actually sends to, deduplicated and
// without the empties.
//
// IT IS THE SLOTS AND NOT THE CATALOG, for the reason the session's own list
// gives: these are models a run will really open a connection to, while the
// fallback list is what a turn reaches for only when no endpoint would take the
// request at all — and a sheet for every one of those would be paying, on every
// run, for a refusal that usually never comes. The model this seam was called
// with leads, because it is the one the caller resolved and the one the work
// will ride; the two config slots follow for the door that has not resolved one
// yet.
//
// IT IS THE LEDGER'S NAME FOR EACH SLOT AND NOT THE OPERATOR'S. A slot may hold
// a floating alias — the shipped default is one — and the router publishes an
// endpoints page under the concrete model it points at, never under the alias
// ([lanes.LedgerModel]).
func laneBeatModels(settings config.Config, model string) []string {
	var models []string
	seen := make(map[string]bool, 3)
	for _, slot := range []string{model, settings.Model, settings.PlanModel} {
		slot = lanes.LedgerModel(slot)
		if slot == "" || seen[slot] {
			continue
		}
		seen[slot] = true
		models = append(models, slot)
	}
	return models
}
