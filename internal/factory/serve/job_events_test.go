package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/guard"
)

func jobEventsHandler(t *testing.T, root string) *Handler {
	t.Helper()
	h := New(factory.Seam{}, Config{Root: root, AccessToken: "outcome-test-token", Mode: api.FactorySolo})
	t.Cleanup(h.Close)
	return h
}

func jobEventsRequest(h *Handler, query string) *httptest.ResponseRecorder {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/job-events"+query, nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer outcome-test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeJobEvents(t *testing.T, w *httptest.ResponseRecorder) api.JobEvents {
	t.Helper()
	var got api.JobEvents
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Events == nil {
		t.Fatalf("job events: %d %s", w.Code, w.Body.String())
	}
	return got
}

// E3, E5, E6: the HTTP cursor survives a handler restart; live ticks do not count.
func TestJobEventsReadCursorAndHandlerRestart(t *testing.T) {
	root := t.TempDir()
	h := jobEventsHandler(t, root)
	initial := decodeJobEvents(t, jobEventsRequest(h, ""))
	if !initial.Reset || initial.Cursor != 0 || len(initial.Events) != 0 {
		t.Fatalf("initial: %+v", initial)
	}
	zero := decodeJobEvents(t, jobEventsRequest(h, "?after=0"))
	if zero.Reset || zero.Cursor != 0 {
		t.Fatalf("zero: %+v", zero)
	}
	if err := live.Append(root, live.New(live.OutcomeKind, 1, live.Outcome{Job: 1, Kind: "finished"}), live.New("machine", 0, live.Payload{})); err != nil {
		t.Fatal(err)
	}
	h.Close()
	reopened := jobEventsHandler(t, root)
	got := decodeJobEvents(t, jobEventsRequest(reopened, "?after=0&limit=1"))
	if got.Reset || got.Cursor != 1 || got.More || len(got.Events) != 1 || got.Events[0].Seq != 1 {
		t.Fatalf("reopened: %+v", got)
	}
	got = decodeJobEvents(t, jobEventsRequest(reopened, "?after=1"))
	if got.Reset || got.Cursor != 1 || len(got.Events) != 0 {
		t.Fatalf("caught up: %+v", got)
	}
}

// E5, E14: malformed values are 400; a handler without a root resets with [].
func TestJobEventsQueryValidationAndEmptyRoot(t *testing.T) {
	h := jobEventsHandler(t, "")
	for _, query := range []string{"?after=-1", "?after=x", "?after=", "?after=0&after=1", "?limit=0", "?limit=501", "?limit=x", "?wait=-1", "?wait=16", "?wait=0.5", "?wait="} {
		w := jobEventsRequest(h, query)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
	}
	got := decodeJobEvents(t, jobEventsRequest(h, "?after=0&wait=15"))
	if !got.Reset || got.Cursor != 0 || got.More || len(got.Events) != 0 {
		t.Fatalf("empty root: %+v", got)
	}
}

// E7: repository visibility is identical to live events; hidden seqs are consumed.
func TestJobEventsVisibilityMovesPastHiddenJobs(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "priya")
	queueAs(t, h, token, false)
	for _, job := range []int{1, 2, 3} {
		if err := live.Append(h.eventRoot(), live.New(live.OutcomeKind, job, live.Outcome{Job: job, Kind: "finished"})); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/job-events?after=0&limit=2", nil)
	r.RemoteAddr = "192.0.2.1:9"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	got := decodeJobEvents(t, w)
	if got.Cursor != 2 || !got.More || len(got.Events) != 1 || got.Events[0].Job != 1 {
		t.Fatalf("visible outcomes: %+v", got)
	}
	r.URL.RawQuery = "after=2"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	got = decodeJobEvents(t, w)
	if got.Cursor != 3 || got.More || len(got.Events) != 1 || got.Events[0].Job != 3 {
		t.Fatalf("continued: %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	r.URL.RawQuery = "after=1&limit=1&wait=15"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	got = decodeJobEvents(t, w)
	if got.Cursor != 3 || got.More || len(got.Events) != 1 || got.Events[0].Job != 3 {
		t.Fatalf("long poll past hidden outcome: %+v", got)
	}
}

func waitForJobEventsJoin(t *testing.T, h *Handler) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	joined := func() bool {
		h.watch.hub.mu.Lock()
		defer h.watch.hub.mu.Unlock()
		return len(h.watch.hub.subs) != 0
	}
	for !joined() {
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("long poll did not join")
		}
	}
}

// E11: an empty read joins first, then returns promptly on the next outcome.
func TestJobEventsLongPollReturnsOnOutcome(t *testing.T) {
	root := t.TempDir()
	h := jobEventsHandler(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/job-events?after=0&wait=15", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer outcome-test-token")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	guard.Go("job outcome long poll test", func() {
		defer close(done)
		h.ServeHTTP(w, r)
	})
	waitForJobEventsJoin(t, h)
	if err := live.Append(root, live.New(live.OutcomeKind, 1, live.Outcome{Job: 1, Kind: "finished"})); err != nil {
		t.Fatal(err)
	}
	h.watch.hub.send("1")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("outcome did not release the long poll")
	}
	got := decodeJobEvents(t, w)
	if got.Reset || len(got.Events) != 1 || got.Cursor != 1 {
		t.Fatalf("long poll: %+v", got)
	}
}

// E11: existing events and resets are immediate, idle waits end at their bound.
func TestJobEventsLongPollImmediateAndTimeout(t *testing.T) {
	root := t.TempDir()
	h := jobEventsHandler(t, root)
	if err := live.Append(root, live.New(live.OutcomeKind, 1, live.Outcome{Job: 1, Kind: "finished"})); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"?wait=15", "?after=0&wait=15", "?after=99&wait=15"} {
		start := time.Now()
		got := decodeJobEvents(t, jobEventsRequest(h, query))
		if time.Since(start) > time.Second || (!got.Reset && len(got.Events) != 1) {
			t.Fatalf("immediate %s: %+v", query, got)
		}
	}
	start := time.Now()
	got := decodeJobEvents(t, jobEventsRequest(h, "?after=1&wait=1"))
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 2*time.Second || got.Reset || got.Cursor != 1 || len(got.Events) != 0 {
		t.Fatalf("idle: %+v after %v", got, elapsed)
	}
	withoutWatch := &Handler{config: Config{Root: root}}
	w := httptest.NewRecorder()
	start = time.Now()
	withoutWatch.jobEvents(w, httptest.NewRequest(http.MethodGet, "/v1/job-events?after=1&wait=15", nil))
	if got := decodeJobEvents(t, w); time.Since(start) > time.Second || got.Cursor != 1 {
		t.Fatalf("no watch: %+v", got)
	}
}
