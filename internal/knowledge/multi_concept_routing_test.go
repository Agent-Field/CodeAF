package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeNewEvidenceUpdatesConceptsAcrossCatalogWindows(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	updated := []string{}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if proposal, ok := p["proposed_subjects"]; ok {
			return map[string]any{"subjects": proposal}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			existing, _ := p["existing_subjects"].([]any)
			if len(existing) > 128 {
				t.Errorf("existing concept catalog exceeds 128 entries: %d", len(existing))
			}
			if catalog[0].(map[string]any)["title"] == "shared-evidence" && len(existing) > 0 {
				subject := existing[0].(map[string]any)
				updated = append(updated, subject["id"].(string))
				assignment := map[string]any{"existing_subject_id": subject["id"], "title": subject["title"], "scope": subject["scope"], "notes": []any{catalog[0].(map[string]any)["id"]}}
				// Repeated assignments must not duplicate citations or pages.
				return map[string]any{"subjects": []any{assignment, assignment}}
			}
			subjects := []any{}
			for _, raw := range catalog {
				note := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": "Concept " + note["title"].(string), "scope": "Cedar", "notes": []any{note["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["remaining_steps"] != nil {
			return map[string]any{"action": "write", "sections": []any{}, "gaps": []any{}}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	for i := 0; i < 129; i++ {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%03d", i), Session: "history", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar mechanism %d has independent durable evidence.", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]bool{}
	for _, page := range before.Pages {
		if page.Kind == "subject" {
			original[page.ID] = true
		}
	}
	if len(original) != 129 {
		t.Fatalf("expected 129 original concepts, got %d", len(original))
	}
	if err := v.Capture(knowledge.Source{ID: "shared-evidence", Session: "new-chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar mechanisms share an additional operating constraint."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || updated[0] == updated[1] {
		t.Fatalf("shared evidence must reach concepts in both catalog windows, got %v", updated)
	}
	for _, id := range updated {
		page, err := v.Read(id)
		if err != nil || !original[id] || len(page.Evidence) != 2 {
			t.Fatalf("concept must retain its identity and both source citations: id=%s evidence=%d err=%v", id, len(page.Evidence), err)
		}
	}
	after, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, page := range after.Pages {
		if page.Kind == "subject" {
			count++
		}
	}
	if count != len(original) {
		t.Fatalf("shared evidence created an extra concept: %d -> %d", len(original), count)
	}
}
