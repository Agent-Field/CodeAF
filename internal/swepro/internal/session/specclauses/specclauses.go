// Spec clause inventory — PURE SUBSET of src/session/spec-clauses.ts.
//
// SCOPE / CUT LINE. The TS module is half heuristic, half LLM+fs plumbing.
// This package ports only the deterministic half:
//
//	ported   spec-clauses.ts:19-34   interface SpecClauseJudgment  (data shape)
//	ported   spec-clauses.ts:50-54   MATRIX_MIN_CLAUSES / MATRIX_MAX_CELLS /
//	                                 MATRIX_CELL_MAXLEN
//	ported   spec-clauses.ts:61-71   clampMatrixCells
//	ported   spec-clauses.ts:123-142 FALLBACK_BEHAVIOR_RE + countSpecClauses
//	CUT AT   spec-clauses.ts:13-17   node:crypto / node:fs / node:path / zod /
//	                                 ./low-judge imports
//	CUT      spec-clauses.ts:36-46   clauseSchema, matrixSchema      (zod)
//	CUT      spec-clauses.ts:73-116  JUDGE_PROMPT_HEADER, MATRIX_PROMPT_HEADER,
//	                                 buildMatrixPrompt               (prompt text
//	                                 for the cut LLM stage)
//	CUT      spec-clauses.ts:144-249 memoPath, specKey,
//	                                 getSpecClauseJudgment           (fs + crypto
//	                                 + low-judge)
//
// No clock and no RNG are read anywhere in the ported subset, so there is no
// injectable now()/random() here.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - countSpecClauses returns a JS `number`, so the Go twin returns float64 and
//     keeps the `bullets + Math.floor(size/3) + Math.floor(sentences/2)`
//     expression shape verbatim (float divides, math.Floor, same accumulation
//     order).
//
//   - Every length/offset that JS observes is a UTF-16 CODE UNIT count, and two
//     of them are load-bearing: the backtick-token `{2,40}` quantifier and the
//     `s.length < 15` sentence floor. Both are therefore evaluated over
//     utf16.Encode units, not runes — "`" + one emoji + "`" IS a token in JS
//     (2 units) but would not be under rune counting.
//
//   - The sentence splitter /(?<=[.!?])\s+|\n+/ uses a LOOKBEHIND, which RE2
//     cannot express. splitSentences is a hand-rolled scanner reproducing
//     ECMA-262 String.prototype.split step-for-step (SplitMatch is ANCHORED at
//     q, alternative 1 is tried before alternative 2, both are greedy, and the
//     `e === p` empty-progress guard is kept even though this separator can
//     never match empty). Alternative ordering is observable: after a `.` a run
//     of whitespace is eaten by `\s+` (so ".\n\n  " is ONE separator), whereas
//     with no `.!?` before it only the `\n` run is eaten and following spaces
//     survive into the next sentence.
//
//   - JS `\s` ≠ RE2 `\s`; every ported regex spells the class out (jsSpace).
//
//   - FALLBACK_BEHAVIOR_RE and /^ifs?\b/ carry the `i` flag. Go's `(?i)` uses
//     Unicode simple case folding, which JS's non-unicode `i` does NOT: `(?i)s`
//     folds U+017F (ſ) in Go, so "muſt" would match `must` in Go and does not in
//     V8. Both regexes are therefore hand-expanded into explicit ASCII [Aa]
//     classes instead of using (?i). RE2's \w and \b are already ASCII-only,
//     matching JS's non-unicode \w and \b exactly.
//
//   - The token Set keys on the raw UTF-16 units (two bytes per unit), not on a
//     decoded Go string, so two JS strings that differ only in an unpaired
//     surrogate stay two distinct Set entries — decoding would collapse both to
//     U+FFFD and undercount tokens.size.
//
// Three details are kept for fidelity even though a mutation sweep proved them
// UNOBSERVABLE, so nobody "simplifies" them back on the grounds that no test
// notices:
//
//   - `if (!spec) return 0` is dead for a string argument — the "" path already
//     falls through to 0 + Math.floor(0/3) + Math.floor(0/2). It only bites for
//     the null/undefined callers the TS types forbid.
//
//   - leadingIfRe is anchored with `^`, not `(?m)^`, and no split part can ever
//     contain a "\n": alternative 2 of the separator fires at EVERY otherwise
//     unconsumed newline, so the multiline flag would have nothing to match on.
//
//   - the `[Ii][Ff][Ss]?` expansion is fold-equivalent to `(?i)ifs?` under RE2
//     (the only extra fold is ſ→s in the optional third position, and the `s?`
//     empty branch plus RE2's ASCII \b already accepts those inputs). It stays
//     expanded to match fallbackBehaviorRe, where the fold IS observable.
package specclauses

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// SpecClauseJudgment mirrors the TS interface; JSON tags follow its declaration
// order.
//
// `matrix?: string[]` is genuinely optional — spec-clauses.ts:248 spreads the
// key in only when the matrix is defined, so an ABSENT matrix and a PRESENT
// empty matrix are different JSON. A *[]string with omitempty is the only shape
// that reproduces both (nil ⇒ key absent, &[]string{} ⇒ `"matrix":[]`); this is
// the same deliberate exception to the repo-wide no-omitempty rule that
// auditconvergence documents.
type SpecClauseJudgment struct {
	Clauses []string  `json:"clauses"`
	Count   float64   `json:"count"`
	Source  string    `json:"source"` // "llm" | "fallback" | "cache"
	Matrix  *[]string `json:"matrix,omitempty"`
}

// MatrixMinClauses mirrors MATRIX_MIN_CLAUSES: a spec with fewer clauses than
// this has no meaningful product to probe, so the second (matrix) LLM call is
// skipped entirely for cost discipline.
const MatrixMinClauses = 4

// MatrixMaxCells mirrors MATRIX_MAX_CELLS: hard cap on emitted interaction
// cells (~60, per the closure design).
const MatrixMaxCells = 60

// MatrixCellMaxlen mirrors MATRIX_CELL_MAXLEN: per-cell character clip.
const MatrixCellMaxlen = 160

// ---------------------------------------------------------------------------
// regexes

// jsSpace is the JS `\s` class written out; RE2's `\s` is only [\t\n\f\r ].
const jsSpace = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// jsNonSpace is JS `\S` — the complement of jsSpace. Note a Go `[^…]` class
// matches \n unless \n is listed, and it is.
const jsNonSpace = `[^\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	// /^(?:[-*+]|\d+[.)])\s+\S/ — JS `\d` is [0-9] even without /u, so the
	// explicit range is exact (Arabic-Indic digits must NOT count as bullets).
	bulletRe = regexp.MustCompile(`^(?:[-*+]|[0-9]+[.)])` + jsSpace + `+` + jsNonSpace)

	// FALLBACK_BEHAVIOR_RE (spec-clauses.ts:123-124), with the /i flag
	// hand-expanded to ASCII-only case classes — see the package doc.
	fallbackBehaviorRe = regexp.MustCompile(`\b(?:` + strings.Join([]string{
		`[Mm][Uu][Ss][Tt]`,
		`[Ss][Hh][Oo][Uu][Ll][Dd]`,
		`[Ss][Hh][Aa][Ll][Ll]`,
		`[Mm][Ee][Aa][Nn][Ss]?`,
		`[Ss][Uu][Pp][Pp][Rr][Ee][Ss][Ss]\w*`,
		`[Aa][Pp][Pp][Ll][Ii][Ee]\w*`,
		`[Ii][Gg][Nn][Oo][Rr]\w*`,
		`[Ss][Uu][Pp][Pp][Oo][Rr][Tt]\w*`,
		`[Aa][Cc][Cc][Ee][Pp][Tt]\w*`,
		`[Cc][Oo][Uu][Nn][Tt]\w*`,
		`[Ii][Nn][Cc][Rr][Ee][Mm][Ee][Nn][Tt]\w*`,
		`[Tt][Aa][Kk][Ee][Ss]? [Ee][Ff][Ff][Ee][Cc][Tt]`,
		`[Ff][Aa][Ll][Ll][Ss]? [Bb][Aa][Cc][Kk]`,
		`[Dd][Oo][Mm][Ii][Nn][Aa][Tt]\w*`,
		`[Rr][Ee][Jj][Ee][Cc][Tt]\w*`,
		`[Cc][Oo][Mm][Bb][Ii][Nn]\w*`,
		`[Pp][Rr][Ee][Ss][Ee][Rr][Vv]\w*`,
		`[Nn][Oo][Rr][Mm][Aa][Ll][Ii][Zz]\w*`,
		`[Dd][Ee][Dd][Uu][Pp]\w*`,
		`[Dd][Ee][Ff][Aa][Uu][Ll][Tt]\w*`,
		`[Oo][Vv][Ee][Rr][Rr][Ii][Dd]\w*`,
	}, `|`) + `)\b`)

	// /^ifs?\b/i — same hand-expansion. `^` is start-of-TEXT in both engines
	// here (JS has no /m; Go has no (?m)).
	leadingIfRe = regexp.MustCompile(`^[Ii][Ff][Ss]?\b`)

	// /\s+/g for clampMatrixCells' whitespace collapse.
	jsSpaceRunRe = regexp.MustCompile(jsSpace + `+`)
)

// ---------------------------------------------------------------------------
// ClampMatrixCells — spec-clauses.ts:61

// ClampMatrixCells clamps a raw model cell list to the closure budget: collapse
// whitespace, drop empties, clip each cell to MatrixCellMaxlen, cap the list at
// MatrixMaxCells. Pure and deterministic.
//
// `cells: readonly unknown[]` becomes []any: every non-string element is
// skipped by the TS `typeof raw !== "string"` guard, so a JSON-decoded
// []any (float64 / bool / nil / []any / map) behaves identically.
//
// KNOWN RESIDUAL DIVERGENCE (not reachable from countSpecClauses, not
// fixtured): `s.slice(0, 159)` is a UTF-16 slice, so when unit 159 is the low
// half of a surrogate pair V8 emits a LONE surrogate, which JSON.stringify
// renders as "\ud83d". Go strings cannot hold an unpaired surrogate; the clip
// below decodes it to U+FFFD instead. Every other input, including astral
// characters that do not straddle the clip point, is byte-identical.
func ClampMatrixCells(cells []any) []string {
	out := []string{}
	for _, raw := range cells {
		str, ok := raw.(string)
		if !ok {
			continue
		}
		// `raw.replace(/\s+/g, " ").trim()` — collapse THEN trim, in that order
		// (the two commute here, but the port keeps the TS order).
		s := utf16of(jscompat.Trim(jsSpaceRunRe.ReplaceAllString(str, " ")))
		if len(s) < 3 {
			continue
		}
		if len(s) > MatrixCellMaxlen {
			out = append(out, decodeUTF16(s[:MatrixCellMaxlen-1])+"…")
		} else {
			out = append(out, decodeUTF16(s))
		}
		if len(out) >= MatrixMaxCells {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// CountSpecClauses — spec-clauses.ts:126

// CountSpecClauses is the deterministic FALLBACK estimate (bullets + backticked
// tokens at 3:1 + behavior-keyword sentences at 2:1). This is resilience for
// when the model path is unavailable — it is never the primary decision-maker.
func CountSpecClauses(spec string) float64 {
	if spec == "" {
		return 0
	}
	bullets := 0.0
	// JS `new Set<string>()`; only `.size` is read, and the key is the exact
	// UTF-16 unit sequence (see the package doc).
	tokens := map[string]struct{}{}
	for _, raw := range strings.Split(spec, "\n") {
		line := jscompat.Trim(raw)
		if bulletRe.MatchString(line) {
			bullets++
		}
		for _, tok := range matchAllBacktickTokens(utf16of(line)) {
			tokens[tok] = struct{}{}
		}
	}
	behaviorSentences := 0.0
	for _, sentence := range splitSentences(utf16of(spec)) {
		s := trimUTF16(sentence)
		if len(s) < 15 {
			continue
		}
		str := decodeUTF16(s)
		if fallbackBehaviorRe.MatchString(str) || leadingIfRe.MatchString(str) {
			behaviorSentences++
		}
	}
	return bullets + math.Floor(float64(len(tokens))/3) + math.Floor(behaviorSentences/2)
}

// ---------------------------------------------------------------------------
// hand-rolled scanners

// matchAllBacktickTokens reproduces
//
//	for (const match of line.matchAll(/`([^`\n]{2,40})`/g)) … match[1]
//
// over UTF-16 code units, returning each capture group encoded losslessly as
// two bytes per unit (the value is only ever used as a Set key).
//
// The greedy `{2,40}` needs no real backtracking: `[^`\n]` already excludes the
// closing delimiter, so the greedy run stops at the first backtick, newline or
// end-of-input. A match exists iff that run has length 2..40 AND the unit right
// after it is a backtick — a run longer than 40 can never be followed by a
// backtick at any of the 40..2 backtrack points, since every unit inside it is
// non-backtick by construction.
func matchAllBacktickTokens(u []uint16) []string {
	var out []string
	i := 0
	for i < len(u) {
		if u[i] != '`' {
			i++
			continue
		}
		j := i + 1
		for j < len(u) && u[j] != '`' && u[j] != '\n' {
			j++
		}
		runLen := j - i - 1
		if runLen >= 2 && runLen <= 40 && j < len(u) && u[j] == '`' {
			out = append(out, unitKey(u[i+1:j]))
			i = j + 1 // lastIndex := end of the whole match
			continue
		}
		i++
	}
	return out
}

// splitSentences reproduces spec.split(/(?<=[.!?])\s+|\n+/) — ECMA-262
// String.prototype.split with a non-global RegExp separator. See the package
// doc for why this is hand-rolled.
func splitSentences(u []uint16) [][]uint16 {
	size := len(u)
	// Step 13: the empty string returns [""] because this separator can never
	// match the empty string. (countSpecClauses never reaches this — `if
	// (!spec) return 0` fires first — but the scanner is faithful anyway.)
	if size == 0 {
		return [][]uint16{u}
	}
	var out [][]uint16
	p, q := 0, 0
	for q < size {
		e, ok := splitMatch(u, q)
		if !ok {
			q++
			continue
		}
		if e == p {
			// Unreachable for this separator (a match is always >= 1 unit long,
			// so e > q >= p); kept because ECMA-262 step 14.c.ii has it.
			q++
			continue
		}
		out = append(out, u[p:q])
		p = e
		q = p
	}
	out = append(out, u[p:size])
	return out
}

// splitMatch is SplitMatch(S, q, R) for /(?<=[.!?])\s+|\n+/: ANCHORED at q,
// alternative 1 attempted first, both alternatives greedy.
func splitMatch(u []uint16, q int) (int, bool) {
	// (?<=[.!?])\s+
	if q > 0 {
		switch u[q-1] {
		case '.', '!', '?':
			if q < len(u) && isJSSpaceUnit(u[q]) {
				e := q
				for e < len(u) && isJSSpaceUnit(u[e]) {
					e++
				}
				return e, true
			}
		}
	}
	// \n+
	if q < len(u) && u[q] == '\n' {
		e := q
		for e < len(u) && u[e] == '\n' {
			e++
		}
		return e, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// UTF-16 helpers

func utf16of(s string) []uint16 { return utf16.Encode([]rune(s)) }

func decodeUTF16(u []uint16) string { return string(utf16.Decode(u)) }

// unitKey encodes UTF-16 units losslessly (big-endian pairs) so unpaired
// surrogates stay distinguishable as Set keys.
func unitKey(u []uint16) string {
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c>>8), byte(c))
	}
	return string(b)
}

// trimUTF16 is String.prototype.trim over code units. Identical in effect to
// jscompat.Trim (no JS whitespace character is a surrogate), but it keeps the
// result in units so `.length` stays a UTF-16 count.
func trimUTF16(u []uint16) []uint16 {
	i, j := 0, len(u)
	for i < j && isJSSpaceUnit(u[i]) {
		i++
	}
	for j > i && isJSSpaceUnit(u[j-1]) {
		j--
	}
	return u[i:j]
}

// isJSSpaceUnit is the JS `\s` class (WhiteSpace ∪ LineTerminator), including
// U+FEFF, which Go's unicode.IsSpace omits.
func isJSSpaceUnit(c uint16) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return c >= 0x2000 && c <= 0x200a
}
