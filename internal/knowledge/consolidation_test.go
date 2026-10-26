package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeConsolidatesWholeInitialTreeBeforeWriting(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var planned, writes atomic.Int32
	var consolidated, wroteEarly atomic.Bool
	respond := func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["consolidation_catalog"].([]any); ok {
			if planned.Load() != 34 {
				t.Errorf("whole-tree review started with only %d evidence notes planned", planned.Load())
			}
			groups := []any{}
			issueMembers := []any{}
			for _, raw := range catalog {
				draft := raw.(map[string]any)
				if strings.Contains(draft["title"].(string), "Header spacing") {
					groups = append(groups, map[string]any{"members": []any{draft["id"]}, "title": "Conversation layout", "scope": "Cedar", "purpose": "Understand conversation spacing", "folder": []any{"Cedar", "Interface"}})
				} else {
					issueMembers = append(issueMembers, draft["id"])
				}
			}
			groups = append(groups, map[string]any{"members": issueMembers, "title": "Issue handling", "scope": "Cedar", "purpose": "Assign, triage and track issues", "folder": []any{"Cedar", "Working practices"}})
			consolidated.Store(true)
			return map[string]any{"groups": groups}
		}
		if proposal, ok := p["proposed_subjects"].([]any); ok {
			return map[string]any{"subjects": proposal}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			planned.Add(int32(len(catalog)))
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				titles := []string{"Issue assignment", "Issue and PR triage", "Attribution rule", "Issue coordination status"}
				title := "Header spacing PR/Issue1505"
				if n["title"] != "header" {
					var number int
					fmt.Sscanf(n["title"].(string), "issue-%d", &number)
					title = titles[number%len(titles)]
				}
				subject := map[string]any{"title": title, "scope": "Cedar", "purpose": title, "folder": []any{"Cedar"}, "notes": []any{n["id"]}}
				for _, prior := range p["existing_subjects"].([]any) {
					existing := prior.(map[string]any)
					if existing["title"] == title {
						subject["existing_subject_id"] = existing["id"]
					}
				}
				subjects = append(subjects, subject)
			}
			return map[string]any{"subjects": subjects}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		writes.Add(1)
		if !consolidated.Load() {
			wroteEarly.Store(true)
		}
		return synthesisDraft(p)
	}
	withConcurrentKnowledgeModel(t, respond, respond)
	for i := 0; i < 34; i++ {
		id, text := fmt.Sprintf("issue-%02d", i), fmt.Sprintf("Cedar issue practice %d preserves a durable rule.", i)
		if i == 33 {
			id, text = "header", "Cedar conversation headings need clear spacing."
		}
		if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if !consolidated.Load() || wroteEarly.Load() {
		t.Fatal("initial pages were written before whole-tree consolidation")
	}
	if writes.Load() != 2 {
		t.Fatalf("redundant workflow drafts were written separately: %d writes", writes.Load())
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	subjects := map[string]knowledge.Page{}
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			subjects[page.Title] = page
		}
	}
	if len(subjects) != 2 || len(subjects["Issue handling"].Evidence) != 33 || len(subjects["Conversation layout"].Evidence) != 1 {
		t.Fatalf("whole-tree consolidation lost canonical headings or evidence: %+v", subjects)
	}
}

func TestKnowledgeRejectsIncompleteOrUnsafeConsolidationBeforeWriting(t *testing.T) {
	for _, mistake := range []string{"missing draft", "repeated draft", "unknown draft", "mixed scopes"} {
		t.Run(mistake, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			var writes atomic.Int32
			invalid := true
			withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
				if messages, ok := p["messages"].([]any); ok {
					claims := []any{}
					for _, raw := range messages {
						s := raw.(map[string]any)
						scope := strings.Fields(s["text"].(string))[0]
						claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": scope, "topic": "Operations", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
					}
					return map[string]any{"claims": claims}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					subjects := []any{}
					for _, raw := range catalog {
						n := raw.(map[string]any)
						subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " operations", "scope": n["scope"], "notes": []any{n["id"]}})
					}
					return map[string]any{"subjects": subjects}
				}
				if p["paragraphs"] != nil {
					return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
				}
				writes.Add(1)
				return synthesisDraft(p)
			}, func(p map[string]any) map[string]any {
				catalog := p["consolidation_catalog"].([]any)
				groups := []any{}
				for _, raw := range catalog {
					draft := raw.(map[string]any)
					groups = append(groups, map[string]any{"members": []any{draft["id"]}, "title": draft["title"], "scope": draft["scope"]})
				}
				if invalid {
					first := groups[0].(map[string]any)
					switch mistake {
					case "missing draft":
						groups = groups[:1]
					case "repeated draft":
						groups[1].(map[string]any)["members"] = first["members"]
					case "unknown draft":
						first["members"] = []any{"uninspected-draft"}
					case "mixed scopes":
						first["members"] = []any{catalog[0].(map[string]any)["id"], catalog[1].(map[string]any)["id"]}
						groups = groups[:1]
					}
				}
				return map[string]any{"groups": groups}
			})
			captureRefinementRound(t, v, "baseline", []string{"Cedar", "Maple"})
			if err := v.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("unsafe consolidation was accepted")
			}
			state, err := v.Snapshot()
			if err != nil || len(state.Pages) != 0 || writes.Load() != 0 {
				t.Fatalf("unsafe plan wrote or published pages: writes=%d pages=%d err=%v", writes.Load(), len(state.Pages), err)
			}
			invalid = false
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatalf("safe retry lost retained source progress: %v", err)
			}
			if writes.Load() != 2 {
				t.Fatalf("retry did not write both protected scopes: %d", writes.Load())
			}
		})
	}
}
