package config

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE HEADLESS DOOR SENDS WHAT THE CHAT SENDS ─────────────────────────────
//
// internal/session builds its adapter with a model, a timeout and its seams and
// nothing else, so a v3 turn has always gone out with no output cap. This file
// used to build a different request for the same models: a 32k floor raised to
// the completion reserve, an `off` reasoning object on the planner and another
// on the executor. Two doors into one process are two answers to "what did we
// actually send", and the one nobody watches is the one that drifts.

// headlessRequestBody sends one completion through the client this package
// builds and returns the bytes that reached the endpoint.
func headlessRequestBody(t *testing.T, settings Config, ctx context.Context) string {
	t.Helper()
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		raw = string(payload)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	settings.APIKey, settings.BaseURL = "k", server.URL
	client, err := settings.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(ctx,
		[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hi"}}}}); err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestTheHeadlessDefaultsCarryNoGenerationParameter is the contract in one
// place: with nobody having configured anything, neither the planning context
// nor the executor's puts an output cap or a reasoning object on the wire.
func TestTheHeadlessDefaultsCarryNoGenerationParameter(t *testing.T) {
	settings := Config{
		Model:         DefaultModel,
		Timeout:       DefaultTimeout,
		Reasoning:     DefaultReasoning,
		ExecReasoning: DefaultExecReasoning,
	}
	for _, pass := range []struct {
		what string
		ctx  context.Context
	}{
		{"the planning pass", settings.Context(context.Background(), "plan this")},
		{"the executor's pass", settings.ExecContext(settings.Context(context.Background(), "do this"))},
	} {
		body := headlessRequestBody(t, settings, pass.ctx)
		for _, knob := range []string{
			`"max_tokens"`, `"max_completion_tokens"`, `"reasoning"`, `"temperature"`, `"top_p"`,
		} {
			if strings.Contains(body, knob) {
				t.Errorf("%s carried %s:\n%s", pass.what, knob, body)
			}
		}
		// And it still carries what the call needs to work at all.
		if !strings.Contains(body, `"model"`) || !strings.Contains(body, `"messages"`) {
			t.Errorf("%s lost the model or the messages:\n%s", pass.what, body)
		}
	}
}

// TestTheDefaultsThemselvesAreAbsenceAndNotAnEconomy pins the values rather
// than the bytes, because the wire test above would also pass if a default of
// `off` were being dropped somewhere further down — and dropping an operator's
// explicit `off` is the opposite defect.
func TestTheDefaultsThemselvesAreAbsenceAndNotAnEconomy(t *testing.T) {
	if DefaultReasoning != provider.EffortNone {
		t.Errorf("DefaultReasoning = %q, want absence — off is a request, not silence", DefaultReasoning)
	}
	if DefaultExecReasoning != provider.EffortNone {
		t.Errorf("DefaultExecReasoning = %q, want absence", DefaultExecReasoning)
	}
}

// TestAnOperatorsExplicitReasoningStillReachesTheWire is the escape hatch the
// old default used to be, and it must keep working: AFORGE_REASONING=off is how
// somebody asks for the thinking pass to be suppressed outright.
func TestAnOperatorsExplicitReasoningStillReachesTheWire(t *testing.T) {
	for _, want := range []struct {
		asked provider.Effort
		spelt string
	}{
		{provider.EffortOff, `"enabled":false`},
		{provider.EffortHigh, `"effort":"high"`},
	} {
		settings := Config{Model: DefaultModel, Timeout: DefaultTimeout, Reasoning: want.asked}
		body := headlessRequestBody(t, settings, settings.Context(context.Background(), "plan this"))
		if !strings.Contains(body, want.spelt) {
			t.Errorf("an operator asking for %q sent %s, want %s", want.asked, body, want.spelt)
		}
		// An explicit thinking level is not an output cap, and asking for one
		// must not bring the other back.
		if strings.Contains(body, `"max_tokens"`) {
			t.Errorf("asking for %q added an output cap:\n%s", want.asked, body)
		}
	}
}
