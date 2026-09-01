package session

import (
	"bytes"
	"encoding/json"

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
	kept.Details = append(json.RawMessage(nil), kept.Details...)
	if kept.Text != "" || len(kept.Details) > 0 {
		kept.Model = b.model
	}
	return kept
}

// joinReasoningDetails joins arrays without decoding their elements. The
// providers require the detail objects back unmodified and decoding them into
// interface values would rewrite numbers and object bytes on the way out.
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
