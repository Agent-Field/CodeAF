package provider

import (
	"context"
	"strings"
	"sync"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── WHAT ONE CALL IS WATCHED WITH ───────────────────────────────────────────
//
// A plan is not a decision. It is what is BELIEVED about the machine expected
// to serve, what the role will wait, where acting could go and what the process
// can afford — assembled here because this is the only place all four are known
// at once, and handed to `internal/lane/control`, which is the only thing in
// this build that decides when a wait has gone on long enough.

// ── THE PURSE THIS PROCESS ACTS UNDER ───────────────────────────────────────

// hedgeBudget is the one budget the whole process acts under.
//
// It is a package variable rather than a field on the Client because the limit
// it holds is about a SESSION and not about an adapter: two clients — the chat
// and a tool loop's own — must not each be allowed six rescues a minute and a
// tenth of the bill.
var (
	hedgeBudgetMu sync.RWMutex
	hedgeBudget   = lanes.DefaultBudget()
)

// currentHedgeBudget is the budget in force.
func currentHedgeBudget() *lanes.Budget {
	hedgeBudgetMu.RLock()
	defer hedgeBudgetMu.RUnlock()
	return hedgeBudget
}

// SetHedgeBudget replaces the process's budget. A nil argument restores the
// default rather than leaving a hole; a budget of zero rescues is how the
// second request is switched off outright.
func SetHedgeBudget(budget *lanes.Budget) {
	hedgeBudgetMu.Lock()
	defer hedgeBudgetMu.Unlock()
	if budget == nil {
		budget = lanes.DefaultBudget()
	}
	hedgeBudget = budget
}

// ── WHAT ONE CALL IS WATCHED WITH ───────────────────────────────────────────

// planFor is everything the controller is built with, for THIS call.
//
// It is assembled here because this is the only place all of it is known at
// once: the role, from the context; the belief, from the ledger; the lanes
// worth going to, from the choice; and the purse, from the process. None of it
// is a decision — the plan says what is believed and what is allowed, and
// `internal/lane/control` is the only thing that decides.
func (c *Client) planFor(ctx context.Context, choice lanes.Choice, model string, expected int) control.Plan {
	now := waitNow()
	// THE PLAN IS `internal/lane`'s ARITHMETIC AND IT IS BUILT THERE: the head
	// of the choice, what is believed about it, the role's ceiling and λ, and
	// the alternatives the frontier named. This layer adds only the four things
	// it is the only one to know.
	plan := lanes.PlanFor(choice, lanes.PaceFor(lanes.ID{Model: model, Lane: lanes.HeadOf(choice)}, now), RoleFrom(ctx), now)
	// ── and then the four things only the transport knows
	//
	// A STRICT PIN IS WHAT `Only` MEANS: the person named the machine, so the
	// act is a question rather than a rescue (offer.go). The purse is the
	// process's. How long the answer will be is the caller's own hint, and it
	// is what makes the commitment half of the inequality computable. And how
	// long this model deliberates is a fact about the model and the rung it was
	// asked at, which is a knob only this layer resolves.
	plan.Pinned = len(choice.Only) > 0
	plan.Purse = lanes.Spending(currentHedgeBudget())
	plan.Expected = expected
	plan.Think = lanes.Thinks(model, c.recordedEffort(model, knobsFrom(ctx)), now)
	// AND WHERE A SECOND REQUEST COULD NOT BE DEMANDED THERE ARE NO
	// ALTERNATIVES — an endpoint that is not a router, or a person who asked
	// for no steering. An empty list is a real state rather than a missing one:
	// the act is then [control.Report], which is the only honest thing to say
	// when there is nowhere better to go, and the ceiling still applies.
	// A DECISION SITE (#433). An alternative is a machine a SECOND REQUEST
	// would demand with `provider.only`, so "could not be demanded" is the
	// base's own answer about carrying a preference and not its hostname.
	if !c.carriesPreferences() || c.routing() == RoutingOff {
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
