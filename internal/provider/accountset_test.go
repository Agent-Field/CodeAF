package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
)

// ── THE CEILING ONLY AN EXCLUDED MACHINE FITS UNDER ─────────────────────────
//
// The real binary, 2026-09-10, deepseek/deepseek-v4.1-flash: a primary with no
// demand and no veto, only a ranking and `max_price` at 1.25 × DeepSeek's own
// tariff, was refused on every turn — `Filter by Max Price 1 → Filter by
// Guardrails 0` — because DeepSeek was the one machine under the ceiling and
// the account's paid-training switch excludes it. The walk rescued every turn,
// so nothing failed where a person could see it, and nothing was learned.
//
// These lanes are that model's measured tariffs (per token, as the sheet
// publishes them) and the list price is DeepSeek's, which is what puts the
// ceiling at 0.1875 / 0.75.

const (
	firstPartyIn  = 0.15e-6
	firstPartyOut = 0.60e-6
)

// cheapestExcludedLanes is the live split: the first-party machine is the only
// one under list × 1.25 and the account excludes it; the machines the router
// would otherwise choose are each over the ceiling on one of the two prices.
func cheapestExcludedLanes(extra ...lanestub.Lane) []lanestub.Lane {
	offered := []lanestub.Lane{
		{Name: "DeepSeek", AccountExcluded: true, Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 7, Tools: true,
			PriceIn: firstPartyIn, PriceOut: firstPartyOut,
		}},
		{Name: "DeepInfra", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 17, Tools: true,
			PriceIn: 0.20e-6, PriceOut: 0.60e-6,
		}},
		{Name: "Io Net", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 19, Tools: true,
			PriceIn: 0.15e-6, PriceOut: 1.20e-6,
		}},
	}
	return append(offered, extra...)
}

// cheapestExcludedRig is the lane rig at that list price, with the sheet
// fetched from the stub's own endpoints page exactly as the beat fetches it.
func cheapestExcludedRig(t *testing.T, name string, offered ...lanestub.Lane) *laneRig {
	t.Helper()
	rig := newLaneRigWithPrice(t, name, func(string) (float64, float64, bool) {
		return firstPartyIn, firstPartyOut, true
	}, offered...)
	if !lanes.WireSheet(rig.server.URL(), "", sheetFetcher{}, false) {
		t.Fatal("the sheet would not take the stub as its base")
	}
	t.Cleanup(func() { lanes.WireSheet("", "", nil, false) })
	if err := lanes.Default().Sheet().Refresh(context.Background(), laneModel(rig.model)); err != nil {
		t.Fatalf("the sheet could not be fetched: %v", err)
	}
	if got := len(lanes.Default().Sheet().Rows(laneModel(rig.model))); got != len(offered) {
		t.Fatalf("the sheet holds %d rows, want the %d the stub publishes", got, len(offered))
	}
	return rig
}

// rankedPastTheFirstParty is the primary as the chooser sent it: a ranking of
// the machines the account can reach, with no demand and no veto, and both on
// its frontier for a walk to go to.
func rankedPastTheFirstParty(model string) lanes.Choice {
	choice := lanes.Choice{Order: []string{"DeepInfra", "Io Net"}}
	for _, lane := range []string{"DeepInfra", "Io Net"} {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: 2, Rate: 2000, Price: 0.01,
		})
	}
	return choice
}

// TestACeilingOnlyAnExcludedMachineFitsUnderIsPaidForOnce is the measured
// failure end to end: the first turn pays the one refusal that teaches, and the
// second goes out without the ceiling and is answered by the first request.
func TestACeilingOnlyAnExcludedMachineFitsUnderIsPaidForOnce(t *testing.T) {
	rig := cheapestExcludedRig(t, "accountset-paid-once", cheapestExcludedLanes()...)
	ctx := WithLaneChoice(talking(), rankedPastTheFirstParty(rig.model))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the first turn did not answer: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) < 2 || asks[0].MaxPrice == nil || len(asks[0].Only) != 0 {
		t.Fatalf("the first turn's primary was not the measured shape (a ceiling, no demand): %+v", asks)
	}
	if !lanes.AccountExcludes("DeepSeek") {
		t.Fatal("a refusal whose set was exactly DeepSeek, by the router's count and the sheet's, taught nothing")
	}

	first := len(asks)
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatalf("the second turn did not answer: %v", err)
	}
	asks = rig.server.Asks()
	if got := len(asks) - first; got != 1 {
		t.Fatalf("the second turn made %d requests, want one: the ceiling only DeepSeek fits under was sent again", got)
	}
	if asks[first].MaxPrice != nil {
		t.Fatalf("the second turn carried max_price %+v, which admits only a machine the account excludes", asks[first].MaxPrice)
	}
	if got := rig.server.Requests("DeepSeek"); got != 0 {
		t.Fatalf("DeepSeek was asked %d times; it is excluded and must never be demanded", got)
	}
}

// TestAKnownExclusionTakesTheCeilingOffTheFirstRequest is the next process: the
// exclusion is already on file, so not even the first request pays for it.
func TestAKnownExclusionTakesTheCeilingOffTheFirstRequest(t *testing.T) {
	rig := cheapestExcludedRig(t, "accountset-known", cheapestExcludedLanes()...)
	lanes.ExcludeForAccount("DeepSeek", "staged")
	ctx := WithLaneChoice(talking(), rankedPastTheFirstParty(rig.model))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the turn did not answer: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) != 1 {
		t.Fatalf("%d requests went out, want one", len(asks))
	}
	if asks[0].MaxPrice != nil {
		t.Fatalf("the request carried max_price %+v although only an excluded machine fits under it", asks[0].MaxPrice)
	}
}

// TestACeilingAReachableMachineFitsUnderIsKept is the promise the price rung was
// split out to keep (#678): the ceiling comes off only when there is no wider
// set under it to try. A reachable machine at list price is such a set.
func TestACeilingAReachableMachineFitsUnderIsKept(t *testing.T) {
	cheap := lanestub.Lane{Name: "Chutes", Profile: lanestub.Profile{
		TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 23, Tools: true,
		PriceIn: firstPartyIn, PriceOut: firstPartyOut,
	}}
	rig := cheapestExcludedRig(t, "accountset-kept", cheapestExcludedLanes(cheap)...)
	lanes.ExcludeForAccount("DeepSeek", "staged")
	ctx := WithLaneChoice(talking(), rankedPastTheFirstParty(rig.model))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the turn did not answer: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 || asks[0].MaxPrice == nil {
		t.Fatal("the ceiling was dropped although Chutes, which the account reaches, fits under it")
	}
}

// TestARefusalTheSheetDisagreesWithTeachesNothing is the guard on the lesson:
// when the sheet prices two machines under the ceiling and the router says its
// set held one, the two are not the same set, and nobody is excluded on a guess.
func TestARefusalTheSheetDisagreesWithTeachesNothing(t *testing.T) {
	phantom := lanestub.Lane{Name: "Chutes", SheetOnly: true, Profile: lanestub.Profile{
		TTFT: 2 * time.Millisecond, Rate: 2000, Tools: true,
		PriceIn: firstPartyIn, PriceOut: firstPartyOut,
	}}
	rig := cheapestExcludedRig(t, "accountset-disagree", cheapestExcludedLanes(phantom)...)
	ctx := WithLaneChoice(talking(), rankedPastTheFirstParty(rig.model))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the turn did not answer: %v", err)
	}
	if lanes.AccountExcludes("DeepSeek") || lanes.AccountExcludes("Chutes") {
		t.Fatal("a refusal counted one machine and the sheet priced two under the ceiling, and something was excluded anyway")
	}
}
