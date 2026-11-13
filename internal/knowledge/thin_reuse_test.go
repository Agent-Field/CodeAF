package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeReusesConceptWhenResponseOnlyContainsIdentity(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "claim", "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			notes := []any{catalog[0].(map[string]any)["id"]}
			if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
				return map[string]any{"subjects": []any{map[string]any{"existing_subject_id": existing[0].(map[string]any)["id"], "notes": notes}}}
			}
			return map[string]any{"subjects": []any{map[string]any{"title": "Queue operations", "scope": "Cedar", "purpose": "Operate durable job queues", "folder": []any{"Cedar", "Operations"}, "notes": notes}}}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			checks := []any{}
			for _, raw := range paragraphs {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
			}
			return map[string]any{"checks": checks}
		}
		refs := []any{}
		for _, raw := range p["claims"].([]any) {
			refs = append(refs, raw.(map[string]any)["id"])
		}
		return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Operations", "paragraphs": []any{map[string]any{"text": "Cedar queues and retries durable work.", "claims": refs}}}}}
	})
	original := ""
	for i, text := range []string{"Cedar queues durable work.", "Cedar retries durable work."} {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%d", i), Session: fmt.Sprintf("chat-%d", i), Role: "user", TS: "2026-10-26T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
		state, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		guides := []knowledge.Page{}
		for _, page := range state.Pages {
			if page.Kind == "subject" {
				guides = append(guides, page)
			}
		}
		if len(guides) != 1 {
			t.Fatalf("expected one canonical guide, got %d", len(guides))
		}
		page := guides[0]
		if i == 0 {
			original = page.ID
		}
		if page.ID != original || page.Title != "Queue operations" || page.Scope != "Cedar" || len(page.Folder) != 2 || page.Folder[0] != "Cedar" || page.Folder[1] != "Operations" {
			t.Fatalf("identity-only reuse lost canonical metadata: %+v", page)
		}
		if len(page.Evidence) != i+1 {
			t.Fatalf("identity-only reuse dropped new evidence: got %d citations, want %d", len(page.Evidence), i+1)
		}
	}
}
