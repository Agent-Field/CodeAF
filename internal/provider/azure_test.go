package provider

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestAnAzureServiceGetsItsKeyAndFieldsInItsOwnDialect pins the three things an
// Azure OpenAI gateway refuses with a 400 or a 401 when they arrive in the
// router's spelling, each observed against a live gateway on 2026-10-27: a
// bearer token instead of the `api-key` header, the `reasoning` object instead
// of `reasoning_effort`, and `max_tokens` instead of `max_completion_tokens`.
func TestAnAzureServiceGetsItsKeyAndFieldsInItsOwnDialect(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		Model: "gpt-5.6-sol-2026-07-09", Direct: true, Effort: EffortHigh,
		KeyHeader: "api-key", OpenAIDialect: true,
	})
	ceiling := 4096
	withCeiling := func(request *ai.Request) error { request.MaxTokens = &ceiling; return nil }
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello"), withCeiling); err != nil {
		t.Fatal(err)
	}
	header := recorded.headers[0]
	if got := header.Get("api-key"); got != "test-key" {
		t.Errorf("api-key header = %q, want the configured key", got)
	}
	if got := header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want no bearer beside the api-key header", got)
	}
	body := recorded.body(0)
	if body["reasoning_effort"] != string(EffortHigh) {
		t.Errorf("reasoning_effort = %#v, want the operator's high", body["reasoning_effort"])
	}
	if _, sent := body["reasoning"]; sent {
		t.Errorf("reasoning object sent to an OpenAI-dialect service: %#v", body["reasoning"])
	}
	// The value is the budget step's to choose (it widens a cap for thinking
	// room); what is pinned here is which field carries it.
	if capped, _ := body["max_completion_tokens"].(float64); capped < float64(ceiling) {
		t.Errorf("max_completion_tokens = %#v, want at least %d", body["max_completion_tokens"], ceiling)
	}
	if _, sent := body["usage"]; sent {
		t.Errorf("router usage opt-in sent to an OpenAI-dialect service: %#v", body["usage"])
	}
	if _, sent := body["max_tokens"]; sent {
		t.Errorf("max_tokens sent to an OpenAI-dialect service: %#v", body["max_tokens"])
	}
}

// TestAnOrdinaryServiceKeepsTheBearer is the other half: an empty KeyHeader is
// every service that is not Azure, and it must still be a bearer token.
func TestAnOrdinaryServiceKeepsTheBearer(t *testing.T) {
	client, recorded := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := recorded.headers[0].Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q, want the bearer", got)
	}
	if got := recorded.headers[0].Get("api-key"); got != "" {
		t.Errorf("api-key = %q, want none on an ordinary service", got)
	}
}

// TestOpenAIEffortSpellsEveryRung keeps the one mapping from the router's
// ladder onto OpenAI's single word.
func TestOpenAIEffortSpellsEveryRung(t *testing.T) {
	for _, tc := range []struct {
		level  Effort
		budget int
		want   Effort
	}{
		{EffortNone, 0, EffortNone},
		{EffortOff, 0, "none"},
		{EffortLow, 0, EffortLow},
		{EffortHigh, 0, EffortHigh},
		{EffortHigh, 32000, "xhigh"},
	} {
		if got := openAIEffort(tc.level, tc.budget); got != tc.want {
			t.Errorf("openAIEffort(%q, %d) = %q, want %q", tc.level, tc.budget, got, tc.want)
		}
	}
}
