// Package prefixhygiene ports src/session/prefix-hygiene.ts:1-109 from swe-pro
// commit 3b25a1a. Match collection order and UTF-16 string indexing are
// observable and intentionally preserved.
package prefixhygiene

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// PrefixHygieneKind is one of the advisory cache-instability classes.
type PrefixHygieneKind string

const (
	KindTimestamp    PrefixHygieneKind = "timestamp"
	KindUUID         PrefixHygieneKind = "uuid"
	KindRandomID     PrefixHygieneKind = "random-id"
	KindUnstablePath PrefixHygieneKind = "unstable-path"
)

// Excerpt is a JS-sliced string. Its distinct type preserves an unpaired
// surrogate if the 80-code-unit clip cuts through an astral character.
type Excerpt string

// PrefixHygieneViolation is one unique matched instability.
type PrefixHygieneViolation struct {
	Kind    PrefixHygieneKind `json:"kind"`
	Excerpt Excerpt           `json:"excerpt"`
}

// PrefixHygieneResult is the advisory scan result.
type PrefixHygieneResult struct {
	Clean      bool                     `json:"clean"`
	Violations []PrefixHygieneViolation `json:"violations"`
}

// StablePrefixResult reports UTF-16 code-unit equality/divergence.
type StablePrefixResult struct {
	Stable          bool `json:"stable"`
	FirstDivergence *int `json:"firstDivergence,omitempty"`
}

var (
	isoTimestamp = regexp.MustCompile(
		`[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:?[0-9]{2})?`,
	)
	epochMillis = regexp.MustCompile(`\b[0-9]{13,}\b`)
	uuidPattern = regexp.MustCompile(
		`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`,
	)
	unstablePath = regexp.MustCompile(
		`(?:/tmp/|/var/folders/)[^"'` +
			"\t\n\v\f\r " +
			`\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+`,
	)
	randomHexID      = regexp.MustCompile(`(?i)\b[0-9a-f]{16,}\b`)
	randomPrefixedID = regexp.MustCompile(`\b(?:req|run|job|sid|session|corr)[-_][A-Za-z0-9_-]{8,}\b`)
	uuidFragmentHead = regexp.MustCompile(`(?i)^[0-9a-f]{8}-`)
)

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isLineTerminator(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

func asciiDatePrefixAt(text string, at int) bool {
	if at+5 > len(text) {
		return false
	}
	return strings.EqualFold(text[at:at+5], "Date:")
}

// collectDateHeaders mirrors /^Date:\s*.+$/gim. It is hand-scanned because
// JS \s includes every LineTerminator and JS multiline anchors recognize CR,
// LF, U+2028, and U+2029; RE2 differs on both points.
func collectDateHeaders(text string) []string {
	hits := []string{}
	search := 0
	for search < len(text) {
		start := search
		if start > 0 {
			_, prevSize := utf8.DecodeLastRuneInString(text[:start])
			prev, _ := utf8.DecodeRuneInString(text[start-prevSize : start])
			if !isLineTerminator(prev) {
				_, size := utf8.DecodeRuneInString(text[start:])
				search += size
				continue
			}
		}
		if !asciiDatePrefixAt(text, start) {
			_, size := utf8.DecodeRuneInString(text[start:])
			search += size
			continue
		}

		afterColon := start + len("Date:")
		cursor := afterColon
		lastHorizontal := -1
		for cursor < len(text) {
			r, size := utf8.DecodeRuneInString(text[cursor:])
			if !isJSWhitespace(r) {
				break
			}
			if !isLineTerminator(r) {
				lastHorizontal = cursor
			}
			cursor += size
		}

		var end int
		switch {
		case cursor < len(text):
			// Greedy \s* stops at the first non-whitespace; .+ consumes the
			// rest of that logical line.
			end = cursor
			for end < len(text) {
				r, size := utf8.DecodeRuneInString(text[end:])
				if isLineTerminator(r) {
					break
				}
				end += size
			}
		case lastHorizontal >= 0:
			// All remaining input is whitespace. RegExp backtracking leaves
			// the last horizontal whitespace for .+ and then takes the next
			// multiline end anchor.
			end = lastHorizontal
			for end < len(text) {
				r, size := utf8.DecodeRuneInString(text[end:])
				if isLineTerminator(r) {
					break
				}
				end += size
			}
		default:
			end = -1
		}
		if end > afterColon {
			hits = append(hits, text[start:end])
			if end > search {
				search = end
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		search += size
	}
	return hits
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f))
	}
	return utf8.AppendRune(dst, cp)
}

func decodeWTF8(text string, at int) (rune, int) {
	b := text[at]
	if b&0xf0 == 0xe0 && at+2 < len(text) &&
		text[at+1]&0xc0 == 0x80 && text[at+2]&0xc0 == 0x80 {
		cp := rune(b&0x0f)<<12 | rune(text[at+1]&0x3f)<<6 | rune(text[at+2]&0x3f)
		if cp >= 0x800 {
			return cp, 3
		}
	}
	r, size := utf8.DecodeRuneInString(text[at:])
	return r, size
}

func utf16SliceTo(text string, units int) string {
	if units <= 0 {
		return ""
	}
	used := 0
	for at := 0; at < len(text); {
		cp, size := decodeWTF8(text, at)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if used+width > units {
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			return text[:at] + string(appendWTF8(nil, high))
		}
		used += width
		at += size
		if used == units {
			return text[:at]
		}
	}
	return text
}

func excerptOf(match string, maxUnits int) Excerpt {
	trimmed := jscompat.Trim(match)
	if len(utf16.Encode([]rune(trimmed))) <= maxUnits {
		return Excerpt(trimmed)
	}
	return Excerpt(utf16SliceTo(trimmed, maxUnits-1) + "…")
}

func (excerpt Excerpt) MarshalJSON() ([]byte, error) {
	raw := string(excerpt)
	hasSurrogate := false
	for at := 0; at < len(raw); {
		cp, size := decodeWTF8(raw, at)
		if cp >= 0xd800 && cp <= 0xdfff {
			hasSurrogate = true
			break
		}
		at += size
	}
	if !hasSurrogate {
		return jscompat.Stringify(raw)
	}
	var out bytes.Buffer
	out.WriteByte('"')
	var run strings.Builder
	flush := func() error {
		if run.Len() == 0 {
			return nil
		}
		encoded, err := jscompat.Stringify(run.String())
		if err != nil {
			return err
		}
		out.Write(encoded[1 : len(encoded)-1])
		run.Reset()
		return nil
	}
	for at := 0; at < len(raw); {
		cp, size := decodeWTF8(raw, at)
		at += size
		if cp >= 0xd800 && cp <= 0xdfff {
			if err := flush(); err != nil {
				return nil, err
			}
			fmt.Fprintf(&out, "\\u%04x", cp)
		} else {
			run.WriteRune(cp)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	out.WriteByte('"')
	return out.Bytes(), nil
}

func pushUnique(
	out *[]PrefixHygieneViolation,
	seen map[string]struct{},
	kind PrefixHygieneKind,
	match string,
) {
	excerpt := excerptOf(match, 80)
	key := string(kind) + ":" + string(excerpt)
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, PrefixHygieneViolation{Kind: kind, Excerpt: excerpt})
}

// CheckPrefixHygiene scans a prompt prefix without mutating it.
func CheckPrefixHygiene(prompt string) PrefixHygieneResult {
	violations := []PrefixHygieneViolation{}
	seen := map[string]struct{}{}

	for _, hit := range isoTimestamp.FindAllString(prompt, -1) {
		pushUnique(&violations, seen, KindTimestamp, hit)
	}
	for _, hit := range epochMillis.FindAllString(prompt, -1) {
		pushUnique(&violations, seen, KindTimestamp, hit)
	}
	for _, hit := range collectDateHeaders(prompt) {
		pushUnique(&violations, seen, KindTimestamp, hit)
	}
	for _, hit := range uuidPattern.FindAllString(prompt, -1) {
		pushUnique(&violations, seen, KindUUID, hit)
	}
	for _, hit := range unstablePath.FindAllString(prompt, -1) {
		pushUnique(&violations, seen, KindUnstablePath, hit)
	}
	for _, hit := range randomPrefixedID.FindAllString(prompt, -1) {
		pushUnique(&violations, seen, KindRandomID, hit)
	}
	for _, hit := range randomHexID.FindAllString(prompt, -1) {
		if uuidFragmentHead.MatchString(hit) || strings.Contains(hit, "-") {
			continue
		}
		partOfUUID := false
		for _, violation := range violations {
			if violation.Kind == KindUUID &&
				strings.Contains(strings.ToLower(string(violation.Excerpt)), strings.ToLower(hit)) {
				partOfUUID = true
				break
			}
		}
		if !partOfUUID {
			pushUnique(&violations, seen, KindRandomID, hit)
		}
	}
	return PrefixHygieneResult{Clean: len(violations) == 0, Violations: violations}
}

// AssertStablePrefix returns the UTF-16 code-unit index of the first mismatch.
func AssertStablePrefix(promptA, promptB string) StablePrefixResult {
	if promptA == promptB {
		return StablePrefixResult{Stable: true}
	}
	a := utf16.Encode([]rune(promptA))
	b := utf16.Encode([]rune(promptB))
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			at := i
			return StablePrefixResult{Stable: false, FirstDivergence: &at}
		}
	}
	return StablePrefixResult{Stable: false, FirstDivergence: &n}
}
