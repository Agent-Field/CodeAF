package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ── A ROLE'S DEADLINE, ON THE WIRE ──────────────────────────────────────────
//
// internal/lane decides what a role will not wait for; this is where that
// decision becomes bytes. The whole point of the pair of assertions below is
// that a REFUSAL has to reach `provider.ignore`: `provider.order` is a ranking
// the router may ignore once `allow_fallbacks` is true, which is #850's own
// sentence and the mechanism behind the 2026-09-11 09:33 drift.

// TestARefusedMachineReachesTheWiresIgnoreAndNotJustTheOrder stages a pool where
// one machine is believed to answer far beyond the role's declared patience and
// asserts on the request as it arrived.
func TestARefusedMachineReachesTheWiresIgnoreAndNotJustTheOrder(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 400, 1.6),
		laneBelief(model, "brass", 600, 400, 1.2),
		laneBelief(model, "molasses", 900, 16, 0.8),
	)

	// A recall is the call that sits in front of a keypress: ten seconds of
	// patience, and an answer nobody reads a token of. Its length comes from the
	// conversation, which is where a model with no receipts yet gets it
	// (workload.go) — four hundred tokens, which the slow machine would take
	// twenty-five seconds to write.
	ctx := WithRole(context.Background(), lanes.RoleRecall)
	if _, err := client.CompleteWithMessages(ctx, answeringAtLength(400)); err != nil {
		t.Fatal(err)
	}
	asks := server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	ask := asks[0]
	if namesEndpoint(ask.Order, "molasses") {
		t.Fatalf("order = %v, want the machine past this role's patience out of it", ask.Order)
	}
	if !namesEndpoint(ask.Ignore, "molasses") {
		t.Fatalf("ignore = %v — leaving a machine out of the order leaves the router free to pick it", ask.Ignore)
	}
	if served := server.Served(); len(served) == 0 || served[0] == "molasses" {
		t.Fatalf("%v answered; the veto did not reach the wire", served)
	}
}

// TestACacheAffinityCannotOutrankARefusal is the 2026-09-11 09:33 mechanism,
// staged.
//
// The affinity pin latches onto whichever machine ANSWERED (affinity.go) and
// leads the next request's order. With `allow_fallbacks` on, one request that
// asked for one machine and was answered by another therefore made that other
// machine the head of every order for the rest of the session — which is how
// six of seventeen reflex calls that morning went to a machine nothing had ever
// measured. A saving on prefill may not buy a wait the role has refused.
func TestACacheAffinityCannotOutrankARefusal(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 400, 1.6),
		laneBelief(model, "brass", 600, 400, 1.2),
		laneBelief(model, "molasses", 900, 16, 0.8),
	)
	// The cache pin says the slow machine holds this conversation's prefix,
	// which is the strongest claim anything in this package can make for a
	// machine short of a person naming it.
	client.pins.hold("a-conversation", lanes.LedgerModel(model), "molasses")

	ctx := WithRole(context.Background(), lanes.RoleRecall)
	ctx = WithCacheKey(ctx, "a-conversation")
	if _, err := client.CompleteWithMessages(ctx, answeringAtLength(400)); err != nil {
		t.Fatal(err)
	}
	ask := server.Asks()[0]
	if len(ask.Order) > 0 && ask.Order[0] == "molasses" {
		t.Fatalf("order = %v, want the warm prefix to lose to the role's own deadline", ask.Order)
	}
	if !namesEndpoint(ask.Ignore, "molasses") {
		t.Fatalf("ignore = %v, want the refused machine named even though it holds the cache", ask.Ignore)
	}
}

// TestNoProbeRidesAWaitedRoleOverManyRequests is the brief's own figure: over
// two hundred requests in a role somebody is waiting on, the machine the sheet
// doubts is never asked first.
//
// THE DRAW IS SEEDED ON THE MOMENT (internal/lane's seedFor), so two hundred
// requests really are two hundred different draws and one in ten of them would
// have promoted the doubted machine before this change.
func TestNoProbeRidesAWaitedRoleOverManyRequests(t *testing.T) {
	client, server, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 400, 1.6),
		doubtedBelief(laneBelief(model, "brass", 300, 400, 1.2)),
	)
	// A FRESH CONTEXT PER REQUEST, because the choice is decided once per call
	// and carried on the context ([Client.withLaneChoice]); two hundred calls on
	// one context would be one draw asserted two hundred times.
	for round := 0; round < 200; round++ {
		ctx := WithRole(context.Background(), lanes.RoleRecall)
		if _, err := client.CompleteWithMessages(ctx, answeringAtLength(60)); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	for index, ask := range server.Asks() {
		if len(ask.Order) > 0 && ask.Order[0] == "brass" {
			t.Fatalf("request %d asked the doubted machine first; a probe rode a call somebody was waiting on", index)
		}
	}
	// The other direction — that a doubt CAN still be settled by a call nobody
	// waits on — is a fact about the role table rather than about the wire, and
	// internal/lane's TestNoProbeRidesARoleSomebodyIsWaitingOn asserts it over
	// every role in that table rather than over the one this fixture stages.
}

// doubtedBelief is a machine the sheet has published a health word against —
// the shape [lane.doubted] reads, without this file knowing how it reads it.
func doubtedBelief(belief lanes.Belief) lanes.Belief {
	belief.Facts.Status = 3
	belief.Quality = lanes.Beta{A: 1, B: 1}
	belief.QualityAt = time.Now()
	return belief
}

// answeringAtLength is a conversation whose last answer was roughly tokens long,
// which is how a model with no receipts yet is given an expected answer size
// (workload.go: "learns from receipts when possible and from the conversation
// otherwise"). It is the honest fixture for this question — an expectation this
// process really could have had, rather than one poked into a memo.
func answeringAtLength(tokens int) []ai.Message {
	said := strings.Repeat("word ", tokens*charsPerToken/5)
	return []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: said}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}},
	}
}
