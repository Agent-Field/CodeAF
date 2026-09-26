//go:build !windows

package msgmodel

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// Part is the seven-variant part union, discriminated on `type`. PartBase is
// embedded first in every variant, so id/sessionID/messageID lead the JSON.
type Part interface {
	// PartType is the `type` discriminant.
	PartType() string
	// PartBase returns the shared {id, sessionID, messageID}.
	Base() PartBase
	json.Marshaler
}

// tagged re-asserts a union discriminant on marshal, then encodes without
// HTML escaping so `<`, `>` and `&` inside a part stay readable.
func tagged(v any) ([]byte, error) { return jsonutil.Marshal(v) }

// ── text ─────────────────────────────────────────

type TextPart struct {
	PartBase
	Type      string        `json:"type"`
	Text      string        `json:"text"`
	Synthetic *bool         `json:"synthetic,omitempty"`
	Ignored   *bool         `json:"ignored,omitempty"`
	Time      *TimeStartEnd `json:"time,omitempty"`
	Metadata  RawObject     `json:"metadata,omitempty"`
}

func (p TextPart) PartType() string { return PartTypeText }
func (p TextPart) Base() PartBase   { return p.PartBase }
func (p TextPart) MarshalJSON() ([]byte, error) {
	type alias TextPart
	p.Type = PartTypeText
	return tagged(alias(p))
}

// ── reasoning ────────────────────────────────────
//
// `time` is REQUIRED here, unlike TextPart's optional one.

type ReasoningPart struct {
	PartBase
	Type     string       `json:"type"`
	Text     string       `json:"text"`
	Metadata RawObject    `json:"metadata,omitempty"`
	Time     TimeStartEnd `json:"time"`
}

func (p ReasoningPart) PartType() string { return PartTypeReasoning }
func (p ReasoningPart) Base() PartBase   { return p.PartBase }
func (p ReasoningPart) MarshalJSON() ([]byte, error) {
	type alias ReasoningPart
	p.Type = PartTypeReasoning
	return tagged(alias(p))
}

// ── file ─────────────────────────────────────────

type FilePart struct {
	PartBase
	Type     string          `json:"type"`
	Mime     string          `json:"mime"`
	Filename *string         `json:"filename,omitempty"`
	URL      string          `json:"url"`
	Source   *FilePartSource `json:"source,omitempty"`
}

func (p FilePart) PartType() string { return PartTypeFile }
func (p FilePart) Base() PartBase   { return p.PartBase }
func (p FilePart) MarshalJSON() ([]byte, error) {
	type alias FilePart
	p.Type = PartTypeFile
	return tagged(alias(p))
}

// ── tool ─────────────────────────────────────────

type ToolPart struct {
	PartBase
	Type     string    `json:"type"`
	CallID   string    `json:"callID"`
	Tool     string    `json:"tool"`
	State    ToolState `json:"state"`
	Metadata RawObject `json:"metadata,omitempty"`
}

func (p ToolPart) PartType() string { return PartTypeTool }
func (p ToolPart) Base() PartBase   { return p.PartBase }
func (p ToolPart) MarshalJSON() ([]byte, error) {
	type alias ToolPart
	p.Type = PartTypeTool
	return tagged(alias(p))
}

// ProviderExecuted reads the one key of ToolPart.metadata that has a read
// path. It is a truthiness test, not a strict `true` comparison.
func (p ToolPart) ProviderExecuted() bool { return p.Metadata.Truthy("providerExecuted") }

// ── step-start ───────────────────────────────────

type StepStartPart struct {
	PartBase
	Type     string  `json:"type"`
	Snapshot *string `json:"snapshot,omitempty"`
}

func (p StepStartPart) PartType() string { return PartTypeStepStart }
func (p StepStartPart) Base() PartBase   { return p.PartBase }
func (p StepStartPart) MarshalJSON() ([]byte, error) {
	type alias StepStartPart
	p.Type = PartTypeStepStart
	return tagged(alias(p))
}

// ── step-finish ──────────────────────────────────

type StepFinishPart struct {
	PartBase
	Type     string  `json:"type"`
	Reason   string  `json:"reason"`
	Snapshot *string `json:"snapshot,omitempty"`
	Cost     float64 `json:"cost"`
	Tokens   Tokens  `json:"tokens"`
	// Upstream is the endpoint OpenRouter reports as having served the call
	// (its response `provider` field). Cache-miss attribution needs to know
	// when successive calls changed endpoint, and the wire already says so.
	// Absent when the provider never reported one.
	Upstream string `json:"upstream,omitempty"`
}

func (p StepFinishPart) PartType() string { return PartTypeStepFinish }
func (p StepFinishPart) Base() PartBase   { return p.PartBase }
func (p StepFinishPart) MarshalJSON() ([]byte, error) {
	type alias StepFinishPart
	p.Type = PartTypeStepFinish
	return tagged(alias(p))
}

// ── compaction ───────────────────────────────────

type CompactionPart struct {
	PartBase
	Type        string  `json:"type"`
	Auto        bool    `json:"auto"`
	Overflow    *bool   `json:"overflow,omitempty"`
	TailStartID *string `json:"tail_start_id,omitempty"`
}

func (p CompactionPart) PartType() string { return PartTypeCompaction }
func (p CompactionPart) Base() PartBase   { return p.PartBase }
func (p CompactionPart) MarshalJSON() ([]byte, error) {
	type alias CompactionPart
	p.Type = PartTypeCompaction
	return tagged(alias(p))
}

// ── union decode ─────────────────────────────────────────────────────────

// UnmarshalPart dispatches on `type`.
func UnmarshalPart(raw []byte) (Part, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	var target any
	switch probe.Type {
	case PartTypeText:
		target = new(TextPart)
	case PartTypeReasoning:
		target = new(ReasoningPart)
	case PartTypeFile:
		target = new(FilePart)
	case PartTypeTool:
		target = new(ToolPart)
	case PartTypeStepStart:
		target = new(StepStartPart)
	case PartTypeStepFinish:
		target = new(StepFinishPart)
	case PartTypeCompaction:
		target = new(CompactionPart)
	default:
		return nil, fmt.Errorf("msgmodel: unknown part type %q", probe.Type)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	switch p := target.(type) {
	case *TextPart:
		return *p, nil
	case *ReasoningPart:
		return *p, nil
	case *FilePart:
		return *p, nil
	case *ToolPart:
		return *p, nil
	case *StepStartPart:
		return *p, nil
	case *StepFinishPart:
		return *p, nil
	case *CompactionPart:
		return *p, nil
	}
	return nil, fmt.Errorf("msgmodel: unknown part type %q", probe.Type)
}

// Parts is `Part[]` with union-aware decoding.
type Parts []Part

// MarshalJSON keeps a nil slice as `[]`; `parts` is a required array.
func (ps Parts) MarshalJSON() ([]byte, error) {
	if ps == nil {
		return []byte("[]"), nil
	}
	return jsonutil.Marshal([]Part(ps))
}

func (ps *Parts) UnmarshalJSON(b []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return err
	}
	out := make(Parts, 0, len(raws))
	for _, raw := range raws {
		p, err := UnmarshalPart(raw)
		if err != nil {
			return err
		}
		out = append(out, p)
	}
	*ps = out
	return nil
}
