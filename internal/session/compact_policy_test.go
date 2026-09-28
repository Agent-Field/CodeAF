package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// /compact shortens a conversation already under the automatic target, as far
// as it goes: the fold takes the old answers and the summary the older
// questions, and the three most recent stay word for word.
func TestManualCompactionReducesHistoryBelowAutomaticTarget(t *testing.T) {
	agent, _ := newTestAgent(t, &summarizer{}, func(c *Config) { c.ContextWindow = 131072; c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl") })
	grownTranscript(agent, 30, 8000)
	before := estimate(agent)
	if before >= agent.compactTargetTokens() {
		t.Fatal("fixture crossed automatic target")
	}
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := estimate(agent); after >= before/2 {
		t.Fatalf("manual compaction barely changed %d to %d", before, after)
	}
	messages := liveTranscript(agent)
	for i := 28; i <= 30; i++ {
		if !holdsText(messages, fmt.Sprintf("question %d", i)) {
			t.Fatalf("lost recent question %d", i)
		}
	}
}

func TestEmergencyCompactionCanArchiveCompletedActiveBatches(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(c *Config) { c.ContextWindow = 40960; c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl") })
	agent.mu.Lock()
	agent.running = true
	agent.turnFloor = 1
	agent.messages = append(agent.messages, textMessage("user", "keep my instructions"))
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("call-%d", i)
		agent.messages = append(agent.messages, ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id, Function: ai.ToolCallFunction{Name: "edit", Arguments: `{"path":"main.go"}`}}}}, ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("edited content ", 600)}}})
	}
	agent.mu.Unlock()
	defer func() { agent.mu.Lock(); agent.running = false; agent.mu.Unlock() }()
	before := estimate(agent)
	if !agent.recoverContext(context.Background(), nil, &provider.APIError{Overflow: true, ContextLimit: 40960, InputTokens: before, OutputTokens: 14746}) {
		t.Fatal("no recovery")
	}
	after := estimate(agent)
	if after >= before {
		t.Fatalf("did not shrink: %d -> %d", before, after)
	}
	messages := liveTranscript(agent)
	assertPaired(t, messages)
	if !holdsText(messages, "keep my instructions") {
		t.Fatal("lost user message")
	}
	if messages[len(messages)-1].ToolCallID != "call-15" {
		t.Fatal("newest result folded")
	}
}

func TestContextRecoveryResetsAfterSuccessfulToolSteps(t *testing.T) {
	refused := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, &provider.APIError{Status: 400, Overflow: true, ContextLimit: 40960, BudgetChanged: true, Message: "context length"}
	}
	completer := &scriptedCompleter{steps: []step{
		refused,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("one", "change", `{}`), nil
		},
		refused,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("two", "change", `{}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	changes := 0
	agent.tools = append(agent.tools, bare.Tool{Name: "change", Description: "records a change", Schema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (string, bool, error) { changes++; return "changed", false, nil }})
	for _, event := range collect(t, mustSubmit(t, agent, "make both changes")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if completer.requests() != 5 || changes != 2 {
		t.Fatalf("requests=%d changes=%d; completed actions must not replay", completer.requests(), changes)
	}
}

func TestContextRecoveryDoesNotLoopWithoutProgress(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, &provider.APIError{Status: 400, Overflow: true, Message: "context length"}
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	var failure error
	for _, event := range collect(t, mustSubmit(t, agent, "hello")) {
		if event.Kind == EventError {
			failure = event.Err
		}
	}
	if failure == nil || completer.requests() != 1 {
		t.Fatalf("error=%v requests=%d", failure, completer.requests())
	}
	if err := agent.Compact(context.Background()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatal(err)
	}
}

// This drives the real session door through the HTTP adapter. Both rejections
// have explicit endpoint windows, and real tools run between them.
func TestContextRecoveryThroughHTTPKeepsWorkingAfterSecondOverflow(t *testing.T) {
	var requests atomic.Int32
	var outputCaps [5]atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		n := int(requests.Add(1))
		if n <= len(outputCaps) {
			outputCaps[n-1].Store(int64(body.MaxTokens))
		}
		w.Header().Set("Content-Type", "application/json")
		switch n {
		case 1, 3:
			limit := 40960
			if n == 3 {
				limit = 32768
			}
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"error":{"code":"context_length_exceeded","message":"This model's maximum context length is %d tokens. However, you requested 14746 output tokens and your prompt contains at least 26215 input tokens.","metadata":{"provider_name":"SmallEndpoint"}}}`, limit)
		case 2, 4:
			fmt.Fprintf(w, `{"model":"budget/http","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"change-%d","type":"function","function":{"name":"change","arguments":"{}"}}]}}],"usage":{"prompt_tokens":26215,"completion_tokens":10}}`, n)
		default:
			fmt.Fprint(w, `{"model":"budget/http","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"finished both changes"}}],"usage":{"prompt_tokens":26215,"completion_tokens":10}}`)
		}
	}))
	defer server.Close()
	client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "budget/http", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.Model = "budget/http"; c.ContextWindow = 131072 })
	changes := 0
	agent.tools = append(agent.tools, bare.Tool{Name: "change", Description: "records one action", Schema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (string, bool, error) { changes++; return "changed", false, nil }})
	for _, event := range collect(t, mustSubmit(t, agent, "make both changes")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if requests.Load() != 5 || changes != 2 {
		t.Fatalf("requests=%d actions=%d", requests.Load(), changes)
	}
	if outputCaps[1].Load() <= 0 || 26215+outputCaps[1].Load()+int64(provider.ContextSafetyTokens(40960)) > 40960 {
		t.Fatalf("first repaired allowance=%d", outputCaps[1].Load())
	}
	if outputCaps[3].Load() <= 0 || 26215+outputCaps[3].Load()+int64(provider.ContextSafetyTokens(32768)) > 32768 {
		t.Fatalf("second repaired allowance=%d", outputCaps[3].Load())
	}
}

func TestContextRecoveryBoundsRepeatedChangedLimitClaims(t *testing.T) {
	refused := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, &provider.APIError{Status: 400, Overflow: true, BudgetChanged: true, Message: "context length"}
	}
	completer := &scriptedCompleter{steps: []step{refused, refused, refused, refused}}
	agent, _ := newTestAgent(t, completer, nil)
	var failure error
	for _, event := range collect(t, mustSubmit(t, agent, "hello")) {
		if event.Kind == EventError {
			failure = event.Err
		}
	}
	if failure == nil || completer.requests() != 1+contextRecoveryAttempts {
		t.Fatalf("error=%v requests=%d", failure, completer.requests())
	}
}

func TestLeanSmallWindowFirstRequestReachesHTTP(t *testing.T) {
	t.Setenv(promptProfileEnv, "lean")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages  []json.RawMessage `json:"messages"`
			Tools     []json.RawMessage `json:"tools"`
			MaxTokens int               `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls.Add(1)
		weight := 0
		for _, message := range body.Messages {
			weight += len(message)
		}
		for _, tool := range body.Tools {
			weight += len(tool)
		}
		input := (weight + 3) / 4
		t.Logf("first request: input=%d output=%d tools=%d", input, body.MaxTokens, len(body.Tools))
		if input < 10000 || len(body.Tools) < 10 {
			t.Error("fixture lost the shipping prompt or tool belt")
		}
		if body.MaxTokens < 512 || input+body.MaxTokens+provider.ContextSafetyTokens(16385) > 16385 {
			t.Error("request budget does not fit")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"A short answer fits."},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "budget/lean-first", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		buildShippedConversation(t, c)
		c.System = ""
		c.Model = "budget/lean-first"
		c.ContextWindow = 16385
		// Include repository instructions; the old prefix weighing omitted them.
		if err := os.WriteFile(filepath.Join(c.Workspace, "AGENTS.md"), []byte(strings.Repeat("Follow project conventions.\n", 200)), 0600); err != nil {
			t.Fatal(err)
		}
	})
	for _, event := range collect(t, mustSubmit(t, agent, "Explain the conjecture.")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP requests = %d", calls.Load())
	}
}

func TestAutomaticNoOpCompactionEmitsNoSeam(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, nil)
	hub := newEventHub()
	events := hub.subscribe()
	if _, err := agent.compact(context.Background(), hub); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("compact error=%v", err)
	}
	hub.close()
	for _, event := range collect(t, events) {
		if event.Kind == EventCompacting || event.Kind == EventCompacted {
			t.Fatal("automatic no-op emitted a compaction seam")
		}
	}
	if err := agent.Compact(context.Background()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("manual no-op lost its feedback: %v", err)
	}
}
