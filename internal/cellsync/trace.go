package cellsync

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// TraceEvent records operation metadata only. RawBytes is unknown at encrypted
// seams; nil is different from a measured zero. HTTP body bytes exclude headers.
type TraceEvent struct {
	Timestamp     time.Time `json:"ts"`
	HTTPStatus    int       `json:"http_status,omitempty"`
	Version       int       `json:"v"`
	Sequence      uint64    `json:"sequence"`
	Op            string    `json:"op"`
	Layer         string    `json:"layer"`
	Phase         string    `json:"phase,omitempty"`
	Class         string    `json:"class"`
	RID           string    `json:"rid,omitempty"`
	Objects       int       `json:"objects,omitempty"`
	RawBytes      *int64    `json:"raw_bytes"`
	PayloadBytes  int64     `json:"payload_bytes,omitempty"`
	SentBytes     int64     `json:"sent_body_bytes,omitempty"`
	ReceivedBytes int64     `json:"received_body_bytes,omitempty"`
	DurationNS    int64     `json:"duration_ns"`
	Outcome       string    `json:"outcome"`
	Gate          string    `json:"gate,omitempty"`
	HeldLocally   *bool     `json:"held_locally,omitempty"`
	Seed          int64     `json:"seed,omitempty"`
	LogicalBytes  int64     `json:"workload_logical_bytes,omitempty"`
	Files         int       `json:"workload_files,omitempty"`
	Attempt       int       `json:"attempt"`
}

// Observer is optional. Observers must not inspect payloads or attach paths.
type Observer func(TraceEvent)

// JSONLTrace writes operation records synchronously only when explicitly used.
// Writer failures are reported separately; they never replace a sync result.
type JSONLTrace struct {
	mu       sync.Mutex
	writer   io.Writer
	sequence uint64
	err      error
}

func NewJSONLTrace(writer io.Writer) *JSONLTrace { return &JSONLTrace{writer: writer} }
func (t *JSONLTrace) Observe(event TraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sequence++
	event.Version = 1
	event.Sequence = t.sequence
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	event.Attempt = max(1, event.Attempt)
	if event.Class == "" {
		event.Class = "unknown"
	}
	if event.Outcome == "" {
		event.Outcome = "ok"
	}
	if t.err == nil {
		t.err = json.NewEncoder(t.writer).Encode(event)
	}
}
func (t *JSONLTrace) Err() error { t.mu.Lock(); defer t.mu.Unlock(); return t.err }

type traceContext struct {
	phase   string
	attempt int
}
type traceKey struct{}

// TracePhase identifies a fixed benchmark phase, not a user-provided label.
func TracePhase(ctx context.Context, phase string, attempt int) context.Context {
	switch phase {
	case "publish", "cold", "warm", "calibration":
	default:
		phase = "unknown"
	}
	return context.WithValue(ctx, traceKey{}, traceContext{phase: phase, attempt: max(1, attempt)})
}
func observed(observer Observer, ctx context.Context, event TraceEvent, start time.Time, err error) {
	if observer == nil {
		return
	}
	if event.RID != "" && !blobstore.ValidRID(event.RID) {
		event.RID = ""
	}
	event.Version = 1
	event.Timestamp = start
	event.DurationNS = time.Since(start).Nanoseconds()
	if event.Outcome == "" {
		event.Outcome = "ok"
	}
	if err != nil {
		event.Outcome = "error"
	}
	if (err != nil || event.Outcome == "incomplete") && ctx.Err() != nil {
		event.Outcome = "cancelled"
	}
	state, _ := ctx.Value(traceKey{}).(traceContext)
	event.Phase = state.phase
	event.Attempt = max(1, state.attempt)
	// An optional diagnostic callback cannot change transport behavior by panicking.
	defer func() { _ = recover() }()
	observer(event)
}
