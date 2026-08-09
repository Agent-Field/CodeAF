package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/afield"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type cpEventCapture struct {
	mu     sync.Mutex
	events []map[string]any
}

func (capture *cpEventCapture) handler(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/health" {
		response.WriteHeader(http.StatusOK)
		return
	}
	if request.URL.Path != "/api/v1/workflow/executions/events" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	capture.mu.Lock()
	capture.events = append(capture.events, body)
	capture.mu.Unlock()
	response.WriteHeader(http.StatusNoContent)
}

func (capture *cpEventCapture) snapshot() []map[string]any {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	out := make([]map[string]any, len(capture.events))
	copy(out, capture.events)
	return out
}

func newCPTestServer(t *testing.T, handler http.Handler) (string, *http.Client) {
	t.Helper()
	var server *httptest.Server
	func() {
		// Some restricted sandboxes prohibit listeners entirely. The fallback
		// below still drives the identical handler through net/http.
		defer func() { _ = recover() }()
		server = httptest.NewServer(handler)
	}()
	if server != nil {
		t.Cleanup(server.Close)
		return server.URL, server.Client()
	}
	client := &http.Client{Transport: roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	return "http://control-plane", client
}

func TestCPBridgeProjectsStageDAG(t *testing.T) {
	capture := &cpEventCapture{}
	baseURL, client := newCPTestServer(t, http.HandlerFunc(capture.handler))

	reporter := afield.NewReporter(afield.Options{
		BaseURL: baseURL,
		RunID:   "run-42",
		NodeID:  "test-node",
		Client:  client,
	})
	defer reporter.Close()

	var output bytes.Buffer
	writer := newEventWriter(&output)
	bridge := newCPBridge(
		reporter, "parent-exec", "Implement bridge visibility cleanly",
	)
	writer.setHook(bridge.consume)

	script := []event{
		{
			Type: "stage", Stage: "bootstrap", Status: "ready",
			Data: map[string]any{"workspace": "/repo"}, Timestamp: 1000,
		},
		{
			Type: "stage", Stage: "classifier", Status: "running",
			Timestamp: 1010,
		},
		{
			Type: "stage", Stage: "classifier", Status: "completed",
			Data: map[string]any{"reason": "clear"}, Timestamp: 1020,
		},
		{
			Type: "stage", Stage: "planner", Status: "running",
			Timestamp: 1030,
		},
		{
			Type: "stage", Stage: "planner", Status: "translated",
			Data: map[string]any{"tasks": 3}, Timestamp: 1040,
		},
		{
			Type: "stage", Stage: "scheduler", Status: "quiet-wait",
			Data: map[string]any{"cycle": 1}, Timestamp: 1050,
		},
		{
			Type: "stage", Stage: "scheduler", Status: "cycle",
			Data: map[string]any{"cycle": 1}, Timestamp: 1060,
		},
		{
			Type: "stage", Stage: "scheduler", Status: "cycle-complete",
			Data: map[string]any{
				"cycle": 1, "dispatched": 2, "failed": 0,
			},
			Timestamp: 1070,
		},
		{
			Type: "stage", Stage: "audit", Status: "running",
			Data: map[string]any{"cycle": 1}, Timestamp: 1080,
		},
		{
			Type: "stage", Stage: "audit", Status: "pass",
			Data: map[string]any{"cycle": 1}, Timestamp: 1090,
		},
		{
			Type: "terminal", Status: "pass",
			Data:      map[string]any{"cost_usd": 1.25, "cycle": 1},
			Timestamp: 1100,
		},
	}
	for _, value := range script {
		writer.emit(value)
	}
	reporter.Flush(3 * time.Second)

	events := capture.snapshot()
	byExecution := map[string][]map[string]any{}
	for _, value := range events {
		executionID, _ := value["execution_id"].(string)
		byExecution[executionID] = append(byExecution[executionID], value)
	}

	assertReasoner := func(executionID, want string) {
		t.Helper()
		values := byExecution[executionID]
		if len(values) == 0 {
			t.Fatalf("missing execution %q", executionID)
		}
		if got := values[0]["reasoner_id"]; got != want {
			t.Errorf("%s reasoner = %v, want %q", executionID, got, want)
		}
	}
	assertPair := func(executionID string) {
		t.Helper()
		values := byExecution[executionID]
		if len(values) != 2 {
			t.Fatalf("%s posts = %d, want 2", executionID, len(values))
		}
		if got := values[0]["status"]; got != "running" {
			t.Errorf("%s open status = %v, want running", executionID, got)
		}
		if got := values[1]["status"]; got != "succeeded" {
			t.Errorf("%s close status = %v, want succeeded", executionID, got)
		}
		if got := values[0]["parent_execution_id"]; got != "run-42-root" {
			t.Errorf("%s parent = %v, want run-42-root", executionID, got)
		}
	}

	assertReasoner("run-42-classifier-1", "classify-goal")
	assertReasoner("run-42-planner-1", "plan-tasks")
	assertReasoner("run-42-audit-1", "audit-1")
	assertPair("run-42-classifier-1")
	assertPair("run-42-planner-1")
	assertPair("run-42-scheduler-1")
	assertPair("run-42-audit-1")

	if len(byExecution) != 6 {
		t.Fatalf("execution nodes = %d, want 6 (quiet-wait must add none)", len(byExecution))
	}
	root := byExecution["run-42-root"]
	if len(root) != 2 {
		t.Fatalf("root posts = %d, want running and terminal", len(root))
	}
	if got := root[0]["parent_execution_id"]; got != "parent-exec" {
		t.Errorf("root parent = %v, want parent-exec", got)
	}
	if got := root[1]["status"]; got != "succeeded" {
		t.Errorf("root terminal status = %v, want succeeded", got)
	}
	result, _ := root[1]["result"].(map[string]any)
	if result["status"] != "pass" || result["cost_usd"] != 1.25 {
		t.Errorf("root terminal result = %#v", result)
	}
}

func TestEventWriterHookPreservesNDJSON(t *testing.T) {
	values := []event{
		{
			Type: "stage", Stage: "classifier", Status: "running",
			Data: map[string]any{"cycle": 1}, Timestamp: 1234,
		},
		{
			Type: "terminal", Status: "pass",
			Data: map[string]any{"cost_usd": 0.5}, Timestamp: 1235,
		},
	}

	var withoutHook bytes.Buffer
	plain := newEventWriter(&withoutHook)
	for _, value := range values {
		plain.emit(value)
	}

	var withHook bytes.Buffer
	hooked := newEventWriter(&withHook)
	observed := []event{}
	hooked.setHook(func(value event) {
		observed = append(observed, value)
	})
	for _, value := range values {
		hooked.emit(value)
	}

	if !bytes.Equal(withoutHook.Bytes(), withHook.Bytes()) {
		t.Fatalf(
			"hook changed NDJSON\nwithout: %s\nwith:    %s",
			withoutHook.Bytes(), withHook.Bytes(),
		)
	}
	if !reflect.DeepEqual(observed, values) {
		t.Fatalf("hook events = %#v, want %#v", observed, values)
	}

	var timestamped bytes.Buffer
	timestampWriter := newEventWriter(&timestamped)
	var final event
	timestampWriter.setHook(func(value event) { final = value })
	timestampWriter.emit(event{Type: "notice"})
	if final.Timestamp == 0 {
		t.Fatal("hook received event before timestamp was filled")
	}
}
