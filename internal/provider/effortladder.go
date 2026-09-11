package provider

import (
	"context"
	"math"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── the effort ladder, translated ───────────────────────────────────────────
//
// THIS FILE IS THE ONLY PLACE A RUNG BECOMES A REQUEST FIELD.
//
// Above the adapter, how hard to think is a word on a five-rung ladder and
// nothing else (internal/effort). Here it becomes OpenRouter's unified
// `reasoning` object, which speaks two dialects: an `effort` word, which is what
// OpenAI-family endpoints understand, and a `max_tokens` thinking budget, which
// is what Anthropic- and Gemini-family endpoints understand. IT TAKES EXACTLY
// ONE OF THEM — a body carrying both is a 400 — and translates whichever it was
// given for the endpoint that speaks the other.
//
// "high" is the top of the word dialect. There is nothing above it to SAY, so
// the two rungs above high say it with the budget INSTEAD of the word: an
// allowance the endpoint that reads budgets will spend, and which the router
// turns back into a word for the endpoint that does not. The budget is what
// makes those two rungs different from high at all, so it is the half that
// travels; wire.go's reasoningFor is where that choice is made, and it is made
// in one place.
//
// The word is still carried on the request through this layer, because it is
// what the catalog gate is asked about and what the top rungs fall back to when
// an endpoint refuses a budget outright.

const (
	// xhighReasoningTokens is a deep pass that still leaves the answer room.
	//
	// It is sized against the WORKING WINDOW rather than against the model's
	// published maximum: 32k of thinking is minutes of deliberation on the
	// models this program is pointed at, and it still leaves a 200k window most
	// of its room for the transcript and the reply. That is what makes xhigh a
	// rung somebody can leave switched on rather than one they reach for once.
	xhighReasoningTokens = 32000

	// maxReasoningTokens is the top of the ladder — the rung for the one
	// question worth waiting on.
	//
	// 64k is about where the published budgets stop paying: past it the extra
	// tokens buy restatement rather than reasoning, and a 200k window has spent
	// a third of itself before the answer begins. It is deliberately not the
	// largest number an endpoint would accept. A ceiling nobody can tell apart
	// from the rung below it is a ceiling that only costs money.
	maxReasoningTokens = 64000
)

// WithConfiguredEffortRung carries a rung a PERSON chose — a dialled
// conversation, a task somebody set, the install's own default. It is sent even
// when the catalog cannot vouch for the model, which is the difference
// [WithConfiguredReasoningEffort] draws and the reason it exists: an operator's
// choice is worth one round-trip to discover an endpoint refuses it, and a
// harness's guess is not.
func WithConfiguredEffortRung(ctx context.Context, rung effort.Rung) context.Context {
	return withEffort(ctx, effortRequestFor(rung, true))
}

// WithEffortRung carries a rung the HARNESS chose for one phase — a sentinel's
// yes-or-no, a standing check. The catalog gate drops it on a model that cannot
// be vouched for, so a default depth can never break a run on an unknown model.
func WithEffortRung(ctx context.Context, rung effort.Rung) context.Context {
	return withEffort(ctx, effortRequestFor(rung, false))
}

// ── the wall, told ──────────────────────────────────────────────────────────
//
// A WALL THE MODEL IS NEVER SHOWN IS A STOPWATCH, NOT A CONTRACT. The structuring
// slots bound one completion (internal/provider/pool's call wall), and until
// this door existed nothing on the wire said so: a reasoning model running at
// its published default — the top of its ladder, on GLM 5.3 — thought until
// the wall cut it, and everything it had thought was lost with the cut. The
// same prompt on another machine answered in two seconds; which side of the
// wall a call landed on was luck (issue #927).
//
// So a walled request carries the wall to this layer, and the thinking pass is
// given the allowance the wall implies, stated in the ladder's own budget
// dialect. It is not a sixth rung and not a second knob: it is the budget half
// of the ladder, DERIVED where the two rungs above high STATE theirs.

// WithThinkingWall carries the wall one completion runs under, so the thinking
// pass in front of its answer is told how long it has ([Client.wallBudget]).
//
// It keeps whatever effort is already on the context and adds only the wall,
// and it is applied by the wall itself — last, on the context the completion is
// actually sent with — because an effort set further in would shadow it.
func WithThinkingWall(ctx context.Context, wall time.Duration) context.Context {
	request := effortFrom(ctx)
	request.wall = wall
	return withEffort(ctx, request)
}

// wallBudget is the thinking allowance a wall implies for this request, and zero
// when the wall has nothing to say.
//
// THE DERIVATION, which is two measured facts and one published one:
//
//	budget = share(the level the pass runs at) × the lane's believed rate × the wall
//
// The rate times the wall is how many tokens the machine this request asked for
// can write before the wall — the wall stated as a ceiling in tokens rather than
// in time. The share is the router's own documented allocation of a ceiling to
// the thinking pass at that level (thinking.go's [thinkingShare]), so the pass is
// given exactly the part of the wall it would have taken of any other ceiling and
// the answer keeps the rest. On a lane believed at 210 tokens a second, under a
// four-minute wall, a model thinking at max is told 47,880 tokens; on one believed
// at 20, it is told 4,560 — the same wall, spent at each machine's own pace.
//
// THE RATE IS THE LANE BELIEF the waiting controller already plans against
// (`internal/lane`'s [lanes.PaceFor], read exactly as waitplan.go reads it): the
// four-level chain that answers for a machine nobody has measured yet from its
// provider and its model, so a first call in a fresh process is sized from what
// is believed rather than from nothing. A lane nothing is believed about, or a
// request that asked for no machine at all, derives no budget — a number nobody
// can derive is not sent, and the wall's own answer ask still keeps the thought
// (pool's [walled]).
//
// IT SPEAKS ONLY FOR A PASS NOTHING ELSE BOUNDED. A word a person or the harness
// chose — low, medium, high, or the disable a model that cannot stop thinking
// turns into its lowest word — is their statement of the depth and stands. What
// the wall bounds is a pass nobody sized: a model that thinks at its own default
// because nothing was sent, or a rung whose own budget is larger than the wall
// can hold. A model that thinks only when asked, and was not asked, has no pass
// to budget and its request is unchanged.
//
// WHY THIS DOES NOT COST AN ANSWER ITS DEPTH: the pass is not given less than
// the wall already allowed a pass that meant to answer. What lies past the
// budget is thinking that would have run into the wall before any answer came,
// and a call cut there kept nothing at all.
func (c *Client) wallBudget(model string, requested effortRequest) int {
	if requested.wall <= 0 || reasoningBudgetRefused(model) {
		return 0
	}
	stated := c.requestedEffort(model, requested)
	if stated == EffortOff || (stated != EffortNone && requested.budget <= 0) {
		return 0
	}
	share := thinkingShare(c.runningEffort(model, stated))
	rate := believedRate(model, requested.lane)
	if share <= 0 || rate <= 0 {
		return 0
	}
	return thinkingRoom(share, rate, requested.wall)
}

// thinkingRoom is the arithmetic on its own: the share of what a machine writing
// at `rate` tokens a second produces in `wall`, to the nearest token.
func thinkingRoom(share, rate float64, wall time.Duration) int {
	return int(math.Round(share * rate * wall.Seconds()))
}

// believedRate is the median output rate, in tokens a second, the lane belief
// holds for the machine this request will be written by, and zero when it holds
// nothing.
//
// The machine the request asked for is asked about itself. A request that asked
// for no machine may be served by any machine the sheet lists for its model, so
// it is sized for the slowest of them the belief holds: the budget then fits the
// wall whichever of them the router picks.
func believedRate(model, lane string) float64 {
	now := waitNow()
	if lane != "" {
		return rateOf(lanes.ID{Model: model, Lane: lane}, now)
	}
	slowest := 0.0
	for _, row := range lanes.Default().Sheet().Rows(model) {
		rate := rateOf(lanes.ID{Model: model, Lane: row.ID.Lane}, now)
		if rate > 0 && (slowest == 0 || rate < slowest) {
			slowest = rate
		}
	}
	return slowest
}

// rateOf is one machine's believed median rate. [lanes.Pace.Gap] is the belief
// about the wait between two tokens, whose log is the rate's negated, so the
// median rate is the exponential of its negated mu.
//
// A MACHINE THIS PROCESS HOLDS NO BELIEF ABOUT IS SIZED BY NOTHING. The chain
// answers for any pair at all — for one never seen and never read off a sheet it
// answers with the world's pace, which is the controller's honest prior about
// how long to wait and no fact about how fast THIS machine thinks. Measured on a
// fresh registry it is about twenty-five tokens a second, and a budget read off
// it would tell a model on a two-hundred-a-second machine an eighth of its wall.
// So the ledger is asked first whether it holds this pair at all — seen in an
// answer or primed from the sheet — and a pair it does not hold derives no rate.
func rateOf(id lanes.ID, now time.Time) float64 {
	if _, held := lanes.Default().Ledger().Belief(id); !held {
		return 0
	}
	pace := lanes.PaceFor(id, now)
	if !pace.Gap.Known() {
		return 0
	}
	return math.Exp(-pace.Gap.Mu)
}

// effortRequestFor is the mapping itself, and it is a total function over the
// ladder: an unknown rung asks for nothing rather than guessing, because a rung
// this adapter has not been taught is a rung whose wire shape nobody decided.
func effortRequestFor(rung effort.Rung, explicit bool) effortRequest {
	switch rung {
	case effort.Low:
		return effortRequest{effort: EffortLow, explicit: explicit}
	case effort.Medium:
		return effortRequest{effort: EffortMedium, explicit: explicit}
	case effort.High:
		return effortRequest{effort: EffortHigh, explicit: explicit}
	case effort.XHigh:
		return effortRequest{effort: EffortHigh, budget: xhighReasoningTokens, explicit: explicit}
	case effort.Max:
		return effortRequest{effort: EffortHigh, budget: maxReasoningTokens, explicit: explicit}
	default:
		return effortRequest{explicit: explicit}
	}
}
