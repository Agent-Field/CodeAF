package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE CHOOSER DEMANDS ─────────────────────────────────────────────────────
//
// `provider.order` is advice. With `allow_fallbacks` true the router reads the
// list, weighs it against its own load, and is free to serve the request from a
// machine the list never named — so every gate the chooser applied was spending
// its evidence on a set the router could ignore. The admitted set is now stated
// as `provider.only`, which the router must honour.
//
// The cost is accepted and it is what these scenarios are about: a pool whose
// admitted machines all refuse has nowhere left to go, and what it does then has
// to be exactly right — narrow to the machines that have not refused, never ask
// the refuser again, and when the last one has gone, take the demand OFF rather
// than sending a set the router has just said it cannot serve.

// pacedPair is two machines behind one model, both believed in, and a client
// pointed at them. `refuse` names the machines whose pool is full: a 429 naming
// itself, before any stream opens, which is the commonest named refusal in the
// ten-day log.
func pacedPair(t *testing.T, refuse ...string) (*Client, *lanestub.Server, string) {
	t.Helper()
	const model = "openrouter/demand-model"
	full := map[string]bool{}
	for _, name := range refuse {
		full[name] = true
	}
	lane := func(name string) lanestub.Lane {
		profile := lanestub.Profile{TTFT: time.Millisecond, Rate: 400, Tokens: 8, Tools: true}
		if full[name] {
			profile.Paced = true
		}
		return lanestub.Lane{Name: name, Profile: profile}
	}
	server := lanestub.New(model, lane("deepinfra"), lane("fireworks"))
	t.Cleanup(server.Close)
	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: server.URL(),
		Model:   model,
		Routing: StaticRouting(RoutingLatency),
	})
	if err != nil {
		t.Fatal(err)
	}
	lanes.HeardPrefsCarried(server.URL())
	client.velocity = newVelocityLedger()
	client.wait = func(context.Context, time.Duration) error { return nil }
	// NEITHER MACHINE DOMINATES THE OTHER, which is what keeps both of them in
	// the frontier: one starts sooner and costs more, the other starts later and
	// costs less, so the Pareto prune has nothing to remove and the demand has
	// two names in it.
	primed(t, model,
		laneBelief(model, "deepinfra", 400, 70, 0.60),
		laneBelief(model, "fireworks", 900, 60, 0.20),
	)
	return client, server, model
}

// TestTheAdmittedSetIsDemandedAndNotMerelyRanked is the first half: what goes
// out when nothing has refused yet.
func TestTheAdmittedSetIsDemandedAndNotMerelyRanked(t *testing.T) {
	client, server, _ := pacedPair(t)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	first := asks[0]
	if len(first.Only) != 2 || !namesEndpoint(first.Only, "deepinfra") || !namesEndpoint(first.Only, "fireworks") {
		t.Fatalf("only = %v, want both admitted machines demanded: %+v", first.Only, first)
	}
	if !first.NoFallbacks {
		t.Fatalf("the request named a set and let the router go outside it: %+v", first)
	}
	if first.Sort != "" {
		t.Fatalf("sort = %q rode beside a demand, which is a second opinion about a set that is closed", first.Sort)
	}
	if len(first.Ignore) != 0 {
		t.Fatalf("ignore = %v named a machine beside a closed set — the two fields say opposite things about one name", first.Ignore)
	}
}

// TestADemandNarrowsToTheMachineThatHasNotRefused is the seam #925 took its
// demand back out over: a pool of two with one full.
//
// The demand narrows to the machine that has not refused, and `allow_fallbacks`
// stays off, because there is still somewhere inside the set to go.
func TestADemandNarrowsToTheMachineThatHasNotRefused(t *testing.T) {
	client, server, _ := pacedPair(t, "deepinfra")

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("the healthy machine should have answered: %v", err)
	}
	asks := server.Asks()
	if len(asks) < 2 {
		t.Fatalf("the call sent %d requests; the full pool should have cost one move", len(asks))
	}
	second := asks[1]
	if len(second.Only) != 1 || second.Only[0] != "fireworks" {
		t.Fatalf("the second body demanded %v, want only the machine that had not refused: %+v", second.Only, second)
	}
	if !second.NoFallbacks {
		t.Fatalf("the second body let the router go outside a set that still had a machine in it: %+v", second)
	}
	for index, ask := range asks[1:] {
		if namesEndpoint(ask.Only, "deepinfra") {
			t.Fatalf("body %d demanded the machine that had already refused this call: %+v", index+2, ask)
		}
	}
	if served := server.Served(); len(served) == 0 || served[len(served)-1] != "fireworks" {
		t.Fatalf("%v answered, want the healthy machine", served)
	}
}

// TestADemandWithNothingLeftInItComesOffTheBody is the other half of the cost,
// asked of the composed object rather than of a whole call — deliberately, and
// the reason is worth stating. On the wire a pool whose every admitted machine
// has refused has no move left: [control.Next] has tried each of them, so the
// call ends or climbs the ladder, and the ladder's first rung takes the whole
// preference off by itself. The state this guards is the one body that WOULD go
// out with an exhausted demand — a comeback the last machine named, a rung
// climbed by a door that kept the object — and it is guarded because keeping the
// last name there is what asked the router to serve the request from the machine
// that had just refused it, while `allow_fallbacks: false` forbade the healthy
// one it would otherwise have found.
func TestADemandWithNothingLeftInItComesOffTheBody(t *testing.T) {
	client, _, model := pacedPair(t)

	knobs := callKnobs{intent: IntentInteractive, refused: &refusedHere{}}
	knobs.refused.add("deepinfra")
	knobs.refused.add("fireworks")
	choice := lanes.Choice{
		Only:  []string{"deepinfra", "fireworks"},
		Order: []string{"deepinfra", "fireworks"},
	}
	knobs.laneChoice = &choice

	prefs := composedFor(t, client, model, knobs)
	if len(prefs.Only) != 0 {
		t.Fatalf("only = %v, want no demand left after every machine in it refused this call", prefs.Only)
	}
	if prefs.AllowFallbacks == nil || !*prefs.AllowFallbacks {
		t.Fatalf("the body named no machines and still forbade the router every other one: %+v", prefs)
	}
	if !namesEndpoint(prefs.Ignore, "deepinfra") || !namesEndpoint(prefs.Ignore, "fireworks") {
		t.Fatalf("ignore = %v, want both refusers still named so the widened body does not land back on one", prefs.Ignore)
	}
}

// TestADemandSomebodyAskedForIsNarrowedByNobody keeps the other demand whole. A
// demand a person pinned, and the one machine a rescue's arm exists to try, are
// instructions — "and nowhere else" — and what happens when the machine refuses
// is [control.Next]'s to decide and the ladder's to act on, never this object's
// to quietly widen.
func TestADemandSomebodyAskedForIsNarrowedByNobody(t *testing.T) {
	client, _, model := pacedPair(t, "deepinfra")

	pin := lanes.Choice{Only: []string{"deepinfra"}}
	belief := lanes.Choice{
		Only:  []string{"deepinfra", "fireworks"},
		Order: []string{"deepinfra", "fireworks"},
	}
	for _, asked := range []struct {
		what   string
		knobs  callKnobs
		demand string
	}{
		{"a person's strict pin", callKnobs{intent: IntentInteractive, laneChoice: &pin}, "deepinfra"},
		{"a rescue's own arm", callKnobs{intent: IntentInteractive, laneChoice: &belief, hedgeLane: "deepinfra"}, "deepinfra"},
	} {
		knobs := asked.knobs
		knobs.refused = &refusedHere{}
		knobs.refused.add("deepinfra")

		prefs := composedFor(t, client, model, knobs)
		if len(prefs.Only) != 1 || prefs.Only[0] != asked.demand {
			t.Errorf("%s: only = %v, want it left exactly as it stands", asked.what, prefs.Only)
		}
		if prefs.AllowFallbacks == nil || *prefs.AllowFallbacks {
			t.Errorf("%s: a refused demand was quietly allowed to fall back: %+v", asked.what, prefs)
		}
	}
}

// composedFor is the object one request would actually go out with.
func composedFor(t *testing.T, client *Client, model string, knobs callKnobs) *providerPrefs {
	t.Helper()
	prefs := client.wirePreferences(model, knobs)
	if prefs == nil {
		t.Fatal("no preference object was composed at all")
	}
	return prefs
}
