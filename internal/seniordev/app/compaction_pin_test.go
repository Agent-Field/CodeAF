//go:build !windows

package app

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	configpkg "github.com/Agent-Field/codeaf/internal/seniordev/config"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/retrysched"
)

func TestParseContextLimitReadsTheNumberedOverflowMessages(t *testing.T) {
	for text, want := range map[string]float64{
		"This endpoint's maximum context length is 262144 tokens. However, you requested about 301000 tokens": 262144,
		"maximum prompt length is 131072":                              131072,
		"context length is only 200000 tokens":                         200000,
		"input exceeds the limit of 128000":                            128000,
		"prompt too large for model with 65536 maximum context length": 65536,
	} {
		if got, ok := parseContextLimit(text); !ok || got != want {
			t.Errorf("%q → %v,%v want %v", text, got, ok, want)
		}
	}
	for _, text := range []string{"prompt is too long", "context_length_exceeded", "400 (no body)", ""} {
		if _, ok := parseContextLimit(text); ok {
			t.Errorf("%q should not parse a limit", text)
		}
	}
}

// overflowBackend is a backend whose transport rejects every request with the
// given body, so DoStream returns the provider error the pin logic inspects.
func overflowBackend(t *testing.T, info configpkg.Info, status int, body string) (*modelAPIBackend, *bytes.Buffer) {
	t.Helper()
	cfg, err := newSeniorDevConfig(info)
	if err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	backend := &modelAPIBackend{
		api: testModelAPI, catalog: seniorDevCatalogFixture(t), config: cfg,
		events: newEventWriter(&events),
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return recordedResponse(request, status, "application/json", body), nil
		})},
	}
	return backend, &events
}

func overflowStream(t *testing.T, backend *modelAPIBackend, session string) error {
	t.Helper()
	projection, _, err := (seniorDevModels{backend: backend, agent: "coder"}).projection("openrouter", "fixture/vendor-model")
	if err != nil {
		t.Fatal(err)
	}
	client := seniorDevStreamClient{
		backend: backend, sessionID: session, agent: "coder", model: projection,
		client: &orclient.Client{
			BaseURL: backend.api.BaseURL, Fetcher: backend.fetch,
			Compatibility: orclient.CompatibilityCompatible,
		},
	}
	_, err = client.DoStream(context.Background(), orclient.RequestParams{
		ModelID: "fixture/vendor-model",
		Prompt:  []msgmodel.ModelMessage{{Role: "user", Content: "hello"}},
	})
	return err
}

const overflowBody = `{"error":{"message":"This endpoint's maximum context length is 131072 tokens. However, you requested about 150000 tokens (150000 of text input). Please reduce the length of either one.","code":400}}`

func TestContextOverflowPinsCapacityUnderTheWindowPolicy(t *testing.T) {
	backend, events := overflowBackend(t, configpkg.Info{"compaction": map[string]any{"policy": "window"}}, 400, overflowBody)

	err := overflowStream(t, backend, "ses-pin")
	if err == nil || !retrysched.IsContextOverflow(retrysched.FromError(err)) {
		t.Fatalf("DoStream error = %v, want a context-overflow rejection passed through", err)
	}
	// Fixture output limit 12,000 is the reservation: 131,072 − 12,000. The
	// fixture's own input window (220,000 − 12,000 = 208,000) is wider, so
	// the pin is what tightens.
	pinned, ok := backend.pinnedCapacityFor("ses-pin")
	if !ok || pinned != 119_072 {
		t.Fatalf("pinned capacity = %v,%v want 119072", pinned, ok)
	}
	if _, ok := backend.pinnedCapacityFor("ses-other"); ok {
		t.Fatal("a pin must be per session")
	}
	got := events.String()
	for _, want := range []string{
		`"stage":"compaction-capacity"`, `"status":"pinned"`, `"source":"parsed"`,
		`"limit_tokens":131072`, `"reservation_tokens":12000`, `"pinned_capacity_tokens":119072`,
		`"capacity_tokens":119072`, `"high_tokens":71443`, `"low_tokens":47628`,
		`"session_id":"ses-pin"`, `"model_id":"fixture/vendor-model"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pinned event lacks %s: %s", want, got)
		}
	}
	// The session's compaction config now carries the pin as capacity_tokens,
	// and the project config is untouched for other sessions.
	cfg, err := backend.overflowConfigFor("ses-pin")
	if err != nil || cfg.Compaction.CapacityTokens == nil || *cfg.Compaction.CapacityTokens != 119_072 {
		t.Fatalf("session config = %+v, %v", cfg.Compaction, err)
	}
	other, _ := backend.overflowConfigFor("ses-other")
	if other.Compaction.CapacityTokens != nil {
		t.Fatalf("other session inherited the pin: %+v", other.Compaction)
	}
	// A later rejection naming a larger limit never raises the pin.
	backend.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return recordedResponse(request, 400, "application/json", `{"error":{"message":"maximum context length is 262144 tokens","code":400}}`), nil
	})}
	_ = overflowStream(t, backend, "ses-pin")
	if pinned, _ := backend.pinnedCapacityFor("ses-pin"); pinned != 119_072 {
		t.Fatalf("pin was raised to %v", pinned)
	}
	// And the configured event for a later turn in that session records it.
	var provenance bytes.Buffer
	runtime := &runtimeAdapter{config: backend.config, backend: backend, events: newEventWriter(&provenance)}
	if _, err := runtime.configureTurn(turn{Agent: "coder", SessionID: "ses-pin", AgentMarkdown: "p", ProviderID: "openrouter", ModelID: "fixture/vendor-model"}); err != nil {
		t.Fatal(err)
	}
	if got := provenance.String(); !strings.Contains(got, `"pinned_capacity_tokens":119072`) || !strings.Contains(got, `"high_tokens":71443`) {
		t.Fatalf("configured event does not carry the pin: %s", got)
	}
}

func TestContextOverflowWithoutANumberPinsNothing(t *testing.T) {
	backend, events := overflowBackend(t, configpkg.Info{"compaction": map[string]any{"policy": "window"}}, 400,
		`{"error":{"message":"prompt is too long for this model","code":400}}`)
	if err := overflowStream(t, backend, "ses-unparsed"); err == nil {
		t.Fatal("expected the rejection to pass through")
	}
	if _, ok := backend.pinnedCapacityFor("ses-unparsed"); ok {
		t.Fatal("an unparsed rejection must not pin")
	}
	got := events.String()
	if !strings.Contains(got, `"status":"overflow-unpinned"`) || !strings.Contains(got, `"source":"unparsed"`) {
		t.Fatalf("unparsed rejection not recorded: %s", got)
	}
	cfg, _ := backend.overflowConfigFor("ses-unparsed")
	if cfg.Compaction.CapacityTokens != nil {
		t.Fatalf("config changed without a pin: %+v", cfg.Compaction)
	}
}

func TestNonOverflowErrorsDoNotPin(t *testing.T) {
	backend, events := overflowBackend(t, configpkg.Info{"compaction": map[string]any{"policy": "window"}}, 429,
		`{"error":{"message":"rate limited","code":429}}`)
	if err := overflowStream(t, backend, "ses-429"); err == nil {
		t.Fatal("expected the error to pass through")
	}
	if _, ok := backend.pinnedCapacityFor("ses-429"); ok || strings.Contains(events.String(), "compaction-capacity") {
		t.Fatalf("a non-overflow error pinned or emitted: %s", events.String())
	}
}
