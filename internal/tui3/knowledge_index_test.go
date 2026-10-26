package tui3

import (
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeRefreshUpdatesLinksAcrossLargeVault(t *testing.T) {
	a := knowledgeArticleApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge.sqlite")
	raw, err := knowledgefixture.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	notes := data["notes"].([]any)
	notes[0].(map[string]any)["links"] = []any{}
	notes[1].(map[string]any)["links"] = []any{map[string]any{"id": "architecture", "kind": "mention"}}
	for i := 0; i < 1000; i++ {
		notes = append(notes, map[string]any{"id": fmt.Sprintf("filler-%d", i), "title": fmt.Sprintf("Topic %d", i), "kind": "subject"})
	}
	data["notes"] = notes
	save := func() {
		t.Helper()
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := knowledgefixture.Write(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	drive(t, a, key("ctrl+r"), key("ctrl+l"), key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Durability") {
		t.Fatal("backlink failed in large vault: " + got)
	}
	drive(t, a, key("left"))
	if got := placeFrameText(a); !strings.Contains(got, "Storage") {
		t.Fatal("back before refresh failed: " + got)
	}
	notes[1].(map[string]any)["links"] = []any{}
	notes = append(notes, map[string]any{"id": "updated", "title": "Updated source", "kind": "subject", "links": []any{map[string]any{"id": "architecture", "kind": "mention"}}, "sections": []any{map[string]any{"heading": "Fresh evidence", "paragraphs": []any{map[string]any{"text": "New source after refresh."}}}}})
	data["notes"] = notes
	save()
	drive(t, a, key("ctrl+r"), key("ctrl+l"))
	if got := placeFrameText(a); !strings.Contains(got, "Updated source") || strings.Contains(got, "1. SQLite") {
		t.Fatal("refreshed connections are stale: " + got)
	}
	drive(t, a, key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Fresh evidence") {
		t.Fatal("refresh retained stale backlink index: " + got)
	}
}

func BenchmarkKnowledgeVaultFrame(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("pages=%d", count), func(b *testing.B) {
			a := benchApp(0)
			a.width, a.height = 120, 40
			a.kb.open = true
			a.kb.vault = knowledge.Open(b.TempDir())
			pages := make([]knowledge.Page, count)
			for i := range pages {
				pages[i] = knowledge.Page{ID: fmt.Sprintf("page-%d", i), Title: fmt.Sprintf("Topic %05d", i), Kind: "subject", Folder: []string{"Topics"}, Sections: []knowledge.Section{{Heading: "Overview", Paragraphs: []knowledge.Paragraph{{Text: "A concise linked article for terminal reading."}}}}}
				if i > 0 {
					pages[i].Links = []knowledge.Link{{ID: pages[i-1].ID, Kind: "mention"}}
				}
			}
			a.adoptKnowledge(knowledgeReadingMsg{vault: a.kb.vault, reading: knowledge.Snapshot{Enabled: true, Pages: pages}})
			a.kb.selected = pages[count-1].ID
			a.placeDraw(placeMemory{}, a.width, a.height)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.placeDraw(placeMemory{}, a.width, a.height)
			}
		})
	}
}

func BenchmarkKnowledgeVaultFilter(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("pages=%d", count), func(b *testing.B) {
			a := benchApp(0)
			for i := 0; i < count; i++ {
				a.kb.reading.Pages = append(a.kb.reading.Pages, knowledge.Page{ID: fmt.Sprintf("page-%d", i), Title: fmt.Sprintf("Topic %05d", i), Kind: "subject", Folder: []string{"Topics"}, Sections: []knowledge.Section{{Paragraphs: []knowledge.Paragraph{{Text: "A concise linked article for terminal reading."}}}}})
			}
			a.kb.filter.insert("Topic")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.kb.rank()
			}
		})
	}
}
