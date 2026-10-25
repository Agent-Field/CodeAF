package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestIdleWorkerWaitsInsteadOfRepeatedlyPullingFinishedStore(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if _, err := st.Create(factory.Item{Title: "finished", State: factory.StateShipped}); err != nil {
			t.Fatal(err)
		}
	}
	var reads atomic.Int64
	cfg := Config{Version: "test"}
	cfg.Store, cfg.PullWait = st, 650*time.Millisecond
	// Each lease operation consults this clock under the desk lock. Counting
	// those calls observes repeated pulls without replacing the real store.
	cfg.LeaseOptions = lease.Options{Now: func() time.Time {
		reads.Add(1)
		return time.Now()
	}}
	h := New(factory.Seam{}, cfg)
	t.Cleanup(h.Close)
	worker := attachIdleWorker(t, h)
	reads.Store(0)
	req := httptest.NewRequest("GET", "http://localhost/v1/workers/pull", nil)
	req.Header.Set("Authorization", "Bearer "+worker.Credential)
	w := httptest.NewRecorder()
	began := time.Now()
	h.ServeHTTP(w, req)
	if time.Since(began) < cfg.PullWait {
		t.Fatal("empty pull returned before its deadline")
	}
	if w.Code != 204 {
		t.Fatalf("idle pull: %d %s", w.Code, w.Body.String())
	}
	// Arrival and departure read presence too, and an attach notification may
	// still arrive. None permits a ten-times-a-second idle pull loop.
	if got := reads.Load(); got > 12 {
		t.Fatalf("idle worker repeated store/lease reads: %d clock calls in 650 ms; want at most 12", got)
	}
}

func attachIdleWorker(t *testing.T, h *Handler) api.WorkerAttached {
	t.Helper()
	body, err := json.Marshal(api.WorkerAttach{Name: "idle", Handle: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	attached := httptest.NewRecorder()
	attach := httptest.NewRequest("POST", "http://localhost/v1/workers/attach", bytes.NewReader(body))
	attach.RemoteAddr = "127.0.0.1:1234"
	h.ServeHTTP(attached, attach)
	var worker api.WorkerAttached
	if err := json.Unmarshal(attached.Body.Bytes(), &worker); err != nil || attached.Code != 201 {
		t.Fatalf("attach: %d %s, %v", attached.Code, attached.Body.String(), err)
	}
	return worker
}

func TestWaitingWorkerPicksUpExternalEnqueueWithinOneSecond(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		name := "filesystem"
		if fallback {
			name = "fallback"
		}
		t.Run(name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := Config{Version: "test"}
			cfg.Store, cfg.PullWait = st, 5*time.Second
			h := New(factory.Seam{}, cfg)
			t.Cleanup(h.Close)
			if fallback && h.watch.fs != nil {
				if err := h.watch.fs.Close(); err != nil {
					t.Fatal(err)
				}
			}
			worker := attachIdleWorker(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("GET", "http://localhost/v1/workers/pull", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+worker.Credential)
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				h.ServeHTTP(w, req)
				close(done)
			}()
			defer func() { cancel(); <-done }()
			// Let the initial empty pull and attach notification finish before
			// enqueueing. This exercises an already waiting filesystem subscriber.
			select {
			case <-done:
				t.Fatal("empty worker did not wait")
			case <-time.After(250 * time.Millisecond):
			}
			other, err := store.Open(st.Root())
			if err != nil {
				t.Fatal(err)
			}
			began := time.Now()
			job, err := other.Create(factory.Item{Title: "arriving", State: factory.StateQueued, Assignee: "sam"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second - time.Since(began)):
				t.Fatal("waiting worker missed enqueue's one-second deadline")
			}
			var pulled api.WorkerPull
			if err := json.Unmarshal(w.Body.Bytes(), &pulled); err != nil || w.Code != 200 || pulled.Job.ID != job.ID {
				t.Fatalf("enqueue pickup: %d %s, %v; want job %d", w.Code, w.Body.String(), err, job.ID)
			}
			t.Logf("enqueue pickup: %s", time.Since(began))
		})
	}
}

func TestIdlePullReturnsBeforeSavedRunnerTimeout(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ChangeRunnerSettings(func(settings *store.RunnerSettings) error {
		settings.RunnerTimeoutSeconds = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: "test"}
	cfg.Store, cfg.PullWait = st, 5*time.Second
	h := New(factory.Seam{}, cfg)
	t.Cleanup(h.Close)
	worker := attachIdleWorker(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", "http://localhost/v1/workers/pull", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+worker.Credential)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 || ctx.Err() != nil {
		t.Fatalf("idle pull outlived its runner heartbeat: HTTP %d, %v", w.Code, ctx.Err())
	}
}

func TestWaitingWorkerUsesAShortenedRunnerTimeout(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: "test", Store: st, PullWait: 5 * time.Second}
	h := New(factory.Seam{}, cfg)
	t.Cleanup(h.Close)
	worker := attachIdleWorker(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "http://localhost/v1/workers/pull", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+worker.Credential)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case <-done:
		t.Fatal("empty worker did not wait")
	case <-time.After(250 * time.Millisecond):
	}
	if _, err := st.ChangeRunnerSettings(func(settings *store.RunnerSettings) error {
		settings.RunnerTimeoutSeconds = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("open pull ignored the shortened runner timeout")
	}
	if w.Code != 204 {
		t.Fatalf("shortened pull: HTTP %d", w.Code)
	}
}
