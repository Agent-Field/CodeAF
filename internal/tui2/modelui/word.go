package modelui

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE ROLE WORDS, and they are the settings page's own.
//
// The five slots used to reach this surface spelled the way the roles table
// names them for itself — voice, architect, hands, skeptic, clerk — and a user
// who clicked a model row was handed that list and reported it as "some weird
// lists like voice, architect, skeptic". They were right, and §14 says why:
// user-facing words only, and a metaphor nobody was taught is an invented
// concept however evocative it reads. Nobody has an architect; they have
// planning.
//
// The words below are NOT a third vocabulary either. They are exactly the
// labels the settings page already shows on its landed role-model rows
// (internal/config's roleWords), so one slot has one spelling on the sheet, in
// this picker, and on the palette row that opens either. word_test.go walks
// [store.ModelRoles] against the settings registry and fails the build if the
// two ever disagree or if a sixth role arrives with no word here.
//
// [store.ModelRole.Word] keeps its own answer and keeps its own job: it is the
// journal's name for a slot, at the store's altitude, and no v2 surface draws
// it any more.
var roleWords = map[store.ModelRole]string{
	store.RoleOrchestrate: "conversation",
	store.RolePlan:        "planning",
	store.RoleWork:        "execution",
	store.RoleVerify:      "verification",
	store.RoleScribe:      "naming",
}

// RoleWord is the plain word for one of the five slots — what a row names
// itself on every surface in this tree.
//
// A role with no word here degrades to the role's own spelling rather than to
// nothing: a row with a blank name reads as a rendering fault, and the test
// above is what makes the degradation unreachable rather than tolerated.
func RoleWord(role store.ModelRole) string {
	if word := roleWords[role]; word != "" {
		return word
	}
	return string(role)
}

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
//	the alias suffix    a trailing "-latest", which is a POINTER at a release
//	                    rather than the name of one — the same kind of fact as
//	                    the date it stands in for, and the exact string a reader
//	                    met on the live build as `deepseek-v4-flash-latest`
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
	return dropAliasSuffix(dropDateSuffix(word))
}

// aliasSuffix is the moving pointer providers hang off a family name. It is
// checked as a WHOLE trailing segment — "-latest" and not "latest" anywhere —
// so a model genuinely called something-latest-something keeps its name, and a
// slug that is nothing BUT the marker keeps it too rather than coming back
// empty.
const aliasSuffix = "-latest"

func dropAliasSuffix(word string) string {
	if base := strings.TrimSuffix(word, aliasSuffix); base != "" && base != word {
		return base
	}
	return word
}

// variantWord is the variant a slug carries after [variantSeparator], lowered,
// or the empty string for a plain slug. It is deliberately open where
// [Effort]'s list is closed: Effort must not misread ":free" as a reasoning
// effort, but a picker row must show WHATEVER the provider hung off the slug —
// an unshown variant is how two rows wear the same word and only one of them
// works.
func variantWord(slug string) string {
	word := strings.TrimSpace(slug)
	index := strings.LastIndexByte(word, variantSeparator)
	if index < 0 || index+1 >= len(word) {
		return ""
	}
	return lower(word[index+1:])
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
