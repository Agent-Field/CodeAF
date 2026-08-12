package palette

import "unicode/utf8"

// The fzf-style as-you-type filter (5.18's idiom, 5.21's "fzf-style inline
// filter", 5.22 rule 2's fuzzy catalog).
//
// Two things run on this path and they have different budgets. RANKING runs
// over every row on every keystroke and must not allocate: it answers "does
// this row match, and how well". HIGHLIGHTING runs only over the rows actually
// on screen — at most a screenful — and may write positions into a buffer the
// caller reuses. Splitting them is what lets a catalog of a few hundred rows
// re-filter on every keypress without touching the heap.
//
// The scoring rule is a deliberate restatement of [registry.FuzzyMatch]'s, not
// a second opinion: an in-order subsequence match scored by how early each
// letter lands, with a flat bonus for a leading match. filter_test.go pins the
// agreement against the registry's own catalog, so the palette and the slash
// filter can never start disagreeing about which row is the best "can".
//
// It is not simply CALLED, because the registry scores registry entries and
// this list also carries rooms and settings, and because ranking there returns
// a freshly allocated []Match per query — the one thing a per-keystroke path
// must not do.

// prefixBonus is subtracted from the score of a haystack that the needle
// prefixes outright, so typing "can" ranks "cancel" ahead of a row that merely
// contains c, a and n somewhere. Same magnitude as the registry's.
const prefixBonus = 100

// score is the subsequence scorer. Lower is better, matching the convention
// every picker in this tree already uses. needle must already be lowercased by
// [lower]; haystack must be one of a row's precomputed lowercase fields.
//
// Bytes, not runes, on purpose. The needle is compared byte for byte, and a
// needle byte below 0x80 can only ever equal a haystack byte below 0x80 —
// UTF-8 lead and continuation bytes are all >= 0xC0 and >= 0x80 respectively —
// so an ASCII query can never match half a multi-byte rune. A non-ASCII query
// degrades to an exact byte-sequence match rather than a wrong one, and
// [appendPositions] refuses to split a rune regardless.
func score(haystack, needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	pos, total := 0, 0
	for i := 0; i < len(needle); i++ {
		want := needle[i]
		found := false
		for pos < len(haystack) {
			if haystack[pos] == want {
				total += pos
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			return 0, false
		}
	}
	if len(needle) <= len(haystack) && haystack[:len(needle)] == needle {
		total -= prefixBonus
	}
	return total, true
}

// appendPositions appends the byte offsets in haystack that needle matched, in
// increasing order, and returns the extended slice. It walks greedily — the
// same walk [score] takes — so the highlighted cells are exactly the cells the
// score was computed from, and a user watching letters light up is watching
// the ranking happen.
//
// A position that is not a rune start is dropped rather than highlighted. That
// can only happen for a non-ASCII needle, and the alternative is painting an
// escape sequence into the middle of a rune, which is a mojibake cell rather
// than a highlight. Dropping loses a highlight; splitting loses the character.
func appendPositions(dst []int32, haystack, needle string) []int32 {
	if needle == "" {
		return dst
	}
	pos := 0
	for i := 0; i < len(needle); i++ {
		want := needle[i]
		found := false
		for pos < len(haystack) {
			if haystack[pos] == want {
				if utf8.RuneStart(haystack[pos]) {
					dst = append(dst, int32(pos))
				}
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			return dst
		}
	}
	return dst
}

// lower is ASCII case folding that returns its argument unallocated when there
// is nothing to fold — the overwhelmingly common case for a query already
// typed in lower case and for every verb the registry seeds. Non-ASCII bytes
// pass through untouched, which degrades an accented query to a case-sensitive
// match instead of mangling it.
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

// hit is one surviving row: its index into the built row slice and the score
// that ranked it. Both are 32-bit so the whole filtered set stays compact —
// this slice is walked twice per frame and is the only per-keystroke state.
type hit struct {
	idx   int32
	score int32
}

// filter re-ranks every row against query and appends the survivors to dst in
// DISPLAY order: sections keep their declared order, and within a section the
// best match comes first, stable on ties.
//
// An empty query matches everything at score 0, which — with a stable sort —
// means the unfiltered list is exactly the wiring's own order. That is the
// palette's convention and it matters: the row under the cursor when the door
// opens must be a row someone decided on — the first action of the leading
// section, in the order the registry seeded it — and never whatever an
// arbitrary tiebreak preferred.
func filter(dst []hit, rows []row, needle string) []hit {
	dst = dst[:0]
	for i := range rows {
		r := &rows[i]
		verbScore, verbOK := score(r.lowerVerb, needle)
		descScore, descOK := score(r.lowerDesc, needle)
		switch {
		case verbOK && descOK:
			dst = append(dst, hit{idx: int32(i), score: int32(min(verbScore, descScore))})
		case verbOK:
			dst = append(dst, hit{idx: int32(i), score: int32(verbScore)})
		case descOK:
			dst = append(dst, hit{idx: int32(i), score: int32(descScore)})
		}
	}
	// Rows are built section-grouped, so the survivors already are; only the
	// order WITHIN each run has to move. Insertion sort, written out rather
	// than sort.SliceStable, because the comparison closure that call needs is
	// one heap allocation per keystroke for a run this short — the longest
	// section here is the registry, and the registry is dozens of rows.
	lo := 0
	for lo < len(dst) {
		hi := lo + 1
		for hi < len(dst) && rows[dst[hi].idx].sec == rows[dst[lo].idx].sec {
			hi++
		}
		sortRun(dst[lo:hi])
		lo = hi
	}
	return dst
}

// sortRun is a stable insertion sort by score, ascending.
func sortRun(run []hit) {
	for i := 1; i < len(run); i++ {
		h := run[i]
		j := i - 1
		for j >= 0 && run[j].score > h.score {
			run[j+1] = run[j]
			j--
		}
		run[j+1] = h
	}
}
