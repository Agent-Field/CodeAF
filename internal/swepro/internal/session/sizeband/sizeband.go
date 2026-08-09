// T2 chunk-size estimator — port of src/session/size-band.ts.
//
// A deterministic, pure function that assigns a SizeBand to a task from static
// signals (description shape, acceptance criteria count, file_scope width,
// dependency fan-in, prose-clause density, section count), deferring to an
// explicit `scope:*` tag as an authoritative override.
//
// Zero I/O, zero LLM calls, no clock, no RNG — so there is nothing to inject
// for testing and no SetClockForTesting hook in this package.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - SizeBand is declared HERE. TS declares it in leaf-outcome.ts and
//     size-band.ts re-exports it (`export type { SizeBand }`) while
//     leaf-outcome.ts imports estimateSizeBand back — a type-only cycle TS
//     tolerates and Go does not. Homing the type in this package breaks it.
//
//   - Every character threshold counts UTF-16 code units (JS `.length`), not
//     runes and not bytes: an emoji costs 2 toward DESC_CHARS_*.
//
//   - The two module-level `/gm` regexes are hand-rolled scanners. RE2's
//     `(?m)^` only fires after `\n`, while JS `^` in multiline mode also fires
//     after `\r`, U+2028 and U+2029; and JS `\s` is a much wider class than
//     RE2's. Both scanners reproduce the global-match loop exactly, including
//     the fact that `\s*` / `\s+` swallow line terminators, so ONE match can
//     span several lines and consume the line starts inside it.
//
//   - `String.prototype.match` with a `/g` regex resets `lastIndex` to 0 before
//     scanning (spec step in %Symbol.match%), so sharing the two module-level
//     regexes across calls is NOT stateful in TS either. Nothing to emulate.
//
//   - KNOWN UNPORTABLE DIVERGENCE (size-band.ts:42-43): SCOPE_BAND is a plain
//     object literal, so `SCOPE_BAND[k]` walks Object.prototype. Two lowercase
//     keys survive `.toLowerCase()` and hit it: `scope:constructor` yields the
//     Object constructor function and `scope:__proto__` yields Object.prototype
//     — both truthy, so TS SHORT-CIRCUITS and returns a non-SizeBand value
//     (`JSON.stringify` of them is `undefined` and `{}` respectively). Go's map
//     lookup has no prototype chain, so those two tags fall through to static
//     scoring instead. Not representable in a function typed `-> SizeBand`;
//     recorded here rather than faked.
package sizeband

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/policyline"
	"github.com/Agent-Field/swe-pro-go/internal/session/specclauses"
)

// SizeBand is TS `type SizeBand = "xs" | "s" | "m" | "l" | "xl"`
// (declared in leaf-outcome.ts, re-exported by size-band.ts — see the package
// doc for why it is homed here).
type SizeBand string

const (
	BandXS SizeBand = "xs"
	BandS  SizeBand = "s"
	BandM  SizeBand = "m"
	BandL  SizeBand = "l"
	BandXL SizeBand = "xl"
)

// bands is `const BANDS: readonly SizeBand[]` — indexed by totalPointsToIndex.
var bands = []SizeBand{BandXS, BandS, BandM, BandL, BandXL}

// ---------------------------------------------------------------------------
// Signal 1: explicit scope tag. Authoritative — short-circuits everything below.
// The real scope vocabulary is `tiny|small|medium|large`; `trivial` is a
// design-doc-compat alias for `tiny`.
//
// A bare Go map is safe here: it is only ever point-queried, never ranged, so
// no iteration order can reach the output.
var scopeBand = map[string]SizeBand{
	"trivial": BandXS,
	"tiny":    BandXS,
	"small":   BandS,
	"medium":  BandM,
	"large":   BandL,
}

const scopePrefix = "scope:"

// explicitScopeBand mirrors explicitScopeBand (size-band.ts:38). The TS
// `typeof t !== "string"` guard is unreachable through a []string and is
// dropped. A nil slice is TS `undefined` and an empty slice is TS `[]`; both
// iterate zero times, so the `if (!tags)` early return needs no separate arm.
func explicitScopeBand(tags []string) (SizeBand, bool) {
	for _, t := range tags {
		if !strings.HasPrefix(t, scopePrefix) {
			continue
		}
		// `t.slice("scope:".length)` — the prefix is ASCII and already
		// matched, so a byte slice is the same as a UTF-16 unit slice.
		band, ok := scopeBand[jsToLowerCase(jscompat.Trim(t[len(scopePrefix):]))]
		// `if (band)` — a truthiness check, so an empty-string value would be
		// skipped. No entry in the table is empty, but the shape is kept.
		if ok && band != "" {
			return band, true
		}
	}
	return "", false
}

// jsToLowerCase is String.prototype.toLowerCase, which uses the Unicode
// *full* (locale-independent) lowercase mapping. Go's unicode.ToLower is the
// *simple* mapping, and the two disagree on U+0130 LATIN CAPITAL LETTER I WITH
// DOT ABOVE: JS produces "i" + U+0307 (two units), Go produces "i". That single
// difference is observable here — "scope:TİNY" lowercases to "ti"+U+0307+"ny" in
// JS (no table hit, falls through to static scoring) but would hit "tiny"
// under a naive port. U+0130 is the ONLY unconditional multi-character
// lowercase mapping in SpecialCasing.txt.
//
// Not emulated: the Final_Sigma conditional (Σ→ς at word end). Its output is
// non-ASCII either way, so it can never change whether the result equals one of
// the five all-ASCII table keys.
func jsToLowerCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0x0130 {
			b.WriteString("i\u0307")
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Signal 2: description shape (chars + line count).
const (
	descCharsS = 120  // ~1-2 sentences: fits a one-line ask
	descCharsM = 400  // a short paragraph: typical single-file task spec
	descCharsL = 1200 // multi-paragraph spec: likely several concerns

	descLinesS = 4
	descLinesM = 12
	descLinesL = 30
)

func descriptionPoints(description string) float64 {
	chars := utf16Len(description) // JS `.length` — UTF-16 code units
	lines := 0
	if chars != 0 {
		// `description.split("\n").length`. '\n' cannot occur inside a
		// multi-byte UTF-8 sequence, so a byte count is exact.
		lines = strings.Count(description, "\n") + 1
	}
	var byChars float64
	switch {
	case chars <= descCharsS:
		byChars = 0
	case chars <= descCharsM:
		byChars = 1
	case chars <= descCharsL:
		byChars = 2
	default:
		byChars = 3
	}
	var byLines float64
	switch {
	case lines <= descLinesS:
		byLines = 0
	case lines <= descLinesM:
		byLines = 1
	case lines <= descLinesL:
		byLines = 2
	default:
		byLines = 3
	}
	// Combine by taking whichever metric indicates the bigger task.
	return math.Max(byChars, byLines)
}

// ---------------------------------------------------------------------------
// Signal 3: acceptance criteria count — `/^\s*-\s*\[[ xX]?\]/gm`.

func acceptanceCriteriaCount(description string) int {
	return countAcceptanceMatches(utf16Of(description))
}

const (
	acceptanceFew     = 2 // 1-2 items: still a tightly bounded task
	acceptanceSeveral = 5 // 3-5 items: a real multi-step task
)

func acceptancePoints(count int) float64 {
	if count <= 0 {
		return 0
	}
	if count <= acceptanceFew {
		return 1
	}
	if count <= acceptanceSeveral {
		return 2
	}
	return 3
}

// ---------------------------------------------------------------------------
// Signal 4: file_scope width, via policy-line.ts's shared parser.
const (
	filesFew     = 3 // 2-3 files: still one coherent change
	filesSeveral = 7 // 4-7 files: a real cross-cutting change
)

func fileScopeCount(description string) int {
	line := policyline.PolicyLine(&description, "file_scope")
	// `parseFileScope(line)?.length ?? 0` — a nil slice is TS `undefined`
	// and an empty slice is TS `[]`; both have length 0.
	return len(policyline.ParseFileScope(line))
}

func fileScopePoints(count int) float64 {
	if count <= 1 { // 0 (absent/unknown) or 1 file: minimal integration surface
		return 0
	}
	if count <= filesFew {
		return 1
	}
	if count <= filesSeveral {
		return 2
	}
	return 3
}

// ---------------------------------------------------------------------------
// Signal 5: dependency fan-in.
const (
	fanInFew     = 1 // 0-1 upstream: trivial or none
	fanInSeveral = 4
	fanInMany    = 9
)

// fanInPoints takes TS `number | undefined`. The leading `!fanIn` is a
// truthiness test, so 0, -0 and NaN all score 0 just like undefined.
func fanInPoints(fanIn *float64) float64 {
	if fanIn == nil || !jscompat.Truthy(*fanIn) || *fanIn <= fanInFew {
		return 0
	}
	if *fanIn <= fanInSeveral {
		return 1
	}
	if *fanIn <= fanInMany {
		return 2
	}
	return 3
}

// ---------------------------------------------------------------------------
// Signal 6: spec-clause density (P4), via spec-clauses.ts's countSpecClauses,
// minus the acceptance count so a checklist is never scored twice.
const (
	clausesMany = 12 // a dense, multi-requirement spec
	clausesLots = 24 // a large, multi-capability spec
	clausesHuge = 40 // a very large, crate-scale spec
)

func specClausePoints(proseClauses float64) float64 {
	// Conservative floor: a handful of clauses contributes NOTHING.
	if proseClauses < clausesMany {
		return 0
	}
	if proseClauses < clausesLots {
		return 1
	}
	if proseClauses < clausesHuge {
		return 2
	}
	return 3
}

// ---------------------------------------------------------------------------
// Signal 7: subsystem/section count (P4) — `/^#{1,6}\s+\S/gm`.
const (
	sectionsSeveral = 5  // organized into several distinct sections
	sectionsMany    = 9  // a multi-subsystem spec
	sectionsLots    = 16 // a sprawling, many-subsystem spec
)

func sectionCount(description string) int {
	return countHeadingMatches(utf16Of(description))
}

func sectionPoints(count int) float64 {
	if count < sectionsSeveral {
		return 0
	}
	if count < sectionsMany {
		return 1
	}
	if count < sectionsLots {
		return 2
	}
	return 3
}

// ---------------------------------------------------------------------------
// Combine into a band index. Baseline is "xs"; every signal only ever adds.
const (
	totalS = 2 // any real signal at all bumps past "xs"
	totalM = 5
	totalL = 8
)

func totalPointsToIndex(total float64) int {
	if total <= 0 {
		return 0
	}
	if total <= totalS {
		return 1
	}
	if total <= totalM {
		return 2
	}
	if total <= totalL {
		return 3
	}
	return 4
}

// noSignalDefault — no static signal present at all. Defaults to "m", matching
// T1's tag-only placeholder fallback.
const noSignalDefault SizeBand = BandM

// EstimateSizeBandInput is the TS inline parameter object of estimateSizeBand.
// Field order matches the TS object type declaration order.
type EstimateSizeBandInput struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	// DependencyFanIn is the number of tasks feeding into this one, if known.
	// nil is TS `undefined`.
	DependencyFanIn *float64 `json:"dependencyFanIn"`
}

// EstimateSizeBand mirrors estimateSizeBand (size-band.ts:214).
func EstimateSizeBand(input EstimateSizeBandInput) SizeBand {
	if explicit, ok := explicitScopeBand(input.Tags); ok {
		return explicit
	}

	// `input.description ?? ""` — a no-op for a Go string; every TS call site
	// already passes `... ?? ""` (cut-policy.ts:76, leaf-outcome.ts:147).
	description := input.Description
	acceptance := acceptanceCriteriaCount(description)
	files := fileScopeCount(description)
	fanIn := input.DependencyFanIn

	if utf16Len(description) == 0 && acceptance == 0 && files == 0 &&
		(fanIn == nil || !jscompat.Truthy(*fanIn)) {
		return noSignalDefault
	}

	// P4 signals: prose-clause density and section count. `proseClauses`
	// subtracts the acceptance (checkbox) count so a checklist is never scored
	// by both Signal 3 and Signal 6.
	proseClauses := math.Max(0, specclauses.CountSpecClauses(description)-float64(acceptance))
	sections := sectionCount(description)
	total := descriptionPoints(description) +
		acceptancePoints(acceptance) +
		fileScopePoints(files) +
		fanInPoints(fanIn) +
		specClausePoints(proseClauses) +
		sectionPoints(sections)
	return bands[totalPointsToIndex(total)]
}

// ---------------------------------------------------------------------------
// hand-rolled /gm scanners

// countAcceptanceMatches reproduces
//
//	description.match(/^\s*-\s*\[[ xX]?\]/gm)?.length ?? 0
//
// as the spec's global-match loop: lastIndex starts at 0; find the leftmost
// match starting at an index >= lastIndex; on success count it and set
// lastIndex to the match END (matches never overlap, and this pattern can
// never match empty, so no zero-length bump is needed).
//
// `^` under /m succeeds at index 0 and after any LineTerminator unit (\n, \r,
// U+2028, U+2029) — NOT merely after \n like RE2's (?m)^.
//
// No backtracking is required anywhere: `\s*` is greedy and both characters it
// is followed by ('-' and '[') are non-space, so a shorter run always fails at
// the very same position the maximal run was tested at. The one real backtrack
// point is `[ xX]?`, handled explicitly below.
func countAcceptanceMatches(u []uint16) int {
	n := 0
	for i := 0; i <= len(u); {
		if !(i == 0 || isLineTerminatorUnit(u[i-1])) {
			i++
			continue
		}
		if end, ok := matchAcceptanceAt(u, i); ok {
			n++
			i = end
			continue
		}
		i++
	}
	return n
}

func matchAcceptanceAt(u []uint16, i int) (int, bool) {
	j := i
	for j < len(u) && isJSSpaceUnit(u[j]) { // \s*
		j++
	}
	if j >= len(u) || u[j] != '-' {
		return 0, false
	}
	j++
	for j < len(u) && isJSSpaceUnit(u[j]) { // \s*
		j++
	}
	if j >= len(u) || u[j] != '[' {
		return 0, false
	}
	j++
	// `[ xX]?` is greedy: try to consume one unit first.
	if j < len(u) && (u[j] == ' ' || u[j] == 'x' || u[j] == 'X') {
		if j+1 < len(u) && u[j+1] == ']' {
			return j + 2, true
		}
		// Backtrack to the empty alternative: `\]` must then match u[j],
		// which is one of ' ', 'x', 'X' — never ']'. Dead end.
		return 0, false
	}
	if j < len(u) && u[j] == ']' {
		return j + 1, true
	}
	return 0, false
}

// countHeadingMatches reproduces
//
//	description.match(/^#{1,6}\s+\S/gm)?.length ?? 0
//
// with the same global-match loop as above. `#{1,6}` is greedy and capped at
// 6, so a run of 7+ '#' fails outright: after taking six, `\s+` sees another
// '#', and every backtrack step to 5..1 also lands on a '#'. `\s+` is greedy
// and `\S` is its exact complement, so backtracking `\s+` can never help
// either — if the maximal space run ends at end-of-input, the match fails.
func countHeadingMatches(u []uint16) int {
	n := 0
	for i := 0; i <= len(u); {
		if !(i == 0 || isLineTerminatorUnit(u[i-1])) {
			i++
			continue
		}
		if end, ok := matchHeadingAt(u, i); ok {
			n++
			i = end
			continue
		}
		i++
	}
	return n
}

func matchHeadingAt(u []uint16, i int) (int, bool) {
	hashes := 0
	j := i
	for j < len(u) && hashes < 6 && u[j] == '#' { // #{1,6}, greedy
		j++
		hashes++
	}
	if hashes == 0 {
		return 0, false
	}
	// Greedy #{1,6} backtracks from `hashes` down to 1. Every shorter count
	// leaves the cursor on a '#', which `\s+` rejects, so only the maximal
	// count can ever succeed — the loop is written out anyway to keep the
	// regex's shape visible.
	for c := hashes; c >= 1; c-- {
		p := i + c
		q := p
		for q < len(u) && isJSSpaceUnit(u[q]) { // \s+
			q++
		}
		if q == p {
			continue // no whitespace at all
		}
		if q < len(u) { // \S — the unit after the maximal space run
			return q + 1, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// UTF-16 helpers

func utf16Of(s string) []uint16 { return utf16.Encode([]rune(s)) }

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// isJSSpaceUnit is the JS `\s` class: WhiteSpace + LineTerminator, i.e.
// [\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]
// (written as escapes so the source stays BOM-free and greppable).
func isJSSpaceUnit(c uint16) bool {
	switch c {
	case 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return c >= 0x2000 && c <= 0x200a
}

// isLineTerminatorUnit is the JS LineTerminator set that `^` under /m anchors
// after: \n, \r, U+2028, U+2029.
func isLineTerminatorUnit(c uint16) bool {
	return c == 0x0a || c == 0x0d || c == 0x2028 || c == 0x2029
}
