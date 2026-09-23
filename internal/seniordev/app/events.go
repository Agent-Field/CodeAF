//go:build !windows

// This file is the NDJSON event stream: the stage events senior-dev emits on
// stdout and the bus payloads it forwards there unchanged.
package app

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
)

type event struct {
	Type       string         `json:"type"`
	Stage      string         `json:"stage,omitempty"`
	Status     string         `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Timestamp  int64          `json:"ts"`
	TraceID    string         `json:"trace_id,omitempty"`
	Step       uint64         `json:"step,omitempty"`
	Occurrence uint64         `json:"occurrence,omitempty"`
	Title      string         `json:"title,omitempty"`
	ElapsedMS  int64          `json:"elapsed_ms,omitempty"`
	// `spend` only. A pointer because a run that has cost nothing yet still
	// reports a figure, and omitempty would drop a real zero.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// `step` only: what was run, and what came back.
	Command     string `json:"command,omitempty"`
	Observation string `json:"observation,omitempty"`
}

type eventWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	hook    func(event)
	trace   *runTrace
	summary *agentSummary
	// steps deduplicates `step` records: a tool part is republished as its
	// state moves, so the same finished call arrives more than once.
	steps map[string]struct{}
}

func newEventWriter(output io.Writer) *eventWriter {
	return &eventWriter{
		encoder: json.NewEncoder(output),
		summary: newAgentSummary(),
		steps:   map[string]struct{}{},
	}
}

func (writer *eventWriter) setHook(hook func(event)) {
	if writer == nil {
		return
	}
	writer.mu.Lock()
	writer.hook = hook
	writer.mu.Unlock()
}

// enableTrace mirrors semantic run events as structured records on notes.
// stdout remains the exhaustive NDJSON event stream; notes is stderr in the
// shipped binary, so the readable trace goes wherever stderr goes.
func (writer *eventWriter) enableTrace(notes io.Writer, runID string) {
	if writer == nil || notes == nil {
		return
	}
	writer.mu.Lock()
	writer.trace = newRunTrace(notes, runID)
	writer.mu.Unlock()
}

func (writer *eventWriter) emit(value event) {
	if writer == nil || writer.encoder == nil {
		return
	}
	if value.Timestamp == 0 {
		value.Timestamp = time.Now().UnixMilli()
	}
	writer.mu.Lock()
	if writer.trace != nil {
		value = writer.trace.event(value)
	}
	_ = writer.encoder.Encode(value)
	if writer.hook != nil {
		writer.hook(value)
	}
	writer.mu.Unlock()
}

// emitUntraced writes a record to stdout without mirroring it into the stderr
// trace. `spend` and `step` exist for a reader consuming stdout; the trace
// already carries its own tool and cost records, and duplicating them there
// would bury the semantic trace under one entry per tool call.
func (writer *eventWriter) emitUntraced(value event) {
	if writer == nil || writer.encoder == nil {
		return
	}
	if value.Timestamp == 0 {
		value.Timestamp = time.Now().UnixMilli()
	}
	writer.mu.Lock()
	_ = writer.encoder.Encode(value)
	if writer.hook != nil {
		writer.hook(value)
	}
	writer.mu.Unlock()
}

// busEvent writes the instance-bus payload without wrapping or renaming it:
// every such line has exactly the Bus.Payload shape {id,type,properties}.
func (writer *eventWriter) busEvent(value bus.Payload) {
	if writer == nil || writer.encoder == nil {
		return
	}
	// Observed OUTSIDE the writer lock: the summary keeps its own mutex, so
	// aggregation never extends the encode critical section.
	spend, completed := writer.summary.observeBus(value)
	step, isStep := toolStepRecord(value)
	writer.mu.Lock()
	_ = writer.encoder.Encode(value)
	if writer.trace != nil {
		writer.trace.busEvent(value)
	}
	if isStep {
		if _, seen := writer.steps[step.key]; seen {
			isStep = false
		} else {
			writer.steps[step.key] = struct{}{}
		}
	}
	writer.mu.Unlock()
	// Both are emitted outside the lock, because emit takes the same one.
	// Neither reaches the model: they are written after the fact, from state
	// the stream already published.
	if isStep {
		writer.emitUntraced(event{
			Type: "step", Command: step.command, Observation: step.observation,
		})
	}
	// The running total, after the message that moved it. A reader enforcing a
	// dollar ceiling while the run is alive reads this and nothing else: the
	// agent-summary and terminal totals arrive only once the run is over.
	if completed {
		total := spend
		writer.emitUntraced(event{Type: "spend", CostUSD: &total})
	}
}

func (writer *eventWriter) stage(stage, status string, data map[string]any) {
	writer.emit(event{Type: "stage", Stage: stage, Status: status, Data: data})
}
