package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// benchJournal writes a synthetic session with the package's own writer, so the
// file under the benchmark is exactly the file a real run leaves behind —
// header line included — rather than a hand-rolled approximation of one.
//
// The shape is the shape a working leaf accumulates: a person's line, the
// model's reply carrying a tool call, and the result answering it. The result
// is the long one because that is where a transcript's bytes actually live, and
// the replay's cost is in the bytes.
func benchJournal(b *testing.B, messages int) string {
	b.Helper()
	directory := b.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	journal, _, err := openSessionFile(path, directory, "test/model")
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < messages; index += 3 {
		journal.appendMessage(ai.Message{
			Role:    "user",
			Content: []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("step %d: keep going", index)}},
		})
		journal.appendMessage(ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("reasoning about the change. ", 30)}},
			ToolCalls: []ai.ToolCall{{
				ID: fmt.Sprintf("call_%d", index), Type: "function",
				Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"internal/session/sessionfile.go"}`},
			}},
		})
		journal.appendMessage(ai.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call_%d", index),
			Content:    []ai.ContentPart{{Type: "text", Text: strings.Repeat("file contents line\n", 200)}},
		})
	}
	if err := journal.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkReplaySessionFile is the resume path: everything between typing
// `aforge --resume` and the transcript being on screen. It is paid once, with a
// person waiting on it, so the number that matters is the whole scan over a
// file the length of a real session.
func BenchmarkReplaySessionFile(b *testing.B) {
	for _, messages := range []int{200, 2000} {
		path := benchJournal(b, messages)
		b.Run(fmt.Sprintf("messages=%d", messages), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				replayed, err := replaySessionFile(path)
				if err != nil {
					b.Fatal(err)
				}
				if len(replayed.messages) == 0 {
					b.Fatal("replayed nothing")
				}
			}
		})
	}
}
