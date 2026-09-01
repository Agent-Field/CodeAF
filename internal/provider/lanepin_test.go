package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHAT A PERSON SAID, ON THE WIRE ─────────────────────────────────────────
//
// The lane row has three states and they are three different requests. These
// tests assert them where they can only be asserted honestly: against a real
// router, reading what actually arrived, rather than against the object that
// was going to be encoded.

// pinned installs one lane row for the duration of a test and puts the process
// back afterwards — the pin is process-wide (lanepin.go), so a test that left
// one set would be a test that pinned every test after it.
func pinned(t *testing.T, pin LanePin) {
	t.Helper()
	before := CurrentLanePin()
	SetLanePin(pin)
	t.Cleanup(func() { SetLanePin(before) })
}

// A STRICT PIN IS A DEMAND. `only` names the one machine, `allow_fallbacks` is
// false, and nothing the belief ranked rides beside it — because an order is a
// list of places to try next, and "nowhere else" is precisely what the picker
// and the manual promise a pin means.
func TestAPinnedLaneGoesNowhereElse(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
		laneBelief(model, "molasses", 1500, 40, 0.20),
	)
	pinned(t, LanePin{Lane: "molasses"})

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	ask := asks[0]
	if len(ask.Only) != 1 || ask.Only[0] != "molasses" {
		t.Fatalf("only = %v, want the machine that was pinned", ask.Only)
	}
	if len(ask.Order) != 0 {
		t.Fatalf("order = %v rode beside a pin, which is a list of places to go instead", ask.Order)
	}
	if ask.Sort != "" {
		t.Fatalf("sort = %q rode beside a pin, which has nothing left to sort", ask.Sort)
	}
	if served := server.Served(); len(served) == 0 || served[0] != "molasses" {
		t.Fatalf("%v answered, want the pinned machine", served)
	}
}

// AND IT RANKS NOTHING, so nothing can quietly route around it — while still
// carrying the candidate set, so that the question a stalled pin raises can name
// where a `y` would go. The two are the whole of "a pin is asked, never
// overridden": the wire is sent `Only` and the offer is pointed at the frontier.
func TestAStrictPinRanksNothingAndStillKnowsWhereToOffer(t *testing.T) {
	client, _, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)
	pinned(t, LanePin{Lane: "brass"})

	choice, made := client.laneChoiceFor(callKnobs{}, model, &ai.Request{Model: model, Messages: userMessages("hello")})
	if !made {
		t.Fatal("a pin made no choice at all")
	}
	if len(choice.Order) != 0 {
		t.Fatalf("a pinned request ranked %v", choice.Order)
	}
	if len(choice.Only) != 1 || !strings.EqualFold(choice.Only[0], "brass") {
		t.Fatalf("only = %v, want the pinned machine and nothing else", choice.Only)
	}
	// AND THE OFFER HAS SOMEWHERE TO POINT. A plan over this choice is pinned,
	// so the act is a question rather than a rescue — and a question that could
	// not name a lane would be one nobody could answer.
	plan := lanes.PlanFor(choice, lanes.Belief{}, lanes.RoleTalk, time.Now())
	if !plan.Pinned && len(choice.Only) > 0 {
		plan.Pinned = true
	}
	if len(plan.Alts) == 0 {
		t.Fatal("a stalled pin could raise no offer: the plan names nowhere a `y` would go")
	}
}

// A PIN THAT MAY BE BORROWED IS A PREFERENCE. The named machine goes to the
// head of the order, the belief's own ranking stays behind it, fallbacks stay
// on, and the alternative the chooser named is still there for a rescue.
func TestABorrowablePinLeadsTheOrderAndKeepsARescue(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
		laneBelief(model, "molasses", 1500, 40, 0.20),
	)
	pinned(t, LanePin{Lane: "molasses", Borrow: true})

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	ask := asks[0]
	if len(ask.Order) == 0 || ask.Order[0] != "molasses" {
		t.Fatalf("order = %v, want the borrowable pin in front", ask.Order)
	}
	if len(ask.Order) < 2 {
		t.Fatalf("order = %v, want the belief's ranking behind the pin", ask.Order)
	}
	if len(ask.Only) != 0 {
		t.Fatalf("only = %v, and a borrowable pin is not a demand", ask.Only)
	}
	// AND THE RESCUE THE BELIEF NAMED IS STILL THERE. It is asserted against the
	// same call with the pin taken off rather than against a name, because which
	// lane a rescue would go to is the chooser's sampled business and not this
	// test's — what a borrowable pin promises is that it did not take one away.
	request := &ai.Request{Model: model, Messages: userMessages("hello")}
	withPin, _ := client.laneChoiceFor(callKnobs{}, model, request)
	SetLanePin(LanePin{})
	without, _ := client.laneChoiceFor(callKnobs{}, model, request)
	rescues := func(choice lanes.Choice) int {
		return len(lanes.PlanFor(choice, lanes.Belief{}, lanes.RoleTalk, time.Now()).Alts)
	}
	if rescues(without) > 0 && rescues(withPin) == 0 {
		t.Fatalf("the belief would have rescued from %v and the borrowable pin took it away", without.Order)
	}
}

// AND `openrouter` ASKS FOR NO LANE AT ALL: no order, no only, and the sort
// word this build sent before it held an opinion about machines.
func TestTheOpenRouterRowSendsTodaysRequest(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)
	pinned(t, LanePin{OpenRouter: true})

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	ask := asks[0]
	if len(ask.Order) != 0 || len(ask.Only) != 0 {
		t.Fatalf("order = %v only = %v, want no lane asked for at all", ask.Order, ask.Only)
	}
	if ask.Sort != "latency" {
		t.Fatalf("sort = %q, want the sort word this build sent before lanes existed", ask.Sort)
	}
	if _, made := client.laneChoiceFor(callKnobs{}, model, &ai.Request{Model: model, Messages: userMessages("hello")}); made {
		t.Fatal("a row asking for no lane still made a lane choice")
	}
}

// ── THE SPEED GUARD ─────────────────────────────────────────────────────────

// THE ROW OFF IS A BUDGET THAT ALLOWS NOTHING AND A PROBE THAT IS NEVER BOUGHT.
// Both halves are one promise — this build may spend a little extra to keep an
// answer moving — so a row that turned off one of them would be a row nobody
// could reason about.
func TestTheGuardOffRefusesEveryHedgeAndEveryProbe(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)
	before := LaneGuardOn()
	t.Cleanup(func() { SetLaneGuard(before) })

	SetLaneGuard(false)
	if currentHedgeBudget().Allow(client.clock(), 0.0001) {
		t.Fatal("the budget allowed a rescue with the guard off")
	}
	if !InstallLaneProber(client, nil) {
		t.Fatal("the prober refused a router client")
	}
	t.Cleanup(func() { lanes.Default().Reset() })
	lanes.Default().Prober().Probe(context.Background(), model, []string{"quicksilver", "brass"})
	if got := len(server.Asks()); got != 0 {
		t.Fatalf("%d requests went out with the guard off, want none", got)
	}

	// And back on, the same budget answers the same question the other way.
	SetLaneGuard(true)
	if !currentHedgeBudget().Allow(client.clock(), 0.0001) {
		t.Fatal("the budget refused a rescue with the guard on")
	}
}

// ── THE KEYSTROKE ───────────────────────────────────────────────────────────

// A KEYSTROKE BUYS ONE PAIR AND THE NEXT ONE BUYS NOTHING. Both halves matter:
// the first is the whole point of probing at all — a measurement of our path
// taken seconds before the request rather than an aggregate half an hour old —
// and the second is what keeps it costing two hundredths of a cent a turn
// instead of two hundredths of a cent a character.
func TestTheFirstKeystrokeBuysAPairAndTheSecondBuysNothing(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
		laneBelief(model, "molasses", 1500, 40, 0.20),
	)
	lanes.Default().SetProber(lanes.NewProber(lanes.ProberConfig{Send: client.probeLane}))
	t.Cleanup(func() { lanes.Default().SetProber(nil) })

	client.ProbeLanes(context.Background(), model)
	waitFor(t, func() bool { return len(server.Asks()) == 2 })

	for index, ask := range server.Asks() {
		if ask.MaxTokens != 1 {
			t.Fatalf("probe %d asked for %d tokens, want one", index, ask.MaxTokens)
		}
		if len(ask.Only) != 1 {
			t.Fatalf("probe %d asked for %v, want exactly one named lane", index, ask.Only)
		}
		if !ask.Stream {
			t.Fatalf("probe %d did not stream, so it timed no first token", index)
		}
	}

	// The second keystroke inside the window buys nothing at all.
	client.ProbeLanes(context.Background(), model)
	time.Sleep(50 * time.Millisecond)
	if got := len(server.Asks()); got != 2 {
		t.Fatalf("%d probes went out for two keystrokes inside one window, want the one pair", got)
	}
}
