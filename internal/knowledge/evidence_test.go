package knowledge_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeRecallOrdersEvidenceByInstantAcrossTimezones(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	for _, source := range []knowledge.Source{
		{ID: "early", Session: "chat", Role: "user", TS: "2026-10-25T10:30:00+02:00", Text: "Cedar queues jobs."},
		{ID: "late", Session: "chat", Role: "user", TS: "2026-10-25T09:00:00Z", Text: "Cedar queues jobs."},
	} {
		if err := v.Capture(source); err != nil {
			t.Fatal(err)
		}
	}
	var basis map[string]any
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["question"] != nil {
			claims := payload["claims"].([]any)
			claim := claims[0].(map[string]any)
			basis = claim["evidence_basis"].(map[string]any)
			return map[string]any{"answer": "Cedar queues jobs.", "citations": []any{claim["id"]}, "abstain": false}
		}
		if payload["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": "early", "quote": "Cedar queues jobs."}, map[string]any{"source": "late", "quote": "Cedar queues jobs."}}}}}
	})
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Recall(context.Background(), "fixture", "Cedar queues jobs?", "Cedar"); err != nil {
		t.Fatal(err)
	}
	if basis["observed_from"] != "2026-10-25T10:30:00+02:00" || basis["observed_to"] != "2026-10-25T09:00:00Z" {
		t.Fatalf("evidence chronology sorted timestamps as text: %+v", basis)
	}
	if basis["age_exceeds_policy"] != nil {
		t.Fatalf("missing freshness policy should not classify evidence age: %+v", basis)
	}
}
