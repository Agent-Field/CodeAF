package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeCorrectionRefreshesSubjectAfterOnlyClaimRemoval(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	retired := ""
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			claim := func(id, text, topic, key string) any {
				return map[string]any{"local_id": id, "text": text, "scope": "Cedar", "topic": topic, "key": key, "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": text}}}
			}
			if s["id"] == "correction" {
				return map[string]any{"claims": []any{claim("new", "Cedar retries three times.", "Updated retries", "retry count")}, "relations": []any{map[string]any{"from": "new:new", "to": retired, "kind": "supersedes"}}}
			}
			return map[string]any{"claims": []any{claim("old", "Cedar retries twice.", "Overview", "retry count"), claim("stable", "Cedar queues durable jobs.", "Overview", "durability")}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["title"], "scope": "Cedar", "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		if p["remaining_steps"] != nil {
			refs := []any{}
			for _, raw := range p["claims"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Current overview", "paragraphs": []any{map[string]any{"text": "Updated overview keeps durable jobs.", "claims": refs}}}}}
		}
		for _, raw := range p["claims"].([]any) {
			claim := raw.(map[string]any)
			if claim["text"] == "Cedar retries twice." {
				retired = claim["id"].(string)
			}
		}
		subject := p["subject"].(map[string]any)
		return map[string]any{"sections": []any{map[string]any{"heading": "Overview", "paragraphs": []any{map[string]any{"text": "Legacy overview combines retries and durability.", "claims": subject["claims"]}}}}}
	})
	for _, source := range []knowledge.Source{
		{ID: "original", Session: "chat", Role: "user", TS: "2026-10-24T12:00:00Z", Text: "Cedar retries twice. Cedar queues durable jobs."},
		{ID: "correction", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar retries three times."},
	} {
		if err := v.Capture(source); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind == "subject" && page.Title == "Overview" {
			if !strings.Contains(knowledge.PageMarkdown(page), "Updated overview keeps durable jobs.") {
				t.Fatal("removing a superseded claim did not refresh its existing subject")
			}
			return
		}
	}
	t.Fatal("existing subject disappeared after correction")
}

func TestKnowledgeCorrectionInvalidatesSourceOnlyGuideWhenRefreshIsEmpty(t *testing.T) {
	t.Run("empty refresh", func(t *testing.T) { checkKnowledgeCorrectionGuide(t, true, false) })
	t.Run("stale refresh", func(t *testing.T) { checkKnowledgeCorrectionGuide(t, false, false) })
}

func TestKnowledgeCorrectionPreservesDisjointSourceBlock(t *testing.T) {
	checkKnowledgeCorrectionGuide(t, true, true)
}

func checkKnowledgeCorrectionGuide(t *testing.T, emptyRefresh, disjointSource bool) {
	t.Helper()
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	retired, corrected := "", false
	guideText := "Guide says Cedar retries twice."
	if disjointSource {
		guideText = "Guide preserves current receipts."
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			relations := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				id := s["id"].(string)
				if id == "unclaimed" {
					continue
				}
				topic, key := "Deployment", "deployment "+id
				if id == "old" || id == "correction" {
					topic, key = "Retries", "retry count"
				}
				if id == "correction" {
					topic = "Updated retries"
					relations = append(relations, map[string]any{"from": "new:" + id, "to": retired, "kind": "supersedes"})
				}
				quote := s["text"]
				if id == "old" && disjointSource {
					quote = "Cedar retries twice."
					claims = append(claims, map[string]any{"local_id": "receipts", "text": "Cedar stores café receipts.", "scope": "Cedar", "topic": "Retries", "key": "receipts", "kind": "fact", "evidence": []any{map[string]any{"source": id, "quote": "Cedar stores café receipts."}}})
				}
				claims = append(claims, map[string]any{"local_id": id, "text": quote, "scope": "Cedar", "topic": topic, "key": key, "kind": "fact", "evidence": []any{map[string]any{"source": id, "quote": quote}}})
			}
			return map[string]any{"claims": claims, "relations": relations}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["title"], "scope": "Cedar", "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}, map[string]any{"id": "P1", "supported": true, "useful": true}}}
		}
		if p["remaining_steps"] != nil {
			if corrected && emptyRefresh {
				return map[string]any{"action": "write", "sections": []any{}, "gaps": []any{}}
			}
			blocks := p["blocks"].([]any)
			if len(blocks) < 2 {
				source := "old"
				if len(blocks) == 1 {
					source = "unclaimed"
				}
				if source == "old" && disjointSource {
					return map[string]any{"action": "read", "source": source, "start": len([]rune("Cedar retries twice. ☕ ")), "length": len([]rune("Cedar stores café receipts."))}
				}
				return map[string]any{"action": "read", "source": source}
			}
			return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Deployment guide", "paragraphs": []any{
				map[string]any{"text": guideText, "blocks": []any{blocks[0].(map[string]any)["id"]}},
				map[string]any{"text": "Guide preserves unextracted audit breadcrumbs.", "blocks": []any{blocks[1].(map[string]any)["id"]}},
			}}}}
		}
		for _, raw := range p["claims"].([]any) {
			claim := raw.(map[string]any)
			if claim["text"] == "Cedar retries twice." {
				retired = claim["id"].(string)
			}
		}
		subject := p["subject"].(map[string]any)
		return map[string]any{"sections": []any{map[string]any{"heading": "Overview", "paragraphs": []any{map[string]any{"text": "Supported initial overview.", "claims": subject["claims"]}}}}}
	})
	capture := func(id, text, ts string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: ts, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	flush := func() {
		t.Helper()
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	original := "Cedar retries twice."
	if disjointSource {
		original += " ☕ Cedar stores café receipts. Cedar retries twice."
	}
	capture("old", original, "2026-10-24T12:00:00Z")
	capture("deploy", "Cedar deploys durable workers.", "2026-10-24T12:00:00Z")
	capture("unclaimed", "Cedar retains audit breadcrumbs.", "2026-10-24T12:00:00Z")
	flush()
	capture("added", "Cedar deploys with extra tracing.", "2026-10-24T13:00:00Z")
	flush()
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guideID := ""
	for _, page := range state.Pages {
		if page.Kind == "subject" && page.Title == "Deployment" {
			guideID = page.ID
			if !strings.Contains(knowledge.PageMarkdown(page), guideText) {
				t.Fatal("source-only guide fixture was not published")
			}
		}
	}
	if guideID == "" {
		t.Fatal("deployment guide missing")
	}
	corrected = true
	capture("correction", "Cedar retries three times.", "2026-10-25T12:00:00Z")
	flush()
	page, err := v.Read(guideID)
	if err != nil {
		t.Fatal(err)
	}
	markdown := knowledge.PageMarkdown(page)
	if disjointSource && !strings.Contains(markdown, guideText) {
		t.Fatal("retiring a disjoint claim removed unrelated advice from the same source")
	}
	if !disjointSource && strings.Contains(markdown, guideText) {
		t.Fatal("source-only guide retained retired advice after an empty refresh")
	}
	if !strings.Contains(markdown, "Guide preserves unextracted audit breadcrumbs.") {
		t.Fatal("correction discarded source-only evidence with no retired claim dependency")
	}
}
