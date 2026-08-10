// LocaleCompare — String.prototype.localeCompare as Bun 1.2.23 (JavaScriptCore
// + bundled ICU) executes it, for the codeaf harness's reachable string domain.
//
// Two ported call sites depend on it and both change bytes on the wire:
//
//   - `deterministicStringify` (@openrouter/ai-sdk-provider/dist/internal/
//     index.mjs:2576-2596) recursively sorts every object key with
//     `entries.sort(([a],[b]) => a.localeCompare(b))` before JSON.stringify,
//     so that assistant `tool_calls[].function.arguments` round-trip through
//     Anthropic's signature validation. Wrong order → a *valid* body whose
//     signatures fail, which reads as a provider bug, not a port bug
//     (ENGINE-DESIGN.md R0).
//   - `src/session/llm.ts:308`,
//     `Object.entries(tools).toSorted(([a],[b]) => a.localeCompare(b))`, which
//     fixes tool order in the request body and therefore prompt-cache keys.
//
// `strings.Compare` is NOT a stand-in: the CLDR root collation orders "_"
// before "-", every punctuation mark before the digits, and lowercase before
// uppercase at the tertiary level.
//
// ── seams ──
//
//   - The collation data. TS gets ICU from the Bun binary; Go gets it from
//     golang.org/x/text/collate, the first and only third-party dependency in
//     this module (ENGINE-DESIGN.md F3 held the line at zero until here; the
//     alternative was hand-transcribing DUCET). Pinned at v0.24.0 — the
//     collate tables are byte-identical in every release from v0.18.0 to
//     v0.38.0 (UnicodeVersion 6.2.0, CLDRVersion 23), and v0.24.0 is the
//     newest that still declares `go 1.23.0`.
//   - The locale. JSC resolves the default collator to "en-US" regardless of
//     LANG (verified: LANG=C.UTF-8 and LANG=en_US.UTF-8 produce identical
//     results over 817,281 pairs), and CLDR gives `en` no collation
//     tailorings, so `language.Und` is the correct Go tag. `language.Und` and
//     `language.MustParse("en-US")` were also verified to agree on every
//     corpus below.
//
// ── sibling ports ──
//
//   None. This file is a leaf; `deterministicStringify` and the llm.ts:308
//   tool sort consume LocaleCompare/LocaleSortStrings.
//
// ── fidelity notes (deliberate, do not "fix") ──
//
//   - Returns exactly -1/0/1. The ECMA-402 spec only requires a sign, but JSC
//     returns -1/0/1 and fixtures record those literal values.
//   - Distinct strings CAN compare 0 — the default collator strength is
//     tertiary with no identical level, so "Å" (Å), "Å" and
//     "Å" (ANGSTROM SIGN) are all equal, as are strings differing only by
//     a completely-ignorable code point ("a­b" == "ab"). Callers must
//     therefore use a STABLE sort (sort.SliceStable / LocaleSortStrings) to
//     match JS `Array.prototype.sort`, which is spec-stable since ES2019.
//   - Invalid UTF-8 has no JS counterpart (a JS string is UTF-16 and can hold
//     lone surrogates, which cannot survive the JSON transport the fixtures
//     use). Bad bytes do not panic and do not collapse to U+FFFD — they sort
//     after it — but their order is x/text's, not anything JSC would produce.
//     Both real call sites read keys out of parsed JSON, so this is
//     unreachable; it is stated so nobody "fixes" it by pre-sanitizing.
//   - Numeric collation is OFF: "10" < "9". Punctuation is NOT ignored
//     (alternate=non-ignorable): "a b" < "ab". Both match the resolved
//     Intl.Collator options {usage:"sort", sensitivity:"variant",
//     ignorePunctuation:false, collation:"default", numeric:false,
//     caseFirst:"false"}.
//
// ── coverage boundary (KNOWN DIVERGENCE, pinned by TestKnownDivergences) ──
//
// x/text's collation tables are frozen at Unicode 6.2.0 / CLDR 23 (2013);
// Bun ships a modern ICU. Every code point assigned after Unicode 6.2 is
// unknown to x/text and gets an implicit weight, so it sorts AFTER every
// character x/text does know, whereas ICU files it under its real primary
// weight. Measured over all 1,112,063 non-surrogate code points against a
// 20-string probe ladder: 40,230 code points (3.618%) diverge. By block:
//
//	ASCII (incl. C0 + DEL)     0/128     Cyrillic                 0/256
//	Latin-1 Supplement         0/96      Hangul Syllables         0/11172
//	Latin Extended-A/B         0/336     Halfwidth/Fullwidth      0/240
//	IPA + combining marks      0/288     Math alphanumeric        0/1024
//	Greek                      1/144     CJK URO              38/20992
//	Hebrew                     3/112     CJK Ext-A          1746/6592
//	Arabic                     9/256     CJK Ext-B         12581/42720
//	Devanagari                 4/128     Emoticons                4/80
//	Thai                       3/128     Misc Sym & Pictographs 235/768
//
// The two shapes behind that table:
//
//  1. post-6.2 assignments (U+037F, U+0605, U+0890, most emoji added after
//     Unicode 6.2 such as U+1F642 SLIGHTLY SMILING FACE, and the CJK URO
//     extensions) — x/text sorts them last, ICU sorts them in place;
//  2. Han beyond the URO. CLDR root orders Han by radical-stroke. U+4E00..
//     U+9FA5 is *already* arranged in radical-stroke order, so code-point
//     order happens to agree there, but Ext-A (U+3400..) and Ext-B
//     (U+20000..) interleave with the URO under CLDR while x/text's DUCET
//     implicit weights append them wholesale. Hence "\U00020000" < "日" in
//     JSC and "日" < "\U00020000" here.
//
// This is a data-version problem, not a logic problem, and it is entirely
// OUTSIDE the domain R0 cares about. Proven exhaustively, order AND
// equality-classes, with zero divergence:
//
//	every 1- and 2-char string over printable ASCII        9,121 strings
//	every 1- and 2-char string over 7-bit ASCII + C0/DEL  16,513 strings
//	every 3- and 4-char string over [aAbZ09_-./]          11,000 strings
//	randomized identifier+Latin-1+combining-mark words     see testdata
//
// Tool names are registry-defined ASCII identifiers; tool-argument keys are
// model-authored but are the ASCII keys of the JSON Schema the model was
// handed. No hand-rolled correction is therefore shipped: correcting the
// divergent set would mean transcribing ~40k ICU collation elements, and every
// candidate correction lies outside the reachable domain. The divergence is
// pinned rather than merely documented — testdata/localecompare.json carries
// an explicit divergence list and TestKnownDivergences fails if any of those
// pairs starts AGREEING, which is the signal that the tables moved and this
// note needs rewriting.

package jscompat

import (
	"sort"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// collate.Collator carries two mutable iterators and a sorter, so a single
// shared value is not safe for concurrent use. One per goroutine via a pool;
// constructing a Collator is just a table lookup plus option copy.
var localeCollators = sync.Pool{
	New: func() any { return collate.New(language.Und) },
}

// LocaleCompare is `a.localeCompare(b)` under the default collator: -1 if a
// sorts before b, 1 if after, 0 if they are collation-equal (which does NOT
// imply a == b — see the fidelity notes).
func LocaleCompare(a, b string) int {
	c := localeCollators.Get().(*collate.Collator)
	r := c.CompareString(a, b)
	localeCollators.Put(c)
	switch {
	case r < 0:
		return -1
	case r > 0:
		return 1
	default:
		return 0
	}
}

// LocaleSortStrings is `ss.toSorted((a, b) => a.localeCompare(b))`: it returns
// a NEW slice and leaves ss untouched, and it is stable, so collation-equal
// strings keep their input order exactly as JS's spec-stable sort does.
//
// Sites that sort key/value pairs rather than bare strings (both real callers
// do) should reach for LocaleCompare inside sort.SliceStable instead — this
// helper exists so they do not each re-derive "stable, and less means < 0".
func LocaleSortStrings(ss []string) []string {
	out := make([]string, len(ss))
	copy(out, ss)
	sort.SliceStable(out, func(i, j int) bool { return LocaleCompare(out[i], out[j]) < 0 })
	return out
}
