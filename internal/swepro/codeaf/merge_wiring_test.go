package codeaf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/merger"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/mergerecovery"
)

type mergeWiringBackend func(context.Context, turn) (turnResult, error)

func (backend mergeWiringBackend) Run(ctx context.Context, request turn) (turnResult, error) {
	return backend(ctx, request)
}

func TestSchedulerOptionsWireMergerAndRecoveryUnconditionally(t *testing.T) {
	workspace := t.TempDir()
	runtime := newRuntime(workspace, mergeWiringBackend(func(context.Context, turn) (turnResult, error) {
		return turnResult{}, nil
	}))
	t.Cleanup(runtime.Close)
	runner := &pipeline{
		workspace: workspace, runtime: runtime,
		pool: poolResolver{high: []string{"openrouter/test-model"}},
	}
	options := runner.schedulerOptions(false)
	if options.DefaultMerge.MergerDispatcher == nil || options.DefaultMerge.RecoveryClient == nil {
		t.Fatalf("default merge wiring = %#v", options.DefaultMerge)
	}
	if _, ok := options.DefaultMerge.MergerDispatcher.(runtimeMergerDispatcher); !ok {
		t.Fatalf("merger dispatcher = %T", options.DefaultMerge.MergerDispatcher)
	}
	if _, ok := options.DefaultMerge.RecoveryClient.(runtimeMergeRecoveryClient); !ok {
		t.Fatalf("recovery client = %T", options.DefaultMerge.RecoveryClient)
	}
}

func TestRuntimeMergerDispatcherUsesHighTierAgentJSON(t *testing.T) {
	workspace := t.TempDir()
	outputPath := filepath.Join(workspace, ".codeaf", "agents", "merger", "leaf.json")
	var captured turn
	runtime := newRuntime(workspace, mergeWiringBackend(func(_ context.Context, request turn) (turnResult, error) {
		captured = request
		body := []byte(`{"result":"resolved","reason":"combined both intents","files_touched":[{"file":"value.go","summary":"merged"}],"unresolved":null}`)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return turnResult{}, err
		}
		return turnResult{Text: "done"}, os.WriteFile(outputPath, body, 0o644)
	}))
	runtime.pool = poolResolver{high: []string{"openrouter/high-model"}}
	t.Cleanup(runtime.Close)

	result, err := (runtimeMergerDispatcher{runtime: runtime}).Dispatch(
		context.Background(), merger.DispatchRequest{
			Agent: "merger", ParentSessionID: "parent", Workspace: workspace,
			TaskPrompt: "semantic prompt", OutputPath: outputPath,
			Fallback: merger.MergerFallback, MaxRetries: 1, Label: "merger",
			Tools: merger.ToolOverrides{Edit: true, ApplyPatch: true}, TimeoutMS: 900_000,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Result != "resolved" || result.Data.Reason != "combined both intents" {
		t.Fatalf("result = %#v", result)
	}
	if captured.Agent != "merger" || captured.ParentSessionID != "parent" ||
		captured.ProviderID != "openrouter" || captured.ModelID != "high-model" {
		t.Fatalf("captured merger turn = %#v", captured)
	}
}

func TestRuntimeMergeRecoveryRunsRawToolLoopWithSelectedLanguage(t *testing.T) {
	workspace := t.TempDir()
	var captured turn
	runtime := newRuntime(workspace, mergeWiringBackend(func(ctx context.Context, request turn) (turnResult, error) {
		captured = request
		_, err := request.Execute(ctx, steploop.ToolCall{
			Name: "report", Input: json.RawMessage(`{"ok":true,"summary":"landed leaf branch"}`),
		})
		return turnResult{CostUSD: 0.25}, err
	}))
	t.Cleanup(runtime.Close)

	result := mergerecovery.LLMMergeRecovery(mergerecovery.Input{
		Language:  runtimeLanguageModel{ProviderID: "openrouter", ModelID: "high-model"},
		Client:    runtimeMergeRecoveryClient{runtime: runtime},
		Workspace: workspace, Worktree: workspace, MergeAt: workspace,
		TaskID: "leaf", TaskTitle: "Leaf", TargetBranch: "master", SourceBranch: "plandb/leaf",
		PriorFailure: mergerecovery.PriorFailure{Summary: "rebase conflicted"},
	})
	if !result.OK || result.Summary != "landed leaf branch" || result.ToolCalls != 0 {
		t.Fatalf("recovery result = %#v", result)
	}
	if !captured.RawModelCall || !captured.DisableRetries || captured.Temperature == nil || *captured.Temperature != 0.1 {
		t.Fatalf("raw recovery controls = %#v", captured)
	}
	if captured.ProviderID != "openrouter" || captured.ModelID != "high-model" ||
		captured.MaxSteps == nil || *captured.MaxSteps != 24 {
		t.Fatalf("raw recovery model/steps = %#v", captured)
	}
	wantTools := []string{"git_leaf", "git_target", "read", "write", "report"}
	gotTools := make([]string, 0, len(captured.Tools))
	for _, definition := range captured.Tools {
		gotTools = append(gotTools, definition.Provider.Name)
	}
	if !reflect.DeepEqual(gotTools, wantTools) {
		t.Fatalf("recovery tools = %#v, want %#v", gotTools, wantTools)
	}
	if runtime.cost() != 0.25 {
		t.Fatalf("recovery cost = %v", runtime.cost())
	}
}

func TestRawRecoveryBypassesNormalGPTToolFiltering(t *testing.T) {
	tools := []orclient.Tool{{Name: "git_leaf"}, {Name: "write"}, {Name: "report"}}
	client := codeafStreamClient{
		model: orclient.Model{ProviderID: "openrouter", ID: "openai/gpt-5"},
		agent: "merge-recovery", bypassToolFilter: true,
	}
	if got := client.visibleTools(tools); !reflect.DeepEqual(got, tools) {
		t.Fatalf("raw recovery tools = %#v, want %#v", got, tools)
	}
	client.bypassToolFilter = false
	if got := client.visibleTools(tools); len(got) != 2 || got[0].Name != "git_leaf" || got[1].Name != "report" {
		t.Fatalf("normal GPT-filtered tools = %#v", got)
	}
}

func TestSchedulerProviderReturnsUsableLanguageModelForGoModule(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/merge\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := schedulerProvider{}
	model, err := provider.GetModel(context.Background(), "openrouter", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	language, err := provider.GetLanguage(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	if language == nil {
		t.Fatal("Go module dispatch language is nil")
	}
	if got := language.(runtimeLanguageModel); got.ProviderID != "openrouter" || got.ModelID != "test-model" {
		t.Fatalf("language = %#v", got)
	}
}
