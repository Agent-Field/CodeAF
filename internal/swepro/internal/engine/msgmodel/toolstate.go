package msgmodel

import (
	"encoding/json"
	"fmt"
)

// ToolState is the four-variant union of message-v2.ts:346-349, discriminated
// on `status`. The field sets genuinely differ — `pending` has NO `time` at
// all (:287-294), which is why the cleanup drain (processor.ts:640-656) has to
// fall back to `now`.
type ToolState interface {
	ToolStatus() string
	// Input is `Record<string, any>`, present on every variant.
	ToolInput() RawObject
	// ToolMetadata is the variant's `metadata`, or a zero RawObject when the
	// variant has none (pending).
	ToolMetadata() RawObject
	// StartTime is `state.time.start`; ok=false for `pending`.
	StartTime() (uint64, bool)
	json.Marshaler
}

// ── pending (message-v2.ts:287-294) ──────────────────────────────────────

type ToolStatePending struct {
	Status string    `json:"status"`
	Input  RawObject `json:"input"`
	Raw    string    `json:"raw"`
}

func (s ToolStatePending) ToolStatus() string        { return ToolStatusPending }
func (s ToolStatePending) ToolInput() RawObject      { return s.Input }
func (s ToolStatePending) ToolMetadata() RawObject   { return nil }
func (s ToolStatePending) StartTime() (uint64, bool) { return 0, false }
func (s ToolStatePending) MarshalJSON() ([]byte, error) {
	type alias ToolStatePending
	s.Status = ToolStatusPending
	return tagged(alias(s))
}

// ── running (message-v2.ts:296-307) ──────────────────────────────────────

// ToolTimeStart is ToolStateRunning.time (:302-304).
type ToolTimeStart struct {
	Start uint64 `json:"start"`
}

type ToolStateRunning struct {
	Status   string        `json:"status"`
	Input    RawObject     `json:"input"`
	Title    *string       `json:"title,omitempty"`
	Metadata RawObject     `json:"metadata,omitempty"`
	Time     ToolTimeStart `json:"time"`
}

func (s ToolStateRunning) ToolStatus() string        { return ToolStatusRunning }
func (s ToolStateRunning) ToolInput() RawObject      { return s.Input }
func (s ToolStateRunning) ToolMetadata() RawObject   { return s.Metadata }
func (s ToolStateRunning) StartTime() (uint64, bool) { return s.Time.Start, true }
func (s ToolStateRunning) MarshalJSON() ([]byte, error) {
	type alias ToolStateRunning
	s.Status = ToolStatusRunning
	return tagged(alias(s))
}

// ── completed (message-v2.ts:309-324) ────────────────────────────────────
//
// `title` and `metadata` are REQUIRED here, unlike every other variant.

// ToolTimeCompleted is ToolStateCompleted.time (:315-319).
type ToolTimeCompleted struct {
	Start     uint64  `json:"start"`
	End       uint64  `json:"end"`
	Compacted *uint64 `json:"compacted,omitempty"`
}

type ToolStateCompleted struct {
	Status      string            `json:"status"`
	Input       RawObject         `json:"input"`
	Output      string            `json:"output"`
	Title       string            `json:"title"`
	Metadata    RawObject         `json:"metadata"`
	Time        ToolTimeCompleted `json:"time"`
	Attachments *[]FilePart       `json:"attachments,omitempty"`
}

func (s ToolStateCompleted) ToolStatus() string        { return ToolStatusCompleted }
func (s ToolStateCompleted) ToolInput() RawObject      { return s.Input }
func (s ToolStateCompleted) ToolMetadata() RawObject   { return s.Metadata }
func (s ToolStateCompleted) StartTime() (uint64, bool) { return s.Time.Start, true }
func (s ToolStateCompleted) MarshalJSON() ([]byte, error) {
	type alias ToolStateCompleted
	s.Status = ToolStatusCompleted
	return tagged(alias(s))
}

// ── error (message-v2.ts:332-344) ────────────────────────────────────────

// ToolTimeSpan is ToolStateError.time (:338-341).
type ToolTimeSpan struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

type ToolStateError struct {
	Status   string       `json:"status"`
	Input    RawObject    `json:"input"`
	Error    string       `json:"error"`
	Metadata RawObject    `json:"metadata,omitempty"`
	Time     ToolTimeSpan `json:"time"`
}

func (s ToolStateError) ToolStatus() string        { return ToolStatusError }
func (s ToolStateError) ToolInput() RawObject      { return s.Input }
func (s ToolStateError) ToolMetadata() RawObject   { return s.Metadata }
func (s ToolStateError) StartTime() (uint64, bool) { return s.Time.Start, true }
func (s ToolStateError) MarshalJSON() ([]byte, error) {
	type alias ToolStateError
	s.Status = ToolStatusError
	return tagged(alias(s))
}

// ── union decode ─────────────────────────────────────────────────────────

// UnmarshalToolState dispatches on `status`.
func UnmarshalToolState(raw []byte) (ToolState, error) {
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	switch probe.Status {
	case ToolStatusPending:
		var s ToolStatePending
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return s, nil
	case ToolStatusRunning:
		var s ToolStateRunning
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return s, nil
	case ToolStatusCompleted:
		var s ToolStateCompleted
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return s, nil
	case ToolStatusError:
		var s ToolStateError
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, fmt.Errorf("msgmodel: unknown tool state status %q", probe.Status)
}

// UnmarshalJSON on ToolPart has to route `state` through the union decoder.
func (p *ToolPart) UnmarshalJSON(b []byte) error {
	type alias struct {
		PartBase
		Type     string          `json:"type"`
		CallID   string          `json:"callID"`
		Tool     string          `json:"tool"`
		State    json.RawMessage `json:"state"`
		Metadata RawObject       `json:"metadata"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	state, err := UnmarshalToolState(a.State)
	if err != nil {
		return err
	}
	p.PartBase = a.PartBase
	p.Type = a.Type
	p.CallID = a.CallID
	p.Tool = a.Tool
	p.State = state
	p.Metadata = a.Metadata
	return nil
}
