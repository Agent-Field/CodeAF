package provider

import (
	"context"
	"strings"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── WHAT ONE CALL IS WATCHED WITH ───────────────────────────────────────────
//
// A plan is not a decision. It is what is BELIEVED about the machine expected
// to serve, what the role will wait, where acting could go and what this call may
// spend rescuing itself — assembled here because this is the only place all four are known
// at once, and handed to `internal/lane/control`, which is the only thing in
// this build that decides when a wait has gone on long enough.

// ── THE PURSE IS THE CALL'S OWN, AND THE PROCESS HAS NONE ───────────────────
//
// There was a package-wide budget here until 2026-09-11 — two rescues in any
// twenty requests and a tenth of the last hour's bill, shared by every client —
// and it is deleted. A window cannot tell the request that needs a rescue from
// the nineteen that do not, so it refused by arrival order and overruled the one
// thing in this build that decides when a wait has gone on long enough
// ([internal/lane]'s hedge.go carries the measurement). What a call may spend
// rescuing itself is its own patience converted through λ
// ([control.Plan.SpendUSD]), and it is built with the rest of the plan.

// ── WHAT ONE CALL IS WATCHED WITH ───────────────────────────────────────────

// planFor is everything the controller is built with, for THIS call.
//
// It is assembled here because this is the only place all of it is known at
// once: the role, from the context; the belief, from the ledger; the lanes
// worth going to, from the choice; and what it may spend rescuing itself, from
// the plan the role already carries. None of it is a decision — the plan says
// what is believed and what is allowed, and `internal/lane/control` is the only
// thing that decides.
func (c *Client) planFor(ctx context.Context, choice lanes.Choice, model string, expected int) control.Plan {
	now := waitNow()
	// THE PLAN IS `internal/lane`'s ARITHMETIC AND IT IS BUILT THERE: the head
	// of the choice, what is believed about it, the role's ceiling and λ, and
	// the alternatives the frontier named. This layer adds only the four things
	// it is the only one to know.
	plan := lanes.PlanFor(choice, lanes.PaceFor(lanes.ID{Model: model, Lane: lanes.HeadOf(choice)}, now), RoleFrom(ctx), now)
	// ── and then the four things only the transport knows
	//
	// A PIN IS WHAT THE CHOICE SAYS IT IS, AND NEVER THE LENGTH OF `Only`: the
	// person named the machine, so the act is a question rather than a rescue
	// (offer.go). This line counted `Only` until 2026-09-11, which was the same
	// sentence for exactly as long as a demand and a pin were the same field.
	// Once the chooser began demanding the set it admitted ([lane.demandOf]),
	// counting would have called ordinary traffic pinned — the hazard's `Ask`
	// road instead of a hedge, `switch to auto?` about a machine nobody chose,
	// and no rescue sent at all. A pin is a person's word and a demand is our
	// own admitted set; [lane.Choice.Pinned] carries the first and `Only` the
	// second. How long the answer will be is the caller's own hint, and it is
	// what makes the commitment half of the inequality computable. And how long
	// this model deliberates is a fact about the model and the rung it was asked
	// at, which is a knob only this layer resolves. THE PURSE IS NOT AMONG THEM
	// ANY MORE: it is the plan's own budget and `lane.PlanFor` fills it in,
	// which is why nothing here names money.
	plan.Pinned = choice.Pinned
	plan.Expected = expected
	// AND A PERSON MAY TURN RESCUING OFF OUTRIGHT (lanepin.go's [SetLaneGuard]).
	// It is the one thing about a plan that is neither a belief nor a role, so it
	// is written where every arm of the question reads it: a purse that refuses
	// everything, which the race's one affordability door already asks.
	if !LaneGuardOn() {
		plan.Purse = lanes.NoSpending()
	}
	plan.Think = lanes.Thinks(model, c.recordedEffort(model, knobsFrom(ctx)), now)
	// AND WHERE A SECOND REQUEST COULD NOT BE DEMANDED THERE ARE NO
	// ALTERNATIVES — an endpoint that is not a router, or a person who asked
	// for no steering. An empty list is a real state rather than a missing one:
	// the act is then [control.Report], which is the only honest thing to say
	// when there is nowhere better to go, and the ceiling still applies.
	// A DECISION SITE (#433). An alternative is a machine a SECOND REQUEST
	// would demand with `provider.only`, so "could not be demanded" is the
	// base's own answer about carrying a preference and not its hostname.
	//
	// AND UNDER `simple` WITH NO PIN THERE IS NOWHERE TO GO EITHER. The row's
	// whole promise is that nothing on this side names a machine, and a rescue
	// is exactly that — a second request demanding a lane this process chose
	// off the sheet, sent while the first is still streaming. So the frontier
	// is empty and a stall is reported rather than rescued. With a pin the
	// alternatives stay, because there the act is the offer — `switch to
	// auto?` — and a question has to name where a `y` would go (offer.go).
	if !c.carriesPreferences() || c.routing() == RoutingOff || (c.routing() == RoutingSimple && !choice.Pinned) {
		plan.Alts = nil
	} else if len(plan.Alts) == 0 {
		// THE CHOOSER RETURNS THE ZERO CHOICE WHEN IT HOLDS FEWER THAN TWO
		// BELIEFS. That is right about ranking and wrong about waiting: a
		// later turn on a model this process has barely measured still has
		// a sheet of other machines, and leaving them off is how a stall
		// sat at "all lanes slow" for 129s with arms:None (F33). Routing
		// and waiting are two questions; an empty Choice is not an empty
		// frontier.
		plan.Alts = sheetAlts(model, plan.Lane)
	}
	return plan
}

// sheetAlts is where a rescue can go when the chooser named nothing. First
// is left unknown on purpose: a sheet row is a prior, not a measurement,
// and the ceiling still acts without one.
func sheetAlts(model, head string) []control.Alternative {
	rows := lanes.Default().Sheet().Rows(model)
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(head)): true}
	alts := make([]control.Alternative, 0, len(rows))
	for _, row := range rows {
		lane := strings.TrimSpace(row.ID.Lane)
		key := strings.ToLower(lane)
		if lane == "" || seen[key] {
			continue
		}
		seen[key] = true
		alts = append(alts, control.Alternative{Lane: lane, Rate: row.Ratep50})
	}
	return alts
}

// equalLane compares two lane names the way every other comparison in this
// design does: the wire spells a lane however it likes.
func equalLane(a, b string) bool { return a != "" && strings.EqualFold(a, b) }
