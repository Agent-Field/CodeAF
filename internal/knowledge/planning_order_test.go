package knowledge_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgePlansAllBoundedBatchesBeforeInitialWriting(t *testing.T) {
	for _, test := range []struct {
		name   string
		notes  int
		writes int32
		state  string
	}{
		{"merged batches", 33, 1, "explained"},
		{"oversized merged concept", 161, 0, "needs_partition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			var planned, understandCalls, learnerCalls atomic.Int32
			var oversizedRequest, wroteBeforePlanning atomic.Bool
			withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
				if messages, ok := p["messages"].([]any); ok {
					claims := []any{}
					for _, raw := range messages {
						s := raw.(map[string]any)
						claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
					}
					return map[string]any{"claims": claims}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					if len(catalog) > 32 {
						oversizedRequest.Store(true)
					}
					planned.Add(int32(len(catalog)))
					notes := []any{}
					for _, raw := range catalog {
						notes = append(notes, raw.(map[string]any)["id"])
					}
					subject := map[string]any{"title": "Job queues", "scope": "Cedar", "notes": notes}
					if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
						subject["existing_subject_id"] = existing[0].(map[string]any)["id"]
					}
					return map[string]any{"subjects": []any{subject}}
				}
				if p["paragraphs"] != nil {
					return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
				}
				if p["remaining_steps"] != nil {
					learnerCalls.Add(1)
					return refinementDraft(p)
				}
				understandCalls.Add(1)
				if planned.Load() != int32(test.notes) {
					wroteBeforePlanning.Store(true)
				}
				return synthesisDraft(p)
			})
			for i := 0; i < test.notes; i++ {
				if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("rule-%02d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar queue rule %d is durable.", i)}); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < (test.notes+63)/64; i++ {
				if err := v.Flush(context.Background(), "fixture", true); err != nil {
					t.Fatal(err)
				}
			}
			if oversizedRequest.Load() {
				t.Fatal("organization sent more than 32 evidence notes in one planning request")
			}
			if wroteBeforePlanning.Load() || planned.Load() != int32(test.notes) {
				t.Fatalf("initial prose started before the complete plan: planned=%d", planned.Load())
			}
			if understandCalls.Load() != test.writes || learnerCalls.Load() != 0 {
				t.Fatalf("one merged initial concept should be written once: initial=%d refresh=%d", understandCalls.Load(), learnerCalls.Load())
			}
			state, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			for _, page := range state.Pages {
				if page.Kind == "subject" {
					if page.LearningState != test.state {
						t.Fatalf("unexpected initial subject state: got %s want %s", page.LearningState, test.state)
					}
					if len(page.Evidence) != test.notes {
						t.Fatalf("late planning batch lost evidence: got %d sources", len(page.Evidence))
					}
					return
				}
			}
			t.Fatal("merged initial concept was not published")
		})
	}
}
