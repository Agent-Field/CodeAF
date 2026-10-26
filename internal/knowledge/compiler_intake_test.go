package knowledge_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestKnowledgeRevisitRejectsPendingSourceWithoutChangingItsContext(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	text := "Cedar queues jobs. Cedar retains receipts."
	if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
		t.Fatal(err)
	}
	if err := v.Revisit([]string{"s"}, map[string]knowledge.SourceWindow{"s": {Start: 19, Length: 23}}); !errors.Is(err, knowledge.ErrCompilerBusy) {
		t.Fatalf("pending revisit must be retryable without mutation: %v", err)
	}
	// A leaked 23-character window would admit both sources despite their
	// original combined size exceeding the compiler's 24,000-character cap.
	if err := v.Capture(knowledge.Source{ID: "next", Session: "chat", Role: "user", TS: "2026-10-25T12:00:01Z", Text: strings.Repeat("x", 23970)}); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		messages := p["messages"].([]any)
		if len(messages) != 1 {
			t.Fatalf("rejected window altered batch budgeting: messages=%d", len(messages))
		}
		m := messages[0].(map[string]any)
		if m["text"] != text || m["source_window"] != nil {
			t.Fatalf("rejected revisit changed first extraction context: %+v", m)
		}
		return map[string]any{"claims": []any{}}
	})
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := v.Status()
	if err != nil || snapshot.Pending != 1 {
		t.Fatalf("first extraction consumed the next bounded batch: pending=%d %v", snapshot.Pending, err)
	}
	// A later valid revisit must get its own requested span, never the rejected one.
	if err := v.Revisit([]string{"s"}, map[string]knowledge.SourceWindow{"s": {Start: 0, Length: 18}}); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeProposalStagingRetriesAfterInFlightExtraction(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(knowledge.Source{ID: "answer", Project: "Cedar", Session: "chat", Role: "assistant", TS: "2026-10-25T12:00:00Z", Text: "Cedar may queue invoices."}); err != nil {
		t.Fatal(err)
	}
	envelope := json.RawMessage(`{"knowledge_delta":[{"op":"learn","text":"Cedar may queue invoices.","subject":"Queues","evidence":[{"source":"self","quote":"may queue invoices"}]}]}`)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		if calls.Add(1) == 1 {
			close(started)
			<-release
		} else {
			hints, ok := p["proposals"].([]any)
			if !ok || len(hints) != 1 {
				t.Errorf("retried sidecar not supplied to extraction: %+v", p["proposals"])
			}
		}
		return map[string]any{"claims": []any{}}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var flushErr error
	go func() { defer close(finished); flushErr = v.Flush(ctx, "fixture", true) }()
	defer func() { unblock(); <-finished }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("extraction did not start")
	}
	if _, err := v.StageProposals("answer", map[string]string{"owner": "test", "project": "Cedar"}, envelope); !errors.Is(err, knowledge.ErrCompilerBusy) {
		t.Fatalf("in-flight staging must return retryable busy: %v", err)
	}
	unblock()
	<-finished
	if flushErr != nil {
		t.Fatal(flushErr)
	}
	receipt, err := v.StageProposals("answer", map[string]string{"owner": "test", "project": "Cedar"}, envelope)
	if err != nil || receipt.New != 1 {
		t.Fatalf("retry did not stage exactly one new proposal: %+v %v", receipt, err)
	}
	if err = v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("staged hint lost instead of reopening extraction: calls=%d", calls.Load())
	}
}
