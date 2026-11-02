package knowledge_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeConsolidatesEachCompleteScopeSeparately(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	reviewed := map[string]int{}
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				scope, topic := "Cedar", "Issue assignment"
				if strings.Contains(s["id"].(string), "desk") {
					scope = "Cedar Desk"
				}
				if strings.Contains(s["id"].(string), "triage") {
					topic = "Issue triage"
				}
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": scope, "topic": topic, "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if proposal, ok := p["proposed_subjects"].([]any); ok {
			return map[string]any{"subjects": proposal}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["title"], "scope": n["scope"], "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			checks := []any{}
			for _, raw := range paragraphs {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
			}
			return map[string]any{"checks": checks}
		}
		return synthesisDraft(p)
	}, func(p map[string]any) map[string]any {
		catalog := p["consolidation_catalog"].([]any)
		scope := catalog[0].(map[string]any)["scope"].(string)
		members := []any{}
		for _, raw := range catalog {
			draft := raw.(map[string]any)
			if draft["scope"] != scope {
				t.Errorf("one consolidation request combined exact scopes %q and %q", scope, draft["scope"])
			}
			members = append(members, draft["id"])
		}
		mu.Lock()
		reviewed[scope] = len(catalog)
		mu.Unlock()
		return map[string]any{"groups": []any{map[string]any{"members": members, "title": "Issue handling", "scope": scope, "purpose": "Assign and triage issues", "folder": []any{scope, "Workflows"}}}}
	})
	for _, source := range []knowledge.Source{
		{ID: "cedar-assignment", Text: "Cedar assigns issues to a maintainer."},
		{ID: "cedar-triage", Text: "Cedar triages issues by impact."},
		{ID: "desk-assignment", Text: "Cedar Desk assigns issues to a designer."},
		{ID: "desk-triage", Text: "Cedar Desk triages issues by customer impact."},
	} {
		source.Session, source.Role, source.TS = "chat", "user", "2026-10-25T12:00:00Z"
		if err := v.Capture(source); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if len(reviewed) != 2 || reviewed["Cedar"] != 2 || reviewed["Cedar Desk"] != 2 {
		t.Fatalf("scope review did not receive both complete draft catalogs: %v", reviewed)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guides := map[string]knowledge.Page{}
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			guides[page.Scope] = page
		}
	}
	if len(guides) != 2 || guides["Cedar"].ID == guides["Cedar Desk"].ID {
		t.Fatalf("overlapping headings lost distinct canonical identities: %+v", guides)
	}
	for scope, guide := range guides {
		if guide.Title != "Issue handling" || len(guide.Evidence) != 2 {
			t.Fatalf("scope %s lost guide detail: %+v", scope, guide)
		}
		for _, evidence := range guide.Evidence {
			if strings.HasPrefix(evidence.Source, "desk-") != (scope == "Cedar Desk") {
				t.Fatalf("scope %s inherited unrelated evidence %s", scope, evidence.Source)
			}
		}
	}
}
