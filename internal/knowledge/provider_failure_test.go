package knowledge_test

import (
	"bytes"
	"context"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgeIncompleteResponseReportsStageAndRetainsSources(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"model":"deepseek/deepseek-v4.1-flash","choices":[{"finish_reason":"length","message":{"content":"{\"claims\":["}}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(raw))}, nil
	})
	err := v.Flush(context.Background(), "fixture", true)
	if err == nil || !strings.Contains(err.Error(), "update") || !strings.Contains(err.Error(), "length") {
		t.Fatalf("incomplete result lost the failing stage/reason: %v", err)
	}
	state, e := v.Snapshot()
	if e != nil || state.Pending != 1 || len(state.Pages) != 0 {
		t.Fatalf("unfinished result consumed evidence: %+v %v", state, e)
	}
}
