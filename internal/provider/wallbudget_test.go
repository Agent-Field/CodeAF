package provider

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ── the wall, told (effortladder.go) ────────────────────────────────────────
//
// Issue #927: a structuring call on GLM 5.3 thought for its whole four-minute
// wall on one machine and answered in two seconds on another, and nothing on
// the wire had ever told it how long it had. These read the body the adapter
// really serialized, because a budget that is right in the arithmetic and absent
// from the request is exactly the failure being fixed.

// wallTested is the four-minute wall issue #927 was cut by, stated here so the
// arithmetic below reads as the incident's own.
const wallTested = 4 * time.Minute

// thinksAtMax is a model that cannot stop thinking and thinks at the top of its
// ladder when nothing is sent — GLM 5.3's published row.
func thinksAtMax(string) (ReasoningProfile, bool) {
	return ReasoningProfile{Mandatory: true, Default: "max"}, true
}

// thinksWhenAsked is a model whose thinking pass runs only when a request
// asks for one.
func thinksWhenAsked(string) (ReasoningProfile, bool) {
	return ReasoningProfile{Efforts: []Effort{EffortLow, EffortHigh}}, true
}

// believeLane scripts what the lane belief holds for one machine: its median
// output rate, known well. It is the reading the wait plan uses, so what these
// tests pin is the budget derived from the same belief the controller waits on.
func believeLane(t *testing.T, model, lane string, tokensPerSecond float64) {
	t.Helper()
	ledger := &scriptedLedger{beliefs: map[lanes.ID]lanes.Belief{
		{Model: model, Lane: lane}: {
			ID:   lanes.ID{Model: model, Lane: lane},
			Rate: lanes.Posterior{X: math.Log(tokensPerSecond), P: 0.01},
		},
	}}
	lanes.Default().SetLedger(ledger)
	t.Cleanup(func() { lanes.Default().SetLedger(nil) })
}

// walledOn is a request under the tested wall that asked for one machine.
func walledOn(ctx context.Context, lane string) context.Context {
	return WithLaneChoice(WithThinkingWall(ctx, wallTested), lanes.Choice{Order: []string{lane}})
}

// reasoningSent is the reasoning object one recorded body carried, nil when it
// carried none.
func reasoningSent(recorded *capture, index int) map[string]any {
	reasoning, _ := recorded.body(index)["reasoning"].(map[string]any)
	return reasoning
}

// TestAWalledCallIsToldTheBudgetItsWallAndItsLaneImply is the fix's first half,
// with the incident's own numbers: a model thinking at max, on a machine believed
// at 210 tokens a second, under a four-minute wall, is told 47,880 tokens.
func TestAWalledCallIsToldTheBudgetItsWallAndItsLaneImply(t *testing.T) {
	client, recorded := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
	believeLane(t, "z-ai/glm-5.3", "Friendli", 210)

	if _, err := client.CompleteWithMessages(walledOn(context.Background(), "Friendli"), userMessages("ground this")); err != nil {
		t.Fatal(err)
	}
	reasoning := reasoningSent(recorded, 0)
	budget, _ := reasoning["max_tokens"].(float64)
	want := thinkingRoom(thinkingShare("max"), 210, wallTested)
	if want != 47880 {
		t.Fatalf("the derivation itself moved: share(max) × 210 × 240s = %d, want 47,880", want)
	}
	if int(budget) != want {
		t.Fatalf("a walled call on a machine believed at 210 tok/s carried budget %.0f, want %d (body %#v)",
			budget, want, recorded.body(0))
	}
	// THE BUDGET TRAVELS ALONE, as every budget on this ladder does: a body that
	// carried the level's word as well would be a 400.
	if word, present := reasoning["effort"]; present {
		t.Fatalf("the budget travelled with the word %v; the wire takes one or the other", word)
	}
}

// TestTheBudgetIsTheLanesPaceAndNotAConstant is the derivation's own claim: the
// same wall on a slower machine is a smaller budget, and on either one the
// answer keeps part of the wall.
func TestTheBudgetIsTheLanesPaceAndNotAConstant(t *testing.T) {
	budgetAt := func(tokensPerSecond float64) int {
		client, recorded := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
		believeLane(t, "z-ai/glm-5.3", "lane", tokensPerSecond)
		if _, err := client.CompleteWithMessages(walledOn(context.Background(), "lane"), userMessages("spine")); err != nil {
			t.Fatal(err)
		}
		budget, _ := reasoningSent(recorded, 0)["max_tokens"].(float64)
		return int(budget)
	}
	fast, slow := budgetAt(210), budgetAt(20)
	if fast <= slow {
		t.Fatalf("210 tok/s was told %d and 20 tok/s was told %d; a faster machine thinks further in the same wall", fast, slow)
	}
	for rate, budget := range map[float64]int{210: fast, 20: slow} {
		if whole := int(rate * wallTested.Seconds()); budget <= 0 || budget >= whole {
			t.Fatalf("at %.0f tok/s the budget is %d of the %d tokens the wall holds; the answer keeps the rest", rate, budget, whole)
		}
	}
}

// TestAnUnwalledCallIsUnchanged is the control: the same model, the same belief,
// no wall — and the request says nothing about thinking, exactly as before.
func TestAnUnwalledCallIsUnchanged(t *testing.T) {
	client, recorded := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
	believeLane(t, "z-ai/glm-5.3", "Friendli", 210)

	ctx := WithLaneChoice(context.Background(), lanes.Choice{Order: []string{"Friendli"}})
	if _, err := client.CompleteWithMessages(ctx, userMessages("ground this")); err != nil {
		t.Fatal(err)
	}
	if reasoning := reasoningSent(recorded, 0); reasoning != nil {
		t.Fatalf("an unwalled call carried a reasoning object %#v; only a wall has anything to say", reasoning)
	}
}

// TestTheWallSpeaksOnlyForAPassNobodySized pins the three requests the wall
// leaves alone, and the one it tightens.
func TestTheWallSpeaksOnlyForAPassNobodySized(t *testing.T) {
	t.Run("a word somebody chose stands", func(t *testing.T) {
		client, recorded := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
		believeLane(t, "z-ai/glm-5.3", "Friendli", 210)
		ctx := walledOn(WithConfiguredReasoningEffort(context.Background(), EffortLow), "Friendli")
		if _, err := client.CompleteWithMessages(ctx, userMessages("ground this")); err != nil {
			t.Fatal(err)
		}
		reasoning := reasoningSent(recorded, 0)
		if word, _ := reasoning["effort"].(string); word != "low" {
			t.Fatalf("a chosen low under a wall went out as %#v; the person's word is their statement of the depth", reasoning)
		}
	})
	t.Run("a model that thinks only when asked is not asked", func(t *testing.T) {
		client, recorded := newTestClient(t, Config{Model: "sim/optional", ReasoningProfile: thinksWhenAsked})
		believeLane(t, "sim/optional", "Friendli", 210)
		if _, err := client.CompleteWithMessages(walledOn(context.Background(), "Friendli"), userMessages("title")); err != nil {
			t.Fatal(err)
		}
		if reasoning := reasoningSent(recorded, 0); reasoning != nil {
			t.Fatalf("a wall switched thinking on for a model nobody asked to think: %#v", reasoning)
		}
	})
	t.Run("a machine nothing is believed about derives nothing", func(t *testing.T) {
		client, recorded := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
		if _, err := client.CompleteWithMessages(walledOn(context.Background(), "Unknown"), userMessages("ground")); err != nil {
			t.Fatal(err)
		}
		if reasoning := reasoningSent(recorded, 0); reasoning != nil {
			t.Fatalf("a budget went out with no belief to derive it from: %#v", reasoning)
		}
	})
	t.Run("the smaller of a rung's budget and the wall's wins", func(t *testing.T) {
		for _, lane := range []struct {
			rate float64
			want int
		}{
			// At 210 tok/s the wall holds more than xhigh asks for, so the
			// rung's own figure stands; at 20 it holds less, and the wall's is
			// what the pass can actually spend.
			{210, xhighReasoningTokens},
			{20, thinkingRoom(thinkingShare(EffortHigh), 20, wallTested)},
		} {
			client, recorded := newTestClient(t, Config{
				Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax,
				SupportsParameter: func(string, string) (bool, bool) { return true, true },
			})
			believeLane(t, "z-ai/glm-5.3", "lane", lane.rate)
			ctx := walledOn(WithConfiguredEffortRung(context.Background(), effort.XHigh), "lane")
			if _, err := client.CompleteWithMessages(ctx, userMessages("plan")); err != nil {
				t.Fatal(err)
			}
			if budget, _ := reasoningSent(recorded, 0)["max_tokens"].(float64); int(budget) != lane.want {
				t.Fatalf("xhigh under a wall at %.0f tok/s sent %.0f, want %d", lane.rate, budget, lane.want)
			}
		}
	})
}

// TestAWalledThoughtIsFiledUnderItsWallAndNotItsCount keeps the thinking-duration
// belief's key stable. The count moves with every answer the lane gives, and a
// key that moved with it would file each walled thought under a rung nobody
// would ever ask about again.
func TestAWalledThoughtIsFiledUnderItsWallAndNotItsCount(t *testing.T) {
	for _, rate := range []float64{210, 20} {
		client, _ := newTestClient(t, Config{Model: "z-ai/glm-5.3", ReasoningProfile: thinksAtMax})
		believeLane(t, "z-ai/glm-5.3", "lane", rate)
		got := client.recordedEffort("z-ai/glm-5.3", knobsFrom(walledOn(context.Background(), "lane")))
		if got != "max within 4m0s" {
			t.Fatalf("at %.0f tok/s the walled rung is recorded as %q, want the wall's own name", rate, got)
		}
	}
}
