package msgmodel

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Part is the 12-variant union of message-v2.ts:405-434, discriminated on
// `type`. Field order inside each variant is the TS Schema.Struct literal
// order; `...partBase` spreads first, so id/sessionID/messageID lead.
type Part interface {
	// PartType is the `type` discriminant.
	PartType() string
	// PartBase returns the shared {id, sessionID, messageID}.
	Base() PartBase
	json.Marshaler
}

// tagged re-asserts a union discriminant on marshal, then encodes through
// jscompat.Stringify so `<`, `>` and `&` survive unescaped the way
// JSON.stringify leaves them (encoding/json escapes them by default).
func tagged(v any) ([]byte, error) { return jscompat.Stringify(v) }

// ── text (message-v2.ts:111-127) ─────────────────────────────────────────

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

// ── subtask (message-v2.ts:224-240) ──────────────────────────────────────

// SubtaskModel is SubtaskPart.model (:230-235).
type SubtaskModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type SubtaskPart struct {
	PartBase
	Type        string        `json:"type"`
	Prompt      string        `json:"prompt"`
	Description string        `json:"description"`
	Agent       string        `json:"agent"`
	Model       *SubtaskModel `json:"model,omitempty"`
	Command     *string       `json:"command,omitempty"`
}

func (p SubtaskPart) PartType() string { return PartTypeSubtask }
func (p SubtaskPart) Base() PartBase   { return p.PartBase }
func (p SubtaskPart) MarshalJSON() ([]byte, error) {
	type alias SubtaskPart
	p.Type = PartTypeSubtask
	return tagged(alias(p))
}

// ── reasoning (message-v2.ts:129-141) ────────────────────────────────────
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

// ── file (message-v2.ts:185-195) ─────────────────────────────────────────

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

// ── tool (message-v2.ts:359-371) ─────────────────────────────────────────

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

// ProviderExecuted decodes just the one key of ToolPart.metadata that has a
// read path (processor.ts:298, :334, :348-350; prompt.ts:1613;
// message-v2.ts:917). TS spreads on truthiness, not on `=== true`.
func (p ToolPart) ProviderExecuted() bool { return p.Metadata.Truthy("providerExecuted") }

// ── step-start (message-v2.ts:257-264) ───────────────────────────────────

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

// ── step-finish (message-v2.ts:266-285) ──────────────────────────────────

type StepFinishPart struct {
	PartBase
	Type     string            `json:"type"`
	Reason   string            `json:"reason"`
	Snapshot *string           `json:"snapshot,omitempty"`
	Cost     jscompat.JSNumber `json:"cost"`
	Tokens   Tokens            `json:"tokens"`
}

func (p StepFinishPart) PartType() string { return PartTypeStepFinish }
func (p StepFinishPart) Base() PartBase   { return p.PartBase }
func (p StepFinishPart) MarshalJSON() ([]byte, error) {
	type alias StepFinishPart
	p.Type = PartTypeStepFinish
	return tagged(alias(p))
}

// ── snapshot (message-v2.ts:92-99) ───────────────────────────────────────

type SnapshotPart struct {
	PartBase
	Type     string `json:"type"`
	Snapshot string `json:"snapshot"`
}

func (p SnapshotPart) PartType() string { return PartTypeSnapshot }
func (p SnapshotPart) Base() PartBase   { return p.PartBase }
func (p SnapshotPart) MarshalJSON() ([]byte, error) {
	type alias SnapshotPart
	p.Type = PartTypeSnapshot
	return tagged(alias(p))
}

// ── patch (message-v2.ts:101-109) ────────────────────────────────────────

type PatchPart struct {
	PartBase
	Type  string   `json:"type"`
	Hash  string   `json:"hash"`
	Files []string `json:"files"`
}

func (p PatchPart) PartType() string { return PartTypePatch }
func (p PatchPart) Base() PartBase   { return p.PartBase }
func (p PatchPart) MarshalJSON() ([]byte, error) {
	type alias PatchPart
	p.Type = PartTypePatch
	if p.Files == nil {
		// Schema.Array is required; a nil Go slice would emit null.
		p.Files = []string{}
	}
	return tagged(alias(p))
}

// ── agent (message-v2.ts:197-211) ────────────────────────────────────────

// AgentSource is AgentPart.source (:202-208).
type AgentSource struct {
	Value string `json:"value"`
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

type AgentPart struct {
	PartBase
	Type   string       `json:"type"`
	Name   string       `json:"name"`
	Source *AgentSource `json:"source,omitempty"`
}

func (p AgentPart) PartType() string { return PartTypeAgent }
func (p AgentPart) Base() PartBase   { return p.PartBase }
func (p AgentPart) MarshalJSON() ([]byte, error) {
	type alias AgentPart
	p.Type = PartTypeAgent
	return tagged(alias(p))
}

// ── retry (message-v2.ts:242-255) ────────────────────────────────────────

type RetryPart struct {
	PartBase
	Type    string           `json:"type"`
	Attempt uint64           `json:"attempt"`
	Error   APIErrorEnvelope `json:"error"`
	Time    TimeCreated      `json:"time"`
}

func (p RetryPart) PartType() string { return PartTypeRetry }
func (p RetryPart) Base() PartBase   { return p.PartBase }
func (p RetryPart) MarshalJSON() ([]byte, error) {
	type alias RetryPart
	p.Type = PartTypeRetry
	return tagged(alias(p))
}

// ── compaction (message-v2.ts:213-222) ───────────────────────────────────

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

// UnmarshalPart dispatches on `type` the way effect Schema's discriminated
// union does.
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
	case PartTypeSubtask:
		target = new(SubtaskPart)
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
	case PartTypeSnapshot:
		target = new(SnapshotPart)
	case PartTypePatch:
		target = new(PatchPart)
	case PartTypeAgent:
		target = new(AgentPart)
	case PartTypeRetry:
		target = new(RetryPart)
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
	case *SubtaskPart:
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
	case *SnapshotPart:
		return *p, nil
	case *PatchPart:
		return *p, nil
	case *AgentPart:
		return *p, nil
	case *RetryPart:
		return *p, nil
	case *CompactionPart:
		return *p, nil
	}
	return nil, fmt.Errorf("msgmodel: unknown part type %q", probe.Type)
}

// Parts is `Part[]` with union-aware decoding.
type Parts []Part

// MarshalJSON keeps a nil slice as `[]`; TS `parts` is a required array.
func (ps Parts) MarshalJSON() ([]byte, error) {
	if ps == nil {
		return []byte("[]"), nil
	}
	return jscompat.Stringify([]Part(ps))
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
