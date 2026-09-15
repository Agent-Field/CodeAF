package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
)

// The whole new `auto`, proved over the wire: OpenRouter routes until two
// refusals reach this client, and then the chooser's closed set goes out.
//
// THE ROUTER HERE DELIVERS ITS REFUSALS rather than absorbing them, because
// every lane it may fall back to is full — which is the only shape a refusal
// ever reaches a person under, and the shape the live wire produced on
// 2026-09-12: three full pools, one turn, and the takeover armed mid-turn.
func TestTwoPacedAnswersHandTheRoadToTheChooser(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	const model = "openrouter/gate-e2e"
	full := lanestub.Profile{TTFT: time.Millisecond, Rate: 400, Tokens: 8, Tools: true, Paced: true, PacedFor: time.Second}
	server := lanestub.New(model,
		lanestub.Lane{Name: "fulla", Profile: full},
		lanestub.Lane{Name: "fullb", Profile: full},
	)
	t.Cleanup(server.Close)
	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL(), Model: model, Routing: StaticRouting(RoutingLatency)})
	if err != nil {
		t.Fatal(err)
	}
	lanes.HeardPrefsCarried(server.URL())
	client.velocity = newVelocityLedger()
	client.wait = func(context.Context, time.Duration) error { return nil }
	primed(t, model,
		laneBelief(model, "fulla", 400, 70, 0.25),
		laneBelief(model, "fullb", 900, 60, 0.20),
	)
	forgetRouterGates()

	// Every lane is full, so the router's own road ends in refusals this
	// client hears — two of them, which is the takeover.
	_, _ = client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if asks := server.Asks(); len(asks) == 0 || len(asks[0].Only) != 0 {
		t.Fatalf("the first call demanded a set on OpenRouter's road: %+v", asks[0].Only)
	}
	if !gateFor(model).TakenOver(time.Now()) {
		t.Fatal("two paced refusals arrived and the gate still lends the router the road")
	}

	// The very next call is the chooser's: the wire carries its admitted set.
	_, _ = client.CompleteWithMessages(context.Background(), userMessages("again"))
	asks := server.Asks()
	last := asks[len(asks)-1]
	if len(last.Only) < 2 {
		t.Fatalf("after the takeover the wire carried only=%v, want the chooser's admitted set", last.Only)
	}
}
