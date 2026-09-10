package provider

import (
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// TestALaterAcceptedStreamCutOutranksThePrimaryRoutingRefusal is the mixed
// failure from candidate-03, through the real HTTP send path. The capped
// primary and the first demanded rescue are structurally refused; the last
// rescue is accepted, writes visible output, and then reaches its stream wall.
// That cut is the failure the caller must receive, because the layer above owns
// the bounded retry and clears the exposed partial answer before asking again.
func TestALaterAcceptedStreamCutOutranksThePrimaryRoutingRefusal(t *testing.T) {
	defer shortenWall(t, 80*time.Millisecond, 80*time.Millisecond)()

	rig := newPricedLaneRig(t, "hedge/terminal-cut",
		lanestub.Lane{Name: "A", SheetOnly: true, Profile: lanestub.Profile{
			PriceIn: 0.66e-6, PriceOut: 1.98e-6,
		}},
		lanestub.Lane{Name: "B", SheetOnly: true, Profile: lanestub.Profile{
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
		lanestub.Lane{Name: "C", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 100, Tokens: 100,
			PriceIn: 1.32e-6, PriceOut: 3.96e-6,
		}},
	)
	SetHedgeBudget(lanes.NewBudget(6, 0))
	choice := lanes.Choice{
		Order: []string{"A", "B", "C"},
		Frontier: []lanes.Scored{
			{ID: lanes.ID{Model: rig.model, Lane: "A"}, TTFT: 2, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: rig.model, Lane: "B"}, TTFT: 5, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: rig.model, Lane: "C"}, TTFT: 5, Rate: 100, Price: 0.01},
		},
	}

	var mu sync.Mutex
	visible := 0
	ctx := WithStreamObserver(WithLaneChoice(talking(), choice), func(event StreamEvent) {
		if event.Kind == StreamDelta && event.Delta != "" {
			mu.Lock()
			visible++
			mu.Unlock()
		}
	})
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if response != nil {
		t.Fatalf("a cut stream returned a response: %+v", response)
	}
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutOverrun {
		t.Fatalf("err = %v, want the accepted rescue's CutOverrun rather than the primary 404", err)
	}
	mu.Lock()
	wrote := visible
	mu.Unlock()
	if wrote == 0 {
		t.Fatal("the accepted rescue exposed no partial output, so the replay-safety case was not staged")
	}

	asks := rig.server.Asks()
	if len(asks) != 3 {
		t.Fatalf("%d requests went out, want the capped primary and two demanded rescues with no hidden retry", len(asks))
	}
	if asks[0].MaxPrice == nil || len(asks[0].Only) != 0 {
		t.Fatalf("primary carried only=%v max_price=%+v, want an ordered capped request", asks[0].Only, asks[0].MaxPrice)
	}
	if !demandedOnly(asks[1], "B") || !demandedOnly(asks[2], "C") {
		t.Fatalf("rescue demands = %v then %v, want B then C", asks[1].Only, asks[2].Only)
	}
	if asks[1].MaxPrice != nil || asks[2].MaxPrice != nil {
		t.Fatalf("rescues repeated the primary price ceiling: B=%+v C=%+v", asks[1].MaxPrice, asks[2].MaxPrice)
	}
}
