package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeInitialOrganizationSeesCompleteHistory(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	discovered := 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			discovered += len(catalog)
		}
		return map[string]any{"subjects": []any{}}
	})
	for i := 0; i < 65; i++ {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%02d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar rule %d has a distinct setting.", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if discovered != 0 {
		t.Fatalf("organized an incomplete first-build catalog: %d notes", discovered)
	}
	if err := v.Capture(knowledge.Source{ID: "later-source", Session: "later-chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar accepts new work while organizing history."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if discovered != 65 {
		t.Fatalf("first organization did not consider every saved source: %d notes", discovered)
	}
	state, err := v.Snapshot()
	if err != nil || state.Pending != 1 {
		t.Fatalf("new work should remain queued beyond the finite initial boundary: pending=%d err=%v", state.Pending, err)
	}
}

func TestKnowledgeNewSessionReusesExistingConceptAndTree(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			n := catalog[0].(map[string]any)
			concept := map[string]any{"title": "Job queues", "scope": "Cedar", "folder": []any{"Cedar", "Operations"}, "notes": []any{n["id"]}}
			if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
				prior := existing[0].(map[string]any)
				concept["existing_subject_id"] = prior["id"]
				concept["title"] = "Queue operations"
			}
			return map[string]any{"subjects": []any{concept}}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		if p["remaining_steps"] != nil {
			refs := []any{}
			for _, raw := range p["claims"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Queues", "paragraphs": []any{map[string]any{"text": "Cedar queues jobs and retries failed jobs.", "claims": refs}}}}}
		}
		s := p["subject"].(map[string]any)
		return map[string]any{"sections": []any{map[string]any{"heading": "Queues", "paragraphs": []any{map[string]any{"text": "Cedar queues jobs.", "claims": s["claims"]}}}}}
	})
	var original string
	for i, text := range []string{"Cedar queues jobs.", "Cedar retries failed jobs."} {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("task-%d", i), Session: fmt.Sprintf("chat-%d", i), Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
		state, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		subjects := []knowledge.Page{}
		for _, p := range state.Pages {
			if p.Kind == "subject" {
				subjects = append(subjects, p)
			}
		}
		if len(subjects) != 1 {
			t.Fatalf("new session fragmented the concept into %d pages", len(subjects))
		}
		p := subjects[0]
		if i == 0 {
			original = p.ID
		} else if p.ID != original || len(p.Evidence) != 2 {
			t.Fatalf("reuse lost stable identity or citations: %+v", p)
		}
		if len(p.Folder) != 2 || p.Folder[0] != "Cedar" || p.Folder[1] != "Operations" {
			t.Fatalf("concept navigation lost: %v", p.Folder)
		}
	}
}

func TestKnowledgeReusesConceptBeyondFirstCatalogWindow(t *testing.T) {
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
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			existing, _ := p["existing_subjects"].([]any)
			if len(existing) > 128 {
				t.Errorf("catalog window exceeded documented bound: %d", len(existing))
			}
			if len(existing) > 0 && catalog[0].(map[string]any)["title"] == "related-work" {
				for _, raw := range existing {
					s := raw.(map[string]any)
					if s["title"] == "Concept source-128" {
						subjects = append(subjects, map[string]any{"existing_subject_id": s["id"], "title": s["title"], "scope": s["scope"], "notes": []any{catalog[0].(map[string]any)["id"]}})
					}
				}
			}
			if len(subjects) == 0 && p["reuse_only"] != true {
				for _, raw := range catalog {
					n := raw.(map[string]any)
					subjects = append(subjects, map[string]any{"title": "Concept " + n["title"].(string), "scope": "Cedar", "notes": []any{n["id"]}})
				}
			}
			return map[string]any{"subjects": subjects}
		}
		if p["remaining_steps"] != nil {
			return map[string]any{"action": "write", "sections": []any{}, "gaps": []any{}}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	for i := 0; i < 129; i++ {
		id := fmt.Sprintf("source-%03d", i)
		if err := v.Capture(knowledge.Source{ID: id, Session: "history", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar durable concept %d has independent evidence.", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	target := ""
	before := 0
	for _, p := range state.Pages {
		if p.Kind == "subject" {
			before++
			if p.Title == "Concept source-128" {
				target = p.ID
			}
		}
	}
	if before != 129 || target == "" {
		t.Fatalf("initial concepts unavailable: %d", before)
	}
	if err := v.Capture(knowledge.Source{ID: "related-work", Session: "new-chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar concept 128 also supports another operation."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	page, err := v.Read(target)
	if err != nil || len(page.Evidence) != 2 {
		t.Fatalf("existing concept outside first window lost its update: evidence=%d err=%v", len(page.Evidence), err)
	}
	state, err = v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	after := 0
	for _, p := range state.Pages {
		if p.Kind == "subject" {
			after++
		}
	}
	if after != before {
		t.Fatalf("catalog truncation produced another concept: %d -> %d", before, after)
	}
}

func TestKnowledgeReviewsConceptPlanBeforeWritingPages(t *testing.T) {
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
		if catalog, ok := p["catalog"].([]any); ok {
			if p["proposed_subjects"] != nil {
				refs := []any{}
				for _, raw := range catalog {
					refs = append(refs, raw.(map[string]any)["id"])
				}
				return map[string]any{"subjects": []any{map[string]any{"title": "Queue reliability", "scope": "Cedar", "folder": []any{"Cedar", "Operations"}, "notes": refs}}}
			}
			subjects := []any{}
			for i, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": fmt.Sprintf("Issue %d fix", i), "scope": "Cedar", "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true}}}
		}
		return map[string]any{"sections": []any{map[string]any{"heading": "Reliability", "paragraphs": []any{map[string]any{"text": "Cedar retries and recovers queued work.", "claims": p["subject"].(map[string]any)["claims"]}}}}}
	})
	for i, text := range []string{"Cedar retries failed queued jobs.", "Cedar recovers queued jobs after restart."} {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("issue-%d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
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
	count := 0
	for _, p := range state.Pages {
		if p.Kind == "subject" {
			count++
			if p.Title != "Queue reliability" || len(p.Evidence) != 2 {
				t.Fatalf("unreviewed task page published: %+v", p)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one lasting concept from reviewed plan, got %d", count)
	}
}

func TestKnowledgeInvalidConceptReferencesCanRetryWithoutPoisonedCache(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	discovery := 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			discovery++
			target := ""
			if discovery == 1 {
				target = "not-an-existing-page"
			}
			return map[string]any{"subjects": []any{map[string]any{"existing_subject_id": target, "title": "Job queues", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err == nil {
		t.Fatal("unknown canonical page reference accepted")
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("invalid reference poisoned retry cache: %v", err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	subjects := 0
	for _, p := range state.Pages {
		if p.Kind == "subject" {
			subjects++
			if len(p.Evidence) != 1 {
				t.Fatal("retry lost evidence")
			}
		}
	}
	if subjects != 1 || discovery != 2 {
		t.Fatalf("expected fresh valid plan on retry: pages=%d requests=%d", subjects, discovery)
	}
}
