//go:build !windows

package msgmodel

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// RawObject is a JSON object kept as the verbatim bytes it arrived as. Never
// decode one into map[string]any: Go sorts map keys on re-marshal, and the
// doom-loop guard compares tool inputs byte for byte, key order included.
//
// A zero-length RawObject is an absent value. On an OPTIONAL field (tagged
// `omitempty`) that means the key is omitted; on a REQUIRED field it marshals
// as `{}`, which is what the processor writes for an empty input/metadata.
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

// Raw returns the underlying bytes, or nil when the value was absent: the
// reading for an OPTIONAL field.
func (r RawObject) Raw() json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	return json.RawMessage(r)
}

// Value returns `{}` for an absent value: the reading for a REQUIRED field
// (`input`, ToolStateCompleted.metadata), which is always at least an empty
// object.
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
// Returns ok=false when the value is not an object; callers treat that as
// "absent".
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
	// Reject trailing garbage.
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

// Field returns the raw value at key, or ok=false when absent (or when the
// receiver is not an object).
func (r RawObject) Field(key string) (json.RawMessage, bool) {
	var (
		value json.RawMessage
		found bool
	)
	// A duplicate key in the source text means the LAST one wins, so scan to
	// the end.
	for _, f := range r.Fields() {
		if f.Key == key {
			value, found = f.Value, true
		}
	}
	return value, found
}

// Truthy applies truthyJSON to the value at key; an absent key is false.
func (r RawObject) Truthy(key string) bool {
	value, ok := r.Field(key)
	if !ok {
		return false
	}
	return truthyJSON(value)
}

// StrictTrue reports whether the value at key is exactly `true`.
func (r RawObject) StrictTrue(key string) bool {
	value, ok := r.Field(key)
	if !ok {
		return false
	}
	return string(bytes.TrimSpace(value)) == "true"
}

// StringField returns the value at key when it is a string.
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

// truthyJSON is the truthiness rule for a JSON value: objects and arrays are
// always truthy; "" / 0 / -0 / false / null are not.
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
	f, err := strconv.ParseFloat(string(trimmed), 64)
	return err == nil && f != 0
}

// providerMeta is the tool metadata minus its providerExecuted key, or nil
// when nothing else is there. The surviving keys keep their original order,
// so this rebuilds the object from the token walk instead of decoding into a
// map.
func providerMeta(metadata RawObject) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	fields, ok := objectFields(metadata)
	if !ok {
		// Non-object metadata has no keys to keep.
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
		key, err := jsonutil.Marshal(f.Key)
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

// SameInput is the doom-loop equality test: two tool inputs compared as
// stored bytes. Both sides are already verbatim, so this only has to
// normalise insignificant whitespace; key order is deliberately NOT
// normalised.
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
