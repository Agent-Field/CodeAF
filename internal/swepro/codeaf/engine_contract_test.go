package codeaf

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

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/loopguard"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
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

func TestCodeafEngineStreamsShapesAndRepairsMisCasedToolCall(t *testing.T) {
	// Round-2 contract: codeaf uses the OpenRouter streaming/request-shaping
	// path, and deepseek-style mis-cased tool names are repaired before execute.
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
	backend := &openRouterBackend{apiKey: "test", client: client, variant: "high"}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen3.6-plus",
		Prompt: "repair the tool", Workspace: t.TempDir(),
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
	if len(result.CallCosts) != 2 || result.CostUSD != 0.02 ||
		result.Parts[0].CostUSD == nil || *result.Parts[0].CostUSD != 0.01 {
		t.Fatalf("cost ledger = total %v calls %v part %+v", result.CostUSD, result.CallCosts, result.Parts[0])
	}
	var body map[string]any
	if err := json.Unmarshal(requests[0], &body); err != nil {
		t.Fatal(err)
	}
	usage, _ := body["usage"].(map[string]any)
	reasoning, _ := body["reasoning"].(map[string]any)
	if body["stream"] != true || usage["include"] != true ||
		body["temperature"] != 0.55 || body["top_p"] != float64(1) ||
		body["max_tokens"] != float64(32_000) || reasoning["effort"] != "high" ||
		body["prompt_cache_key"] == "" {
		t.Fatalf("shaped request = %s", requests[0])
	}
}

func TestCodeafAdaptiveRouterFailsOverAndRegistersOutcomes(t *testing.T) {
	// Round-2 contract: a persistent primary 500 is registered, then the retry
	// resolves through the same live router and selects another pool candidate.
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
			testRouterCandidate("openrouter/qwen/qwen-primary", adaptive.ModelTierHigh, 0),
			testRouterCandidate("openrouter/deepseek/deepseek-secondary", adaptive.ModelTierHigh, 1),
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
	backend := &openRouterBackend{
		apiKey: "test", client: client, router: router,
		sleep: func(context.Context, time.Duration) error { return nil },
	}
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen-primary",
		Prompt: "fail over", Workspace: t.TempDir(),
	})
	if err != nil || result.Text != "recovered" {
		t.Fatalf("Run = (%+v, %v)", result, err)
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

func TestCodeafRunRouterUsesLowPoolForLeafTierAgent(t *testing.T) {
	// Round-2 contract: pipeline bootstrap initializes adaptive state once and
	// baked low-tier agents resolve from --low rather than cosmetic --high.
	models := []string{}
	backend := &openRouterBackend{
		apiKey: "test",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				return nil, err
			}
			models = append(models, body.Model)
			return recordedResponse(
				request, http.StatusOK, "text/event-stream", chatReply("observed", 10),
			), nil
		})},
	}
	_ = newPipeline(cliArgs{
		High: "openrouter/qwen/qwen-high", Low: "openrouter/deepseek/deepseek-low",
	}, t.TempDir(), pipelineDeps{Backend: backend})
	if backend.router == nil {
		t.Fatal("pipeline did not install adaptive router")
	}
	_, err := backend.Run(context.Background(), turn{
		Agent: "explorer", ProviderID: "openrouter", ModelID: "qwen/qwen-high",
		Prompt: "use the leaf pool", Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "deepseek/deepseek-low" {
		t.Fatalf("routed models = %v, want low pool", models)
	}
}

func TestCodeafLeafCostCapTripsFromEngineLedger(t *testing.T) {
	// Round-2 contract: provider usage reaches LeafRunResult and the first tool
	// action, making CODEAF_LEAF_MAX_COST_USD enforceable by the scheduler guard.
	responses := []string{
		toolCallReply("bash", `{"command":"true"}`),
		chatReply("done", 10),
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := responses[0]
		responses = responses[1:]
		return recordedResponse(request, http.StatusOK, "text/event-stream", response), nil
	})}
	runtime := newRuntime(t.TempDir(), &openRouterBackend{apiKey: "test", client: client})
	t.Cleanup(runtime.Close)
	result, err := runtime.RunLeaf(context.Background(), scheduler.LeafRunRequest{
		Agent: scheduler.AgentInfo{Name: "coder"}, ProviderID: "openrouter",
		ModelID: "qwen/qwen3.6-plus", Worktree: t.TempDir(), Prompt: "spend once",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.02 || len(result.CallCosts) != 2 {
		t.Fatalf("leaf ledger = total %v calls %v", result.CostUSD, result.CallCosts)
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

func TestCodeafDeadlineCancelsMidStream(t *testing.T) {
	// Round-2 contract: the caller deadline reaches an already-open SSE stream
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
	backend := &openRouterBackend{apiKey: "test", client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := backend.Run(ctx, turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "qwen/qwen3.6-plus",
		Prompt: "wait", Workspace: t.TempDir(),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("mid-stream cancellation took %s", elapsed)
	}
}

func testRouterCandidate(
	id string, tier adaptive.ModelTier, priority float64,
) adaptive.ModelCandidate {
	return adaptive.ModelCandidate{
		ID: id, Tier: tier, Family: adaptive.DeriveFamily(id),
		PromptUSDPerMtok: 1, CompletionUSDPerMtok: 1,
		Priority: jscompat.JSNumber(priority),
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
