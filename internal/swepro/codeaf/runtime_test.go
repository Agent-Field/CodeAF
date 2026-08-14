package codeaf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

type capturingBackend struct {
	turns []turn
}

type scratchCreatingBackend struct{ root string }

func (backend scratchCreatingBackend) Run(_ context.Context, request turn) (turnResult, error) {
	if err := os.MkdirAll(filepath.Join(backend.root, request.SessionID, "go-build"), 0o755); err != nil {
		return turnResult{}, err
	}
	return turnResult{Text: "done"}, nil
}

func TestRunLeafTearsDownScratchOnCompletionContract(t *testing.T) {
	// Parity audit contract: the runtime invokes scratch teardown when a leaf
	// finishes, rather than relying only on the serve process's startup sweep.
	root := t.TempDir()
	t.Setenv("CODEAF_SCRATCH_ROOT", root)
	runtime := newRuntime(t.TempDir(), scratchCreatingBackend{root: root})
	defer runtime.Close()
	result, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, Worktree: runtime.workspace,
		TaskID: "scratch-contract", Prompt: "finish",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, result.SessionID)); !os.IsNotExist(err) {
		t.Fatalf("leaf scratch remains after completion: %v", err)
	}
}

func TestRunLeafDoesNotTeardownScratchBeforeBackendStartsContract(t *testing.T) {
	// Parity audit contract 7: failures before backend execution do not reclaim
	// caches belonging to another user of the already-claimed leaf session.
	root := t.TempDir()
	t.Setenv("CODEAF_SCRATCH_ROOT", root)
	runtime := newRuntime(t.TempDir(), &capturingBackend{})
	defer runtime.Close()
	input := scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, Worktree: runtime.workspace,
		TaskID: "scratch-early-failure", Attempt: 1, Prompt: "finish",
	}
	sessionID, err := runtime.leafSession(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, sessionID, "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	runtime.backend = nil
	result, err := runtime.RunLeaf(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "backend is required") {
		t.Fatalf("early RunLeaf error = %v", err)
	}
	_ = result
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("early failure removed unstarted leaf scratch: %v", err)
	}
}

func (backend *capturingBackend) Run(_ context.Context, request turn) (turnResult, error) {
	backend.turns = append(backend.turns, request)
	return turnResult{}, nil
}

func TestRunLeafFiltersDefinitionsForActualModel(t *testing.T) {
	// Validation contract 4 (revised by D9): the edit-strategy gate that once
	// gave gpt-family models apply_patch and everyone else edit/write is gone, so
	// every coder — deepseek included — gets edit, write, and apply_patch
	// together. Two models are still exercised to assert the toolset is now
	// model-independent.
	tests := []struct {
		modelID string
		want    []string
	}{
		{
			modelID: "deepseek/deepseek-v4-pro",
			want:    []string{"question", "bash", "read", "glob", "grep", "edit", "write", "webfetch", "apply_patch"},
		},
		{
			modelID: "openai/gpt-6.1-codex",
			want:    []string{"question", "bash", "read", "glob", "grep", "edit", "write", "webfetch", "apply_patch"},
		},
	}
	for _, test := range tests {
		t.Run(test.modelID, func(t *testing.T) {
			backend := &capturingBackend{}
			runtime := newRuntime(t.TempDir(), backend)
			_, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
				Agent:      scheduler.AgentInfo{Name: "coder"},
				ProviderID: "openrouter", ModelID: test.modelID,
				Worktree: t.TempDir(), Prompt: "test",
			})
			if err != nil {
				t.Fatalf("RunLeaf: %v", err)
			}
			if len(backend.turns) != 1 {
				t.Fatalf("backend calls = %d, want 1", len(backend.turns))
			}
			if got := requestToolNames(backend.turns[0].Tools); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tools = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunLeafToolListMatchesAgentAndModelContract(t *testing.T) {
	// Validation contract C1: the live leaf request has the exact TS-visible
	// builtin names after agent permission and model edit-strategy filtering.
	tests := []struct {
		agent string
		model string
		want  []string
	}{
		// aforge-embed: D9 — the model edit-strategy gate is gone; the coder and
		// root-orchestrator both get edit, write, and apply_patch regardless of
		// model. The rows still pin agent-based filtering: coder drops task/plandb.
		{"coder", "deepseek/deepseek-v4-pro", []string{"question", "bash", "read", "glob", "grep", "edit", "write", "webfetch", "apply_patch"}},
		{"root-orchestrator", "deepseek/deepseek-v4-pro", []string{"question", "bash", "read", "glob", "grep", "edit", "write", "task", "webfetch", "plandb", "apply_patch"}},
		{"root-orchestrator", "openai/gpt-6.1-codex", []string{"question", "bash", "read", "glob", "grep", "edit", "write", "task", "webfetch", "plandb", "apply_patch"}},
	}
	for _, test := range tests {
		t.Run(test.agent+"/"+test.model, func(t *testing.T) {
			backend := &capturingBackend{}
			runtime := newRuntime(t.TempDir(), backend)
			t.Cleanup(runtime.Close)
			_, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
				Agent: scheduler.AgentInfo{Name: test.agent}, ProviderID: "openrouter",
				ModelID: test.model, Worktree: runtime.workspace, Prompt: "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := requestToolNames(backend.turns[0].Tools); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tools = %v, want %v", got, test.want)
			}
		})
	}
}

type taskSpawningBackend struct{ request turn }

func (backend *taskSpawningBackend) Run(_ context.Context, request turn) (turnResult, error) {
	backend.request = request
	return turnResult{SessionID: request.SessionID, Text: "child finished"}, nil
}

func TestTaskToolUsesDurableRuntimeSessionPathContract(t *testing.T) {
	// Validation contract C1: the registered task tool runs a real child through
	// the runtime's durable session creation path rather than a stub executor.
	workspace := t.TempDir()
	backend := &taskSpawningBackend{}
	runtime := newRuntime(workspace, backend)
	t.Cleanup(runtime.Close)
	runtime.pool = poolResolver{high: []string{"provider/model"}}
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: workspace, Worktree: workspace,
	})
	parentID := runtime.nextID("session")
	if err := runtime.ensureRootSession(ctx, parentID, "parent", "root-orchestrator"); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"description":"inspect child","prompt":"Inspect the parser","subagent_type":"explorer"}`)
	result, err := runtime.registry.Execute(ctx, steploop.ToolCall{
		ID: "call", Name: "task", Input: input, SessionID: parentID, Agent: "root-orchestrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend.request.ParentSessionID != parentID || backend.request.Agent != "explorer" || backend.request.SessionID == "" {
		t.Fatalf("child request = %+v", backend.request)
	}
	if _, err := runtime.durable.sessions.Get(ctx, backend.request.SessionID); err != nil {
		t.Fatalf("durable child session missing: %v", err)
	}
	if !strings.Contains(result.Output, "task_id: "+backend.request.SessionID) || !strings.Contains(result.Output, "child finished") {
		t.Fatalf("task output = %q", result.Output)
	}
}

func TestOpenRouterEndpoint(t *testing.T) {
	// Finding 8: both bare proxy roots and already-versioned roots produce one
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

// aforge-embed: D9 — apply_patch is no longer model-gated, so a deepseek coder
// keeps it alongside edit; only the explicitly-disabled write drops out.
func TestModelFilteringPreservesDisabledTools(t *testing.T) {
	runtime := newRuntime(t.TempDir(), &capturingBackend{})
	t.Cleanup(runtime.Close)
	got := requestToolNames(runtime.definitionsFor(
		"openrouter", "deepseek/deepseek-v4-pro", "coder", map[string]bool{"write": true},
	))
	want := []string{"question", "bash", "read", "glob", "grep", "edit", "webfetch", "apply_patch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
}

func TestDefinitionsForCodeafProviderIncludesWebSearch(t *testing.T) {
	for _, name := range []string{
		"CODEAF_EXPERIMENTAL", "CODEAF_ENABLE_EXA", "CODEAF_EXPERIMENTAL_EXA",
		"CODEAF_ENABLE_PARALLEL", "CODEAF_EXPERIMENTAL_PARALLEL",
	} {
		t.Setenv(name, "")
	}
	runtime := newRuntime(t.TempDir(), &capturingBackend{})
	t.Cleanup(runtime.Close)
	got := requestToolNames(runtime.definitionsFor(
		"codeaf", "deepseek/deepseek-v4-pro", "coder", nil,
	))
	if !slices.Contains(got, "websearch") {
		t.Fatalf("codeaf tools = %v", got)
	}
}

func requestToolNames(definitions []steploop.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Provider.Name)
	}
	return names
}

type boundaryOnlyBackend struct{}

func (boundaryOnlyBackend) Run(context.Context, turn) (turnResult, error) {
	return turnResult{Parts: []scheduler.LeafPart{{Type: "compaction", Text: ""}}}, nil
}

func TestRunLeafTreatsBareCompactionBoundaryAsEmptyResult(t *testing.T) {
	// Final-scan finding 3: a boundary with no continuation output must not
	// satisfy the scheduler's non-empty dispatch check.
	runtime := newRuntime(t.TempDir(), boundaryOnlyBackend{})
	t.Cleanup(runtime.Close)
	result, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "deepseek/deepseek-v4-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 0 {
		t.Fatalf("expected empty parts for bare boundary, got %+v", result.Parts)
	}
}

type directLeafCapturingBackend struct{ request turn }

func (backend *directLeafCapturingBackend) Run(_ context.Context, request turn) (turnResult, error) {
	backend.request = request
	return turnResult{Text: "done"}, nil
}

func TestDirectLeafClearsInstructionClaimsAfterAssistant(t *testing.T) {
	// Finding 6 contract: direct-leaf execution receives the same per-assistant
	// instruction-claim cleanup hook as runtime and scheduler leaves.
	workspace := t.TempDir()
	backend := &directLeafCapturingBackend{}
	runner := &pipeline{
		workspace: workspace,
		runtime:   newRuntime(workspace, backend),
		pool:      poolResolver{high: []string{"provider/model"}},
	}
	if err := runner.runDirectLeaf(context.Background(), "goal", "coder", "project", "root"); err != nil {
		t.Fatal(err)
	}
	if backend.request.AfterAssistant == nil {
		t.Fatal("direct leaf omitted AfterAssistant instruction cleanup")
	}
}
