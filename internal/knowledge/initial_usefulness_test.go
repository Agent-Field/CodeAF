package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeInitialExplanationRequiresIndependentUsefulness(t *testing.T) {
	for _, decision := range []string{"rejected", "omitted", "accepted"} {
		t.Run(decision, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			text := "The checkpoint worker completed twelve assignments and entered run stage four."
			if decision == "accepted" {
				text = "Checkpoints preserve the durable queue cursor so interrupted workers can resume."
			}
			withKnowledgeModel(t, func(p map[string]any) map[string]any {
				if p["messages"] != nil {
					return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": text, "scope": "Cedar", "topic": "Checkpoint evidence", "kind": "fact", "evidence": []any{map[string]any{"source": "source", "quote": text}}}}}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					notes := []any{}
					for _, raw := range catalog {
						notes = append(notes, raw.(map[string]any)["id"])
					}
					return map[string]any{"subjects": []any{map[string]any{"title": "Checkpoint recovery", "scope": "Cedar", "notes": notes}}}
				}
				if p["paragraphs"] != nil {
					check := map[string]any{"id": "P0", "supported": true}
					if decision != "omitted" {
						check["useful"] = decision == "accepted"
					}
					return map[string]any{"checks": []any{check}}
				}
				return map[string]any{"sections": []any{map[string]any{"heading": "Recovery", "paragraphs": []any{map[string]any{"text": text, "claims": p["subject"].(map[string]any)["claims"]}}}}}
			})
			if err := v.Capture(knowledge.Source{ID: "source", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
				t.Fatal(err)
			}
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			state, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			foundSubject, foundEvidence := false, false
			for _, page := range state.Pages {
				if page.Kind != "subject" {
					foundEvidence = foundEvidence || strings.Contains(knowledge.PageMarkdown(page), text)
					continue
				}
				foundSubject = true
				if decision == "accepted" {
					if !strings.Contains(knowledge.PageMarkdown(page), text) {
						t.Fatal("supported useful explanation was rejected")
					}
				} else if len(page.Sections) != 0 {
					t.Fatal("supported execution diary entered article prose without an affirmative usefulness review")
				}
				if len(page.Evidence) != 1 || page.Evidence[0].Quote != text || len(page.Links) == 0 {
					t.Fatal("usefulness filtering lost original evidence or its reading link")
				}
			}
			if !foundSubject || !foundEvidence {
				t.Fatal("concept or linked evidence page disappeared")
			}
		})
	}
}
