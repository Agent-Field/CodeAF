package orclient

// The normalized stream-part union the OpenRouter transform emits — the
// `SingleRequestTextStreamPart` half of ENGINE-DESIGN §3.3 (`ai/dist/index.d.ts:4529-4603`).
//
// ENGINE-DESIGN is explicit that this union must stay DISTINCT from the outer
// `TextStreamPart` the step layer produces: this one uses `delta`, has
// `stream-start`/`response-metadata`, and has no `start-step`/`finish-step`
// framing. Collapsing them loses the framing `processor.ts` depends on.
//
// Every variant carries its own MarshalJSON with the TS object-literal key
// order, because these parts are what the fixture corpus compares byte-for-byte
// against the running provider.

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/calc"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// StreamPart is one emitted part.
type StreamPart interface {
	PartType() string
	json.Marshaler
}

// Part type tags.
const (
	PartTypeResponseMetadata = "response-metadata"
	PartTypeReasoningStart   = "reasoning-start"
	PartTypeReasoningDelta   = "reasoning-delta"
	PartTypeReasoningEnd     = "reasoning-end"
	PartTypeTextStart        = "text-start"
	PartTypeTextDelta        = "text-delta"
	PartTypeTextEnd          = "text-end"
	PartTypeSource           = "source"
	PartTypeToolInputStart   = "tool-input-start"
	PartTypeToolInputDelta   = "tool-input-delta"
	PartTypeToolInputEnd     = "tool-input-end"
	PartTypeToolCall         = "tool-call"
	PartTypeFile             = "file"
	PartTypeError            = "error"
	PartTypeFinish           = "finish"
	PartTypeRaw              = "raw"
	PartTypeAbort            = "abort"
)

// objectWriter builds a JSON object with explicit key order, skipping keys
// whose value is the JS `undefined`.
type objectWriter struct {
	buf   bytes.Buffer
	first bool
	err   error
}

func newObjectWriter() *objectWriter {
	w := &objectWriter{first: true}
	w.buf.WriteByte('{')
	return w
}

func (w *objectWriter) raw(key string, value json.RawMessage) {
	if w.err != nil || value == nil {
		return
	}
	if !w.first {
		w.buf.WriteByte(',')
	}
	w.first = false
	k, err := jscompat.Stringify(key)
	if err != nil {
		w.err = err
		return
	}
	w.buf.Write(k)
	w.buf.WriteByte(':')
	w.buf.Write(value)
}

func (w *objectWriter) str(key, value string) {
	enc, err := jscompat.Stringify(value)
	if err != nil {
		w.err = err
		return
	}
	w.raw(key, enc)
}

func (w *objectWriter) marshal(key string, value any) {
	enc, err := json.Marshal(value)
	if err != nil {
		w.err = err
		return
	}
	w.raw(key, enc)
}

func (w *objectWriter) done() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	w.buf.WriteByte('}')
	return w.buf.Bytes(), nil
}

// ── response-metadata ─────────────────────────────────────────────────────

// ResponseMetadataPart is emitted TWICE per chunk that carries both an `id`
// and a `model` — once with only `id` (`:3793`), once with only `modelId`
// (`:3800`). They are deliberately separate parts, not one merged part.
type ResponseMetadataPart struct {
	ID      string
	ModelID string
	// IsModel selects which of the two emissions this is.
	IsModel bool
}

func (p ResponseMetadataPart) PartType() string { return PartTypeResponseMetadata }

func (p ResponseMetadataPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeResponseMetadata)
	if p.IsModel {
		w.str("modelId", p.ModelID)
	} else {
		w.str("id", p.ID)
	}
	return w.done()
}

// ── reasoning ─────────────────────────────────────────────────────────────

// ReasoningStartPart is `{type:"reasoning-start", id}`.
type ReasoningStartPart struct{ ID string }

func (p ReasoningStartPart) PartType() string { return PartTypeReasoningStart }

func (p ReasoningStartPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeReasoningStart)
	w.str("id", p.ID)
	return w.done()
}

// ReasoningDeltaPart is `{type:"reasoning-delta", delta, id}` — note `delta`
// precedes `id` here, the reverse of the tool-input parts.
type ReasoningDeltaPart struct {
	Delta string
	ID    string
}

func (p ReasoningDeltaPart) PartType() string { return PartTypeReasoningDelta }

func (p ReasoningDeltaPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeReasoningDelta)
	w.str("delta", p.Delta)
	w.str("id", p.ID)
	return w.done()
}

// ReasoningDetailsView is a LIVE reference to the provider's
// `accumulatedReasoningDetails` array.
//
// This indirection is NOT decoration. In TS every emitted part shares the SAME
// array object, so a `reasoning-end` emitted at chunk 2 and serialised after the
// stream ends shows entries that only arrived at chunk 3. Verified against the
// running provider. Copying at emit time is the obvious Go implementation and
// is WRONG.
type ReasoningDetailsView struct {
	ref *[]ReasoningDetail
	own []ReasoningDetail
}

// DetailsValue builds a detached view, for hand-constructed parts and tests.
func DetailsValue(details ...ReasoningDetail) ReasoningDetailsView {
	return ReasoningDetailsView{own: details}
}

func detailsRef(ref *[]ReasoningDetail) ReasoningDetailsView {
	return ReasoningDetailsView{ref: ref}
}

// Slice resolves the view.
func (v ReasoningDetailsView) Slice() []ReasoningDetail {
	if v.ref != nil {
		return *v.ref
	}
	return v.own
}

// MarshalJSON always writes an array, never null: the empty case is meaningful
// (`:3905-3915`) — it signals "the provider produced no reasoning tokens this
// turn", which is a different statement from "no metadata".
func (v ReasoningDetailsView) MarshalJSON() ([]byte, error) {
	details := v.Slice()
	if details == nil {
		details = []ReasoningDetail{}
	}
	return json.Marshal(details)
}

// ReasoningEndPart carries the FULL accumulated reasoning_details array.
type ReasoningEndPart struct {
	ID      string
	Details ReasoningDetailsView
}

func (p ReasoningEndPart) PartType() string { return PartTypeReasoningEnd }

func (p ReasoningEndPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeReasoningEnd)
	w.str("id", p.ID)
	w.marshal("providerMetadata", reasoningProviderMetadata(p.Details))
	return w.done()
}

// reasoningProviderMetadata is `{openrouter:{reasoning_details:[...]}}`.
type reasoningProviderMetadataValue struct {
	Openrouter reasoningDetailsEnvelope `json:"openrouter"`
}

type reasoningDetailsEnvelope struct {
	ReasoningDetails ReasoningDetailsView `json:"reasoning_details"`
}

func reasoningProviderMetadata(details ReasoningDetailsView) reasoningProviderMetadataValue {
	return reasoningProviderMetadataValue{Openrouter: reasoningDetailsEnvelope{ReasoningDetails: details}}
}

// ── text ──────────────────────────────────────────────────────────────────

// TextStartPart's id is the OpenRouter response id (`gen-…`) when one has been
// seen, else a freshly minted 16-char id (`:3920`).
type TextStartPart struct{ ID string }

func (p TextStartPart) PartType() string { return PartTypeTextStart }

func (p TextStartPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeTextStart)
	w.str("id", p.ID)
	return w.done()
}

// TextDeltaPart is `{type:"text-delta", delta, id}`.
type TextDeltaPart struct {
	Delta string
	ID    string
}

func (p TextDeltaPart) PartType() string { return PartTypeTextDelta }

func (p TextDeltaPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeTextDelta)
	w.str("delta", p.Delta)
	w.str("id", p.ID)
	return w.done()
}

// TextEndPart is `{type:"text-end", id}`.
type TextEndPart struct{ ID string }

func (p TextEndPart) PartType() string { return PartTypeTextEnd }

func (p TextEndPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeTextEnd)
	w.str("id", p.ID)
	return w.done()
}

// ── source ────────────────────────────────────────────────────────────────

// SourcePart's `id` is THE URL ITSELF (`:3939`), not a generated id.
type SourcePart struct {
	URL        string
	Title      string
	Content    string
	StartIndex float64
	EndIndex   float64
}

func (p SourcePart) PartType() string { return PartTypeSource }

func (p SourcePart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeSource)
	w.str("sourceType", "url")
	w.str("id", p.URL)
	w.str("url", p.URL)
	w.str("title", p.Title)
	inner := newObjectWriter()
	inner.str("content", p.Content)
	inner.marshal("startIndex", jscompat.JSNumber(p.StartIndex))
	inner.marshal("endIndex", jscompat.JSNumber(p.EndIndex))
	innerRaw, err := inner.done()
	if err != nil {
		return nil, err
	}
	outer := newObjectWriter()
	outer.raw("openrouter", innerRaw)
	outerRaw, err := outer.done()
	if err != nil {
		return nil, err
	}
	w.raw("providerMetadata", outerRaw)
	return w.done()
}

// ── tool input / call ─────────────────────────────────────────────────────

// ToolInputStartPart is `{type:"tool-input-start", id, toolName}`.
type ToolInputStartPart struct {
	ID       string
	ToolName string
}

func (p ToolInputStartPart) PartType() string { return PartTypeToolInputStart }

func (p ToolInputStartPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeToolInputStart)
	w.str("id", p.ID)
	w.str("toolName", p.ToolName)
	return w.done()
}

// ToolInputDeltaPart is `{type:"tool-input-delta", id, delta}` — `id` first,
// unlike the text/reasoning deltas. `processor.ts:308-309` handles it as a
// no-op, so it exists purely for symmetry with the SDK's expectations.
type ToolInputDeltaPart struct {
	ID    string
	Delta string
}

func (p ToolInputDeltaPart) PartType() string { return PartTypeToolInputDelta }

func (p ToolInputDeltaPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeToolInputDelta)
	w.str("id", p.ID)
	w.str("delta", p.Delta)
	return w.done()
}

// ToolInputEndPart is `{type:"tool-input-end", id}`.
type ToolInputEndPart struct{ ID string }

func (p ToolInputEndPart) PartType() string { return PartTypeToolInputEnd }

func (p ToolInputEndPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeToolInputEnd)
	w.str("id", p.ID)
	return w.done()
}

// ToolCallPart's `input` is the RAW accumulated argument STRING, not a parsed
// object — `LanguageModelV3ToolCall.input: string`. `providerMetadata` is
// attached to the FIRST tool call only (`reasoningDetailsAttachedToToolCall`,
// `:3763`); later calls omit the key entirely.
type ToolCallPart struct {
	ToolCallID string
	ToolName   string
	Input      string

	HasProviderMetadata bool
	Details             ReasoningDetailsView
}

func (p ToolCallPart) PartType() string { return PartTypeToolCall }

func (p ToolCallPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeToolCall)
	w.str("toolCallId", p.ToolCallID)
	w.str("toolName", p.ToolName)
	w.str("input", p.Input)
	if p.HasProviderMetadata {
		w.marshal("providerMetadata", reasoningProviderMetadata(p.Details))
	}
	return w.done()
}

// ── file ──────────────────────────────────────────────────────────────────

// FilePart is `{type:"file", mediaType, data}` from `delta.images[]`.
type FilePart struct {
	MediaType string
	Data      string
}

func (p FilePart) PartType() string { return PartTypeFile }

func (p FilePart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeFile)
	w.str("mediaType", p.MediaType)
	w.str("data", p.Data)
	return w.done()
}

// ── error ─────────────────────────────────────────────────────────────────

// ErrorPart carries whatever the transform put in `error`. Three sources:
//
//	zod parse failure  → an AI_TypeValidationError / AI_JSONParseError object
//	`"error" in value` → the RAW `error` object from the chunk (passthrough)
//	reader error       → the caught exception, at flush
//
// The second case is the interesting one and is reproduced byte-for-byte:
// when the chunk parses as the CHUNK variant (because `choices` is present) the
// error object is whatever passthrough kept, un-normalised; when it parses as
// the ERROR variant, zod has applied `.default(null)` to `code`/`type`/`param`
// and reordered to shape order first, extras after.
type ErrorPart struct {
	Error json.RawMessage
}

func (p ErrorPart) PartType() string { return PartTypeError }

func (p ErrorPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeError)
	if p.Error == nil {
		w.raw("error", json.RawMessage("null"))
	} else {
		w.raw("error", p.Error)
	}
	return w.done()
}

// ── raw ───────────────────────────────────────────────────────────────────

// RawPart is emitted only when `includeRawChunks` is set. The SDK defaults it
// off (`ai/dist/index.mjs:6392`) and codeaf never sets it, so this is dead on
// the codeaf path — kept because it is the FIRST thing the transform does and
// omitting it would silently reorder every other part if the flag were ever
// turned on.
type RawPart struct {
	RawValue json.RawMessage
}

func (p RawPart) PartType() string { return PartTypeRaw }

func (p RawPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeRaw)
	w.raw("rawValue", p.RawValue)
	return w.done()
}

// AbortPart is the outer AI SDK's clean abort surface
// (`ai/dist/index.mjs:6842-6880`). The abort reason is serialized through
// getErrorMessage, so it is a string when present.
type AbortPart struct {
	Reason    string
	HasReason bool
}

func (p AbortPart) PartType() string { return PartTypeAbort }

func (p AbortPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeAbort)
	if p.HasReason {
		w.str("reason", p.Reason)
	}
	return w.done()
}

// ── finish ────────────────────────────────────────────────────────────────

// FinishReason is the provider's `{unified, raw}` pair (`:2600-2623`). `raw` is
// undefined when no `finish_reason` was ever seen, and survives the two
// synthetic promotions.
type FinishReason struct {
	Unified string
	Raw     *string
}

// MarshalJSON writes `{unified, raw?}`.
func (f FinishReason) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("unified", f.Unified)
	if f.Raw != nil {
		w.str("raw", *f.Raw)
	}
	return w.done()
}

// Unified finish-reason values (`@ai-sdk/provider/dist/index.d.ts:1771`).
const (
	FinishStop          = "stop"
	FinishLength        = "length"
	FinishContentFilter = "content-filter"
	FinishToolCalls     = "tool-calls"
	FinishError         = "error"
	FinishOther         = "other"
)

// MapToUnified is `mapToUnified` (`:2600-2614`). Everything unrecognised —
// including `null` and `"error"` — becomes "other". `finish_reason: "error"`
// mapping to `other` rather than `error` is not a typo; see ENGINE-DESIGN F2.
func MapToUnified(finishReason string) string {
	switch finishReason {
	case "stop":
		return FinishStop
	case "length":
		return FinishLength
	case "content_filter":
		return FinishContentFilter
	case "function_call", "tool_calls":
		return FinishToolCalls
	}
	return FinishOther
}

// MapOpenRouterFinishReason is `mapOpenRouterFinishReason`.
func MapOpenRouterFinishReason(finishReason *string) FinishReason {
	if finishReason == nil {
		return FinishReason{Unified: FinishOther}
	}
	raw := *finishReason
	return FinishReason{Unified: MapToUnified(raw), Raw: &raw}
}

// The `openrouterUsage` object (`:3812-3833`) is an ORDERED accumulator, not a
// struct: it is a plain JS object mutated across chunks, so a key that first
// appears on chunk 2 lands at the END, after keys chunk 1 already wrote. A
// stream that reports `prompt_tokens_details` only on its second usage chunk
// therefore serialises as
// `{promptTokens, completionTokens, totalTokens, promptTokensDetails}` — not the
// assignment order of a single chunk. Hence *Object, not a Go struct.
//
// codeaf ignores every field of it (ENGINE-DESIGN §3.5): `cost` never enters
// `LanguageModelV3Usage`, and `totalTokens` is discarded in favour of the SDK's
// `input + output` recomputation.

// FinishPart is the single terminal part, emitted from `flush`.
type FinishPart struct {
	FinishReason FinishReason
	Usage        calc.LanguageModelV3Usage
	Metadata     OpenRouterMetadata
}

// OpenRouterMetadata is `providerMetadata.openrouter` at flush (`:4160-4176`):
// `{usage}` first, then `provider` if the stream ever carried one, then
// `reasoning_details` (always), then `annotations` only when non-empty.
type OpenRouterMetadata struct {
	Usage            *Object
	Provider         *string
	ReasoningDetails ReasoningDetailsView
	Annotations      []json.RawMessage
}

func (m OpenRouterMetadata) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	usage := m.Usage
	if usage == nil {
		usage = NewObject()
	}
	w.marshal("usage", usage)
	if m.Provider != nil {
		w.str("provider", *m.Provider)
	}
	w.marshal("reasoning_details", m.ReasoningDetails)
	if len(m.Annotations) > 0 {
		w.marshal("annotations", m.Annotations)
	}
	return w.done()
}

func (p FinishPart) PartType() string { return PartTypeFinish }

func (p FinishPart) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", PartTypeFinish)
	w.marshal("finishReason", p.FinishReason)
	w.marshal("usage", p.Usage)
	inner := newObjectWriter()
	inner.marshal("openrouter", p.Metadata)
	innerRaw, err := inner.done()
	if err != nil {
		return nil, err
	}
	w.raw("providerMetadata", innerRaw)
	return w.done()
}
