package provider

import (
	"strings"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
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

// ── A PIN THE WIRE REFUSES, UNDER `simple` ──────────────────────────────────
//
// The loose end the mode shipped with. `simple` is the row that promises the
// wire carries the person's own instruction and nothing else, and the danger in
// that promise is the silent half: a demand that never goes out leaves the
// chrome saying `@deepseek` over a request the router answered from wherever it
// liked, which is the exact complaint the mode was written to close.
//
// So the pin is SENT, once, even when this process already believes the
// account's own settings exclude the machine — the wire is the only authority
// on that, the belief is a day old by design (internal/lane's account.go), and
// one refused round trip a run is a smaller price than a promise quietly
// broken. When the refusal comes back the pin is retired through the SAME door
// every other mode retires one through, so the person reads the one sentence
// this build has for it and the chrome stops naming the machine in the same
// breath.
func TestSimpleRoutingSendsThePinOnceThenRetiresItOutLoud(t *testing.T) {
	rig := newLaneRig(t, "simple/pin-refused", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	pinned(t, LanePin{Lane: "Ghost"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	// 1. THE DEMAND GOES OUT. Under `simple` the pin is the whole request.
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn died on a refusal the widened retry was supposed to absorb: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	if !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask carried %+v, want the one machine the person pinned", asks[0])
	}
	before := len(asks)

	// 2. AND THE PERSON IS TOLD, in the sentence this build already has.
	said := notes.retired()
	if len(said) != 1 {
		t.Fatalf("the conversation was told %d times, want exactly once: %q", len(said), said)
	}
	if want := RetiredPinLine("Ghost"); said[0] != want {
		t.Fatalf("the conversation reads %q, want %q", said[0], want)
	}

	// 3. AND THE CHROME STOPS NAMING THE MACHINE IN THE SAME BREATH. This is
	//    the whole defect: a status line still reading `@ghost` over a bare
	//    request is the chrome promising a machine the wire was never asked for.
	if still := PinnedFor(rig.model); still != "" {
		t.Fatalf("the chrome would still say @%s after the wire retired the pin", still)
	}

	// 4. AND THE NEXT REQUEST GOES BARE rather than buying the same 404 again.
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	asks = rig.server.Asks()
	if len(asks) <= before {
		t.Fatal("the second turn sent nothing")
	}
	for _, later := range asks[before:] {
		if len(later.Only) != 0 || later.Sort != "" || len(later.Order) != 0 || later.MaxPrice != nil {
			t.Fatalf("a request after the retirement carried %+v, want a bare request", later)
		}
	}
	if said := notes.retired(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times over two turns: %q", len(said), said)
	}
}

// AND A PIN THIS PROCESS ALREADY BELIEVES THE ACCOUNT EXCLUDES IS STILL SENT.
//
// The store is a belief carried between processes — `account-exclusions.json`,
// written a day at a time — and under every other row it is allowed to retire a
// strict pin before the call (lanes.go). Under `simple` it may not: the row's
// whole promise is that what a person wrote is what goes out, and a pin retired
// on a saved belief is a pin the person would watch fail with no request ever
// made. One refused round trip a run is what honesty costs here, and the
// sentence they read afterwards is the same one.
func TestSimpleRoutingStillAsksForAPinTheSavedExclusionsCover(t *testing.T) {
	rig := newLaneRig(t, "simple/pin-excluded", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	// The state the live run started in: a previous process wrote the machine
	// out of every model's serving set on the router's own sentence.
	lanes.ExcludeForAccount("Ghost", "the account's own settings exclude it")
	t.Cleanup(func() { lanes.ClearAccountExclusion("Ghost") })
	if !lanes.AccountExcludes("Ghost") {
		t.Fatal("the fixture did not write the exclusion it is about")
	}
	pinned(t, LanePin{Lane: "Ghost"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	if _, err := rig.client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 || !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask carried %+v, want the pin sent to the one authority on it", asks)
	}
	if said := notes.retired(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times about its own row, want exactly once: %q", len(said), said)
	}
	if still := PinnedFor(rig.model); still != "" {
		t.Fatalf("the chrome would still say @%s over a request that no longer asks for it", still)
	}
}
