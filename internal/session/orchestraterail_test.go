package session

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── A SESSION'S CAP IS HONOURED BY THE WORK IT HANDS OUT ────────────────────
//
// The composer layer's third line says `it may spend up to $2.00 before it
// asks` (tui3's composerlayer.go, SCREEN 2e). What makes that a fact rather
// than a caption is this: the figure becomes the errand session's own spend
// rail (Config.SpendRailUSD), and a session with a rail hands out no adaptive
// run whose tank is larger than the rail — so the run's own gate is where the
// figure bites, and the gate is a question put to a person.

// TestARunStartedByACappedSessionCannotOutspendTheCap is the arithmetic half:
// whatever tank a caller asks for, a session with a cap hands out its own.
func TestARunStartedByACappedSessionCannotOutspendTheCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent, _ := newTestAgent(t, replier(func([]ai.Message) string { return "{}" }),
		func(config *Config) { config.SpendRailUSD = 2.00 })
	for _, c := range []struct{ asked, want float64 }{
		{0, 2.00},    // a run nobody bounded is bounded by the session
		{5.00, 2.00}, // a bigger tank is held down to the cap
		{0.50, 0.50}, // a smaller one is the caller's, untouched
	} {
		if got := agent.railCap(c.asked); got != c.want {
			t.Fatalf("a $2.00 session asked for %v handed out %v, wanted %v", c.asked, got, c.want)
		}
	}
	// AND A SESSION WITH NO CAP CHANGES NOTHING. Zero still means the run
	// nobody bounded, which is what every launch that names no rail does today.
	plain, _ := newTestAgent(t, replier(func([]ai.Message) string { return "{}" }), nil)
	for _, asked := range []float64{0, 5.00} {
		if got := plain.railCap(asked); got != asked {
			t.Fatalf("an uncapped session turned %v into %v", asked, got)
		}
	}
}

// TestARunStartedWithACapStopsAndAsksAtThatCap is the whole seam end to end: a
// task started by a session capped at X opens on a tank of X — not on the ten
// dollars a caller asked for — and when the spend reaches X the run stops
// launching and puts the question to a person.
func TestARunStartedWithACapStopsAndAsksAtThatCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The planner's own call is priced by the provider, which is the honest
	// shape: a run's tank pays for the judgement as well as the work
	// ([orchestrateCost] takes the provider's own figure when there is one).
	price := 0.60
	completer := replier(func(messages []ai.Message) string {
		if isPlannerCall(messages) {
			return `{"add":[{"id":"n1","goal":"read the notes"}]}`
		}
		return "n1 read the notes"
	})
	agent, _ := newTestAgent(t, pricedAt(completer, price),
		func(config *Config) { config.SpendRailUSD = 0.50 })

	// The caller asks for ten dollars. The session was given fifty cents.
	id, err := agent.RunOrchestrate(context.Background(), "summarise the release notes", "", 10.00)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(15 * time.Second)
	for {
		snap, known := agent.OrchestrateSnapshot(id)
		if !known {
			t.Fatalf("the run %q is not registered", id)
		}
		if snap.Fuel.Cap != 0.50 {
			t.Fatalf("the run opened on a tank of %v, wanted the session's own 0.50", snap.Fuel.Cap)
		}
		if snap.Paused {
			if snap.Fuel.Spent < snap.Fuel.Cap {
				t.Fatalf("the run asked at %v of %v", snap.Fuel.Spent, snap.Fuel.Cap)
			}
			// AND THE QUESTION IS A PERSON'S TO ANSWER, which is what "before it
			// asks" means: the run is holding, and a top-up carries it on.
			if _, err := agent.ResolveOrchestrate(id, "topup:1.00"); err != nil {
				t.Fatalf("the gate would not take an answer: %v", err)
			}
			return
		}
		if snap.Done {
			t.Fatalf("the run finished on %v of a %v tank without ever asking",
				snap.Fuel.Spent, snap.Fuel.Cap)
		}
		select {
		case <-deadline:
			t.Fatalf("the run neither asked nor finished: %+v", snap.Fuel)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// pricedAt is a completer whose every answer carries a provider cost, so that a
// scripted run spends real dollars against its tank. It wraps rather than
// replaces [replier] because what a call SAYS and what it COSTS are two facts
// and only one of them is the script's.
func pricedAt(inner Completer, usd float64) Completer {
	return pricedCompleter{inner: inner, usd: usd}
}

type pricedCompleter struct {
	inner Completer
	usd   float64
}

func (p pricedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	response, err := p.inner.CompleteWithMessages(ctx, messages, opts...)
	if err != nil || response == nil {
		return response, err
	}
	cost := p.usd
	if response.Usage == nil {
		response.Usage = &ai.Usage{}
	}
	response.Usage.Cost = &cost
	return response, nil
}
