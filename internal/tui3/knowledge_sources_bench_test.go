package tui3

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
	"testing"
)

func BenchmarkKnowledgeSourceFrame(b *testing.B) {
	for _, count := range []int{10, 100} {
		b.Run(fmt.Sprintf("sources=%d", count), func(b *testing.B) {
			a := benchApp(0)
			a.width, a.height = 120, 40
			v := knowledge.Open(b.TempDir())
			evidence := make([]knowledge.Evidence, count)
			for i := range evidence {
				evidence[i] = knowledge.Evidence{Source: fmt.Sprintf("s%d", i), Role: "assistant", TS: "2026-10-25T12:00:00Z", Quote: "## Storage\n\n**Durability** matters.\n\n- Keep data\n- Verify recovery\n\n" + strings.Repeat("A source paragraph with `code` and **emphasis**. ", 10)}
			}
			a.kb.open = true
			a.kb.vault = v
			a.adoptKnowledge(knowledgeReadingMsg{vault: v, reading: knowledge.Snapshot{Enabled: true, Pages: []knowledge.Page{{ID: "storage", Title: "Storage", Kind: "subject", Evidence: evidence}}}})
			a.kb.sources = true
			a.kb.article = true
			a.placeDraw(placeMemory{}, a.width, a.height)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.placeDraw(placeMemory{}, a.width, a.height)
			}
		})
	}
}
