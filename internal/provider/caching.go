package provider

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Prefix-cache discipline: what each provider class lets this adapter say.
//
// A tool loop re-sends its whole transcript every turn. That is not a defect to
// be engineered away — the model has to see what it already did — but it means
// the same bytes are billed once per turn, and the ledgers say what that costs:
// 5.76x duplication, 61% of normalized dollars spent re-sending text the
// provider had already read. A prefix cache is the only thing that makes the
// re-send cheap, and there are exactly two ways to reach one:
//
//   - AUTOMATIC caching. DeepSeek, OpenAI and most OpenRouter-fronted endpoints
//     keep a server-side cache keyed on the exact leading bytes of a request.
//     Nothing in the body turns it on and nothing turns it off; the only thing a
//     client controls is whether the bytes are stable (see the freeze rules in
//     internal/exec) and whether consecutive requests reach the SAME replica.
//     That second half is what the routing key below is for: a warm prefix on
//     replica A is a cold miss on replica B, and a leaf whose six turns were
//     round-robined across six replicas pays cold six times while reporting a
//     perfectly stable prompt. This is the read of the ledger's 34% line — cache
//     reads quantized in 256s, six concurrent leaves, six competing prefixes.
//
//   - EXPLICIT breakpoints. Anthropic-family models — natively, and through
//     OpenRouter, which passes the field through rather than normalizing it away
//     — take a `cache_control: {"type":"ephemeral"}` marker that says "write a
//     cache entry covering everything up to here". Up to four may be placed.
//
// The three positions used here are pi's, and each is a different economy:
//
//	(i)   the system block             — every leaf of a run shares it byte for
//	                                     byte (see internal/exec/prefix_test.go),
//	                                     so one write serves the whole fan-out.
//	(ii)  the LAST tool definition     — a marker on the last element caches the
//	                                     whole schema array before it. Anthropic
//	                                     orders the prefix tools -> system ->
//	                                     messages, so (i) already covers the
//	                                     schemas; (ii) is what keeps them cached
//	                                     when a router reorders those regions or
//	                                     when the system block is absent.
//	(iii) the last content block of    — this one ROLLS FORWARD. Every turn
//	      the final user/tool message     writes an entry over the whole
//	                                      transcript-so-far and the next turn
//	                                      reads it. Without it only the head is
//	                                      cached and the growing tail — which is
//	                                      the part that actually got expensive —
//	                                      is re-billed cold on every turn.
//
// What is NOT expressible, so that a future reader does not go looking:
//
//   - OpenAI-family models (gpt-*, o*) have no cache_control field at all. Their
//     cache is automatic and their only client-side control is prompt_cache_key,
//     which this adapter always sends (see wireRequest).
//   - DeepSeek's cache is automatic, keyed on exact prefix bytes, with no knob
//     whatsoever. Byte stability plus routing affinity is the entire lever.
//   - Google/Gemini has explicit caching, but through a separate cachedContent
//     handle rather than an inline marker, and the OpenAI-compatible body this
//     adapter speaks has no field for it.
//
// Sending a marker to an endpoint that does not know it risks a 400, so the
// dialect is gated twice: on the model family AND on the endpoint being one that
// is known to pass the field through — and a refusal is learned and remembered
// exactly like the reasoning quirk (see quirks.go). No list of model names is
// hard-coded; the family test is on the vendor prefix, which is part of the slug
// the operator typed.

// cacheDialect names what a given endpoint-and-model pair lets this adapter say
// about caching. It is a property of the pair rather than of either half: the
// same Claude slug takes breakpoints through OpenRouter and drops them through a
// gateway that normalizes bodies.
type cacheDialect int

const (
	// cacheDialectAutomatic is the floor and the common case: the provider
	// caches prefixes on its own, and the only levers are byte stability and
	// routing affinity. Both are already applied to every request.
	cacheDialectAutomatic cacheDialect = iota

	// cacheDialectBreakpoints is automatic caching plus the three markers.
	cacheDialectBreakpoints
)

// ephemeralBreakpoint is the marker itself. It is a raw message rather than a
// struct because it is a constant: one allocation-free value shared by every
// position of every request, and no field of it is ever computed.
var ephemeralBreakpoint = json.RawMessage(`{"type":"ephemeral"}`)

// cacheControlDomains are the endpoints known to carry a cache_control field to
// an Anthropic-family model rather than stripping or rejecting it.
var cacheControlDomains = []string{"openrouter.ai", "anthropic.com"}

// dialectFor decides what this request may say about caching.
func (c *Client) dialectFor(model string) cacheDialect {
	if !honoursCacheControl(c.config.BaseURL, model) {
		return cacheDialectAutomatic
	}
	if cacheControlRefused(model) {
		return cacheDialectAutomatic
	}
	return cacheDialectBreakpoints
}

// honoursCacheControl is the static half of the gate: the model family takes
// the marker and the endpoint carries it. Both halves are required, because
// either one alone is a guess.
func honoursCacheControl(baseURL, model string) bool {
	return anthropicFamily(model) && hostIn(baseURL, cacheControlDomains)
}

// anthropicFamily reads the vendor out of the slug the operator configured. It
// matches on the vendor prefix and on the product name, because both spellings
// reach this adapter: "anthropic/claude-opus-5" from OpenRouter's catalog and a
// bare "claude-opus-5" from a direct endpoint.
func anthropicFamily(model string) bool {
	normalized := normalizeModel(model)
	return strings.HasPrefix(normalized, "anthropic/") || strings.Contains(normalized, "claude")
}

// hostIn matches a base URL against a domain suffix list, the same way the
// max_completion_tokens rewrite vouches for an endpoint.
func hostIn(baseURL string, domains []string) bool {
	host := hostOf(baseURL)
	if host == "" {
		return false
	}
	for _, domain := range domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// breakpoints names which messages carry a marker. It is separated from the
// encoding so the placement rule is assertable on its own — the positions are
// the whole discipline, and a test that had to decode a request body to check
// them would be testing the encoder instead.
//
// Both indices are -1 when the position does not exist, which is not an error:
// a request with no system message still caches its tail, and a single-message
// request marks one position rather than two.
type breakpoints struct {
	// system is the last message of the leading run of system messages. It is
	// the leading run rather than any system message anywhere, because a system
	// nudge injected mid-transcript is not the shared prefix — marking it would
	// write an entry no other leaf of the run can read.
	system int
	// tail is the last user or tool message. It is where the rolling marker
	// goes, and it is deliberately not simply the last message: a prefilled
	// assistant turn at the end is content the model is about to continue, not a
	// boundary the next turn will re-send unchanged.
	tail int
}

func breakpointsFor(messages []ai.Message) breakpoints {
	placed := breakpoints{system: -1, tail: -1}
	for index, message := range messages {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			break
		}
		placed.system = index
	}
	for index := len(messages) - 1; index >= 0; index-- {
		role := strings.ToLower(strings.TrimSpace(messages[index].Role))
		if role == "user" || role == "tool" {
			placed.tail = index
			break
		}
	}
	// One message cannot be two breakpoints. When the only message is a system
	// one the tail claim is dropped, because the head marker already covers
	// everything there is to cover.
	if placed.tail == placed.system {
		placed.tail = -1
	}
	return placed
}

// encodeMessages serializes the transcript, marking the two in-transcript
// positions when the dialect allows it.
//
// Every unmarked message goes through the SDK's own MarshalJSON, so a request
// that carries no markers is byte-identical to what this adapter sent before
// breakpoints existed. That is not politeness — it is what keeps the automatic
// caches on every other provider warm across the change.
func encodeMessages(messages []ai.Message, dialect cacheDialect) ([]json.RawMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	placed := breakpoints{system: -1, tail: -1}
	if dialect == cacheDialectBreakpoints {
		placed = breakpointsFor(messages)
	}
	encoded := make([]json.RawMessage, len(messages))
	for index, message := range messages {
		var (
			raw []byte
			err error
		)
		if index == placed.system || index == placed.tail {
			raw, err = marshalMarked(message)
		} else {
			raw, err = json.Marshal(message)
		}
		if err != nil {
			return nil, err
		}
		encoded[index] = raw
	}
	return encoded, nil
}

// markedToolMessage is a tool result with the marker beside the content rather
// than inside it. A tool message's content is a bare string on this wire, and
// the Anthropic passthrough reads a message-level cache_control for exactly this
// case — putting the field here is what lets a tool result be a breakpoint at
// all, and the tail of a working leaf is a tool result far more often than not.
type markedToolMessage struct {
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ToolCallID   string          `json:"tool_call_id"`
	CacheControl json.RawMessage `json:"cache_control"`
}

// markedMessage is a system or user message forced into array-content form.
//
// The SDK collapses single-text content to a bare string for compatibility, and
// a bare string has nowhere to hang a marker. Expanding to the array form is the
// only way to place one, and it is safe in the other direction: every endpoint
// that accepts the string form also accepts the array.
type markedMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

// markedTextPart is a text block carrying the breakpoint.
type markedTextPart struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control"`
}

// marshalMarked serializes one message with a breakpoint on it. A message with
// no place to put a marker — an image-only user turn, a tool result with no id —
// falls back to the plain encoding rather than inventing a position, so a
// breakpoint is never placed where the provider would not read one.
func marshalMarked(message ai.Message) ([]byte, error) {
	role := strings.ToLower(strings.TrimSpace(message.Role))
	if role == "tool" {
		if message.ToolCallID == "" {
			return json.Marshal(message)
		}
		content := ""
		if len(message.Content) == 1 && message.Content[0].Type == "text" {
			content = message.Content[0].Text
		}
		return json.Marshal(markedToolMessage{
			Role:         message.Role,
			Content:      content,
			ToolCallID:   message.ToolCallID,
			CacheControl: ephemeralBreakpoint,
		})
	}
	if len(message.ToolCalls) > 0 {
		return json.Marshal(message)
	}
	last := lastTextPart(message.Content)
	if last < 0 {
		return json.Marshal(message)
	}
	parts := make([]json.RawMessage, len(message.Content))
	for index, part := range message.Content {
		var (
			raw []byte
			err error
		)
		if index == last {
			raw, err = json.Marshal(markedTextPart{
				Type: "text", Text: part.Text, CacheControl: ephemeralBreakpoint,
			})
		} else {
			raw, err = json.Marshal(part)
		}
		if err != nil {
			return nil, err
		}
		parts[index] = raw
	}
	return json.Marshal(markedMessage{Role: message.Role, Content: parts})
}

// lastTextPart is where a marker goes inside a multi-part message: the last text
// block, matching the position the Anthropic passthrough reads. Returns -1 when
// the message has no text at all.
func lastTextPart(content []ai.ContentPart) int {
	for index := len(content) - 1; index >= 0; index-- {
		if content[index].Type == "text" {
			return index
		}
	}
	return -1
}

// markedTool is a tool definition carrying the breakpoint that caches the whole
// schema array before it.
type markedTool struct {
	Type         string          `json:"type"`
	Function     ai.ToolFunction `json:"function"`
	CacheControl json.RawMessage `json:"cache_control"`
}

// encodeTools serializes the schema array, marking the last element when the
// dialect allows it.
//
// The tool block is the second-largest fixed cost in a leaf's prompt after the
// system message and it never changes within a run — internal/exec pins that
// with a test that the block is append-only and never reshuffled. One marker on
// the last element turns the whole array into a single cache entry.
//
// Two honest caveats, because this position is the least evidenced of the three:
//
//   - The message positions are ported from a converter whose wire shape is
//     known good. This one is Anthropic's documented native position, carried on
//     the assumption that the passthrough copies unknown tool fields. If it does
//     not, the field is dropped and nothing is lost: Anthropic orders the cached
//     prefix tools -> system -> messages, so the system marker already covers the
//     schemas. If instead it is rejected, the learned refusal downgrades the
//     model on one call. Neither failure is silent and neither is expensive.
//   - Arming a tool mid-run appends to the array and therefore MOVES this
//     marker. That invalidates the tool block's own entry — but appending a tool
//     changes the prefix bytes anyway, so the marker is not what cost anything.
func encodeTools(tools []ai.ToolDefinition, dialect cacheDialect) ([]json.RawMessage, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	encoded := make([]json.RawMessage, len(tools))
	for index, tool := range tools {
		var (
			raw []byte
			err error
		)
		if dialect == cacheDialectBreakpoints && index == len(tools)-1 {
			raw, err = json.Marshal(markedTool{
				Type: tool.Type, Function: tool.Function, CacheControl: ephemeralBreakpoint,
			})
		} else {
			raw, err = json.Marshal(tool)
		}
		if err != nil {
			return nil, err
		}
		encoded[index] = raw
	}
	return encoded, nil
}

// noteCacheControlRefused remembers that this model's endpoint rejected an
// ephemeral breakpoint, so no later call on it pays for the discovery again.
// It mirrors noteReasoningMandatory exactly, including the off-path save: the
// call that learned this is waiting to be re-sent and must not wait on a disk.
func noteCacheControlRefused(model string) {
	if quirks.noteNoCacheControl(model, time.Now().UTC()) {
		quirks.persist()
	}
}

func cacheControlRefused(model string) bool { return quirks.knowsNoCacheControl(model) }

// CacheControlRejected reports that this model's endpoint has refused a cache
// breakpoint. Like ReasoningMandatory it is learned rather than published, so it
// is empty until some call has been told no — which is exactly what a surface
// should show: a fact when there is one, and nothing when there is not.
func CacheControlRejected(model string) bool { return cacheControlRefused(model) }

// refusesCacheControl reads a 400 body for the one complaint the breakpoints can
// cause. Like the reasoning refusal it matches on both halves together — the
// subject and the rejection — so an unrelated 400 that merely echoes a request
// body containing the field is not mistaken for a refusal of it.
func refusesCacheControl(payload []byte) bool {
	text := strings.ToLower(string(payload))
	if !strings.Contains(text, "cache_control") && !strings.Contains(text, "cache control") {
		return false
	}
	for _, rejection := range []string{
		"unrecognized",
		"unexpected",
		"unknown",
		"not supported",
		"unsupported",
		"not permitted",
		"extra inputs",
		"additional properties",
		"invalid",
	} {
		if strings.Contains(text, rejection) {
			return true
		}
	}
	return false
}
