package codeaf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/loopguard"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

type scriptedRoundTripper struct {
	mu       sync.Mutex
	replies  []string
	statuses []int
	requests [][]byte
}

type recordedChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (transport *scriptedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	transport.requests = append(transport.requests, body)
	reply := transport.replies[0]
	transport.replies = transport.replies[1:]
	status := http.StatusOK
	if len(transport.statuses) > 0 {
		status = transport.statuses[0]
		transport.statuses = transport.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(reply)),
		Request:    request,
	}, nil
}

func chatReply(content string, promptTokens float64) string {
	encodedContent, _ := json.Marshal(content)
	return `data: {"id":"gen-text","choices":[{"delta":{"content":` +
		string(encodedContent) + `}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"cost":0.01,"prompt_tokens":` +
		strconv.FormatFloat(promptTokens, 'f', -1, 64) +
		`,"completion_tokens":10,"total_tokens":` +
		strconv.FormatFloat(promptTokens+10, 'f', -1, 64) + `}}` + "\n\n" +
		"data: [DONE]\n\n"
}

func toolCallReply(name, arguments string) string {
	encodedName, _ := json.Marshal(name)
	encodedArguments, _ := json.Marshal(arguments)
	return `data: {"choices":[{"delta":{"tool_calls":[{` +
		`"index":0,"id":"call-1","type":"function","function":{"name":` + string(encodedName) +
		`,"arguments":` + string(encodedArguments) + `}}]},"finish_reason":"tool_calls"}],` +
		`"usage":{"cost":0.01,"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}` +
		"\n\ndata: [DONE]\n\n"
}

func TestOpenRouterRejectsToolOmittedFromRequestDefinitions(t *testing.T) {
	// Round-2 invalid-tool contract: an unavailable write projects through the
	// synthetic invalid tool as a successful correction, without mutating disk.
	workspace := t.TempDir()
	target := filepath.Join(workspace, "forbidden.txt")
	arguments, err := json.Marshal(map[string]any{
		"filePath": target,
		"content":  "must not be written",
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("write", string(arguments)),
		chatReply("continued after rejection", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	runtime := newRuntime(workspace, backend)
	t.Cleanup(runtime.Close)
	result, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "openai/gpt-6.1-codex",
		Worktree: workspace, Prompt: "test filtered execution",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("filtered write changed the workspace: %v", statErr)
	}
	if len(result.Parts) != 2 || result.Parts[0].Type != "tool" ||
		result.Parts[0].Tool != "invalid" || result.Parts[0].Status != "completed" ||
		result.Parts[1].Text != "continued after rejection" {
		t.Fatalf("scheduler-visible parts = %#v", result.Parts)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("HTTP requests = %d, want rejected turn plus continuation", len(transport.requests))
	}
	if stats := leafoutcome.AggregateSessionStats(result.Messages); stats.ToolErrors != 0 {
		t.Fatalf("synthetic invalid call counted %v tool errors", stats.ToolErrors)
	}
	want := "The arguments provided to the tool are invalid: Model tried to call unavailable tool 'write'."
	if !strings.Contains(string(transport.requests[1]), want) {
		t.Fatalf("model-visible rejection = %s, want substring %q", transport.requests[1], want)
	}
}

func TestOpenRouterSystemIncludesRootInstructionsAndReadOnlyInjectsNestedRules(t *testing.T) {
	// Round-2 root-instruction contract: root AGENTS.md is in every engine
	// system message, while only a read below a nested rules file gets a
	// nested system-reminder (the root path is excluded from Resolve).
	workspace := t.TempDir()
	rootRules := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(rootRules, []byte("ROOT ENGINE CONTRACT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "root.txt"), []byte("root target"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(workspace, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("NESTED READ CONTRACT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runRead := func(target string) [][]byte {
		t.Helper()
		arguments, err := json.Marshal(map[string]string{"filePath": target})
		if err != nil {
			t.Fatal(err)
		}
		transport := &scriptedRoundTripper{replies: []string{
			toolCallReply("read", string(arguments)), chatReply("done", 10),
		}}
		runtime := newRuntime(workspace, &openRouterBackend{
			apiKey: "test", client: &http.Client{Transport: transport},
		})
		if _, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
			Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "vendor/model",
			Worktree: workspace, Prompt: "read the target",
		}); err != nil {
			t.Fatal(err)
		}
		return transport.requests
	}

	rootRequests := runRead(filepath.Join(workspace, "root.txt"))
	if !strings.Contains(string(rootRequests[0]), "ROOT ENGINE CONTRACT") {
		t.Fatalf("root instruction missing from system message: %s", rootRequests[0])
	}
	if strings.Contains(string(rootRequests[1]), "<system-reminder>") {
		t.Fatalf("root-level read injected a nested reminder: %s", rootRequests[1])
	}

	nestedRequests := runRead(filepath.Join(nested, "main.go"))
	if !strings.Contains(string(nestedRequests[0]), "ROOT ENGINE CONTRACT") ||
		!strings.Contains(string(nestedRequests[1]),
			"<system-reminder>\\nInstructions from: "+filepath.Join(nested, "AGENTS.md")+"\\nNESTED READ CONTRACT") {
		t.Fatalf("root/nested instruction projection = %s", nestedRequests[1])
	}
}

func TestOpenRouterLeafCompactsContextAndContinues(t *testing.T) {
	// Validation contract 5: inflated usage shrinks the next live iteration to
	// [system, original user, summary context] and the loop keeps advancing.
	transport := &scriptedRoundTripper{replies: []string{
		chatReply("working", 70_000),
		chatReply("anchored summary", 10),
		chatReply("finished after compaction", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system prompt", ModelID: "vendor/model",
		Prompt: "original task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "finished after compaction" {
		t.Fatalf("result text = %q", result.Text)
	}
	if len(transport.requests) != 3 {
		t.Fatalf("HTTP requests = %d, want response + summary + continued response", len(transport.requests))
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(transport.requests[1], &summary); err != nil {
		t.Fatal(err)
	}
	if _, exists := summary["tools"]; exists {
		t.Fatalf("summary request serialized tools: %s", transport.requests[1])
	}
	var summaryMessages []recordedChatMessage
	if err := json.Unmarshal(summary["messages"], &summaryMessages); err != nil {
		t.Fatal(err)
	}
	for _, message := range summaryMessages {
		if message.Role == "system" {
			t.Fatalf("summary request retained system message: %#v", summaryMessages)
		}
	}
	var continued struct {
		Messages []recordedChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(transport.requests[2], &continued); err != nil {
		t.Fatal(err)
	}
	if len(continued.Messages) < 3 || continued.Messages[0].Role != "system" {
		t.Fatalf("continued context = %#v", continued.Messages)
	}
	continuedJSON := string(transport.requests[2])
	if !strings.Contains(continuedJSON, "anchored summary") ||
		!strings.Contains(continuedJSON, "Continue") {
		t.Fatalf("continued context = %s", transport.requests[2])
	}
}

func TestProjectConfigDisablesAutoCompactionOnLiveTurn(t *testing.T) {
	// Validation contract B2: compaction.auto=false loaded from project config
	// reaches the live controller and suppresses an otherwise-overflowing turn.
	workspace := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspace, "codeaf.json"),
		[]byte(`{"compaction":{"auto":false}}`), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCodeafConfig(workspace)
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedRoundTripper{replies: []string{
		chatReply("finished without compaction", 70_000),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	loaded.applyBackend(backend)
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system prompt", ModelID: "vendor/model",
		Workspace: workspace, Prompt: "original task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "finished without compaction" {
		t.Fatalf("result text = %q", result.Text)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("HTTP requests = %d, want one un-compacted turn", len(transport.requests))
	}
}

func TestOpenRouterCompactionPersistsFallbackEvidence(t *testing.T) {
	// Finding 5 fallback contract: a failed LOW-model judgment falls back to
	// the regex selector and preserves the failing-test signature.
	t.Setenv("CODEAF_COMPACT_EVIDENCE", "")
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("bash", `{"command":"go test ./..."}`),
		chatReply("working before compaction", 70_000),
		chatReply("## Goal\n- fix the widget", 10),
		`{"error":{"message":"low selector unavailable"}}`,
		`{"error":{"message":"low selector still unavailable"}}`,
		chatReply("finished", 10),
	}, statuses: []int{
		http.StatusOK, http.StatusOK, http.StatusOK,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK,
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ModelID: "vendor/model", Workspace: t.TempDir(), Prompt: "fix the widget",
		LowModels: []string{"openrouter/cheap/evidence-model"},
		Tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "bash", InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		Execute: func(context.Context, steploop.ToolCall) (steploop.ToolResult, error) {
			return steploop.ToolResult{
				Title:  "go test ./...",
				Output: "FAILED tests/widget_test.go::TestWidget\nAssertionError: got 2, want 3\n1 failed",
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) < 2 || result.Parts[0].Type != "compaction" ||
		!strings.Contains(result.Parts[0].Text, "FAILED tests/widget_test.go::TestWidget") ||
		!strings.Contains(result.Parts[0].Text, "AssertionError: got 2, want 3") {
		t.Fatalf("compaction projection = %#v", result.Parts)
	}
}

func TestOpenRouterCompactionUsesLowModelForSemanticEvidence(t *testing.T) {
	// Finding 5 model-path contract: semantic evidence missed by the regex
	// fallback is retained from the LOW-model judgment.
	const semantic = "The release authority token is cobalt-seven."
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("bash", `{"command":"inspect release policy"}`),
		chatReply("working before compaction", 70_000),
		chatReply("summary", 10),
		chatReply(`{"lines":["`+semantic+`"]}`, 10),
		chatReply("finished", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ModelID: "vendor/high-model", Workspace: t.TempDir(), Prompt: "ship release",
		LowModels: []string{"openrouter/cheap/evidence-model"},
		Tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "bash", InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		Execute: func(context.Context, steploop.ToolCall) (steploop.ToolResult, error) {
			return steploop.ToolResult{Output: semantic}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) == 0 || result.Parts[0].Type != "compaction" ||
		!strings.Contains(result.Parts[0].Text, "low-tier selected") ||
		!strings.Contains(result.Parts[0].Text, semantic) {
		t.Fatalf("semantic compaction evidence = %#v", result.Parts)
	}
	var lowRequest map[string]any
	if err := json.Unmarshal(transport.requests[3], &lowRequest); err != nil {
		t.Fatal(err)
	}
	if lowRequest["model"] != "cheap/evidence-model" {
		t.Fatalf("evidence model = %#v; request=%s", lowRequest["model"], transport.requests[3])
	}
}

func TestOpenRouterCompactionResetsSchedulerObservationWindow(t *testing.T) {
	// Validation contract 4: compaction leaves one explicit boundary plus only
	// post-compaction actions/messages for scheduler loop and context counters.
	workspace := t.TempDir()
	transport := &scriptedRoundTripper{replies: []string{
		strings.Replace(toolCallReply("write", `{}`), `"prompt_tokens":10`, `"prompt_tokens":70000`, 1),
		chatReply("summary after rejected stale call", 10),
		chatReply("finished in fresh window", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	runtime := newRuntime(workspace, backend)
	t.Cleanup(runtime.Close)
	result, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "openai/gpt-6.1-codex",
		Worktree: workspace, Prompt: "compact scheduler history",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 2 || result.Parts[0].Type != "compaction" ||
		result.Parts[0].Text != "summary after rejected stale call" ||
		result.Parts[1].Type != "text" || result.Parts[1].Text != "finished in fresh window" {
		t.Fatalf("scheduler-visible parts = %#v", result.Parts)
	}
	guard := loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{})
	for _, part := range result.Parts {
		if part.Type == "tool" {
			guard.Observe(loopguard.LoopAction{Tool: part.Tool, ArgsKey: part.ArgsKey})
		}
	}
	if got := guard.Snapshot().ActionCount; got != 0 {
		t.Fatalf("post-compaction loop actions = %v, want 0", got)
	}
	stats := leafoutcome.AggregateSessionStats(result.Messages)
	if stats.Turns != 1 || stats.ToolErrors != 0 || stats.CostUsd < 0.029 || stats.CostUsd > 0.031 {
		t.Fatalf("post-compaction stats = %#v", stats)
	}
}

func TestOpenRouterLeafSummaryFailureRetainsAccumulatedCost(t *testing.T) {
	// Finding 2: cost from completed live turns survives a failed compaction
	// summary and is recorded by runtimeAdapter on the error path.
	transport := &scriptedRoundTripper{
		replies: []string{
			chatReply("working", 70_000),
			`{"error":{"message":"summary rejected"}}`,
		},
		statuses: []int{http.StatusOK, http.StatusBadRequest},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	runtime := newRuntime(t.TempDir(), backend)
	t.Cleanup(runtime.Close)
	_, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "vendor/model",
		Worktree: t.TempDir(), Prompt: "original task",
	})
	if err == nil || !strings.Contains(err.Error(), "summary rejected") {
		t.Fatalf("summary failure = %v", err)
	}
	if got := runtime.cost(); got != 0.01 {
		t.Fatalf("recorded cost = %v, want 0.01", got)
	}
}

func TestOpenRouterLeafHardOverflowCompactsAndRetries(t *testing.T) {
	// Finding 1: a hard provider overflow takes the same capped summary path as
	// usage-based overflow, then retries with rebuilt context.
	transport := &scriptedRoundTripper{
		replies: []string{
			`{"error":{"message":"maximum context length is 128000 tokens"}}`,
			chatReply("anchored summary", 10),
			chatReply("finished after hard overflow", 10),
		},
		statuses: []int{http.StatusBadRequest, http.StatusOK, http.StatusOK},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system prompt", ModelID: "vendor/model",
		Prompt: "original task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "finished after hard overflow" || len(transport.requests) != 3 {
		t.Fatalf("result=%+v requests=%d", result, len(transport.requests))
	}
}

func TestOpenRouterLeafFourthOverflowFails(t *testing.T) {
	// Validation contract 5: three compactions are allowed; the fourth
	// overflowing live iteration fails the leaf without another summary call.
	replies := []string{}
	for index := 0; index < maxLeafCompactions; index++ {
		replies = append(replies,
			chatReply("overflow", 70_000),
			chatReply("summary", 10),
		)
	}
	replies = append(replies, chatReply("fourth overflow", 70_000))
	transport := &scriptedRoundTripper{replies: replies}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	_, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system", ModelID: "vendor/model", Prompt: "task",
	})
	if err == nil || err.Error() != "codeaf: leaf compaction limit 3 exceeded" {
		t.Fatalf("fourth overflow error = %v", err)
	}
	if len(transport.requests) != 7 {
		t.Fatalf("HTTP requests = %d, want 4 live + 3 summary", len(transport.requests))
	}
}

func TestOpenRouterEngineHasNoUnconditionalSixtyFourTurnCap(t *testing.T) {
	// Round-2 turn-limit contract: TS has no unconditional engine cap; action,
	// loop, cost, and agent step budgets own termination. A valid 65-tool-turn
	// sequence must therefore reach its natural terminal response.
	replies := make([]string, 0, 66)
	for index := 0; index < 65; index++ {
		replies = append(replies, toolCallReply("bash", `{"command":"true"}`))
	}
	replies = append(replies, chatReply("natural stop", 10))
	transport := &scriptedRoundTripper{replies: replies}
	backend := &openRouterBackend{apiKey: "test", client: &http.Client{Transport: transport}}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ModelID: "vendor/model", Workspace: t.TempDir(), Prompt: "keep going",
		Tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "bash", InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		Execute: func(context.Context, steploop.ToolCall) (steploop.ToolResult, error) {
			return steploop.ToolResult{Output: "ok"}, nil
		},
	})
	if err != nil || result.Text != "natural stop" || len(transport.requests) != 66 {
		t.Fatalf("result=%+v err=%v requests=%d", result, err, len(transport.requests))
	}
}

func TestOpenRouterLeafPerTurnGuardStopsEndlessToolCalls(t *testing.T) {
	// Finding 1 contract: the leaf action guard runs between provider turns, so
	// an endless tool-call provider is stopped without waiting for RunLeaf.
	maximum := 3.0
	guard := loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{MaxActions: &maximum})
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("bash", `{"command":"one"}`),
		toolCallReply("bash", `{"command":"two"}`),
		toolCallReply("bash", `{"command":"three"}`),
		toolCallReply("bash", `{"command":"four"}`),
	}}
	runtime := newRuntime(t.TempDir(), &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	})
	t.Cleanup(runtime.Close)
	_, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "vendor/model",
		Worktree: t.TempDir(), Prompt: "keep calling tools",
		AfterTurn: func(_ context.Context, turn scheduler.LeafTurnObservation) error {
			for _, part := range turn.Parts {
				verdict := guard.Observe(loopguard.LoopAction{Tool: part.Tool, ArgsKey: part.ArgsKey})
				if verdict.Status == loopguard.LoopStatusStop {
					return errors.New(*verdict.Reason)
				}
			}
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "action budget reached (3/3)") {
		t.Fatalf("guard error = %v", err)
	}
	if len(transport.requests) != 3 {
		t.Fatalf("provider calls = %d, want 3", len(transport.requests))
	}
}

func TestOpenRouterReloadsRootInstructionsEachTurn(t *testing.T) {
	// Finding 3 contract: a root instruction created by turn one appears in
	// turn two's system message.
	workspace := t.TempDir()
	rules := filepath.Join(workspace, "AGENTS.md")
	arguments, err := json.Marshal(map[string]string{
		"filePath": rules, "content": "MID-LEAF ROOT CONTRACT",
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("write", string(arguments)), chatReply("done", 10),
	}}
	runtime := newRuntime(workspace, &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	})
	t.Cleanup(runtime.Close)
	if _, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ModelID: "vendor/model",
		Worktree: workspace, Prompt: "create instructions",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(transport.requests[0]), "MID-LEAF ROOT CONTRACT") {
		t.Fatalf("turn one unexpectedly contained future instructions: %s", transport.requests[0])
	}
	if !strings.Contains(string(transport.requests[1]), "MID-LEAF ROOT CONTRACT") {
		t.Fatalf("turn two did not reload root instructions: %s", transport.requests[1])
	}
}

func TestOpenRouterLeafEmptyBodyOverflowCompacts(t *testing.T) {
	// Final-scan finding 1: an empty 400 body must classify as context
	// overflow ("400 (no body)" APICallError shape) and take the summary path.
	transport := &scriptedRoundTripper{
		replies: []string{
			``,
			chatReply("empty-body summary", 10),
			chatReply("finished after empty-body overflow", 10),
		},
		statuses: []int{http.StatusBadRequest, http.StatusOK, http.StatusOK},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system prompt", ModelID: "vendor/model",
		Prompt: "original task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "finished after empty-body overflow" || len(transport.requests) != 3 {
		t.Fatalf("result=%+v requests=%d", result, len(transport.requests))
	}
}
