package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
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
	server := lanestub.New(model, lane("cinnabar"), lane("tinder"))
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
		laneBelief(model, "cinnabar", 400, 70, 0.60),
		laneBelief(model, "tinder", 900, 60, 0.20),
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
	if len(first.Only) != 2 || !namesEndpoint(first.Only, "cinnabar") || !namesEndpoint(first.Only, "tinder") {
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
	client, server, _ := pacedPair(t, "cinnabar")

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("the healthy machine should have answered: %v", err)
	}
	asks := server.Asks()
	if len(asks) < 2 {
		t.Fatalf("the call sent %d requests; the full pool should have cost one move", len(asks))
	}
	second := asks[1]
	if len(second.Only) != 1 || second.Only[0] != "tinder" {
		t.Fatalf("the second body demanded %v, want only the machine that had not refused: %+v", second.Only, second)
	}
	if !second.NoFallbacks {
		t.Fatalf("the second body let the router go outside a set that still had a machine in it: %+v", second)
	}
	for index, ask := range asks[1:] {
		if namesEndpoint(ask.Only, "cinnabar") {
			t.Fatalf("body %d demanded the machine that had already refused this call: %+v", index+2, ask)
		}
	}
	if served := server.Served(); len(served) == 0 || served[len(served)-1] != "tinder" {
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
	knobs.refused.add("cinnabar")
	knobs.refused.add("tinder")
	choice := lanes.Choice{
		Only:  []string{"cinnabar", "tinder"},
		Order: []string{"cinnabar", "tinder"},
	}
	knobs.laneChoice = &choice

	prefs := composedFor(t, client, model, knobs)
	if len(prefs.Only) != 0 {
		t.Fatalf("only = %v, want no demand left after every machine in it refused this call", prefs.Only)
	}
	if prefs.AllowFallbacks == nil || !*prefs.AllowFallbacks {
		t.Fatalf("the body named no machines and still forbade the router every other one: %+v", prefs)
	}
	if !namesEndpoint(prefs.Ignore, "cinnabar") || !namesEndpoint(prefs.Ignore, "tinder") {
		t.Fatalf("ignore = %v, want both refusers still named so the widened body does not land back on one", prefs.Ignore)
	}
}

// TestADemandSomebodyAskedForIsNarrowedByNobody keeps the other demand whole. A
// demand a person pinned, and the one machine a rescue's arm exists to try, are
// instructions — "and nowhere else" — and what happens when the machine refuses
// is [control.Next]'s to decide and the ladder's to act on, never this object's
// to quietly widen.
func TestADemandSomebodyAskedForIsNarrowedByNobody(t *testing.T) {
	client, _, model := pacedPair(t, "cinnabar")

	pin := lanes.Choice{Only: []string{"cinnabar"}, Pinned: true}
	belief := lanes.Choice{
		Only:  []string{"cinnabar", "tinder"},
		Order: []string{"cinnabar", "tinder"},
	}
	for _, asked := range []struct {
		what   string
		knobs  callKnobs
		demand string
	}{
		{"a person's strict pin", callKnobs{intent: IntentInteractive, laneChoice: &pin}, "cinnabar"},
		{"a rescue's own arm", callKnobs{intent: IntentInteractive, laneChoice: &belief, hedgeLane: "cinnabar"}, "cinnabar"},
	} {
		knobs := asked.knobs
		knobs.refused = &refusedHere{}
		knobs.refused.add("cinnabar")

		prefs := composedFor(t, client, model, knobs)
		if len(prefs.Only) != 1 || prefs.Only[0] != asked.demand {
			t.Errorf("%s: only = %v, want it left exactly as it stands", asked.what, prefs.Only)
		}
		if prefs.AllowFallbacks == nil || *prefs.AllowFallbacks {
			t.Errorf("%s: a refused demand was quietly allowed to fall back: %+v", asked.what, prefs)
		}
	}
}

// ── A DEMAND IS NOT A PIN ───────────────────────────────────────────────────
//
// They are two different facts that happen to travel in one field. A PIN is a
// person's one machine, typed by hand, and it is ASKED about rather than routed
// around: `coreweave is slow · switch to auto? (y)`. A DEMAND is the set THIS
// process admitted, and it is ours to relax the instant it stops working — a
// rescue, a walk, a narrowing, none of which anybody needs to be consulted
// about.
//
// The distinction had no field of its own while `Only` held nothing but a pin,
// so the plan counted names: `Pinned = len(choice.Only) > 0`. The moment the
// chooser began demanding its own admitted set, that sentence called EVERY
// ordinary call pinned — the hazard takes the `Ask` road before it even looks
// for somewhere to go, so a person is offered `switch to auto?` about a machine
// they never chose, and no rescue is sent at all; and the primary arm stops
// walking off a 429, which is the opposite of what this wave is for. So the
// fact is carried ([lanes.Choice.Pinned]) and these two scenarios are the law
// at the wire: one stall, two choices, two different acts.

// demandedChoice is what the chooser hands an ordinary routed call: the machines
// it admitted, demanded, and nobody's pin.
func demandedChoice(model string) lanes.Choice {
	choice := choiceFor(model, 0)
	choice.Only = append([]string(nil), choice.Order...)
	return choice
}

// TestADemandedCallIsRescuedAndAPersonsPinIsAsked is the whole of the
// difference, proved over one wire.
//
// The machine that goes quiet is the same machine, the belief about it is the
// same belief, and the controller's arithmetic is identical. Only the ACT
// differs, and it must: the set we admitted is ours to leave, the machine a
// person named is theirs to be asked about.
func TestADemandedCallIsRescuedAndAPersonsPinIsAsked(t *testing.T) {
	for _, scenario := range []struct {
		what   string
		choice func(model string) lanes.Choice
		act    string
		asked  bool
	}{
		{"the set the chooser admitted", demandedChoice, "hedge", false},
		{"the one machine a person named", func(model string) lanes.Choice {
			return pinnedChoice(model, "A", "B")
		}, "ask", true},
	} {
		t.Run(scenario.what, func(t *testing.T) {
			told := listen(t)
			rig := newLaneRig(t, "demand/act",
				lanestub.Lane{Name: "A", Profile: lanestub.Profile{
					TTFT: time.Second, Rate: 2000, Tokens: 24, Heartbeats: true,
				}},
				lanestub.Lane{Name: "B", Profile: lanestub.Profile{
					TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24,
				}},
			)
			rig.believes("A", 20, 2000)
			rig.patience(t, 150*time.Millisecond)

			report := &HedgeReport{}
			ctx := WithHedgeReport(context.Background(), report)
			ctx = WithLaneChoice(ctx, scenario.choice(rig.model))

			answers := make(chan int, 1)
			go func() {
				response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
				if err != nil {
					answers <- -1
					return
				}
				answers <- answerTokens(response)
			}()
			// A QUESTION IS ONLY A QUESTION IF SOMEBODY ANSWERS IT, so the
			// pinned row says yes and the demanded row must never be waiting
			// for anybody at all.
			if scenario.asked {
				if !AnswerOffer(waitForOffer(t, told), true) {
					t.Fatal("the offer was gone before it was answered")
				}
			}
			if tokens := <-answers; tokens != 24 {
				t.Fatalf("the answer is %d tokens, want the rescuer's 24", tokens)
			}
			if got := report.Action(); got != scenario.act {
				t.Fatalf("the wait was answered with %q, want %q", got, scenario.act)
			}
			if _, asked := told.find(PhaseAsking); asked != scenario.asked {
				t.Fatalf("a question was raised: %v, want %v — %v", asked, scenario.asked, told.story())
			}
			if got := rig.server.Requests("B"); got != 1 {
				t.Fatalf("Requests(B) = %d, want the one rescue this scenario is about", got)
			}
		})
	}
}

// TestADemandedPrimaryWalksOffAFaultThatBreaksIt is the second road out of the
// same mistake.
//
// A person's pin does not walk off a transient fault — the machine they named
// is still the machine they named, and the ladder rather than the race is what
// answers for it — and while every ordinary call read as pinned, neither did
// anything else: the primary arm stayed put with a healthy machine one line
// away. A stream that was arriving and then was not is 1,884 of the 1,887
// in-stream failures in the census, so it is the fault this is asked about.
func TestADemandedPrimaryWalksOffAFaultThatBreaksIt(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "demand/walk",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24, TearAfter: 4,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{
			TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24,
		}},
	)
	rig.believes("A", 20, 2000)

	ctx := WithLaneChoice(context.Background(), demandedChoice(rig.model))
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("a broken stream on one of two demanded machines ended the call: %v", err)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the whole answer from the machine that walked on", tokens)
	}
	if got := rig.server.Requests("B"); got == 0 {
		t.Fatal("the broken machine was never left, so the demand was read as somebody's pin")
	}
	if _, asked := told.find(PhaseAsking); asked {
		t.Fatalf("a machine nobody pinned was asked about: %v", told.story())
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
