package knowledge_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestKnowledgeOrdinaryBuildRetriesFailedInputAutomatically(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	v = v.WithCalls(func(ctx context.Context) (context.Context, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("temporary compiler failure")
		}
		return ctx, nil
	})
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": "good", "quote": "Cedar queues jobs."}}}}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- v.Run(ctx, runtimeHistory{journals: []knowledge.Journal{{ID: "good", Session: "chat", Size: 1}}}, func() string { return "fixture" })
	}()
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("worker cancellation: %v", err)
		}
	}()
	deadline := time.Now().Add(9 * time.Second)
	failedMessage := ""
	for time.Now().Before(deadline) {
		state, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if state.Build.Phase == knowledge.BuildFailed {
			failedMessage = state.Build.Error
		}
		if state.Build.Phase == knowledge.BuildReady {
			if !strings.Contains(failedMessage, "Completed progress is retained; retrying automatically.") {
				t.Errorf("automatic retry notice: %q", failedMessage)
			}
			hits, err := v.Search("queues", 5)
			if err != nil || len(hits) != 1 || state.Pending != 0 {
				t.Fatalf("retried source was not published: hits=%+v pending=%d err=%v", hits, state.Pending, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unchanged failed input did not retry automatically: attempts=%d notice=%q", attempts.Load(), failedMessage)
}

func TestKnowledgeMemoryOffDoesNotBuild(t *testing.T) {
	for _, journals := range [][]knowledge.Journal{nil, {{ID: "saved", Session: "chat", Size: 1}}} {
		t.Run(fmt.Sprintf("journals=%d", len(journals)), func(t *testing.T) {
			profile := t.TempDir()
			if err := config.WriteMemoryEnabled(profile, false); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			v := knowledge.Open(profile).WithCalls(func(ctx context.Context) (context.Context, error) {
				calls.Add(1)
				return nil, errors.New("memory off must not compile")
			})
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = knowledgeTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("memory off must not call a provider")
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
			defer cancel()
			if err := v.Run(ctx, runtimeHistory{journals: journals}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if err := v.Flush(context.Background(), "fixture", true); !errors.Is(err, knowledge.ErrDisabled) {
				t.Fatalf("explicit compilation while off: %v", err)
			}
			state, err := v.Snapshot()
			if err != nil || state.Enabled || state.Pending != 0 || len(state.Pages) != 0 || state.Build.Phase != knowledge.BuildUnbuilt || calls.Load() != 0 {
				t.Fatalf("memory off built or opted in: state=%+v calls=%d err=%v", state, calls.Load(), err)
			}
		})
	}
}

func TestKnowledgeFactoryBuildRetainsOwnedFailureAndAccounting(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"stage", "other-job"} {
		if err := v.Capture(knowledge.Source{ID: session, Session: session, Job: true, Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	var billed []provider.Billed
	var unowned atomic.Int32
	knowledge.SetUsageRecorder(func(context.Context, provider.Billed) { unowned.Add(1) })
	t.Cleanup(func() { knowledge.SetUsageRecorder(nil) })
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		if provider.SessionFrom(r.Context()) != "stage" || provider.BillingSinkFrom(r.Context()) == nil {
			return nil, errors.New("Factory call lost its owner or billing sink")
		}
		result := `{"subjects":[]}`
		finish := "stop"
		switch calls.Add(1) {
		case 1:
			finish, result = "length", `{`
		case 2:
			result = `{"claims":[{"local_id":"c","text":"Cedar queues jobs.","topic":"Queues","scope":"Cedar","kind":"fact","evidence":[{"source":"stage","quote":"Cedar queues jobs."}]}]}`
		}
		raw := fmt.Sprintf(`{"model":%q,"choices":[{"finish_reason":%q,"message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.125}}`, knowledge.Model, finish, result)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	owned := v.WithCalls(func(ctx context.Context) (context.Context, error) {
		ctx = provider.WithSession(ctx, "stage")
		return provider.WithBilling(ctx, func(b provider.Billed) { billed = append(billed, b) }), nil
	})
	ctx := knowledge.WithOwnerSessions(context.Background(), []string{"stage"})
	if err := owned.Flush(ctx, "fixture", true); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("Factory failure must return to its caller: %v", err)
	}
	state, err := v.Snapshot()
	if err != nil || state.Pending != 2 || len(state.Pages) != 0 {
		t.Fatalf("failed Factory build lost pending sources: %+v %v", state, err)
	}
	background, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	if err := v.Run(background, runtimeHistory{}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("ordinary worker retried Factory work: calls=%d", calls.Load())
	}
	if err := owned.Flush(ctx, "fixture", true); err != nil {
		t.Fatalf("owner could not retry failed compilation: %v", err)
	}
	state, err = v.Snapshot()
	hits, searchErr := v.Search("queues", 5)
	if err != nil || searchErr != nil || state.Pending != 1 || len(hits) != 1 || len(hits[0].Page.Evidence) != 1 || hits[0].Page.Evidence[0].Source != "stage" {
		t.Fatalf("Factory retry crossed job ownership: state=%+v hits=%+v errors=%v, %v", state, hits, err, searchErr)
	}
	if len(billed) != int(calls.Load()) || len(billed) != 3 || unowned.Load() != 0 {
		t.Fatalf("Factory attempts lost accounting: billed=%+v calls=%d unowned=%d", billed, calls.Load(), unowned.Load())
	}
	for _, bill := range billed {
		if bill.Cost != 0.125 || bill.PromptTokens != 10 || bill.CompletionTokens != 2 {
			t.Fatalf("Factory receipt changed: %+v", bill)
		}
	}
}

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
	statusDone := make(chan error, 1)
	go func() {
		state, err := v.Status()
		if err == nil && (state.Pending != 1 || state.Build.Sources != 1) {
			err = fmt.Errorf("progress while provider is blocked: %+v", state)
		}
		statusDone <- err
	}()
	select {
	case err := <-statusDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("progress read blocked behind the model request")
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
