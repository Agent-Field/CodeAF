package afield

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type capture struct {
	mu       sync.Mutex
	requests []capturedRequest
}

type capturedRequest struct {
	path    string
	headers http.Header
	body    map[string]any
}

func (c *capture) handler(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		c.mu.Lock()
		c.requests = append(c.requests, capturedRequest{
			path: r.URL.Path, headers: r.Header.Clone(), body: body,
		})
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

func (c *capture) snapshot() []capturedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedRequest(nil), c.requests...)
}

func TestReporterPostsInOrder(t *testing.T) {
	c := &capture{}
	server := httptest.NewServer(c.handler(0))
	defer server.Close()
	r := NewReporter(Options{BaseURL: server.URL, RunID: "run-1", NodeID: "node-1"})
	for i := 0; i < 20; i++ {
		r.Post(Event{
			ExecutionID: "ex-" + string(rune('a'+i)), Reasoner: "step",
			Status: "running", Input: map[string]any{"seq": i},
		})
	}
	r.Close()
	got := c.snapshot()
	if len(got) != 20 {
		t.Fatalf("expected 20 requests, got %d", len(got))
	}
	for i, req := range got {
		if req.path != "/api/v1/workflow/executions/events" {
			t.Fatalf("path = %q", req.path)
		}
		if seq := req.body["input_data"].(map[string]any)["seq"].(float64); int(seq) != i {
			t.Fatalf("request %d has seq %v — order not preserved", i, seq)
		}
		if req.body["run_id"] != "run-1" || req.body["agent_node_id"] != "node-1" {
			t.Fatalf("request %d missing run/node ids: %v", i, req.body)
		}
	}
}

func TestReporterNeverBlocksCaller(t *testing.T) {
	c := &capture{}
	server := httptest.NewServer(c.handler(300 * time.Millisecond))
	defer server.Close()
	r := NewReporter(Options{BaseURL: server.URL, RunID: "run-2", NodeID: "n"})
	defer r.Close()
	start := time.Now()
	for i := 0; i < 5; i++ {
		r.Post(Event{ExecutionID: "e", Reasoner: "x", Status: "running"})
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Post blocked the caller for %s", elapsed)
	}
}

func TestReporterNoteEndpointAndHeaders(t *testing.T) {
	c := &capture{}
	server := httptest.NewServer(c.handler(0))
	defer server.Close()
	r := NewReporter(Options{BaseURL: server.URL, RunID: "run-3", NodeID: "swe-pro-go", Token: "tok"})
	r.NoteExec("exec-9", "planner produced 3 tasks", "info")
	r.Close()
	got := c.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	req := got[0]
	if req.path != "/api/ui/v1/executions/note" {
		t.Fatalf("path = %q", req.path)
	}
	if req.headers.Get("X-Run-ID") != "run-3" || req.headers.Get("X-Execution-ID") != "exec-9" ||
		req.headers.Get("X-Agent-Node-ID") != "swe-pro-go" ||
		req.headers.Get("Authorization") != "Bearer tok" {
		t.Fatalf("headers wrong: %v", req.headers)
	}
	if req.body["message"] != "planner produced 3 tasks" {
		t.Fatalf("body = %v", req.body)
	}
}

func TestReporterOverflowDropsWithoutDeadlock(t *testing.T) {
	c := &capture{}
	server := httptest.NewServer(c.handler(50 * time.Millisecond))
	defer server.Close()
	r := NewReporter(Options{
		BaseURL: server.URL, RunID: "run-4", NodeID: "n", QueueSize: 2,
		Logf: func(string, ...any) {},
	})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			r.Post(Event{ExecutionID: "e", Reasoner: "x", Status: "running"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("posting deadlocked on a full queue")
	}
	r.Close()
	if r.Dropped() == 0 {
		t.Fatal("expected drops with QueueSize=2 and a slow server")
	}
}

func TestReporterSurvivesDeadControlPlane(t *testing.T) {
	r := NewReporter(Options{
		BaseURL: "http://127.0.0.1:1", RunID: "run-5", NodeID: "n",
		Logf: func(string, ...any) {},
	})
	r.Post(Event{ExecutionID: "e", Reasoner: "x", Status: "running"})
	r.Note("still fine")
	r.Close() // must not hang or panic
}
