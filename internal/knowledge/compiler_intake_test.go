package knowledge_test

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
	"testing"
)

func TestKnowledgeRevisitConstrainsQuotesToExactWindow(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs. Cedar retains receipts."}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	calls := 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		calls++
		quote := "Cedar queues jobs."
		if calls == 2 {
			m := p["messages"].([]any)[0].(map[string]any)
			if m["text"] != "Cedar retains receipts." || m["source_window"] == nil {
				t.Fatalf("not exact source window: %+v", m)
			}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": "s", "quote": quote}}}}}
	})
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if err := v.Revisit([]string{"s"}, map[string]knowledge.SourceWindow{"s": {Start: 19, Length: 23}}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := v.Snapshot()
	if err != nil || len(snapshot.Pages) != 1 || len(snapshot.Pages[0].Claims) != 1 || calls != 2 {
		t.Fatalf("window revisit lost original or accepted hidden quote: %+v calls=%d %v", snapshot, calls, err)
	}
}
func TestKnowledgeStagesResponseProposalsAsExtractionHints(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "answer", Project: "Cedar", Session: "chat", Role: "assistant", TS: "2026-10-25T12:00:00Z", Text: "Cedar may queue invoices."}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	envelope := json.RawMessage(`{"knowledge_delta":[{"op":"learn","text":"Cedar may queue invoices.","subject":"Queues","evidence":[{"source":"self","quote":"may queue invoices"}]}]}`)
	if _, err := v.StageProposals("answer", map[string]string{"owner": "test", "project": "Cedar"}, envelope); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		hints, ok := p["proposals"].([]any)
		if !ok || len(hints) != 1 || !strings.Contains(hints[0].(map[string]any)["text"].(string), "may queue invoices") {
			t.Fatalf("proposal hint absent: %+v", p)
		}
		return map[string]any{"claims": []any{}}
	})
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := v.Snapshot()
	if err != nil || len(snapshot.Pages) != 0 {
		t.Fatalf("unaccepted sidecar became knowledge: %+v %v", snapshot, err)
	}
}
