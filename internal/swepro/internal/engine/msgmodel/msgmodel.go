// Package msgmodel is a bug-for-bug port of the pure half of
// src/session/message-v2.ts — the persisted message/part model, the
// `differentModel` metadata-stripping rule, the WithParts→UIMessage→ModelMessage
// conversion pipeline, `filterCompacted`, and the part-assembly helpers the
// step loop and the stream processor build on. ENGINE-DESIGN.md calls this
// package `engine/message`; the brief names it `engine/msgmodel`.
//
// Database access is represented by the narrow Store interface in storage.go;
// cursor/page/stream/parts/get keep the source ordering and pagination
// behavior without coupling this core model to a concrete SQL driver.
// Provider error parsing is similarly injected into FromError through the
// ProviderErrorParser seam.
//
// ── seams ────────────────────────────────────────────────────────────────
//   - MessageID.ascending() — the id minted for the synthetic
//     "Attached media from tool result:" user message (message-v2.ts:979). No
//     Identifier port exists yet, and the id never reaches the ModelMessage
//     output, so the default is a package-local counter and
//     SetMessageIDFactoryForTesting swaps it.
//   - Provider.Model — only four fields are read (`providerID`, `id`,
//     `api.npm`, `api.id`), so Model here is a narrow local struct rather than
//     a dependency on the unbuilt llm/catalog.
//   - tool `toModelOutput` — convertToModelMessages takes it as a callback
//     (ai/dist/index.mjs:1668); ToModelMessages supplies message-v2's own.
//
// ── sibling ports ────────────────────────────────────────────────────────
//   - internal/jscompat — Stringify (JSON.stringify parity: compact, no HTML
//     escaping), FormatNumber (String(n)), JSNumber (Schema.Finite fields),
//     SliceTo (JS Array.prototype.slice with a float end).
//
// ── fidelity notes (deliberate, do not "fix") ────────────────────────────
//   - Opaque `Record<string, any>` fields (every `metadata`, every tool
//     `input`, `Assistant.structured`) are RawObject/json.RawMessage, never
//     map[string]any. Two reasons: JSON.stringify preserves JS insertion order
//     while Go sorts map keys, and the doom-loop guard
//     (processor.ts:356-364, ENGINE-DESIGN §5.5) is a *stringify equality*
//     test on the tool input, so the bytes are load-bearing.
//   - `providerMeta` (message-v2.ts:723-727) is an object rest-spread, so it
//     must drop the `providerExecuted` key and keep every other key in its
//     original order — hence the token-level ordered object walk in
//     rawobject.go rather than a decode/re-encode.
//   - `Schema.optional` fields are pointers (or a zero-length RawObject /
//     RawValue) with `omitempty`: TS `Schema.optional` means *absent*, and
//     JSON.stringify drops both absent keys and present-but-undefined keys.
//     ENGINE-DESIGN §5.1's blanket "no omitempty" rule is about optionals that
//     TS materialises as explicit `null`; message-v2 has none — effect
//     Schema.optional never decodes null.
//   - Every discriminated union re-asserts its own tag in MarshalJSON, so a
//     hand-built ToolStateError can never serialise as `"status":""`. The tag
//     is written through jscompat.Stringify, not json.Marshal, so `<`/`>`/`&`
//     inside a part survive unescaped exactly as JSON.stringify leaves them.
//   - `NonNegativeInt` fields are uint64 per ENGINE-DESIGN §5.1, which
//     overrides F7's blanket "every JS number is float64". `Schema.Finite`
//     fields (`cost`) stay jscompat.JSNumber.
//   - `Part`, `ToolState` and `Info` are satisfied by the VALUE forms
//     (`TextPart`, not `*TextPart`) — every constructor and every Unmarshal*
//     returns a value, and the conversion pipeline type-switches on values.
//     A pointer also satisfies the interface (the methods have value
//     receivers) but would silently miss every branch, so do not store one.
//     No runtime guard: `differentModel` makes this a per-part hot path
//     (§5.3) and a reflect-based unwrap would tax every message of every turn.
//   - Field order in every struct is the TS `Schema.Struct` literal order,
//     including the `...partBase` / `...messageBase` / `...filePartSourceBase`
//     spreads landing first.
package msgmodel

import (
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// SYNTHETIC_ATTACHMENT_PROMPT (message-v2.ts:38).
const SyntheticAttachmentPrompt = "Attached media from tool result:"

// ── opaque JSON aliases ──────────────────────────────────────────────────

// RawValue is any JS value carried verbatim. A zero-length RawValue is TS
// `undefined` (the key is absent), not JSON null.
type RawValue = json.RawMessage

// ── discriminant tags ────────────────────────────────────────────────────

// Part `type` discriminants (message-v2.ts:405-434).
const (
	PartTypeText       = "text"
	PartTypeSubtask    = "subtask"
	PartTypeReasoning  = "reasoning"
	PartTypeFile       = "file"
	PartTypeTool       = "tool"
	PartTypeStepStart  = "step-start"
	PartTypeStepFinish = "step-finish"
	PartTypeSnapshot   = "snapshot"
	PartTypePatch      = "patch"
	PartTypeAgent      = "agent"
	PartTypeRetry      = "retry"
	PartTypeCompaction = "compaction"
)

// ToolState `status` discriminants (message-v2.ts:346-349).
const (
	ToolStatusPending   = "pending"
	ToolStatusRunning   = "running"
	ToolStatusCompleted = "completed"
	ToolStatusError     = "error"
)

// AssistantError `name` discriminants (message-v2.ts:462-472).
const (
	ErrNameProviderAuth        = "ProviderAuthError"
	ErrNameUnknown             = "UnknownError"
	ErrNameMessageOutputLength = "MessageOutputLengthError"
	ErrNameMessageAborted      = "MessageAbortedError"
	ErrNameStructuredOutput    = "StructuredOutputError"
	ErrNameContextOverflow     = "ContextOverflowError"
	ErrNameAPI                 = "APIError"
)

// ── Provider.Model, narrowed ─────────────────────────────────────────────

// ModelAPI is the `api` sub-object of Provider.Model. Only `npm` and `id` are
// read by message-v2 (`supportsMediaInToolResult`, message-v2.ts:745-755).
type ModelAPI struct {
	Npm string `json:"npm"`
	ID  string `json:"id"`
}

// Model is the slice of Provider.Model message-v2 touches: `providerID` and
// `id` feed `differentModel` (:841), `api` feeds `supportsMediaInToolResult`.
type Model struct {
	ProviderID string   `json:"providerID"`
	ID         string   `json:"id"`
	API        ModelAPI `json:"api"`
}

// ── output format (message-v2.ts:64-82) ──────────────────────────────────

// OutputFormat is the `OutputFormatText | OutputFormatJsonSchema` union. It is
// only carried, never inspected, by anything in this package, so it keeps its
// bytes verbatim.
type OutputFormat = json.RawMessage

// ── shared bases ─────────────────────────────────────────────────────────

// PartBase is `partBase` (message-v2.ts:86-90). Embedded first in every part
// so id/sessionID/messageID lead the JSON, matching the spread.
type PartBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
}

// MessageBase is `messageBase` (message-v2.ts:373-376).
type MessageBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
}

// ── time sub-structs ─────────────────────────────────────────────────────

// TimeStartEnd is `{start, end?}` — TextPart.time (:117-122) and
// ReasoningPart.time (:134-137) share the shape; only the outer optionality
// differs.
type TimeStartEnd struct {
	Start uint64  `json:"start"`
	End   *uint64 `json:"end,omitempty"`
}

// TimeCreated is `{created}` — RetryPart.time (:249-251).
type TimeCreated struct {
	Created uint64 `json:"created"`
}

// TokenCache is `{read, write}` (:281-284, :571-574).
type TokenCache struct {
	Read  uint64 `json:"read"`
	Write uint64 `json:"write"`
}

// Tokens is the shared token block of StepFinishPart (:275-285) and Assistant
// (:565-575).
type Tokens struct {
	Total     *uint64    `json:"total,omitempty"`
	Input     uint64     `json:"input"`
	Output    uint64     `json:"output"`
	Reasoning uint64     `json:"reasoning"`
	Cache     TokenCache `json:"cache"`
}

// ── file part sources (message-v2.ts:143-181) ────────────────────────────

// FilePartSourceText is `filePartSourceBase.text` (:144-148).
type FilePartSourceText struct {
	Value string `json:"value"`
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// LSPPosition / LSPRange mirror src/lsp/lsp.ts:25-33 — SymbolSource.range.
type LSPPosition struct {
	Line      uint64 `json:"line"`
	Character uint64 `json:"character"`
}

type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// FilePartSource is the `FileSource | SymbolSource | ResourceSource` union
// (:178-181), discriminated on `type`. Nothing in this package reads it, so it
// is a single carrier struct rather than an interface; the field order is the
// union of the three literals with `text` first (the base spread).
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

// ── FileDiff (src/session/file-diff.ts:5-10) ─────────────────────────────

type FileDiff struct {
	File      string            `json:"file"`
	Patch     string            `json:"patch"`
	Additions jscompat.JSNumber `json:"additions"`
	Deletions jscompat.JSNumber `json:"deletions"`
}
