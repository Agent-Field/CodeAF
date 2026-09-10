package session

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// reasoningBuffer is one streamed assistant step's continuation metadata. It
// is separate from partialBuffer because an interrupted answer may preserve
// visible text, while an attempt that did not complete must never preserve a
// half-reasoned continuation.
type reasoningBuffer struct {
	reasoning provider.MessageReasoning
	// model is the slug the attempt in progress was sent to. It is stamped
	// per attempt (loop.go) because a step can hop models mid-loop, and it
	// rides out on the snapshot so a later /model switch knows whose working
	// this was and leaves it home.
	model string
}

func (a *Agent) alignReasoningLocked() {
	switch {
	case len(a.messageReasoning) < len(a.messages):
		a.messageReasoning = append(a.messageReasoning,
			make([]provider.MessageReasoning, len(a.messages)-len(a.messageReasoning))...)
	case len(a.messageReasoning) > len(a.messages):
		a.messageReasoning = a.messageReasoning[:len(a.messages)]
	}
}

func (b *reasoningBuffer) reset() { b.reasoning = provider.MessageReasoning{} }

// begin resets the buffer for one attempt and names the model it is going to.
func (b *reasoningBuffer) begin(model string) {
	b.reset()
	b.model = model
}

func (b *reasoningBuffer) write(event provider.StreamEvent) {
	if b == nil || event.Kind != provider.StreamReasoning {
		return
	}
	// WORKING THAT CAME OUT OF THE ANSWER CHANNEL IS NOT A CONTINUATION. It was
	// fenced inside `content` and the endpoint never gave it a reasoning field
	// (internal/provider's answer.go), so there is nothing to replay it under —
	// and the encoder refuses an unnamed field outright rather than guess
	// ([provider.MessageReasoning]). It is shown and never sent back.
	if event.FromAnswer {
		return
	}
	if b.reasoning.Field == "" && event.ReasoningField != "" {
		b.reasoning.Field = event.ReasoningField
	}
	b.reasoning.Text += event.Delta
	b.reasoning.Details = joinReasoningDetails(b.reasoning.Details, event.ReasoningDetails)
}

func (b *reasoningBuffer) snapshot() provider.MessageReasoning {
	if b == nil {
		return provider.MessageReasoning{}
	}
	kept := b.reasoning
	kept.Details = assembledReasoningDetails(kept.Details)
	if kept.Text != "" || len(kept.Details) > 0 {
		kept.Model = b.model
	}
	return kept
}

// joinReasoningDetails captures streamed fragments without interpreting them.
// Assembly happens once at snapshot, rather than reparsing growing reasoning
// text on every token. Raw elements preserve unknown fields and opaque blocks.
func joinReasoningDetails(current, next json.RawMessage) json.RawMessage {
	next = bytes.TrimSpace(next)
	if len(next) < 2 || next[0] != '[' || next[len(next)-1] != ']' {
		return current
	}
	if len(current) == 0 {
		return append(json.RawMessage(nil), next...)
	}
	current = bytes.TrimSpace(current)
	if len(current) < 2 || current[0] != '[' || current[len(current)-1] != ']' {
		return append(json.RawMessage(nil), next...)
	}
	left := bytes.TrimSpace(current[1 : len(current)-1])
	right := bytes.TrimSpace(next[1 : len(next)-1])
	joined := make([]byte, 0, len(left)+len(right)+3)
	joined = append(joined, '[')
	joined = append(joined, left...)
	if len(left) > 0 && len(right) > 0 {
		joined = append(joined, ',')
	}
	joined = append(joined, right...)
	joined = append(joined, ']')
	return joined
}

// assembledReasoningDetails joins streamed text/summary fragments once, at the
// completed assistant boundary. CRITICAL: transport chunks are not separate
// reasoning blocks; replay must preserve the completed block across tool calls.
// OpenRouter's client does the same before replay:
// https://github.com/OpenRouterTeam/ai-sdk-provider/blob/main/src/chat/index.ts
// Explicit identity changes remain boundaries. Opaque blocks stay byte-identical.
func assembledReasoningDetails(raw json.RawMessage) json.RawMessage {
	var fragments []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fragments) != nil {
		return append(json.RawMessage(nil), raw...)
	}
	out := []byte{'['}
	var pending json.RawMessage
	var metadata map[string]json.RawMessage
	var field string
	var text strings.Builder
	merged := false
	flush := func() {
		if pending == nil {
			return
		}
		if merged {
			metadata[field], _ = json.Marshal(text.String())
			pending, _ = json.Marshal(metadata)
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(out, pending...)
	}
	for _, fragment := range fragments {
		var next map[string]json.RawMessage
		_ = json.Unmarshal(fragment, &next)
		var kind string
		_ = json.Unmarshal(next["type"], &kind)
		nextField := ""
		switch kind {
		case "reasoning.text":
			nextField = "text"
		case "reasoning.summary":
			nextField = "summary"
		}
		var delta string
		if value, present := next[nextField]; present && json.Unmarshal(value, &delta) != nil {
			nextField = ""
		}
		compatible := field != "" && field == nextField
		for key, value := range next {
			if key == "signature" && (string(metadata[key]) == "null" || string(value) == "null") {
				continue
			}
			if old, present := metadata[key]; key != field && present && !bytes.Equal(bytes.TrimSpace(old), bytes.TrimSpace(value)) {
				compatible = false
			}
		}
		if compatible {
			for key, value := range next {
				if key != "signature" || string(value) != "null" || len(metadata[key]) == 0 {
					metadata[key] = value
				}
			}
			text.WriteString(delta)
			merged = true
			continue
		}
		flush()
		pending, metadata, field, merged = fragment, next, nextField, false
		text.Reset()
		text.WriteString(delta)
	}
	flush()
	return append(out, ']')
}

func (a *Agent) recordAssistant(message ai.Message, reasoning provider.MessageReasoning) {
	a.mu.Lock()
	a.alignReasoningLocked()
	a.messages = append(a.messages, message)
	a.messageReasoning = append(a.messageReasoning, reasoning)
	if a.file != nil {
		a.file.appendReasonedMessage(message, reasoning)
	}
	a.chatlog.post(message)
	a.mu.Unlock()
}

// snapshotWithReasoning takes both aligned slices under one lock. THE SIDECAR
// ALWAYS HAS THE TRANSCRIPT'S LENGTH, so no concurrent append can put model
// working beside the wrong assistant message.
func (a *Agent) snapshotWithReasoning() ([]ai.Message, []provider.MessageReasoning) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.alignReasoningLocked()
	messages := append([]ai.Message(nil), a.messages...)
	hasReasoning := false
	for _, carried := range a.messageReasoning {
		if carried.Text != "" || len(carried.Details) > 0 {
			hasReasoning = true
			break
		}
	}
	if !hasReasoning {
		return messages, nil
	}
	reasoning := append([]provider.MessageReasoning(nil), a.messageReasoning...)
	return messages, reasoning
}
