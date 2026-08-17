package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// benchClient is newCachingClient without the *testing.T a benchmark cannot
// hand it. The handler is never reached — encodeRequest performs no I/O — so
// the client is here only to carry the config the encode reads off it.
func benchClient(b *testing.B, baseURL, model string) *Client {
	b.Helper()
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: baseURL, Model: model,
		HTTPClient: &http.Client{},
	})
	if err != nil {
		b.Fatal(err)
	}
	return client
}

// dirtyTranscript is benchTranscript with ids a strict backend would reject, so
// the repair path is measured too. The clean transcript is the common case —
// aforge's own ids are already conservative — but a sanitizer that got cheaper
// on the no-op and dearer on the repair would be a bad trade, and only a
// benchmark of both says which happened.
func dirtyTranscript(turns int) []ai.Message {
	messages := benchTranscript(turns)
	for index := range messages {
		if messages[index].ToolCallID != "" {
			messages[index].ToolCallID = "call:" + messages[index].ToolCallID
		}
	}
	return messages
}

// BenchmarkSanitizeMessages is the outbound hygiene pass, paid once per
// provider call over the whole transcript. Its cost is therefore linear in the
// length of the run and its total is quadratic, which is why the no-op case is
// the one that matters: almost every call is one.
func BenchmarkSanitizeMessages(b *testing.B) {
	for _, turns := range []int{45, 81} {
		messages := benchTranscript(turns)
		b.Run(fmt.Sprintf("clean/turns=%d", turns), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sanitizeMessages(messages)
			}
		})
	}
	for _, turns := range []int{45, 81} {
		messages := dirtyTranscript(turns)
		b.Run(fmt.Sprintf("scrubbed/turns=%d", turns), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sanitizeMessages(messages)
			}
		})
	}
}

// BenchmarkEncodeRequest is the whole per-call body: hygiene, the economy
// fields, the message and tool serialization, and the final marshal. It is the
// number a run actually pays before every send.
func BenchmarkEncodeRequest(b *testing.B) {
	client := benchClient(b, "https://openrouter.ai/api/v1", "anthropic/claude-opus-5")
	tools := benchTools(12)
	for _, turns := range []int{45, 81} {
		request := &ai.Request{
			Model:    "anthropic/claude-opus-5",
			Messages: benchTranscript(turns),
			Tools:    tools,
		}
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := client.encodeRequest(request, callKnobs{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
