package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// BenchmarkAppendMessage is one journal line: the flatten, the marshal, and the
// append. It runs once per message on the live path, which is why the flatten's
// copy of the text is worth a number rather than an assumption.
func BenchmarkAppendMessage(b *testing.B) {
	for _, size := range []struct {
		name string
		text string
	}{
		{"reply", strings.Repeat("reasoning about the change. ", 30)},
		{"toolresult", strings.Repeat("file contents line\n", 200)},
	} {
		b.Run(size.name, func(b *testing.B) {
			directory := b.TempDir()
			journal, _, err := openSessionFile(filepath.Join(directory, "session.jsonl"), directory, "test/model", "")
			if err != nil {
				b.Fatal(err)
			}
			defer journal.Close()
			message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: size.text}}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				journal.appendMessage(message)
			}
		})
	}
}
