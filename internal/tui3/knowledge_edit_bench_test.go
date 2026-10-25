package tui3

import (
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
	"testing"
)

func BenchmarkKnowledgeHumanMarkdownWarmFrame(b *testing.B) {
	a := benchApp(0)
	a.width, a.height = 120, 40
	v := knowledge.Open(b.TempDir(), "/benchmark")
	body := strings.Repeat("A **human-written** paragraph with `code` and a linked concept.\n\n", 2200)
	a.kb.open = true
	a.kb.vault = v
	a.adoptKnowledge(knowledgeReadingMsg{vault: v, reading: knowledge.Snapshot{Enabled: true, Pages: []knowledge.Page{{ID: "manual", Title: "Long human article", Kind: "subject", HumanEdited: true, Revision: "fixture", Markdown: body, Sections: []knowledge.Section{{Paragraphs: []knowledge.Paragraph{{Text: body}}}}}}}})
	a.placeDraw(placeMemory{}, a.width, a.height)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.placeDraw(placeMemory{}, a.width, a.height)
	}
}
