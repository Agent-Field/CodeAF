package msgmodel

import (
	"bytes"
	"encoding/json"
	"io"
	"math"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// RawObject is a TS `Record<string, any>` kept as the verbatim JSON bytes it
// arrived as (ENGINE-DESIGN §5.2). Never decode one into map[string]any: Go
// sorts map keys on re-marshal, JS preserves insertion order, and the
// doom-loop guard compares tool inputs by JSON.stringify equality
// (processor.ts:356-364).
//
// A zero-length RawObject is TS `undefined`. On an OPTIONAL field (tagged
// `omitempty`) that means the key is absent; on a REQUIRED field it marshals
// as `{}`, which is what processor.ts writes for an empty input/metadata.
type RawObject json.RawMessage

func (r RawObject) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("{}"), nil
	}
	return []byte(r), nil
}

func (r *RawObject) UnmarshalJSON(b []byte) error {
	*r = RawObject(append([]byte(nil), b...))
	return nil
}

// Raw returns the underlying bytes, or nil when the value was absent — the
// reading appropriate for an OPTIONAL TS field.
func (r RawObject) Raw() json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	return json.RawMessage(r)
}

// Value returns `{}` for an absent value — the reading appropriate for a
// REQUIRED TS field (`input`, ToolStateCompleted.metadata), which effect
// Schema guarantees is at least an empty object.
func (r RawObject) Value() json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(r)
}

func trimSpace(b []byte) []byte { return bytes.TrimSpace(b) }

// arrayElements walks a JSON array at the token level, keeping each element's
// bytes verbatim. ok=false when the value is not an array.
func arrayElements(raw []byte) ([]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var out []json.RawMessage
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil, false
	}
	return out, true
}

// RawField is one own property of a JSON object, in source order.
type RawField struct {
	Key   string
	Value json.RawMessage
}

// objectFields walks a JSON object at the token level so key ORDER survives.
// Returns ok=false when the value is not an object (JS `x?.y` on a non-object
// yields undefined rather than throwing, so callers treat that as "absent").
func objectFields(raw []byte) ([]RawField, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, false
	}
	var out []RawField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		out = append(out, RawField{Key: key, Value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	// Reject trailing garbage the way a JSON.parse would.
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return out, true
}

// Fields returns the object's own properties in insertion order, or nil when
// the value is absent or not an object.
func (r RawObject) Fields() []RawField {
	fields, ok := objectFields(r)
	if !ok {
		return nil
	}
	return fields
}

// Field is `obj?.key`: the raw value, or ok=false when absent (or when the
// receiver is not an object).
func (r RawObject) Field(key string) (json.RawMessage, bool) {
	var (
		value json.RawMessage
		found bool
	)
	// JS objects have one binding per key; a duplicate key in the source text
	// means the LAST one wins, so scan to the end.
	for _, f := range r.Fields() {
		if f.Key == key {
			value, found = f.Value, true
		}
	}
	return value, found
}

// Truthy is JS `!!obj?.key`.
func (r RawObject) Truthy(key string) bool {
	value, ok := r.Field(key)
	if !ok {
		return false
	}
	return truthyJSON(value)
}

// StrictTrue is JS `obj?.key === true`.
func (r RawObject) StrictTrue(key string) bool {
	value, ok := r.Field(key)
	if !ok {
		return false
	}
	return string(bytes.TrimSpace(value)) == "true"
}

// StringField is `typeof obj?.key === "string" ? obj.key : undefined`.
func (r RawObject) StringField(key string) (string, bool) {
	value, ok := r.Field(key)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return "", false
	}
	return s, true
}

// truthyJSON applies JS truthiness to a decoded JSON value. Objects and
// arrays are always truthy; "" / 0 / -0 / false / null are not. NaN cannot
// appear in JSON.
func truthyJSON(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == 0:
		return false
	case string(trimmed) == "null", string(trimmed) == "false":
		return false
	case string(trimmed) == "true":
		return true
	case trimmed[0] == '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return false
		}
		return s != ""
	case trimmed[0] == '{' || trimmed[0] == '[':
		return true
	}
	f := jscompat.ToNumber(string(trimmed))
	return f != 0 && !math.IsNaN(f)
}

// providerMeta is message-v2.ts:723-727 — `const {providerExecuted: _, ...rest}
// = metadata; return Object.keys(rest).length > 0 ? rest : undefined`. The rest
// spread keeps the surviving keys in their original order, so this rebuilds the
// object from the token walk instead of decoding into a map.
func providerMeta(metadata RawObject) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	fields, ok := objectFields(metadata)
	if !ok {
		// Non-object metadata: `{...rest}` of a primitive yields {}, whose key
		// count is 0, so TS returns undefined.
		return nil
	}
	kept := make([]RawField, 0, len(fields))
	for _, f := range fields {
		if f.Key == "providerExecuted" {
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range kept {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := jscompat.Stringify(f.Key)
		if err != nil {
			return nil
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(f.Value)
	}
	buf.WriteByte('}')
	return json.RawMessage(buf.Bytes())
}

// SameInput is the doom-loop equality test (processor.ts:363,
// ENGINE-DESIGN §5.5): `JSON.stringify(a) === JSON.stringify(b)`. Both sides
// are already the verbatim stored bytes, so this only has to normalise
// insignificant whitespace — key order is deliberately NOT normalised.
func SameInput(a, b RawObject) bool {
	return bytes.Equal(compactRaw(a), compactRaw(b))
}

func compactRaw(r RawObject) []byte {
	raw, err := r.MarshalJSON()
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}
