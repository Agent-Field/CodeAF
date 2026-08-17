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
func BenchmarkEncodeMessages(b *testing.B) {
	for _, turns := range []int{8, 45, 81} {
		messages := benchTranscript(turns)
		for _, dialect := range []struct {
			name string
			d    cacheDialect
		}{{"automatic", cacheDialectAutomatic}, {"breakpoints", cacheDialectBreakpoints}} {
			b.Run(fmt.Sprintf("turns=%d/%s", turns, dialect.name), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := encodeMessages(messages, dialect.d); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkEncodeTools is the tool-schema serialization. The schemas do not
// change between calls within a run, so every allocation here is repeated work
// by construction.
func BenchmarkEncodeTools(b *testing.B) {
	for _, count := range []int{12, 30} {
		tools := benchTools(count)
		b.Run(fmt.Sprintf("tools=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := encodeTools(tools, cacheDialectBreakpoints); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
