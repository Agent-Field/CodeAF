//go:build !windows

package app

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

type capturingBackend struct {
	turns []turn
}

func (backend *capturingBackend) Run(_ context.Context, request turn) (turnResult, error) {
	backend.turns = append(backend.turns, request)
	return turnResult{}, nil
}

func TestOpenRouterEndpoint(t *testing.T) {
	// Both bare proxy roots and already-versioned roots produce one
	// /api/v1 segment before chat/completions.
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "http://proxy", want: "http://proxy/api/v1/chat/completions"},
		{input: "http://proxy/", want: "http://proxy/api/v1/chat/completions"},
		{input: "http://proxy/api/v1", want: "http://proxy/api/v1/chat/completions"},
		{input: "http://proxy/api/v1/", want: "http://proxy/api/v1/chat/completions"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := openRouterEndpoint(test.input); got != test.want {
				t.Fatalf("openRouterEndpoint(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestDefaultBackendHasNoHTTPClientWallClockTimeout(t *testing.T) {
	configured, ok := defaultBackend("").(*openRouterBackend)
	if !ok {
		t.Fatalf("defaultBackend type = %T, want *openRouterBackend", defaultBackend(""))
	}
	if configured.client == nil {
		t.Fatal("default backend has no HTTP client")
	}
	if configured.client.Timeout != 0 {
		t.Fatalf("default HTTP client timeout = %s, want disabled", configured.client.Timeout)
	}
}

func TestModelFilteringPreservesDisabledTools(t *testing.T) {
	runtime := newRuntime(t.TempDir(), &capturingBackend{})
	t.Cleanup(runtime.Close)
	got := requestToolNames(runtime.definitionsFor(
		"openrouter", "deepseek/deepseek-v4-pro", "coder", map[string]bool{"write": true},
	))
	want := []string{"question", "bash", "read", "glob", "grep", "edit", "webfetch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
}

func TestDefinitionsForSeniorDevProviderIncludesWebSearch(t *testing.T) {
	for _, name := range []string{
		"SENIOR_DEV_EXPERIMENTAL", "SENIOR_DEV_ENABLE_EXA", "SENIOR_DEV_EXPERIMENTAL_EXA",
		"SENIOR_DEV_ENABLE_PARALLEL", "SENIOR_DEV_EXPERIMENTAL_PARALLEL",
	} {
		t.Setenv(name, "")
	}
	runtime := newRuntime(t.TempDir(), &capturingBackend{})
	t.Cleanup(runtime.Close)
	got := requestToolNames(runtime.definitionsFor(
		"senior-dev", "deepseek/deepseek-v4-pro", "coder", nil,
	))
	if !slices.Contains(got, "websearch") {
		t.Fatalf("senior-dev tools = %v", got)
	}
}

func requestToolNames(definitions []steploop.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Provider.Name)
	}
	return names
}

type turnCapturingBackend struct{ request turn }

func (backend *turnCapturingBackend) Run(_ context.Context, request turn) (turnResult, error) {
	backend.request = request
	return turnResult{Text: "done"}, nil
}

func TestSoloTurnClearsInstructionClaimsAfterAssistant(t *testing.T) {
	// The coding turn must receive the per-assistant instruction-claim cleanup
	// hook. The solo pipeline has exactly one such turn, so if it omits the
	// hook nothing else will supply it.
	workspace := t.TempDir()
	backend := &turnCapturingBackend{}
	runner := &pipeline{
		workspace: workspace,
		runtime:   newRuntime(workspace, backend),
		pool:      poolResolver{high: []string{"provider/model"}},
		events:    newEventWriter(discardWriter{}),
		notes:     discardWriter{},
	}
	if _, err := runner.soloTurn(context.Background(), "goal", "do the thing"); err != nil {
		t.Fatal(err)
	}
	if backend.request.AfterAssistant == nil {
		t.Fatal("the solo coding turn omitted AfterAssistant instruction cleanup")
	}
	if backend.request.Agent != "coder" {
		t.Fatalf("solo turn agent = %q, want coder", backend.request.Agent)
	}
}
