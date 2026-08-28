package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// benchTranscript builds a transcript the shape a working leaf actually
// accumulates: one system prompt, then rounds of user -> assistant -> tool.
// Tool results are the bulk of the bytes in a real run, which is why they are
// the long ones here.
func benchTranscript(turns int) []ai.Message {
	out := make([]ai.Message, 0, turns*3+1)
	out = append(out, ai.Message{
		Role:    "system",
		Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("system rules. ", 120)}},
	})
	for index := 0; index < turns; index++ {
		out = append(out, ai.Message{
			Role:    "user",
			Content: []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("step %d: keep going", index)}},
		})
		out = append(out, ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("reasoning about the change. ", 30)}},
		})
		out = append(out, ai.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call_%d", index),
			Content:    []ai.ContentPart{{Type: "text", Text: strings.Repeat("file contents line\n", 80)}},
		})
	}
	return out
}

func benchTools(count int) []ai.ToolDefinition {
	out := make([]ai.ToolDefinition, 0, count)
	for index := 0; index < count; index++ {
		out = append(out, ai.ToolDefinition{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        fmt.Sprintf("tool_%d", index),
				Description: strings.Repeat("what this tool does. ", 12),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string", "description": "a path"},
						"content": map[string]any{"type": "string", "description": "the bytes"},
						"limit":   map[string]any{"type": "integer", "description": "how many"},
					},
					"required": []any{"path"},
				},
			},
		})
	}
	return out
}

// BenchmarkEncodeMessages is the per-call transcript serialization. The turn
// counts bracket what BENCHMARKS.md records for real runs (45 and 81 turns),
// and the interesting number is how the cost per call grows with them: encode
// is paid once per provider call, so a cost linear in transcript length is a
// total cost quadratic in the length of the run.
//
// It runs through the MEMO, because that is what a client pays (memo.go). The
// transcript is the same on every iteration, which is the steady state a tool
// loop is in: the head of turn N+1 is the whole of turn N. What survives the
// memo is the two breakpoint positions, which is why the breakpoints dialect
// still marshals here and the automatic one does not.
func BenchmarkEncodeMessages(b *testing.B) {
	for _, turns := range []int{8, 45, 81} {
		messages := benchTranscript(turns)
		for _, dialect := range []struct {
			name string
			d    cacheDialect
		}{{"automatic", cacheDialectAutomatic}, {"breakpoints", cacheDialectBreakpoints}} {
			b.Run(fmt.Sprintf("turns=%d/%s", turns, dialect.name), func(b *testing.B) {
				var memo encodeMemo
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := memo.encodeMessages(messages, dialect.d); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkEncodeMessagesCold is the same work with nothing remembered: the
// first call of a conversation, and the cost every call paid before the memo
// existed.
func BenchmarkEncodeMessagesCold(b *testing.B) {
	messages := benchTranscript(81)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := encodeMessages(messages, cacheDialectBreakpoints); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeTools is the tool-schema serialization. The schemas do not
// change between calls within a run — the belt is append-only — so every
// allocation the memo does not take away here is repeated work by construction.
func BenchmarkEncodeTools(b *testing.B) {
	for _, count := range []int{12, 30} {
		tools := benchTools(count)
		b.Run(fmt.Sprintf("tools=%d", count), func(b *testing.B) {
			var memo encodeMemo
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := memo.encodeTools(tools, cacheDialectBreakpoints); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
