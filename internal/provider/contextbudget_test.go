package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestContextBudgetReadsTheReportedTotalNotThePrompt(t *testing.T) {
	body := `{"error":{"message":"Upstream error from DeepInfra: This model's maximum context length is 40960 tokens. However, you requested 14746 output tokens and your prompt contains at least 26215 input tokens, for a total of at least 40961 tokens.","metadata":{"provider_name":"DeepInfra"}}}`
	failure, _ := RefusalFrom(apiError(400, []byte(body)))
	if !failure.Overflow || failure.ContextLimit != 40960 || failure.InputTokens != 26215 || failure.OutputTokens != 14746 {
		t.Fatalf("wrong evidence: %+v", failure)
	}
	other, _ := RefusalFrom(apiError(400, []byte(`{"error":{"message":"context length exceeded by 12 tokens"}}`)))
	if other.ContextLimit != 0 {
		t.Fatalf("invented total limit: %+v", other)
	}
}

func TestContextBudgetCountsToolsAndReplayedReasoning(t *testing.T) {
	client, _ := NewClient(Config{BaseURL: "http://budget.test", Model: "budget/count", Direct: true})
	request := &ai.Request{Model: "budget/count", Messages: userMessages("hello")}
	knobs := callKnobs{contextBudget: ContextBudget{Window: 8192, Reserve: 2048}}
	if _, err := client.encodeRequest(request, knobs); err != nil {
		t.Fatal(err)
	}
	request.Tools = []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "big", Parameters: map[string]any{"description": strings.Repeat("schema ", 6000)}}}}
	if _, err := client.encodeRequest(request, knobs); err == nil {
		t.Fatal("oversized tool schema was not counted")
	}
	request.Tools = nil
	request.Messages = append(request.Messages, ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "working"}}})
	knobs.reasoning = []MessageReasoning{{}, {Field: "reasoning", Text: strings.Repeat("working ", 5000), Model: request.Model}}
	if _, err := client.encodeRequest(request, knobs); err == nil {
		t.Fatal("replayed reasoning was not counted")
	}
}

func TestContextBudgetStopsOversizeBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "must not send", 500) }))
	defer server.Close()
	client, _ := NewClient(Config{APIKey: "test", BaseURL: server.URL, Model: "budget/oversize", Direct: true})
	ctx := WithContextBudget(context.Background(), ContextBudget{Window: 8192, Reserve: 2048})
	_, err := client.CompleteWithMessages(ctx, userMessages(strings.Repeat("input ", 9000)))
	failure, ok := RefusalFrom(err)
	if !ok || !failure.Local || !failure.Overflow || calls.Load() != 0 {
		t.Fatalf("error %v, requests %d", err, calls.Load())
	}
}

func TestContextBudgetFitsInputAndOutputAndIgnoresOldModelMemo(t *testing.T) {
	model := "budget/fit"
	quirksAt(t, model)
	NoteServedWindow(model, 26217)
	client, _ := NewClient(Config{BaseURL: "http://budget.test", Model: model, Direct: true})
	failure := &APIError{Overflow: true, ContextLimit: 40960, Provider: "DeepInfra"}
	client.rememberContextLimit(model, failure)
	request := &ai.Request{Model: model, Messages: userMessages(strings.Repeat("x", 26215*4))}
	body, err := client.encodeRequest(request, callKnobs{contextBudget: ContextBudget{Window: 131072, Reserve: 65536}})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err = json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.MaxTokens <= 0 || 26215+wire.MaxTokens+ContextSafetyTokens(40960) > 40960 {
		t.Fatalf("output allowance %d does not fit", wire.MaxTokens)
	}
	// A pinned different endpoint and a different account do not inherit this
	// endpoint's refusal. An advisory order may still land on the smaller one.
	if got := client.servingWindow(model, &providerPrefs{Only: []string{"Other"}}, 131072); got != 131072 {
		t.Fatalf("other endpoint got %d", got)
	}
	other, _ := NewClient(Config{BaseURL: "http://another.test", Model: model, Direct: true})
	if got := other.servingWindow(model, nil, 131072); got != 131072 {
		t.Fatalf("other account got %d", got)
	}
	quirks.settle()
	path, _ := quirks.snapshot()
	fresh := &quirksStore{mandatory: map[string]time.Time{}, disableIgnored: map[string]time.Time{}, noCacheControl: map[string]time.Time{}, noReasoningBudget: map[string]time.Time{}, noReasoningReplay: map[string]time.Time{}, answerCut: map[string]int{}, servedWindow: map[string]int{}}
	fresh.load(path)
	key := contextLimitKey(client.config.BaseURL, model, "DeepInfra")
	if got := fresh.contextLimits[key].Tokens; got != 40960 {
		t.Fatalf("reloaded context limit = %d", got)
	}
}

func TestContextBudgetAllowsShorterAnswerOnSmallWindow(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls.Add(1)
		// The reported lean launch leaves 1,663 tokens after its safety margin.
		if body.MaxTokens != 1663 {
			t.Errorf("output allowance = %d, want the available 1663", body.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A short answer fits."},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "budget/small-answer", Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithContextBudget(context.Background(), ContextBudget{Window: 16385, Reserve: 4096, PromptFloor: 13903})
	response, err := client.CompleteWithMessages(ctx, userMessages("Explain the conjecture."))
	if err != nil || response == nil || calls.Load() != 1 {
		t.Fatalf("response=%v error=%v HTTP requests=%d", response, err, calls.Load())
	}
}

func TestContextBudgetStillRequiresUsefulAnswerRoom(t *testing.T) {
	client, _ := NewClient(Config{BaseURL: "http://budget.test", Model: "budget/minimum", Direct: true})
	request := &ai.Request{Model: "budget/minimum", Messages: userMessages("hello")}
	for _, test := range []struct {
		name      string
		room      int
		wantError bool
	}{
		{"short answer fits", minimumContextAnswer, false},
		{"not enough room", minimumContextAnswer - 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			knobs := callKnobs{contextBudget: ContextBudget{Window: 16385, Reserve: 4096, PromptFloor: 16385 - ContextSafetyTokens(16385) - test.room}}
			body, err := client.encodeRequest(request, knobs)
			if (err != nil) != test.wantError {
				t.Fatalf("encode error=%v", err)
			}
			if err == nil {
				var wire struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.Unmarshal(body, &wire); err != nil {
					t.Fatal(err)
				}
				if wire.MaxTokens != test.room {
					t.Fatalf("allowance=%d room=%d", wire.MaxTokens, test.room)
				}
			}
		})
	}
}
