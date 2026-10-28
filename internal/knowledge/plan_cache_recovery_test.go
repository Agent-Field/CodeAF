package knowledge_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeOrganizationCoverageFailureRetainsPagesAndRetriesFreshPlan(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	organizationCalls := 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if proposal, ok := p["proposed_subjects"].([]any); ok {
			organizationCalls++
			if organizationCalls == 1 {
				return map[string]any{"subjects": proposal[:1]}
			}
			return map[string]any{"subjects": proposal}
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
		return synthesisDraft(p)
	})
	capture := func(id int) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%d", id), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar durable mechanism %d has independent evidence.", id)}); err != nil {
			t.Fatal(err)
		}
	}
	capture(0)
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil || len(before.Pages) == 0 {
		t.Fatalf("initial publication unavailable: %+v %v", before, err)
	}
	capture(1)
	capture(2)
	if err := v.Flush(context.Background(), "fixture", true); err == nil || !strings.Contains(err.Error(), "coverage") {
		t.Fatalf("expected rejected evidence coverage, got %v", err)
	}
	failed, err := v.Snapshot()
	if err != nil || !reflect.DeepEqual(before.Pages, failed.Pages) {
		t.Fatalf("failed organization changed the prior publication: %+v %v", failed, err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("coverage failure poisoned the retry cache: %v", err)
	}
	after, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, page := range after.Pages {
		if page.Kind == "subject" {
			for _, evidence := range page.Evidence {
				sources[evidence.Source] = true
			}
		}
	}
	if organizationCalls != 2 || !reflect.DeepEqual(sources, map[string]bool{"source-0": true, "source-1": true, "source-2": true}) {
		t.Fatalf("fresh plan did not preserve all source evidence: requests=%d sources=%v", organizationCalls, sources)
	}
}

func TestKnowledgeOrganizationCanAddUnassignedCatalogEvidence(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%t", unknown), func(t *testing.T) {
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
					selected := catalog[:2]
					if p["proposed_subjects"] != nil {
						selected = catalog
					}
					subjects := []any{}
					for i, raw := range selected {
						n := raw.(map[string]any)
						id := n["id"]
						if unknown && i == 2 {
							id = "uninspected-note"
						}
						subjects = append(subjects, map[string]any{"title": n["title"], "scope": "Cedar", "notes": []any{id}})
					}
					return map[string]any{"subjects": subjects}
				}
				if p["paragraphs"] != nil {
					return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
				}
				return synthesisDraft(p)
			})
			for i := 0; i < 3; i++ {
				if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("source-%d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar durable mechanism %d has independent evidence.", i)}); err != nil {
					t.Fatal(err)
				}
			}
			err := v.Flush(context.Background(), "fixture", true)
			if unknown {
				if err == nil || !strings.Contains(err.Error(), "uninspected evidence note") {
					t.Fatalf("organization accepted an unknown added note: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("organization rejected additional inspected evidence: %v", err)
			}
			state, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			sources := map[string]bool{}
			for _, page := range state.Pages {
				if page.Kind == "subject" {
					for _, evidence := range page.Evidence {
						sources[evidence.Source] = true
					}
				}
			}
			if !reflect.DeepEqual(sources, map[string]bool{"source-0": true, "source-1": true, "source-2": true}) {
				t.Fatalf("organization lost planned or added evidence: %v", sources)
			}
		})
	}
}
