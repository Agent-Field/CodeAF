package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// ── the ladder, rung by rung, on a real request body ────────────────────────
//
// The mapping is the whole feature at this layer, and it is exactly the kind of
// thing that is right in a table and wrong on the wire: a rung that renders in
// the settings sheet while the request carries nothing is the failure mode. So
// every assertion here reads a body the adapter actually serialized.

// THE FIVE RUNGS, AND WHAT EACH ONE ASKS FOR.
//
// low, medium and high are the provider's own three words. xhigh and max are
// high plus a thinking budget, because there is no word above high — see
// effortladder.go for why the two figures are the two figures.
func TestEveryRungOfTheLadderReachesTheWireAsItsOwnShape(t *testing.T) {
	for _, want := range []struct {
		rung   effort.Rung
		level  string
		budget float64
	}{
		{effort.Low, "low", 0},
		{effort.Medium, "medium", 0},
		{effort.High, "high", 0},
		{effort.XHigh, "high", xhighReasoningTokens},
		{effort.Max, "high", maxReasoningTokens},
	} {
		t.Run(want.rung.String(), func(t *testing.T) {
			client, recorded := newTestClient(t, Config{
				SupportsParameter: func(string, string) (bool, bool) { return true, true },
			})
			ctx := WithConfiguredEffortRung(context.Background(), want.rung)
			if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
				t.Fatal(err)
			}
			reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
			if level, _ := reasoning["effort"].(string); level != want.level {
				t.Fatalf("%s sent effort %q, want %q (body %#v)", want.rung, level, want.level, recorded.body(0))
			}
			budget, present := reasoning["max_tokens"].(float64)
			switch {
			case want.budget == 0 && present:
				t.Fatalf("%s sent a thinking budget of %.0f; the three word rungs carry none", want.rung, budget)
			case want.budget != 0 && budget != want.budget:
				t.Fatalf("%s sent a budget of %.0f, want %.0f", want.rung, budget, want.budget)
			}
		})
	}
}

// ABSENCE IS ABSENCE. The rung nobody set sends no reasoning field at all —
// not an empty object, not a zero budget — because an unstamped request is the
// one shape that leaves the body byte-for-byte what it was.
func TestTheUnsetRungSendsNoReasoningFieldAtAll(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	ctx := WithConfiguredEffortRung(context.Background(), effort.None)
	if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("an unset rung sent %#v, want no reasoning field", recorded.body(0))
	}
}

// A HARNESS RUNG OBEYS THE CATALOG GATE AND A PERSON'S DOES NOT. This is the
// existing law for the effort word, restated for the ladder's door: the two
// setters differ in exactly one way and it is this one.
func TestOnlyAPersonsRungReachesAModelTheCatalogCannotVouchFor(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return false, false },
	})
	if _, err := client.CompleteWithMessages(
		WithEffortRung(context.Background(), effort.Max), userMessages("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(
		WithConfiguredEffortRung(context.Background(), effort.Max), userMessages("b")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("a harness rung reached an unknown model: %#v", recorded.body(0))
	}
	reasoning, _ := recorded.body(1)["reasoning"].(map[string]any)
	if budget, _ := reasoning["max_tokens"].(float64); budget != maxReasoningTokens {
		t.Fatalf("the person's rung = %#v, want a budget of %d", recorded.body(1), maxReasoningTokens)
	}
}

// A model whose catalog row says it takes no reasoning knob gets neither half
// of one — the budget does not survive the word it rides on.
func TestNeitherHalfOfTheKnobReachesAModelThatRefusesReasoning(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return false, true },
	})
	ctx := WithConfiguredEffortRung(context.Background(), effort.XHigh)
	if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["reasoning"]; present {
		t.Fatalf("request = %#v, want no knob for a model that does not take one", recorded.body(0))
	}
}

// budgetRefusingHandler answers a thinking budget the way an endpoint that does
// not take one does, and answers everything else normally.
func budgetRefusingHandler(recorded *capture) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(recorded.count() - 1)
		reasoning, _ := body["reasoning"].(map[string]any)
		if budget, present := reasoning["max_tokens"].(float64); present && budget > 0 {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(
				`{"error":{"message":"reasoning.max_tokens is not supported for this model","code":400}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"model":"budget/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

// THE TOP RUNGS DEGRADE, THEY NEVER FAIL.
//
// An endpoint that will not take a thinking allowance still takes the effort
// word, so max on such a model becomes the deepest thing that endpoint has a
// word for — high — and the fact is remembered, so the discovery costs one
// request per model and never one per call.
func TestAModelThatRefusesAThinkingBudgetKeepsTheLevelAndLosesTheBudget(t *testing.T) {
	quirksAt(t, "budget/no-allowance")
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "budget/no-allowance",
		HTTPClient:        handlerClient(budgetRefusingHandler(recorded)),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithConfiguredEffortRung(context.Background(), effort.Max)
	if _, err := client.CompleteWithMessages(ctx, userMessages("think hard")); err != nil {
		t.Fatalf("a refused budget must be repaired, not returned: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the rejected one and its repair", recorded.count())
	}
	if reasoning, _ := recorded.body(0)["reasoning"].(map[string]any); reasoning["max_tokens"] == nil {
		t.Fatalf("the first request carried no budget, so there was nothing to be refused: %#v", recorded.body(0))
	}
	repaired, _ := recorded.body(1)["reasoning"].(map[string]any)
	if _, present := repaired["max_tokens"]; present {
		t.Fatalf("the repair kept the budget: %#v", recorded.body(1))
	}
	if level, _ := repaired["effort"].(string); level != "high" {
		t.Fatalf("the repair sent effort %q, want high — the level survives the budget", level)
	}
	if !ReasoningBudgetRefused("budget/no-allowance") {
		t.Fatal("the refusal was repaired and not remembered")
	}

	// And it is remembered: the second call costs ONE request, not two.
	if _, err := client.CompleteWithMessages(ctx, userMessages("again")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 3 {
		t.Fatalf("sent %d requests in total, want 3 — the fact was relearned", recorded.count())
	}
}

// The mapping itself, without a wire: a rung this adapter has not been taught
// asks for nothing rather than guessing at a shape nobody decided.
func TestAnUnknownRungAsksForNothing(t *testing.T) {
	if got := effortRequestFor(effort.Rung("deepest"), true); got.effort != EffortNone || got.budget != 0 {
		t.Fatalf("an unknown rung mapped to %+v, want an empty request", got)
	}
}
