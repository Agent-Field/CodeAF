package retrysched

// JS runtime primitives retry.ts leans on that Go does not provide
// identically. Everything here is a straight transcription of a JS builtin;
// nothing has behaviour of its own.
//
// The parsed-JSON model is internal/router/adaptive's JSValue rather than a
// local one: JSON.parse and the classifier probes need the same value lattice
// (insertion-ordered objects, JS number formatting, `typeof x === "object"`),
// and reusing it means routerval.go's JSONToRouterValue is the SAME decoder
// retryable()'s parseJSON uses — one implementation, two consumers.

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

// ── clock seam ────────────────────────────────────────────────────────────

// nowMS is both Date.now() (retry.ts:98) and Clock.currentTimeMillis
// (retry.ts:248). Effect's default Clock is Date.now(), so one seam covers
// both.
var nowMS = func() float64 { return float64(time.Now().UnixMilli()) }

// SetNowMSForTesting pins the clock. Returns a restore func, matching
// ratehead.SetNowMSForTesting / adaptive.SetClockForTesting.
func SetNowMSForTesting(f func() float64) func() {
	prev := nowMS
	nowMS = f
	return func() { nowMS = prev }
}

// ── TIMEOUT_MESSAGE_RE (retry.ts:41) ──────────────────────────────────────

// timeoutMessageRE is /timeout|timed out|deadline exceeded/i. Go's (?i) folds
// Unicode where JS's non-unicode `i` flag folds only what round-trips through
// toUpperCase; the alternation contains no letter with a non-ASCII fold
// partner (no 's'/'ſ', no 'k'/'K'), so the two agree on every input.
var timeoutMessageRE = regexp.MustCompile(`(?i)timeout|timed out|deadline exceeded`)

// jsRegExpTest is `re.test(value)` where value may be undefined: RegExp.test
// runs ToString on its argument, so an absent message is tested as the literal
// string "undefined" rather than throwing. delay() (retry.ts:75) relies on
// this; isTimeoutError (retry.ts:61) guards against it instead.
func jsRegExpTest(re *regexp.Regexp, value *string) bool {
	if value == nil {
		return re.MatchString("undefined")
	}
	return re.MatchString(*value)
}

// ── timeoutRetryDelayMs (retry.ts:44-50) ──────────────────────────────────

func timeoutRetryDelayMs() float64 {
	raw, ok := os.LookupEnv(TimeoutRetryDelayEnv)
	// `raw === undefined || raw === ""` — short-circuited BEFORE Number(""),
	// which would otherwise yield 0 and then fail the `n < 1` guard anyway.
	if !ok || raw == "" {
		return TimeoutRetryDelayDefault
	}
	n := envNumber(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 1_800_000 {
		return TimeoutRetryDelayDefault
	}
	return n
}

// envNumber is jscompat.ToNumber with the two JS rules Go's strconv quietly
// relaxes re-imposed. Copied verbatim from internal/router/orfetch, which
// documents the reasoning: strconv accepts "1_000" and "inf"/"nan", JS
// Number() does not. jscompat is off-limits to the port, so the guard is
// duplicated per-package (the same pattern internal/session/cochange uses).
func envNumber(raw string) float64 {
	trimmed := jscompat.Trim(raw)
	if strings.ContainsRune(trimmed, '_') {
		return math.NaN()
	}
	unsigned := strings.TrimPrefix(strings.TrimPrefix(trimmed, "+"), "-")
	switch strings.ToLower(unsigned) {
	case "inf", "infinity", "nan":
		if unsigned != "Infinity" {
			return math.NaN()
		}
	}
	return jscompat.ToNumber(trimmed)
}

// ── Number.parseFloat ─────────────────────────────────────────────────────

// jsParseFloat is Number.parseFloat: leading JS whitespace is skipped, then
// the LONGEST prefix matching StrDecimalLiteral is consumed and anything after
// it ignored. Unlike strconv.ParseFloat it never fails on trailing garbage
// ("3abc" is 3), never accepts a radix prefix ("0x10" is 0, stopping at 'x'),
// never accepts numeric separators ("1_000" is 1), and spells infinity exactly
// "Infinity". An empty prefix is NaN.
func jsParseFloat(s string) float64 {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isJSWhitespace(r) {
			break
		}
		i += size
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if s[start] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	intDigits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		intDigits++
	}
	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			fracDigits++
		}
	}
	if intDigits == 0 && fracDigits == 0 {
		return math.NaN()
	}
	end := i
	// ExponentPart is only consumed when it carries at least one digit; "1e"
	// and "1e+" both parse as 1.
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		expDigits := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
			expDigits++
		}
		if expDigits > 0 {
			end = j
		}
	}
	f, err := strconv.ParseFloat(s[start:end], 64)
	if err != nil {
		// Only ErrRange is reachable (the grammar above is a strict subset of
		// strconv's), and JS overflows to ±Infinity / underflows to ±0 exactly
		// like strconv's ErrRange result.
		return f
	}
	return f
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// ── Date.parse ────────────────────────────────────────────────────────────

// maxTimeMS is the ES time-value clip: |t| > 8.64e15 is Invalid Date.
const maxTimeMS = 8.64e15

// httpDateLayouts are the three HTTP-date productions of RFC 7231 plus the two
// weekday-stripped retries. `local` marks the asctime form, which carries no
// zone and is therefore interpreted in the RUNTIME's zone by both JSC and
// time.ParseInLocation.
var httpDateLayouts = []struct {
	layout string
	local  bool
}{
	{http.TimeFormat, false},            // Mon, 02 Jan 2006 15:04:05 GMT
	{time.RFC850, false},                // Monday, 02-Jan-06 15:04:05 MST
	{time.ANSIC, true},                  // Mon Jan _2 15:04:05 2006
	{"02 Jan 2006 15:04:05 GMT", false}, // weekday already stripped
	{"02-Jan-06 15:04:05 MST", false},   // weekday already stripped
	{"Jan _2 2006 15:04:05 MST", false}, // JSC month-first, explicit zone
	{"Jan _2 2006 15:04:05", true},      // JSC month-first, zone-less → local
	{"Jan _2 2006", true},               // JSC date-only → local midnight
}

// jsDateParse is Date.parse restricted to what retry.ts:98 can reach.
//
// Reachability argument (same as internal/router/ratehead/jsdate.go): the
// branch runs only after Number.parseFloat returned NaN, and every ISO-8601
// spelling starts with a digit or a sign, so parseFloat consumes it first
// ("2015-10-21T07:28:00Z" becomes 2015 seconds and never reaches here). Only
// strings starting with a letter arrive, i.e. the three HTTP-date forms.
//
// swe-pro runs on bun, so the reference is JavaScriptCore's legacy parser.
// Two of its quirks are reproduced: a nonsense weekday is ignored ("Foobar, 21
// Oct 2015 07:28:00 GMT" parses), and the zone-less asctime form is read as
// LOCAL time.
//
// KNOWN DIVERGENCES from bun 1.2.23. Every one is Invalid Date here where bun
// returns a date, so the header is ignored (and delay() falls through to
// exponential backoff) rather than being mis-timed. None is a spelling a
// server emits for Retry-After:
//   - asctime with a nonsense leading word ("Foobar Oct 21 07:28:00 2015") —
//     the weekday strip below only fires on a "…, " separator.
//   - unpadded RFC1123 days ("Wed, 1 Oct 2015 07:28:00 GMT").
//   - trailing numeric zone offsets ("… 07:28:00 +0000") and bracketed
//     comments ("… GMT (BST)"), both of which JSC's parser accepts.
//
// testdata/fixtures.json records the real Date.parse for every string in the
// corpus; fixtures_test.go asserts equality, so the list above cannot rot
// without a test going red.
func jsDateParse(s string) float64 {
	if ms, ok := tryHTTPDate(s); ok {
		return ms
	}
	// JSC scans the leading words only for a month name, so a wrong weekday is
	// ignored. Strip it and retry against the weekday-less layouts.
	if i := strings.Index(s, ", "); i > 0 {
		if ms, ok := tryHTTPDate(s[i+2:]); ok {
			return ms
		}
	}
	return math.NaN()
}

func tryHTTPDate(s string) (float64, bool) {
	for _, l := range httpDateLayouts {
		var t time.Time
		var err error
		if l.local {
			t, err = time.ParseInLocation(l.layout, s, time.Local)
		} else {
			t, err = time.Parse(l.layout, s)
		}
		if err != nil {
			continue
		}
		ms := float64(t.UnixMilli())
		if math.Abs(ms) > maxTimeMS {
			return math.NaN(), true
		}
		return ms, true
	}
	return 0, false
}

// ── JSON.parse and the value probes ───────────────────────────────────────

type jsValue = adaptive.JSValue

const (
	jsKindString = adaptive.JSString
	jsKindNumber = adaptive.JSNumber
	jsKindBool   = adaptive.JSBool
	jsKindObject = adaptive.JSObject
	jsKindArray  = adaptive.JSArray
	jsKindNull   = adaptive.JSNull
)

// parseJSONString is retry.ts's parseJSON (retry.ts:216-225): a non-string
// input and any parse failure both become undefined, modelled as nil.
func parseJSONString(value *string) *jsValue {
	if value == nil {
		return nil
	}
	return parseJSONText(*value)
}

// parseJSONText is JSON.parse, producing an insertion-ordered value.
func parseJSONText(text string) *jsValue {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := decodeJSONValue(dec)
	if err != nil {
		return nil
	}
	// JSON.parse rejects trailing content; so must this.
	if _, err := dec.Token(); err != io.EOF {
		return nil
	}
	return v
}

func decodeJSONValue(dec *json.Decoder) (*jsValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := adaptive.Obj()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				// A duplicate key keeps its ORIGINAL position and takes the
				// later value, exactly like a JS object literal.
				obj.Props.Set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			items := []*jsValue{}
			for dec.More() {
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				items = append(items, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return adaptive.Arr(items...), nil
		}
		return nil, io.ErrUnexpectedEOF
	case string:
		return adaptive.Str(t), nil
	case json.Number:
		// JSON.parse yields a double; an out-of-range literal ("1e400")
		// becomes ±Infinity rather than an error, which is what strconv's
		// ErrRange result already is.
		f, _ := strconv.ParseFloat(string(t), 64)
		return adaptive.Num(f), nil
	case bool:
		return adaptive.Bool(t), nil
	case nil:
		return adaptive.Null(), nil
	}
	return nil, io.ErrUnexpectedEOF
}

// propOf is a bracket read guarded the way optional chaining is: a nullish or
// non-object base yields undefined (nil) rather than throwing.
func propOf(v *jsValue, key string) *jsValue {
	if v == nil || v.Kind != jsKindObject || v.Props == nil {
		return nil
	}
	got, ok := v.Props.Get(key)
	if !ok {
		return nil
	}
	return got
}

// jsTruthy is ToBoolean. nil is undefined.
func jsTruthy(v *jsValue) bool {
	if v == nil {
		return false
	}
	switch v.Kind {
	case adaptive.JSUndefined, jsKindNull:
		return false
	case jsKindString:
		return len(v.Str) > 0
	case jsKindNumber:
		return v.Num != 0 && !math.IsNaN(v.Num)
	case jsKindBool:
		return v.Bool
	}
	return true
}

// jsIsObject is `typeof v === "object"`, which is true for arrays and null
// (the callers exclude null with their own `!json` guard first).
func jsIsObject(v *jsValue) bool {
	return v != nil && (v.Kind == jsKindObject || v.Kind == jsKindArray || v.Kind == jsKindNull)
}

// jsStrictEqStr is `v === "literal"`.
func jsStrictEqStr(v *jsValue, want string) bool {
	return v != nil && v.Kind == jsKindString && v.Str == want
}

// jsStr is retry.ts's str() (retry.ts:208-211): undefined and null become the
// empty string, everything else goes through String().
func jsStr(v *jsValue) string {
	if v == nil || v.Kind == adaptive.JSUndefined || v.Kind == jsKindNull {
		return ""
	}
	return jsToString(v)
}

// jsToString is String(v) for the kinds JSON.parse can produce.
func jsToString(v *jsValue) string {
	switch v.Kind {
	case jsKindString:
		return v.Str
	case jsKindNumber:
		return jscompat.FormatNumber(v.Num)
	case jsKindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case jsKindArray:
		parts := make([]string, len(v.Items))
		for i, item := range v.Items {
			if item == nil || item.Kind == adaptive.JSUndefined || item.Kind == jsKindNull {
				parts[i] = ""
			} else {
				parts[i] = jsToString(item)
			}
		}
		return strings.Join(parts, ",")
	case jsKindNull:
		return "null"
	}
	return "[object Object]"
}

// jsNum is retry.ts's num() (retry.ts:213-217) specialised to the one call
// site, whose argument is a header value: `str(value)` is the string itself
// for a present header and "" for a missing one, and Headers.Get already
// collapses those two.
func jsNum(value string) *float64 {
	parsed := jsParseFloat(value)
	if math.IsNaN(parsed) {
		return nil
	}
	return &parsed
}

// ── String.prototype.toLowerCase ─────────────────────────────────────────

// jsLowerCase mirrors String.prototype.toLowerCase (locale-independent, FULL
// Unicode lowercase). Duplicated from internal/router/adaptive/jsstr.go for
// the same reason that file states: jscompat is the only shared surface and it
// does not carry these. Go's strings.ToLower implements the SIMPLE mapping;
// the default part of SpecialCasing.txt adds exactly two rules on top — U+0130
// → "i"+U+0307, and Final_Sigma U+03A3 → U+03C2.
//
// Reachable here: retryable() lowercases a provider-supplied error message
// before substring-matching the three rate-limit phrases (retry.ts:181).
func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03A3 && isFinalSigma(runes, i):
			b.WriteRune(0x03C2)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', 0x2019, 0x00AD, 0x02B9, 0x0385, 0x1FBF, 0x1FC1, 0x1FCD, 0x1FCE,
		0x1FCF, 0x1FDD, 0x1FDE, 0x1FDF, 0x1FED, 0x1FEE, 0x1FEF, 0x1FFD, 0x1FFE, 0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r)
}

// ── Record<string,string> with JS key order ──────────────────────────────

// Headers is a JS `Record<string, string>` (message-v2.ts:55, :57). It keeps
// insertion order because JSON.stringify of an APIError does, and
// ErrToRouterValue feeds that text to the router's substring classifiers.
//
// A nil *Headers is an ABSENT property; a non-nil empty one is `{}`, which is
// truthy and therefore selects delay()'s unbounded branch.
type Headers struct {
	m *jscompat.OrderedMap[string, string]
}

// NewHeaders builds a Headers from key/value pairs in order.
func NewHeaders(pairs ...string) *Headers {
	h := &Headers{m: jscompat.NewOrderedMap[string, string]()}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.m.Set(pairs[i], pairs[i+1])
	}
	return h
}

// Get is a bracket read. A missing key and an empty value are both falsy in
// JS, so collapsing them to "" loses nothing.
func (h *Headers) Get(key string) string {
	if h == nil || h.m == nil {
		return ""
	}
	v, _ := h.m.Get(key)
	return v
}

// Entries returns the pairs in insertion order.
func (h *Headers) Entries() []jscompat.Entry[string, string] {
	if h == nil || h.m == nil {
		return nil
	}
	return h.m.Entries()
}

func (h *Headers) UnmarshalJSON(data []byte) error {
	h.m = jscompat.NewOrderedMap[string, string]()
	if string(data) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if _, err := dec.Token(); err != nil { // '{'
		return err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		var val string
		if err := dec.Decode(&val); err != nil {
			return err
		}
		h.m.Set(key, val)
	}
	_, err := dec.Token() // '}'
	return err
}

func (h *Headers) MarshalJSON() ([]byte, error) {
	if h == nil {
		return []byte("null"), nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, e := range h.Entries() {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := jscompat.Stringify(e.Key)
		if err != nil {
			return nil, err
		}
		v, err := jscompat.Stringify(e.Val)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
