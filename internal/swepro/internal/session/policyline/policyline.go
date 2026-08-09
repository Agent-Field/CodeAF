// Package policyline ports src/session/policy-line.ts — the shared parser for
// the `key: value` policy-line convention used in PlanDB task descriptions.
//
// The TS original is two one-liners built on a dynamically-constructed
// JavaScript RegExp. Neither one-liner survives a naive `regexp.MustCompile`
// translation, because three JS regex behaviours have no RE2 equivalent:
//
//   - JS `m` mode: `^` matches after ANY LineTerminator — \n, \r, U+2028,
//     U+2029. Go's `(?m)^` only matches after \n. Reproduced here by consuming
//     an optional leading terminator: `(?:\A|[\n\r\x{2028}\x{2029}])`. The
//     terminator is consumed *before* the key, which shifts the match START one
//     rune left but leaves capture group 1 (the only thing policyLine reads)
//     identical.
//   - JS `.`: matches anything except those same four terminators. Go's `.`
//     only excludes \n. Reproduced with the explicit class jsDot.
//   - JS `\s`: WhiteSpace ∪ LineTerminator, which includes U+00A0, U+1680,
//     U+2000-U+200A, U+2028, U+2029, U+202F, U+205F, U+3000 and U+FEFF. RE2's
//     `\s` is only [\t\n\f\r ]. Reproduced with the explicit class jsSpace.
//     Note `\s*` can therefore span a newline: "file_scope:\n  a" yields "a".
//
// The trailing `$` of the TS pattern is dropped deliberately: `(.+)` with
// JS-dot semantics is greedy and cannot cross a terminator, so after maximal
// expansion `$` (in `m` mode) is satisfied unconditionally. Dropping it changes
// no match and no capture.
//
// Fidelity notes (deliberate, do not "fix"):
//   - `key` is interpolated into the pattern RAW, not escaped. A key containing
//     a capture group makes `?.[1]` return the KEY's group instead of the
//     value — policyLine("a: 1", "(a|b)") is "a" in TS, and "a" here.
//   - Case-insensitivity uses JS non-unicode Canonicalize (toUpperCase-based,
//     with the "don't fold non-ASCII down to ASCII" rule), NOT Go's `(?i)`
//     Unicode simple folding. They disagree on U+017F/s, U+212A/k, U+1E9E/ß.
//     See compileKey.
package policyline

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// jsSpace is the JS `\s` class written out. Order/shape mirrors the spec's
// WhiteSpace ∪ LineTerminator production.
const jsSpace = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// jsDot is the JS `.` class (no `s` flag) written out.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

// jsLineStart stands in for `^` under the `m` flag. It CONSUMES the preceding
// terminator; policyLine only reads capture group 1, so the wider match span is
// unobservable.
const jsLineStart = `(?:\A|[\n\r\x{2028}\x{2029}])`

var reCache sync.Map // key string -> *regexp.Regexp

// compileKey builds the Go analogue of `new RegExp("^"+key+":\\s*(.+)$", "im")`.
//
// When key is a plain literal (no regex metacharacter) each rune is expanded
// into an explicit alternation class computed from JS Canonicalize, so the port
// folds case exactly the way JS does. When key carries metacharacters the port
// falls back to Go `(?i)` + the raw key: RE2 then owns both the syntax and the
// folding, which is a DIVERGENCE for lookahead/backreference keys (RE2 rejects
// them where JS accepts) and for the three folding pairs listed above.
func compileKey(key string) *regexp.Regexp {
	if v, ok := reCache.Load(key); ok {
		return v.(*regexp.Regexp)
	}
	var pat string
	if isLiteralKey(key) {
		pat = jsLineStart + literalKeyPattern(key) + `:` + jsSpace + `*(` + jsDot + `+)`
	} else {
		pat = `(?i)` + jsLineStart + key + `:` + jsSpace + `*(` + jsDot + `+)`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		// `new RegExp` throws a SyntaxError in JS; the TS call sites never
		// catch it, so the closest analogue is an unrecovered panic.
		panic("policyline: cannot compile key `" + key + "`: " + err.Error())
	}
	reCache.Store(key, re)
	return re
}

// regexMeta lists every character that changes the shape of a JS/RE2 pattern.
// A key free of all of them is a literal and takes the exact-folding path.
const regexMeta = `\.+*?()|[]{}^$`

func isLiteralKey(key string) bool {
	return !strings.ContainsAny(key, regexMeta)
}

func literalKeyPattern(key string) string {
	var b strings.Builder
	for _, r := range key {
		set := jsCaseSet(r)
		if len(set) == 1 {
			b.WriteString(regexp.QuoteMeta(string(r)))
			continue
		}
		b.WriteByte('[')
		for _, c := range set {
			b.WriteString(classEscape(c))
		}
		b.WriteByte(']')
	}
	return b.String()
}

func classEscape(r rune) string {
	const hex = "0123456789abcdef"
	var out []byte
	out = append(out, '\\', 'x', '{')
	v := uint32(r)
	digits := []byte{}
	if v == 0 {
		digits = append(digits, '0')
	}
	for v > 0 {
		digits = append(digits, hex[v&0xf])
		v >>= 4
	}
	for i := len(digits) - 1; i >= 0; i-- {
		out = append(out, digits[i])
	}
	out = append(out, '}')
	return string(out)
}

// jsCanonicalize is the ECMA-262 Canonicalize abstract operation for a
// NON-unicode-mode, ignoreCase RegExp: uppercase the single char; bail out if
// the uppercase form is not exactly one char; and refuse to map a non-ASCII
// char onto an ASCII one (this is what keeps U+017F from matching "s").
func jsCanonicalize(r rune) rune {
	u := strings.ToUpper(string(r))
	ru := []rune(u)
	if len(ru) != 1 {
		return r
	}
	cu := ru[0]
	if r >= 128 && cu < 128 {
		return r
	}
	return cu
}

// jsCaseSet returns every rune a JS ignoreCase RegExp would treat as equal to
// r: r's Unicode simple-fold orbit, filtered down to the members that share r's
// Canonicalize value.
func jsCaseSet(r rune) []rune {
	canon := jsCanonicalize(r)
	set := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if jsCanonicalize(f) == canon {
			set = append(set, f)
		}
	}
	return set
}

// PolicyLine mirrors policyLine: extract the value of a `key: value` line from
// a task description. Anchored per-line, case-insensitive; first match wins.
// A nil description (TS `undefined`) short-circuits to nil, as does no match
// and a non-participating capture group 1.
func PolicyLine(description *string, key string) *string {
	if description == nil {
		return nil
	}
	m := compileKey(key).FindStringSubmatchIndex(*description)
	if m == nil {
		return nil
	}
	// `?.[1]` — undefined when group 1 never participated in the match, which
	// happens whenever `key` itself contributes the first capture group.
	if len(m) < 4 || m[2] < 0 {
		return nil
	}
	v := jscompat.Trim((*description)[m[2]:m[3]])
	return &v
}

// ParseFileScope mirrors parseFileScope: parse a file_scope policy line into a
// list of glob patterns. Accepts comma- or newline-separated values; strips
// surrounding whitespace/quotes.
//
// A nil return is TS `undefined` (falsy input, blank after trim, or the
// "unknown until probe" sentinel — the caller must treat that as conflicting
// with anything). An empty non-nil slice is TS `[]`, which the TS reaches
// whenever every part filters out (e.g. ",,,").
func ParseFileScope(scope *string) []string {
	if scope == nil || *scope == "" {
		return nil
	}
	cleaned := jscompat.Trim(*scope)
	if cleaned == "" || cleaned == "unknown until probe" {
		return nil
	}
	out := []string{}
	for _, p := range splitCommaOrLF(cleaned) {
		p = stripEdgeQuotes(jscompat.Trim(p))
		if p != "" { // .filter(Boolean)
			out = append(out, p)
		}
	}
	return out
}

// splitCommaOrLF is `.split(/[,\n]/)`. Note the class does NOT include \r, so a
// CRLF-separated scope keeps a trailing \r on each part — which the following
// .trim() then removes. Byte-wise scanning is safe: neither ',' nor '\n' can
// appear inside a UTF-8 multi-byte sequence.
func splitCommaOrLF(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' || s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// stripEdgeQuotes is `.replace(/^["']|["']$/g, "")`.
//
// The global flag makes this two independent, NON-OVERLAPPING matches: one at
// index 0 and one at the last index. For a one-character string the leading
// match consumes the only character, so the trailing alternative can no longer
// apply — `"` becomes "" (not an error, and not left as `"`). At most one quote
// comes off each end regardless of nesting: `"'a'"` becomes `'a'`.
func stripEdgeQuotes(s string) string {
	if s == "" {
		return s
	}
	start, end := 0, len(s)
	if s[0] == '"' || s[0] == '\'' {
		start = 1
	}
	if end-1 >= start && (s[end-1] == '"' || s[end-1] == '\'') {
		end--
	}
	return s[start:end]
}
