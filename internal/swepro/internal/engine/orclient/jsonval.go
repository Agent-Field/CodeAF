package orclient

// An ordered JSON value model.
//
// Needed because three things in this package are order-sensitive in a way
// `map[string]any` cannot express:
//
//  1. `deterministicStringify` (`internal/index.mjs:2576-2596`) SORTS keys, so
//     it must be able to enumerate them in the order JS would — which is NOT
//     insertion order when a key looks like an array index (ES
//     OrdinaryOwnPropertyKeys: canonical numeric indices first, ascending, then
//     string keys in insertion order). Two keys that collate EQUAL under
//     localeCompare keep their relative enumeration order, because V8's sort is
//     stable — so the enumeration rule is observable.
//  2. The request body is `{...getArgs(options), ...restOpenrouterOptions, ...}`
//     — a shallow spread whose result key order is "getArgs' order, then any
//     option key getArgs did not already have". Byte equality of the body (and
//     therefore prompt-cache hit rate) depends on it.
//  3. remeda's `mergeDeep` builds `{...target, ...source}` and recurses, which
//     is the same rule one level down.
//
// Re-serialisation is JSON.stringify semantics, not encoding/json's: numbers go
// through `String(n)` (so `1.0` prints as `1` and `1e2` as `100`), strings
// through jscompat.Stringify (no HTML escaping), and a non-finite number — only
// reachable by parsing `1e400`, which JSON.parse maps to Infinity — prints as
// `null`.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
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
	// kindUndefined is the JS `undefined`. It is NOT producible by parsing —
	// it exists because `getArgs` writes ~25 keys whose value is
	// `this.settings.X` (all undefined for codeaf) and JSON.stringify drops
	// them WITHOUT giving up their position: a later `{...rest}` spread that
	// supplies one of those keys lands at the original slot, not at the end.
	// That is exactly how `usage` ends up between `messages` and `tools`.
	kindUndefined
)

// jsonValue is a JSON value with object key order preserved.
type jsonValue struct {
	Kind   jsonKind
	Bool   bool
	Number float64
	String string
	Array  []jsonValue
	Object []jsonMember
}

type jsonMember struct {
	Key   string
	Value jsonValue
}

// parseJSONValue decodes `raw` into the ordered model. Object members come out
// in ES enumeration order, not source order (see canonicalKeyOrder).
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
		// JSON.parse produces a double; an out-of-range literal becomes
		// ±Infinity rather than an error.
		f := jscompat.ToNumber(t.String())
		return jsonValue{Kind: kindNumber, Number: f}, nil
	case json.Delim:
		switch t {
		case '[':
			arr := []jsonValue{}
			for dec.More() {
				el, err := decodeJSONValue(dec)
				if err != nil {
					return jsonValue{}, err
				}
				arr = append(arr, el)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return jsonValue{}, err
			}
			return jsonValue{Kind: kindArray, Array: arr}, nil
		case '{':
			members := []jsonMember{}
			index := map[string]int{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return jsonValue{}, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return jsonValue{}, fmt.Errorf("expected object key, got %T", keyTok)
				}
				val, err := decodeJSONValue(dec)
				if err != nil {
					return jsonValue{}, err
				}
				// JSON.parse keeps the LAST occurrence of a duplicate key at
				// the FIRST occurrence's position, exactly like `o[k] = v`.
				if at, dup := index[key]; dup {
					members[at].Value = val
					continue
				}
				index[key] = len(members)
				members = append(members, jsonMember{Key: key, Value: val})
			}
			if _, err := dec.Token(); err != nil { // '}'
				return jsonValue{}, err
			}
			return jsonValue{Kind: kindObject, Object: canonicalKeyOrder(members)}, nil
		}
	}
	return jsonValue{}, fmt.Errorf("unexpected JSON token %v", tok)
}

// canonicalKeyOrder implements ES OrdinaryOwnPropertyKeys: every key that is a
// canonical array index (a decimal integer 0 ≤ n < 2^32-1 with no leading zero,
// no sign, no fraction) is enumerated FIRST in ascending numeric order, then
// every other key in insertion order.
//
// `Object.entries` / `for...in` / `JSON.stringify` all use it, so an object
// like `{"b":1,"2":2,"10":3,"a":4}` enumerates as 2, 10, b, a.
func canonicalKeyOrder(members []jsonMember) []jsonMember {
	numeric := false
	for _, m := range members {
		if _, ok := arrayIndex(m.Key); ok {
			numeric = true
			break
		}
	}
	if !numeric {
		return members
	}
	indexed := make([]jsonMember, 0, len(members))
	rest := make([]jsonMember, 0, len(members))
	keys := make(map[string]uint32, len(members))
	for _, m := range members {
		if n, ok := arrayIndex(m.Key); ok {
			keys[m.Key] = n
			indexed = append(indexed, m)
			continue
		}
		rest = append(rest, m)
	}
	sort.SliceStable(indexed, func(i, j int) bool {
		return keys[indexed[i].Key] < keys[indexed[j].Key]
	})
	return append(indexed, rest...)
}

func arrayIndex(key string) (uint32, bool) {
	if key == "" || len(key) > 10 {
		return 0, false
	}
	if key == "0" {
		return 0, true
	}
	if key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return uint32(n), true
}

// marshalJSONValue re-serialises with JSON.stringify semantics.
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
		b, err := jscompat.JSNumber(v.Number).MarshalJSON()
		if err != nil {
			return err
		}
		buf.Write(b)
	case kindString:
		b, err := jscompat.Stringify(v.String)
		if err != nil {
			return err
		}
		buf.Write(b)
	case kindUndefined:
		// Only reachable inside an array, where JSON.stringify writes null.
		buf.WriteString("null")
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
		first := true
		for _, m := range v.Object {
			// JSON.stringify drops undefined-valued keys — but the member
			// stays in the member list so a spread can overwrite it in place.
			if m.Value.Kind == kindUndefined {
				continue
			}
			if !first {
				buf.WriteByte(',')
			}
			first = false
			k, err := jscompat.Stringify(m.Key)
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

// ── deterministicStringify ────────────────────────────────────────────────

// DeterministicStringify is `deterministicStringify(value)`
// (`@openrouter/ai-sdk-provider/dist/internal/index.mjs:2576-2596`) — the
// recursive `localeCompare` key sort assistant `tool_calls[].function.arguments`
// go through, so that Anthropic-through-OpenRouter signature validation
// survives a round-trip.
//
// ENGINE-DESIGN R0: this must NOT be a byte-order sort. `_` vs `-`, digits vs
// letters, case and accents all collate differently under ICU, and tool
// argument keys are model-authored.
//
// The input is the already-parsed JS value; in Go it arrives as the raw JSON
// bytes of `ToolCallContent.Input`, so this parses first. An input that is not
// valid JSON is a caller bug — TS would have crashed at `JSON.parse` long
// before reaching here — and surfaces as an error rather than a silent
// passthrough.
func DeterministicStringify(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		// `deterministicStringify(undefined)` is `JSON.stringify(undefined)`,
		// which is the JS value `undefined` — and an `undefined` property is
		// dropped by the enclosing JSON.stringify. Callers model that by
		// omitting the field, so report the empty case rather than guessing.
		return nil, errUndefinedStringify
	}
	v, err := parseJSONValue(raw)
	if err != nil {
		return nil, err
	}
	return marshalJSONValue(sortKeys(v))
}

var errUndefinedStringify = errors.New("deterministicStringify: undefined value")

// sortKeys is the recursive half. Arrays recurse element-wise; objects sort
// their entries with `a.localeCompare(b)` and recurse into the values.
//
// `Array.prototype.sort` is stable in V8 and localeCompare returns 0 for keys
// that collate equal (e.g. "ab" vs "ab", since U+0000 is completely
// ignorable in the root collation) — hence sort.SliceStable over the
// enumeration order established by canonicalKeyOrder, never sort.Slice.
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
			return jscompat.LocaleCompare(members[i].Key, members[j].Key) < 0
		})
		for i := range members {
			members[i].Value = sortKeys(members[i].Value)
		}
		// The rebuilt `sorted` object is a FRESH object literal whose keys were
		// assigned in sorted order — so its own enumeration order is subject to
		// the array-index rule all over again when JSON.stringify walks it.
		return jsonValue{Kind: kindObject, Object: canonicalKeyOrder(members)}
	default:
		return v
	}
}

// ── ordered object helpers (the `{...a, ...b}` spread) ────────────────────

// Object is an insertion-ordered JSON object: the Go stand-in for a JS
// `Record<string, any>` whose key order reaches the wire.
type Object struct {
	members []jsonMember
	index   map[string]int
}

// NewObject builds an empty ordered object.
func NewObject() *Object { return &Object{index: map[string]int{}} }

// ParseObject decodes a JSON object literal into an ordered Object.
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
	o := NewObject()
	for _, m := range v.Object {
		o.set(m.Key, m.Value)
	}
	return o, nil
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

// Set assigns `o[key] = value` where value is raw JSON.
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

// SetUndefined reserves a key slot whose value is the JS `undefined`. The key
// is dropped by JSON.stringify but keeps its position for a later spread.
func (o *Object) SetUndefined(key string) {
	o.set(key, jsonValue{Kind: kindUndefined})
}

// SetNumberPtr writes a number, or reserves an undefined slot when nil.
func (o *Object) SetNumberPtr(key string, value *float64) {
	if value == nil {
		o.SetUndefined(key)
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

// Has reports `key in o`.
func (o *Object) Has(key string) bool {
	if o == nil || o.index == nil {
		return false
	}
	_, ok := o.index[key]
	return ok
}

// Keys returns the enumeration order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	out := make([]string, 0, len(o.members))
	for _, m := range o.enumeratedMembers() {
		out = append(out, m.Key)
	}
	return out
}

// Len is the number of own keys.
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
		return jsonValue{Kind: kindObject, Object: nil}
	}
	members := o.enumeratedMembers()
	return jsonValue{Kind: kindObject, Object: members}
}

func (o *Object) enumeratedMembers() []jsonMember {
	if o == nil {
		return nil
	}
	members := make([]jsonMember, len(o.members))
	copy(members, o.members)
	return canonicalKeyOrder(members)
}

// MarshalJSON writes the object in enumeration order with JSON.stringify
// number/string formatting.
func (o *Object) MarshalJSON() ([]byte, error) {
	return marshalJSONValue(o.value())
}

// Clone is a shallow copy sufficient for the spread semantics below (values are
// immutable in this model).
func (o *Object) Clone() *Object {
	out := NewObject()
	if o == nil {
		return out
	}
	for _, m := range o.enumeratedMembers() {
		out.set(m.Key, m.Value)
	}
	return out
}

// MergeOptions is `mergeOptions(target, source)` (`llm.ts:36-37`) — remeda's
// `mergeDeep(target, source ?? {})`.
//
// remeda (`dist/chunk-PDQFB3TV.js`):
//
//	let r = {...e, ...t};
//	for (let n in t) { if (!(n in e)) continue; ... if both plain objects, r[n] = s(i, c) }
//
// so: target's keys first in target's order, then source-only keys appended in
// source's order, and a key present in both with plain-object values on both
// sides recurses IN PLACE (keeping target's position).
func MergeOptions(target, source *Object) *Object {
	out := NewObject()
	if target != nil {
		for _, m := range target.enumeratedMembers() {
			out.set(m.Key, m.Value)
		}
	}
	if source == nil {
		return out
	}
	for _, m := range source.enumeratedMembers() {
		out.set(m.Key, m.Value)
	}
	for _, m := range source.enumeratedMembers() {
		if target == nil || !target.Has(m.Key) {
			continue
		}
		at := target.index[m.Key]
		left := target.members[at].Value
		if left.Kind != kindObject || m.Value.Kind != kindObject {
			continue
		}
		lo := objectFromValue(left)
		ro := objectFromValue(m.Value)
		out.set(m.Key, MergeOptions(lo, ro).value())
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

// jsString is JS `String(x)` for the values a JSON document can hold. Used by
// convertToOpenRouterChatMessages' `String(filename ?? "")`.
func jsString(v jsonValue) string {
	switch v.Kind {
	case kindNull:
		return "null"
	case kindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case kindNumber:
		return jscompat.FormatNumber(v.Number)
	case kindString:
		return v.String
	case kindArray:
		parts := make([]string, 0, len(v.Array))
		for _, el := range v.Array {
			if el.Kind == kindNull {
				parts = append(parts, "")
				continue
			}
			parts = append(parts, jsString(el))
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}
