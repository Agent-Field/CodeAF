//go:build !windows

package msgmodel

// UserText builds a plain-text user message in the one content shape the wire
// converter accepts: a []any list holding a single TextContent, which is what
// ConvertToModelMessages emits for the coder's own prompt.
//
// The shape is easy to get wrong: a typed []TextContent slice fails the
// converter's `msg.Content.([]any)` assertion. Every hand-built user message
// must come from here, and the converter rejects any other shape instead of
// sending an empty turn.
func UserText(text string) ModelMessage {
	return ModelMessage{
		Role:    "user",
		Content: []any{TextContent{Type: "text", Text: text}},
	}
}
