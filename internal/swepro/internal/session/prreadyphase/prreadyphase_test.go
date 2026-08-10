package prreadyphase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type testPromptOps struct {
	requests []PromptRequest
	onPrompt func(PromptRequest) error
}

func (ops *testPromptOps) ResolvePromptParts(
	_ context.Context, template string,
) ([]any, error) {
	return []any{template}, nil
}

func (ops *testPromptOps) Prompt(_ context.Context, input any) (any, error) {
	request := input.(PromptRequest)
	ops.requests = append(ops.requests, request)
	if ops.onPrompt != nil {
		return nil, ops.onPrompt(request)
	}
	return nil, nil
}

func testDependencies() Dependencies {
	id := 0
	return Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"p/model/sub"} }),
		Sessions: SessionCreatorFunc(func(_ context.Context, _, agent string) (string, error) {
			return agent + "-session", nil
		}),
		NewMessageID: func() string {
			id++
			return "message"
		},
	}
}

func TestRunPRReadyPhaseCompletesBothStages(t *testing.T) {
	workspace := t.TempDir()
	prompt := &testPromptOps{}
	prompt.onPrompt = func(request PromptRequest) error {
		if err := os.MkdirAll(filepath.Join(workspace, ".codeaf"), 0o755); err != nil {
			return err
		}
		path := PlanRelativePath
		if request.Agent == "pr-formatter" {
			path = SummaryRelativePath
		}
		if err := os.WriteFile(filepath.Join(workspace, path), []byte("output"), 0o644); err != nil {
			return err
		}
		return errors.New("prompt errors are swallowed")
	}
	result, err := RunPRReadyPhase(context.Background(), Input{
		Workspace: workspace, ParentSessionID: "parent", PromptOps: prompt,
		UserPrompt: "Ship it", BaseSHA: "abc",
	}, testDependencies())
	if err != nil || result.Status != "completed" || result.PlanPath == nil ||
		result.SummaryPath == nil || len(prompt.requests) != 2 {
		t.Fatalf("result=%#v err=%v requests=%#v", result, err, prompt.requests)
	}
	plannerTools, ok := prompt.requests[0].Tools.(PlannerTools)
	if !ok || plannerTools != (PlannerTools{}) {
		t.Fatalf("planner tools=%#v", prompt.requests[0].Tools)
	}
	formatterTools, ok := prompt.requests[1].Tools.(FormatterTools)
	if !ok || formatterTools != (FormatterTools{}) {
		t.Fatalf("formatter tools=%#v", prompt.requests[1].Tools)
	}
}

func TestRunPRReadyPhaseFailuresAndNoModel(t *testing.T) {
	result, err := RunPRReadyPhase(context.Background(), Input{}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return nil }),
	})
	if err != nil || result.Status != "skipped" || result.Reason == nil {
		t.Fatalf("no-model result=%#v err=%v", result, err)
	}

	workspace := t.TempDir()
	prompt := &testPromptOps{}
	result, err = RunPRReadyPhase(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt,
	}, testDependencies())
	if err != nil || result.Status != "planner-failed" || len(prompt.requests) != 1 {
		t.Fatalf("planner failure result=%#v err=%v calls=%d", result, err, len(prompt.requests))
	}

	prompt = &testPromptOps{}
	prompt.onPrompt = func(request PromptRequest) error {
		if request.Agent == "pr-ready-planner" {
			if err := os.MkdirAll(filepath.Join(workspace, ".codeaf"), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(workspace, PlanRelativePath), []byte("plan"), 0o644)
		}
		return nil
	}
	result, err = RunPRReadyPhase(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt,
	}, testDependencies())
	if err != nil || result.Status != "formatter-failed" || result.PlanPath == nil {
		t.Fatalf("formatter failure result=%#v err=%v", result, err)
	}
}

func TestRunPRReadyPhaseAcceptsStaleDirectories(t *testing.T) {
	workspace := t.TempDir()
	for _, relative := range []string{PlanRelativePath, SummaryRelativePath} {
		if err := os.MkdirAll(filepath.Join(workspace, relative), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prompt := &testPromptOps{}
	result, err := RunPRReadyPhase(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt,
	}, testDependencies())
	if err != nil || result.Status != "completed" || len(prompt.requests) != 2 {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, len(prompt.requests))
	}
}
