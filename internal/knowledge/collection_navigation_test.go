package knowledge_test

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"testing"
)

func TestKnowledgeTopicMapUsesItsPagesCollection(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
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
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["title"], "scope": "Cedar", "folder": []any{"Library", "Cedar"}, "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	for i, text := range []string{"Cedar queues durable work.", "Cedar reports usage spending."} {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("concept-%d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	maps := 0
	for _, p := range state.Pages {
		if p.Kind == "map" {
			maps++
			if len(p.Folder) != 2 || p.Folder[0] != "Library" || p.Folder[1] != "Cedar" {
				t.Fatalf("topic map created a disconnected scope folder: %v", p.Folder)
			}
		}
	}
	if maps != 1 {
		t.Fatalf("expected one connected topic map, got %d", maps)
	}
}
