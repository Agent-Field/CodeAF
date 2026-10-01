package provider

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A clipped argument is cut at a character boundary at or under the byte
// limit, so the text a no-tools model reads is never a broken character.
func TestToolHistoryClipsArgumentsAtACharacterBoundary(t *testing.T) {
	long := `{"path":"` + strings.Repeat("é", 400) + `"}`
	messages := readableToolHistory([]ai.Message{{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "read", Arguments: long}}}}})
	text := messages[0].Content[len(messages[0].Content)-1].Text
	if !utf8.ValidString(text) {
		t.Fatalf("clipped history is not valid UTF-8: %q", text)
	}
	if clipped := strings.TrimSuffix(text[strings.Index(text, " with ")+len(" with "):], "…"); len(clipped) > toolHistoryArgumentLimit {
		t.Fatalf("clipped argument is %d bytes, over the %d-byte limit", len(clipped), toolHistoryArgumentLimit)
	}
}
