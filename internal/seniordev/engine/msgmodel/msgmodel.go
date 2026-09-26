//go:build !windows

// Package msgmodel is the persisted message and part model: the assistant,
// user and tool parts a session stores, the conversion pipeline from stored
// messages to the model-facing message list, the compaction filter, and the
// part-assembly helpers the step loop and the stream processor build on.
//
// Opaque provider objects (metadata, tool input, structured output) are kept
// as raw JSON so their bytes round-trip unchanged; the doom-loop guard
// compares tool inputs byte for byte. Optional fields are pointers with
// omitempty. Every discriminated union re-asserts its own tag in
// MarshalJSON, and parts are stored and type-switched as VALUES, never
// pointers.
package msgmodel

import (
	"encoding/json"
)

// SyntheticAttachmentPrompt opens the synthetic user message that carries
// media extracted from a tool result.
const SyntheticAttachmentPrompt = "Attached media from tool result:"

// ── opaque JSON aliases ──────────────────────────────────────────────────

// RawValue is any JSON value carried verbatim. A zero-length RawValue is an
// absent value (the key is omitted), not JSON null.
type RawValue = json.RawMessage

// ── discriminant tags ────────────────────────────────────────────────────

// Part `type` discriminants.
const (
	PartTypeText       = "text"
	PartTypeReasoning  = "reasoning"
	PartTypeFile       = "file"
	PartTypeTool       = "tool"
	PartTypeStepStart  = "step-start"
	PartTypeStepFinish = "step-finish"
	PartTypeCompaction = "compaction"
)

// ToolState `status` discriminants.
const (
	ToolStatusPending   = "pending"
	ToolStatusRunning   = "running"
	ToolStatusCompleted = "completed"
	ToolStatusError     = "error"
)

// AssistantError `name` discriminants.
const (
	ErrNameUnknown             = "UnknownError"
	ErrNameMessageOutputLength = "MessageOutputLengthError"
	ErrNameMessageAborted      = "MessageAbortedError"
	ErrNameStructuredOutput    = "StructuredOutputError"
	ErrNameContextOverflow     = "ContextOverflowError"
	ErrNameAPI                 = "APIError"
)

// ── Provider.Model, narrowed ─────────────────────────────────────────────

// ModelAPI is the `api` sub-object of a catalog model: the provider SDK
// identifier and the provider-side model id. supportsMediaInToolResult reads
// both.
type ModelAPI struct {
	Npm string `json:"npm"`
	ID  string `json:"id"`
}

// Model is the slice of a catalog model this package reads: `providerID` and
// `id` feed DifferentModel, `api` feeds supportsMediaInToolResult.
type Model struct {
	ProviderID string   `json:"providerID"`
	ID         string   `json:"id"`
	API        ModelAPI `json:"api"`
}

// ── output format ──────────────────────────────────

// OutputFormat is the `OutputFormatText | OutputFormatJsonSchema` union. It is
// only carried, never inspected, by anything in this package, so it keeps its
// bytes verbatim.
type OutputFormat = json.RawMessage

// ── shared bases ─────────────────────────────────────────────────────────

// PartBase is embedded first in every part so id/sessionID/messageID lead
// the JSON.
type PartBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
}

// MessageBase is the id pair every message carries.
type MessageBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
}

// ── time sub-structs ─────────────────────────────────────────────────────

// TimeStartEnd is `{start, end?}`. TextPart.time and ReasoningPart.time share
// the shape; only the outer optionality differs.
type TimeStartEnd struct {
	Start uint64  `json:"start"`
	End   *uint64 `json:"end,omitempty"`
}

// TimeCreated is `{created}`.
type TimeCreated struct {
	Created uint64 `json:"created"`
}

// TokenCache is `{read, write}`.
type TokenCache struct {
	Read  uint64 `json:"read"`
	Write uint64 `json:"write"`
}

// Tokens is the token block shared by StepFinishPart and Assistant.
type Tokens struct {
	Total     *uint64    `json:"total,omitempty"`
	Input     uint64     `json:"input"`
	Output    uint64     `json:"output"`
	Reasoning uint64     `json:"reasoning"`
	Cache     TokenCache `json:"cache"`
}

// ── file part sources ────────────────────────────

// FilePartSourceText is the text span a file part source covers.
type FilePartSourceText struct {
	Value string `json:"value"`
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// LSPPosition / LSPRange locate a symbol source in its file.
type LSPPosition struct {
	Line      uint64 `json:"line"`
	Character uint64 `json:"character"`
}

type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// FilePartSource is the file / symbol / resource source union, discriminated
// on `type`. Nothing in this package reads it, so it is a single carrier
// struct rather than an interface, with the shared `text` first.
type FilePartSource struct {
	Text       FilePartSourceText `json:"text"`
	Type       string             `json:"type"`
	Path       string             `json:"path,omitempty"`
	Range      *LSPRange          `json:"range,omitempty"`
	Name       string             `json:"name,omitempty"`
	Kind       *uint64            `json:"kind,omitempty"`
	ClientName string             `json:"clientName,omitempty"`
	URI        string             `json:"uri,omitempty"`
}

// ── FileDiff ─────────────────────────────────────────────────────────────

type FileDiff struct {
	File      string  `json:"file"`
	Patch     string  `json:"patch"`
	Additions float64 `json:"additions"`
	Deletions float64 `json:"deletions"`
}
