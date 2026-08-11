package modelui

import "strings"

// Model words, never provider ids (5.10). "anthropic/claude-sonnet-4-20250514"
// is provenance; "claude-sonnet-4" is what a person says out loud, and a chip
// that spent eleven cells on a vendor prefix would be spending them on the one
// part of the string nobody reads.
//
// The rule is ported from the v1 surface (internal/tui/view.go's modelShort) so
// the two surfaces call the same model the same thing during the overlap, and
// it is deliberately conservative: it removes only the four things that are
// provably provenance and never tries to prettify a name it does not recognize.
// An unrecognized slug comes back whole, which is the honest failure — a word
// the reader can still match against a provider's own documentation.

// effortWords is the closed set of reasoning-effort words a slug may carry, in
// the vocabulary internal/provider already speaks ([provider.Effort]). It is a
// closed list because an open one would read "claude-sonnet-4:free" as an
// effort of "free" and put a lie on the chip.
var effortWords = [...]string{"off", "low", "medium", "high"}

// variantSeparator is what a provider hangs a variant off. OpenRouter's slugs
// use it for every variant they publish — ":free", ":online", ":high" — so it
// is the only shape [Effort] recognizes today.
const variantSeparator = ':'

// ModelWord shortens a provider's model slug to the word a person says.
//
// Four things come off, and nothing else does:
//
//	the vendor prefix   "anthropic/claude-sonnet-4" → "claude-sonnet-4"
//	the alias marker    OpenRouter's leading "~"
//	the variant suffix  ":free", ":high" — including the effort words, which
//	                    the chip renders separately (see [Effort])
//	the date suffix     a trailing "-YYYY-MM-DD", which is a release stamp
//
// An empty slug returns the empty string. The caller decides what a missing
// model looks like; this function will not invent a placeholder, because a
// placeholder chosen here would be a fact invented three layers from anyone who
// could check it.
func ModelWord(slug string) string {
	word := strings.TrimSpace(slug)
	if word == "" {
		return ""
	}
	if index := strings.LastIndexByte(word, '/'); index >= 0 && index+1 < len(word) {
		word = word[index+1:]
	}
	word = strings.TrimPrefix(word, "~")
	if base, _, ok := strings.Cut(word, string(variantSeparator)); ok && base != "" {
		word = base
	}
	return dropDateSuffix(word)
}

// Effort is the reasoning effort a slug carries, or the empty string.
//
// Effort has no axis of its own in the journal (12.3.5): there is no
// Provenance field and no column, so a role binding's value is exactly one
// string and the effort is inside it. That is why this is a READER and there is
// no writer beside it — the chip shows what the slug says, and 5.10's "effort
// rides the chip" stays a display promise until the doc is amended and the
// axis exists.
func Effort(slug string) string {
	word := strings.TrimSpace(slug)
	index := strings.LastIndexByte(word, variantSeparator)
	if index < 0 || index+1 >= len(word) {
		return ""
	}
	suffix := strings.ToLower(word[index+1:])
	for _, known := range effortWords {
		if suffix == known {
			return known
		}
	}
	return ""
}

// dropDateSuffix removes a trailing release stamp, in the two shapes providers
// actually publish: "-YYYYMMDD" (anthropic/claude-sonnet-4-20250514) and
// "-YYYY-MM-DD" (the hyphenated form the v1 surface was written against).
//
// Both are checked in FULL — the digit counts and the digits — so a model
// genuinely named with a trailing number keeps it. "llama-3.3-70b" survives;
// so would a hypothetical "gpt-5-2026", because four digits alone is a year and
// not a stamp, and guessing at that would rename a model on the user's screen.
func dropDateSuffix(word string) string {
	parts := strings.Split(word, "-")
	if len(parts) >= 2 && isDigits(parts[len(parts)-1], 8) {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	if len(parts) >= 4 &&
		isDigits(parts[len(parts)-3], 4) &&
		isDigits(parts[len(parts)-2], 2) &&
		isDigits(parts[len(parts)-1], 2) {
		return strings.Join(parts[:len(parts)-3], "-")
	}
	return word
}

func isDigits(value string, n int) bool { return len(value) == n && allDigits(value) }

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// lower is ASCII-only and BYTE-LENGTH PRESERVING, which is the property the
// match highlighter depends on: an offset found in the lowered form has to
// index the original directly, and strings.ToLower does not promise that (İ
// lowers to two bytes). Model slugs and role words are ASCII by construction;
// a query is not, and this is what keeps a pasted non-ASCII query from painting
// an escape sequence into the middle of a rune.
func lower(s string) string {
	upper := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			upper = i
			break
		}
	}
	if upper < 0 {
		return s
	}
	out := make([]byte, len(s))
	copy(out, s[:upper])
	for i := upper; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out[i] = b
	}
	return string(out)
}
