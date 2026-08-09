package adaptive

// The classifiers in adaptive.ts all take `err: unknown` and probe it with
// `instanceof Error`, `typeof err === "object"`, bracket property access and
// `JSON.stringify`. Go has no `unknown`, so this file models the subset of the
// JS value lattice those probes can distinguish. Everything the TS module can
// observe about an error value — its message, its `cause` chain, an arbitrary
// `name`, numeric `status`/`statusCode`/`status_code` props, and the exact
// JSON.stringify text of a plain object/array — round-trips through JSValue.
//
// NOT modelled (deliberate, documented in the port report):
//   - functions and symbols, for which JSON.stringify returns `undefined` and
//     the TS `errorText` then hands `undefined` to `.toLowerCase()`, throwing.
//   - circular objects, which make JSON.stringify throw and errorText fall
//     back to `String(err)`.
//
// Both are unreachable from every real call site (the argument is always a
// caught Error or an OpenRouter JSON body).

import (
	"bytes"
	"math"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type JSValueKind int

const (
	JSUndefined JSValueKind = iota
	JSNull
	JSString
	JSNumber
	JSBool
	JSObject
	JSArray
	JSError
)

// JSValue is one JS value as the adaptive classifiers can observe it.
type JSValue struct {
	Kind JSValueKind
	Str  string
	Num  float64
	Bool bool

	// JSError only. Name == "" means the value inherits Error.prototype.name
	// ("Error"); Message mirrors `err.message` (always a string on a real
	// Error instance). Cause == nil means the `cause` property is absent.
	Message string
	Name    string
	Cause   *JSValue

	// Own enumerable properties in insertion order. For JSError these are the
	// EXTRA properties bolted onto the instance (the shape OpenRouter errors
	// arrive in: `Object.assign(new Error(msg), { status: 429 })`).
	Props *jscompat.OrderedMap[string, *JSValue]

	// JSArray elements. A nil element is JS `undefined` (a hole), which
	// JSON.stringify renders as null.
	Items []*JSValue
}

// ── constructors (only used by tests / fixture decoding) ──────────────────

func Undefined() *JSValue { return &JSValue{Kind: JSUndefined} }
func Null() *JSValue      { return &JSValue{Kind: JSNull} }
func Str(s string) *JSValue {
	return &JSValue{Kind: JSString, Str: s}
}
func Num(f float64) *JSValue  { return &JSValue{Kind: JSNumber, Num: f} }
func Bool(b bool) *JSValue    { return &JSValue{Kind: JSBool, Bool: b} }
func Err(msg string) *JSValue { return &JSValue{Kind: JSError, Message: msg} }

// ErrWithStatus is the shape the OpenRouter client throws: an Error carrying a
// numeric `status` own-property, which statusCodeOf reads.
func ErrWithStatus(msg string, status float64) *JSValue {
	v := Err(msg)
	v.Props = jscompat.NewOrderedMap[string, *JSValue]()
	v.Props.Set("status", Num(status))
	return v
}

func Obj(pairs ...any) *JSValue {
	v := &JSValue{Kind: JSObject, Props: jscompat.NewOrderedMap[string, *JSValue]()}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Props.Set(pairs[i].(string), pairs[i+1].(*JSValue))
	}
	return v
}

func Arr(items ...*JSValue) *JSValue { return &JSValue{Kind: JSArray, Items: items} }

// ── probes ────────────────────────────────────────────────────────────────

// isNullish is JS `v == null` (loose equality against null): true for both
// null and undefined, false for everything else.
func isNullish(v *JSValue) bool {
	return v == nil || v.Kind == JSUndefined || v.Kind == JSNull
}

// jsTruthy is the JS ToBoolean coercion.
func jsTruthy(v *JSValue) bool {
	if v == nil {
		return false
	}
	switch v.Kind {
	case JSUndefined, JSNull:
		return false
	case JSString:
		return len(v.Str) > 0
	case JSNumber:
		return v.Num != 0 && !math.IsNaN(v.Num)
	case JSBool:
		return v.Bool
	}
	return true // every object, array and Error instance is truthy
}

// isObjectLike is JS `typeof v === "object"` for a non-null v. Arrays and
// Error instances are objects; null is excluded by the callers' `v == null` /
// `v &&` guard, which is why it is not folded in here.
func isObjectLike(v *JSValue) bool {
	return v != nil && (v.Kind == JSObject || v.Kind == JSArray || v.Kind == JSError)
}

// getProp is a bracket property read. Own props win over the Error intrinsics
// exactly like a real instance, so `Object.assign(new Error("x"), {name:"AbortError"})`
// reports name "AbortError".
func getProp(v *JSValue, key string) *JSValue {
	if v == nil {
		return nil
	}
	if v.Props != nil {
		if got, ok := v.Props.Get(key); ok {
			return got
		}
	}
	if v.Kind == JSError {
		switch key {
		case "name":
			if v.Name != "" {
				return Str(v.Name)
			}
			return Str("Error")
		case "message":
			return Str(v.Message)
		case "cause":
			return v.Cause
		}
	}
	return nil
}

// ── JSON.stringify ────────────────────────────────────────────────────────

// jsonStringify mirrors JSON.stringify(v) for the modelled kinds. The bool
// result is JSON.stringify's `undefined` return, which only happens for a
// top-level undefined (already excluded by every caller).
func jsonStringify(v *JSValue) (string, bool) {
	var b bytes.Buffer
	if !writeJSON(&b, v) {
		return "", false
	}
	return b.String(), true
}

func writeJSON(b *bytes.Buffer, v *JSValue) bool {
	if v == nil || v.Kind == JSUndefined {
		return false
	}
	switch v.Kind {
	case JSNull:
		b.WriteString("null")
	case JSBool:
		if v.Bool {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case JSNumber:
		// JSON.stringify renders every non-finite number as null.
		if math.IsNaN(v.Num) || math.IsInf(v.Num, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(jscompat.FormatNumber(v.Num))
		}
	case JSString:
		writeJSONString(b, v.Str)
	case JSArray:
		b.WriteByte('[')
		for i, item := range v.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			// A hole / undefined element serializes as null.
			if !writeJSON(b, item) {
				b.WriteString("null")
			}
		}
		b.WriteByte(']')
	case JSObject, JSError:
		b.WriteByte('{')
		first := true
		if v.Props != nil {
			for _, e := range v.Props.Entries() {
				var inner bytes.Buffer
				if !writeJSON(&inner, e.Val) {
					continue // undefined-valued keys are omitted
				}
				if !first {
					b.WriteByte(',')
				}
				first = false
				writeJSONString(b, e.Key)
				b.WriteByte(':')
				b.Write(inner.Bytes())
			}
		}
		b.WriteByte('}')
	}
	return true
}

func writeJSONString(b *bytes.Buffer, s string) {
	encoded, err := jscompat.Stringify(s)
	if err != nil {
		b.WriteString(`""`)
		return
	}
	b.Write(encoded)
}

// jsString is `String(v)` — only reachable through errorText's catch arm,
// which this port cannot enter (see the package note above). Kept so the
// shape of errorText matches the TS line for line.
func jsString(v *JSValue) string {
	if v == nil {
		return "undefined"
	}
	switch v.Kind {
	case JSUndefined:
		return "undefined"
	case JSNull:
		return "null"
	case JSString:
		return v.Str
	case JSNumber:
		return jscompat.FormatNumber(v.Num)
	case JSBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case JSError:
		name := v.Name
		if name == "" {
			name = "Error"
		}
		if v.Message == "" {
			return name
		}
		return name + ": " + v.Message
	case JSArray:
		parts := make([]string, len(v.Items))
		for i, item := range v.Items {
			if isNullish(item) {
				parts[i] = ""
			} else {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}
