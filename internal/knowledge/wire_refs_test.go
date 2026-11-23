package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func wireReferenceFixture(t *testing.T, organize func(map[string]any) map[string]any) {
	t.Helper()
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if p["catalog"] != nil {
			return organize(p)
		}
		return map[string]any{"action": "write", "sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
}

func TestKnowledgeWireReferencesRejectNoteAsPageAndRetryThinReuse(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	invalidTargets := []string{"N0", "P999"}
	wireReferenceFixture(t, func(p map[string]any) map[string]any {
		catalog := p["catalog"].([]any)
		if catalog[0].(map[string]any)["id"] != "N0" {
			t.Errorf("note wire reference is not typed: %v", catalog[0])
		}
		if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
			if existing[0].(map[string]any)["id"] != "P0" {
				t.Errorf("page wire reference is not typed: %v", existing[0])
			}
			attempts++
			target := "P0"
			if attempts <= len(invalidTargets) {
				target = invalidTargets[attempts-1]
			}
			return map[string]any{"subjects": []any{map[string]any{"existing_subject_id": target, "notes": []any{"N0"}}}}
		}
		return map[string]any{"subjects": []any{map[string]any{"title": "Queues", "scope": "Cedar", "notes": []any{"N0"}}}}
	})
	capture := func(id string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar queue rule " + id + " remains durable."}); err != nil {
			t.Fatal(err)
		}
	}
	capture("baseline")
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var original knowledge.Page
	for _, page := range before.Pages {
		if page.Kind == "subject" {
			original = page
		}
	}
	if original.ID == "" || original.ID == "P0" || len(original.Evidence) != 1 {
		t.Fatalf("canonical baseline missing: %+v", original)
	}
	invalidTargets = append(invalidTargets, original.ID)
	capture("added")
	for _, target := range invalidTargets {
		if err := v.Flush(context.Background(), "fixture", true); err == nil || !strings.Contains(err.Error(), "reference") {
			t.Fatalf("invalid page reference %q accepted: %v", target, err)
		}
		failed, err := v.Read(original.ID)
		if err != nil || len(failed.Evidence) != 1 {
			t.Fatalf("rejected wire response changed publication: %+v %v", failed, err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("invalid wire response poisoned retry: %v", err)
	}
	after, err := v.Read(original.ID)
	if err != nil || len(after.Evidence) != 2 || after.Title != original.Title || attempts != 4 {
		t.Fatalf("thin page reference lost identity/evidence: %+v attempts=%d err=%v", after, attempts, err)
	}
}

func TestKnowledgeWireReferencesTranslateOrganizationProposal(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	reviews := 0
	wireReferenceFixture(t, func(p map[string]any) map[string]any {
		if proposal, ok := p["proposed_subjects"].([]any); ok {
			reviews++
			for i, raw := range proposal {
				subject := raw.(map[string]any)
				if _, ambiguous := subject["id"]; ambiguous {
					t.Error("organization proposal retained ambiguous id field")
				}
				if subject["notes"].([]any)[0] != fmt.Sprintf("N%d", i) {
					t.Errorf("proposal note did not use local note reference: %+v", subject)
				}
			}
			if proposal[0].(map[string]any)["existing_subject_id"] != "P0" {
				t.Errorf("proposal reuse did not use local page reference: %+v", proposal[0])
			}
			return map[string]any{"subjects": proposal}
		}
		subjects := []any{}
		for i, raw := range p["catalog"].([]any) {
			note := raw.(map[string]any)
			subject := map[string]any{"title": note["title"], "scope": "Cedar", "notes": []any{note["id"]}}
			if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 && i == 0 {
				subject = map[string]any{"existing_subject_id": existing[0].(map[string]any)["id"], "notes": []any{note["id"]}}
			}
			subjects = append(subjects, subject)
		}
		return map[string]any{"subjects": subjects}
	})
	capture := func(id string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar durable mechanism " + id + " has independent evidence."}); err != nil {
			t.Fatal(err)
		}
	}
	capture("baseline")
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	original := ""
	for _, page := range before.Pages {
		if page.Kind == "subject" {
			original = page.ID
		}
	}
	for i := 0; i < 3; i++ {
		capture(fmt.Sprintf("added-%d", i))
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	page, err := v.Read(original)
	if err != nil || len(page.Evidence) != 2 || reviews != 1 {
		t.Fatalf("organization round trip lost canonical reuse: %+v reviews=%d err=%v", page, reviews, err)
	}
}

func TestKnowledgeWirePageReferencesAreLocalToEachCatalogWindow(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	targets := []string{}
	wireReferenceFixture(t, func(p map[string]any) map[string]any {
		if proposal, ok := p["proposed_subjects"].([]any); ok {
			return map[string]any{"subjects": proposal}
		}
		catalog := p["catalog"].([]any)
		if catalog[0].(map[string]any)["title"] == "shared-update" {
			existing := p["existing_subjects"].([]any)
			first := existing[0].(map[string]any)
			if first["id"] != "P0" || catalog[0].(map[string]any)["id"] != "N0" || p["reuse_only"] != true {
				t.Errorf("references not reset within catalog window: %+v", first)
			}
			targets = append(targets, first["title"].(string))
			return map[string]any{"subjects": []any{map[string]any{"existing_subject_id": "P0", "notes": []any{"N0"}}}}
		}
		subjects := []any{}
		for _, raw := range catalog {
			note := raw.(map[string]any)
			subjects = append(subjects, map[string]any{"title": note["title"], "scope": "Cedar", "notes": []any{note["id"]}})
		}
		return map[string]any{"subjects": subjects}
	})
	for i := 0; i < 129; i++ {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%03d", i), Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: fmt.Sprintf("Cedar independent mechanism %d has durable evidence.", i)}); err != nil {
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
	canonical := map[string]string{}
	for _, page := range before.Pages {
		if page.Kind == "subject" {
			canonical[page.Title] = page.ID
		}
	}
	if len(canonical) != 129 {
		t.Fatalf("window fixture needs 129 canonical pages, got %d", len(canonical))
	}
	if err := v.Capture(knowledge.Source{ID: "shared-update", Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar shares this durable update between two independent mechanisms."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] == targets[1] {
		t.Fatalf("did not inspect two distinct catalog windows: %v", targets)
	}
	for _, title := range targets {
		page, err := v.Read(canonical[title])
		if err != nil || page.Title != title || len(page.Evidence) != 2 {
			t.Fatalf("local P0 resolved to the wrong canonical page: %+v %v", page, err)
		}
	}
}
