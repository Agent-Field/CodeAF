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
//   - THE WHOLE MATRIX IS KEPT, NOT OVERWRITTEN. Row i's cells live in the
//     buffer's i-th slice rather than shearing onto the one before them,
//     because the pass is worth more than its last row: [TermHit] reads the
//     alignment back off the cells that produced the score, walking the
//     decision bits the pass records as it goes. fzf rebuilds positions with
//     a second, reversed pass and can return a span the forward pass never
//     scored; this port backtracks the one DP that ran.
//   - MATRIX WIDTH n−m+1. The p-th query byte needs p−1 bytes before it and
//     m−p after it, so m+1 haystack cells can never match; the rows are sheared
//     into the same window either way, and width is the one dimension the
//     matrix needs.
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
// THE SLAB. One matcher — the matrix, the bonus line and the backtrack's
// scratch — is pooled and reused across calls, so a keystroke that re-ranks
// a thousand rows touches no allocator. DIRECTION: a higher score is a
// better match, the one convention for the whole repo.
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

// TermHit is where one term of a query landed: the field it won, and the
// ascending byte indices of the alignment that won it — the OPTIMAL span,
// read back off the pass that scored it, not a second guess at one
// (fzf's reversed pass can return a span its own forward pass never scored;
// "foo" against "xf foo" hits the f at 3, 4 and 5 — the whole word — and
// not fzf's f at 1).
//
// Pos IS EMPTY WHEN THE TERM MATCHED WITHOUT A SPAN WORTH VOUCHING FOR, and
// the match and its score stand unchanged — the caller simply has nothing
// to draw. Three ways that happens: a term past the needle guard, which
// the prefilter answers at score zero; a field that had to be case-folded
// as a whole (non-ASCII, case-insensitive), whose folded bytes are not the
// field's; and a lineage whose prefix was floored by its own gap costs — a
// gap longer than the whole prefix it swallowed — where the score the DP
// kept belongs to the suffix alone and no full alignment stands behind it.
type TermHit struct {
	// Field is the index of the field the term won — best-scoring, and the
	// earliest on ties, exactly as [ScoreFields] ranks them.
	Field int
	// Pos is the matched byte indices in that field, ascending and with no
	// duplicates. It points into the span slice the call filled — a caller
	// buffer, rewritten by the next call, so copy what must survive one.
	Pos []int
}

// ScoreHits is [Score] that also answers where each term landed, as byte
// indices into the haystack: one field, so every [TermHit]'s Field is zero.
// The score is [Score]'s answer unchanged; hits and span are the caller's
// own buffers, reused across calls so a list re-ranked per keystroke
// allocates nothing per row — hits is rewritten from its start and span
// holds each call's positions back to back, with every TermHit.Pos a
// subslice of it.
func ScoreHits(haystack string, terms []Term, hits *[]TermHit, span *[]int) (int, bool) {
	if len(terms) == 0 {
		*hits = (*hits)[:0]
		return 0, true
	}
	m := takeSlab()
	defer dropSlab(m)
	*hits = (*hits)[:0]
	*span = (*span)[:0]
	total := 0
	for i := range terms {
		score, ok, pos := m.matchTermPos(haystack, &terms[i])
		if !ok {
			*hits = (*hits)[:0]
			return 0, false
		}
		at := len(*span)
		*span = append(*span, pos...)
		*hits = append(*hits, TermHit{Field: 0, Pos: (*span)[at : at+len(pos)]})
		total += int(score)
	}
	return total, true
}

// ScoreFieldsHits is [ScoreFields] that also answers where each term landed:
// per term, the field it won and the matched indices in that field — so a
// list that draws one of the fields can highlight a term exactly where it
// matched, and only there. The score is [ScoreFields]' answer unchanged,
// field for field and tie for tie. The buffers are the caller's, as
// [ScoreHits] holds them.
func ScoreFieldsHits(fields []string, terms []Term, hits *[]TermHit, span *[]int) (int, bool) {
	if len(terms) == 0 {
		*hits = (*hits)[:0]
		return 0, true
	}
	m := takeSlab()
	defer dropSlab(m)
	*hits = (*hits)[:0]
	*span = (*span)[:0]
	total := 0
	for i := range terms {
		best, any, won := uint16(0), false, 0
		// THE WINNING FIELD'S SPAN IS READ WHILE ITS PASS IS STILL THE LAST
		// ONE RUN: the matrix holds one field at a time, so a later field
		// that beats it rewrites the span — and a field that only ties it
		// does not, because the earliest field wins ties, exactly as the
		// score ranks them.
		at := len(*span)
		for f, field := range fields {
			score, ok, pos := m.matchTermPos(field, &terms[i])
			if !ok || (any && score <= best) {
				continue
			}
			best, any, won = score, true, f
			*span = (*span)[:at]
			*span = append(*span, pos...)
		}
		if !any {
			*hits = (*hits)[:0]
			return 0, false
		}
		*hits = append(*hits, TermHit{Field: won, Pos: (*span)[at:len(*span)]})
		total += int(best)
	}
	return total, true
}

// ── the scoring constants ────────────────────────────────────────────────────

// fzf's calibration, with nucleo's one retune (bonusCamel123): the first
// byte a gap skips costs start, and each byte after it in the same gap costs
// extension, so a one-character gap costs 3 and a two-character gap costs 4
// — the exact price of the consecutive floor, which is the balance nucleo
// tuned for.
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
// the query's first i+1 bytes ending exactly at this column, the consecutive
// bonus that alignment carries into the next column, and the decision bits
// the backtrack needs — how this cell was reached, and how the gap value
// into this column was built. A score of zero is the no-match sentinel —
// every real cell holds at least [scoreMatch], so the sentinel cannot occur
// naturally and needs no flag.
type cell struct {
	score  uint16
	consec uint8
	via    uint8
}

// The decision bits a cell carries, set while the pass runs so the backtrack
// walks the alignment the score actually came from. viaDiag says the match
// at this column continued the diagonal — the previous query byte matched
// the byte before it — and its absence is the arrival out of a gap. viaGapExt
// says the gap value into this column extended the gap into the column before
// it rather than opening fresh off the diagonal two columns back; sentinel
// cells carry it too, because the gap chain runs whether or not this column
// matched.
const (
	viaDiag   uint8 = 1 << 0
	viaGapExt uint8 = 1 << 1
)

// matcher is the slab: the matrix, the bonus line and the backtrack's
// scratch, reused across every call so the hot path allocates nothing. It
// is pooled, not global, because two surfaces may rank on different
// goroutines.
type matcher struct {
	mat   []cell
	bonus []uint8
	pos   []int
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
	score, ok, _ := m.matchTermPos(field, term)
	return score, ok
}

// matchTermPos is [matchTerm] with the span the score came from: the
// ascending byte indices of the winning alignment, in the matcher's own
// scratch — overwritten by the next call, so a caller copies what must
// survive into its own span. Empty means the match stands and the span does
// not, for any of the reasons [TermHit] names: the guard, a whole-field
// fold, or a prefix the gap costs floored.
func (m *matcher) matchTermPos(field string, term *Term) (uint16, bool, []int) {
	m.pos = m.pos[:0]
	needle := term.needle
	if needle == "" {
		return 0, true, m.pos
	}
	if len(needle) > len(field) {
		return 0, false, m.pos
	}
	hay, fold := field, !term.sensitive
	folded := false
	if fold && !isASCII(field) {
		// A field holding non-ASCII bytes is folded once, here, and matched
		// from the fold: the classes of its ASCII bytes are then read from
		// the folded text and its camel turns are gone — the documented price
		// of the byte-at-a-time convention, paid only by fields that need it.
		hay = strings.ToLower(field)
		fold = false
		folded = true
	}
	start, end, ok := m.walk(hay, needle, fold)
	if !ok {
		return 0, false, m.pos
	}
	if len(needle) > maxNeedle {
		// The subsequence stands; a term this long has no meaningful score,
		// and no span stands behind a score of zero.
		return 0, true, m.pos
	}
	score, at, width, ok := m.align(hay, needle, fold, start, end)
	if !ok {
		return 0, false, m.pos
	}
	if folded {
		// The fold changed the bytes the field is drawn with: the span is
		// where the term landed in the FOLD's shape, and what a caller
		// highlights is the original's — not the same string, so none leaves.
		return score, true, m.pos
	}
	pos, span := m.positions(len(needle), width, at)
	if !span {
		return score, true, m.pos[:0]
	}
	// The backtrack speaks in window columns; a caller speaks in the field's
	// own bytes. The window starts one byte before the first candidate, so
	// every position shifts by it — and stays a byte index into the field.
	for i := range pos {
		pos[i] += start
	}
	return score, true, pos
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
// and returns the best cell of the last row — the highest-scoring alignment
// of the whole needle, its window column, and the width the rows share — or
// zero when there is none. [Score] and [ScoreFields] read only the score;
// [matchTermPos] hands the cell and the width to [matcher.positions], which
// walks the alignment back off the matrix this pass filled.
//
// EVERY ROW KEEPS ITS OWN SLICE of the one buffer, row-major: the backtrack
// needs row i−1's cells after row i has written its own, so the old
// sheared-onto-one-row storage is gone. The gap matrix still never
// materializes — its two carried values, the skip score into this column
// and the diagonal one column behind it, run as scalars along the row, exactly
// as nucleo carries them — and the pass records which arm each decision
// took, in the cells' [via] bits, so the backtrack follows the alignment
// that produced the score and no other.
func (m *matcher) align(hay, needle string, fold bool, start, end int) (best uint16, at, width int, ok bool) {
	n := end - start
	width = n - len(needle) + 1
	if width < 1 {
		// The walk found the subsequence, so this cannot happen; kept honest
		// rather than trusted.
		return 0, 0, 0, false
	}
	mat := grow(m.mat, len(needle)*width)
	m.mat = mat
	bonus := grow(m.bonus, n)
	m.bonus = bonus
	first := mat[:width]

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
				first[k] = cell{score: scoreMatch + b*bonusFirstCharMultiplier, consec: uint8(b)}
			} else {
				first[k] = cell{}
			}
		}
	}

	// The remaining rows. At each column the alignment can arrive
	// two ways: consecutively, off the diagonal cell one column up and one
	// left (whose running bonus carries), or out of a gap, whose value runs
	// along the row as the better of opening a gap from the diagonal two
	// columns back and extending the gap that was already open. A match at
	// the column takes the better of the two arrivals; no match leaves the
	// sentinel behind for the row above to read — carrying the gap-chain
	// bit, which the backtrack reads on any cell, matched or not.
	for i := 1; i < len(needle); i++ {
		above, row := mat[(i-1)*width:i*width], mat[i*width:(i+1)*width]
		var skipIn, prevSkip, diagBehind uint16
		for idx := 0; idx < width; idx++ {
			col := i + idx
			diag := above[idx]
			skipIn = satSub(diagBehind, penaltyGapStart)
			via := uint8(0) // a gap opening fresh off the diagonal two back
			if extended := satSub(prevSkip, penaltyGapExtension); extended > skipIn {
				skipIn, via = extended, viaGapExt
			}
			if eqByte(hay[start+col], needle[i], fold) {
				row[idx] = advance(skipIn, uint16(bonus[col]), diag)
				row[idx].via |= via
			} else {
				row[idx] = cell{via: via}
			}
			prevSkip, diagBehind = skipIn, diag.score
		}
	}

	// The best cell of the last row, leftmost on ties — the same cell the
	// score comes from and the one the backtrack starts at.
	last := mat[(len(needle)-1)*width:]
	for idx := 0; idx < width; idx++ {
		if last[idx].score > best {
			best, at = last[idx].score, idx
		}
	}
	return best, at, width, best > 0
}

// advance is one match cell from its three inputs, nucleo's next_m_cell: the
// skip value carried into this column, this byte's boundary bonus, and the
// diagonal cell. From the diagonal the match is consecutive — the running
// bonus carries, floored at [bonusConsecutive] and raised when this byte
// opens a fresh boundary worth more — and from the gap it starts clean at
// this byte's own bonus; whichever scores higher is the cell. A zero
// diagonal is the no-match sentinel, so the gap path is all there is. The
// winner is said in the [via] bit, for the backtrack.
func advance(skipIn, bonus uint16, diag cell) cell {
	if diag.score == 0 {
		return cell{score: skipIn + bonus + scoreMatch, consec: uint8(bonus), via: 0}
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
		return cell{score: fromDiag + scoreMatch, consec: uint8(carry), via: viaDiag}
	}
	return cell{score: fromSkip + scoreMatch, consec: uint8(bonus), via: 0}
}

// positions reads the winning alignment back off the matrix [align] just
// filled: from the best-scoring cell of the last row, one recorded decision
// at a time, to the row-0 start — and returns the matched window columns as
// byte indices into the field, ascending. It is THE OPTIMAL SPAN, the
// alignment whose score the pass reported, walked off the pass that scored
// it and never re-derived or guessed: fzf rebuilds positions with a second,
// reversed pass that can land on a span its own forward pass never scored.
//
// ok is false when the walk meets a gap whose source is the sentinel — a
// lineage whose prefix was floored by its own gap costs, where the score
// stands behind the suffix alone and no alignment of the whole needle does.
// A half-span highlighted is a lie, so no span is returned and the match,
// and its score, stand unhighlighted ([TermHit] says the same at the API).
func (m *matcher) positions(needleLen, width, bestAt int) ([]int, bool) {
	last := needleLen - 1
	pos := m.pos[:0]
	col := last + bestAt
	for i := last; i > 0; i-- {
		c := m.mat[i*width+col-i]
		if c.via&viaDiag != 0 {
			// Consecutive: the previous query byte matched the byte before
			// this one.
			pos = append(pos, col)
			col--
			continue
		}
		// The match at col arrived out of a gap. Walk the gap chain — each
		// column it extends through says so in its bit — back to the column
		// where the gap opened, which is two past the query byte before it.
		j := col
		for j > i && m.mat[i*width+j-i].via&viaGapExt != 0 {
			j--
		}
		pred := j - 2
		if pred < i-1 || pred-(i-1) >= width || m.mat[(i-1)*width+pred-(i-1)].score == 0 {
			// The gap's source is the sentinel: this lineage's prefix was
			// floored, and the cell's score belongs to no full alignment.
			return m.pos[:0], false
		}
		pos = append(pos, col)
		col = pred
	}
	pos = append(pos, col)
	// The walk collected the span backwards; a caller reads it forwards.
	for l, r := 0, len(pos)-1; l < r; l, r = l+1, r-1 {
		pos[l], pos[r] = pos[r], pos[l]
	}
	m.pos = pos
	return pos, true
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
