package knobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ── JS plain-object image ────────────────────────────────────────────────
//
// knobs.ts leans on three JS-object behaviours that a bare Go map destroys:
//
//   1. `Record<string, number>` / `Record<string, source>` values are returned
//      to callers and JSON.stringify'd, so key order is observable and equals
//      insertion order.
//   2. loadKnobFile returns the raw JSON.parse result, so its key order is
//      V8's: array-index-like keys ("0", "7", up to 2^32-2) iterate first in
//      ASCENDING NUMERIC order, then every other key in insertion order.
//      Nested objects get the same treatment.
//   3. Duplicate keys in the parsed JSON collapse to the LAST value while
//      keeping the FIRST occurrence's position.
//
// Record is the insertion-ordered image of such an object; it wraps
// jscompat.OrderedMap and adds JSON.stringify-compatible marshalling (V8
// number formatting, non-finite → null, no HTML escaping).

// Record mirrors a JS object used as a string-keyed dictionary. The zero value
// is not usable; call NewRecord. A nil *Record reads as an empty object, which
// is what a TS `= {}` default parameter produces.
type Record[V any] struct {
	om *jscompat.OrderedMap[string, V]
}

func NewRecord[V any]() *Record[V] {
	return &Record[V]{om: jscompat.NewOrderedMap[string, V]()}
}

// Set stores k → v. An existing key keeps its original position (JS object
// property-assignment semantics), matching jscompat.OrderedMap.
func (r *Record[V]) Set(k string, v V) { r.om.Set(k, v) }

// Get returns the value for k. A missing key (or a nil receiver — the `= {}`
// default) reports ok=false, standing in for JS `undefined`.
func (r *Record[V]) Get(k string) (V, bool) {
	if r == nil || r.om == nil {
		var zero V
		return zero, false
	}
	return r.om.Get(k)
}

func (r *Record[V]) Has(k string) bool {
	_, ok := r.Get(k)
	return ok
}

// Keys mirrors Object.keys(obj): insertion order, already normalised for the
// array-index-first rule at construction time.
func (r *Record[V]) Keys() []string {
	if r == nil || r.om == nil {
		return nil
	}
	return r.om.Keys()
}

func (r *Record[V]) Len() int {
	if r == nil || r.om == nil {
		return 0
	}
	return r.om.Len()
}

func (r *Record[V]) Entries() []jscompat.Entry[string, V] {
	if r == nil || r.om == nil {
		return nil
	}
	return r.om.Entries()
}

// MarshalJSON reproduces JSON.stringify of a plain object: keys in iteration
// order, numbers through V8's formatter (so -0 prints "0" where Go's encoder
// would print "-0"), NaN/±Infinity as null.
func (r *Record[V]) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	if r != nil && r.om != nil {
		for i, k := range r.om.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(jsonString(k))
			b.WriteByte(':')
			v, _ := r.om.Get(k)
			vb, err := marshalJSValue(any(v))
			if err != nil {
				return nil, err
			}
			b.Write(vb)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalJSValue encodes one value of the JSON.parse value model
// (nil | bool | float64 | string | []any | *Record[any]) plus the concrete
// value types this package stores in Records.
func marshalJSValue(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return []byte("null"), nil
	case float64:
		return jscompat.JSNumber(t).MarshalJSON()
	case string:
		return jsonString(t), nil
	case KnobSource:
		return jsonString(string(t)), nil
	case bool:
		if t {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, el := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			eb, err := marshalJSValue(el)
			if err != nil {
				return nil, err
			}
			b.Write(eb)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	case *Record[any]:
		return t.MarshalJSON()
	default:
		return jscompat.Stringify(v)
	}
}

// jsonString quotes a string exactly as JSON.stringify's QuoteJSONString does.
// jscompat.Stringify cannot be used here: Go's encoder spells U+0008 and
// U+000C with long six-character escapes where JS uses the short backslash-b
// and backslash-f forms, and it escapes
// U+2028/U+2029 where JS emits them raw. Those bytes reach the output whenever
// LoadKnobFile echoes a knobs.json string, so the divergence is real.
func jsonString(s string) []byte {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				const hex = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.Bytes()
}

// ── JSON.parse image ─────────────────────────────────────────────────────

var errBadJSON = errors.New("knobs: malformed JSON")

// parseJSON reproduces JSON.parse for the value model above. It is only used by
// LoadKnobFile, which swallows every error, so failures collapse to ok=false.
//
// Deliberate fidelity choices:
//   - numbers are decoded from their literal with strconv.ParseFloat and an
//     out-of-range result is KEPT (1e999 → +Inf, 1e-999 → 0), because that is
//     what JSON.parse does; Go's plain json.Unmarshal would error instead.
//   - object keys are reordered array-index-first, recursively.
func parseJSON(text string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := parseJSONValue(dec)
	if err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return v, true
}

func parseJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewRecord[any]()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, errBadJSON
				}
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				// Duplicate key: last value wins, first position kept.
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return reorderJSObject(obj), nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := parseJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, errBadJSON
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			var numErr *strconv.NumError
			// Overflow/underflow are not errors in JSON.parse: 1e999 is
			// Infinity and 1e-999 is 0 (sign preserved).
			if !errors.As(err, &numErr) || !errors.Is(numErr.Err, strconv.ErrRange) {
				return nil, err
			}
		}
		return f, nil
	case string:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil
	}
	return nil, errBadJSON
}

// reorderJSObject applies V8's own-property enumeration order to a freshly
// parsed object: array-index keys ascending, then the rest in insertion order.
func reorderJSObject(obj *Record[any]) *Record[any] {
	keys := obj.Keys()
	var idx []string
	var rest []string
	for _, k := range keys {
		if _, ok := arrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return obj
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, _ := arrayIndex(idx[i])
		b, _ := arrayIndex(idx[j])
		return a < b
	})
	out := NewRecord[any]()
	for _, k := range idx {
		v, _ := obj.Get(k)
		out.Set(k, v)
	}
	for _, k := range rest {
		v, _ := obj.Get(k)
		out.Set(k, v)
	}
	return out
}

// arrayIndex reports whether s is a canonical array index string, i.e. the
// decimal form of a uint32 in [0, 2^32-2]. "01", "1.0", "-1", " 1" and
// "4294967295" are not.
func arrayIndex(s string) (uint32, bool) {
	if s == "" || len(s) > 10 {
		return 0, false
	}
	if s == "0" {
		return 0, true
	}
	if s[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return uint32(n), true
}

// ── string ordering ──────────────────────────────────────────────────────

// lessUTF16 orders two strings the way Array.prototype.sort's default
// comparator does: by UTF-16 code unit, not by UTF-8 byte. The two agree on
// everything below U+FFFF but invert above it — "\u{10000}" sorts BEFORE
// "￿" in JS (its lead surrogate is 0xD800) and AFTER it in Go byte order.
func lessUTF16(a, b string) bool {
	if isASCII(a) && isASCII(b) {
		return a < b
	}
	au := utf16.Encode([]rune(a))
	bu := utf16.Encode([]rune(b))
	n := len(au)
	if len(bu) < n {
		n = len(bu)
	}
	for i := 0; i < n; i++ {
		if au[i] != bu[i] {
			return au[i] < bu[i]
		}
	}
	return len(au) < len(bu)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
