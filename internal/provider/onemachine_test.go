package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ONLY A REQUEST THAT REALLY HAD ONE MACHINE BEHIND IT IS CALLED ONE (#1343).
//
// A cut that says OneMachine gets the harness's one wait with no end when the
// person is watching and no model is left to move to (internal/taxonomy's
// waitsForEver), so the flag is a claim with a price. It used to be set on
// every request that drew no lane choice, which is the shipped router's
// default pool, and an ordinary pool user whose stream died before naming its
// server was waited on as though their only server were down.
func TestOnlyARequestWithNoPoolIsCalledOneMachine(t *testing.T) {
	const router = "https://openrouter.ai/api/v1"
	const own = "http://own-server.invalid:8080/v1"
	pinned := lanes.Choice{Only: []string{"Fireworks"}, Pinned: true}
	drawn := lanes.Choice{Order: []string{"Together", "DeepInfra"}}

	cases := []struct {
		name   string
		base   string
		direct bool
		choice *lanes.Choice
		served string
		want   bool
	}{
		// The pool, in every shape that draws no lane choice. The routing row
		// steers the router; it does not take the pool away.
		{name: "the shipped router with no choice drawn", base: router, want: false},
		{name: "the shipped router with a lane order drawn", base: router, choice: &drawn, want: false},
		{name: "the shipped router naming the machine that served", base: router, served: "Together", want: false},
		// The three shapes that really are one machine.
		{name: "a pin to one lane on the shipped router", base: router, choice: &pinned, want: true},
		{name: "a connected direct service", base: "https://api.direct.invalid/v1", direct: true, want: true},
		{name: "a person's own base url", base: own, want: true},
		// And a machine that names itself is somebody the next ask can avoid.
		{name: "an own base url whose stream named a server", base: own, served: "node-2", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(Config{BaseURL: tc.base, Model: "deepseek/deepseek-v4-flash", Direct: tc.direct})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			ctx := context.Background()
			if tc.choice != nil {
				ctx = WithLaneChoice(ctx, *tc.choice)
			}
			cut := &StreamCut{Reason: CutSilent}
			client.stampCut(ctx, cut, tc.served, time.Now(), 0)
			if cut.OneMachine != tc.want {
				t.Fatalf("OneMachine = %v, want %v (base %q, served %q, choice %+v)",
					cut.OneMachine, tc.want, tc.base, tc.served, tc.choice)
			}
		})
	}
}
