package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func attachMessageReasoning(encoded []json.RawMessage, reasoning []MessageReasoning) ([]json.RawMessage, error) {
	for index := range encoded {
		if index >= len(reasoning) {
			break
		}
		raw, err := encodeReasoningFields(encoded[index], reasoning[index])
		if err != nil {
			return nil, err
		}
		encoded[index] = raw
	}
	return encoded, nil
}
