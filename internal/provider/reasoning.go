package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ReasoningReplayPolicy is the wire law for model working carried across a
// tool loop. The wording lives once because a weaker caller-specific rule is
// exactly how an older assistant step would quietly lose its continuation.
const ReasoningReplayPolicy = "pass every retained assistant message's reasoning back unmodified under the field it arrived on"

// MessageReasoning is the provider-only half of one assistant message. The SDK
// message deliberately has no home for these fields, so callers carry a slice
// aligned with the messages in one request rather than putting model working in
// Content, where it would become part of the answer and disturb tool pairing.
type MessageReasoning struct {
	Field   string
	Text    string
	Details json.RawMessage
	// Model is the slug this working was produced under. A thinking pass is
	// the endpoint's own — OpenRouter encrypts it, and replaying it to any
	// other model is answered with a 404 ("encrypted payloads can only be
	// replayed to the endpoint that created them"), which is what a /model
	// switch mid-conversation ran into on 2026-08-28. Empty on a sidecar
	// restored from a journal written before the field existed; that one is
	// replayed on trust and repaired on refusal (client.go's sendRepaired).
	Model string
}

// producedElsewhere reports that this working belongs to some other model and
// must not travel to the one being asked. An untagged sidecar is not foreign —
// it is unknown, and the wire is the only thing that can say.
func (r MessageReasoning) producedElsewhere(model string) bool {
	return r.Model != "" && normalizeModel(r.Model) != normalizeModel(model)
}

type messageReasoningContextKey struct{}

// WithMessageReasoning attaches the sidecar for one request. The slice is
// copied because a request may outlive the transcript lock that assembled it.
func WithMessageReasoning(ctx context.Context, reasoning []MessageReasoning) context.Context {
	if len(reasoning) == 0 {
		return ctx
	}
	kept := append([]MessageReasoning(nil), reasoning...)
	for index := range kept {
		kept[index].Details = append(json.RawMessage(nil), kept[index].Details...)
	}
	return context.WithValue(ctx, messageReasoningContextKey{}, kept)
}

// MessageReasoningFrom returns a copy of the request's aligned sidecar. It is
// exported so a Completer test double can assert the same contract the real
// encoder reads without learning the provider's private context key.
func MessageReasoningFrom(ctx context.Context) []MessageReasoning {
	reasoning, _ := ctx.Value(messageReasoningContextKey{}).([]MessageReasoning)
	return append([]MessageReasoning(nil), reasoning...)
}

func validReasoningField(field string) bool {
	switch field {
	case "reasoning", "reasoning_content", "reasoning_text":
		return true
	default:
		return false
	}
}

// encodeReasoningFields adds fields beside the SDK message without decoding
// and rebuilding it. Keeping the old bytes as the prefix is what preserves the
// byte-stable prompt cache when no reasoning is present and when it is added.
func encodeReasoningFields(raw json.RawMessage, reasoning MessageReasoning) (json.RawMessage, error) {
	if reasoning.Text == "" && len(reasoning.Details) == 0 {
		return raw, nil
	}
	if reasoning.Text != "" && !validReasoningField(reasoning.Field) {
		return nil, fmt.Errorf("unsupported reasoning replay field %q", reasoning.Field)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("encoded message is not an object")
	}
	out := make([]byte, 0, len(trimmed)+len(reasoning.Text)+len(reasoning.Details)+64)
	out = append(out, trimmed[:len(trimmed)-1]...)
	if len(out) > 1 {
		out = append(out, ',')
	}
	if reasoning.Text != "" {
		name, _ := json.Marshal(reasoning.Field)
		value, _ := json.Marshal(reasoning.Text)
		out = append(out, name...)
		out = append(out, ':')
		out = append(out, value...)
	}
	if len(reasoning.Details) > 0 {
		details := bytes.TrimSpace(reasoning.Details)
		if !json.Valid(details) || len(details) == 0 || details[0] != '[' {
			return nil, fmt.Errorf("reasoning_details is not a JSON array")
		}
		if reasoning.Text != "" {
			out = append(out, ',')
		}
		out = append(out, `"reasoning_details":`...)
		out = append(out, details...)
	}
	out = append(out, '}')
	return out, nil
}

// carriesBoundReasoning reports whether any message in the sidecar carries
// `reasoning_details` — the opaque, provider-encrypted half of a model's
// working. Plain reasoning text is readable by anybody and binds nothing.
func carriesBoundReasoning(reasoning []MessageReasoning) bool {
	for _, entry := range reasoning {
		if len(bytes.TrimSpace(entry.Details)) > 0 {
			return true
		}
	}
	return false
}

// withoutBoundReasoning is the sidecar with every opaque `reasoning_details`
// taken off and everything else kept: the reasoning text, its field and its
// model tag. It is a copy, because the caller's sidecar belongs to the
// transcript and this repair is true of one request only.
func withoutBoundReasoning(reasoning []MessageReasoning) []MessageReasoning {
	kept := make([]MessageReasoning, len(reasoning))
	for index, entry := range reasoning {
		entry.Details = nil
		kept[index] = entry
	}
	return kept
}

// refusesBoundReasoning recognises the router refusing a request because the
// encrypted working it carries belongs to a provider the request itself
// excluded. RECOGNISED BY STRUCTURE FIRST (endpoints.go's header says why a
// phrase list is always one sentence behind, and [refusesForeignReasoning]
// was: run21's "can only be processed by the provider that created it" matched
// none of its four phrases):
//
//   - the refusal is a 404 or 400 in the router's own error envelope;
//   - the request carried opaque `reasoning_details`;
//   - and EITHER the router refused on its own account (no upstream named)
//     while the request went out with a membership filter — `ignore` or
//     `only`, the two fields that can keep the creating provider out — OR the
//     router's words say the content is encrypted, which covers a provider
//     the account's own settings excluded where no field on the wire shows it.
//
// A false reading costs one free resend without the opaque items, after which
// the refusal goes back to the ladder with the request whole; a missed one
// costs the turn, which is what run21 paid.
func refusesBoundReasoning(response *http.Response, payload []byte, carried []MessageReasoning) bool {
	if response == nil || !endpointRefusalStatus(response.StatusCode) || !carriesBoundReasoning(carried) {
		return false
	}
	upstream, ok := routerErrorEnvelope(payload)
	if !ok {
		return false
	}
	if strings.Contains(strings.ToLower(string(payload)), "encrypted") {
		return true
	}
	sent := refusedWirePreferences(response)
	return upstream == "" && sent != nil && (len(sent.Ignore) > 0 || len(sent.Only) > 0)
}

func attachMessageReasoning(encoded []json.RawMessage, reasoning []MessageReasoning, model string) ([]json.RawMessage, error) {
	for index := range encoded {
		if index >= len(reasoning) {
			break
		}
		// Another model's working stays home. The message itself still travels
		// — the words are the conversation; the thinking was only ever the
		// endpoint's own continuation, and a new endpoint has none to continue.
		if reasoning[index].producedElsewhere(model) {
			continue
		}
		raw, err := encodeReasoningFields(encoded[index], reasoning[index])
		if err != nil {
			return nil, err
		}
		encoded[index] = raw
	}
	return encoded, nil
}
