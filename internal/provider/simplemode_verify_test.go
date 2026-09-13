package provider

import (
	"strings"
	"testing"
)

// THROWAWAY VERIFICATION for the simple routing mode: what the wire carries
// when the row reads `simple`. Kept as a law because the mode's whole promise
// is the shape of the request — bare without a pin, the pin's demand and
// nothing else with one.
func TestSimpleModeSendsOnlyThePersonsPin(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.URL(), server)
	client.config.Routing = StaticRouting(RoutingSimple)

	// No pin: the request carries no provider object at all, and nobody
	// hedges — one ask per turn.
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("unpinned simple turn failed: %v", err)
	}
	// A pin: the demand is the whole object.
	pinned(t, LanePin{Lane: "Harbor"})
	if _, err := client.CompleteWithMessages(talking(), userMessages("again")); err != nil {
		t.Fatalf("pinned simple turn failed: %v", err)
	}

	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("two turns produced %d asks, want exactly 2 (no hedging)", len(asks))
	}
	bare := asks[0]
	if bare.Sort != "" || bare.Only != nil || bare.Order != nil || bare.Ignore != nil || bare.MaxPrice != nil || bare.NoFallbacks {
		t.Fatalf("simple with no pin carried %+v, want a bare request", bare)
	}
	demand := asks[1]
	if len(demand.Only) != 1 || !strings.EqualFold(demand.Only[0], "Harbor") ||
		!demand.NoFallbacks || demand.Sort != "" || demand.MaxPrice != nil || demand.Order != nil {
		t.Fatalf("simple with a pin carried %+v, want only Harbor and fallbacks off", demand)
	}
}
