// Package fuzzy is the one quick-search scorer for every picker on the chat
// surface: fzf's FuzzyMatchV2 — a modified Smith-Waterman local alignment —
// with the corrections helix's nucleo documents for the same algorithm.
//
// THE ALIGNMENT, NOT THE FIRST OCCURRENCE. The query must appear in the item
// as a subsequence: no substitutions, no skipped query characters, and text
// characters may be skipped between two query characters at a gap price. The
// dynamic program finds the highest-scoring alignment, not the first one that
// happens to fit — which is the whole difference between this and a greedy
// subsequence walk, and the reason a port that only finds A match is not this
// package.
//
// SOURCES AND DIVERGENENCES. The algorithm and its constants are ported from
// junegunn/fzf's src/algo/algo.go (MIT) as corrected by helix-editor/nucleo's
// matcher/src/fuzzy_optimal.rs (MPL-2.0). What this port takes from nucleo:
//
//   - TWO MATRICES. The match matrix carries each cell's score and the
//     running consecutive bonus along the diagonal; the gap matrix runs as
//     two scalars per row (the value carried into this column, and the
//     diagonal one column further back). fzf's single matrix conflates the
//     gap state with the bonus state and is provably not optimal under its
//     own scoring: query "foo" against "xf foo" picks the span "xf_oo" when
//     "x__foo" scores higher — the alignment repro test pins that case.
//   - MATRIX WIDTH n−m+1. The p-th query byte needs p−1 bytes before it and
//     m−p after it, so m+1 haystack cells can never match; every row shears
//     into the same n−m+1 window and the rows overwrite one buffer in place.
//   - THE CAMEL RETUNE. bonusCamel123 is 5, not fzf's 7: fzf's 7 lets a
//     camelCase hit beat a hyphenated word, and nucleo lowered it to balance
//     camel, snake and consecutive forms against each other.
//   - NO PENALTY FOR A LATE START, none for candidate length either. A match
//     beginning later in the item scores the same as one beginning earlier
//     with the same shape; length is a tie-break callers may apply, never a
//     score term here.
//
// What it keeps from fzf: the constants (scoreMatch 16, gap start 3,
// extension 1, boundary 8, white boundary 10, delimiter boundary 9,
// consecutive floor 4, first-character multiplier 2), the delimiter set
// "/,:;|", the whitespace set " \t\n\v\f\r", and the prefilter that walks the
// query in order and fails fast when the first byte never appears.
//
// CASE. Smart-case, per term: a word typed with no uppercase letter matches
// case-insensitively, any uppercase makes that word case-sensitive. Over
// pure-ASCII text the fold is done byte-wise on the spot so the character
// classes — and with them the camelCase bonuses — are still read from the
// original casing. Over text with non-ASCII bytes the field is case-folded
// once with strings.ToLower and matched byte-wise from the fold, and
// non-ASCII bytes carry no character class: no boundary, no camel — the
// byte-at-a-time convention this program's pickers already keep. That fold is
// the one place a matched item allocates on the hot path.
//
// ARITHMETIC. Scores are uint16 and penalties subtract saturating, which is
// Smith-Waterman's floor-at-zero had for free and keeps the hot path free of
// allocation and of overflow guards. A term longer than [maxNeedle] bytes is
// beyond what a typed search word can be; the prefilter still answers whether
// it matches, at score 0.
//
// THE SLAB. One matcher — the score row and the bonus line — is pooled and
// reused across calls, so a keystroke that re-ranks a thousand rows touches
// no allocator. DIRECTION: a higher score is a better match, the one
// convention for the whole repo.
package fuzzy

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// ── the query ────────────────────────────────────────────────────────────────

// Term is one whitespace-separated word of a query, prepared once per
// keystroke by [Terms] and then scored against every row: a row matches when
// every term matches it, and ranks by the sum of what each term scored.
type Term struct {
	// needle is the word's bytes as they must be found.
	needle string
	// sensitive is the smart-case reading: no uppercase in the word as typed
	// means case-insensitive, any uppercase makes the whole word
	// case-sensitive — fzf's convention, and per word rather than per query
	// so "ds V4" can be a loose word beside a pinned one.
	sensitive bool
}

// Terms splits a query into its terms. Whitespace separates; every term has
// to match ([Score]). An empty query is no terms, and no terms match
// everything at score 0 — an untyped filter is not a filter, just the list.
//
// The scan is rune-wise over the query itself and allocates the one slice it
// returns: terms are built once per keystroke, and the pickers hold
// allocation budgets per keystroke that count them.
func Terms(query string) []Term {
	var terms []Term
	at := 0
	for at < len(query) {
		for at < len(query) {
			r, size := utf8.DecodeRuneInString(query[at:])
			if !unicode.IsSpace(r) {
				break
			}
			at += size
		}
		if at >= len(query) {
			break
		}
		start := at
		for at < len(query) {
			r, size := utf8.DecodeRuneInString(query[at:])
			if unicode.IsSpace(r) {
				break
			}
			at += size
		}
		term := Term{needle: query[start:at]}
		for _, r := range query[start:at] {
			if unicode.IsUpper(r) {
				term.sensitive = true
				break
			}
		}
		if !term.sensitive {
			// A word with no uppercase is folded once here, at query time,
			// rather than once per row it is scored against — a no-op
			// allocation-wise for the already-lowercase word every real query
			// is.
			term.needle = strings.ToLower(query[start:at])
		}
		terms = append(terms, term)
	}
	return terms
}

// Score is one haystack against the terms: every term must match, and the
// result is the sum of what each scored. Higher is better; false means some
// term matched nothing and the row is out.
func Score(haystack string, terms []Term) (int, bool) {
	if len(terms) == 0 {
		return 0, true
	}
	m := takeSlab()
	defer dropSlab(m)
	total := 0
	for i := range terms {
		score, ok := m.matchTerm(haystack, &terms[i])
		if !ok {
			return 0, false
		}
		total += int(score)
	}
	return total, true
}

// ScoreFields is [Score] over a row that answers in several fields: per
// term, the best-scoring field wins, and the total is the sum over terms —
// so "yolo" finds a settings row by the value it carries even when the label
// says something else entirely. An empty field list matches nothing but the
// empty query.
func ScoreFields(fields []string, terms []Term) (int, bool) {
	if len(terms) == 0 {
		return 0, true
	}
	m := takeSlab()
	defer dropSlab(m)
	total := 0
	for i := range terms {
		best, any := uint16(0), false
		for _, field := range fields {
			if score, ok := m.matchTerm(field, &terms[i]); ok && (!any || score > best) {
				best, any = score, true
			}
		}
		if !any {
			return 0, false
		}
		total += int(best)
	}
	return total, true
}

// ── the scoring constants ────────────────────────────────────────────────────

// fzf's calibration, with nucleo's one retune (bonusCamel123) and its gap
// reading: the gap into a column costs start, and every gap column including
// the first costs extension on top, so a one-character gap costs 4 — exactly
// the consecutive floor, which is the balance nucleo tuned for.
const (
	scoreMatch               = 16
	penaltyGapStart          = 3
	penaltyGapExtension      = 1
	bonusBoundary            = 8
	bonusBoundaryWhite       = 10
	bonusBoundaryDelimiter   = 9
	bonusNonWord             = 8
	bonusConsecutive         = 4
	bonusCamel123            = 5
	bonusFirstCharMultiplier = 2

	// maxNeedle is fzf's guard against a term that could overflow the 16-bit
	// score or pay for a quadratic matrix nothing would ever type. Past it
	// the prefilter's subsequence answer stands, at score 0.
	maxNeedle = 1000
)

// The delimiter and whitespace sets, fzf's defaults: a boundary after any of
// these is worth more than one mid-word, the delimiter bonus between the two.
const (
	delimiterBytes = "/,:;|"
	whiteBytes     = " \t\n\v\f\r"
)

// ── character classes ────────────────────────────────────────────────────────

// charClass is the coarse class a byte carries into the bonus model. NonASCII
// bytes carry none: the fold has already passed through them and the model
// has no opinion about them, so they give no boundary and take none.
type charClass uint8

const (
	classNone charClass = iota
	classWhite
	classNonWord
	classDelimiter
	classLower
	classUpper
	classNumber
)

// asciiClass is the class of each ASCII byte, built once at init.
var asciiClass [utf8.RuneSelf]charClass

func init() {
	for i := range asciiClass {
		c := byte(i)
		switch {
		case c >= 'a' && c <= 'z':
			asciiClass[c] = classLower
		case c >= 'A' && c <= 'Z':
			asciiClass[c] = classUpper
		case c >= '0' && c <= '9':
			asciiClass[c] = classNumber
		case strings.IndexByte(whiteBytes, c) >= 0:
			asciiClass[c] = classWhite
		case strings.IndexByte(delimiterBytes, c) >= 0:
			asciiClass[c] = classDelimiter
		default:
			asciiClass[c] = classNonWord
		}
	}
}

// classOf is the class of one haystack byte. A non-ASCII byte is classNone
// whatever it folds to: the bonus model reads ASCII only.
func classOf(b byte) charClass {
	if b >= utf8.RuneSelf {
		return classNone
	}
	return asciiClass[b]
}

// bonusFor is what a byte at this class is worth for matching, given the
// class of the byte before it: a word beginning after whitespace, a
// delimiter or a non-word byte; a camelCase or letter-to-number turn; a
// non-word or whitespace byte standing on its own. classNone on either side
// of the transition yields nothing but what the current byte is in itself.
func bonusFor(prev, cur charClass) uint16 {
	if cur > classWhite {
		switch prev {
		case classWhite:
			return bonusBoundaryWhite
		case classDelimiter:
			return bonusBoundaryDelimiter
		case classNonWord:
			return bonusBoundary
		}
	}
	if prev == classLower && cur == classUpper ||
		prev != classNumber && prev != classNone && cur == classNumber {
		return bonusCamel123
	}
	switch cur {
	case classNonWord, classDelimiter:
		return bonusNonWord
	case classWhite:
		return bonusBoundaryWhite
	}
	return 0
}

// ── the matcher ──────────────────────────────────────────────────────────────

// cell is one cell of the match matrix: the score of the best alignment of
// the query's first i+1 bytes ending exactly at this column, and the
// consecutive bonus that alignment carries into the next column. A score of
// zero is the no-match sentinel — every real cell holds at least
// [scoreMatch], so the sentinel cannot occur naturally and needs no flag.
type cell struct {
	score  uint16
	consec uint8
}

// matcher is the slab: the score row and the bonus line, reused across every
// call so the hot path allocates nothing. It is pooled, not global, because
// two surfaces may rank on different goroutines.
type matcher struct {
	row   []cell
	bonus []uint8
}

var slab = sync.Pool{New: func() any { return new(matcher) }}

func takeSlab() *matcher  { return slab.Get().(*matcher) }
func dropSlab(m *matcher) { slab.Put(m) }

// grow returns a slice of at least need backed by the buffer, growing it
// geometrically so a larger row than any before costs one allocation and
// never a second.
func grow[T any](buf []T, need int) []T {
	if cap(buf) < need {
		size := 2 * cap(buf)
		if size < need {
			size = need
		}
		buf = make([]T, size)
	}
	return buf[:need]
}

// matchTerm scores one field against one term, allocation-free for ASCII
// text: the case fold of an ASCII field happens byte-wise inside the
// comparison, so the classes the bonus model reads stay the original ones.
func (m *matcher) matchTerm(field string, term *Term) (uint16, bool) {
	needle := term.needle
	if needle == "" {
		return 0, true
	}
	if len(needle) > len(field) {
		return 0, false
	}
	hay, fold := field, !term.sensitive
	if fold && !isASCII(field) {
		// A field holding non-ASCII bytes is folded once, here, and matched
		// from the fold: the classes of its ASCII bytes are then read from
		// the folded text and its camel turns are gone — the documented price
		// of the byte-at-a-time convention, paid only by fields that need it.
		hay = strings.ToLower(field)
		fold = false
	}
	start, end, ok := m.walk(hay, needle, fold)
	if !ok {
		return 0, false
	}
	if len(needle) > maxNeedle {
		// The subsequence stands; a term this long has no meaningful score.
		return 0, true
	}
	return m.align(hay, needle, fold, start, end)
}

// isASCII reports whether s holds no byte at or above the UTF-8 self mark.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// eqByte is one needle byte against one haystack byte under the term's case
// rule: exact for a case-sensitive term or an already-folded haystack, and for
// a case-insensitive term over ASCII, folded on the spot — the needle is
// lowercase by construction, so the fold is one comparison against its
// uppercase twin.
func eqByte(have, want byte, fold bool) bool {
	if have == want {
		return true
	}
	return fold && want >= 'a' && want <= 'z' && have == want-32
}

// walk is the prefilter and the window. The needle's bytes are looked for in
// order, and the first missing one ends the match in O(n) — the walk is the
// whole cost of rejecting a row, and no matrix is built for it. The window it
// returns is the slice of the haystack every alignment lives in: from one
// byte before the first occurrence of the first byte (that byte is only
// context for the boundary bonus) through the last occurrence of the last.
// Every alignment starts at an occurrence of the first byte at or after the
// first one and ends at an occurrence of the last byte at or before the last
// one, so nothing outside the window can be missed by cutting it.
func (m *matcher) walk(hay, needle string, fold bool) (start, end int, ok bool) {
	first, last := needle[0], needle[len(needle)-1]
	at := -1
	for i := 0; i < len(hay); i++ {
		if eqByte(hay[i], first, fold) {
			at = i
			break
		}
	}
	if at < 0 {
		return 0, 0, false
	}
	start = at
	if at > 0 {
		start = at - 1
	}
	pos := at + 1
	for k := 1; k < len(needle); k++ {
		found := -1
		for i := pos; i < len(hay); i++ {
			if eqByte(hay[i], needle[k], fold) {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, 0, false
		}
		pos = found + 1
	}
	end = pos
	for i := pos; i < len(hay); i++ {
		if eqByte(hay[i], last, fold) {
			end = i + 1
		}
	}
	return start, end, true
}

// align runs the two-matrix dynamic program over the window the walk chose
// and returns the best score in the last row — the highest-scoring alignment
// of the whole needle, or zero when there is none.
//
// THE ROWS SHARE ONE BUFFER. Row i's cell at window column j lives at index
// j−i, so every row occupies the same width n−m+1 cells and overwrites the
// buffer in place: the cell being written and the diagonal it builds on share
// an index, read before write. The gap matrix never materializes — its two
// carried values, the skip score into this column and the diagonal one
// column behind it, run as scalars along the row, exactly as nucleo carries
// them.
func (m *matcher) align(hay, needle string, fold bool, start, end int) (uint16, bool) {
	n := end - start
	width := n - len(needle) + 1
	if width < 1 {
		// The walk found the subsequence, so this cannot happen; kept honest
		// rather than trusted.
		return 0, false
	}
	row := grow(m.row, width)
	m.row = row
	bonus := grow(m.bonus, n)
	m.bonus = bonus

	// One pass builds the bonus line and the first row together: the first
	// byte's cells are its score wherever it matches, doubled bonus and all,
	// and zero elsewhere. The byte before the window sets the class the
	// window's first byte is read against; at the very start of the haystack
	// that class is whitespace, which is what makes position zero a boundary.
	prevClass := classWhite
	if start > 0 {
		prevClass = classOf(hay[start-1])
	}
	for k := 0; k < n; k++ {
		class := classOf(hay[start+k])
		b := bonusFor(prevClass, class)
		bonus[k] = uint8(b)
		prevClass = class
		if k < width {
			if eqByte(hay[start+k], needle[0], fold) {
				row[k] = cell{score: scoreMatch + b*bonusFirstCharMultiplier, consec: uint8(b)}
			} else {
				row[k] = cell{}
			}
		}
	}

	// The remaining rows, in place. At each column the alignment can arrive
	// two ways: consecutively, off the diagonal cell one column up and one
	// left (whose running bonus carries), or out of a gap, whose value runs
	// along the row as the better of opening a gap from the diagonal two
	// columns back and extending the gap that was already open. A match at
	// the column takes the better of the two arrivals; no match leaves the
	// sentinel behind for the row above to read.
	for i := 1; i < len(needle); i++ {
		var skipIn, prevSkip, diagBehind uint16
		for idx := 0; idx < width; idx++ {
			col := i + idx
			diag := row[idx]
			skipIn = satSub(diagBehind, penaltyGapStart)
			if extended := satSub(prevSkip, penaltyGapExtension); extended > skipIn {
				skipIn = extended
			}
			if eqByte(hay[start+col], needle[i], fold) {
				row[idx] = advance(skipIn, uint16(bonus[col]), diag)
			} else {
				row[idx] = cell{}
			}
			prevSkip, diagBehind = skipIn, diag.score
		}
	}

	best := uint16(0)
	for idx := 0; idx < width; idx++ {
		if row[idx].score > best {
			best = row[idx].score
		}
	}
	return best, best > 0
}

// advance is one match cell from its three inputs, nucleo's next_m_cell: the
// skip value carried into this column, this byte's boundary bonus, and the
// diagonal cell. From the diagonal the match is consecutive — the running
// bonus carries, floored at [bonusConsecutive] and raised when this byte
// opens a fresh boundary worth more — and from the gap it starts clean at
// this byte's own bonus; whichever scores higher is the cell. A zero
// diagonal is the no-match sentinel, so the gap path is all there is.
func advance(skipIn, bonus uint16, diag cell) cell {
	if diag.score == 0 {
		return cell{score: skipIn + bonus + scoreMatch, consec: uint8(bonus)}
	}
	carry := uint16(diag.consec)
	if carry < bonusConsecutive {
		carry = bonusConsecutive
	}
	if bonus >= bonusBoundary && bonus > carry {
		carry = bonus
	}
	fromDiag := diag.score + maxU16(carry, bonus)
	fromSkip := skipIn + bonus
	if fromDiag > fromSkip {
		return cell{score: fromDiag + scoreMatch, consec: uint8(carry)}
	}
	return cell{score: fromSkip + scoreMatch, consec: uint8(bonus)}
}

// satSub is subtraction floored at zero — Smith-Waterman's local-alignment
// floor, which saturating unsigned arithmetic gives without a branch per
// penalty.
func satSub(a, b uint16) uint16 {
	if a < b {
		return 0
	}
	return a - b
}

func maxU16(a, b uint16) uint16 {
	if a > b {
		return a
	}
	return b
}
