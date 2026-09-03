package provider

import (
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ceilingRaceChoice is the measured five-lane frontier: the first-party lane
// is the only one under list × 1.25, and more alternatives remain than any one
// race may send.
func ceilingRaceChoice(model string) lanes.Choice {
	names := []string{"DeepSeek", "Fireworks", "Together", "Nebius", "Novita"}
	choice := lanes.Choice{Order: append([]string(nil), names...)}
	for _, lane := range names {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: 2, Rate: 2000, Price: 0.01,
		})
	}
	return choice
}

// ceilingRaceLanes stages the live price split. DeepSeek is published on the
// sheet but excluded from completions by the account, while every reseller is
// twice list price and therefore disappears only while the ceiling is present.
func ceilingRaceLanes(excludeFirstRescue bool) []lanestub.Lane {
	lanesOffered := []lanestub.Lane{
		{Name: "DeepSeek", SheetOnly: true, Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 11,
			PriceIn: 0.66e-6, PriceOut: 1.98e-6,
		}},
		{Name: "Fireworks", SheetOnly: excludeFirstRescue, Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 17,
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
		{Name: "Together", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 19,
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
		{Name: "Nebius", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 23,
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
		{Name: "Novita", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 29,
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
	}
	return lanesOffered
}

func TestACeilingRefusalTeachesBeforeTheRaceWalksToARescue(t *testing.T) {
	rig := newPricedLaneRig(t, "ceiling/walk", ceilingRaceLanes(false)...)
	SetHedgeBudget(lanes.NewBudget(6, 0))
	ctx := WithLaneChoice(talking(), ceilingRaceChoice(rig.model))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the rescue did not answer after the ceiling refusal: %v", err)
	}
	if got := answerTokens(response); got != 17 {
		t.Fatalf("the answer is %d tokens, want Fireworks' 17", got)
	}
	asks := rig.server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests went out, want the capped primary and one rescue", len(asks))
	}
	if asks[0].MaxPrice == nil {
		t.Fatal("the refused primary carried no ceiling, so this test proves nothing")
	}
	if !demandedOnly(asks[1], "Fireworks") {
		t.Fatalf("the rescue demanded %v, want Fireworks", asks[1].Only)
	}
	if asks[1].MaxPrice != nil {
		t.Fatalf("the first rescue repeated the refused ceiling: %+v", asks[1].MaxPrice)
	}

	// THE MEMO SURVIVES THE RACE. The walk kept the primary out of the ladder,
	// so a later ordinary request proves the refusal itself taught the ledger.
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatalf("the next request to the model failed: %v", err)
	}
	asks = rig.server.Asks()
	if got := len(asks); got != 3 {
		t.Fatalf("the next turn made %d requests, want one", got-2)
	}
	if asks[2].MaxPrice != nil {
		t.Fatalf("the next turn repeated the ceiling the router refused: %+v", asks[2].MaxPrice)
	}
}

func TestARefusedCeilingClimbsTheLadderWhenThePurseFundsNoWalk(t *testing.T) {
	rig := newPricedLaneRig(t, "ceiling/empty-purse", ceilingRaceLanes(false)...)
	SetHedgeBudget(lanes.NewBudget(0, 0))
	var notices []string
	ctx := noticeContext(WithLaneChoice(talking(), ceilingRaceChoice(rig.model)), &notices)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the relaxed retry did not answer: %v", err)
	}
	if got := answerTokens(response); got != 17 {
		t.Fatalf("the answer is %d tokens, want Fireworks' 17", got)
	}
	asks := rig.server.Asks()
	if len(asks) != 2 {
		t.Fatalf("%d requests went out, want the refused primary and its relaxed retry", len(asks))
	}
	if asks[0].MaxPrice == nil || asks[1].MaxPrice != nil {
		t.Fatalf("ceilings were first=%+v retry=%+v, want present then absent", asks[0].MaxPrice, asks[1].MaxPrice)
	}
	for _, ask := range asks {
		if ask.Model != rig.model {
			t.Fatalf("asked model %q, want the one the person chose (%q)", ask.Model, rig.model)
		}
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "dropped the price ceiling and relaxed the endpoint filter") {
		t.Fatalf("notices = %#v, want the price-ceiling retry line", notices)
	}
}

func TestARefusedRescueClimbsTheLadderWhenThePurseFundsNoSecondWalk(t *testing.T) {
	rig := newPricedLaneRig(t, "ceiling/one-walk", ceilingRaceLanes(true)...)
	SetHedgeBudget(lanes.NewBudget(1, 0))
	ctx := WithLaneChoice(talking(), ceilingRaceChoice(rig.model))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the relaxed rescue did not answer: %v", err)
	}
	if got := answerTokens(response); got != 19 {
		t.Fatalf("the answer is %d tokens, want Together's 19", got)
	}
	asks := rig.server.Asks()
	if len(asks) != 3 {
		t.Fatalf("%d requests went out, want the capped primary, one walk, and one relaxed retry", len(asks))
	}
	if asks[0].MaxPrice == nil {
		t.Fatal("the refused primary carried no ceiling, so this test proves nothing")
	}
	if !demandedOnly(asks[1], "Fireworks") || asks[1].MaxPrice != nil {
		t.Fatalf("the walk carried only=%v max_price=%+v, want an uncapped Fireworks demand", asks[1].Only, asks[1].MaxPrice)
	}
	if len(asks[2].Only) != 0 || asks[2].MaxPrice != nil {
		t.Fatalf("the relaxed retry carried only=%v max_price=%+v", asks[2].Only, asks[2].MaxPrice)
	}
	for _, ask := range asks {
		if ask.Model != rig.model {
			t.Fatalf("asked model %q, want the one the person chose (%q)", ask.Model, rig.model)
		}
	}
}
