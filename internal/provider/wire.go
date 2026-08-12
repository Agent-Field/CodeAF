package provider

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// reasoningKnob is OpenRouter's unified reasoning control. Two of its fields are
// modelled — the effort level and the outright disable — because they are the
// two economically distinct requests. Everything else it accepts is
// provider-specific and would reintroduce exactly the per-model branching this
// adapter avoids.
type reasoningKnob struct {
	Effort  Effort `json:"effort,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// reasoningFor maps an effort onto the wire. Off is a disable rather than a
// level, so it takes the other field; the two are never sent together.
func reasoningFor(effort Effort) *reasoningKnob {
	switch effort {
	case EffortNone:
		return nil
	case EffortOff:
		disabled := false
		return &reasoningKnob{Enabled: &disabled}
	default:
		return &reasoningKnob{Effort: effort}
	}
}

// noteReasoningMandatory remembers that a model's endpoint refused to have its
// reasoning turned off — see quirks.go for why the catalog cannot answer this
// and why nothing here is a list of model names.
//
// The memo is process-wide rather than per-client because the fact belongs to
// the model, and one model is served by several clients here: the panel builds
// one adapter per rung and the config builds more for its own surfaces. What
// one of them learns the rest should not have to relearn.
func noteReasoningMandatory(model string) {
	if quirks.note(model, time.Now().UTC()) {
		// Off the request path: the call that discovered this is waiting to be
		// re-sent, and it should not wait on a disk write to do it.
		guard.Go("provider/quirks", quirks.save)
	}
}

// ReasoningMandatory reports that this model's endpoint has refused to have its
// reasoning turned off. It is learned rather than published — no catalog field
// says it — so it is empty until some call has been told no, and then it stays
// known across processes (see [LoadQuirks]). A surface should show exactly
// that: a fact when there is one, and nothing when there is not.
func ReasoningMandatory(model string) bool { return reasoningMandatory(model) }

func reasoningMandatory(model string) bool { return quirks.knows(model) }

// normalizeModel keys the memo on the model itself rather than on how it was
// written. The leading "~" is Aforge's own routing marker, not part of the
// slug, so "~minimax/minimax-m2.7" and "minimax/minimax-m2.7" are one model.
func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "~"))
}

// refusesDisabledReasoning reads a 400 body for the one complaint this adapter
// can repair by itself. It matches on the two halves together — the subject and
// the refusal — so an unrelated 400 that merely mentions reasoning is left to
// surface as the error it is.
func refusesDisabledReasoning(payload []byte) bool {
	text := strings.ToLower(string(payload))
	if !strings.Contains(text, "reasoning") {
		return false
	}
	for _, refusal := range []string{
		"mandatory",
		"cannot be disabled",
		"can not be disabled",
		"cannot be turned off",
		"must be enabled",
		"required for this endpoint",
	} {
		if strings.Contains(text, refusal) {
			return true
		}
	}
	return false
}

// wireRequest is the serialized body. It shadows the SDK's max_tokens so the
// adapter, not the SDK, decides which output-limit field a given endpoint gets,
// and adds the three fields the SDK's Request type has no home for.
type wireRequest struct {
	*requestAlias

	MaxTokens           *int `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`

	// PromptCacheKey is the request-body half of prompt-cache affinity. Paired
	// with the session-affinity header it asks the router to keep one run on
	// one warm instance instead of scattering a byte-stable prefix across
	// providers that each have to write the cache from cold.
	PromptCacheKey string `json:"prompt_cache_key,omitempty"`

	// Reasoning is omitted entirely unless the model is known to accept it or
	// the operator asked for it explicitly. An unsupported knob is a 400, and a
	// 400 on every call is a worse failure than a model thinking too hard.
	Reasoning *reasoningKnob `json:"reasoning,omitempty"`
}

type requestAlias ai.Request

// encodeRequest applies outbound hygiene and the economy fields, then
// serializes. It is deterministic: the same request and knobs always produce
// the same bytes, which is what keeps the transcript layer's byte-stable prefix
// byte-stable all the way to the wire.
func (c *Client) encodeRequest(request *ai.Request, knobs callKnobs) ([]byte, error) {
	scrubbed := *request
	scrubbed.Messages = sanitizeMessages(request.Messages)

	// OpenRouter only reports cache reads, cache writes, and native cost when
	// the request opts into usage accounting. Without it the single largest
	// lever on a long run's bill is invisible, so it is never optional here.
	if scrubbed.Usage == nil {
		scrubbed.Usage = &ai.RequestUsage{Include: true}
	}

	model := c.modelFor(&scrubbed)

	wire := wireRequest{
		requestAlias:   (*requestAlias)(&scrubbed),
		PromptCacheKey: knobs.cacheKey,
	}
	wire.Reasoning = reasoningFor(c.resolveEffort(model, knobs.effort))
	if scrubbed.MaxTokens != nil {
		if needsMaxCompletionTokens(model) && isVouchedRewriteEndpoint(c.config.BaseURL) {
			wire.MaxCompletionTokens = scrubbed.MaxTokens
		} else {
			wire.MaxTokens = scrubbed.MaxTokens
		}
	}
	return json.Marshal(wire)
}

// resolveEffort decides whether the knob may travel, and in what shape.
func (c *Client) resolveEffort(model string, requested effortRequest) Effort {
	effort := c.requestedEffort(model, requested)
	// A model that reasons unconditionally answers the disable with a 400, and
	// no amount of operator intent changes that. Sending nothing is what the
	// harness wanted anyway — the cheapest request the endpoint will accept —
	// so the economy degrades to the model's own default instead of failing.
	if effort == EffortOff && reasoningMandatory(model) {
		return EffortNone
	}
	return effort
}

// requestedEffort applies the catalog gate. The catalog is consulted first
// because it is the only authority that can say "this model would reject it";
// when the catalog is cold or silent, only an explicit operator request gets
// sent, so a default economy can never break a run on an unknown model.
func (c *Client) requestedEffort(model string, requested effortRequest) Effort {
	if requested.effort == EffortNone {
		return EffortNone
	}
	if c.config.SupportsParameter != nil {
		if supported, known := c.config.SupportsParameter(model, "reasoning"); known {
			if !supported {
				return EffortNone
			}
			return requested.effort
		}
	}
	if requested.explicit {
		return requested.effort
	}
	return EffortNone
}

// sanitizeMessages is the outbound hygiene pass for multi-backend routing.
// It is a pure function and a no-op on already-clean input, so a transcript
// that never needed repair keeps producing byte-identical requests.
func sanitizeMessages(messages []ai.Message) []ai.Message {
	if len(messages) == 0 {
		return messages
	}
	changed := false
	cleaned := make([]ai.Message, len(messages))
	for index, message := range messages {
		cleaned[index] = message
		if id := scrubToolCallID(message.ToolCallID); id != message.ToolCallID {
			cleaned[index].ToolCallID = id
			changed = true
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]ai.ToolCall, len(message.ToolCalls))
			callsChanged := false
			for callIndex, call := range message.ToolCalls {
				calls[callIndex] = call
				if id := scrubToolCallID(call.ID); id != call.ID {
					calls[callIndex].ID = id
					callsChanged = true
				}
			}
			if callsChanged {
				cleaned[index].ToolCalls = calls
				changed = true
			}
		}
		if content, dropped := dropEmptyTextParts(message.Role, message.Content, len(message.ToolCalls) > 0); dropped {
			cleaned[index].Content = content
			changed = true
		}
	}
	if !changed {
		return messages
	}
	return cleaned
}

// scrubToolCallID maps an id onto the conservative charset every OpenAI-
// compatible backend accepts. Disallowed bytes become a hex escape rather than
// a shared placeholder so two distinct ids can never collapse into one and
// orphan a tool result. Applied identically to the assistant's tool_calls[].id
// and to the matching tool message's tool_call_id, the pairing always survives.
func scrubToolCallID(id string) string {
	if id == "" || !needsScrub(id) {
		return id
	}
	const hexDigits = "0123456789abcdef"
	var scrubbed strings.Builder
	scrubbed.Grow(len(id))
	for index := 0; index < len(id); index++ {
		character := id[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '_', character == '-':
			scrubbed.WriteByte(character)
		default:
			scrubbed.WriteByte('_')
			scrubbed.WriteByte('x')
			scrubbed.WriteByte(hexDigits[character>>4])
			scrubbed.WriteByte(hexDigits[character&0x0f])
		}
	}
	return scrubbed.String()
}

func needsScrub(id string) bool {
	for index := 0; index < len(id); index++ {
		character := id[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '_', character == '-':
		default:
			return true
		}
	}
	return false
}

// dropEmptyTextParts removes assistant content blocks with no text. Several
// strict backends reject an empty content block outright, and an assistant turn
// that is pure tool calls legitimately produces one. The last empty part is
// kept when nothing else would remain and the message carries no tool calls, so
// a content-less assistant message never changes shape on the wire.
func dropEmptyTextParts(role string, content []ai.ContentPart, hasToolCalls bool) ([]ai.ContentPart, bool) {
	if !strings.EqualFold(strings.TrimSpace(role), "assistant") || len(content) == 0 {
		return content, false
	}
	kept := make([]ai.ContentPart, 0, len(content))
	for _, part := range content {
		if part.Type == "text" && part.Text == "" && part.ImageURL == nil && part.VideoURL == nil && part.InputAudio == nil && part.InputFile == nil {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == len(content) {
		return content, false
	}
	if len(kept) == 0 && !hasToolCalls {
		return content, false
	}
	return kept, true
}

// needsMaxCompletionTokens mirrors the SDK's rewrite rule so owning the wire
// body does not silently drop an output cap for newer OpenAI-family models.
// The legacy families that still take max_tokens are a closed set; everything
// else in the gpt-* and o-series lines takes max_completion_tokens.
func needsMaxCompletionTokens(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(normalized, "/"); index >= 0 {
		normalized = normalized[index+1:]
	}
	if normalized == "gpt-4" || strings.HasPrefix(normalized, "gpt-4-") || strings.HasPrefix(normalized, "gpt-3") {
		return false
	}
	if strings.HasPrefix(normalized, "gpt-") {
		return true
	}
	return len(normalized) >= 2 && normalized[0] == 'o' && normalized[1] >= '0' && normalized[1] <= '9'
}

var vouchedRewriteDomains = []string{"openai.com", "openai.azure.com", "openrouter.ai"}

func isVouchedRewriteEndpoint(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	for _, domain := range vouchedRewriteDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}
