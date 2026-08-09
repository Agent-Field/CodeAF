// Package ratehead is a bug-for-bug port of
// src/router/openrouter-rate-headers.ts — the OpenRouter rate-limit response
// metadata parser that feeds the AIMD limiter.
//
// Boundary note: the TS entry point takes a Web-platform `Headers`. Go has no
// such type, so the port takes net/http's `http.Header`, the same stand-in the
// sibling internal/router/orfetch port picked. Two `Headers` behaviours live
// on the caller's side of that boundary and are reproduced by the fixture
// harness rather than by this package:
//   - Headers.set/append NORMALIZE the value at write time (leading and
//     trailing SP/HTAB are stripped, per the Fetch spec). http.Header.Add does
//     not. For this module the difference is invisible for single values —
//     parseInteger/parseFloatValue/parseRetryAfter trim anyway — but it IS
//     visible for repeated headers, because headerGet joins with ", ".
//   - Headers rejects values outside Latin-1 (a U+FEFF value throws a
//     TypeError before ever reaching this code); http.Header accepts them.
//
// Fidelity notes (deliberate, do not "fix"):
//   - RateLimitInfo's four fields are `?:` in TS and the object literal assigns
//     `undefined` to the ones that did not parse. JSON.stringify DROPS
//     undefined-valued keys, so the fields are pointers carrying `,omitempty`
//     — the same deliberate exception to the repo-wide no-omitempty rule that
//     internal/session/auditconvergence documents.
//   - The fields are jscompat.JSNumber, not float64, so that a `-0` (reachable:
//     `Retry-After: -0` → Number.parseInt("-0", 10) → -0) marshals as `0` the
//     way JSON.stringify does. Go's encoding/json prints `-0`.
//   - parseInteger keeps Number.parseInt's radix-10 prefix semantics: "12abc"
//     is 12, "0x10" is 0, ".5" is NaN, and a digit run too long for a float64
//     becomes Infinity, which Number.isFinite then rejects.
//   - parseRetryAfter tries Number.parseFloat FIRST, so a header that merely
//     STARTS with a digit never reaches the date parser: "21 Oct 2015 07:28:00
//     GMT" yields 21 seconds and "2015-10-21T07:28:00Z" yields 2015 seconds.
//     That is a real bug in the original; it is preserved. It also means the
//     ES5 ISO-8601 branch of `new Date(string)` is UNREACHABLE from here, and
//     only the legacy parser (see jsdate.go) had to be ported.
//   - Date.now() is the injectable nowMS seam (SetNowMSForTesting), matching
//     how internal/router/adaptive and internal/router/orfetch handle clocks.
package ratehead

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// nowMS is the Date.now() seam. Milliseconds since the epoch as a float64,
// because that is what the TS expression `(date.getTime() - Date.now()) / 1000`
// operates on.
var nowMS = func() float64 { return float64(time.Now().UnixMilli()) }

// SetNowMSForTesting pins Date.now(). Returns a restore func.
func SetNowMSForTesting(f func() float64) func() {
	prev := nowMS
	nowMS = f
	return func() { nowMS = prev }
}

// RateLimitInfo mirrors the TS type of the same name. Key order in the JSON
// tags is the TS object-literal order.
type RateLimitInfo struct {
	Limit             *jscompat.JSNumber `json:"limit,omitempty"`
	Remaining         *jscompat.JSNumber `json:"remaining,omitempty"`
	Reset             *jscompat.JSNumber `json:"reset,omitempty"`
	RetryAfterSeconds *jscompat.JSNumber `json:"retryAfterSeconds,omitempty"`
}

// ParseRateLimitHeaders mirrors parseRateLimitHeaders(). A nil result is the
// TS `undefined`: every one of the four fields failed to parse.
func ParseRateLimitHeaders(headers http.Header) *RateLimitInfo {
	info := &RateLimitInfo{
		Limit:             parseInteger(headerGet(headers, "X-RateLimit-Limit")),
		Remaining:         parseInteger(headerGet(headers, "X-RateLimit-Remaining")),
		Reset:             parseFloatValue(headerGet(headers, "X-RateLimit-Reset")),
		RetryAfterSeconds: parseRetryAfter(headerGet(headers, "Retry-After")),
	}
	if info.Limit == nil &&
		info.Remaining == nil &&
		info.Reset == nil &&
		info.RetryAfterSeconds == nil {
		return nil
	}
	return info
}

// IsLowRemaining mirrors isLowRemaining(): true when the remaining quota is
// <= 1 absolute OR <= 10% of a positive limit. A nil info is TS `undefined`.
//
// Every comparison here is a float comparison against a possibly-NaN operand,
// and Go agrees with JS that any comparison involving NaN is false — so a NaN
// remaining falls through to `false`, as in TS.
func IsLowRemaining(info *RateLimitInfo) bool {
	if info == nil || info.Remaining == nil {
		return false
	}
	if *info.Remaining <= 1 {
		return true
	}
	if info.Limit == nil || *info.Limit <= 0 {
		return false
	}
	return *info.Remaining / *info.Limit <= 0.1
}

// ── header access ────────────────────────────────────────────────────────

// headerGet mirrors Headers.get(): case-insensitive lookup, ", " join for a
// repeated header, and null (nil) when the header is absent.
func headerGet(headers http.Header, name string) *string {
	if headers == nil {
		return nil
	}
	values, ok := headers[http.CanonicalHeaderKey(name)]
	if !ok || len(values) == 0 {
		return nil
	}
	joined := strings.Join(values, ", ")
	return &joined
}

// ── the three private parsers ────────────────────────────────────────────

// parseInteger mirrors parseInteger(). `if (!trimmed)` is JS string
// truthiness, where the empty string is the only falsy string.
func parseInteger(value *string) *jscompat.JSNumber {
	if value == nil {
		return nil
	}
	trimmed := jscompat.Trim(*value)
	if trimmed == "" {
		return nil
	}
	n := jsParseInt10(trimmed)
	if isFinite(n) {
		return jsNum(n)
	}
	return nil
}

// parseFloatValue mirrors parseFloatValue(). Number.parseFloat happily returns
// ±Infinity for "Infinity" / "1e400"; Number.isFinite then rejects it.
func parseFloatValue(value *string) *jscompat.JSNumber {
	if value == nil {
		return nil
	}
	trimmed := jscompat.Trim(*value)
	if trimmed == "" {
		return nil
	}
	n := jsParseFloat(trimmed)
	if isFinite(n) {
		return jsNum(n)
	}
	return nil
}

// parseRetryAfter mirrors parseRetryAfter(): seconds first, HTTP-date second.
// The float expression `(date.getTime() - Date.now()) / 1000` is kept in shape.
func parseRetryAfter(value *string) *jscompat.JSNumber {
	if value == nil {
		return nil
	}
	trimmed := jscompat.Trim(*value)
	if trimmed == "" {
		return nil
	}
	seconds := jsParseFloat(trimmed)
	if isFinite(seconds) {
		return jsNum(math.Max(0, seconds))
	}
	ms := parseJSDateMS(trimmed)
	if math.IsNaN(ms) {
		return nil
	}
	return jsNum(math.Max(0, (ms-nowMS())/1000))
}

// ── JS numeric coercions ─────────────────────────────────────────────────

// isFinite is Number.isFinite for an already-numeric value.
func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func jsNum(f float64) *jscompat.JSNumber {
	v := jscompat.JSNumber(f)
	return &v
}

// jsParseInt10 mirrors Number.parseInt(s, 10) for an already-trimmed string:
// an optional sign, then the longest run of decimal digits, NaN when there is
// none. The digit run is rounded to the nearest float64 (so
// "9007199254740993" collapses to 9007199254740992 and a 400-digit run becomes
// Infinity), which is what the spec's "Number value for the MV" requires.
func jsParseInt10(s string) float64 {
	i := 0
	sign := 1.0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			sign = -1
		}
		i++
	}
	start := i
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	if i == start {
		return math.NaN()
	}
	// The only error a pure digit run can raise is ErrRange, and ParseFloat
	// still returns ±Inf in that case — which is what JS yields too.
	magnitude, _ := strconv.ParseFloat(s[start:i], 64)
	return sign * magnitude
}

// jsParseFloat mirrors Number.parseFloat(s) for an already-trimmed string: the
// longest StrDecimalLiteral prefix, including the "Infinity" spelling, and NaN
// when the string does not start with one.
func jsParseFloat(s string) float64 {
	i := 0
	negative := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		negative = s[i] == '-'
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if negative {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}

	j := i
	intDigits := 0
	for j < len(s) && isASCIIDigit(s[j]) {
		j++
		intDigits++
	}
	fracDigits := 0
	if j < len(s) && s[j] == '.' {
		k := j + 1
		for k < len(s) && isASCIIDigit(s[k]) {
			k++
			fracDigits++
		}
		// "5." is a valid literal; ".x" is not.
		if intDigits > 0 || fracDigits > 0 {
			j = k
		}
	}
	if intDigits == 0 && fracDigits == 0 {
		return math.NaN()
	}
	end := j
	// The exponent only extends the prefix when it actually has digits:
	// parseFloat("1e") is 1, not NaN.
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		expDigits := 0
		for k < len(s) && isASCIIDigit(s[k]) {
			k++
			expDigits++
		}
		if expDigits > 0 {
			end = k
		}
	}

	literal := s[:end]
	// strconv rejects a trailing '.'; JS accepts it.
	if strings.HasSuffix(literal, ".") {
		literal = literal[:len(literal)-1]
	}
	f, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		var numErr *strconv.NumError
		// Overflow/underflow keeps the ±Inf / ±0 result, exactly like JS.
		if errors.As(err, &numErr) && numErr.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }
