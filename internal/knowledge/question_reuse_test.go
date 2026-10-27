package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeNewQuestionEvidenceDoesNotRepurchaseUnchangedGroups(t *testing.T) {
	for _, size := range []int{6, 30, 128} {
		t.Run(fmt.Sprint(size), func(t *testing.T) { checkQuestionGroupReuse(t, size, false) })
	}
	t.Run("same-text questions", func(t *testing.T) { checkQuestionGroupReuse(t, 6, true) })
}

func checkQuestionGroupReuse(t *testing.T, size int, duplicateText bool) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	resolverCalls, inspected := 0, 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": strings.Fields(s["text"].(string))[0], "topic": "Operations", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if p["reuse_only"] == true {
			return map[string]any{"subjects": []any{}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			subjects := []any{}
			for _, raw := range catalog {
				n := raw.(map[string]any)
				subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " guide", "scope": n["scope"], "notes": []any{n["id"]}})
			}
			return map[string]any{"subjects": subjects}
		}
		if p["per_subject"] != nil {
			return map[string]any{"keep": []any{}}
		}
		if p["budget"] != nil {
			return map[string]any{"selected": []any{}}
		}
		if p["paragraphs"] != nil {
			qs := []any{}
			for _, raw := range p["questions"].([]any) {
				qs = append(qs, map[string]any{"id": raw.(map[string]any)["id"], "grounded": true})
			}
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}, "questions": qs}
		}
		if qs, ok := p["questions"].([]any); ok && p["subject"] == nil {
			resolverCalls++
			inspected += len(qs)
			if len(qs) > 16 {
				t.Errorf("unbounded question request: %d", len(qs))
			}
			claims := map[any]map[string]any{}
			for _, raw := range p["claims"].([]any) {
				claim := raw.(map[string]any)
				claims[claim["id"]] = claim
			}
			crosscut := false
			for _, c := range claims {
				crosscut = crosscut || c["scope"] == "Crosscut"
			}
			matches := []any{}
			for _, raw := range qs {
				q := raw.(map[string]any)
				refs := q["candidates"].([]any)
				scope := strings.Fields(q["question"].(string))[0]
				want := 6
				if size == 6 {
					want = 5
				}
				if scope == "Aster" {
					want = 1
				} else if crosscut {
					want = 6
				}
				if scope == "Aster" && crosscut {
					want = 2
				}
				if len(refs) != want {
					t.Errorf("candidate coverage for %s: got %d want %d", scope, len(refs), want)
				}
				for _, id := range refs {
					claim := claims[id]
					if claim["scope"] == scope || (scope == "Aster" && claim["scope"] != "Reference" && claim["scope"] != "Crosscut") || (scope != "Aster" && claim["scope"] == "Reference") {
						t.Errorf("candidate scope or owning-subject exclusion changed: %s -> %v", scope, claim)
					}
				}
				matches = append(matches, map[string]any{"id": q["id"], "relation": "related", "claims": []any{}})
			}
			return map[string]any{"matches": matches}
		}
		s := p["subject"].(map[string]any)
		qs := []any{}
		terms := []any{"shared", "durable", "checkpoint"}
		if s["scope"] == "Aster" {
			terms = []any{"novel", "protocol", "checkpoint"}
		}
		if s["scope"] != "Reference" && s["scope"] != "Crosscut" {
			for i := 0; i < 5; i++ {
				ordinal := i
				search := terms
				if duplicateText {
					ordinal = 0
					search = append(append([]any{}, terms...), fmt.Sprintf("detail%d", i))
				}
				qs = append(qs, map[string]any{"question": fmt.Sprintf("%s operating question %d?", s["scope"], ordinal), "claims": s["claims"], "search_terms": search})
			}
		}
		return map[string]any{"sections": []any{map[string]any{"heading": "Operations", "paragraphs": []any{map[string]any{"text": s["scope"].(string) + " operating guidance.", "claims": s["claims"]}}}}, "questions": qs}
	})
	capture := func(scope, text string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: scope, Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: scope + " " + text}); err != nil {
			t.Fatal(err)
		}
	}
	capture("Aster", "shared durable operations.")
	for i := 1; i < size; i++ {
		capture(fmt.Sprintf("Scope%03d", i), "shared durable operations.")
	}
	for pass := 0; pass < (size+63)/64; pass++ {
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	if resolverCalls != ((size-1)*5+15)/16 || inspected != (size-1)*5 {
		t.Fatalf("baseline question coverage: calls=%d questions=%d", resolverCalls, inspected)
	}
	resolverCalls, inspected = 0, 0
	v = knowledge.Open(profile)
	capture("Reference", "novel protocol uses a checkpoint.")
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if resolverCalls != 1 || inspected != 5 {
		t.Fatalf("unchanged question groups repurchased: calls=%d questions=%d; want only five newly answerable questions", resolverCalls, inspected)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guides := 0
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			guides++
			if len(page.Evidence) != 1 || len(page.Sections) == 0 {
				t.Fatalf("reuse lost published guidance or evidence: %+v", page)
			}
		}
	}
	if guides != size+1 {
		t.Fatalf("reuse lost a guide: got %d", guides)
	}
	resolverCalls, inspected = 0, 0
	capture("Crosscut", "novel protocol shared durable checkpoint correction.")
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if resolverCalls > (size*5+15)/16 || inspected != size*5 {
		t.Fatalf("cross-cutting evidence fragmented fresh work: calls=%d questions=%d", resolverCalls, inspected)
	}
}
