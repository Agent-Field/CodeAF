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

// calibratedWindow is the observation window the default grant is actually
// calibrated against: the one a leaf gets when nothing could name its model.
//
// The tests below used to spell it observationWindow(1<<20), and that was the
// same number by accident. Every long-context model was clamped to 64KB, so
// naming a frontier model and naming none picked windows within a factor of two
// of each other and it did not matter which was written down. The clamp is gone
// — see TestTheObservationWindowHasNoAbsoluteCeiling — and the two are now
// orders of magnitude apart, so these tests have to say which they mean. They
// mean this one: they are tests about the default grant, and a window sized for
// a million tokens is a question about a grant sized for a million tokens.
func calibratedWindow() int { return observationWindow(0) }

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

	// The harness as it is: the window the default grant is calibrated against,
	// billed with the cached share weighted down to what it actually costs.
	after := turnsAffordable(calibratedWindow(), defaultLeafTokens, true)
	if after < before {
		t.Fatalf("growing the observation window cost the leaf turns: %d before, %d after — "+
			"a leaf that cannot finish inside one budget is bought out in extension nodes",
			before, after)
	}
}

// The other half of the same statement, and the one the deleted ceiling used to
// hide: window and grant are one decision made twice.
//
// The 64KB clamp made the window a fixed cost, so a run could move from a 600k
// model to a 2M one and afford exactly the same turns. That looked like safety
// and was a category error wearing safety's clothes — it capped MEMORY to
// protect SPEND, and every model above about 600k tokens was handed the same
// sixty-four kilobytes whatever the catalog said about it. The window is now a
// share of the model's own context, which means the grant beside it has to be
// sized for the same model, and nothing inside exec can do that: the grant is
// the caller's, arriving through NewLinear.
//
// So what is pinned is the coupling, as a number a reader can act on. A leaf on
// a much larger model needs a proportionally larger grant to afford the turns
// the default one buys, and if that multiple ever runs away the sizing has
// stopped being proportional and this fails.
func TestALargerWindowCostsAProportionallyLargerGrant(t *testing.T) {
	want := turnsAffordable(calibratedWindow(), defaultLeafTokens, true)

	for _, context := range []int{200_000, 1 << 20} {
		window := observationWindow(context)
		// The grant that buys the same number of turns at this window. Doubling
		// rather than solving: what is being asserted is the order of magnitude,
		// and the exact figure is the caller's to pick anyway.
		grant := defaultLeafTokens
		for grant < 4096*defaultLeafTokens && turnsAffordable(window, grant, true) < want {
			grant *= 2
		}
		multiple := grant / defaultLeafTokens
		if turnsAffordable(window, grant, true) < want {
			t.Fatalf("a %d-token model's %d-byte window never affords %d turns", context, window, want)
		}
		// The window grew by this much, so the grant may grow by about this much
		// and no more. Slack of 2x covers the doubling itself.
		if ratio := window / calibratedWindow(); multiple > 2*ratio+2 {
			t.Errorf("a %d-token model's window is %dx the calibrated one but needs %dx the grant — "+
				"the sizing has stopped being proportional", context, ratio, multiple)
		}
		t.Logf("a %d-token model: %d-byte window, %dx the default grant to afford %d turns",
			context, window, multiple, want)
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
	turn := steadyTurn(calibratedWindow(), false)
	outcome.Usage.PromptTokens = turn.prompt
	outcome.Usage.CompletionTokens = turn.completion
	if used := budgetUsed(outcome, defaultLeafTokens, 0); used > wrapUpAt {
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
// provider to run it, so it can push a great many raw tokens through the model
// before its grant is gone — which is what the traces show, twice, at 1.4M and
// 1.6M on single nodes.
//
// THE MONEY IS WHAT ENDS IT, and this is where that claim is checked rather
// than asserted. The raw bound used to be what fired here and is now a warning
// (see meter.go on reuseCeiling for the measurement that demoted both Σ-of-
// prompt bounds). What must still hold is everything the audit actually cared
// about: the leaf ends well inside the turn backstop rather than at it, it ends
// far short of the 1.4M the traces show, it ends the graceful way with a
// landing that delivers what exists — and it ends having spent the grant, which
// is the only honest reason to stop a leaf that is being billed for every turn
// it takes.
func TestACacheDiscountedRunawayLandsOnItsMoney(t *testing.T) {
	client := &warmRunaway{window: calibratedWindow(), hitPercent: 98}
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

	// The grant, not the turn cap, is what fired. If the cap is what caught it,
	// the meter is not doing its job and every runaway costs the full backstop.
	if outcome.Turns >= maxTurnBackstop {
		t.Fatalf("ran %d turns of a %d backstop — the grant did not fire first",
			outcome.Turns, maxTurnBackstop)
	}

	// And the grant held to its own arithmetic, plus the landing reserve it
	// gives after crossing.
	turn := warmTurn(calibratedWindow(), 98)
	slack := (landingTurns + 1) * spentOfTurn(turn)
	if spent(outcome) > defaultLeafTokens+slack {
		t.Fatalf("billed spend %d overran the %d grant by more than the %d its landing reserve costs",
			spent(outcome), defaultLeafTokens, slack)
	}

	// AND IT ENDED FAR SHORT OF THE AUDIT'S OWN CASES. The traces this test was
	// written from show single nodes at 1.4M and 1.6M raw prompt tokens; a warm
	// runaway that is billed for every turn runs out of money well before it
	// reaches either, which is what makes the Σ-of-prompt ceiling that used to
	// fire here redundant as well as blind.
	if raw := rawSpent(outcome); raw >= 1_400_000 {
		t.Fatalf("the runaway pushed %d raw tokens, reaching the audited 1.4M the grant is supposed to forestall", raw)
	}

	// The whole point, stated as the comparison the audit made: cost alone would
	// have bought this leaf several times the work it just did.
	costOnly := 0
	usage := &Outcome{}
	for costOnly < 10_000 && spent(usage) < defaultLeafTokens {
		turn := warmTurn(calibratedWindow(), 98)
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
	window := calibratedWindow()
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
//
// And the raw bound no longer LANDS anything: it reaches the leaf as the
// wrap-up warning, which is what a bound that cannot tell hard work from
// circling is worth. The measurement that demoted it is in meter.go against
// reuseCeiling, and the two are the same quantity.
func TestTheRawBoundWarnsAndOnlyTheCostCeilingLands(t *testing.T) {
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
	// AND IT IS NOT LANDED FOR IT. The raw ceiling was a stop until ink s9 of
	// 2026-08-29 showed what a Σ-of-prompt bound actually measures — turns ×
	// mean-context, which climbs identically for a leaf doing hard work and one
	// circling. It is a warning now. See PERF.md, "A leaf's bounds".
	if exhausted(warm, defaultLeafTokens) {
		t.Fatalf("a leaf inside its %d-token grant was landed at %d raw tokens; the money is the only meter that lands work",
			defaultLeafTokens, rawSpent(warm))
	}
	// And the wrap-up warning reaches it, on the bound it is actually near.
	if budgetUsed(warm, defaultLeafTokens, 0) <= wrapUpAt {
		t.Fatalf("a leaf at its raw bound reads %.2f used, under the %.2f wrap-up mark",
			budgetUsed(warm, defaultLeafTokens, 0), wrapUpAt)
	}
}

// The backstop's value is a policy, and this test is where the policy has to
// say so. It was 40, calibrated to the measured spread of well-sized leaves
// (8-16 turns, longest honest leaf 25) — and it also stopped honest complex
// work, which is the worse failure: the token budget is the meter that binds,
// and a turn count exists only for the runaway tokens cannot see (turns gone
// cheap on cache). The 2026-08-12 product decision: complex tasks are never
// stopped by an iteration counter. Four hundred sits an order of magnitude
// past the longest honest leaf while still ending a loop stuck on rails.
func TestTheTurnBackstopIsCalibratedToTheMeasuredSpread(t *testing.T) {
	const longestHonestLeaf = 25
	if maxTurnBackstop <= longestHonestLeaf*4 {
		t.Fatalf("a %d-turn backstop crowds the %d-turn leaves the audit traced finishing honestly",
			maxTurnBackstop, longestHonestLeaf)
	}
	if maxTurnBackstop < 300 || maxTurnBackstop > 500 {
		t.Fatalf("the backstop is %d, outside the 300-500 the policy set; changing it is a policy change and says so here", maxTurnBackstop)
	}
}

// The backstop is a backstop: it is the loop's own, and no caller may raise it.
func TestTheTurnBackstopCannotBeRaisedByACaller(t *testing.T) {
	if got := NewLinear(nil, nil, nil, 1000, defaultLeafTokens, time.Minute).maxTurns; got != maxTurnBackstop {
		t.Fatalf("a caller asking for 1000 turns got %d, want the %d backstop", got, maxTurnBackstop)
	}
	if got := NewLinear(nil, nil, nil, 0, defaultLeafTokens, time.Minute).maxTurns; got != maxTurnBackstop {
		t.Fatalf("an unset turn count got %d, want the %d backstop", got, maxTurnBackstop)
	}
	if got := NewLinear(nil, nil, nil, 200, defaultLeafTokens, time.Minute).maxTurns; got != 200 {
		t.Fatalf("a caller asking for 200 turns under the backstop got %d", got)
	}
	if got := NewLinear(nil, nil, nil, 4, defaultLeafTokens, time.Minute).maxTurns; got != 4 {
		t.Fatalf("a caller asking for a tighter 4 turns got %d", got)
	}
}

// spentOfTurn is one warm turn priced the way the grant prices it, so a test
// about the grant's slack is written in the grant's own unit rather than in raw
// tokens that the discount makes incomparable.
func spentOfTurn(turn turnBilling) int {
	cached := turn.cached
	if cached > turn.prompt {
		cached = turn.prompt
	}
	return turn.prompt - cached + cached*cachedTokenWeightPercent/100 + turn.completion
}

// THE METER IS READ AT LAND, NOT AT THE GRANT.
//
// Both live bounds are read when the landing reserve is handed out, and then the
// landing turns run — more model calls, more seconds. Every ⏳ line and every
// stored leaf_exhausted.reached therefore reported the leaf's spend one turn
// before it stopped: recomputed from one run's own usage rows, 172,791 tokens
// were written down as 152,090 and 199,131 as 178,086, an under-report of 12-14%
// on the one figure an autopsy of a runaway leaf is made of.
func TestTheExhaustionMeterIsReadWhenTheLeafActuallyStops(t *testing.T) {
	client := &warmRunaway{window: calibratedWindow(), hitPercent: 98}
	linear := NewLinear(client, workspace(t), nil, maxTurnBackstop, defaultLeafTokens, time.Hour)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Meter.Name != MeterCost {
		t.Fatalf("the leaf was landed by %q, so this test is not measuring what it claims to",
			outcome.Meter.Name)
	}
	if outcome.Meter.Reached != spent(outcome) {
		t.Fatalf("the record says the leaf reached %d and it spent %d — the reading was taken "+
			"when the landing was granted, not when the leaf stopped",
			outcome.Meter.Reached, spent(outcome))
	}
	// And the landing really did cost something, or the two figures would agree
	// whether or not anything was recomputed.
	if landed := spent(outcome) - outcome.Meter.Allowed; landed <= 0 {
		t.Fatalf("the landing reserve billed nothing (%d spent against a %d grant), so the "+
			"two readings cannot differ", spent(outcome), outcome.Meter.Allowed)
	}
}
