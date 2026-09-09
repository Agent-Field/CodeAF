package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHAT A DEFAULT REQUEST DOES NOT CARRY ───────────────────────────────────
//
// These tests run through the adapter's real send path against a local handler
// and assert on the BYTES. That matters more here than anywhere else in this
// package: every field below is optional upstream and omitting it means "the
// provider's own default answers", so a regression that reintroduces one is
// invisible in behaviour, invisible in the response, and expensive on every
// call in the process.

// generationKnobs is every generation parameter an OpenAI-compatible endpoint
// takes. A default request carries NONE of them.
//
// The list is deliberately longer than what this adapter has ever set: the
// failure being pinned is a field arriving from somewhere nobody looked —
// a config default, the SDK's own request builder, a shadowed struct field
// that serialized a zero — and a test that only names the fields we already
// know about cannot catch that.
var generationKnobs = []string{
	"max_tokens",
	"max_completion_tokens",
	"reasoning",
	"reasoning_effort",
	"temperature",
	"top_p",
	"top_k",
	"top_a",
	"min_p",
	"frequency_penalty",
	"presence_penalty",
	"repetition_penalty",
	"seed",
	"stop",
	"logit_bias",
	"logprobs",
	"top_logprobs",
	"verbosity",
}

// assertNoGenerationKnobs fails naming the exact key, because "the body was
// wrong" is not a useful failure for a shape this wide.
func assertNoGenerationKnobs(t *testing.T, what string, body map[string]any) {
	t.Helper()
	if body == nil {
		t.Fatalf("%s: no request reached the endpoint", what)
	}
	for _, knob := range generationKnobs {
		if value, present := body[knob]; present {
			t.Errorf("%s carried %q = %#v — a generation parameter nobody asked for", what, knob, value)
		}
	}
}

// TestADefaultRequestCarriesNoGenerationParameter is the whole wave in one
// assertion: an adapter built the way every surface builds it, asked for a
// completion the way every turn asks for one, puts no output cap, no sampling
// setting and no reasoning object on the wire.
func TestADefaultRequestCarriesNoGenerationParameter(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	body := recorded.body(0)
	assertNoGenerationKnobs(t, "a default completion", body)

	// AND WHAT IT DOES CARRY, in the same test, because "sent nothing" would
	// pass this file trivially. The model, the conversation and the usage
	// receipt are not knobs and are not optional.
	if body["model"] != "sim/model" {
		t.Errorf("model = %#v, want the configured slug", body["model"])
	}
	if messages, _ := body["messages"].([]any); len(messages) != 1 {
		t.Errorf("messages = %#v, want the one turn that was asked", body["messages"])
	}
	if usage, _ := body["usage"].(map[string]any); usage == nil || usage["include"] != true {
		t.Errorf("usage = %#v, want the accounting opt-in kept", body["usage"])
	}
}

// TestADefaultRequestCarriesNoNullOutputCapEither is the failure that hides
// behind the one above. `"max_tokens": null` and `"max_tokens": 0` are both
// "the key is present", and a zero is worse than a large cap — it asks an
// endpoint for no answer at all — so the raw bytes are read for the key rather
// than the decoded map for a value.
func TestADefaultRequestCarriesNoNullOutputCapEither(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	recorded.mu.Lock()
	raw := string(recorded.raw[0])
	recorded.mu.Unlock()
	for _, knob := range []string{`"max_tokens"`, `"max_completion_tokens"`, `"temperature"`, `"reasoning"`} {
		if strings.Contains(raw, knob) {
			t.Errorf("the encoded body spells %s at all:\n%s", knob, raw)
		}
	}
}

// TestAnOperatorsOwnOutputCapStillTravels is the other half of the contract and
// the one that must not be lost in the tidying: absence is the DEFAULT, never a
// refusal to carry what a caller asked for.
func TestAnOperatorsOwnOutputCapStillTravels(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello"),
		ai.WithMaxTokens(777)); err != nil {
		t.Fatal(err)
	}
	if got := recorded.body(0)["max_tokens"]; got != float64(777) {
		t.Fatalf("max_tokens = %#v, want the caller's own 777", got)
	}
}

// TestAnOperatorsOwnThinkingLevelStillTravels pins the same thing for the knob
// a person actually turns. The seat pin is what `vendor/model:high` resolves to
// and it is explicit by construction, so it travels even to a model no catalog
// can vouch for.
func TestAnOperatorsOwnThinkingLevelStillTravels(t *testing.T) {
	client, recorded := newTestClient(t, Config{Effort: EffortHigh})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if reasoning == nil || reasoning["effort"] != string(EffortHigh) {
		t.Fatalf("reasoning = %#v, want the operator's own high", recorded.body(0)["reasoning"])
	}
	// The level is a request about thinking and never an output cap: asking for
	// one must not conjure the other.
	if _, capped := recorded.body(0)["max_tokens"]; capped {
		t.Fatalf("asking for a thinking level added an output cap: %#v", recorded.body(0))
	}
}

// TestAnOperatorsExplicitOffStillSuppressesThinking keeps the escape hatch the
// old default used to be. AFORGE_REASONING=off resolves to [EffortOff], and
// that is a REQUEST — the disable object — rather than the silence a default
// now sends.
func TestAnOperatorsExplicitOffStillSuppressesThinking(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if reasoning == nil || reasoning["enabled"] != false {
		t.Fatalf("reasoning = %#v, want the explicit disable", recorded.body(0)["reasoning"])
	}
}

// TestAStructuredToolCallKeepsItsShapeWithNoGenerationKnobs is the request
// shape a working turn actually sends. Tools, the tool choice and the schema
// are functional fields — a belt the model cannot see is a turn that cannot
// act — so they travel while the knobs beside them do not.
func TestAStructuredToolCallKeepsItsShapeWithNoGenerationKnobs(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	tools := []ai.ToolDefinition{{
		Type: "function",
		Function: ai.ToolFunction{
			Name:        "write_file",
			Description: "write a file",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
	}}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("write it"),
		ai.WithTools(tools)); err != nil {
		t.Fatal(err)
	}
	body := recorded.body(0)
	assertNoGenerationKnobs(t, "a tool-carrying completion", body)
	declared, _ := body["tools"].([]any)
	if len(declared) != 1 {
		t.Fatalf("tools = %#v, want the one tool that was declared", body["tools"])
	}
	tool, _ := declared[0].(map[string]any)
	function, _ := tool["function"].(map[string]any)
	if function["name"] != "write_file" {
		t.Fatalf("tool = %#v, want write_file declared whole", tool)
	}
	if _, parameters := function["parameters"]; !parameters {
		t.Fatalf("tool = %#v, want the schema carried with it", tool)
	}
}

// TestARetriedRequestIsStillFreeOfGenerationKnobs walks the path a real
// failure takes. A retry re-encodes the body from scratch, so it is its own
// opportunity to reintroduce a default — and a retry is exactly where nobody
// is watching the bytes.
func TestARetriedRequestIsStillFreeOfGenerationKnobs(t *testing.T) {
	forgetLanes(t)
	recorded := &capture{}
	var attempts int
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		attempts++
		if attempts == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"message":"upstream hiccup"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The backoff is recorded rather than slept, exactly as the rest of this
	// package's retry tests do it.
	client.wait = func(context.Context, time.Duration) error { return nil }

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Fatalf("the 500 was not retried (%d attempt(s)) — this test asserts nothing without the retry", attempts)
	}
	for index := range attempts {
		assertNoGenerationKnobs(t, "retry attempt "+string(rune('1'+index)), recorded.body(index))
	}
}
