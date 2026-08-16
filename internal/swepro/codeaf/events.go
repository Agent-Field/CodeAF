// This file ports terminal pipeline reporting from swe-pro/src/cli/cmd/run.ts:367-2930.
package codeaf

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
)

type event struct {
	Type      string         `json:"type"`
	Stage     string         `json:"stage,omitempty"`
	Status    string         `json:"status,omitempty"`
	Message   string         `json:"message,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp int64          `json:"ts"`
}

type eventWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	hook    func(event)
}

func newEventWriter(output io.Writer) *eventWriter {
	return &eventWriter{encoder: json.NewEncoder(output)}
}

func (writer *eventWriter) setHook(hook func(event)) {
	if writer == nil {
		return
	}
	writer.mu.Lock()
	writer.hook = hook
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
	_ = writer.encoder.Encode(value)
	if writer.hook != nil {
		writer.hook(value)
	}
	writer.mu.Unlock()
}

// busEvent writes the instance-bus payload without wrapping or renaming it.
// This is the stdout seam used by pinned TS run.ts via attachHarnessUi: every
// line has exactly the Bus.Payload shape {id,type,properties}.
func (writer *eventWriter) busEvent(value bus.Payload) {
	if writer == nil || writer.encoder == nil {
		return
	}
	writer.mu.Lock()
	_ = writer.encoder.Encode(value)
	writer.mu.Unlock()
}

func (writer *eventWriter) stage(stage, status string, data map[string]any) {
	writer.emit(event{Type: "stage", Stage: stage, Status: status, Data: data})
}
