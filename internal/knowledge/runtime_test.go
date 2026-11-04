package knowledge_test

import (
	"context"
	"database/sql"
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
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func TestKnowledgeOrdinaryBuildRetriesFailedInputAutomatically(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		if payload["messages"] == nil {
			return map[string]any{"subjects": []any{}}
		}
		return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": "good", "quote": "Cedar queues jobs."}}}}}
	})
	respond := http.DefaultTransport
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			raw := fmt.Sprintf(`{"model":%q,"choices":[{"finish_reason":"length","message":{"content":"{"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.125}}`, knowledge.Model)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
		}
		return respond.RoundTrip(r)
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
			t.Logf("automatic retry recovered: paid calls=%d", attempts.Load())
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unchanged failed input did not retry automatically: attempts=%d notice=%q", attempts.Load(), failedMessage)
}

func TestKnowledgeOrdinaryBuildStopsAfterThreePaidFailures(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	failKnowledgeRequests(t, &calls)
	ctx, cancel := context.WithTimeout(context.Background(), 38*time.Second)
	defer cancel()
	if err := v.Run(ctx, runtimeHistory{journals: []knowledge.Journal{{ID: "good", Session: "chat", Size: 1}}}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	state, err := v.Snapshot()
	t.Logf("paid calls=%d; error=%q", calls.Load(), state.Build.Error)
	if err != nil || calls.Load() != 3 || state.Build.Phase != knowledge.BuildFailed || !strings.Contains(state.Build.Error, "use Build or add new sources") {
		t.Fatalf("unchanged failed input must stop after 3 paid calls: state=%+v calls=%d err=%v", state.Build, calls.Load(), err)
	}
}

func TestKnowledgeOrdinaryBuildRetryCountSurvivesRestart(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	captureRuntimeSource(t, v, "first")
	var calls atomic.Int32
	failKnowledgeRequests(t, &calls)
	for want := int32(1); want <= 3; want++ {
		runToKnowledgeFailure(t, knowledge.Open(profile), &calls, want, want == 3)
	}
	assertKnowledgeBuildIdle(t, knowledge.Open(profile), &calls, 3)
}

func TestKnowledgeOrdinaryBuildInterruptedFinalAttemptStopsAfterRestart(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	captureRuntimeSource(t, v, "first")
	var calls atomic.Int32
	failKnowledgeRequests(t, &calls)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 1, false)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 2, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failing := http.DefaultTransport
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		response, err := failing.RoundTrip(r)
		cancel()
		return response, err
	})
	if err := v.Run(ctx, runtimeHistory{}, func() string { return "fixture" }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertKnowledgeBuildIdle(t, knowledge.Open(profile), &calls, 3)
}

func TestKnowledgeOrdinaryBuildRetryCountRestarts(t *testing.T) {
	for _, action := range []string{"new source", "Build"} {
		t.Run(action, func(t *testing.T) {
			profile := t.TempDir()
			v := knowledge.Open(profile)
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			captureRuntimeSource(t, v, "first")
			var calls atomic.Int32
			failKnowledgeRequests(t, &calls)
			for want := int32(1); want <= 3; want++ {
				runToKnowledgeFailure(t, knowledge.Open(profile), &calls, want, want == 3)
			}
			if action == "Build" {
				if err := v.StartBuild(); err != nil {
					t.Fatal(err)
				}
			} else {
				captureRuntimeSource(t, v, "second")
			}
			for want := int32(4); want <= 6; want++ {
				runToKnowledgeFailure(t, knowledge.Open(profile), &calls, want, want == 6)
			}
			assertKnowledgeBuildIdle(t, knowledge.Open(profile), &calls, 6)
		})
	}
}

func TestKnowledgeOrdinaryBuildSuccessClearsRetryCount(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	// A successful 64-source batch must clear the two preceding failures,
	// allowing three attempts for the last source without changing the input.
	for i := 0; i < 65; i++ {
		captureRuntimeSource(t, v, fmt.Sprintf("source-%02d", i))
	}
	var calls atomic.Int32
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(*http.Request) (*http.Response, error) {
		finish, result := "length", "{"
		if calls.Add(1) == 3 {
			finish, result = "stop", `{"claims":[]}`
		}
		raw := fmt.Sprintf(`{"model":%q,"choices":[{"finish_reason":%q,"message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.125}}`, knowledge.Model, finish, result)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 1, false)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 2, false)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 4, false)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 5, false)
	runToKnowledgeFailure(t, knowledge.Open(profile), &calls, 6, true)
	assertKnowledgeBuildIdle(t, knowledge.Open(profile), &calls, 6)
	state, err := v.Snapshot()
	if err != nil || state.Pending != 1 {
		t.Fatalf("successful batch progress was lost: pending=%d err=%v", state.Pending, err)
	}
}

func TestKnowledgeOrdinaryBuildLoadsLegacyFailedInput(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	captureRuntimeSource(t, v, "first")
	// Seed an old database; all assertions use the worker and its public status.
	uri := sqliteuri.File(filepath.Join(v.Directory(), "knowledge.sqlite"))
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE state SET payload=json_set(payload,'$.failed_input','old-input') WHERE key='job'`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("legacy fixture: %v, %v", err, closeErr)
	}
	var calls atomic.Int32
	failKnowledgeRequests(t, &calls)
	for want := int32(1); want <= 3; want++ {
		runToKnowledgeFailure(t, knowledge.Open(profile), &calls, want, want == 3)
	}
	assertKnowledgeBuildIdle(t, knowledge.Open(profile), &calls, 3)
}

func captureRuntimeSource(t *testing.T, v *knowledge.Vault, id string) {
	t.Helper()
	if err := v.Capture(knowledge.Source{ID: id, Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
}

func runToKnowledgeFailure(t *testing.T, v *knowledge.Vault, calls *atomic.Int32, want int32, stopped bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx, runtimeHistory{}, func() string { return "fixture" }) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("worker cancellation: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	notice := "retrying automatically"
	if stopped {
		notice = "use Build or add new sources"
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		got := calls.Load()
		state, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		// Read the call count before the status so an old failure cannot satisfy a new attempt.
		if got == want && state.Build.Phase == knowledge.BuildFailed && strings.Contains(state.Build.Error, notice) {
			t.Logf("paid calls=%d; error=%q", want, state.Build.Error)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker did not persist failure at paid calls=%d (got %d)", want, calls.Load())
}

func assertKnowledgeBuildIdle(t *testing.T, v *knowledge.Vault, calls *atomic.Int32, want int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	if err := v.Run(ctx, runtimeHistory{}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	state, err := v.Snapshot()
	t.Logf("after restart and idle checks: paid calls=%d; error=%q", calls.Load(), state.Build.Error)
	if err != nil || calls.Load() != want || state.Build.Phase != knowledge.BuildFailed || !strings.Contains(state.Build.Error, "use Build or add new sources") {
		t.Fatalf("stopped build changed: state=%+v calls=%d want=%d err=%v", state.Build, calls.Load(), want, err)
	}
}

func failKnowledgeRequests(t *testing.T, calls *atomic.Int32) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = knowledgeTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		raw := fmt.Sprintf(`{"model":%q,"choices":[{"finish_reason":"length","message":{"content":"{"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.125}}`, knowledge.Model)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
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
			t.Logf("memory off: paid calls=%d", calls.Load())
			if err := v.Flush(context.Background(), "fixture", true); !errors.Is(err, knowledge.ErrDisabled) {
				t.Fatalf("explicit compilation while off: %v", err)
			}
			// No phase at all: with Memory off and nothing asked for, no pass looked.
			state, err := v.Snapshot()
			if err != nil || state.Enabled || state.Pending != 0 || len(state.Pages) != 0 || state.Build.Phase != "" || calls.Load() != 0 {
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
	t.Logf("Factory: paid calls=%d; owned receipts=%d; unowned=%d", calls.Load(), len(billed), unowned.Load())
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
