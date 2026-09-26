package modelapi

// The model's working on one answer, caught as the funnel streams it.
//
// The SDK's response has no field for reasoning, so the only place the words a
// thinking model wrote before its answer arrive is the funnel's stream
// observer (provider.StreamReasoning). They are gathered here and handed to the
// program on its answer, because a thinking model in a tool loop is continued
// by being handed its own working back — and a program can only hand back what
// it was given.
//
// NOTHING IS FORWARDED WHILE IT ARRIVES. The funnel can replace an answer it
// has begun (a stalled stream rescued by a second request, provider's
// StreamReplaced), and a program cannot be told to forget bytes it has already
// read; so the working is kept until the answer is final and a replacement
// empties it.

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// captured is one answer's working: the field it arrived on, its words, and the
// structured blocks a router sends beside them.
type captured struct {
	field   string
	text    string
	details json.RawMessage
}

// present reports whether there is any working to hand over.
func (c captured) present() bool { return c.text != "" || len(c.details) > 0 }

// onto writes the working onto a message or a delta under THE ROUTER'S OWN
// NAME, `reasoning`, whatever field it arrived on. A program written against
// OpenRouter reads that name and no other, and a direct endpoint's
// `reasoning_content` would be working it never saw and so could never hand
// back; the field it really arrived on is remembered for the thread instead
// ([threads.arrived]), and a hand-back is replayed under it.
func (c captured) onto(target map[string]any) {
	if c.text != "" {
		target["reasoning"] = c.text
	}
	if len(c.details) > 0 {
		target["reasoning_details"] = c.details
	}
}

// catcher is the stream observer one call installs. The funnel calls it on its
// own read loop, synchronously, so it does nothing but append under a lock.
type catcher struct {
	mu      sync.Mutex
	field   string
	text    strings.Builder
	details json.RawMessage
}

// observe is the provider.StreamObserver.
func (c *catcher) observe(event provider.StreamEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch event.Kind {
	case provider.StreamReplaced:
		// Everything gathered belonged to the answer being thrown away.
		c.field, c.details = "", nil
		c.text.Reset()
	case provider.StreamReasoning:
		// Working carved out of the answer's own text has no field to be
		// handed back under (provider's answer.go), so it is not a
		// continuation and is not kept.
		if event.FromAnswer {
			return
		}
		if c.field == "" && event.ReasoningField != "" {
			c.field = event.ReasoningField
		}
		c.text.WriteString(event.Delta)
		c.details = joinArrays(c.details, event.ReasoningDetails)
	}
}

// caught is what the observer holds now.
func (c *catcher) caught() captured {
	c.mu.Lock()
	defer c.mu.Unlock()
	return captured{field: c.field, text: c.text.String(), details: append(json.RawMessage(nil), c.details...)}
}

// joinArrays appends one streamed array of reasoning blocks to the blocks
// already held, as the wire sent them: a client written for a router's stream
// assembles them itself, exactly as it would have assembled that router's
// chunks.
func joinArrays(current, next json.RawMessage) json.RawMessage {
	next = bytes.TrimSpace(next)
	if len(next) < 2 || next[0] != '[' || next[len(next)-1] != ']' {
		return current
	}
	inner := bytes.TrimSpace(next[1 : len(next)-1])
	if len(inner) == 0 {
		return current
	}
	if len(current) == 0 {
		return append(json.RawMessage(nil), next...)
	}
	held := bytes.TrimSpace(current[1 : len(current)-1])
	joined := make(json.RawMessage, 0, len(held)+len(inner)+3)
	joined = append(joined, '[')
	joined = append(joined, held...)
	if len(held) > 0 {
		joined = append(joined, ',')
	}
	joined = append(joined, inner...)
	return append(joined, ']')
}
