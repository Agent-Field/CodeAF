package exec

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// turnBilling is one turn of a leaf as the ceiling sees it: what the whole
// prompt weighed, how much of that the provider served from its prefix cache,
// and what came back.
//
// The shape is the loop's own. Every turn re-sends the entire transcript, so a
// turn's prompt is the fixed floor plus a full observation window; everything
// in it except this turn's own additions is byte-identical to the previous
// call, which is precisely the prefix a cache serves warm.
type turnBilling struct {
	prompt     int
	cached     int
	completion int
}

const (
	// freshTurnTokens is what one turn genuinely adds: the assistant message it
	// just wrote and the tool results it just collected. Everything else in the
	// next prompt is a re-send.
	freshTurnTokens = 2_500
	// completionTurnTokens is an ordinary reply plus its reasoning.
	completionTurnTokens = 800
)

// steadyTurn bills one turn of a leaf carrying a full window. warm says the
// provider reported the re-sent prefix as a cache read, which is true from the
// second call of a run onwards wherever prompt caching is actually in play, and
// never true when the plumbing that reports it is dark.
func steadyTurn(windowBytes int, warm bool) turnBilling {
	prompt := observationFixedFloorTokens + windowBytes/observationBytesPerToken
	turn := turnBilling{prompt: prompt, completion: completionTurnTokens}
	if warm {
		turn.cached = prompt - freshTurnTokens
	}
	return turn
}

// warmTurn bills one turn at a stated prefix-cache hit rate, which is how the
// audit measured real runs (92-98% on the nodes that melted) and the one dial
// that decides how far apart the two bounds sit.
func warmTurn(windowBytes, hitPercent int) turnBilling {
	turn := steadyTurn(windowBytes, false)
	turn.cached = turn.prompt * hitPercent / 100
	return turn
}

// turnsAffordable counts the turns a leaf can pay for out of one ceiling, using
// the production accounting rather than a copy of it: the same exhausted() the
// loop tests on every pass, which reads both of the leaf's bounds.
//
// caching says the run's prefix cache is both engaged and reported. The first
// turn is always cold — there is no previous call to have warmed anything.
func turnsAffordable(windowBytes, ceiling int, caching bool) int {
	return turnsUntilExhausted(ceiling, func(turn int) turnBilling {
		return steadyTurn(windowBytes, caching && turn > 0)
	})
}

// turnsUntilExhausted runs the ceiling's own arithmetic forward over a supplied
// billing pattern and reports where the loop would have granted the landing.
func turnsUntilExhausted(ceiling int, bill func(turn int) turnBilling) int {
	outcome := &Outcome{}
	turns := 0
	for turns < 10_000 && !exhausted(outcome, ceiling) {
		turn := bill(turns)
		outcome.Usage.Calls++
		outcome.Usage.PromptTokens += turn.prompt
		outcome.Usage.CachedTokens += turn.cached
		outcome.Usage.CompletionTokens += turn.completion
		turns++
	}
	return turns
}

// The invariant that broke silently, pinned so it cannot break silently again.
//
// abcc5a5 stopped sizing the observation window from the leaf's spend ceiling,
// which was right — a cumulative budget is not a quantity that fits in one
// request — and left the ceiling itself at the value it had been given back
// when a turn cost about 6k prompt tokens. The window went from 25KB to 256KB
// and the wallet did not move, so the same leaf could afford roughly two turns
// where it used to afford a dozen. Measured on one task that meant seven
// extension rounds against trunk's one, 4.5x the tokens and 3.2x the wall.
//
// Nothing in the type system connects those two numbers, and nothing said out
// loud what the connection was. This is that statement: however the window is
// sized, a leaf must still be able to afford at least as many turns as it could
// before the window grew. Turns affordable is what governs whether a leaf
// finishes inside its budget, which is what governs whether the reconciler has
// to buy the rest of the work in extension nodes.
func TestAffordableTurnsDidNotFallWhenTheWindowGrew(t *testing.T) {
	// The harness as it was: the window the spend ceiling used to buy, billed
	// the way the ceiling used to bill — every re-sent token at full weight,
	// because nothing was reading the cached share.
	const legacyWindow = defaultLeafTokens / 6
	before := turnsAffordable(legacyWindow, defaultLeafTokens, false)
	if before < 8 {
		t.Fatalf("the baseline arithmetic is wrong: %d turns, expected the low teens", before)
	}

	// The harness as it is: the window a long-context model is handed, billed
	// with the cached share weighted down to what it actually costs.
	after := turnsAffordable(observationWindow(1<<20), defaultLeafTokens, true)
	if after < before {
		t.Fatalf("growing the observation window cost the leaf turns: %d before, %d after — "+
			"a leaf that cannot finish inside one budget is bought out in extension nodes",
			before, after)
	}
}

// The cap is what makes the window a fixed cost rather than a function of
// whichever model happens to be configured. Every model past the clamp gets the
// same window, so every one of them affords the same number of turns; a run
// that moves from a 600k-context model to a 2M one must not quietly lose its
// ability to finish.
func TestEveryLongContextModelAffordsTheSameTurns(t *testing.T) {
	want := turnsAffordable(observationWindow(600_000), defaultLeafTokens, true)
	for _, context := range []int{1 << 20, 2_000_000, 10_000_000} {
		if got := turnsAffordable(observationWindow(context), defaultLeafTokens, true); got != want {
			t.Fatalf("a %d-token model affords %d turns where a 600k one affords %d", context, got, want)
		}
	}
}

// The wrap-up warning fires at 70% of the ceiling and the landing reserve takes
// the tail, and both were calibrated against a turn that cost about 6k tokens.
// A leaf measured at 152k tokens on its FIRST call is past the warning before
// it has done any work, so it spends its entire existence in landing mode — the
// sharpest form of the same defect. One turn must stay one turn — read through
// budgetUsed, which is what the loop itself now tests, so that adding a second
// bound beside the ceiling cannot push a first turn into landing mode either.
func TestOneTurnDoesNotCrossTheWrapUpThreshold(t *testing.T) {
	outcome := &Outcome{}
	turn := steadyTurn(observationWindow(1<<20), false)
	outcome.Usage.PromptTokens = turn.prompt
	outcome.Usage.CompletionTokens = turn.completion
	if used := budgetUsed(outcome, defaultLeafTokens); used > wrapUpAt {
		t.Fatalf("a single cold turn reads %.2f used of a %d ceiling, past the %.2f wrap-up mark",
			used, defaultLeafTokens, wrapUpAt)
	}
	if exhausted(outcome, defaultLeafTokens) {
		t.Fatal("a single cold turn exhausted the leaf")
	}
}

// The discount is a discount and never a credit. A provider that reports
// nothing is billed exactly as the ceiling always billed, and a provider that
// reports something impossible cannot make a leaf immortal.
func TestTheCachedDiscountOnlyEverDiscounts(t *testing.T) {
	plain := &Outcome{Usage: Usage{PromptTokens: 10_000, CompletionTokens: 1_000}}
	if got := spent(plain); got != 11_000 {
		t.Fatalf("an unreported prefix cache changed the bill: %d, want 11000", got)
	}
	warm := &Outcome{Usage: Usage{PromptTokens: 10_000, CachedTokens: 10_000, CompletionTokens: 1_000}}
	if got := spent(warm); got != 10_000*cachedTokenWeightPercent/100+1_000 {
		t.Fatalf("a fully cached prompt billed %d", got)
	}
	if spent(warm) >= spent(plain) {
		t.Fatal("a cached prompt cost at least as much as a cold one")
	}
	absurd := &Outcome{Usage: Usage{PromptTokens: 10_000, CachedTokens: 900_000, CompletionTokens: 1_000}}
	if got := spent(absurd); got < 1_000 {
		t.Fatalf("an over-reported cache read credited the leaf: %d", got)
	}
}

// warmRunaway is the node that melted: a leaf that never converges on its own,
// whose re-sent prefix the provider serves warm from the second call onwards.
// It is the exact shape of the worst nodes in the audit — no single prompt above
// ~20k tokens, and a bill the cache discount shrinks by an order of magnitude.
type warmRunaway struct {
	window     int
	hitPercent int
	calls      int
}

func (w *warmRunaway) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	w.calls++
	turn := warmTurn(w.window, w.hitPercent)
	if w.calls == 1 {
		turn.cached = 0
	}
	return &ai.Response{
		Choices: []ai.Choice{{
			Message: ai.Message{
				Role:    "assistant",
				Content: []ai.ContentPart{{Type: "text", Text: "still working"}},
				ToolCalls: []ai.ToolCall{call(fmt.Sprintf("c%d", w.calls), "write",
					fmt.Sprintf(`{"path":"out-%d.txt","text":"x"}`, w.calls))},
			},
			FinishReason: "tool_calls",
		}},
		Usage: &ai.Usage{
			PromptTokens:         turn.prompt,
			CompletionTokens:     turn.completion,
			CacheReadInputTokens: turn.cached,
		},
	}, nil
}

// The regression itself, driven through the real loop.
//
// A leaf billing at a 98% hit rate spends about a sixth of what it costs the
// provider to run it, so the cost ceiling alone would let it run until it had
// pushed roughly a million raw tokens through the model — which is what the
// traces show, twice, at 1.4M and 1.6M on single nodes. The raw bound has to be
// what ends it, it has to end it inside the turn backstop rather than at it, and
// it has to end it the graceful way: a landing that delivers what exists.
func TestACacheDiscountedRunawayLandsOnTheRawBound(t *testing.T) {
	client := &warmRunaway{window: observationWindow(1 << 20), hitPercent: 98}
	linear := NewLinear(client, workspace(t), nil, maxTurnBackstop, defaultLeafTokens, time.Hour)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}

	// Landing, not guillotine: the leaf was told to wrap up and delivered.
	if outcome.Stop != StopBudget || outcome.Exhausted != StopBudget {
		t.Fatalf("stop = %s, exhausted = %q — want the budget landing both times",
			outcome.Stop, outcome.Exhausted)
	}
	if outcome.Text == "" {
		t.Fatal("the landing delivered nothing; the raw bound must wrap up, not kill")
	}

	// The raw bound, not the turn cap, is what fired. If the cap is what caught
	// it, the bound is not doing its job and every runaway costs the full 40.
	if outcome.Turns >= maxTurnBackstop {
		t.Fatalf("ran %d turns of a %d backstop — the raw bound did not fire first",
			outcome.Turns, maxTurnBackstop)
	}

	// And the bound held to its own arithmetic, plus the landing reserve it
	// grants after crossing.
	raw := rawSpent(outcome)
	ceiling := rawCeiling(defaultLeafTokens)
	turn := warmTurn(observationWindow(1<<20), 98)
	if slack := (landingTurns + 1) * (turn.prompt + turn.completion); raw > ceiling+slack {
		t.Fatalf("raw spend %d overran the %d bound by more than the %d its landing reserve costs",
			raw, ceiling, slack)
	}

	// The whole point, stated as the comparison the audit made: cost alone would
	// have bought this leaf several times the work it just did.
	costOnly := 0
	usage := &Outcome{}
	for costOnly < 10_000 && spent(usage) < defaultLeafTokens {
		turn := warmTurn(observationWindow(1<<20), 98)
		usage.Usage.PromptTokens += turn.prompt
		usage.Usage.CachedTokens += turn.cached
		usage.Usage.CompletionTokens += turn.completion
		costOnly++
	}
	if costOnly <= outcome.Turns {
		t.Fatalf("the discounted ceiling bound at %d turns and the raw bound at %d — "+
			"this test is not measuring what it claims to", costOnly, outcome.Turns)
	}
}

// The bound must hold across the whole range of hit rates a provider can
// report, not just at the one the loop was measured at. However cheap the turns
// become, the leaf converges inside its raw allowance and inside the backstop —
// and at zero caching nothing changes at all, because cost still binds first.
func TestTheRawBoundHoldsAtEveryHitRate(t *testing.T) {
	window := observationWindow(1 << 20)
	cold := turnsUntilExhausted(defaultLeafTokens, func(int) turnBilling {
		return warmTurn(window, 0)
	})
	for _, hit := range []int{0, 50, 90, 95, 98, 100} {
		turns := turnsUntilExhausted(defaultLeafTokens, func(turn int) turnBilling {
			if turn == 0 {
				return warmTurn(window, 0)
			}
			return warmTurn(window, hit)
		})
		if turns >= maxTurnBackstop {
			t.Fatalf("at a %d%% hit rate a leaf runs %d turns before either bound trips, "+
				"past the %d backstop", hit, turns, maxTurnBackstop)
		}
		if turns < cold {
			t.Fatalf("at a %d%% hit rate a leaf affords %d turns against %d cold — "+
				"the discount must never cost a leaf turns", hit, turns, cold)
		}
	}
}

// The raw bound is a bound on convergence and never on cost: it may not shorten
// a leaf that is spending honestly. A cold leaf sees exactly the ceiling it was
// granted, because three times a number it cannot reach is not a limit.
func TestTheRawBoundNeverBindsBeforeTheCostCeiling(t *testing.T) {
	cold := &Outcome{Usage: Usage{PromptTokens: defaultLeafTokens - 1, CompletionTokens: 0}}
	if exhausted(cold, defaultLeafTokens) {
		t.Fatal("a leaf one token short of its ceiling was called exhausted")
	}
	cold.Usage.CompletionTokens = 1
	if !exhausted(cold, defaultLeafTokens) {
		t.Fatal("a leaf at its cost ceiling was not called exhausted")
	}
	// The same tokens, now reported as cache reads: cheap enough that cost says
	// keep going, plentiful enough that the raw bound says land.
	warm := &Outcome{Usage: Usage{
		PromptTokens:     rawCeiling(defaultLeafTokens),
		CachedTokens:     rawCeiling(defaultLeafTokens),
		CompletionTokens: 0,
	}}
	if spent(warm) >= defaultLeafTokens {
		t.Fatalf("the discount stopped discounting: %d of %d", spent(warm), defaultLeafTokens)
	}
	if !exhausted(warm, defaultLeafTokens) {
		t.Fatalf("a leaf at %d raw tokens against a %d bound was not called exhausted",
			rawSpent(warm), rawCeiling(defaultLeafTokens))
	}
	// And the wrap-up warning reaches it, on the bound it is actually near.
	if budgetUsed(warm, defaultLeafTokens) <= wrapUpAt {
		t.Fatalf("a leaf at its raw bound reads %.2f used, under the %.2f wrap-up mark",
			budgetUsed(warm, defaultLeafTokens), wrapUpAt)
	}
}

// The backstop's value is a calibration, not a taste, so it is pinned here with
// the measurements that set it. It has to sit above every honest leaf the audit
// traced — a well-sized one finishes in 8 to 16 turns, the profile's own spread
// reports identical briefs landing at 9, 16 and 25, and the longest honest leaf
// in the traces ran 25 — and far enough below the old 200 that a warm runaway
// meets it in useful time. Anything outside this band is a change of policy and
// should have to say so here.
func TestTheTurnBackstopIsCalibratedToTheMeasuredSpread(t *testing.T) {
	const longestHonestLeaf = 25
	if maxTurnBackstop <= longestHonestLeaf {
		t.Fatalf("a %d-turn backstop cuts off the %d-turn leaves the audit traced finishing honestly",
			maxTurnBackstop, longestHonestLeaf)
	}
	if maxTurnBackstop < 30 || maxTurnBackstop > 50 {
		t.Fatalf("the backstop is %d, outside the 30-50 the measured spread supports", maxTurnBackstop)
	}
}

// The backstop is a backstop: it is the loop's own, and no caller may raise it.
func TestTheTurnBackstopCannotBeRaisedByACaller(t *testing.T) {
	if got := NewLinear(nil, nil, nil, 200, defaultLeafTokens, time.Minute).maxTurns; got != maxTurnBackstop {
		t.Fatalf("a caller asking for 200 turns got %d, want the %d backstop", got, maxTurnBackstop)
	}
	if got := NewLinear(nil, nil, nil, 0, defaultLeafTokens, time.Minute).maxTurns; got != maxTurnBackstop {
		t.Fatalf("an unset turn count got %d, want the %d backstop", got, maxTurnBackstop)
	}
	if got := NewLinear(nil, nil, nil, 4, defaultLeafTokens, time.Minute).maxTurns; got != 4 {
		t.Fatalf("a caller asking for a tighter 4 turns got %d", got)
	}
}
