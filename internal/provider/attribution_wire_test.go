package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Proves the headers the chat stack puts on the wire — from a Config that says
// nothing at all about attribution, because there is nothing it can say. The
// values are constants and a caller has no way to supply, vary or omit them.
func TestAttributionOnTheWire(t *testing.T) {
	seen := http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL + "/v1",
		Model:   "openrouter/probe",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := c.newHTTPRequest(context.Background(), &ai.Request{}, []byte(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	t.Logf("HTTP-Referer: %q", seen.Get("HTTP-Referer"))
	t.Logf("X-OpenRouter-Title: %q", seen.Get("X-OpenRouter-Title"))
	t.Logf("X-OpenRouter-Categories: %q", seen.Get("X-OpenRouter-Categories"))
	if seen.Get("HTTP-Referer") != "https://agentfield.ai" {
		t.Fatalf("referer missing: %v", seen)
	}
	if seen.Get("X-OpenRouter-Title") != "AgentField AI" {
		t.Fatalf("title missing: %v", seen)
	}
	if seen.Get("X-OpenRouter-Categories") != "cli-agent,programming-app" {
		t.Fatalf("categories missing: %v", seen)
	}
}

// ── the own OpenRouter client ───────────────────────────────────────────────
//
// X-OpenRouter-Categories is the load-bearing assertion in every test below and
// not decoration: the SDK's client has no field for it and cannot send it. A
// request carrying all four headers is therefore proof that the request was
// made by this package rather than delegated.

// attributed is what every aforge-owned request must carry.
func assertAttributed(t *testing.T, header http.Header, where string) {
	t.Helper()
	for name, want := range map[string]string{
		"HTTP-Referer":            "https://agentfield.ai",
		"X-OpenRouter-Title":      "AgentField AI",
		"X-Title":                 "AgentField AI",
		"X-OpenRouter-Categories": "cli-agent,programming-app",
	} {
		if got := header.Get(name); got != want {
			t.Fatalf("%s: %s = %q, want %q", where, name, got, want)
		}
	}
}

// attributedConfig is an OpenRouter-shaped adapter configured exactly as the
// chat stack configures one — which is to say with no attribution in it, since
// the app is a constant of this package rather than a setting.
func attributedConfig(handler http.Handler) Config {
	return Config{
		APIKey:     "test-key",
		BaseURL:    "https://openrouter.ai/api/v1",
		Model:      "sim/model",
		HTTPClient: handlerClient(handler),
	}
}

// (a) A streamed chat call is made by this package, headers and all.
func TestOwnClientAttributesEveryStreamedRequest(t *testing.T) {
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"completion_tokens\":1}}\n\n" +
				"data: [DONE]\n\n"))
	})
	client, err := NewClient(attributedConfig(handler))
	if err != nil {
		t.Fatal(err)
	}

	var streamed strings.Builder
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamDelta {
			streamed.WriteString(event.Delta)
		}
	})
	response, err := client.CompleteWithMessages(ctx, userMessages("say hi"))
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "hi" || response.Text() != "hi" {
		t.Fatalf("streamed %q, accumulated %q", streamed.String(), response.Text())
	}
	if len(recorded.headers) != 1 {
		t.Fatalf("requests = %d, want 1", len(recorded.headers))
	}
	assertAttributed(t, recorded.headers[0], "streamed chat call")
	if body := recorded.body(0); body["stream"] != true {
		t.Fatalf("the observed call did not stream: %#v", body)
	}
}

// (b) A whole tool-call round trip runs on the own client: the belt goes out,
// the call comes back, the dispatcher runs, the result goes back, and the
// second turn is the answer. Both requests carry the categories header, which
// is what says neither of them was the SDK's.
func TestOwnClientRunsTheToolCallLoop(t *testing.T) {
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		if len(recorded.bodies) == 1 {
			_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"tool_calls",` +
				`"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function",` +
				`"function":{"name":"repo__read","arguments":"{\"path\":\"go.mod\"}"}}]}}],` +
				`"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"module aforge"}}],` +
			`"usage":{"prompt_tokens":20,"completion_tokens":4,"total_tokens":24}}`))
	})
	client, err := NewClient(attributedConfig(handler))
	if err != nil {
		t.Fatal(err)
	}

	var dispatchedName string
	var dispatchedArgs map[string]interface{}
	dispatch := func(_ context.Context, target string, input map[string]interface{}) (map[string]interface{}, error) {
		dispatchedName, dispatchedArgs = target, input
		return map[string]interface{}{"text": "module aforge"}, nil
	}
	tools := []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "repo__read"}}}

	response, trace, err := client.ExecuteToolCallLoop(
		context.Background(),
		userMessages("what module is this"),
		tools,
		ai.ToolCallConfig{MaxTurns: 4, MaxToolCalls: 4},
		dispatch,
	)
	if err != nil {
		t.Fatal(err)
	}

	// The name is put back the way the caller registered it.
	if dispatchedName != "repo:read" {
		t.Fatalf("dispatched tool = %q, want %q", dispatchedName, "repo:read")
	}
	if path, _ := dispatchedArgs["path"].(string); path != "go.mod" {
		t.Fatalf("dispatched arguments = %#v", dispatchedArgs)
	}
	if response.Text() != "module aforge" || trace.FinalResponse != "module aforge" {
		t.Fatalf("answer = %q, trace = %q", response.Text(), trace.FinalResponse)
	}
	if trace.TotalTurns != 2 || trace.TotalToolCalls != 1 || len(trace.Calls) != 1 {
		t.Fatalf("trace = %+v", trace)
	}
	// Every call the loop made is accounted for, not just the last one.
	if len(trace.Usage) != 2 {
		t.Fatalf("trace usage = %+v", trace.Usage)
	}

	if len(recorded.headers) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded.headers))
	}
	assertAttributed(t, recorded.headers[0], "tool-loop turn 1")
	assertAttributed(t, recorded.headers[1], "tool-loop turn 2")

	// Turn one carried the belt; turn two carried the tool result back.
	if first := recorded.body(0); first["tools"] == nil {
		t.Fatalf("turn 1 carried no tools: %#v", first)
	}
	messages, _ := recorded.body(1)["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("turn 2 messages = %#v", messages)
	}
	result, _ := messages[2].(map[string]any)
	if result["role"] != "tool" || result["tool_call_id"] != "call-1" {
		t.Fatalf("turn 2 did not return the tool result: %#v", result)
	}
	var returned map[string]string
	if err := json.Unmarshal([]byte(result["content"].(string)), &returned); err != nil {
		t.Fatal(err)
	}
	if returned["text"] != "module aforge" {
		t.Fatalf("tool result content = %#v", returned)
	}
}

// (c) The endpoint-refusal ladder still fires from inside the own loop. An
// endpoint that will not serve a belt refuses turn one; the ladder takes the
// belt off and the answer arrives, where the SDK path would have ended the turn
// on a 404.
func TestOwnClientKeepsTheRefusalLadder(t *testing.T) {
	recorded := &capture{}
	// The chat surface streams, so the turn that gets refused is a streamed one
	// — the same handler endpoints_test uses, answering in SSE once it is
	// willing to answer at all.
	client, err := NewClient(attributedConfig(streamedRefusal(func(body map[string]any) bool {
		_, hasTools := body["tools"]
		return !hasTools
	}, recorded)))
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var notices []string
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind != StreamNotice {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, event.Delta)
	})
	response, trace, err := client.ExecuteToolCallLoop(
		ctx,
		userMessages("read the file"),
		[]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}},
		ai.ToolCallConfig{MaxTurns: 4, MaxToolCalls: 4},
		func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			t.Error("no tool should have been dispatched")
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("the ladder should have landed the loop's turn: %v", err)
	}
	if response.Text() != "ok" || trace.FinalResponse != "ok" {
		t.Fatalf("answer = %q, trace = %q", response.Text(), trace.FinalResponse)
	}

	// The ladder narrated itself and ended on the rung that takes tools off.
	mu.Lock()
	defer mu.Unlock()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "removed tools") {
		t.Fatalf("retry notices = %#v", notices)
	}
	// More than one request was made, and every one of them was ours.
	if len(recorded.headers) < 2 {
		t.Fatalf("requests = %d, want the refusal plus at least one retry", len(recorded.headers))
	}
	for _, header := range recorded.headers {
		assertAttributed(t, header, "refusal-chain attempt")
	}
}
