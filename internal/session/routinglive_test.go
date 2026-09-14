package session

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// A SESSION THAT CARRIES NO ROW OF ITS OWN READS THE LIVE ONE. The row a person
// wrote is installed process-wide and moves while the process runs — the
// settings panel writes it and re-installs it in the same keystroke — so a gate
// on this side that read a field filled in at launch would be measuring
// endpoints for somebody who has since turned routing off (issue #1022).
//
// A caller that really carries a row — an engine host, a task child — still
// hands one down, and a handed-down answer still wins.
func TestASessionsRoutingFollowsTheInstalledRowUnlessItCarriesItsOwn(t *testing.T) {
	before := provider.RoutingNow()
	t.Cleanup(func() { provider.InstallRouting(before) })

	fromProfile := Config{}
	provider.InstallRouting(provider.RoutingLatency)
	if got := fromProfile.routingInForce(); got != provider.RoutingLatency {
		t.Fatalf("a session handed no row routes by %q, want the installed latency", got)
	}
	provider.InstallRouting(provider.RoutingOff)
	if got := fromProfile.routingInForce(); got != provider.RoutingOff {
		t.Fatalf("the row moved to off and the session still reads %q", got)
	}

	carried := Config{Routing: provider.RoutingPrice}
	if got := carried.routingInForce(); got != provider.RoutingPrice {
		t.Fatalf("a session carrying its own row reads %q, want price", got)
	}

	// AND WITH NOTHING INSTALLED AT ALL the answer is the row this build ships,
	// which is the same answer the adapter gives such a caller.
	provider.InstallRouting("")
	if got := fromProfile.routingInForce(); got != provider.DefaultRouting {
		t.Fatalf("with no row installed the session reads %q, want the shipped row", got)
	}
}
