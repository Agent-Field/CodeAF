package capability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ---------------------------------------------------------------------------
// A minimal model of an ORDINARY JS OBJECT, needed because capability.ts leaks
// JS object semantics into observable output in three places:
//
//  1. `snapshot()` builds `const cells: Record<string, [number,number][]> = {}`
//     and assigns `cells[modelID] = …`. `JSON.stringify` of that object emits
//     ARRAY-INDEX keys ("0", "1", "42", …) FIRST in ascending numeric order and
//     only then the remaining keys in insertion order. Go's `map[string]any`
//     marshals in sorted-string order, which is a different order for
//     e.g. {"10","2"}, so a plain map would silently break byte parity.
//
//  2. `cells["__proto__"] = …` does NOT create an own property — it invokes the
//     __proto__ setter — so a model literally named `__proto__` VANISHES from
//     the snapshot. `Set` reproduces that; `defineOwn` (the JSON.parse path,
//     which uses CreateDataProperty and therefore DOES create an own
//     `__proto__`) does not.
//
//  3. `loadOutcomes` returns the raw `JSON.parse` results typed as LeafOutcome.
//     Re-stringifying them reproduces the ORIGINAL key set and key order of
//     each JSONL line, not the LeafOutcome interface's declaration order — a
//     partial record like {"model":…,"sizeBand":"m","verdict":"pass"} round
//     trips with exactly those three keys. LeafOutcome therefore keeps the
//     parsed value and marshals from it (see leafoutcome.go).
//
// Nothing here is a "nicety": each of the three shows up in the golden
// fixtures.
type jsObject struct {
	keys []string
	vals map[string]any
}

func newJSObject() *jsObject { return &jsObject{vals: make(map[string]any)} }

// defineOwn is CreateDataProperty — what JSON.parse does for every key,
// INCLUDING "__proto__" (which becomes a real own property).
func (o *jsObject) defineOwn(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Set is `obj[k] = v` on an object-literal-created object: assigning to
// "__proto__" hits Object.prototype's accessor and creates NO own property.
func (o *jsObject) Set(k string, v any) {
	if k == "__proto__" {
		return
	}
	o.defineOwn(k, v)
}

func (o *jsObject) Get(k string) any {
	if o == nil {
		return nil
	}
	return o.vals[k]
}

func (o *jsObject) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the own enumerable keys in JS property order: array-index keys
// ascending first, then the rest in insertion order (OrdinaryOwnPropertyKeys).
func (o *jsObject) Keys() []string {
	if o == nil {
		return nil
	}
	idx := make([]string, 0, len(o.keys))
	rest := make([]string, 0, len(o.keys))
	for _, k := range o.keys {
		if isArrayIndexKey(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, _ := strconv.ParseUint(idx[i], 10, 64)
		b, _ := strconv.ParseUint(idx[j], 10, 64)
		return a < b
	})
	out := make([]string, 0, len(idx)+len(rest))
	out = append(out, idx...)
	out = append(out, rest...)
	return out
}

func (o *jsObject) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys() {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := jscompat.Stringify(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := jscompat.Stringify(o.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *jsObject) UnmarshalJSON(b []byte) error {
	v, err := parseJSValue(b)
	if err != nil {
		return err
	}
	obj, ok := v.(*jsObject)
	if !ok {
		return fmt.Errorf("capability: expected a JSON object")
	}
	o.keys = obj.keys
	o.vals = obj.vals
	return nil
}

// isArrayIndexKey mirrors the spec's "array index" test: ToString(ToUint32(P))
// === P and ToUint32(P) !== 2^32-1. "0" and "42" qualify; "01", "-1", "1.0",
// "" and "4294967295" do not.
func isArrayIndexKey(k string) bool {
	if k == "" || len(k) > 10 {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	if len(k) > 1 && k[0] == '0' {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil {
		return false
	}
	return n < 4294967295
}

// ---------------------------------------------------------------------------
// JSON.parse, preserving key order.

func parseJSValue(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeJSValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("capability: trailing data after JSON value")
	}
	return v, nil
}

func decodeJSValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := newJSObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeJSValue(dec)
				if err != nil {
					return nil, err
				}
				// JSON.parse uses CreateDataProperty, so "__proto__" lands as a
				// real own property here (unlike an object-literal assignment).
				obj.defineOwn(k, v)
			}
			if _, err := dec.Token(); err != nil { // closing '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeJSValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // closing ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("capability: unexpected delimiter %v", t)
	case json.Number:
		// JSON.parse produces IEEE doubles; "1e2" and "1.0" both become 1e2/1.
		f, err := strconv.ParseFloat(t.String(), 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case string:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil
	}
	return nil, fmt.Errorf("capability: unexpected token %v", tok)
}

// ---------------------------------------------------------------------------
// JS coercions used by `restore` (Number(x)) and by the tolerant guards.

// jsIsArray mirrors Array.isArray for the values that can reach `restore`:
// a decoded JSON array ([]any) or a Go slice built by `snapshot()`. A nil
// slice models JSON `null`, which Array.isArray rejects.
func jsIsArray(v any) ([]any, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case []any:
		return t, true
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, false
	}
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return nil, false
		}
	case reflect.Array:
	default:
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// jsIndex is `arr[i]`: out-of-range yields JS `undefined`, which Number()
// turns into NaN (whereas Number(null) is 0) — hence the second return value.
func jsIndex(arr []any, i int) (any, bool) {
	if i < 0 || i >= len(arr) {
		return nil, false
	}
	return arr[i], true
}

// jsNumber reproduces `Number(v)` for the value shapes a snapshot can carry.
func jsNumber(v any, defined bool) float64 {
	if !defined {
		return math.NaN() // Number(undefined)
	}
	switch t := v.(type) {
	case nil:
		return 0 // Number(null)
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return t
	case jscompat.JSNumber:
		return float64(t)
	case string:
		return jscompat.ToNumber(t)
	}
	if arr, ok := jsIsArray(v); ok {
		// Number(array) === Number(String(array)) === Number(array.join(",")).
		return jscompat.ToNumber(jsArrayToString(arr))
	}
	return math.NaN() // Number({}) — "[object Object]"
}

func jsArrayToString(arr []any) string {
	parts := make([]string, len(arr))
	for i, e := range arr {
		parts[i] = jsJoinElement(e)
	}
	return strings.Join(parts, ",")
}

func jsJoinElement(v any) string {
	switch t := v.(type) {
	case nil:
		return "" // null and undefined join as ""
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(t)
	case jscompat.JSNumber:
		return jscompat.FormatNumber(float64(t))
	case string:
		return t
	}
	if arr, ok := jsIsArray(v); ok {
		return jsArrayToString(arr)
	}
	return "[object Object]"
}

// ---------------------------------------------------------------------------
// Math / Object.prototype helpers.

// jsPow is Math.pow. Go's math.Pow short-circuits `Pow(1, y) == 1` BEFORE its
// NaN check, so math.Pow(1, NaN) is 1 while Math.pow(1, NaN) is NaN. That is
// reachable here: crossBandWeak/crossBandStrong may be 1 and the exponent is
// NaN whenever the observed band is an Object.prototype key (see bandIndex).
func jsPow(base, exp float64) float64 {
	if math.IsNaN(exp) {
		return math.NaN()
	}
	return math.Pow(base, exp)
}

// jsMax / jsMin are Math.max / Math.min: NaN in any argument wins. Go's
// math.Max returns +Inf for Max(NaN, +Inf); JS returns NaN.
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Max(a, b)
}

func jsMin(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Min(a, b)
}

// objectPrototypeKeys is Object.getOwnPropertyNames(Object.prototype) in
// Bun 1.2.23 / V8. Every plain-object lookup in capability.ts (BAND_INDEX,
// VERDICT_CREDIT, PRIOR_MEAN, the caller-supplied tierMap) walks the prototype
// chain, so these twelve strings are NOT `undefined` there.
var objectPrototypeKeys = map[string]bool{
	"toString":             true,
	"toLocaleString":       true,
	"valueOf":              true,
	"hasOwnProperty":       true,
	"propertyIsEnumerable": true,
	"isPrototypeOf":        true,
	"__defineGetter__":     true,
	"__defineSetter__":     true,
	"__lookupGetter__":     true,
	"__lookupSetter__":     true,
	"__proto__":            true,
	"constructor":          true,
}

func isObjectPrototypeKey(k string) bool { return objectPrototypeKeys[k] }
