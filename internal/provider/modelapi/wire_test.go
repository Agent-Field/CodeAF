package modelapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// applied is the SDK request a call's options build, which is exactly what the
// funnel encodes.
func applied(t *testing.T, decoded *call) ai.Request {
	t.Helper()
	var request ai.Request
	for _, option := range decoded.options {
		if err := option(&request); err != nil {
			t.Fatal(err)
		}
	}
	return request
}

// THE BODY A PROGRAM SENDS IS THE REQUEST THE FUNNEL MAKES: messages of every
// role with their tool calls and results, the tools, the program's own
// tool_choice over the SDK's default, the output ceiling in either spelling,
// the temperature, the response format, the reasoning depth, the cache key and
// the working handed back on an assistant message.
func TestTheWireCarriesEveryFieldTheFunnelHasAHomeFor(t *testing.T) {
	body := `{
		"model": "openrouter/deepseek/deepseek-v4-flash-0731",
		"messages": [
			{"role": "developer", "content": "you are careful"},
			{"role": "user", "content": [{"type": "text", "text": "fix the test"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AA=="}}]},
			{"role": "assistant", "content": null, "reasoning": "look first", "reasoning_details": [{"type": "reasoning.text", "text": "look first"}],
			 "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "bash", "arguments": "{\"cmd\":\"go test\"}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "ok"}
		],
		"tools": [{"type": "function", "function": {"name": "bash", "description": "run a command", "parameters": {"type": "object", "properties": {"cmd": {"type": "string"}}}}}],
		"tool_choice": {"type": "function", "function": {"name": "bash"}},
		"max_tokens": 900,
		"max_completion_tokens": 1200,
		"temperature": 0.2,
		"reasoning": {"effort": "high"},
		"prompt_cache_key": "thread-a",
		"response_format": {"type": "json_schema", "json_schema": {"name": "verdict", "strict": true, "schema": {"type": "object"}}},
		"provider": {"order": ["somebody"]},
		"stream": true,
		"stream_options": {"include_usage": true},
		"usage": {"include": true}
	}`
	decoded, err := decodeRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.asked != "openrouter/deepseek/deepseek-v4-flash-0731" || decoded.cacheKey != "thread-a" || decoded.thread != "thread-a" || !decoded.stream {
		t.Fatalf("call = %+v", decoded)
	}
	if len(decoded.messages) != 4 || decoded.messages[0].Role != "system" {
		t.Fatalf("messages = %+v, want four with the developer turn read as system", decoded.messages)
	}
	if parts := decoded.messages[1].Content; len(parts) != 2 || parts[1].ImageURL == nil {
		t.Fatalf("the user's picture was lost: %+v", parts)
	}
	assistant := decoded.messages[2]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "bash" || assistant.ToolCalls[0].Function.Arguments != `{"cmd":"go test"}` {
		t.Fatalf("assistant tool calls = %+v", assistant.ToolCalls)
	}
	if tool := decoded.messages[3]; tool.Role != "tool" || tool.ToolCallID != "call_1" || tool.Content[0].Text != "ok" {
		t.Fatalf("tool result = %+v", tool)
	}
	// The router's own `reasoning` is left unnamed here: the call names it
	// from its thread (threads.name).
	if len(decoded.reasoning) != 4 || decoded.reasoning[2].Field != "" || decoded.reasoning[2].Text != "look first" ||
		!strings.Contains(string(decoded.reasoning[2].Details), "reasoning.text") || decoded.reasoning[0].Text != "" {
		t.Fatalf("working sidecar = %+v, want the assistant's working aligned with its message", decoded.reasoning)
	}
	if decoded.depth != (depth{rung: effort.High}) {
		t.Fatalf("depth = %+v, want the high rung", decoded.depth)
	}
	request := applied(t, decoded)
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "bash" || request.Tools[0].Function.Parameters["type"] != "object" {
		t.Fatalf("tools = %+v", request.Tools)
	}
	choice, ok := request.ToolChoice.(map[string]any)
	if !ok || choice["type"] != "function" {
		t.Fatalf("tool_choice = %#v, want the program's own object over the SDK's auto", request.ToolChoice)
	}
	if request.MaxTokens == nil || *request.MaxTokens != 1200 {
		t.Fatalf("max tokens = %v, want max_completion_tokens' 1200", request.MaxTokens)
	}
	if request.Temperature == nil || *request.Temperature != 0.2 {
		t.Fatalf("temperature = %v", request.Temperature)
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema.Name != "verdict" ||
		!request.ResponseFormat.JSONSchema.Strict || string(request.ResponseFormat.JSONSchema.Schema) != `{"type": "object"}` {
		t.Fatalf("response_format = %+v", request.ResponseFormat)
	}
}

// EVERY SPELLING OF A DEPTH LANDS ON CODEAF'S OWN LADDER: the three shared
// words and the two rungs above them as rungs — senior-dev's `--variant xhigh`
// included — the pass switched off and the router's lowest word as the
// adapter's own words, and anything else as nothing.
func TestTheWireReadsEveryReasoningSpelling(t *testing.T) {
	for _, row := range []struct {
		body string
		want depth
	}{
		{`"reasoning_effort": "low"`, depth{rung: effort.Low}},
		{`"reasoning": {"effort": "medium"}`, depth{rung: effort.Medium}},
		{`"reasoning": {"effort": "xhigh"}`, depth{rung: effort.XHigh}},
		{`"reasoning_effort": "max"`, depth{rung: effort.Max}},
		{`"reasoning": {"effort": "minimal"}`, depth{word: provider.EffortMinimal}},
		{`"reasoning": {"enabled": false}`, depth{word: provider.EffortOff}},
		{`"reasoning_effort": "none"`, depth{word: provider.EffortOff}},
		{`"reasoning": {"enabled": true}`, depth{}},
		// A word nothing in codeaf has a place for is never sent.
		{`"reasoning_effort": "ultra"`, depth{}},
	} {
		decoded, err := decodeRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],` + row.body + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.depth != row.want {
			t.Fatalf("%s: depth %+v, want %+v", row.body, decoded.depth, row.want)
		}
	}
}

func TestTheWireRefusesWhatItCannotCarryInOneSentence(t *testing.T) {
	for _, row := range []struct{ body, says string }{
		{`not json`, "not a chat-completions request"},
		{`{"model":"m","messages":[]}`, "no messages"},
		{`{"model":"m","messages":[{"role":"wizard","content":"hi"}]}`, `"wizard"`},
		{`{"model":"m","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"retrieval"}]}`, "function tools"},
		{`{"model":"m","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema"}}`, "no schema"},
	} {
		if _, err := decodeRequest([]byte(row.body)); err == nil || !strings.Contains(err.Error(), row.says) {
			t.Fatalf("%s: err = %v, want it to say %q", row.body, err, row.says)
		}
	}
	// A response_format of text is the default and travels as nothing; no
	// tools means no tool_choice either.
	decoded, err := decodeRequest([]byte(`{"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"text"},"tool_choice":"required"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request := applied(t, decoded); request.ResponseFormat != nil || request.ToolChoice != nil || request.Tools != nil {
		t.Fatalf("request = %+v", request)
	}
}

// THE ANSWER IS THE ROUTER'S OWN SHAPE, and a stream ends the way the
// router's does: the working, the words, each tool call under its index, the
// finish, a usage chunk carrying the cost, in that order.
func TestAStreamedAnswerIsTheRoutersChunksInTheRoutersOrder(t *testing.T) {
	said := answer{
		id: "gen-1", model: "m", created: 7, text: "done",
		calls:     []ai.ToolCall{{ID: "call_1", Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"}}, {ID: "call_2", Function: ai.ToolCallFunction{Name: "edit", Arguments: `{"a":1}`}}},
		finish:    "tool_calls",
		reasoning: captured{field: "reasoning_content", text: "thinking", details: json.RawMessage(`[{"type":"reasoning.text","text":"thinking"}]`)},
		usage:     usageBlock{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.25, PromptTokensDetails: promptDetail{CachedTokens: 4}},
	}
	chunks := said.chunks()
	if len(chunks) != 6 {
		t.Fatalf("%d chunks, want working, words, two calls, finish and usage", len(chunks))
	}
	// Working that arrived as a direct endpoint's reasoning_content is handed
	// out under the router's own name, the one a program reads.
	if working := chunks[0].Choices[0].Delta; working["reasoning"] != "thinking" || working["reasoning_details"] == nil || working["reasoning_content"] != nil {
		t.Fatalf("working chunk = %+v, want the working under the router's own name", working)
	}
	if chunks[1].Choices[0].Delta["content"] != "done" {
		t.Fatalf("words chunk = %+v", chunks[1].Choices[0].Delta)
	}
	second := chunks[3].Choices[0].Delta["tool_calls"].([]wireToolCall)[0]
	if second.Index == nil || *second.Index != 1 || second.ID != "call_2" || second.Type != "function" || second.Function.Name != "edit" || second.Function.Arguments != `{"a":1}` {
		t.Fatalf("second call = %+v", second)
	}
	if finish := chunks[4].Choices[0].FinishReason; finish == nil || *finish != "tool_calls" || chunks[4].Usage != nil {
		t.Fatalf("finish chunk = %+v", chunks[4])
	}
	if last := chunks[5]; last.Usage == nil || last.Usage.Cost != 0.25 || last.Usage.PromptTokensDetails.CachedTokens != 4 || last.Choices[0].FinishReason != nil {
		t.Fatalf("usage chunk = %+v", last)
	}
	encoded, _ := json.Marshal(chunks[5])
	for _, want := range []string{`"object":"chat.completion.chunk"`, `"cost":0.25`, `"prompt_tokens_details":{"cached_tokens":4}`, `"finish_reason":null`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("usage chunk %s lacks %s", encoded, want)
		}
	}
}

func TestAWholeAnswerCarriesItsCallsAndACostOfZeroOutLoud(t *testing.T) {
	said := answer{id: "gen-2", model: "m", created: 9, calls: []ai.ToolCall{{ID: "c", Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"}}}, finish: "tool_calls"}
	encoded, err := json.Marshal(said.whole())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"object":"chat.completion"`, `"content":null`, `"tool_calls":[{"id":"c","type":"function"`, `"finish_reason":"tool_calls"`, `"cost":0`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("whole answer %s lacks %s", encoded, want)
		}
	}
	if strings.Contains(string(encoded), `"tool_calls":[{"index"`) {
		t.Fatalf("a whole answer's tool calls carry a stream's index: %s", encoded)
	}
}

// A THREAD REMEMBERS ITS PREVIOUS REQUEST AND NOTHING ELSE: what is new is the
// delta, the model's own replies are never sent words, and a request that is
// not the previous one extended is a restart.
func TestAThreadRecordsOnlyWhatItHadNotSaidBefore(t *testing.T) {
	system := ai.Message{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: "rules"}}}
	user := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "fix it"}}}
	reply := ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"}}}}
	result := ai.Message{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: "PASS"}}}
	var memory threads
	sent, restarted := memory.delta("main", []ai.Message{system, user})
	if restarted || len(sent) != 2 || sent[0].Role != "system" || sent[1].Text != "fix it" {
		t.Fatalf("first call sent %+v restarted %v", sent, restarted)
	}
	sent, restarted = memory.delta("main", []ai.Message{system, user, reply, result})
	if restarted || len(sent) != 1 || sent[0].Role != "tool" || sent[0].Tool != "bash" || sent[0].Text != "PASS" {
		t.Fatalf("second call sent %+v restarted %v, want only the tool's result, naming its tool", sent, restarted)
	}
	// The same request again — a retry — added nothing.
	if sent, restarted = memory.delta("main", []ai.Message{system, user, reply, result}); restarted || len(sent) != 0 {
		t.Fatalf("a retry sent %+v restarted %v", sent, restarted)
	}
	summary := ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "so far: tests pass"}}}
	sent, restarted = memory.delta("main", []ai.Message{system, summary})
	if !restarted || len(sent) != 2 || sent[1].Text != "so far: tests pass" {
		t.Fatalf("a rewritten history sent %+v restarted %v, want the whole of it and the restart said", sent, restarted)
	}
	// Another thread has its own memory.
	if sent, restarted = memory.delta("helper", []ai.Message{system, user}); restarted || len(sent) != 2 {
		t.Fatalf("a second thread sent %+v restarted %v, want its own first call", sent, restarted)
	}
}

// WORKING HANDED BACK UNDER THE ROUTER'S NAME GOES BACK UNDER THE FIELD IT
// ARRIVED ON: a thread whose working came in as reasoning_content has it
// replayed as reasoning_content; a thread that has not said is the router's;
// a field the program named outright is kept; and the threads do not share.
func TestHandedBackWorkingIsNamedByWhatItsThreadLastArrivedOn(t *testing.T) {
	var memory threads
	memory.arrived("coder", "reasoning_content")
	memory.arrived("coder", "")
	working := func() []provider.MessageReasoning {
		return []provider.MessageReasoning{{}, {Text: "mine"}, {Field: "reasoning_text", Text: "named"}, {Details: json.RawMessage(`[{}]`)}}
	}
	named := memory.name("coder", working())
	if named[1].Field != "reasoning_content" || named[2].Field != "reasoning_text" || named[0].Field != "" || named[3].Field != "" {
		t.Fatalf("named on the coder's thread = %+v", named)
	}
	if other := memory.name("helper", working()); other[1].Field != "reasoning" {
		t.Fatalf("a thread that has not said named its working %q, want the router's own", other[1].Field)
	}
}

func TestAMessageThatIsNotTextIsNamedInBrackets(t *testing.T) {
	message := ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "look"}, {Type: "image_url", ImageURL: &ai.ImageURLData{URL: "x"}}, {Type: "file"},
	}}
	if got := said(message, nil).Text; got != "look\n[image]\n[file]" {
		t.Fatalf("said %q", got)
	}
}

func TestStreamedWorkingIsJoinedAsTheWireSentIt(t *testing.T) {
	var held json.RawMessage
	held = joinArrays(held, json.RawMessage(`[{"a":1}]`))
	held = joinArrays(held, json.RawMessage(` [] `))
	held = joinArrays(held, json.RawMessage(`not an array`))
	held = joinArrays(held, json.RawMessage(`[{"b":2},{"c":3}]`))
	if string(held) != `[{"a":1},{"b":2},{"c":3}]` {
		t.Fatalf("joined %s", held)
	}
	catch := &catcher{}
	catch.observe(provider.StreamEvent{Kind: provider.StreamReasoning, Delta: "one ", ReasoningField: "reasoning"})
	catch.observe(provider.StreamEvent{Kind: provider.StreamReasoning, Delta: "shown only", FromAnswer: true})
	catch.observe(provider.StreamEvent{Kind: provider.StreamReasoning, Delta: "two", ReasoningDetails: json.RawMessage(`[{"t":1}]`)})
	if got := catch.caught(); got.field != "reasoning" || got.text != "one two" || string(got.details) != `[{"t":1}]` {
		t.Fatalf("caught %+v", got)
	}
	// A replaced answer takes its working with it.
	catch.observe(provider.StreamEvent{Kind: provider.StreamReplaced, Delta: "retrying"})
	if got := catch.caught(); got.present() {
		t.Fatalf("working survived its answer's replacement: %+v", got)
	}
}
