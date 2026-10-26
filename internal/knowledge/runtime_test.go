package knowledge_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeConcurrentBuildWaitsAndCanBeCancelled(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(knowledge.Source{ID: "source", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	firstDone := make(chan error, 1)
	go func() { firstDone <- v.Flush(firstCtx, "fixture", true) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first build never reached provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := v.Flush(ctx, "fixture", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("concurrent build returned without compiling or waiting for cancellation: %v", err)
	}
	cancelFirst()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("first cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled build did not stop")
	}
	snapshot, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 1 {
		t.Fatalf("cancelled build lost source: %+v", snapshot)
	}
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": "source", "quote": "Cedar queues jobs."}}}}}
	})
	reopened := knowledge.Open(filepath.Dir(v.Directory()))
	if err = reopened.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("retry after reopening cancelled build: %v", err)
	}
	hits, err := reopened.Search("queues", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("retried saved source is not searchable: %+v %v", hits, err)
	}
}

type runtimeHistory struct{ journals []knowledge.Journal }

func (h runtimeHistory) Inventory(context.Context) ([]knowledge.Journal, error) {
	return h.journals, nil
}
func (h runtimeHistory) Read(_ context.Context, j knowledge.Journal, c knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	return knowledge.JournalBatch{Sources: []knowledge.Source{{ID: j.ID, Session: j.Session, Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}}, Cursor: knowledge.JournalCursor{Offset: j.Size}}, nil
}

func TestKnowledgeBuildIsolatesUnreadableChats(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": "good", "quote": "Cedar queues jobs."}}}}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- v.Run(ctx, runtimeHistory{journals: []knowledge.Journal{{ID: "bad", Error: "saved chat is unreadable"}, {ID: "good", Session: "chat", Size: 1}}}, func() string { return "fixture" })
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Build.Phase == knowledge.BuildReady {
			t.Fatalf("unreadable saved chat reported complete: %+v", snapshot.Build)
		}
		if snapshot.Build.Phase == knowledge.BuildPartial {
			hits, err := v.Search("queues", 5)
			if err != nil || len(hits) != 1 {
				t.Fatalf("healthy chat not searchable: %+v %v", hits, err)
			}
			if snapshot.Build.Problems != 1 {
				t.Fatalf("unreadable chat missing from status: %+v", snapshot.Build)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("healthy chat was not published with partial status")
}
