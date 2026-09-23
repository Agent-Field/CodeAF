//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/loopguard"
)

func recordedResponse(
	request *http.Request, status int, contentType string, body string,
) *http.Response {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", contentType)
	recorder.WriteHeader(status)
	_, _ = recorder.WriteString(body)
	response := recorder.Result()
	response.Request = request
	return response
}

func TestSeniorDevEngineStreamsShapesAndRepairsMisCasedToolCall(t *testing.T) {
	// senior-dev uses the OpenRouter streaming/request-shaping path, and a
	// mis-cased tool name (BASH for bash) is repaired before execute.
	var requests [][]byte
	var executed steploop.ToolCall
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		requests = append(requests, raw)
		if len(requests) == 1 {
			return recordedResponse(
				request, http.StatusOK, "text/event-stream",
				toolCallReply("BASH", `{"command":"true"}`),
			), nil
		}
		return recordedResponse(
			request, http.StatusOK, "text/event-stream", chatReply("done", 10),
		), nil
	})}
	backend := &modelAPIBackend{api: testModelAPI, client: client, variant: "high"}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen3.6-plus",
		Prompt: "repair the tool", Workspace: t.TempDir(), AgentMarkdown: testAgentPrompt,
		Tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "bash", Description: "run a command",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		}}},
		Execute: func(_ context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
			executed = call
			return steploop.ToolResult{Output: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed.Name != "bash" || result.Text != "done" || len(result.Parts) != 2 ||
		result.Parts[0].Tool != "bash" || result.Parts[0].Status != "completed" {
		t.Fatalf("executed=%+v result=%+v", executed, result)
	}
	if result.CostUSD != 0.02 ||
		result.Parts[0].CostUSD == nil || *result.Parts[0].CostUSD != 0.01 {
		t.Fatalf("cost ledger = total %v part %+v", result.CostUSD, result.Parts[0])
	}
	var body map[string]any
	if err := json.Unmarshal(requests[0], &body); err != nil {
		t.Fatal(err)
	}
	usage, _ := body["usage"].(map[string]any)
	reasoning, _ := body["reasoning"].(map[string]any)
	if body["stream"] != true || usage["include"] != true ||
		body["max_tokens"] != float64(32_000) || reasoning["effort"] != "high" ||
		body["prompt_cache_key"] == "" {
		t.Fatalf("shaped request = %s", requests[0])
	}
	// No source set a sampling parameter, so none is sent: the provider's own
	// default applies.
	for _, key := range []string{"temperature", "top_p", "top_k", "seed", "provider"} {
		if _, present := body[key]; present {
			t.Fatalf("unconfigured %s reached the wire: %s", key, requests[0])
		}
	}
}

func TestSeniorDevAdaptiveRouterFailsOverAndRegistersOutcomes(t *testing.T) {
	// A failed request is registered but never replayed inside the engine. The
	// next caller-owned turn resolves through the same live router and selects
	// another pool candidate.
	nowMS := float64(1_700_000_000_000)
	restoreNow := orclient.SetNowForTesting(func() float64 {
		nowMS += 1_000
		return nowMS
	})
	defer restoreNow()
	seed := float64(4)
	var eventMu sync.Mutex
	events := []adaptive.AdaptiveRouteEvent{}
	router := adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels: []adaptive.ModelCandidate{
			testRouterCandidate("openrouter/qwen/qwen-primary", 0),
			testRouterCandidate("openrouter/deepseek/deepseek-secondary", 1),
		},
		RandomSeed: &seed,
		OnEvent: func(event adaptive.AdaptiveRouteEvent) {
			eventMu.Lock()
			events = append(events, event)
			eventMu.Unlock()
		},
	})
	models := []string{}
	primary := ""
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		models = append(models, body.Model)
		if primary == "" {
			primary = body.Model
		}
		if body.Model == primary {
			return recordedResponse(
				request, http.StatusInternalServerError, "application/json",
				`{"error":{"message":"Provider returned error"}}`,
			), nil
		}
		return recordedResponse(
			request, http.StatusOK, "text/event-stream", chatReply("recovered", 10),
		), nil
	})}
	backend := &modelAPIBackend{api: testModelAPI, client: client, router: router}
	request := turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen-primary",
		Prompt: "fail over", Workspace: t.TempDir(), AgentMarkdown: testAgentPrompt,
	}
	if _, err := backend.Run(context.Background(), request); err == nil {
		t.Fatal("primary provider failure returned nil")
	}
	if len(models) != 1 {
		t.Fatalf("first turn made %d model requests, want exactly 1", len(models))
	}
	result, err := backend.Run(context.Background(), request)
	if err != nil || result.Text != "recovered" {
		t.Fatalf("caller-owned recovery turn = (%+v, %v)", result, err)
	}
	if len(models) != 2 || models[0] == models[1] {
		t.Fatalf("routed models = %v, want failover", models)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) != 2 || events[0].Failures != 1 || events[0].Error == "" ||
		events[0].ElapsedS != 1 || events[1].Successes != 1 ||
		events[1].ElapsedS != 1 || events[1].Error != "" {
		t.Fatalf("router outcomes = %#v", events)
	}
}

func TestSeniorDevCostCapTripsFromEngineLedger(t *testing.T) {
	// Provider usage reaches the turn result and is charged to its first tool
	// action, so a cost cap can be enforced from the engine's own ledger.
	responses := []string{
		toolCallReply("bash", `{"command":"true"}`),
		chatReply("done", 10),
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := responses[0]
		responses = responses[1:]
		return recordedResponse(request, http.StatusOK, "text/event-stream", response), nil
	})}
	runtime := newRuntime(t.TempDir(), &modelAPIBackend{api: testModelAPI, client: client})
	t.Cleanup(runtime.Close)
	result, err := runTestTurn(t, runtime, testTurn{
		Agent: "coder", ProviderID: "openrouter",
		ModelID: "qwen/qwen3.6-plus", Workspace: t.TempDir(), Prompt: "spend once",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.02 {
		t.Fatalf("turn ledger = total %v", result.CostUSD)
	}
	maxCost := 0.005
	guard := loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{MaxCostUsd: &maxCost})
	var verdict loopguard.LoopVerdict
	for _, part := range result.Parts {
		if part.Type == "tool" {
			verdict = guard.Observe(loopguard.LoopAction{
				Tool: part.Tool, ArgsKey: part.ArgsKey, CostUsd: part.CostUSD,
			})
		}
	}
	if verdict.Status != loopguard.LoopStatusStop || verdict.Reason == nil ||
		!strings.Contains(*verdict.Reason, "cost budget reached") {
		t.Fatalf("cost verdict = %#v; parts=%#v", verdict, result.Parts)
	}
}

func TestSeniorDevDeadlineCancelsMidStream(t *testing.T) {
	// The caller deadline reaches an already-open SSE stream
	// and terminates it without waiting for provider/watchdog timeouts.
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := &deadlineStreamBody{
			ctx: request.Context(),
			first: bytes.NewReader([]byte(
				"data: {\"id\":\"gen-deadline\",\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
			)),
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body, Request: request,
		}, nil
	})}
	backend := &modelAPIBackend{api: testModelAPI, client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := backend.Run(ctx, turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen3.6-plus",
		Prompt: "wait", Workspace: t.TempDir(), AgentMarkdown: testAgentPrompt,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("mid-stream cancellation took %s", elapsed)
	}
}

func testRouterCandidate(id string, priority float64) adaptive.ModelCandidate {
	return adaptive.ModelCandidate{
		ID: id, PromptUSDPerMtok: 1, CompletionUSDPerMtok: 1,
		Priority: float64(priority),
	}
}

type deadlineStreamBody struct {
	ctx   context.Context
	first *bytes.Reader
}

func (body *deadlineStreamBody) Read(target []byte) (int, error) {
	if body.first.Len() > 0 {
		return body.first.Read(target)
	}
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (*deadlineStreamBody) Close() error { return nil }
