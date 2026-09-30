//go:build !windows

package orclient

// An insertion-ordered JSON value model.
//
// The request body, the messages inside it and the tool definitions are
// built with this model instead of map[string]any so that the same input
// always produces the same bytes: key order is the order the code writes
// keys in (or, for DeterministicStringify, sorted), which keeps prompt-cache
// keys stable across calls. Numbers parsed from JSON keep their original
// literal so a re-encoded document does not drift.

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// jsonKind tags a jsonValue.
type jsonKind uint8

const (
	kindNull jsonKind = iota
	kindBool
	kindNumber
	kindString
	kindArray
	kindObject
)

// jsonValue is a JSON value with object key order preserved.
type jsonValue struct {
	Kind   jsonKind
	Bool   bool
	Number float64
	// Literal is the number's original JSON text when it was parsed rather
	// than computed; it is re-emitted verbatim.
	Literal string
	String  string
	Array   []jsonValue
	Object  []jsonMember
}

type jsonMember struct {
	Key   string
	Value jsonValue
}

// parseJSONValue decodes raw into the ordered model.
func parseJSONValue(raw []byte) (jsonValue, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeJSONValue(dec)
	if err != nil {
		return jsonValue{}, err
	}
	if _, err := dec.Token(); err == nil {
		return jsonValue{}, errors.New("unexpected trailing JSON content")
	}
	return v, nil
}

func decodeJSONValue(dec *json.Decoder) (jsonValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return jsonValue{}, err
	}
	return decodeJSONValueFrom(dec, tok)
}

func decodeJSONValueFrom(dec *json.Decoder, tok json.Token) (jsonValue, error) {
	switch t := tok.(type) {
	case nil:
		return jsonValue{Kind: kindNull}, nil
	case bool:
		return jsonValue{Kind: kindBool, Bool: t}, nil
	case string:
		return jsonValue{Kind: kindString, String: t}, nil
	case json.Number:
		f, _ := strconv.ParseFloat(t.String(), 64)
		return jsonValue{Kind: kindNumber, Number: f, Literal: t.String()}, nil
	case json.Delim:
		switch t {
		case '[':
			items := []jsonValue{}
			for dec.More() {
				item, err := decodeJSONValue(dec)
				if err != nil {
					return jsonValue{}, err
				}
				items = append(items, item)
			}
			if _, err := dec.Token(); err != nil {
				return jsonValue{}, err
			}
			return jsonValue{Kind: kindArray, Array: items}, nil
		case '{':
			var members []jsonMember
			index := map[string]int{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return jsonValue{}, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return jsonValue{}, errors.New("expected object key")
				}
				value, err := decodeJSONValue(dec)
				if err != nil {
					return jsonValue{}, err
				}
				// A duplicate key keeps its first position and takes the
				// later value.
				if at, seen := index[key]; seen {
					members[at].Value = value
					continue
				}
				index[key] = len(members)
				members = append(members, jsonMember{Key: key, Value: value})
			}
			if _, err := dec.Token(); err != nil {
				return jsonValue{}, err
			}
			return jsonValue{Kind: kindObject, Object: members}, nil
		}
	}
	return jsonValue{}, errors.New("unexpected JSON token")
}

// marshalJSONValue encodes a value compactly, without HTML escaping.
func marshalJSONValue(v jsonValue) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSONValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSONValue(buf *bytes.Buffer, v jsonValue) error {
	switch v.Kind {
	case kindNull:
		buf.WriteString("null")
	case kindBool:
		if v.Bool {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case kindNumber:
		if v.Literal != "" {
			buf.WriteString(v.Literal)
		} else {
			b, err := json.Marshal(v.Number)
			if err != nil {
				return err
			}
			buf.Write(b)
		}
	case kindString:
		b, err := jsonutil.Marshal(v.String)
		if err != nil {
			return err
		}
		buf.Write(b)
	case kindArray:
		buf.WriteByte('[')
		for i, el := range v.Array {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONValue(buf, el); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case kindObject:
		buf.WriteByte('{')
		for i, m := range v.Object {
			if i > 0 {
				buf.WriteByte(',')
			}
			k, err := jsonutil.Marshal(m.Key)
			if err != nil {
				return err
			}
			buf.Write(k)
			buf.WriteByte(':')
			if err := writeJSONValue(buf, m.Value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	}
	return nil
}

// ── DeterministicStringify ────────────────────────────────────────────────

// DeterministicStringify re-encodes a JSON document with every object's keys
// sorted, recursively. Assistant tool-call arguments go through it before
// they are sent back to the provider so the same call always serialises the
// same way. An empty input is reported rather than encoded, since the caller
// omits the field in that case.
func DeterministicStringify(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errUndefinedStringify
	}
	v, err := parseJSONValue(raw)
	if err != nil {
		return nil, err
	}
	return marshalJSONValue(sortKeys(v))
}

var errUndefinedStringify = errors.New("deterministicStringify: undefined value")

// sortKeys sorts object keys recursively; arrays recurse element-wise.
func sortKeys(v jsonValue) jsonValue {
	switch v.Kind {
	case kindArray:
		out := make([]jsonValue, len(v.Array))
		for i, el := range v.Array {
			out[i] = sortKeys(el)
		}
		return jsonValue{Kind: kindArray, Array: out}
	case kindObject:
		members := make([]jsonMember, len(v.Object))
		copy(members, v.Object)
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].Key < members[j].Key
		})
		for i := range members {
			members[i].Value = sortKeys(members[i].Value)
		}
		return jsonValue{Kind: kindObject, Object: members}
	default:
		return v
	}
}

// ── ordered object builder ────────────────────────────────────────────────

// Object is an insertion-ordered JSON object. Setting an existing key
// replaces its value in place.
type Object struct {
	members []jsonMember
	index   map[string]int
}

// NewObject builds an empty ordered object.
func NewObject() *Object { return &Object{index: map[string]int{}} }

// ParseObject decodes a JSON object literal into an ordered Object. Empty
// input yields an empty object.
func ParseObject(raw []byte) (*Object, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return NewObject(), nil
	}
	v, err := parseJSONValue(raw)
	if err != nil {
		return nil, err
	}
	if v.Kind != kindObject {
		return nil, errors.New("orclient: expected a JSON object")
	}
	return objectFromValue(v), nil
}

func (o *Object) set(key string, val jsonValue) {
	if o.index == nil {
		o.index = map[string]int{}
	}
	if at, ok := o.index[key]; ok {
		o.members[at].Value = val
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, jsonMember{Key: key, Value: val})
}

// Set assigns raw JSON to key.
func (o *Object) Set(key string, raw json.RawMessage) error {
	v, err := parseJSONValue(raw)
	if err != nil {
		return err
	}
	o.set(key, v)
	return nil
}

// SetString assigns a string value.
func (o *Object) SetString(key, value string) {
	o.set(key, jsonValue{Kind: kindString, String: value})
}

// SetNumber assigns a numeric value.
func (o *Object) SetNumber(key string, value float64) {
	o.set(key, jsonValue{Kind: kindNumber, Number: value})
}

// SetBool assigns a boolean value.
func (o *Object) SetBool(key string, value bool) {
	o.set(key, jsonValue{Kind: kindBool, Bool: value})
}

// SetObject assigns a nested ordered object.
func (o *Object) SetObject(key string, value *Object) {
	o.set(key, value.value())
}

// SetNumberPtr assigns a number, or leaves the key absent when value is nil.
func (o *Object) SetNumberPtr(key string, value *float64) {
	if value == nil {
		return
	}
	o.SetNumber(key, *value)
}

// SetArray assigns an array of already-built ordered objects.
func (o *Object) SetArray(key string, items []*Object) {
	vals := make([]jsonValue, 0, len(items))
	for _, it := range items {
		vals = append(vals, it.value())
	}
	o.set(key, jsonValue{Kind: kindArray, Array: vals})
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	if o == nil || o.index == nil {
		return false
	}
	_, ok := o.index[key]
	return ok
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	out := make([]string, 0, len(o.members))
	for _, m := range o.members {
		out = append(out, m.Key)
	}
	return out
}

// Len is the number of keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.members)
}

// Get returns the raw JSON of one key.
func (o *Object) Get(key string) (json.RawMessage, bool) {
	if o == nil || o.index == nil {
		return nil, false
	}
	at, ok := o.index[key]
	if !ok {
		return nil, false
	}
	b, err := marshalJSONValue(o.members[at].Value)
	if err != nil {
		return nil, false
	}
	return b, true
}

func (o *Object) value() jsonValue {
	if o == nil {
		return jsonValue{Kind: kindObject}
	}
	members := make([]jsonMember, len(o.members))
	copy(members, o.members)
	return jsonValue{Kind: kindObject, Object: members}
}

// MarshalJSON writes the object in insertion order.
func (o *Object) MarshalJSON() ([]byte, error) {
	return marshalJSONValue(o.value())
}

// Clone is a shallow copy (values are immutable in this model).
func (o *Object) Clone() *Object {
	out := NewObject()
	if o == nil {
		return out
	}
	for _, m := range o.members {
		out.set(m.Key, m.Value)
	}
	return out
}

// Without is a copy of the object with the named keys left out, in the order
// the rest were set.
func (o *Object) Without(keys ...string) *Object {
	drop := make(map[string]bool, len(keys))
	for _, key := range keys {
		drop[key] = true
	}
	out := NewObject()
	if o == nil {
		return out
	}
	for _, m := range o.members {
		if !drop[m.Key] {
			out.set(m.Key, m.Value)
		}
	}
	return out
}

// MergeOptions deep-merges source into target: target's keys come first in
// their own order, source-only keys are appended in source order, and a key
// whose value is an object on both sides is merged recursively in place.
func MergeOptions(target, source *Object) *Object {
	out := NewObject()
	if target != nil {
		for _, m := range target.members {
			out.set(m.Key, m.Value)
		}
	}
	if source == nil {
		return out
	}
	for _, m := range source.members {
		out.set(m.Key, m.Value)
	}
	for _, m := range source.members {
		if target == nil || !target.Has(m.Key) {
			continue
		}
		left := target.members[target.index[m.Key]].Value
		if left.Kind != kindObject || m.Value.Kind != kindObject {
			continue
		}
		out.set(m.Key, MergeOptions(objectFromValue(left), objectFromValue(m.Value)).value())
	}
	return out
}

func objectFromValue(v jsonValue) *Object {
	o := NewObject()
	for _, m := range v.Object {
		o.set(m.Key, m.Value)
	}
	return o
}

// stringValue is a small helper for building literal JSON in the body writer.
func stringValue(s string) jsonValue { return jsonValue{Kind: kindString, String: s} }

func rawJSONValue(raw json.RawMessage) jsonValue {
	v, err := parseJSONValue(raw)
	if err != nil {
		return jsonValue{Kind: kindNull}
	}
	return v
}

// textOf renders a JSON scalar as plain text: strings verbatim, numbers and
// booleans as their literals, null as "null". Arrays join their elements
// with commas; objects have no useful text form.
func textOf(v jsonValue) string {
	switch v.Kind {
	case kindNull:
		return "null"
	case kindBool:
		return strconv.FormatBool(v.Bool)
	case kindNumber:
		if v.Literal != "" {
			return v.Literal
		}
		return strconv.FormatFloat(v.Number, 'f', -1, 64)
	case kindString:
		return v.String
	case kindArray:
		parts := make([]string, 0, len(v.Array))
		for _, el := range v.Array {
			if el.Kind == kindNull {
				parts = append(parts, "")
				continue
			}
			parts = append(parts, textOf(el))
		}
		return strings.Join(parts, ",")
	default:
		return ""
	}
}
