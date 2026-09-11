package lane

import (
	"math"
	"reflect"
	"testing"
)

// ── AN ANSWER WHOSE LENGTH NOBODY HAS MEASURED ──────────────────────────────
//
// A fresh conversation has no completed answer from which to estimate the
// next one. That absence is honest, but it is not a zero-length answer: pricing
// only the prompt makes three lanes with hundredfold output tariffs look
// equally cheap and hands the first turn to whichever says its first word
// soonest. These tests hold the comparison at the ordinary talk shape while
// leaving the request itself untouched and unknown.

// unknownAnswerLane differs from its neighbours only where a routing decision
// is allowed to differ: the output tariff and the two waits. Input price is
// deliberately held equal, and the beliefs are tight enough that exploration
// cannot make the fixture's decision for it.
func unknownAnswerLane(name string, ttft, rate, priceOut float64) Belief {
	sigma := math.Log(1.05) / zP90
	return Belief{
		ID: ID{Model: testModel, Lane: name},
		Facts: Facts{
			Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100,
			PriceIn: 0.00000075, PriceOut: priceOut,
		},
		TTFT:    Posterior{X: math.Log(ttft), P: sigma * sigma / 20},
		Rate:    Posterior{X: math.Log(rate), P: sigma * sigma / 20},
		Quality: Beta{A: 39, B: 1},
		At:      noon,
	}
}

func unknownAnswerRequest() Request {
	return Request{
		Model: testModel, PromptTokens: 20_000, MaxTokens: 4_000,
		ValueOfTime: AttentionValue, QualityNeed: 0.90,
		Horizon: ExplorationHorizon, Now: noon,
	}
}

func unknownAnswerThreeLanes() *fakeLedger {
	return &fakeLedger{beliefs: []Belief{
		unknownAnswerLane("cheap", 1200, 25, 0.000003),
		unknownAnswerLane("dear", 500, 60, 0.00003),
		unknownAnswerLane("absurd", 400, 80, 0.0003),
	}}
}

func frontierNames(frontier []Scored) []string {
	names := make([]string, 0, len(frontier))
	for _, candidate := range frontier {
		names = append(names, candidate.ID.Lane)
	}
	return names
}

// TestAnUnknownAnswerIsComparedLikeTheOrdinaryAnswer is the whole decision,
// rather than one arithmetic seam: candidate set, sampled order, price and the
// explanation must all come from the same reading of the request.
func TestAnUnknownAnswerIsComparedLikeTheOrdinaryAnswer(t *testing.T) {
	chooser := chooserOn(unknownAnswerThreeLanes())
	unknown := unknownAnswerRequest()
	stated := unknown
	stated.Visible = AssumedAnswerTokens

	withoutMeasurement := chooser.Choose(unknown)
	withStatedLength := chooser.Choose(stated)
	if !reflect.DeepEqual(withoutMeasurement.Order, withStatedLength.Order) {
		t.Fatalf("unknown answer order = %v, ordinary answer order = %v", withoutMeasurement.Order, withStatedLength.Order)
	}
	if !reflect.DeepEqual(withoutMeasurement.Frontier, withStatedLength.Frontier) {
		t.Fatalf("unknown answer candidates = %+v, ordinary answer candidates = %+v", withoutMeasurement.Frontier, withStatedLength.Frontier)
	}
	if named(frontierNames(withoutMeasurement.Frontier), "absurd") || named(frontierNames(withStatedLength.Frontier), "absurd") {
		t.Fatalf("the hundredfold output tariff survived one of the two ceilings: unknown=%v stated=%v",
			frontierNames(withoutMeasurement.Frontier), frontierNames(withStatedLength.Frontier))
	}
}

// TestTheFirstTurnChoosesTheCheapLane holds the measured failure in its
// smallest useful set. The dear lane starts sooner and writes faster, but its
// tenfold output tariff costs more than that felt head start is worth.
func TestTheFirstTurnChoosesTheCheapLane(t *testing.T) {
	ledger := &fakeLedger{beliefs: []Belief{
		unknownAnswerLane("cheap", 1200, 25, 0.000003),
		unknownAnswerLane("dear", 500, 60, 0.00003),
	}}
	choice := chooserOn(ledger).Choose(unknownAnswerRequest())
	if len(choice.Order) == 0 || choice.Order[0] != "cheap" {
		t.Fatalf("the first turn's order = %v, want the cheap lane first; candidates: %+v", choice.Order, choice.Frontier)
	}
}

// TestAnUnknownAnswerStillDropsALaneBeatenOnEveryAxis asks the frontier
// directly. Even a caller below Choose cannot turn missing answer evidence
// into an exemption from dominance.
func TestAnUnknownAnswerStillDropsALaneBeatenOnEveryAxis(t *testing.T) {
	better := unknownAnswerLane("better", 500, 60, 0.000003)
	beaten := unknownAnswerLane("beaten", 1200, 25, 0.000003)
	frontier := frontierFor([]Belief{better, beaten}, unknownAnswerRequest(), gateOptions{}, nil)
	if named(frontierNames(frontier), "beaten") {
		t.Fatalf("a lane beaten on every axis survived because the answer length was unknown: %+v", frontier)
	}
}

// TestMissingAndImpossibleLengthsReceiveOneVisibleAnswer keeps corrupt or
// subtractive estimates from becoming negative work. The assumed answer is
// visible because it is the prose somebody at an empty line is waiting for;
// treating it as hidden would buy generation speed the person cannot feel.
func TestMissingAndImpossibleLengthsReceiveOneVisibleAnswer(t *testing.T) {
	for _, req := range []Request{
		{},
		{Visible: -1},
		{Hidden: -1},
		{Visible: -1, Hidden: 2},
		{Visible: 2, Hidden: -1},
		{Visible: -2, Hidden: 2},
	} {
		got := req.answered()
		if got.Visible != AssumedAnswerTokens || got.Hidden != 0 {
			t.Errorf("answered(%+v) = visible %d, hidden %d", req, got.Visible, got.Hidden)
		}
	}

	for _, stated := range []Request{
		{Visible: 23, PromptTokens: 99},
		{Hidden: 17, PromptTokens: 99},
		{Visible: 23, Hidden: 17, PromptTokens: 99},
	} {
		if got := stated.answered(); got != stated {
			t.Fatalf("a stated answer was rewritten: got %+v, want %+v", got, stated)
		}
	}
}

// TestNoPublishedOutputTariffStillMeansNoRelativeCeiling is the unchanged
// escape hatch: an assumed length supplies no price a provider never
// published, so every otherwise acceptable lane remains.
func TestNoPublishedOutputTariffStillMeansNoRelativeCeiling(t *testing.T) {
	candidates := []Scored{
		{ID: ID{Model: testModel, Lane: "one"}, TTFT: 400, Rate: 80},
		{ID: ID{Model: testModel, Lane: "two"}, TTFT: 1200, Rate: 25},
	}
	facts := []Facts{{PriceIn: 0.00000075}, {PriceIn: 0.00000075}}
	kept := underPriceCeiling(candidates, facts, unknownAnswerRequest())
	if !reflect.DeepEqual(kept, candidates) {
		t.Fatalf("lanes with no published output tariff met an invented ceiling: %+v", kept)
	}
}

// TestNobodyWaitingStillChoosesOnTheAssumedPrice keeps λ = 0 honest. The
// quicker lane remains under the relative ceiling, but saving its wait is
// worth nothing and cannot overrule the cheaper answer.
func TestNobodyWaitingStillChoosesOnTheAssumedPrice(t *testing.T) {
	ledger := &fakeLedger{beliefs: []Belief{
		unknownAnswerLane("cheap", 1200, 25, 0.000003),
		unknownAnswerLane("quick", 400, 80, 0.0000036),
	}}
	req := unknownAnswerRequest()
	req.ValueOfTime = 0
	choice := chooserOn(ledger).Choose(req)
	if len(choice.Order) == 0 || choice.Order[0] != "cheap" {
		t.Fatalf("nobody was waiting but speed bought the first place: order=%v candidates=%+v", choice.Order, choice.Frontier)
	}
}
