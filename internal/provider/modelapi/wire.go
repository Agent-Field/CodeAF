package modelapi

// The wire: an OpenAI chat-completions body in, codeaf's funnel types out, and
// the answer back in OpenRouter's own shape.
//
// OPENROUTER'S SHAPE, BECAUSE THAT IS WHAT THE PROGRAMS WERE WRITTEN AGAINST.
// senior-dev reads `usage.cost` off the last chunk of a stream and stops
// budgeting silently when it is not there, so the answer is not "an
// OpenAI-compatible reply" in the loose sense: it is the router's own body —
// the `cost`, the cached-token nesting, the usage chunk after the finish, the
// `: …` comment lines while a call is thinking — so a program moved from a
// router onto codeaf cannot tell the road changed.
//
// WHAT CODEAF DECIDES IS DROPPED, NOT PASSED. A program's `provider` routing
// object, its `models` fallback list, `route`, `transforms` and `plugins` are
// how a caller steers OpenRouter; here codeaf's own router steers, with the
// lane beliefs, pins and ceilings a person set, so those fields are read past.
// `stream_options` and `usage` are read past too, because usage and its cost
// are always sent.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// chatRequest is the body a program sends, as far as codeaf reads it.
type chatRequest struct {
	Model    string            `json:"model"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []wireTool        `json:"tools"`
	// ToolChoice is "auto", "none", "required" or an object naming one
	// function; it is kept raw and handed on as the program wrote it.
	ToolChoice          json.RawMessage `json:"tool_choice"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	Reasoning           *wireReasoning  `json:"reasoning"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	PromptCacheKey      string          `json:"prompt_cache_key"`
	ResponseFormat      json.RawMessage `json:"response_format"`
	Stream              bool            `json:"stream"`
}

// wireTool is one tool the program offers its model.
type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// wireReasoning is OpenRouter's unified reasoning object.
type wireReasoning struct {
	Effort  string `json:"effort"`
	Enabled *bool  `json:"enabled"`
}

// messageExtras are the fields of one message the SDK's type has no home for:
// the model's working a program hands back on an assistant message so a
// thinking model can continue its own tool loop (provider.MessageReasoning).
type messageExtras struct {
	Reasoning        string          `json:"reasoning"`
	ReasoningContent string          `json:"reasoning_content"`
	ReasoningText    string          `json:"reasoning_text"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
}

// call is one decoded request: what the funnel is handed and what the log is
// written from.
type call struct {
	asked     string
	thread    string
	cacheKey  string
	stream    bool
	messages  []ai.Message
	reasoning []provider.MessageReasoning
	options   []ai.Option
	depth     depth
}

// depth is how hard the program asked its model to think, in codeaf's own
// words: a rung of the ladder (internal/effort) from low to max, or — for the
// two requests that are not rungs, the pass switched off and the lowest word
// the router has — the adapter's own word. At most one of the two is set.
type depth struct {
	rung effort.Rung
	word provider.Effort
}

// maxRequestBytes bounds one request body. A transcript with pictures in it is
// megabytes, never this; the bound is the provider's own answer ceiling, so a
// question can be as large as an answer may be and no larger.
const maxRequestBytes = 64 << 20

// decodeRequest reads one body into a call, or says in one sentence what is
// wrong with it — which is the whole of a 400's message.
func decodeRequest(body []byte) (*call, error) {
	var request chatRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("the body is not a chat-completions request: %v", err)
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("the request carries no messages")
	}
	decoded := &call{
		asked:    strings.TrimSpace(request.Model),
		cacheKey: strings.TrimSpace(request.PromptCacheKey),
		stream:   request.Stream,
	}
	decoded.thread = decoded.cacheKey
	hasReasoning := false
	for index, raw := range request.Messages {
		var message ai.Message
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("message %d does not parse: %v", index, err)
		}
		message.Role = strings.ToLower(strings.TrimSpace(message.Role))
		switch message.Role {
		case "system", "user", "assistant", "tool":
		case "developer":
			// OpenAI's newer name for the system turn. Not every model a
			// person's service carries knows it, and every one knows system.
			message.Role = "system"
		default:
			return nil, fmt.Errorf("message %d has the role %q; a message is system, user, assistant or tool", index, message.Role)
		}
		decoded.messages = append(decoded.messages, message)
		working := provider.MessageReasoning{}
		if message.Role == "assistant" {
			var extras messageExtras
			_ = json.Unmarshal(raw, &extras)
			working = extras.working()
			if working.Text != "" || len(working.Details) > 0 {
				hasReasoning = true
			}
		}
		decoded.reasoning = append(decoded.reasoning, working)
	}
	if !hasReasoning {
		decoded.reasoning = nil
	}
	options, err := request.options()
	if err != nil {
		return nil, err
	}
	decoded.options = options
	decoded.depth = request.depth()
	return decoded, nil
}

// working is the reasoning a program handed back, under the field it arrived
// on — the provider's replay law is that working travels back unmodified under
// the name it came in with (provider.ReasoningReplayPolicy).
func (e messageExtras) working() provider.MessageReasoning {
	working := provider.MessageReasoning{}
	switch {
	case e.Reasoning != "":
		working.Field, working.Text = "reasoning", e.Reasoning
	case e.ReasoningContent != "":
		working.Field, working.Text = "reasoning_content", e.ReasoningContent
	case e.ReasoningText != "":
		working.Field, working.Text = "reasoning_text", e.ReasoningText
	}
	if details := strings.TrimSpace(string(e.ReasoningDetails)); strings.HasPrefix(details, "[") && details != "[]" {
		working.Details = append(json.RawMessage(nil), e.ReasoningDetails...)
	}
	return working
}

// options are the funnel's per-call settings for everything the SDK request
// has a field for: the tools and the choice among them, the output ceiling,
// the temperature and the response format. The model is set by the caller,
// once it is decided ([Resolve]).
func (r chatRequest) options() ([]ai.Option, error) {
	var options []ai.Option
	if len(r.Tools) > 0 {
		tools := make([]ai.ToolDefinition, 0, len(r.Tools))
		for index, tool := range r.Tools {
			kind := strings.TrimSpace(tool.Type)
			if kind == "" {
				kind = "function"
			}
			if kind != "function" {
				return nil, fmt.Errorf("tool %d is a %q tool; the model API carries function tools", index, kind)
			}
			if strings.TrimSpace(tool.Function.Name) == "" {
				return nil, fmt.Errorf("tool %d has no name", index)
			}
			parameters := tool.Function.Parameters
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
				Name: tool.Function.Name, Description: tool.Function.Description, Parameters: parameters,
			}})
		}
		options = append(options, ai.WithTools(tools))
		// The SDK's WithTools says "auto"; a choice the program made is
		// applied after it, so the program's word is the one that travels.
		if choice, ok := decodeToolChoice(r.ToolChoice); ok {
			options = append(options, withToolChoice(choice))
		}
	}
	if ceiling := firstCeiling(r.MaxCompletionTokens, r.MaxTokens); ceiling > 0 {
		options = append(options, ai.WithMaxTokens(ceiling))
	}
	if r.Temperature != nil {
		options = append(options, ai.WithTemperature(*r.Temperature))
	}
	format, err := decodeResponseFormat(r.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if format != nil {
		options = append(options, withResponseFormat(format))
	}
	return options, nil
}

// firstCeiling is the output ceiling a request named: max_completion_tokens,
// OpenAI's newer spelling, when it is there, and max_tokens otherwise. The
// provider decides which of the two a given endpoint is sent.
func firstCeiling(ceilings ...*int) int {
	for _, ceiling := range ceilings {
		if ceiling != nil && *ceiling > 0 {
			return *ceiling
		}
	}
	return 0
}

// decodeToolChoice reads tool_choice as the program wrote it: one of the
// three words, or an object naming a function.
func decodeToolChoice(raw json.RawMessage) (any, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, false
	}
	var word string
	if json.Unmarshal(raw, &word) == nil {
		word = strings.TrimSpace(word)
		return word, word != ""
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil && len(object) > 0 {
		return object, true
	}
	return nil, false
}

// decodeResponseFormat reads response_format. `text` is the default and is
// sent as nothing; json_object and json_schema travel in the SDK's own shape.
func decodeResponseFormat(raw json.RawMessage) (*ai.ResponseFormat, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, nil
	}
	var format ai.ResponseFormat
	if err := json.Unmarshal(raw, &format); err != nil {
		return nil, fmt.Errorf("response_format does not parse: %v", err)
	}
	switch strings.TrimSpace(format.Type) {
	case "", "text":
		return nil, nil
	case "json_object":
		return &ai.ResponseFormat{Type: "json_object"}, nil
	case "json_schema":
		if format.JSONSchema == nil || len(format.JSONSchema.Schema) == 0 {
			return nil, errors.New("response_format json_schema carries no schema")
		}
		return &format, nil
	default:
		return nil, fmt.Errorf("response_format %q is not one the model API carries", format.Type)
	}
}

// withToolChoice sets the program's own tool_choice on the SDK request.
func withToolChoice(choice any) ai.Option {
	return func(request *ai.Request) error {
		request.ToolChoice = choice
		return nil
	}
}

// withResponseFormat sets a decoded response_format on the SDK request.
func withResponseFormat(format *ai.ResponseFormat) ai.Option {
	return func(request *ai.Request) error {
		request.ResponseFormat = format
		return nil
	}
}

// depth is the reasoning depth the program asked for — OpenRouter's
// `reasoning` object or OpenAI's `reasoning_effort` — on codeaf's own ladder:
// low, medium and high are the words every provider shares, and xhigh and max
// are the two rungs above them, which codeaf says with a thinking budget
// (internal/provider's effortladder.go). `enabled: false` and `none` switch the
// pass off, and `minimal` is the router's own lowest word. A word none of that
// has a place for is not sent, because a knob a model would refuse must never
// reach the wire.
func (r chatRequest) depth() depth {
	word := strings.TrimSpace(r.ReasoningEffort)
	if r.Reasoning != nil {
		if r.Reasoning.Enabled != nil && !*r.Reasoning.Enabled {
			return depth{word: provider.EffortOff}
		}
		if said := strings.TrimSpace(r.Reasoning.Effort); said != "" {
			word = said
		}
	}
	switch word = strings.ToLower(word); word {
	case "none", "off":
		return depth{word: provider.EffortOff}
	case "minimal":
		return depth{word: provider.EffortMinimal}
	}
	if rung := effort.Rung(word); rung.Valid() {
		return depth{rung: rung}
	}
	return depth{}
}

// ── the answer ──────────────────────────────────────────────────────────────

// completion is one whole answer, the body a request that did not ask for a
// stream is given.
type completion struct {
	ID       string        `json:"id"`
	Provider string        `json:"provider,omitempty"`
	Model    string        `json:"model"`
	Object   string        `json:"object"`
	Created  int64         `json:"created"`
	Choices  []wholeChoice `json:"choices"`
	Usage    usageBlock    `json:"usage"`
}

type wholeChoice struct {
	Index              int            `json:"index"`
	Message            map[string]any `json:"message"`
	FinishReason       string         `json:"finish_reason"`
	NativeFinishReason string         `json:"native_finish_reason"`
	Logprobs           any            `json:"logprobs"`
}

// chunk is one event of a streamed answer.
type chunk struct {
	ID       string        `json:"id"`
	Provider string        `json:"provider,omitempty"`
	Model    string        `json:"model"`
	Object   string        `json:"object"`
	Created  int64         `json:"created"`
	Choices  []chunkChoice `json:"choices"`
	Usage    *usageBlock   `json:"usage,omitempty"`
	Error    *errorDetail  `json:"error,omitempty"`
}

type chunkChoice struct {
	Index              int            `json:"index"`
	Delta              map[string]any `json:"delta"`
	FinishReason       *string        `json:"finish_reason"`
	NativeFinishReason *string        `json:"native_finish_reason"`
	Logprobs           any            `json:"logprobs"`
}

// usageBlock is OpenRouter's usage object. COST IS ALWAYS PRESENT, zero
// included: a program that budgets reads it off every answer, and a missing
// field is a budget that silently stops counting.
type usageBlock struct {
	PromptTokens        int          `json:"prompt_tokens"`
	CompletionTokens    int          `json:"completion_tokens"`
	TotalTokens         int          `json:"total_tokens"`
	Cost                float64      `json:"cost"`
	PromptTokensDetails promptDetail `json:"prompt_tokens_details"`
}

type promptDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

// errorBody is OpenRouter's error envelope: a sentence and a numeric code.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// wireToolCall is one tool call on the answer, with the index a stream's
// delta carries so a client can assemble calls by position.
type wireToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// answer is everything one call came back with, in the shape both the whole
// body and the stream are written from.
type answer struct {
	id        string
	provider  string
	model     string
	created   int64
	text      string
	calls     []ai.ToolCall
	finish    string
	reasoning captured
	usage     usageBlock
}

// finishOf is the answer's own word for how it ended, "tool_calls" for an
// answer that is a tool call and said nothing, "stop" when nothing was said.
func finishOf(response *ai.Response) string {
	finish := strings.TrimSpace(provider.FinishReason(response))
	if finish != "" {
		return finish
	}
	if response != nil && response.HasToolCalls() {
		return "tool_calls"
	}
	return "stop"
}

// toolCalls is the answer's tool calls in the wire's shape, indexed when the
// shape is a stream's.
func toolCalls(calls []ai.ToolCall, indexed bool) []wireToolCall {
	out := make([]wireToolCall, 0, len(calls))
	for position, call := range calls {
		wired := wireToolCall{ID: call.ID, Type: "function"}
		if strings.TrimSpace(call.Type) != "" {
			wired.Type = call.Type
		}
		wired.Function.Name = call.Function.Name
		wired.Function.Arguments = call.Function.Arguments
		if indexed {
			at := position
			wired.Index = &at
		}
		out = append(out, wired)
	}
	return out
}

// message is the whole answer's assistant message. THE WORKING TRAVELS UNDER
// THE FIELD IT ARRIVED ON, so a program that hands it back on its next call is
// handing back exactly what the endpoint wrote (provider.ReasoningReplayPolicy):
// OpenRouter's `reasoning`, or a direct endpoint's `reasoning_content`.
func (a answer) message() map[string]any {
	message := map[string]any{"role": "assistant", "refusal": nil}
	if a.text != "" || len(a.calls) == 0 {
		message["content"] = a.text
	} else {
		message["content"] = nil
	}
	if len(a.calls) > 0 {
		message["tool_calls"] = toolCalls(a.calls, false)
	}
	a.reasoning.onto(message)
	return message
}

// whole is the answer as one completion body.
func (a answer) whole() completion {
	return completion{
		ID: a.id, Provider: a.provider, Model: a.model, Object: "chat.completion", Created: a.created,
		Choices: []wholeChoice{{Index: 0, Message: a.message(), FinishReason: a.finish, NativeFinishReason: a.finish}},
		Usage:   a.usage,
	}
}

// chunks is the answer as the events of a stream, in the order OpenRouter
// sends them: the working, the words, each tool call whole under its index, the
// finish, and then the usage on a chunk of its own — the last thing before
// `[DONE]`, which is where a program that budgets reads its cost.
func (a answer) chunks() []chunk {
	head := func(delta map[string]any, finish *string) chunk {
		return chunk{
			ID: a.id, Provider: a.provider, Model: a.model, Object: "chat.completion.chunk", Created: a.created,
			Choices: []chunkChoice{{Index: 0, Delta: delta, FinishReason: finish, NativeFinishReason: finish}},
		}
	}
	var out []chunk
	if a.reasoning.present() {
		delta := map[string]any{"role": "assistant", "content": ""}
		a.reasoning.onto(delta)
		out = append(out, head(delta, nil))
	}
	if a.text != "" || len(a.calls) == 0 {
		out = append(out, head(map[string]any{"role": "assistant", "content": a.text}, nil))
	}
	for _, call := range toolCalls(a.calls, true) {
		out = append(out, head(map[string]any{"role": "assistant", "content": nil, "tool_calls": []wireToolCall{call}}, nil))
	}
	finish := a.finish
	out = append(out, head(map[string]any{"role": "assistant", "content": ""}, &finish))
	usage := a.usage
	last := head(map[string]any{"role": "assistant", "content": ""}, nil)
	last.Usage = &usage
	return append(out, last)
}

// failedChunk is a failure after the stream has begun: OpenRouter's in-band
// error event, an `error` beside a choice that finished on "error".
func failedChunk(id, model string, created int64, status int, message string) chunk {
	finish := "error"
	return chunk{
		ID: id, Model: model, Object: "chat.completion.chunk", Created: created,
		Error:   &errorDetail{Message: message, Code: status},
		Choices: []chunkChoice{{Index: 0, Delta: map[string]any{"content": ""}, FinishReason: &finish, NativeFinishReason: &finish}},
	}
}
