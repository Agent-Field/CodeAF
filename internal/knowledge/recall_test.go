package knowledge_test

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
	"testing"
)

func TestKnowledgeRecallUsesCurrentCitationsAndHumanEdits(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] != nil {
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": "s", "quote": "Cedar queues jobs."}}}}}
		}
		if p["question"] != nil {
			claims := p["claims"].([]any)
			c := claims[0].(map[string]any)
			return map[string]any{"answer": c["text"], "citations": []any{c["id"]}}
		}
		return map[string]any{"subjects": []any{}}
	})
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	answer, err := v.Recall(context.Background(), "fixture", "What does Cedar queue?", "Cedar")
	if err != nil || !strings.Contains(answer.Answer, "queues jobs") || len(answer.Citations) != 1 || answer.Citations[0].Source != "s" {
		t.Fatalf("supported answer missing: %+v %v", answer, err)
	}
	answer, err = v.Recall(context.Background(), "fixture", "What does Cedar queue?", "Other")
	if err != nil || len(answer.Citations) != 0 {
		t.Fatalf("foreign scope leaked: %+v %v", answer, err)
	}
	snapshot, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.EditPage(snapshot.Pages[0].ID, snapshot.Pages[0].Revision, "Cedar queues invoices."); err != nil {
		t.Fatal(err)
	}
	answer, err = v.Recall(context.Background(), "fixture", "What does Cedar queue?", "Cedar")
	if err != nil || !strings.Contains(answer.Answer, "invoices") || len(answer.Citations) != 1 || !strings.HasPrefix(answer.Citations[0].Source, "human-edit:") {
		t.Fatalf("human edit not authoritative as its own source: %+v %v", answer, err)
	}
}

func TestKnowledgeRefinesNextDirtySubjectOnLaterPass(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	guides := map[string]int{}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for i, m := range messages {
				s := m.(map[string]any)
				scope := strings.Fields(s["text"].(string))[0]
				claims = append(claims, map[string]any{"local_id": string(rune('a' + i)), "text": s["text"], "scope": scope, "topic": "Operations", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " guide", "scope": n["scope"], "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["remaining_steps"] != nil {
			s := p["subject"].(map[string]any)
			scope := s["scope"].(string)
			guides[scope]++
			refs := []any{}
			for _, raw := range p["claims"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Current operations", "paragraphs": []any{map[string]any{"text": scope + " baseline and added guidance.", "claims": refs}}}}}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
	})
	for _, round := range []string{"baseline", "added"} {
		for _, scope := range []string{"Cedar", "Maple"} {
			if err := v.Capture(knowledge.Source{ID: scope + round, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: scope + " " + round + " guidance."}); err != nil {
				t.Fatal(err)
			}
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	if len(guides) != 1 {
		t.Fatalf("expected one bounded guide job, got %v", guides)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if len(guides) != 2 {
		t.Fatalf("second dirty subject was stranded: %v", guides)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	for scope, n := range guides {
		if n != 1 {
			t.Fatalf("unchanged %s guide retried %d times", scope, n)
		}
	}
	hits, err := v.Search("baseline and added", 5)
	found := 0
	for _, hit := range hits {
		if hit.Page.Kind == "subject" && strings.Contains(knowledge.PageMarkdown(hit.Page), "baseline and added guidance.") {
			found++
		}
	}
	if err != nil || found != 2 {
		t.Fatalf("both refined subjects should be searchable: found=%d %v", found, err)
	}
}

func TestKnowledgeFrozenReplayReportsFailedSourceContracts(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	withKnowledgeModel(t, func(p map[string]any) map[string]any { return map[string]any{"claims": []any{}} })
	raw, err := v.RunSupportContracts(context.Background(), "fixture")
	if err == nil {
		t.Fatal("missing required source support must fail the frozen replay")
	}
	var report struct {
		ManifestHash  string `json:"manifest_hash"`
		Total, Passed int
		Cases         []struct {
			ID     string
			Checks map[string]bool
		}
	}
	if decodeErr := json.Unmarshal(raw, &report); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if report.ManifestHash != "f6c09588eb7cf01e" || report.Total != 6 || len(report.Cases) != 6 || report.Passed == report.Total {
		t.Fatalf("incomplete or mutated frozen manifest: %+v", report)
	}
}

func TestKnowledgeRecallAbstentionPublishesNoSupportingCitations(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] != nil {
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Kestrel stores credentials locally.", "scope": "Kestrel", "topic": "Storage", "kind": "fact", "evidence": []any{map[string]any{"source": "s", "quote": "Kestrel stores credentials locally."}}}}}
		}
		if p["question"] != nil {
			c := p["claims"].([]any)[0].(map[string]any)
			return map[string]any{"answer": "No supported answer: these claims do not address annual subscription pricing.", "abstain": true, "citations": []any{c["id"]}}
		}
		return map[string]any{"subjects": []any{}}
	})
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Kestrel stores credentials locally."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	answer, err := v.Recall(context.Background(), "fixture", "What is Kestrel's annual subscription price?", "Kestrel")
	if err != nil || len(answer.Citations) != 0 || !strings.Contains(answer.Answer, "No supported answer") {
		t.Fatalf("abstention presented irrelevant evidence as answer support: %+v %v", answer, err)
	}
}
