package afield

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one DAG node upsert on the control plane. Reasoner is the node's
// display name and is immutable after the first post for an ExecutionID;
// Input/Result may be enriched by later posts until Status goes terminal.
type Event struct {
	ExecutionID string
	Reasoner    string
	Status      string // "running", "succeeded", "failed"
	Parent      string
	Input       map[string]any
	Result      map[string]any
	Error       string
	DurationMS  *int64
}

// Options configures a Reporter. BaseURL is the control-plane root
// (e.g. http://localhost:8080); RunID groups all events into one trace.
type Options struct {
	BaseURL   string
	RunID     string
	NodeID    string
	Token     string
	QueueSize int                  // default 512
	Logf      func(string, ...any) // default stderr
	Client    *http.Client         // default 5s timeout
}

// Reporter mirrors run progress onto an AgentField control plane. All
// delivery is asynchronous through a single ordered queue: Post and Note
// never block the caller and a dead control plane never fails a run.
type Reporter struct {
	base    string
	runID   string
	nodeID  string
	token   string
	logf    func(string, ...any)
	client  *http.Client
	queue   chan message
	done    chan struct{}
	dropped atomic.Int64
	warn    sync.Once

	mu     sync.RWMutex // guards closed vs. sends on queue
	closed bool
}

type message struct {
	body    []byte
	url     string
	headers map[string]string
	ack     chan struct{} // non-nil for flush sentinels
}

// NewReporter builds a reporter and starts its sender goroutine. It does not
// probe the control plane; call Probe when the caller wants fail-fast setup.
func NewReporter(opt Options) *Reporter {
	if opt.QueueSize <= 0 {
		opt.QueueSize = 512
	}
	if opt.Logf == nil {
		opt.Logf = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 5 * time.Second}
	}
	r := &Reporter{
		base:   strings.TrimSuffix(opt.BaseURL, "/"),
		runID:  opt.RunID,
		nodeID: opt.NodeID,
		token:  opt.Token,
		logf:   opt.Logf,
		client: opt.Client,
		queue:  make(chan message, opt.QueueSize),
		done:   make(chan struct{}),
	}
	go r.sender()
	return r
}

// Probe checks the control plane's health endpoint synchronously so callers
// can disable reporting up front on a bad URL.
func (r *Reporter) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("control plane health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// RunID returns the trace id shared by every event this reporter posts.
func (r *Reporter) RunID() string { return r.runID }

// Post enqueues one DAG event. It never blocks: when the queue is full the
// event is dropped and counted.
func (r *Reporter) Post(ev Event) {
	body := map[string]any{
		"execution_id":  ev.ExecutionID,
		"workflow_id":   r.runID,
		"run_id":        r.runID,
		"reasoner_id":   ev.Reasoner,
		"agent_node_id": r.nodeID,
		"status":        ev.Status,
	}
	if ev.Parent != "" {
		body["parent_execution_id"] = ev.Parent
	}
	if ev.Input != nil {
		body["input_data"] = ev.Input
	}
	if ev.Result != nil {
		body["result"] = ev.Result
	}
	if ev.Error != "" {
		body["error"] = ev.Error
	}
	if ev.DurationMS != nil {
		body["duration_ms"] = *ev.DurationMS
	}
	r.enqueue(r.base+"/api/v1/workflow/executions/events", body, nil)
}

// Note posts a human-readable progress line with run-level headers only.
// The control plane's note endpoint rejects notes without an execution id —
// prefer NoteExec anchored to a node you created.
func (r *Reporter) Note(text string, tags ...string) {
	r.NoteExec("", text, tags...)
}

// NoteExec attaches the note to a specific execution when execID is set.
func (r *Reporter) NoteExec(execID, text string, tags ...string) {
	if tags == nil {
		tags = []string{}
	}
	headers := map[string]string{
		"X-Run-ID":        r.runID,
		"X-Workflow-ID":   r.runID,
		"X-Agent-Node-ID": r.nodeID,
	}
	if execID != "" {
		headers["X-Execution-ID"] = execID
	}
	r.enqueue(r.noteURL(), map[string]any{
		"message":       text,
		"tags":          tags,
		"timestamp":     float64(time.Now().UnixNano()) / 1e9,
		"agent_node_id": r.nodeID,
	}, headers)
}

// noteURL mirrors the SDK's mapping of the API base onto the UI API.
func (r *Reporter) noteURL() string {
	if strings.Contains(r.base, "/api/v1") {
		return strings.Replace(r.base, "/api/v1", "/api/ui/v1", 1) + "/executions/note"
	}
	return r.base + "/api/ui/v1/executions/note"
}

func (r *Reporter) enqueue(url string, body map[string]any, headers map[string]string) {
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	if !r.offer(message{body: raw, url: url, headers: headers}) {
		if r.dropped.Add(1) == 1 {
			r.logf("[afield] event queue full, dropping (suppressing further warnings)")
		}
	}
}

// offer attempts a non-blocking queue send; false when full or closed.
func (r *Reporter) offer(msg message) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return false
	}
	select {
	case r.queue <- msg:
		return true
	default:
		return false
	}
}

// Flush waits until every queued message has been sent, or the timeout
// elapses. Safe to call multiple times.
func (r *Reporter) Flush(timeout time.Duration) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ack := make(chan struct{})
	// The sentinel needs a queue slot; retry briefly while draining.
	for !r.offer(message{ack: ack}) {
		select {
		case <-deadline.C:
			return
		case <-r.done:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-ack:
	case <-deadline.C:
	case <-r.done:
	}
}

// Close flushes briefly and stops the sender goroutine.
func (r *Reporter) Close() {
	r.Flush(3 * time.Second)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	close(r.queue)
	<-r.done
}

// Dropped reports how many messages were discarded due to a full queue.
func (r *Reporter) Dropped() int64 { return r.dropped.Load() }

func (r *Reporter) sender() {
	defer close(r.done)
	for msg := range r.queue {
		if msg.ack != nil {
			close(msg.ack)
			continue
		}
		r.send(msg)
	}
}

func (r *Reporter) send(msg message) {
	req, err := http.NewRequest(http.MethodPost, msg.url, bytes.NewReader(msg.body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	for key, value := range msg.headers {
		req.Header.Set(key, value)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		r.warn.Do(func() {
			r.logf("[afield] control-plane post failed (suppressing further warnings): %v", err)
		})
		return
	}
	if resp.StatusCode >= 300 {
		r.warn.Do(func() {
			r.logf("[afield] control-plane post HTTP %d (suppressing further warnings)", resp.StatusCode)
		})
	}
	resp.Body.Close()
}
