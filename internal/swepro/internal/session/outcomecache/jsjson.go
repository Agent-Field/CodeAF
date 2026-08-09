package outcomecache

// JS-value fidelity layer for the outcome cache.
//
// outcome-cache.ts is round-trip persistence: `put` JSON.stringify's a value
// into the store, `loadOutcomeCacheStore` JSON.parse's a workspace file, and
// `get` JSON.parse's a store entry and hands the PARSED OBJECT straight back to
// the caller (`return parsed` — no validation beyond `parsed.key === key` and a
// truthy `parsed.outcome`). A byte-parity port therefore cannot decode into a Go
// struct and re-encode: the observable output has to keep
//
//   - unknown extra keys that were in the file/entry,
//   - the entry's own key order (JSON.parse preserves it),
//   - JS object property order, i.e. array-index-like keys ASCENDING FIRST and
//     only then the remaining keys in insertion order — which is reachable here
//     twice: `Object.entries(raw)` in loadOutcomeCacheStore and
//     `JSON.stringify(Object.fromEntries(store))` in persistOutcomeCacheStore,
//   - V8's number → string form, and
//   - lone surrogates, which survive JSON.parse → JSON.stringify verbatim.
//
// Hence: a small ordered JSON value (jsVal), a strict JSON.parse-equivalent
// parser, a JSON.stringify-equivalent serializer, and WTF-8 string helpers (Go
// strings hold UTF-8; an unpaired surrogate is stored as its 3-byte
// UTF-8-style encoding, which Go's own utf8/utf16 packages refuse to produce).
//
// This mirrors internal/session/ledgers/jsjson.go, which solved the identical
// problem for the two ledgers; the two copies are independent because Go has no
// way to share unexported helpers across packages and neither package may be
// modified by the other's port.
//
// jscompat.Stringify is still the parity gate in the tests — the exported types
// here simply implement json.Marshaler on top of this serializer so that gate
// sees V8-identical bytes.

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── WTF-8 helpers ────────────────────────────────────────────────────────

// decodeWTF8 decodes one code point at s[i], accepting the surrogate range
// D800-DFFF that Go's utf8.DecodeRuneInString rejects. Malformed bytes yield
// (utf8.RuneError, 1) so callers can copy the offending byte through verbatim.
func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xE0 == 0xC0:
		if i+1 < len(s) && s[i+1]&0xC0 == 0x80 {
			cp := rune(b&0x1F)<<6 | rune(s[i+1]&0x3F)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xF0 == 0xE0:
		if i+2 < len(s) && s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80 {
			cp := rune(b&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xF8 == 0xF0:
		if i+3 < len(s) && s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80 && s[i+3]&0xC0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
			if cp >= 0x10000 && cp <= 0x10FFFF {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

// appendWTF8 appends cp, encoding surrogates in the 3-byte form instead of
// substituting U+FFFD the way utf8.AppendRune would.
func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xD800 && cp <= 0xDFFF {
		return append(dst,
			byte(0xE0|cp>>12),
			byte(0x80|(cp>>6)&0x3F),
			byte(0x80|cp&0x3F))
	}
	return utf8.AppendRune(dst, cp)
}

// toUTF8 replaces unpaired surrogates with U+FFFD, which is what Node does when
// a JS string is encoded as utf8 — both crypto's `update(str, "utf8")` (the
// cache key) and fs's "utf8" write encoding (the sidecar).
func toUTF8(s string) string {
	needs := false
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		if cp >= 0xD800 && cp <= 0xDFFF {
			needs = true
			break
		}
		i += size
	}
	if !needs {
		return s
	}
	var b []byte
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		if cp >= 0xD800 && cp <= 0xDFFF {
			b = utf8.AppendRune(b, utf8.RuneError)
		} else {
			b = append(b, s[i:i+size]...)
		}
		i += size
	}
	return string(b)
}

// decodeUTF8Lossy mirrors Node's fs read with encoding "utf8": invalid bytes
// become U+FFFD (one per invalid byte here, where WHATWG emits one per maximal
// subpart — the two agree on every sequence this module can itself produce,
// since everything it writes is valid UTF-8).
func decodeUTF8Lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out []byte
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			out = utf8.AppendRune(out, utf8.RuneError)
			i++
			continue
		}
		out = append(out, b[i:i+size]...)
		i += size
	}
	return string(out)
}

// ── JSON.stringify-compatible string quoting ─────────────────────────────

const hexDigits = "0123456789abcdef"

// appendJSQuoted writes s as a JSON string literal the way V8's JSON.stringify
// does: lowercase \u escapes, U+2028/U+2029 emitted RAW (Go's encoding/json
// escapes them), and unpaired surrogates escaped as \udXXX (well-formed
// JSON.stringify, ES2019).
func appendJSQuoted(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		i += size
		switch {
		case cp == '"':
			dst = append(dst, '\\', '"')
		case cp == '\\':
			dst = append(dst, '\\', '\\')
		case cp == '\b':
			dst = append(dst, '\\', 'b')
		case cp == '\f':
			dst = append(dst, '\\', 'f')
		case cp == '\n':
			dst = append(dst, '\\', 'n')
		case cp == '\r':
			dst = append(dst, '\\', 'r')
		case cp == '\t':
			dst = append(dst, '\\', 't')
		case cp < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[cp>>4], hexDigits[cp&0xf])
		case cp >= 0xD800 && cp <= 0xDFFF:
			dst = append(dst, '\\', 'u',
				hexDigits[(cp>>12)&0xf], hexDigits[(cp>>8)&0xf],
				hexDigits[(cp>>4)&0xf], hexDigits[cp&0xf])
		default:
			dst = appendWTF8(dst, cp)
		}
	}
	return append(dst, '"')
}

// ── ordered JSON value ───────────────────────────────────────────────────

type jsKind int

const (
	jsNull jsKind = iota
	jsBool
	jsNumber
	jsString
	jsArray
	jsObject
)

type jsObj struct {
	keys []string
	vals map[string]jsVal
}

func newJSObj() *jsObj { return &jsObj{vals: map[string]jsVal{}} }

func (o *jsObj) get(k string) (jsVal, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// set mirrors JS property assignment: a re-assigned key keeps its original
// creation position (which is what JSON.parse does for duplicate keys too).
func (o *jsObj) set(k string, v jsVal) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

type jsVal struct {
	kind jsKind
	b    bool
	num  float64
	str  string
	arr  []jsVal
	obj  *jsObj
}

func nullVal() jsVal            { return jsVal{kind: jsNull} }
func boolVal(b bool) jsVal      { return jsVal{kind: jsBool, b: b} }
func numberVal(f float64) jsVal { return jsVal{kind: jsNumber, num: f} }
func stringVal(s string) jsVal  { return jsVal{kind: jsString, str: s} }
func arrayVal(a []jsVal) jsVal  { return jsVal{kind: jsArray, arr: a} }
func objectVal(o *jsObj) jsVal  { return jsVal{kind: jsObject, obj: o} }

// truthy is JS `!!v` for a JSON-derived value: null, false, 0, -0, NaN and ""
// are falsy; every object and array — empty ones included — is truthy.
func (v jsVal) truthy() bool {
	switch v.kind {
	case jsNull:
		return false
	case jsBool:
		return v.b
	case jsNumber:
		return v.num != 0 && !math.IsNaN(v.num)
	case jsString:
		return v.str != ""
	}
	return true
}

// prop is `v[k]` for a JSON-derived value. found=false stands for undefined,
// which covers every primitive receiver (`"abc".key` is undefined) and, for
// arrays, everything but a canonical index or "length".
func (v jsVal) prop(k string) (jsVal, bool) {
	switch v.kind {
	case jsObject:
		return v.obj.get(k)
	case jsArray:
		if n, ok := isArrayIndexKey(k); ok && int(n) < len(v.arr) {
			return v.arr[n], true
		}
		if k == "length" {
			return numberVal(float64(len(v.arr))), true
		}
	}
	return nullVal(), false
}

// isArrayIndexKey reports whether k is a canonical array index (0 ≤ n < 2³²-1),
// the class of property names JS enumerates first, in ascending numeric order.
func isArrayIndexKey(k string) (uint32, bool) {
	if k == "" || len(k) > 10 {
		return 0, false
	}
	if k == "0" {
		return 0, true
	}
	if k[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return uint32(n), true
}

// orderedKeys applies the JS own-property order: array-index keys ascending,
// then every other key in insertion order.
func (o *jsObj) orderedKeys() []string {
	type idxKey struct {
		n uint32
		k string
	}
	var idx []idxKey
	var rest []string
	for _, k := range o.keys {
		if n, ok := isArrayIndexKey(k); ok {
			idx = append(idx, idxKey{n, k})
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(idx, func(i, j int) bool { return idx[i].n < idx[j].n })
	out := make([]string, 0, len(o.keys))
	for _, e := range idx {
		out = append(out, e.k)
	}
	return append(out, rest...)
}

// appendJSON serializes v exactly as JSON.stringify(v) would.
func (v jsVal) appendJSON(dst []byte) []byte {
	switch v.kind {
	case jsNull:
		return append(dst, "null"...)
	case jsBool:
		if v.b {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case jsNumber:
		if math.IsNaN(v.num) || math.IsInf(v.num, 0) {
			return append(dst, "null"...)
		}
		return append(dst, jscompat.FormatNumber(v.num)...)
	case jsString:
		return appendJSQuoted(dst, v.str)
	case jsArray:
		dst = append(dst, '[')
		for i, el := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = el.appendJSON(dst)
		}
		return append(dst, ']')
	case jsObject:
		dst = append(dst, '{')
		first := true
		for _, k := range v.obj.orderedKeys() {
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst = appendJSQuoted(dst, k)
			dst = append(dst, ':')
			dst = v.obj.vals[k].appendJSON(dst)
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

func (v jsVal) stringify() string { return string(v.appendJSON(nil)) }

// ── JSON.parse-compatible parser ─────────────────────────────────────────

type jsParser struct {
	s string
	i int
}

// parseJSON mirrors JSON.parse: ok=false stands for the SyntaxError that every
// call site in outcome-cache.ts catches and turns into "treat as absent".
func parseJSON(s string) (jsVal, bool) {
	p := &jsParser{s: s}
	p.skipWS()
	v, ok := p.parseValue()
	if !ok {
		return nullVal(), false
	}
	p.skipWS()
	if p.i != len(p.s) {
		return nullVal(), false
	}
	return v, true
}

func (p *jsParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsParser) parseValue() (jsVal, bool) {
	if p.i >= len(p.s) {
		return nullVal(), false
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		s, ok := p.parseString()
		if !ok {
			return nullVal(), false
		}
		return stringVal(s), true
	case c == 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return boolVal(true), true
		}
	case c == 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return boolVal(false), true
		}
	case c == 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return nullVal(), true
		}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	}
	return nullVal(), false
}

func (p *jsParser) parseObject() (jsVal, bool) {
	p.i++ // '{'
	o := newJSObj()
	p.skipWS()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return objectVal(o), true
	}
	for {
		p.skipWS()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nullVal(), false
		}
		k, ok := p.parseString()
		if !ok {
			return nullVal(), false
		}
		p.skipWS()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nullVal(), false
		}
		p.i++
		p.skipWS()
		v, ok := p.parseValue()
		if !ok {
			return nullVal(), false
		}
		o.set(k, v)
		p.skipWS()
		if p.i >= len(p.s) {
			return nullVal(), false
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == '}' {
			p.i++
			return objectVal(o), true
		}
		return nullVal(), false
	}
}

func (p *jsParser) parseArray() (jsVal, bool) {
	p.i++ // '['
	arr := []jsVal{}
	p.skipWS()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return arrayVal(arr), true
	}
	for {
		p.skipWS()
		v, ok := p.parseValue()
		if !ok {
			return nullVal(), false
		}
		arr = append(arr, v)
		p.skipWS()
		if p.i >= len(p.s) {
			return nullVal(), false
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == ']' {
			p.i++
			return arrayVal(arr), true
		}
		return nullVal(), false
	}
}

func (p *jsParser) hex4(at int) (rune, bool) {
	if at+4 > len(p.s) {
		return 0, false
	}
	var v rune
	for i := at; i < at+4; i++ {
		c := p.s[i]
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return v, true
}

func (p *jsParser) parseString() (string, bool) {
	p.i++ // opening quote
	var b []byte
	for {
		if p.i >= len(p.s) {
			return "", false
		}
		c := p.s[p.i]
		if c == '"' {
			p.i++
			return string(b), true
		}
		if c < 0x20 {
			return "", false // raw control characters are a SyntaxError
		}
		if c != '\\' {
			_, size := decodeWTF8(p.s, p.i)
			b = append(b, p.s[p.i:p.i+size]...)
			p.i += size
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			return "", false
		}
		switch p.s[p.i] {
		case '"':
			b, p.i = append(b, '"'), p.i+1
		case '\\':
			b, p.i = append(b, '\\'), p.i+1
		case '/':
			b, p.i = append(b, '/'), p.i+1
		case 'b':
			b, p.i = append(b, '\b'), p.i+1
		case 'f':
			b, p.i = append(b, '\f'), p.i+1
		case 'n':
			b, p.i = append(b, '\n'), p.i+1
		case 'r':
			b, p.i = append(b, '\r'), p.i+1
		case 't':
			b, p.i = append(b, '\t'), p.i+1
		case 'u':
			hi, ok := p.hex4(p.i + 1)
			if !ok {
				return "", false
			}
			p.i += 5
			if hi >= 0xD800 && hi <= 0xDBFF && p.i+1 < len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
				if lo, ok2 := p.hex4(p.i + 2); ok2 && lo >= 0xDC00 && lo <= 0xDFFF {
					p.i += 6
					b = appendWTF8(b, 0x10000+((hi-0xD800)<<10)+(lo-0xDC00))
					continue
				}
			}
			b = appendWTF8(b, hi)
		default:
			return "", false
		}
	}
}

func (p *jsParser) parseNumber() (jsVal, bool) {
	start := p.i
	if p.i < len(p.s) && p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return nullVal(), false
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nullVal(), false
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nullVal(), false
		}
	}
	lit := p.s[start:p.i]
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		// Out-of-range literals are not an error in JS: 1e999 → Infinity,
		// 1e-999 → 0. ParseFloat still returns the saturated value.
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return nullVal(), false
		}
	}
	return numberVal(f), true
}
