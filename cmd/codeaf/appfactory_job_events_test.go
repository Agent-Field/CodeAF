package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// E3, E10, E14: the real read announces support and keeps a Factory cursor across engines.
func TestAppFactoryJobEventsReadAndEngineRestart(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	if !strings.Contains(alice.w.hello, appwire.FeatureFactoryJobEvents) {
		t.Fatalf("hello: %s", alice.w.hello)
	}
	var caps appwire.FactoryCapabilities
	alice.w.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.JobEvents {
		t.Fatalf("capabilities: %+v", caps)
	}
	var initial appwire.FactoryEvents
	alice.w.must("factory.events_since", map[string]any{"factory": alice.conn.ID}, &initial)
	if !initial.Supported || !initial.Reset || initial.Events == nil {
		t.Fatalf("initial: %+v", initial)
	}
	for _, bad := range []map[string]any{{}, {"factory": alice.conn.ID, "after": -1}, {"factory": alice.conn.ID, "limit": 501}} {
		alice.refused(t, "factory.events_since", bad, appwire.CodeBadParams)
	}
	if err := alice.w.backend.Close(); err != nil {
		t.Fatal(err)
	}
	job := f.create(t, factory.Item{State: factory.StateRunning, Title: "Retained outcome"})
	at := f.clock.now()
	if err := f.st.Update(job.ID, func(it *factory.Item) error {
		it.State, it.DoneAt = factory.StateShipped, at
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bob := signedIn(t, f, 2, "bob")
	var got appwire.FactoryEvents
	bob.w.must("factory.events_since", map[string]any{"factory": bob.conn.ID, "after": initial.Cursor}, &got)
	if !got.Supported || got.Reset || len(got.Events) != 1 || got.Cursor != 1 {
		t.Fatalf("restarted read: %+v", got)
	}
	e := got.Events[0]
	if e.Factory != bob.conn.ID || e.Seq != 1 || e.Kind != "finished" || e.Job != job.ID || !e.At.Equal(at) || e.How != "answered" || e.Title != job.Title {
		t.Fatalf("event: %+v", e)
	}
}

func nextFactoryOutcome(t *testing.T, w *factoryWire) appwire.FactoryEvent {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw, ok := <-w.answers:
			if !ok {
				t.Fatal("wire closed before outcome")
			}
			var frame struct {
				Event string               `json:"event"`
				Data  appwire.FactoryEvent `json:"data"`
			}
			if json.Unmarshal(raw, &frame) == nil && frame.Event == appwire.EventFactoryEvent {
				return frame.Data
			}
		case <-timer.C:
			t.Fatal("no factory.event push")
		}
	}
}

func factoryOutcomePushes(w *factoryWire) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, line := range w.lines {
		var frame struct {
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(line), &frame) == nil && frame.Event == appwire.EventFactoryEvent {
			n++
		}
	}
	return n
}

func jobFollowerCount(s *appFactoryJobEvents) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.followers)
}

func waitForNoJobFollowers(t *testing.T, s *appFactoryJobEvents) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for jobFollowerCount(s) != 0 {
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("follower did not end")
		}
	}
}

// E8: two later outcomes arrive once in order; close cancels and joins the follower.
func TestAppFactoryJobEventsPushOnceAndClose(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	// The harness normally records backend events separately; send these to its wire.
	alice.w.backend.inbox.next = alice.w.server
	params := map[string]any{"factory": alice.conn.ID, "after": 0, "follow": true}
	var got appwire.FactoryEvents
	alice.w.must("factory.events_since", params, &got)
	alice.w.must("factory.events_since", params, &got)
	s := alice.w.backend.factoryHub().jobEventsState()
	if jobFollowerCount(s) != 1 {
		t.Fatal("repeated follow did not keep one follower")
	}
	jobs := []factory.Item{f.create(t, factory.Item{State: factory.StateRunning}), f.create(t, factory.Item{State: factory.StateRunning})}
	at := f.clock.now()
	for _, job := range jobs {
		if err := f.st.Update(job.ID, func(it *factory.Item) error {
			it.State, it.DoneAt = factory.StateShipped, at
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, job := range jobs {
		e := nextFactoryOutcome(t, alice.w)
		if e.Factory != alice.conn.ID || e.Seq != int64(i+1) || e.Kind != "finished" || e.Job != job.ID || !e.At.Equal(at) {
			t.Fatalf("push %d: %+v", i, e)
		}
	}
	if err := alice.w.backend.Close(); err != nil {
		t.Fatal(err)
	}
	if jobFollowerCount(s) != 0 {
		t.Fatal("close left a follower")
	}
	if err := f.st.Update(jobs[0].ID, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{State: factory.PRMerged, MergedAt: at, Number: 7}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if factoryOutcomePushes(alice.w) != 2 {
		t.Fatalf("pushes: %d, want 2", factoryOutcomePushes(alice.w))
	}
}

// E8: detaching is observed by the credentialed read and removes the follower.
func TestAppFactoryJobEventsDetachEndsFollower(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	var got appwire.FactoryEvents
	alice.w.must("factory.events_since", map[string]any{"factory": alice.conn.ID, "after": 0, "follow": true}, &got)
	s := alice.w.backend.factoryHub().jobEventsState()
	alice.w.must("factory.detach", map[string]any{"factory": alice.conn.ID}, nil)
	job := f.create(t, factory.Item{State: factory.StateRunning})
	if err := f.st.Update(job.ID, func(it *factory.Item) error { it.State = factory.StateShipped; return nil }); err != nil {
		t.Fatal(err)
	}
	waitForNoJobFollowers(t, s)
}

// E9: the older route's exact refusal becomes supported false, with no follower.
func TestAppFactoryJobEventsOlderFactory(t *testing.T) {
	f := startActionFactory(t)
	target, _ := url.Parse(f.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var reads atomic.Int64
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/job-events" {
			reads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"factory endpoint not found","reason":"not_found"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer old.Close()
	alice := signedInAt(t, f, old.URL, 1, "alice")
	var got appwire.FactoryEvents
	alice.w.must("factory.events_since", map[string]any{"factory": alice.conn.ID, "follow": true}, &got)
	if got.Supported || got.Factory != alice.conn.ID || got.Events == nil || len(got.Events) != 0 || reads.Load() != 1 {
		t.Fatalf("older Factory: %+v, reads %d", got, reads.Load())
	}
	h := alice.w.backend.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobEvents != nil {
		t.Fatal("unsupported read started a follower registry")
	}
}

// E8: resets are silent, repeated reads raise the cursor, retries cap and recover.
func TestAppFactoryJobEventsRetryAndReset(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/job-events" {
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer outcome-test-token" {
			t.Error("outcome read lost credentials")
		}
		n := requests.Add(1)
		if n <= 7 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"busy"}`))
			return
		}
		if n == 8 {
			_ = json.NewEncoder(w).Encode(api.JobEvents{Events: []api.JobEvent{}, Cursor: 9, Reset: true})
			return
		}
		if n == 9 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"busy"}`))
			return
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	h := testFactoryHub(t)
	h.loaded = true
	h.rows = []appFactoryRow{{ID: "f", URL: server.URL, Mode: api.FactoryTeam}}
	h.sessions["f"] = appFactorySession{token: "outcome-test-token", expires: time.Now().Add(time.Hour)}
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = h })
	t.Cleanup(func() { _ = b.Close() })
	s := h.jobEventsState()
	delays := make(chan time.Duration, 12)
	s.after = func(delay time.Duration) <-chan time.Time {
		delays <- delay
		ready := make(chan time.Time, 1)
		ready <- time.Now()
		return ready
	}
	s.follow(b, h, "f", 0)
	want := []time.Duration{2, 4, 8, 16, 32, 60, 60, 2}
	for _, seconds := range want {
		select {
		case got := <-delays:
			if got != seconds*time.Second {
				t.Fatalf("backoff %v, want %v seconds", got, seconds)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("retry did not continue")
		}
	}
	f := func() *appFactoryJobFollower {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.followers["f"]
	}()
	s.follow(b, h, "f", 12)
	s.follow(b, h, "f", 10)
	if f == nil || s.cursor(f) != 12 || jobFollowerCount(s) != 1 {
		t.Fatal("follow did not raise the single cursor")
	}
	rec := b.inbox.next.(*appRecorder)
	rec.mu.Lock()
	for _, event := range rec.events {
		if event == appwire.EventFactoryEvent {
			t.Error("reset emitted an outcome")
		}
	}
	rec.mu.Unlock()
	h.stopJobEvents()
	if jobFollowerCount(s) != 0 {
		t.Fatal("retry follower remained after cancellation")
	}
}

// E8: a cancelled backend context and a detached connection stop, other errors retry.
func TestAppFactoryJobEventsEndingErrors(t *testing.T) {
	for _, code := range []string{appwire.CodeNotFound, appwire.CodeClosed} {
		if !jobEventsEnded(context.Background(), appwire.FactoryEvents{}, appwire.Errorf(code, "ended")) {
			t.Fatalf("%s did not end following", code)
		}
	}
	for _, code := range []string{appwire.CodeUnreachable, appwire.CodeSignedOut, appwire.CodeGitHubExpired, appwire.CodeBusy, appwire.CodeEngine} {
		if jobEventsEnded(context.Background(), appwire.FactoryEvents{}, appwire.Errorf(code, "retry")) {
			t.Fatalf("%s ended following", code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !jobEventsEnded(ctx, appwire.FactoryEvents{Supported: true}, nil) || !jobEventsEnded(context.Background(), appwire.FactoryEvents{Supported: false}, nil) {
		t.Fatal("cancelled or unsupported follower did not end")
	}
}

type blockedJobOutcomeEmitter struct {
	started chan struct{}
}

func (*blockedJobOutcomeEmitter) Emit(string, any) {
	panic("job outcome used the uncancellable emitter")
}

func (e *blockedJobOutcomeEmitter) EmitContext(ctx context.Context, _ string, _ any) {
	close(e.started)
	<-ctx.Done()
}

// E8: close releases a follower blocked on the wire before joining it.
func TestAppFactoryJobEventsCloseCancelsBlockedDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.JobEvents{Events: []api.JobEvent{{Seq: 1, Kind: "finished", Job: 1}}, Cursor: 1})
	}))
	t.Cleanup(server.Close)
	h := testFactoryHub(t)
	h.loaded = true
	h.rows = []appFactoryRow{{ID: "f", URL: server.URL, Mode: api.FactoryTeam}}
	h.sessions["f"] = appFactorySession{token: "outcome-test-token", expires: time.Now().Add(time.Hour)}
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = h })
	t.Cleanup(func() { _ = b.Close() })
	emitter := &blockedJobOutcomeEmitter{started: make(chan struct{})}
	b.inbox.next = emitter
	s := h.jobEventsState()
	s.follow(b, h, "f", 0)
	select {
	case <-emitter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery did not reach the wire")
	}
	done := make(chan struct{})
	guard.Go("job outcome close test", func() {
		defer close(done)
		h.close()
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not cancel blocked delivery")
	}
	if jobFollowerCount(s) != 0 {
		t.Fatal("blocked follower was not joined")
	}
}

// E8: the registry admits no more followers than attached Factory connections.
func TestAppFactoryJobEventsFollowerBound(t *testing.T) {
	h := testFactoryHub(t)
	s := h.jobEventsState()
	for i := 0; i < appwire.MaxFactoryConnections; i++ {
		s.followers[fmt.Sprint(i)] = &appFactoryJobFollower{}
	}
	s.follow(nil, h, "overflow", 0)
	if jobFollowerCount(s) != appwire.MaxFactoryConnections {
		t.Fatal("follower bound was exceeded")
	}
	s.cancel()
}

// E14: each call is one credentialed GET; all outcome fields keep their values.
func TestAppFactoryJobEventsQueryAndMapping(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	event := api.JobEvent{
		Seq: 7, Kind: "merged", Job: 42, At: at, Title: "A job", Group: "batch",
		How: "answered", Reason: "question", Need: "question", Code: "check", Step: "verify",
		PRNumber: 9, PRURL: "https://github.com/acme/api/pull/9",
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/v1/job-events" || r.Header.Get("Authorization") != "Bearer outcome-test-token" || q.Has("wait") {
			t.Errorf("read: %s, auth %q", r.URL.String(), r.Header.Get("Authorization"))
		}
		if n == 1 && (q.Has("after") || q.Get("limit") != "100") {
			t.Errorf("initial query: %v", q)
		}
		if n == 2 && (!q.Has("after") || q.Get("after") != "0" || q.Get("limit") != "2") {
			t.Errorf("zero query: %v", q)
		}
		_ = json.NewEncoder(w).Encode(api.JobEvents{Events: []api.JobEvent{event}, Cursor: 9, More: true})
	}))
	t.Cleanup(server.Close)
	h := testFactoryHub(t)
	h.loaded = true
	h.rows = []appFactoryRow{{ID: "f", URL: server.URL, Mode: api.FactoryTeam}}
	h.sessions["f"] = appFactorySession{token: "outcome-test-token", expires: time.Now().Add(time.Hour)}
	w := newFactoryWire(t, h)
	for _, params := range []map[string]any{{"factory": "f"}, {"factory": "f", "after": 0, "limit": 2}} {
		var got appwire.FactoryEvents
		w.must("factory.events_since", params, &got)
		if !got.Supported || got.Factory != "f" || got.Cursor != 9 || got.Reset || !got.More || len(got.Events) != 1 || got.Events[0].Factory != "f" {
			t.Fatalf("answer: %+v", got)
		}
		actual, _ := json.Marshal(got.Events[0])
		var mapped map[string]any
		_ = json.Unmarshal(actual, &mapped)
		delete(mapped, "factory")
		actual, _ = json.Marshal(mapped)
		want, _ := json.Marshal(event)
		var normalized map[string]any
		_ = json.Unmarshal(want, &normalized)
		want, _ = json.Marshal(normalized)
		if string(actual) != string(want) {
			t.Fatalf("mapped %s, want %s", actual, want)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("reads: %d", calls.Load())
	}
}
