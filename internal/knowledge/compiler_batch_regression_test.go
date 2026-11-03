package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeFlushKeepsNextUnownedBatchPending(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	for _, source := range []knowledge.Source{
		{ID: "first", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: strings.Repeat("a", 12001)},
		{ID: "next", Session: "chat", Role: "user", TS: "2026-10-25T12:00:01Z", Text: strings.Repeat("b", 12001)},
	} {
		if err := v.Capture(source); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] != nil {
			calls++
			return map[string]any{"claims": []any{}}
		}
		return map[string]any{"subjects": []any{}}
	})
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	status, err := v.Status()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || status.Pending != 1 {
		t.Fatalf("Flush consumed another bounded batch: extraction calls=%d pending=%d; want 1/1", calls, status.Pending)
	}
}
