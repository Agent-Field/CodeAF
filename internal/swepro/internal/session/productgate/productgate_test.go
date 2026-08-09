package productgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type scriptedPromptOps struct {
	resolved []any
	requests []PromptRequest
	onPrompt func(call int, request PromptRequest) error
}

func (ops *scriptedPromptOps) ResolvePromptParts(
	_ context.Context, _ string,
) ([]any, error) {
	return ops.resolved, nil
}

func (ops *scriptedPromptOps) Prompt(
	_ context.Context, input any,
) (any, error) {
	request := input.(PromptRequest)
	ops.requests = append(ops.requests, request)
	if ops.onPrompt != nil {
		return nil, ops.onPrompt(len(ops.requests), request)
	}
	return nil, nil
}

func TestRunProductGateRetriesSameSessionAndWrites(t *testing.T) {
	workspace := t.TempDir()
	prdPath := filepath.Join(workspace, ".codeaf", "plan", "product.md")
	prompt := &scriptedPromptOps{resolved: []any{"resolved-prompt"}}
	prompt.onPrompt = func(call int, _ PromptRequest) error {
		if call == 2 {
			if err := os.WriteFile(prdPath, []byte("# Product"), 0o644); err != nil {
				return err
			}
		}
		return errors.New("prompt failure is swallowed")
	}
	sessionCalls := 0
	idCalls := 0
	result, err := RunProductGate(context.Background(), Input{
		Workspace: workspace, ParentSessionID: "parent", PromptOps: prompt,
		UserPrompt: "Build it",
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"provider/model/sub"} }),
		Sessions: SessionCreatorFunc(func(context.Context, string, string) (string, error) {
			sessionCalls++
			return "pm-session", nil
		}),
		NewMessageID: func() string {
			idCalls++
			return "msg-" + intString(idCalls)
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "wrote" || result.PRDPath != prdPath ||
		sessionCalls != 1 || len(prompt.requests) != 2 || idCalls != 3 {
		t.Fatalf("result=%#v sessions=%d ids=%d requests=%#v",
			result, sessionCalls, idCalls, prompt.requests)
	}
	if prompt.requests[0].SessionID != "pm-session" ||
		prompt.requests[1].SessionID != "pm-session" ||
		prompt.requests[0].MessageID != "msg-2" ||
		prompt.requests[1].MessageID != "msg-3" {
		t.Fatalf("session/id behavior drifted: %#v", prompt.requests)
	}
	if len(prompt.requests[0].Parts) != 2 || len(prompt.requests[1].Parts) != 3 {
		t.Fatalf("part counts=%d/%d", len(prompt.requests[0].Parts), len(prompt.requests[1].Parts))
	}
	retry, ok := prompt.requests[1].Parts[1].(PromptPart)
	if !ok || retry != BuildRetryReminder(
		2, prdPath, "attempt 1 ended without writing "+prdPath,
	) {
		t.Fatalf("retry reminder=%#v", prompt.requests[1].Parts[1])
	}
}

func TestRunProductGateFailureAndNoModel(t *testing.T) {
	workspace := t.TempDir()
	result, err := RunProductGate(context.Background(), Input{
		Workspace: workspace, PromptOps: &scriptedPromptOps{}, UserPrompt: "x",
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return nil }),
	})
	if err != nil || result.Status != "failed" || result.Reason == nil ||
		*result.Reason != "no HIGH-tier model available" {
		t.Fatalf("no-model result=%#v err=%v", result, err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".codeaf")); !os.IsNotExist(statErr) {
		t.Fatalf("output directory should not be created before model resolution: %v", statErr)
	}

	prompt := &scriptedPromptOps{}
	result, err = RunProductGate(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt, UserPrompt: "x",
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"p/m"} }),
		Sessions: SessionCreatorFunc(func(context.Context, string, string) (string, error) {
			return "session", nil
		}),
	})
	if err != nil || result.Status != "failed" || result.Reason == nil ||
		len(prompt.requests) != 3 {
		t.Fatalf("failure result=%#v err=%v calls=%d", result, err, len(prompt.requests))
	}
}

func TestRunProductGateExistenceOnlyAcceptsDirectory(t *testing.T) {
	workspace := t.TempDir()
	prdPath := filepath.Join(workspace, ".codeaf", "plan", "product.md")
	if err := os.MkdirAll(prdPath, 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := &scriptedPromptOps{}
	result, err := RunProductGate(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt, UserPrompt: "x",
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"p/m"} }),
		Sessions: SessionCreatorFunc(func(context.Context, string, string) (string, error) {
			return "session", nil
		}),
	})
	if err != nil || result.Status != "wrote" || len(prompt.requests) != 1 {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, len(prompt.requests))
	}
}
