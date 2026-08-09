package orclient

// reasoning_details — the round-trip ENGINE-DESIGN §3.4 pins.
//
// Three variants (`internal/index.mjs:2324-2421`), all sharing
// `{id?: string|null, format?: enum|null, index?: number}`:
//
//	reasoning.summary    summary: string
//	reasoning.encrypted  data: string
//	reasoning.text       text?: string|null, signature?: string|null
//
// `ReasoningDetailsWithUnknownSchema` (`:2423-2426`) maps any entry that
// matches none of the three to `null`, and `ReasoningDetailArraySchema`
// (`:2427`) filters the nulls — so a future variant is dropped SILENTLY and
// PER-ENTRY, never fatally.
//
// Two things make this more than a struct:
//
//   - zod's `.extend(CommonReasoningDetailSchema.shape)` produces a STRIP
//     object (the `.loose()` lives on Common, and `.extend` copies only the
//     shape), so parsing DROPS unknown keys and re-emits the known ones in
//     SCHEMA SHAPE ORDER, not input order. Verified against the running
//     provider: `{signature,index,text,format,type,id,zzz}` comes back as
//     `{type,text,signature,id,format,index}`.
//   - `text` / `signature` / `id` / `format` are `.nullish()`, so an explicit
//     `null` survives and is distinct from an absent key. They are therefore
//     json.RawMessage (nil == absent) rather than *string.

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Reasoning detail type tags.
const (
	ReasoningDetailSummary   = "reasoning.summary"
	ReasoningDetailEncrypted = "reasoning.encrypted"
	ReasoningDetailText      = "reasoning.text"
)

// DefaultReasoningFormat is `DEFAULT_REASONING_FORMAT` (`:2322`).
const DefaultReasoningFormat = "anthropic-claude-v1"

// reasoningFormats is the `ReasoningFormat` enum (`:2313-2320`). A `format`
// outside this set fails the member schema, which drops the whole entry.
var reasoningFormats = map[string]bool{
	"unknown":                   true,
	"openai-responses-v1":       true,
	"azure-openai-responses-v1": true,
	"xai-responses-v1":          true,
	"anthropic-claude-v1":       true,
	"google-gemini-v1":          true,
}

// ReasoningDetail is one parsed entry.
type ReasoningDetail struct {
	Type string

	// Summary is required for the summary variant.
	Summary string
	// Data is required for the encrypted variant.
	Data string
	// Text / Signature are `.nullish()` on the text variant.
	Text      json.RawMessage
	Signature json.RawMessage

	// Common, `.nullish()` / `.optional()`.
	ID     json.RawMessage
	Format json.RawMessage
	Index  *float64

	// Raw is the zod-normalized provider object. Once a detail crosses the
	// provider boundary it is opaque metadata; keeping these bytes avoids a
	// decode-to-map/re-encode round trip changing key order or number spelling.
	// It is cleared only when the provider's consecutive-text merge mutates
	// the detail.
	Raw json.RawMessage
}

// MarshalJSON writes the zod shape order: the variant's own keys first (as
// declared in the `z.object({...})` literal), then the three common ones from
// `.extend(CommonReasoningDetailSchema.shape)`. Absent keys are omitted;
// explicit nulls are written.
func (d ReasoningDetail) MarshalJSON() ([]byte, error) {
	if d.Raw != nil {
		return append([]byte(nil), d.Raw...), nil
	}
	return d.marshalNormalized()
}

func (d ReasoningDetail) marshalNormalized() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	write := func(key string, raw json.RawMessage) error {
		if raw == nil {
			return nil
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		k, err := jscompat.Stringify(key)
		if err != nil {
			return err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(raw)
		return nil
	}
	typeRaw, err := jscompat.Stringify(d.Type)
	if err != nil {
		return nil, err
	}
	if err := write("type", typeRaw); err != nil {
		return nil, err
	}
	switch d.Type {
	case ReasoningDetailSummary:
		s, err := jscompat.Stringify(d.Summary)
		if err != nil {
			return nil, err
		}
		if err := write("summary", s); err != nil {
			return nil, err
		}
	case ReasoningDetailEncrypted:
		s, err := jscompat.Stringify(d.Data)
		if err != nil {
			return nil, err
		}
		if err := write("data", s); err != nil {
			return nil, err
		}
	case ReasoningDetailText:
		if err := write("text", d.Text); err != nil {
			return nil, err
		}
		if err := write("signature", d.Signature); err != nil {
			return nil, err
		}
	}
	if err := write("id", d.ID); err != nil {
		return nil, err
	}
	if err := write("format", d.Format); err != nil {
		return nil, err
	}
	if d.Index != nil {
		idx, err := jscompat.JSNumber(*d.Index).MarshalJSON()
		if err != nil {
			return nil, err
		}
		if err := write("index", idx); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ParseReasoningDetails is `ReasoningDetailArraySchema` — parse each entry,
// map an unrecognised one to null, filter the nulls.
func ParseReasoningDetails(raw json.RawMessage) []ReasoningDetail {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	out := make([]ReasoningDetail, 0, len(entries))
	for _, entry := range entries {
		d, ok := parseReasoningDetail(entry)
		if !ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

func parseReasoningDetail(raw json.RawMessage) (ReasoningDetail, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ReasoningDetail{}, false
	}
	typeRaw, ok := obj["type"]
	if !ok {
		return ReasoningDetail{}, false
	}
	var typ string
	if err := json.Unmarshal(typeRaw, &typ); err != nil {
		return ReasoningDetail{}, false
	}

	d := ReasoningDetail{Type: typ}
	switch typ {
	case ReasoningDetailSummary:
		s, ok := requiredString(obj, "summary")
		if !ok {
			return ReasoningDetail{}, false
		}
		d.Summary = s
	case ReasoningDetailEncrypted:
		s, ok := requiredString(obj, "data")
		if !ok {
			return ReasoningDetail{}, false
		}
		d.Data = s
	case ReasoningDetailText:
		text, ok := nullishRaw(obj, "text")
		if !ok {
			return ReasoningDetail{}, false
		}
		sig, ok := nullishRaw(obj, "signature")
		if !ok {
			return ReasoningDetail{}, false
		}
		d.Text = text
		d.Signature = sig
	default:
		return ReasoningDetail{}, false
	}

	id, ok := nullishRaw(obj, "id")
	if !ok {
		return ReasoningDetail{}, false
	}
	d.ID = id

	format, ok := nullishRaw(obj, "format")
	if !ok {
		return ReasoningDetail{}, false
	}
	if format != nil && !bytes.Equal(format, []byte("null")) {
		var f string
		if err := json.Unmarshal(format, &f); err != nil || !reasoningFormats[f] {
			return ReasoningDetail{}, false
		}
	}
	d.Format = format

	if idxRaw, present := obj["index"]; present {
		var idx float64
		if err := json.Unmarshal(idxRaw, &idx); err != nil {
			return ReasoningDetail{}, false
		}
		d.Index = &idx
	}
	normalized, err := d.marshalNormalized()
	if err != nil {
		return ReasoningDetail{}, false
	}
	d.Raw = normalized
	return d, true
}

func requiredString(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, present := obj[key]
	if !present {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// nullishRaw accepts absent (nil, true), explicit null, or a string.
func nullishRaw(obj map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	raw, present := obj[key]
	if !present {
		return nil, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("null"), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	enc, err := jscompat.Stringify(s)
	if err != nil {
		return nil, false
	}
	return enc, true
}

// jsOr is JS `a || b` over a nullish JSON field: `null`, `""` and an absent
// value are all falsy, so they fall through to b. The result may be nil
// (undefined), which the enclosing JSON.stringify drops — which is how a
// merge can turn an explicit `null` signature into an ABSENT one.
func jsOr(a, b json.RawMessage) json.RawMessage {
	if rawTruthy(a) {
		return a
	}
	return b
}

func rawTruthy(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == 0:
		return false
	case bytes.Equal(trimmed, []byte("null")):
		return false
	case bytes.Equal(trimmed, []byte(`""`)):
		return false
	case bytes.Equal(trimmed, []byte("false")):
		return false
	case bytes.Equal(trimmed, []byte("0")):
		return false
	}
	return true
}

// rawString unwraps a nullish JSON string to its Go value; `null` and absent
// both give "".
func rawString(raw json.RawMessage) string {
	if !rawTruthy(raw) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// rawStringLiteral encodes a Go string as a JSON string.
func rawStringLiteral(s string) json.RawMessage {
	enc, err := jscompat.Stringify(s)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return enc
}
