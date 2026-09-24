package modelapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// seenCall is one call as the funnel was handed it: the model, the messages,
// the SDK request the options build, and the per-call facts on the context.
type seenCall struct {
	completerModel string
	messages       []ai.Message
	request        ai.Request
	cacheKey       string
	effort         provider.Effort
	reasoning      []provider.MessageReasoning
	role           lanes.Role
}

// script is a funnel a test writes: it records every call and answers with
// whatever reply says.
type script struct {
	mu    sync.Mutex
	seen  []seenCall
	reply func(ctx context.Context, model string, messages []ai.Message, request ai.Request) (*ai.Response, error)
}

func (s *script) completerFor(model string) modelapi.Completer {
	return scriptCall{script: s, model: model}
}

func (s *script) calls() []seenCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenCall(nil), s.seen...)
}

type scriptCall struct {
	script *script
	model  string
}

func (c scriptCall) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	c.script.mu.Lock()
	c.script.seen = append(c.script.seen, seenCall{
		completerModel: c.model, messages: messages, request: request,
		cacheKey: provider.CacheKeyFrom(ctx), effort: provider.ReasoningEffortFrom(ctx),
		reasoning: provider.MessageReasoningFrom(ctx), role: provider.RoleFrom(ctx),
	})
	c.script.mu.Unlock()
	return c.script.reply(ctx, request.Model, messages, request)
}

// bill is the funnel telling whoever armed the call what an answer cost, the
// way the provider's decode does.
func bill(ctx context.Context, model string, in, out, cached int, cost float64) {
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: model, PromptTokens: in, CompletionTokens: out, CachedTokens: cached, Cost: cost})
	}
}

// saying is an answer of words.
func saying(model, text string) *ai.Response {
	return &ai.Response{ID: "upstream-1", Model: model, Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "stop",
	}}}
}

// words answers every call with its text, billed at cost.
func words(text string, cost float64) func(context.Context, string, []ai.Message, ai.Request) (*ai.Response, error) {
	return func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		bill(ctx, model, 100, 20, 30, cost)
		return saying(model, text), nil
	}
}

// open starts an API for one test and closes it after.
func open(t *testing.T, config modelapi.Config) (*modelapi.Server, delegate.ModelAPI) {
	t.Helper()
	server, err := modelapi.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, server.API()
}

// post sends one body to the API with the token given.
func post(t *testing.T, api delegate.ModelAPI, token, body string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, modelapi.ChatURL(api.BaseURL), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

// errorOf reads the router's error envelope.
func errorOf(t *testing.T, payload []byte) (string, int) {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("not an error envelope: %s", payload)
	}
	return body.Error.Message, body.Error.Code
}

// whole reads a whole completion.
type whole struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role      string  `json:"role"`
			Content   *string `json:"content"`
			Reasoning string  `json:"reasoning"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int      `json:"prompt_tokens"`
		CompletionTokens    int      `json:"completion_tokens"`
		TotalTokens         int      `json:"total_tokens"`
		Cost                *float64 `json:"cost"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// events splits an event stream into its data payloads and counts its
// comment lines.
func events(body []byte) (data []string, comments int) {
	for _, block := range strings.Split(string(body), "\n\n") {
		block = strings.TrimSpace(block)
		switch {
		case block == "":
		case strings.HasPrefix(block, ":"):
			comments++
		case strings.HasPrefix(block, "data: "):
			data = append(data, strings.TrimPrefix(block, "data: "))
		}
	}
	return data, comments
}

// rawTurns is every line the conversation log holds, in the order written.
func rawTurns(t *testing.T, dir string) []delegate.Turn {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, delegate.ConversationFile))
	if err != nil {
		t.Fatal(err)
	}
	var turns []delegate.Turn
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var turn delegate.Turn
		if err := json.Unmarshal([]byte(line), &turn); err != nil {
			t.Fatalf("a log line does not parse: %s", line)
		}
		turns = append(turns, turn)
	}
	return turns
}

const hello = `{"model":"deepseek/deepseek-v4-flash-0731","messages":[{"role":"user","content":"hi"}]}`

// THE TOKEN IS THE ONLY WAY IN, AND IT DIES WITH THE RUN: no token and a wrong
// one are both refused in the router's own shape, the right one is answered,
// and after Close the API hands out no token and the port answers nobody.
func TestTheAPIOpensToItsTokenAloneAndClosesWithTheRun(t *testing.T) {
	calls := &script{reply: words("hello", 0.01)}
	server, api := open(t, modelapi.Config{CompleterFor: calls.completerFor})
	if !strings.HasPrefix(api.BaseURL, "http://127.0.0.1:") || !strings.HasSuffix(api.BaseURL, "/v1") || len(api.Token) < 32 {
		t.Fatalf("api = %+v, want a loopback /v1 base and a real token", api)
	}
	for _, token := range []string{"", "not-the-token"} {
		status, payload := post(t, api, token, hello)
		if message, code := errorOf(t, payload); status != http.StatusUnauthorized || code != 401 || message == "" {
			t.Fatalf("token %q: status %d body %s, want 401 in the router's shape", token, status, payload)
		}
	}
	if len(calls.calls()) != 0 {
		t.Fatal("a refused token reached the funnel")
	}
	if status, payload := post(t, api, api.Token, hello); status != http.StatusOK {
		t.Fatalf("the right token was answered %d: %s", status, payload)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if after := server.API(); after.Token != "" || after.Ready() {
		t.Fatalf("a closed API still hands out %+v", after)
	}
	request, _ := http.NewRequest(http.MethodPost, modelapi.ChatURL(api.BaseURL), strings.NewReader(hello))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
		t.Fatalf("the old token still opens a closed API: %d", response.StatusCode)
	}
}

// A CALL PAST THE CEILING IS NEVER MADE: it is answered 402 in the router's
// shape, the funnel is not asked, the refusal is a turn of the log, and every
// charge before it reached the bank in order with the run's rising total.
func TestACallPastTheCeilingIsRefusedBeforeItIsMade(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("ok", 0.06)}
	var mu sync.Mutex
	var banked []modelapi.Charge
	server, api := open(t, modelapi.Config{
		TaskDir: dir, CompleterFor: calls.completerFor, Ceiling: 0.10,
		Bank: func(charge modelapi.Charge) {
			mu.Lock()
			defer mu.Unlock()
			banked = append(banked, charge)
		},
	})
	for call := 0; call < 2; call++ {
		if status, payload := post(t, api, api.Token, hello); status != http.StatusOK {
			t.Fatalf("call %d under the ceiling was answered %d: %s", call+1, status, payload)
		}
	}
	status, payload := post(t, api, api.Token, hello)
	message, code := errorOf(t, payload)
	if status != http.StatusPaymentRequired || code != 402 || !strings.Contains(message, "ceiling of $0.10") {
		t.Fatalf("the call past the ceiling was answered %d: %s", status, payload)
	}
	if got := len(calls.calls()); got != 2 {
		t.Fatalf("the funnel was asked %d times, want the two calls under the ceiling and not the third", got)
	}
	if refused := server.RefusedAtCeiling(); refused != 1 {
		t.Fatalf("refused at the ceiling = %d, want the one call", refused)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(banked) != 2 || banked[0].Spent != 0.06 || banked[1].Spent != 0.12 || banked[1].CostUSD != 0.06 ||
		banked[0].TokensIn != 100 || banked[0].Cached != 30 || banked[0].Model != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("banked = %+v", banked)
	}
	turns, err := delegate.ReadTurns(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 3 || turns[2].Refused == "" || turns[2].Ended.IsZero() || turns[2].CostUSD != 0 || turns[2].InFlight() {
		t.Fatalf("turns = %+v, want the third written as a refusal that cost nothing", turns)
	}
}

// THE WHOLE BODY REACHES THE FUNNEL AND THE WHOLE ANSWER COMES BACK: tools and
// the program's own tool_choice, a tool call and its result, the reasoning
// depth, the cache key, the response format, the working handed back — and
// the model's tool calls, its words and its working on the way out.
func TestTheCallCrossesIntoTheFunnelWholeAndTheAnswerComesBackWhole(t *testing.T) {
	calls := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		provider.EmitReasoning(ctx, "reasoning", "plan first", json.RawMessage(`[{"type":"reasoning.text","text":"plan first"}]`))
		bill(ctx, model, 1200, 400, 1000, 0.0042)
		return &ai.Response{ID: "upstream", Model: model, Choices: []ai.Choice{{
			Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "call_9", Type: "function", Function: ai.ToolCallFunction{Name: "edit", Arguments: `{"path":"a.go"}`}}}},
		}}}, nil
	}}
	_, api := open(t, modelapi.Config{CompleterFor: calls.completerFor, Role: lanes.RoleLeafAttached})
	body := `{
		"model": "moonshotai/kimi-k2.6",
		"messages": [
			{"role": "system", "content": "be brief"},
			{"role": "user", "content": "fix it"},
			{"role": "assistant", "content": "", "reasoning_content": "earlier working", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "bash", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "FAIL"}
		],
		"tools": [{"type": "function", "function": {"name": "edit", "parameters": {"type": "object"}}}],
		"tool_choice": "required",
		"max_tokens": 4096,
		"reasoning_effort": "low",
		"prompt_cache_key": "sd-main",
		"response_format": {"type": "json_object"}
	}`
	status, payload := post(t, api, api.Token, body)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	seen := calls.calls()
	if len(seen) != 1 {
		t.Fatalf("the funnel was asked %d times", len(seen))
	}
	call := seen[0]
	if call.completerModel != "moonshotai/kimi-k2.6" || call.request.Model != "moonshotai/kimi-k2.6" {
		t.Fatalf("model = %q / %q", call.completerModel, call.request.Model)
	}
	if len(call.messages) != 4 || call.messages[2].ToolCalls[0].ID != "call_1" || call.messages[3].ToolCallID != "call_1" {
		t.Fatalf("messages = %+v", call.messages)
	}
	if len(call.request.Tools) != 1 || call.request.Tools[0].Function.Name != "edit" || call.request.ToolChoice != "required" {
		t.Fatalf("tools %+v choice %#v", call.request.Tools, call.request.ToolChoice)
	}
	if call.request.MaxTokens == nil || *call.request.MaxTokens != 4096 || call.request.ResponseFormat == nil || call.request.ResponseFormat.Type != "json_object" {
		t.Fatalf("request = %+v", call.request)
	}
	if call.cacheKey != "sd-main" || call.effort != provider.EffortLow || call.role != lanes.RoleLeafAttached {
		t.Fatalf("cache key %q effort %q role %q", call.cacheKey, call.effort, call.role)
	}
	if len(call.reasoning) != 4 || call.reasoning[2].Field != "reasoning_content" || call.reasoning[2].Text != "earlier working" {
		t.Fatalf("working handed back = %+v", call.reasoning)
	}
	var answer whole
	if err := json.Unmarshal(payload, &answer); err != nil {
		t.Fatalf("%v: %s", err, payload)
	}
	choice := answer.Choices[0]
	if answer.Object != "chat.completion" || choice.FinishReason != "tool_calls" || choice.Message.Content != nil ||
		len(choice.Message.ToolCalls) != 1 || choice.Message.ToolCalls[0].ID != "call_9" || choice.Message.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("answer = %s", payload)
	}
	if choice.Message.Reasoning != "plan first" || !strings.Contains(string(payload), `"reasoning_details":[{"type":"reasoning.text"`) {
		t.Fatalf("the model's working did not come back: %s", payload)
	}
	if answer.Usage.Cost == nil || *answer.Usage.Cost != 0.0042 || answer.Usage.PromptTokens != 1200 || answer.Usage.TotalTokens != 1600 || answer.Usage.PromptTokensDetails.CachedTokens != 1000 {
		t.Fatalf("usage = %+v", answer.Usage)
	}
}

// A THINKING MODEL'S WORKING MAKES THE ROUND TRIP: an endpoint that writes it
// as reasoning_content has it handed to the program under the router's own
// `reasoning`, and the program handing it back that way has it replayed to the
// endpoint under the field it came in with.
func TestAModelsWorkingGoesOutUnderTheRoutersNameAndComesBackUnderItsOwn(t *testing.T) {
	calls := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		provider.EmitReasoning(ctx, "reasoning_content", "run the tests first", nil)
		return &ai.Response{Model: model, Choices: []ai.Choice{{
			Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"}}}},
		}}}, nil
	}}
	_, api := open(t, modelapi.Config{CompleterFor: calls.completerFor})
	status, payload := post(t, api, api.Token, `{"model":"m","messages":[{"role":"user","content":"fix it"}]}`)
	if status != http.StatusOK || !strings.Contains(string(payload), `"reasoning":"run the tests first"`) || strings.Contains(string(payload), "reasoning_content") {
		t.Fatalf("status %d, the working did not go out under the router's name: %s", status, payload)
	}
	back := `{"model":"m","messages":[{"role":"user","content":"fix it"},` +
		`{"role":"assistant","content":null,"reasoning":"run the tests first","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"ok"}]}`
	if status, payload := post(t, api, api.Token, back); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	seen := calls.calls()
	if len(seen) != 2 || len(seen[1].reasoning) != 3 || seen[1].reasoning[1].Field != "reasoning_content" || seen[1].reasoning[1].Text != "run the tests first" {
		t.Fatalf("the working handed back reached the funnel as %+v", seen[len(seen)-1].reasoning)
	}
}

// A STREAM IS THE ROUTER'S STREAM: the words as a delta, the finish, then a
// chunk carrying the usage with its cost, then [DONE].
func TestAStreamedAnswerEndsWithItsCostThenDone(t *testing.T) {
	calls := &script{reply: words("all green", 0.0125)}
	_, api := open(t, modelapi.Config{CompleterFor: calls.completerFor})
	status, payload := post(t, api, api.Token, `{"model":"z-ai/glm-5.1","stream":true,"messages":[{"role":"user","content":"go"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	data, _ := events(payload)
	if len(data) < 3 || data[len(data)-1] != "[DONE]" {
		t.Fatalf("events = %q, want chunks and then [DONE]", data)
	}
	var content strings.Builder
	var finish string
	var cost *float64
	for _, event := range data[:len(data)-1] {
		var chunk struct {
			Object  string `json:"object"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Cost *float64 `json:"cost"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(event), &chunk); err != nil || chunk.Object != "chat.completion.chunk" {
			t.Fatalf("event %s: %v", event, err)
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
		}
		if chunk.Usage != nil {
			cost = chunk.Usage.Cost
		}
	}
	if content.String() != "all green" || finish != "stop" || cost == nil || *cost != 0.0125 {
		t.Fatalf("content %q finish %q cost %v", content.String(), finish, cost)
	}
}

// A MODEL THAT THINKS FOR A LONG TIME NEVER LOOKS LIKE A DEAD CONNECTION: a
// stream is sent comment lines while it waits, and a whole body is sent the
// whitespace JSON allows before its value, and both still read as what they
// are.
func TestAWaitingAnswerSaysItIsStillComing(t *testing.T) {
	calls := &script{reply: func(ctx context.Context, model string, messages []ai.Message, request ai.Request) (*ai.Response, error) {
		time.Sleep(150 * time.Millisecond)
		return words("slow", 0.001)(ctx, model, messages, request)
	}}
	_, api := open(t, modelapi.Config{CompleterFor: calls.completerFor, Keepalive: 20 * time.Millisecond})
	_, payload := post(t, api, api.Token, `{"model":"m","stream":true,"messages":[{"role":"user","content":"go"}]}`)
	data, comments := events(payload)
	if comments < 2 || !strings.HasPrefix(string(payload), ": keepalive\n\n") || data[len(data)-1] != "[DONE]" {
		t.Fatalf("%d comments before the answer, want several:\n%s", comments, payload)
	}
	status, payload := post(t, api, api.Token, `{"model":"m","messages":[{"role":"user","content":"go"}]}`)
	if status != http.StatusOK || !strings.HasPrefix(string(payload), "\n") {
		t.Fatalf("status %d, a whole body with no whitespace kept alive: %q", status, payload)
	}
	var answer whole
	if err := json.Unmarshal(bytes.TrimSpace(payload), &answer); err != nil || *answer.Choices[0].Message.Content != "slow" {
		t.Fatalf("the kept-alive body no longer reads: %v %s", err, payload)
	}
	if err := json.Unmarshal(payload, &answer); err != nil {
		t.Fatalf("a JSON reader refuses the leading whitespace: %v", err)
	}
}

// EVERY CALL IS WRITTEN TWICE UNDER ONE NUMBER — when it starts and when it
// ends — and what a turn says it sent is only what the thread's previous
// request did not carry.
func TestEveryCallIsOneTurnWrittenAtItsStartAndItsEnd(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("done", 0.002)}
	_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor})
	first := `{"model":"qwen/qwen3.6-plus","messages":[{"role":"system","content":"rules"},{"role":"user","content":"fix the test"}]}`
	second := `{"model":"qwen/qwen3.6-plus","messages":[{"role":"system","content":"rules"},{"role":"user","content":"fix the test"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"go test\"}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"FAIL: TestX"}]}`
	for _, body := range []string{first, second} {
		if status, payload := post(t, api, api.Token, body); status != http.StatusOK {
			t.Fatalf("status %d: %s", status, payload)
		}
	}
	lines := rawTurns(t, dir)
	if len(lines) != 4 {
		t.Fatalf("%d records, want two per call", len(lines))
	}
	for index, line := range lines {
		wantSeq, ended := index/2+1, index%2 == 1
		if line.Seq != wantSeq || line.Ended.IsZero() == ended || line.Thread != delegate.MainThread {
			t.Fatalf("record %d = %+v, want seq %d ended %v on the main thread", index, line, wantSeq, ended)
		}
	}
	if !lines[0].InFlight() || lines[1].InFlight() {
		t.Fatal("the start record does not read as in flight, or the end record still does")
	}
	turns, err := delegate.ReadTurns(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("%d turns, want two", len(turns))
	}
	one, two := turns[0], turns[1]
	if len(one.Sent) != 2 || one.Sent[0].Role != "system" || one.Sent[1].Text != "fix the test" || one.Restarted {
		t.Fatalf("first turn sent %+v", one.Sent)
	}
	if len(two.Sent) != 1 || two.Sent[0].Role != "tool" || two.Sent[0].Tool != "bash" || two.Sent[0].Text != "FAIL: TestX" || two.Restarted {
		t.Fatalf("second turn sent %+v, want only the tool's result", two.Sent)
	}
	if two.Model != "qwen/qwen3.6-plus" || two.Served != "" || two.Reply != "done" || two.TokensIn != 100 || two.TokensOut != 20 || two.Cached != 30 || two.CostUSD != 0.002 {
		t.Fatalf("second turn = %+v", two)
	}
}

// A REWRITTEN HISTORY IS SAID TO BE ONE, AND TWO THREADS ARE TWO
// CONVERSATIONS: each thread's first call sends its whole brief, and a thread
// whose next request is not its last one extended is a restart.
func TestARewrittenHistoryIsARestartAndThreadsAreKeptApart(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("ok", 0)}
	_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor})
	send := func(key, messages string) {
		t.Helper()
		body := `{"model":"m","prompt_cache_key":"` + key + `","messages":[` + messages + `]}`
		if status, payload := post(t, api, api.Token, body); status != http.StatusOK {
			t.Fatalf("status %d: %s", status, payload)
		}
	}
	send("coder", `{"role":"user","content":"write it"},{"role":"assistant","content":"written"},{"role":"user","content":"now test it"}`)
	send("summariser", `{"role":"user","content":"summarise the coder"}`)
	send("coder", `{"role":"user","content":"summary: written and tested"},{"role":"user","content":"ship it"}`)
	turns, err := delegate.ReadTurns(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 3 {
		t.Fatalf("%d turns", len(turns))
	}
	if turns[0].Thread != "coder" || len(turns[0].Sent) != 2 || turns[0].Restarted {
		t.Fatalf("coder's first turn = %+v, want its two words of its own and not the model's", turns[0])
	}
	if turns[1].Thread != "summariser" || len(turns[1].Sent) != 1 || turns[1].Restarted {
		t.Fatalf("the second thread's first turn = %+v, want a first call of its own", turns[1])
	}
	if !turns[2].Restarted || len(turns[2].Sent) != 2 || turns[2].Sent[0].Text != "summary: written and tested" {
		t.Fatalf("the rewritten coder turn = %+v, want a restart that sends it whole", turns[2])
	}
}

// A LINEAGE NAMED ONLY IN THE ROUTER'S HEADER IS STILL THE CALL'S LINEAGE: it
// is the cache key the funnel is handed and the thread the log keeps, and a
// body's own key wins over it.
func TestTheSessionAffinityHeaderIsTheLineageWhenTheBodyNamesNone(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("ok", 0)}
	_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor})
	for _, body := range []string{hello, `{"model":"m","prompt_cache_key":"from-body","messages":[{"role":"user","content":"hi"}]}`} {
		request, _ := http.NewRequest(http.MethodPost, modelapi.ChatURL(api.BaseURL), strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+api.Token)
		request.Header.Set("x-session-affinity", "from-header")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	seen := calls.calls()
	if len(seen) != 2 || seen[0].cacheKey != "from-header" || seen[1].cacheKey != "from-body" {
		t.Fatalf("cache keys = %+v", seen)
	}
	if turns, _ := delegate.ReadTurns(dir, 0); len(turns) != 2 || turns[0].Thread != "from-header" || turns[1].Thread != "from-body" {
		t.Fatalf("threads = %+v", turns)
	}
}

// A MODEL THIS MACHINE CANNOT SERVE IS ANSWERED ON THE RUN'S SEAT, AND THE
// TURN SAYS SO: the program asked for one id, the funnel was handed the seat,
// and Served names what answered.
func TestAModelThisMachineCannotServeIsAnsweredOnTheSeat(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("from the seat", 0.003)}
	_, api := open(t, modelapi.Config{
		TaskDir: dir, CompleterFor: calls.completerFor, Seat: "mybox/qwen3-coder",
		Serves: func(model string) bool { return strings.HasPrefix(model, "mybox/") },
	})
	status, payload := post(t, api, api.Token, `{"model":"openrouter/deepseek/deepseek-v4-pro","messages":[{"role":"user","content":"go"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	if seen := calls.calls(); len(seen) != 1 || seen[0].request.Model != "mybox/qwen3-coder" || seen[0].completerModel != "mybox/qwen3-coder" {
		t.Fatalf("the funnel was handed %+v, want the seat", seen)
	}
	turns, _ := delegate.ReadTurns(dir, 0)
	if len(turns) != 1 || turns[0].Model != "openrouter/deepseek/deepseek-v4-pro" || turns[0].Served != "mybox/qwen3-coder" {
		t.Fatalf("turn = %+v, want the ask kept and the seat named as what answered", turns)
	}
}

// A CALL IS NEVER LOST ONLY BECAUSE THIS MACHINE DOES NOT KNOW THE ID: when the
// funnel itself says it cannot serve the ask — no key for its service — the
// call goes out once more on the seat.
func TestAnAskTheFunnelCannotServeFallsToTheSeat(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: func(ctx context.Context, model string, messages []ai.Message, request ai.Request) (*ai.Response, error) {
		if model != "seat/model" {
			return nil, provider.ErrNoAPIKey
		}
		return words("seated", 0.001)(ctx, model, messages, request)
	}}
	_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor, Seat: "seat/model"})
	status, payload := post(t, api, api.Token, `{"model":"minimax/minimax-m2.7","messages":[{"role":"user","content":"go"}]}`)
	if status != http.StatusOK || !strings.Contains(string(payload), "seated") {
		t.Fatalf("status %d: %s", status, payload)
	}
	if seen := calls.calls(); len(seen) != 2 || seen[1].request.Model != "seat/model" {
		t.Fatalf("the funnel saw %+v, want the ask and then the seat", seen)
	}
	if turns, _ := delegate.ReadTurns(dir, 0); len(turns) != 1 || turns[0].Served != "seat/model" || turns[0].Failed != "" {
		t.Fatalf("turns = %+v", turns)
	}
}

// A MODEL'S FAILURE IS THE ROUTER'S ERROR WITH A STATUS THAT MEANS THE SAME
// THING — and an account refused upstream is a gateway's refusal, never the
// program's own token being wrong.
func TestAModelFailureIsTheRoutersErrorAndItsTurnSaysSo(t *testing.T) {
	dir := t.TempDir()
	var refusal error
	calls := &script{reply: func(context.Context, string, []ai.Message, ai.Request) (*ai.Response, error) { return nil, refusal }}
	_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor})
	for _, row := range []struct {
		err    error
		status int
	}{
		{&provider.APIError{Status: 429, Message: "slow down"}, 429},
		{&provider.APIError{Status: 401, Message: "no such account"}, 502},
		{errors.New("connection reset"), 502},
	} {
		refusal = row.err
		status, payload := post(t, api, api.Token, hello)
		message, code := errorOf(t, payload)
		if status != row.status || code != row.status || message == "" {
			t.Fatalf("%v: status %d body %s, want %d", row.err, status, payload, row.status)
		}
	}
	turns, _ := delegate.ReadTurns(dir, 0)
	if len(turns) != 3 || !strings.Contains(turns[0].Failed, "slow down") || turns[0].Ended.IsZero() {
		t.Fatalf("turns = %+v, want each failure written with its sentence", turns)
	}
	// A run started with no road answers with that sentence and makes no call.
	_, bare := open(t, modelapi.Config{TaskDir: t.TempDir()})
	status, payload := post(t, bare, bare.Token, hello)
	if message, _ := errorOf(t, payload); status != http.StatusServiceUnavailable || !strings.Contains(message, "no road to a model") {
		t.Fatalf("a road-less run answered %d: %s", status, payload)
	}
}

// A STREAM CUT BEFORE ITS USAGE BLOCK IS PRICED LATE AND STILL COUNTED ONCE:
// the receipt reaches the bank marked late, and the call's turn is written
// again with the figure; a receipt that never comes is told as unbilled.
func TestALateReceiptIsBankedAndItsTurnRewritten(t *testing.T) {
	dir := t.TempDir()
	late := make(chan struct{})
	calls := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		// Owed the way the provider owes a receipt: before the fetch, and
		// answered after the sink.
		done := provider.ReceiptPendingFrom(ctx)()
		sink := provider.ReconcileSinkFrom(ctx)
		go func() {
			defer done()
			time.Sleep(50 * time.Millisecond)
			sink(provider.Reconciled{Billed: provider.Billed{Model: model, PromptTokens: 50, CompletionTokens: 5, Cost: 0.02}, Found: true})
			sink(provider.Reconciled{Billed: provider.Billed{Model: "other"}, Found: false})
			close(late)
		}()
		return saying(model, "cut short"), nil
	}}
	var mu sync.Mutex
	var banked []modelapi.Charge
	var unbilled []string
	_, api := open(t, modelapi.Config{
		TaskDir: dir, CompleterFor: calls.completerFor,
		Bank:     func(charge modelapi.Charge) { mu.Lock(); banked = append(banked, charge); mu.Unlock() },
		Unbilled: func(model string) { mu.Lock(); unbilled = append(unbilled, model); mu.Unlock() },
	})
	if status, payload := post(t, api, api.Token, hello); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	<-late
	mu.Lock()
	if len(banked) != 1 || !banked[0].Late || banked[0].CostUSD != 0.02 || banked[0].Spent != 0.02 || len(unbilled) != 1 || unbilled[0] != "other" {
		t.Fatalf("banked %+v unbilled %v", banked, unbilled)
	}
	mu.Unlock()
	turns, _ := delegate.ReadTurns(dir, 0)
	if len(turns) != 1 || turns[0].CostUSD != 0.02 || turns[0].TokensIn != 50 {
		t.Fatalf("turn = %+v, want it rewritten with the late receipt", turns)
	}
}

// THE RUN'S BOOKS CLOSE WITH THE RECEIPT OF THE CALL IT WAS CUT IN: the funnel
// owes a receipt for a call that ended without its usage block, and it arrives
// well after the call has returned — the stopped runs of 2026-09-23 saw it
// twenty seconds later. Close waits for it, so the total read after Close and
// the bank both hold it, and the watcher is told once how many were owed. A
// receipt that never comes costs the ending no more than the bound.
func TestCloseWaitsForTheReceiptOwedOnACutCall(t *testing.T) {
	gate := make(chan struct{})
	calls := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		// The provider's own order: owed before the fetch, answered after the
		// sink has the money.
		done := provider.ReceiptPendingFrom(ctx)()
		sink := provider.ReconcileSinkFrom(ctx)
		go func() {
			defer done()
			<-gate
			sink(provider.Reconciled{Billed: provider.Billed{Model: model, PromptTokens: 52139, CompletionTokens: 4895, Cost: 0.058188488}, Found: true})
		}()
		return saying(model, "cut short"), nil
	}}
	var mu sync.Mutex
	var banked []modelapi.Charge
	var settling []int
	server, api := open(t, modelapi.Config{
		CompleterFor: calls.completerFor,
		Bank:         func(charge modelapi.Charge) { mu.Lock(); banked = append(banked, charge); mu.Unlock() },
		Settling:     func(owed int) { mu.Lock(); settling = append(settling, owed); mu.Unlock() },
	})
	if status, payload := post(t, api, api.Token, hello); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	if spent := server.Spent(); spent != 0 {
		t.Fatalf("spent %v before the receipt came", spent)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(gate)
	}()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if spent := server.Spent(); spent != 0.058188488 {
		t.Fatalf("spent after Close = %v, want the late receipt's $0.058188488 in it", spent)
	}
	mu.Lock()
	if len(banked) != 1 || !banked[0].Late || banked[0].CostUSD != 0.058188488 || len(settling) != 1 || settling[0] != 1 {
		mu.Unlock()
		t.Fatalf("banked %+v settling %v, want the one late charge banked before Close returned, told once", banked, settling)
	}
	mu.Unlock()

	// A receipt that never comes: Close gives up at the bound.
	defer modelapi.ShortenReceiptWait(150 * time.Millisecond)()
	never := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		provider.ReceiptPendingFrom(ctx)()
		return saying(model, "cut short"), nil
	}}
	stuck, stuckAPI := open(t, modelapi.Config{CompleterFor: never.completerFor})
	if status, payload := post(t, stuckAPI, stuckAPI.Token, hello); status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	began := time.Now()
	if err := stuck.Close(); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(began); waited < 100*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("Close waited %s for a receipt that never came, want about the bound", waited)
	}
}

// THE PAGE NAMES THE MODEL THAT ANSWERED. When nothing here could take the ask
// or the seat, the conversation's own pool put the call on its seat, which this
// API never hears of — run 3d6d asked qwen and was answered by gpt-5.6-sol, at a
// price its service does not report. The funnel's bill names who answered, and
// the turn says so; an ask answered by the same model under another spelling,
// or by a dated build of it, names nothing more.
func TestATurnNamesTheModelTheFunnelBilledWhenItIsNotTheAsk(t *testing.T) {
	for _, row := range []struct {
		name, asked, billed, answer, want string
	}{
		{name: "the pool's seat answered", asked: "qwen/qwen3.6-plus", billed: "gpt-5.6-sol", want: "gpt-5.6-sol"},
		{name: "the same model without its service", asked: "openrouter/deepseek/deepseek-v4-pro", billed: "deepseek/deepseek-v4-pro", want: ""},
		{name: "a dated build of the ask", asked: "deepseek/deepseek-v4-pro", billed: "deepseek/deepseek-v4-pro-0731", want: ""},
		{name: "nothing billed, the answer names another", asked: "moonshotai/kimi-k2.6", answer: "z-ai/glm-5.1", want: "z-ai/glm-5.1"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := &script{reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
				answered := model
				if row.billed != "" {
					// A service that reports no price: tokens, and no dollars.
					bill(ctx, row.billed, 58511, 405, 55356, 0)
					answered = row.billed
				}
				if row.answer != "" {
					answered = row.answer
				}
				return saying(answered, "done"), nil
			}}
			_, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor})
			body := `{"model":"` + row.asked + `","messages":[{"role":"user","content":"go"}]}`
			if status, payload := post(t, api, api.Token, body); status != http.StatusOK {
				t.Fatalf("status %d: %s", status, payload)
			}
			turns, _ := delegate.ReadTurns(dir, 0)
			if len(turns) != 1 || turns[0].Model != row.asked || turns[0].Served != row.want {
				t.Fatalf("turn = %+v, want the ask %q kept and %q named as what answered", turns, row.asked, row.want)
			}
		})
	}
}

// A RUN WITH NOTHING LEFT OF ITS LIMIT MAKES NO CALL AT ALL. The conversation
// hands such a run the smallest positive ceiling, because zero means none; the
// first call used to go through and be paid for, since nothing spent was still
// "under" it. It is refused with the ceiling's own sentence, and the funnel is
// never asked.
func TestARunWhoseCeilingIsAlreadySpentMakesNoCall(t *testing.T) {
	dir := t.TempDir()
	calls := &script{reply: words("paid for", 0.139463)}
	server, api := open(t, modelapi.Config{TaskDir: dir, CompleterFor: calls.completerFor, Ceiling: math.SmallestNonzeroFloat64})
	status, payload := post(t, api, api.Token, hello)
	message, code := errorOf(t, payload)
	if status != http.StatusPaymentRequired || code != 402 ||
		message != "the run's dollar ceiling of $0.00 is reached ($0.00 spent), so codeaf made no call" {
		t.Fatalf("the first call of a spent run was answered %d: %s", status, payload)
	}
	if len(calls.calls()) != 0 || server.Spent() != 0 || server.RefusedAtCeiling() != 1 {
		t.Fatalf("the funnel was asked %d times, spent %v, refused %d", len(calls.calls()), server.Spent(), server.RefusedAtCeiling())
	}
}

// AN ANSWER NOTHING PRICED IS SAID, NOT SILENT. A call answered whole whose
// answer carried no usage block was billed nowhere and marked nowhere; it is
// told as a call nobody could price, with no money invented. A call the funnel
// billed — at a price or at none — and a call whose receipt is owed are not.
func TestAnAnsweredCallNothingPricedIsToldAsUnbilled(t *testing.T) {
	for _, row := range []struct {
		name  string
		reply func(context.Context, string, []ai.Message, ai.Request) (*ai.Response, error)
		want  []string
	}{
		{name: "no usage block, no receipt", want: []string{"moonshotai/kimi-k2.6"},
			reply: func(_ context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
				return saying(model, "whole"), nil
			}},
		{name: "billed", reply: words("whole", 0.03)},
		{name: "billed with no price", reply: words("whole", 0)},
		{name: "a receipt owed",
			reply: func(ctx context.Context, model string, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
				done := provider.ReceiptPendingFrom(ctx)()
				go done()
				return saying(model, "cut"), nil
			}},
	} {
		t.Run(row.name, func(t *testing.T) {
			calls := &script{reply: row.reply}
			var mu sync.Mutex
			var unbilled []string
			_, api := open(t, modelapi.Config{
				CompleterFor: calls.completerFor,
				Unbilled:     func(model string) { mu.Lock(); unbilled = append(unbilled, model); mu.Unlock() },
			})
			body := `{"model":"moonshotai/kimi-k2.6","messages":[{"role":"user","content":"go"}]}`
			if status, payload := post(t, api, api.Token, body); status != http.StatusOK {
				t.Fatalf("status %d: %s", status, payload)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(unbilled, ",") != strings.Join(row.want, ",") {
				t.Fatalf("unbilled = %v, want %v", unbilled, row.want)
			}
		})
	}
}
