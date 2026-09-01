package provider

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

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

// predictiveSpreadFloor is the smallest per-draw variability the waiting
// arithmetic will use, in nats of log-spread.
//
// A BELIEF'S OWN SPREAD IS THE SPREAD OF AN ESTIMATE and it shrinks toward
// nothing as evidence accumulates — a lane whose median is known to the
// millisecond. What a wait is judged against is how variable ONE DRAW is, which
// never shrinks below the lane's own variability: the measured sheet published
// p90 ≈ 3.5 × p50 on its worst lanes, which is a nat. A controller handed the
// estimate's spread would believe a tail impossible and would never act on the
// lane that has one.
const predictiveSpreadFloor = 1.0

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

// purse is [lanes.Budget] answering [control.Purse]: the question asked WITHOUT
// spending.
//
// The two halves are deliberately different calls. The controller asks whether
// an arm is affordable while it is deciding, which is a reading; the race takes
// the allowance at the moment the arm really goes out, which is a decision. A
// controller that reserved would leave allowances held by requests that
// recovered on their own.
type purse struct{ budget *lanes.Budget }

func (p purse) Allows(usd float64, now time.Time) bool {
	return p.budget.Affordable(now, usd)
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
	plan := lanePlan(choice, RoleFrom(ctx), model, now)
	// ── and then the four things only the transport knows
	//
	// A STRICT PIN IS WHAT `Only` MEANS: the person named the machine, so the
	// act is a question rather than a rescue (offer.go). The purse is the
	// process's. How long the answer will be is the caller's own hint, and it
	// is what makes the commitment half of the inequality computable. And how
	// long this model deliberates is a fact about the model and the rung it was
	// asked at, which is a knob only this layer resolves.
	plan.Pinned = len(choice.Only) > 0
	plan.Purse = purse{budget: currentHedgeBudget()}
	plan.Expected = expected
	plan.Think = thinkSurvival(model, c.recordedEffort(model, knobsFrom(ctx)), now)
	// AND WHERE A SECOND REQUEST COULD NOT BE DEMANDED THERE ARE NO
	// ALTERNATIVES — an endpoint that is not a router, or a person who asked
	// for no steering. An empty list is a real state rather than a missing one:
	// the act is then [control.Report], which is the only honest thing to say
	// when there is nowhere better to go, and the ceiling still applies.
	if !c.isOpenRouter() || c.routing() == RoutingOff {
		plan.Alts = nil
	}
	return plan
}

// waitHysteresis is how much better acting has to look before it is done: the
// smallest difference worth changing one's mind over, and what stops a
// controller flapping at the crossing.
const waitHysteresis = 250 * time.Millisecond

// lanePlan is the belief-and-role half of a plan: what is believed about the
// machine expected to serve, what the role will wait, and where acting could
// go.
//
// IT IS `internal/lane`'s OWN ARITHMETIC AND IT BELONGS THERE. The waiting
// wave landed `lane.PlanFor(choice, belief, role, now)` with exactly this shape
// on its own branch; this is the same function against the ledger door this
// branch has, and when the two meet it becomes that call and this goes.
func lanePlan(choice lanes.Choice, role lanes.Role, model string, now time.Time) control.Plan {
	head := headLane(choice)
	wait, gap := laneSurvival(lanes.ID{Model: model, Lane: head}, now)
	return control.Plan{
		Lane:    head,
		Ceiling: role.Ceiling(),
		Floor:   lanes.ActionFloor,
		Lambda:  role.Lambda(),
		Margin:  waitHysteresis.Seconds(),
		First:   wait,
		Gap:     gap,
		Alts:    laneAlternatives(choice, head, model),
		Began:   now,
	}
}

// altsFor is where acting could go: the frontier, best first, minus the lane
// already being asked and the ones the choice ruled out.
//
// IT IS EMPTY WHENEVER A SECOND REQUEST COULD NOT BE DEMANDED — an endpoint
// that is not a router, or a person who asked for no steering — and an empty
// list is a real state rather than a missing one: the act is then [control.Report],
// which is the only honest thing to say when there is nowhere better to go.
func laneAlternatives(choice lanes.Choice, head, model string) []control.Alternative {
	numbers := make(map[string]lanes.Scored, len(choice.Frontier))
	for _, scored := range choice.Frontier {
		numbers[strings.ToLower(scored.ID.Lane)] = scored
	}
	seen := map[string]bool{strings.ToLower(head): true}
	alts := make([]control.Alternative, 0, len(choice.Frontier))
	add := func(lane string) {
		key := strings.ToLower(strings.TrimSpace(lane))
		if key == "" || seen[key] || namesEndpoint(choice.Ignore, lane) {
			return
		}
		seen[key] = true
		scored := numbers[key]
		alts = append(alts, control.Alternative{
			Lane:  lane,
			First: expectedIn(scored.TTFT / 1000),
			Rate:  scored.Rate,
			Extra: scored.Price,
		})
	}
	// THE ORDER IS A RANKING AND THE FRONTIER IS A SET. The ranking wins where
	// there is one, and the rest of the frontier follows it, so the machine the
	// chooser would have asked next is the machine a rescue goes to.
	for _, lane := range choice.Order {
		add(lane)
	}
	for _, scored := range choice.Frontier {
		add(scored.ID.Lane)
	}
	return alts
}

// expectedIn is a point estimate of an interval, in seconds, as the
// distribution whose MEAN is exactly that. The frontier scores lanes with
// medians, and a median read as a mean would price every alternative as cheaper
// than it is.
func expectedIn(seconds float64) control.Survival {
	if seconds <= 0 {
		return control.Survival{}
	}
	return control.Survival{
		Mu:    math.Log(seconds) - predictiveSpreadFloor*predictiveSpreadFloor/2,
		Sigma: predictiveSpreadFloor,
	}
}

// equalLane compares two lane names the way every other comparison in this
// design does: the wire spells a lane however it likes.
func equalLane(a, b string) bool { return a != "" && strings.EqualFold(a, b) }

// laneSurvival is what is believed about one lane, as the two distributions a
// wait is judged against: how long its first word takes, and how long a gap
// between two visible ones may be. Both are in SECONDS.
//
// IT READS THE HIERARCHY WHERE THERE IS ONE. A four-level chain answers for a
// pair nobody has measured — the world's pace, plus the provider's own offset,
// plus the model's — which is the whole of cold start; a flat belief answers
// only for a pair it has seen. The ledger is one object that may be both, so
// this asks for the better door and falls back to the plainer one.
func laneSurvival(id lanes.ID, now time.Time) (wait, gap control.Survival) {
	if id.Lane == "" || id.Model == "" {
		return control.Survival{}, control.Survival{}
	}
	ledger := lanes.Default().Ledger()
	if chains, ok := ledger.(lanes.Hierarchy); ok {
		wait = chains.Wait(id, now).Survival(predictiveSpreadFloor, 1000)
		gap = rateToGap(chains.Rate(id, now).Survival(predictiveSpreadFloor, 1))
		if wait.Known() || gap.Known() {
			return wait, gap
		}
	}
	belief, ok := ledger.Belief(id)
	if !ok {
		return control.Survival{}, control.Survival{}
	}
	if belief.TTFT.Known() {
		wait = control.Survival{Mu: belief.TTFT.X - math.Log(1000), Sigma: spread(belief.TTFT.P)}
	}
	if belief.Rate.Known() {
		gap = control.Survival{Mu: -belief.Rate.X, Sigma: spread(belief.Rate.P)}
	}
	return wait, gap
}

// thinkSurvival is how long this model's whole thinking phase is expected to
// last, in seconds, and it is keyed on the model and the effort rung rather
// than on the lane: a lane cannot make a model think less, it can only make the
// same thought arrive faster, which the rate belief already says.
func thinkSurvival(model, rung string, now time.Time) control.Survival {
	chains, ok := lanes.Default().Ledger().(lanes.Hierarchy)
	if !ok || model == "" {
		return control.Survival{}
	}
	return chains.Think(model, rung, now).Survival(predictiveSpreadFloor, 1)
}

// rateToGap turns a belief about tokens a second into one about the seconds
// between two tokens. The log of a reciprocal is the negated log, and the
// spread is unchanged, which is the whole conversion.
func rateToGap(rate control.Survival) control.Survival {
	if !rate.Known() {
		return control.Survival{}
	}
	return control.Survival{Mu: -rate.Mu, Sigma: rate.Sigma}
}

// spread is a belief's predictive spread: its own, floored at what one draw
// really varies by. See [predictiveSpreadFloor].
func spread(variance float64) float64 {
	if sigma := math.Sqrt(variance); sigma > predictiveSpreadFloor {
		return sigma
	}
	return predictiveSpreadFloor
}
