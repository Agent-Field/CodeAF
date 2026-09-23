//go:build !windows

package app

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

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/loopguard"
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

// summaryPathConfig is a project config with a zero verbatim-tail budget. The
// histories these tests build are a few hundred tokens, which the default
// 20K-token tail would keep whole -- leaving nothing to summarize and no
// summary request for the scripted transport to answer. A zero budget keeps
// only the newest message verbatim, so every compaction here takes the summary
// path through the real transport, which is what these tests exist to prove.
func summaryPathConfig(t *testing.T) *seniorDevConfig {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspace, "senior-dev.json"),
		[]byte(`{"compaction":{"preserve_recent_tokens":0}}`), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSeniorDevConfig(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func validCompactionSummary(goal string) string {
	return strings.Join([]string{
		"## Working State",
		"### Completed", "- " + goal,
		"### Current", "- continue",
		"### Verification", "- (none)",
		"### Next", "- continue",
		"### Files", "- (none)",
	}, "\n")
}

func TestSeniorDevCompactionSizerIncludesSystemPromptAndToolSchemas(t *testing.T) {
	model := compaction.Model{Message: msgmodel.Model{
		ProviderID: "openrouter", ID: "vendor/model",
	}}
	base, err := (seniorDevContextSizer{}).EstimateContext(context.Background(), nil, model)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("context-bearing-token ", 500)
	full, err := (seniorDevContextSizer{
		system: func(context.Context) string { return large },
		tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "large_tool",
			Description: large, InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
	}).EstimateContext(context.Background(), nil, model)
	if err != nil {
		t.Fatal(err)
	}
	if full <= base+4_000 {
		t.Fatalf("full request estimate = %v, base = %v; system/tool context was not counted", full, base)
	}
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
	// An unavailable write projects through the synthetic invalid tool as a
	// successful correction, without mutating disk.
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
	result, err := runTestTurn(t, runtime, testTurn{
		Agent: "coder", ModelID: "openai/gpt-6.1-codex",
		Workspace: workspace, Prompt: "test filtered execution",
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
		t.Fatalf("turn parts = %#v", result.Parts)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("HTTP requests = %d, want rejected turn plus continuation", len(transport.requests))
	}
	for _, part := range result.Parts {
		if part.Type == "tool" && part.Status == "error" {
			t.Fatalf("synthetic invalid call counted as a tool error: %#v", part)
		}
	}
	want := "The arguments provided to the tool are invalid: Model tried to call unavailable tool 'write'."
	if !strings.Contains(string(transport.requests[1]), want) {
		t.Fatalf("model-visible rejection = %s, want substring %q", transport.requests[1], want)
	}
}

func TestOpenRouterSystemIncludesRootInstructionsAndReadOnlyInjectsNestedRules(t *testing.T) {
	// Root AGENTS.md is in every engine system message, while only a read
	// below a nested rules file gets a
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
		if _, err := runTestTurn(t, runtime, testTurn{
			Agent: "coder", ModelID: "vendor/model",
			Workspace: workspace, Prompt: "read the target",
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

func TestOpenRouterCompactsContextAndContinues(t *testing.T) {
	// Inflated usage shrinks the next live iteration to [system, original
	// user, summary context] and the loop keeps advancing.
	transport := &scriptedRoundTripper{replies: []string{
		chatReply("working", 70_000),
		chatReply(validCompactionSummary("anchored summary for the original task"), 10),
		chatReply("finished after compaction", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
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
	systemCount := 0
	for _, message := range summaryMessages {
		if message.Role != "system" {
			continue
		}
		systemCount++
		var content []struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(message.Content, &content); err != nil {
			t.Fatal(err)
		}
		texts := make([]string, 0, len(content))
		for _, part := range content {
			texts = append(texts, part.Text)
		}
		if got := strings.Join(texts, "\n"); got != compaction.SummarySystemPrompt {
			t.Fatalf("summary system prompt = %q", got)
		}
	}
	if systemCount != 1 {
		t.Fatalf("summary system message count = %d; messages=%#v", systemCount, summaryMessages)
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
		!strings.Contains(continuedJSON, "Continue from the current state") ||
		!strings.Contains(continuedJSON, "working") {
		t.Fatalf("continued context = %s", transport.requests[2])
	}
	// The summary request carried the flattened head -- the original task --
	// and not the verbatim tail, and it carried it as real content.
	summaryJSON := string(transport.requests[1])
	if !strings.Contains(summaryJSON, `<conversation>\n[User]: original task`) ||
		strings.Contains(summaryJSON, `[Assistant]: working`) {
		t.Fatalf("summary request = %s", summaryJSON)
	}
}

func TestProjectConfigDisablesAutoCompactionOnLiveTurn(t *testing.T) {
	// compaction.auto=false loaded from project config reaches the live
	// controller and suppresses an otherwise-overflowing turn.
	workspace := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspace, "senior-dev.json"),
		[]byte(`{"compaction":{"auto":false}}`), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSeniorDevConfig(workspace)
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

func TestOpenRouterCompactionHarvestsEvidenceByCodeAlone(t *testing.T) {
	// Evidence is harvested from the summarized head by code: the failing-test
	// signature survives the boundary, and no second model is asked anything
	// -- exactly four requests, all to the coder's own model.
	transport := &scriptedRoundTripper{replies: []string{
		toolCallReply("bash", `{"command":"go test ./..."}`),
		chatReply("working before compaction", 70_000),
		chatReply(validCompactionSummary("fix the widget"), 10),
		chatReply("finished", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ModelID: "vendor/model", Workspace: t.TempDir(), Prompt: "fix the widget",
		AgentMarkdown: testAgentPrompt,
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
	if len(transport.requests) != 4 {
		t.Fatalf("HTTP requests = %d, want tool turn + overflow turn + summary + continuation", len(transport.requests))
	}
	for index, request := range transport.requests {
		if strings.Contains(string(request), "cheap/evidence-model") {
			t.Fatalf("request %d went to the evidence model: %s", index, request)
		}
	}
}

func TestOpenRouterCompactionResetsTheObservationWindow(t *testing.T) {
	// Compaction leaves one explicit boundary plus only post-compaction
	// actions/messages for the loop guard and context counters.
	workspace := t.TempDir()
	transport := &scriptedRoundTripper{replies: []string{
		strings.Replace(toolCallReply("write", `{}`), `"prompt_tokens":10`, `"prompt_tokens":70000`, 1),
		chatReply(validCompactionSummary("summary after rejected stale call"), 10),
		chatReply("finished in fresh window", 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
	runtime := newRuntime(workspace, backend)
	t.Cleanup(runtime.Close)
	result, err := runTestTurn(t, runtime, testTurn{
		Agent: "coder", ModelID: "openai/gpt-6.1-codex",
		Workspace: workspace, Prompt: "compact the history",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 2 || result.Parts[0].Type != "compaction" ||
		!strings.Contains(result.Parts[0].Text, "summary after rejected stale call") ||
		result.Parts[1].Type != "text" || result.Parts[1].Text != "finished in fresh window" {
		t.Fatalf("turn parts = %#v", result.Parts)
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
	if result.CostUSD < 0.029 || result.CostUSD > 0.031 {
		t.Fatalf("post-compaction cost = %v, want the three calls' 0.03", result.CostUSD)
	}
}

func TestOpenRouterSummaryFailureInstallsRecordAndContinues(t *testing.T) {
	// A failed summary call is not a dead run: the boundary completes with the
	// deterministic record after exactly one attempt, the verbatim tail is kept,
	// the turn goes on, and completed live-call cost is still recorded. A 502 is
	// deliberate: a retryable status must not make the summary request replay.
	transport := &scriptedRoundTripper{
		replies: []string{
			chatReply("working", 70_000),
			`{"error":{"message":"summary provider unavailable"}}`,
			chatReply("finished after a failed summary", 10),
		},
		statuses: []int{http.StatusOK, http.StatusBadGateway, http.StatusOK},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
	runtime := newRuntime(t.TempDir(), backend)
	t.Cleanup(runtime.Close)
	result, err := runTestTurn(t, runtime, testTurn{
		Agent: "coder", ModelID: "vendor/model",
		Workspace: t.TempDir(), Prompt: "original task",
	})
	if err != nil {
		t.Fatalf("a failed summary killed the turn: %v", err)
	}
	if result.Text != "finished after a failed summary" || len(transport.requests) != 3 {
		t.Fatalf("result=%q requests=%d", result.Text, len(transport.requests))
	}
	if len(result.Parts) == 0 || result.Parts[0].Type != "compaction" ||
		!strings.Contains(result.Parts[0].Text, "no state record could be generated") {
		t.Fatalf("compaction projection = %#v", result.Parts)
	}
	continued := string(transport.requests[2])
	if !strings.Contains(continued, "working") || !strings.Contains(continued, "original task") {
		t.Fatalf("continuation lost the tail or the pinned task: %s", continued)
	}
	if got := runtime.cost(); got < 0.019 || got > 0.021 {
		t.Fatalf("recorded cost = %v, want the two completed live calls", got)
	}
}

func TestOpenRouterHardOverflowCompactsAndRetries(t *testing.T) {
	// A hard provider overflow takes the same capped summary path as
	// usage-based overflow, then retries with rebuilt context.
	transport := &scriptedRoundTripper{
		replies: []string{
			`{"error":{"message":"maximum context length is 128000 tokens"}}`,
			chatReply(validCompactionSummary("anchored summary"), 10),
			chatReply("finished after hard overflow", 10),
		},
		statuses: []int{http.StatusBadRequest, http.StatusOK, http.StatusOK},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	summaryPathConfig(t).applyBackend(backend)
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

func TestOpenRouterAllowsMoreThanThreeSuccessfulCompactions(t *testing.T) {
	// Compaction count is not a termination policy. A long but reducible run
	// can compact repeatedly and still reach its natural terminal response.
	const compactions = 5
	replies := []string{}
	for index := 0; index < compactions; index++ {
		replies = append(replies,
			chatReply("overflow", 70_000),
			chatReply(validCompactionSummary("task"), 10),
		)
	}
	replies = append(replies, chatReply("natural stop", 10))
	transport := &scriptedRoundTripper{replies: replies}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system", ModelID: "vendor/model", Prompt: "task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "natural stop" {
		t.Fatalf("turn result = %q", result.Text)
	}
	if len(transport.requests) != 2*compactions+1 {
		t.Fatalf("HTTP requests = %d, want %d live/summary requests", len(transport.requests), 2*compactions+1)
	}
}

func TestOpenRouterStopsWhenAuthoritativeTaskCannotFitAfterRebuild(t *testing.T) {
	// Unlimited successful compactions must not become an infinite retry loop.
	// If the durable task itself cannot leave continuation headroom, fail with
	// an explicit capacity error after one model summary and one local rebuild.
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".senior-dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, ".senior-dev", "spec.md"),
		[]byte(strings.Repeat("irreducible authoritative requirement ", 10_000)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	transport := &scriptedRoundTripper{replies: []string{
		chatReply("overflow", 70_000),
		chatReply(validCompactionSummary("task"), 10),
	}}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
		contextLimit: 128_000, outputLimit: 32_768,
	}
	summaryPathConfig(t).applyBackend(backend)
	_, err := backend.Run(context.Background(), turn{
		Agent: "coder", AgentMarkdown: "system", ModelID: "vendor/model",
		Workspace: workspace, Prompt: "task",
	})
	if !errors.Is(err, compaction.ErrContextCapacityExhausted) {
		t.Fatalf("error = %v, want context capacity exhausted", err)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("HTTP requests = %d, want live request plus one summary", len(transport.requests))
	}
}

func TestOpenRouterEngineHasNoUnconditionalSixtyFourTurnCap(t *testing.T) {
	// The engine has no unconditional turn cap; action, loop, cost, and agent
	// step budgets own termination. A valid 65-tool-turn
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
		AgentMarkdown: testAgentPrompt,
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

func TestOpenRouterReloadsRootInstructionsEachTurn(t *testing.T) {
	// A root instruction created by turn one appears in turn two's system
	// message.
	workspace := t.TempDir()
	rules := filepath.Join(workspace, "AGENTS.md")
	arguments, err := json.Marshal(map[string]string{
		"filePath": rules, "content": "MID-TURN ROOT CONTRACT",
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
	if _, err := runTestTurn(t, runtime, testTurn{
		Agent: "coder", ModelID: "vendor/model",
		Workspace: workspace, Prompt: "create instructions",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(transport.requests[0]), "MID-TURN ROOT CONTRACT") {
		t.Fatalf("turn one unexpectedly contained future instructions: %s", transport.requests[0])
	}
	if !strings.Contains(string(transport.requests[1]), "MID-TURN ROOT CONTRACT") {
		t.Fatalf("turn two did not reload root instructions: %s", transport.requests[1])
	}
}

func TestOpenRouterEmptyBodyOverflowCompacts(t *testing.T) {
	// An empty 400 body must classify as context overflow ("400 (no body)")
	// and take the summary path.
	transport := &scriptedRoundTripper{
		replies: []string{
			``,
			chatReply(validCompactionSummary("empty-body summary"), 10),
			chatReply("finished after empty-body overflow", 10),
		},
		statuses: []int{http.StatusBadRequest, http.StatusOK, http.StatusOK},
	}
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: transport},
	}
	summaryPathConfig(t).applyBackend(backend)
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
