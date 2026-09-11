package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE BELIEF'S ORDER DROPS THE WIRE CEILING ───────────────────────────────
//
// THE MEASURED FAILURE (docs/design/routing/ASSESSMENT-20260911.md). `max_price`
// is list × 1.25 and the list price is the cheapest endpoint, so on 2026-09-11
// a request the belief addressed to GMICloud, Novita, Fireworks went out under a
// $0.75 ceiling that left one of them, and the account's data policy excluded
// that one: 404, and the two fastest lanes for the model were reachable only
// through a rescue arm.
func TestTheBeliefsOrderRidesWithoutThePriceCeiling(t *testing.T) {
	client, recorded := pricedClient(t, nil, 0.0000002, 0.0000006, true)
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "vendor/fast-model"
	primed(t, model,
		laneBelief(model, "DeepInfra", 6500, 22, 0.60),
		laneBelief(model, "Novita", 1800, 234, 1.20),
		laneBelief(model, "GMICloud", 2900, 211, 1.20),
	)
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs := prefsOn(t, recorded, 0)
	if prefs == nil {
		t.Fatal("no preference on the wire")
	}
	order, _ := prefs["order"].([]any)
	if len(order) == 0 {
		t.Fatalf("the belief's order did not reach the wire: %v", prefs)
	}
	if ceiling := ceilingOn(t, recorded, 0); ceiling != nil {
		t.Fatalf("a belief-ordered request carried max_price %v, which vetoes the lanes the order names", ceiling)
	}
}

// And with no belief to speak, the latency ask keeps its ceiling exactly as
// [TestALatencyAskCarriesACeilingDerivedFromTheModelsOwnListPrice] demands —
// that test is the other half of this one and is left untouched.

// ── A REFUSAL REACHES THE BELIEF ────────────────────────────────────────────

func TestAPacedPoolIsToldToTheBeliefAsARefusal(t *testing.T) {
	ledger, _ := testLedger()
	const base = "https://openrouter.ai/api/v1"
	client := &Client{config: Config{BaseURL: base}, velocity: ledger}
	lanes.HeardPrefsCarried(base)
	const model = "vendor/split-model"
	recording := primed(t, model)
	refusal := client.laneRefusalFor(model, "Fireworks",
		apiError(http.StatusTooManyRequests, []byte(
			`{"error":{"message":"Provider returned error","metadata":{"provider_name":"DeepInfra"}}}`)))
	client.refuseLane(model, refusal, 0)
	judged := recording.judged()
	if len(judged) != 1 {
		t.Fatalf("the belief heard %d outcomes from a paced pool, want one", len(judged))
	}
	got := judged[0]
	if !got.Refused || got.Accepted {
		t.Fatalf("the outcome was %+v, want a refusal", got)
	}
	if got.ID.Lane != "DeepInfra" || got.ID.Model != model {
		t.Fatalf("the refusal was filed against %+v, want the pool that refused", got.ID)
	}
	if got.At.IsZero() || time.Since(got.At) > time.Minute {
		t.Fatalf("the refusal was stamped %v, want now", got.At)
	}
}
