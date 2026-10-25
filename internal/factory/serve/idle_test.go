package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

// TestIdleRequestsDoNotRereadTheFloor is the request half of an idle
// engine: opening the factory asks which deadlines are due, and asking
// again while nothing on the floor changed must not decode every job
// a second time. A later edit is a new look.
func TestIdleRequestsDoNotRereadTheFloor(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var first factory.Item
	for range 8 {
		created, err := st.Create(factory.Item{Title: "finished", State: factory.StateShipped})
		if err != nil {
			t.Fatal(err)
		}
		first = created
	}
	h := New(factory.Seam{}, Config{Version: "test", Store: st})
	t.Cleanup(h.Close)
	var lists atomic.Int32
	h.floorList = func(st *store.Store) ([]factory.Item, error) {
		lists.Add(1)
		return st.List()
	}
	get := func() {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/about", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("about: %d %s", w.Code, w.Body.String())
		}
	}
	get()
	get()
	if got := lists.Load(); got != 1 {
		t.Fatalf("two idle reads decoded the floor %d times, want 1", got)
	}
	if err := st.Update(first.ID, func(it *factory.Item) error {
		it.Title = "edited"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	get()
	if got := lists.Load(); got != 2 {
		t.Fatalf("an edited job was not read again: %d floor reads, want 2", got)
	}
}

// TestAChangeDuringTheIdleLookIsNotFrozen keeps the idle skip from
// remembering a look that was already stale. The signature is taken
// before the decode and again after it. A job that changes in between
// must be read on the next request, not treated as still idle.
func TestAChangeDuringTheIdleLookIsNotFrozen(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.Create(factory.Item{Title: "finished", State: factory.StateShipped})
	if err != nil {
		t.Fatal(err)
	}
	h := New(factory.Seam{}, Config{Version: "test", Store: st})
	t.Cleanup(h.Close)
	var lists atomic.Int32
	h.floorList = func(st *store.Store) ([]factory.Item, error) {
		if lists.Add(1) == 1 {
			if err := st.Update(created.ID, func(it *factory.Item) error {
				it.Title = "changed while reading"
				return nil
			}); err != nil {
				return nil, err
			}
		}
		return st.List()
	}
	get := func() {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/about", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("about: %d %s", w.Code, w.Body.String())
		}
	}
	get()
	get()
	if got := lists.Load(); got != 2 {
		t.Fatalf("a change during the look was frozen: %d floor reads, want 2", got)
	}
}

// TestADueApprovalIsReadAgainWhenItsUndoArrives keeps the idle skip from
// swallowing a deadline. The floor is left alone while the undo is still
// ahead, and decoded again once that moment has passed.
func TestADueApprovalIsReadAgainWhenItsUndoArrives(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.Create(factory.Item{Title: "waiting", State: factory.StateLanded})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC)
	undo := now.Add(30 * time.Second)
	if err := st.Update(created.ID, func(it *factory.Item) error {
		it.Approval = &factory.Approval{State: "approving", Kind: "proof", DecidedBy: "sam", At: now, UndoUntil: undo}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := New(factory.Seam{}, Config{Version: "test", Store: st, Now: func() time.Time { return now }})
	t.Cleanup(h.Close)
	var lists atomic.Int32
	h.floorList = func(st *store.Store) ([]factory.Item, error) {
		lists.Add(1)
		return st.List()
	}
	get := func() {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/about", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		h.ServeHTTP(w, r)
	}
	get()
	get()
	if got := lists.Load(); got != 1 {
		t.Fatalf("reads before the undo: %d, want 1", got)
	}
	now = now.Add(time.Minute)
	get()
	// The deadline is due, so this request reads the floor, then reads it
	// once more under the consent lock before it acts. A skipped deadline
	// would still be 1.
	if got := lists.Load(); got != 3 {
		t.Fatalf("the undo did not cause another read: %d, want 3", got)
	}
}

// TestIdleHeartbeatsDoNotRewriteTheRunnerFile is why an idle engine stays
// quiet. Each presence request moves the heartbeat in memory. Writing that
// to disk on every request wakes every waiting pull, which then re-reads
// the floor. A second request inside the save interval must leave the
// file alone; the first one still persists, and a request after the
// interval persists again.
func TestIdleHeartbeatsDoNotRewriteTheRunnerFile(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC)
	h := New(factory.Seam{}, Config{
		Version: "test", Store: st,
		LeaseOptions: lease.Options{Now: func() time.Time { return now }},
	})
	t.Cleanup(h.Close)
	worker := attachIdleWorker(t, h)
	path := filepath.Join(st.Root(), "runners.json")
	read := func() []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	beat := func() {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/runner/self", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+worker.Credential)
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("runner self: %d %s", w.Code, w.Body.String())
		}
	}
	attached := read()
	now = now.Add(time.Second)
	beat()
	first := read()
	if bytes.Equal(attached, first) {
		t.Fatal("the first heartbeat did not persist")
	}
	now = now.Add(time.Second)
	beat()
	if second := read(); !bytes.Equal(first, second) {
		t.Fatal("a second heartbeat rewrote the runner file")
	}
	now = now.Add(presenceSaveEvery)
	beat()
	if later := read(); bytes.Equal(first, later) {
		t.Fatal("a heartbeat after the save interval did not persist")
	}
}

// TestAnIdleDirectoryIsNotRereadOnTheFallback is the quiet floor. The
// fallback tick used to read every job file twice a second even while
// filesystem notifications were already watching. A quiet directory must
// stay unread; a job written the way the store writes one (a dotfile
// renamed into place) must still be announced.
func TestAnIdleDirectoryIsNotRereadOnTheFallback(t *testing.T) {
	root := t.TempDir()
	for i := range 20 {
		if err := os.WriteFile(filepath.Join(root, strconv.Itoa(i+1)+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := watchRoot(root)
	t.Cleanup(w.stop)
	ch := w.hub.join()
	t.Cleanup(func() { w.hub.leave(ch) })
	time.Sleep(1200 * time.Millisecond)
	if got := w.looks.Load(); got != 0 {
		t.Fatalf("a quiet directory was reread %d times", got)
	}
	if err := os.WriteFile(filepath.Join(root, ".99.json"), []byte(`{"n":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, ".99.json"), filepath.Join(root, "99.json")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got != "99" && got != "*" {
			t.Fatalf("announced %q, want the new job", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a new job file was not announced")
	}
	if w.looks.Load() == 0 {
		t.Fatal("a new job file was not reread")
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
