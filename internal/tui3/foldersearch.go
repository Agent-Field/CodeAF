package tui3

// FINDING A FOLDER BY TYPING AT IT.
//
// folderpick.go is the component and folderplace.go is the app around it; this
// file is the one question they both hand off — GIVEN WHAT SOMEBODY HAS TYPED,
// WHICH OF THESE DIRECTORIES DID THEY MEAN. It is kept apart from both because
// it is the only part of the picker that is pure arithmetic over strings, and
// because the answer wants to change on evidence about how people type without
// anybody reopening a file that draws.
//
// It is NOT [pathScore]. The `@` completion ranks files under one directory,
// where every candidate shares a prefix and the shorter path is nearly always
// the better guess. This ranks a few hundred directories scattered over a
// disk, where the leaf name is what a person is thinking of, the segments above
// it are how they narrow it, and the thing they typed may simply be misspelled.
// The two lists want opposite tiebreaks, so they get two scorers, and the `@`
// one is left exactly as it stands.
//
// ── THE LADDER ──────────────────────────────────────────────────────────────
//
// A match falls into ONE TIER and the tiers are compared before anything else.
// Inside a tier the layer decides, then how often the folder has actually been
// picked, and only then how tight the match was. That order is deliberate: the
// tiers are coarse enough that two candidates in the same one are equally good
// answers to what was typed, and between two equally good answers the folder
// somebody uses every day is the one they meant. A scorer that let a two-cell
// difference in match offset outrank a week of use would reorder the top of the
// list every time a person added a letter.
//
//	aforge      → the leaf name, spelled out          folderTierName
//	afo         → the leaf name starts with it        folderTierNameLead
//	internal    → a segment above the leaf            folderTierSegment
//	~/code      → the path itself starts with it      folderTierPathLead
//	forge       → inside the leaf name                folderTierNameIn
//	ait         → the initials, in order              folderTierShort
//	de/af       → somewhere else in the path          folderTierPathIn
//	afv         → the letters, in order               folderTierLoose
//	afroge      → one slip away from a name           folderTierSlip
//
// THE LAST RUNG IS THE ONE THAT PAYS FOR ITSELF. `afroge` is the typo people
// actually make on this program's name, and every tier above it answers it with
// nothing at all — a list that says "no folder matches" to a query that is one
// swapped pair of letters from the folder in front of them. It is last because
// it is a guess, it is bounded by [folderSlipBound] so that a two-letter query
// cannot be one slip from everything, and it is only ever reached when nothing
// else matched.

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// folderTier is HOW a query matched, and the order of the constants is the
// order a person means them in — best first.
type folderTier uint8

const (
	// folderTierName is the leaf name, spelled out whole.
	folderTierName folderTier = iota
	// folderTierNameLead is the leaf name started.
	folderTierNameLead
	// folderTierSegment is a segment above the leaf, or a run of them spelled
	// from a boundary — `internal`, `internal/tui3`.
	folderTierSegment
	// folderTierPathLead is the shown path's own beginning — `~/code`.
	folderTierPathLead
	// folderTierNameIn is inside the leaf name but not at its front.
	folderTierNameIn
	// folderTierShort is the initials: every letter on a word start, or
	// carrying straight on from the letter before it ([folderShort]).
	folderTierShort
	// folderTierPathIn is anywhere else in the path.
	folderTierPathIn
	// folderTierLoose is the letters in order with anything between them.
	folderTierLoose
	// folderTierSlip is one or two slips away from a segment — the typo rung.
	folderTierSlip
)

// folderHit is one candidate's answer to one query.
type folderHit struct {
	tier folderTier
	// detail orders a tier internally: how far the match sat from the leaf,
	// where in the text it landed, and how much text it left over. Lower is
	// nearer to what was typed. It is a tiebreak and NOT a score — the ladder
	// above decides first, and the layer and the frecency decide before this.
	detail int
}

// better reports whether this hit answers the query more nearly than the other.
func (h folderHit) better(other folderHit) bool {
	if h.tier != other.tier {
		return h.tier < other.tier
	}
	return h.detail < other.detail
}

// ── the query ───────────────────────────────────────────────────────────────

// folderQuery is what was typed, prepared once for a whole pass over the
// candidates. Folding and measuring it per candidate would be the same work a
// few hundred times per keystroke.
type folderQuery struct {
	// text is trimmed and lowercased, and is what every tier is asked about.
	text string
	// slip is how many slips a segment may be away and still answer this query
	// — zero for a query too short to guess at (see [folderSlipBound]).
	slip int
}

// newFolderQuery prepares one query.
func newFolderQuery(raw string) folderQuery {
	text := strings.ToLower(strings.TrimSpace(raw))
	return folderQuery{text: text, slip: folderSlipBound(text)}
}

// blank reports whether nothing has been typed. AN EMPTY BOX MATCHES EVERYTHING
// AND RANKS NOTHING, which is this list's whole opening frame: every candidate
// ties on the ladder and the layers and the frecency alone decide, exactly as
// they did before this file existed.
func (q folderQuery) blank() bool { return q.text == "" }

// The two lengths that decide how far a guess may reach. Below four characters
// a single slip reaches so many names that the rung stops meaning anything —
// `abc` is one slip from `abd`, `abe`, `ab`, `abcd` and every other three
// letter name on the disk. From eight characters up, two slips is still a
// tighter claim than one slip on a short word.
const (
	folderSlipFloor = 4
	folderSlipFar   = 8
)

// folderSlipBound is how many slips a query of this length is allowed to be
// wrong by.
func folderSlipBound(text string) int {
	switch n := utf8.RuneCountInString(text); {
	case n < folderSlipFloor:
		return 0
	case n < folderSlipFar:
		return 1
	default:
		return 2
	}
}

// ── the candidate, folded ───────────────────────────────────────────────────

// folderFolded is one candidate's shown path, prepared once. The lowercasing
// and the segment boundaries are what every tier reads, and neither may be
// recomputed per keystroke.
type folderFolded struct {
	// lower is the shown path — `~/code/aforge-v2/internal/tui3` — lowercased.
	// It is the SHOWN spelling and not the absolute one, because a person
	// typing `~/co` is typing what they can see.
	lower string
	// starts is the byte index at which each segment begins, so a segment is a
	// slice of lower and splitting it allocates nothing.
	starts []int
}

// foldFolder prepares one candidate.
func foldFolder(show string) folderFolded {
	lower := strings.ToLower(show)
	// A LEADING SEPARATOR IS NOT A SEGMENT. `/usr/local` has two names in it
	// and not three, and counting the empty one before the first slash would
	// put every absolute path one level further from its own leaf than it is.
	at := 0
	if strings.HasPrefix(lower, "/") {
		at = 1
	}
	f := folderFolded{lower: lower, starts: []int{at}}
	for ; at < len(lower); at++ {
		if lower[at] == '/' && at+1 < len(lower) {
			f.starts = append(f.starts, at+1)
		}
	}
	return f
}

// segment is one segment's text, by its index in starts.
func (f folderFolded) segment(at int) string {
	start := f.starts[at]
	if at+1 < len(f.starts) {
		// The next segment begins one byte past the separator, so the end of
		// this one is two back from it.
		return f.lower[start : f.starts[at+1]-1]
	}
	return f.lower[start:]
}

// base is the leaf name — the last segment, and the one a person is nearly
// always thinking of.
func (f folderFolded) base() string {
	if len(f.starts) == 0 {
		return f.lower
	}
	return f.lower[f.starts[len(f.starts)-1]:]
}

// ── the ladder, rung by rung ────────────────────────────────────────────────

// folderScore is the whole scorer: which rung of the ladder this candidate
// answers this query on, and false when it answers on none of them.
//
// A BLANK QUERY ANSWERS ON THE TOP RUNG WITH NO DETAIL, so every candidate ties
// and the caller's own order stands untouched.
func folderScore(q folderQuery, f folderFolded) (folderHit, bool) {
	if q.blank() {
		return folderHit{}, true
	}
	needle := q.text
	lower, base := f.lower, f.base()

	switch {
	case base == needle:
		return folderHit{tier: folderTierName}, true
	case strings.HasPrefix(base, needle):
		return folderHit{tier: folderTierNameLead, detail: len(base) - len(needle)}, true
	}
	// THE WHOLE PATH IS ASKED BEFORE ITS SEGMENTS ARE. `~/co` is somebody
	// spelling out where they are going from the top, and the first segment's
	// own prefix check would answer the same query on the segment rung —
	// two readings of one gesture, and the more specific one is this.
	if strings.HasPrefix(lower, needle) {
		return folderHit{tier: folderTierPathLead, detail: len(lower) - len(needle)}, true
	}
	if hit, ok := folderSegmentHit(f, needle); ok {
		return hit, true
	}
	if at := strings.Index(base, needle); at >= 0 {
		return folderHit{tier: folderTierNameIn, detail: at<<8 + len(base) - len(needle)}, true
	}
	// THE INITIALS ARE ASKED OF THE LEAF FIRST. `av2` is somebody naming
	// `aforge-v2`, and the same letters read across a whole path are a weaker
	// claim about the same thing — so the second reading is pushed a fixed
	// distance behind the first rather than given a rung of its own.
	if span, ok := folderShort(base, needle); ok {
		return folderHit{tier: folderTierShort, detail: span}, true
	}
	if span, ok := folderShort(lower, needle); ok {
		return folderHit{tier: folderTierShort, detail: folderShortAway + span}, true
	}
	if at := strings.Index(lower, needle); at >= 0 {
		return folderHit{tier: folderTierPathIn, detail: at<<8 + len(lower) - len(needle)}, true
	}
	if span, ok := subsequence(lower, needle); ok {
		return folderHit{tier: folderTierLoose, detail: span<<8 + len(lower)}, true
	}
	return folderSlipHit(f, needle, q.slip)
}

// folderShortAway is how far behind the leaf's own initials a reading across
// the whole path sits. It is larger than any span either reading can produce on
// a path this list will hold, so the two never interleave.
const folderShortAway = 1 << 12

// folderScoreOf is [folderScore] for a caller holding only the shown path. It
// folds on the spot, so it is for a single question and never for a keystroke
// over a whole list — [folderRanker] is what that wants.
func folderScoreOf(query, show string) (folderHit, bool) {
	return folderScore(newFolderQuery(query), foldFolder(show))
}

// folderSegmentHit is the segment rung: the needle is a whole segment above the
// leaf, the lead of one, or a run of segments spelled from a boundary —
// `internal`, `inter`, `internal/tui3`.
//
// IT IS SCANNED FROM THE LEAF UPWARDS and the first hit wins, because a person
// who types a name that appears twice in one path means the one nearer the
// thing they are pointing at. `~/code/aforge/internal/aforge` typed at with
// `aforge` is the deeper one every time.
func folderSegmentHit(f folderFolded, needle string) (folderHit, bool) {
	for at := len(f.starts) - 1; at >= 0; at-- {
		start := f.starts[at]
		if !strings.HasPrefix(f.lower[start:], needle) {
			continue
		}
		end := start + len(needle)
		// A run that stops inside a name is a narrower claim than one that ends
		// on a boundary: `internal/tui` has not finished naming `tui3` yet.
		whole := end == len(f.lower) || f.lower[end] == '/'
		away := len(f.starts) - 1 - at
		detail := away << 9
		if !whole {
			detail += 1 << 8
		}
		return folderHit{tier: folderTierSegment, detail: detail + len(f.lower) - end}, true
	}
	return folderHit{}, false
}

// ── the initials ────────────────────────────────────────────────────────────

// folderShort matches a query against the INITIALS of a name — the letters
// somebody types when they are naming a place rather than spelling it. It
// answers the SPAN the match covered, which is what tells a tight abbreviation
// from a coincidence.
//
// Every character has to land either on the start of a word or immediately
// after the character before it. That one rule is what separates this from the
// loose rung below it: `av2` reaches `aforge-v2` because `a` and `v` start
// words and `2` carries straight on from `v`, while `ag2` reaches it on the
// loose rung only, where it belongs.
//
// A word starts at the beginning, after a separator, or where a digit follows a
// letter. The text is already lowercased by the time it arrives, so camel case
// is not a boundary this can see — and in the directory names people actually
// have, the separators are.
func folderShort(text, needle string) (int, bool) {
	if needle == "" || text == "" {
		return 0, false
	}
	// EVERY WORD START IS TRIED AS THE ANCHOR. A single greedy pass from the
	// first matching letter answers `ab-ac` typed at with `ac` wrongly — it
	// takes the `a` of `ab`, finds no `c` after it, and gives up on a name that
	// abbreviates perfectly from its second word. There are a handful of words
	// in a path, so trying each of them costs nothing worth saving.
	best, found := 0, false
	for at := 0; at < len(text); at++ {
		if !folderWordStart(text, at) || text[at] != needle[0] {
			continue
		}
		span, ok := folderShortFrom(text, needle, at)
		if ok && (!found || span < best) {
			best, found = span, true
		}
	}
	return best, found
}

// folderShortFrom runs the greedy match with the first character pinned at
// start, and answers how far apart the first and last matched characters
// landed.
func folderShortFrom(text, needle string, start int) (int, bool) {
	at, last := start+1, start
	for want := 1; want < len(needle); want++ {
		hit := -1
		for scan := at; scan < len(text); scan++ {
			if text[scan] != needle[want] {
				continue
			}
			// The character carries on from the one before it, or it starts a
			// word of its own. Anything else is a letter that merely appears
			// later, which is the loose rung's business and not this one's.
			if scan == at || folderWordStart(text, scan) {
				hit = scan
				break
			}
		}
		if hit < 0 {
			return 0, false
		}
		at, last = hit+1, hit
	}
	return last - start, true
}

// folderWordStart reports whether the byte at is the first of a word.
func folderWordStart(text string, at int) bool {
	if at == 0 {
		return true
	}
	switch before := text[at-1]; {
	case before == '/', before == '-', before == '_', before == '.', before == ' ', before == '~':
		return true
	case folderDigit(text[at]) && !folderDigit(before):
		return true
	}
	return false
}

func folderDigit(b byte) bool { return b >= '0' && b <= '9' }

// ── the slip ────────────────────────────────────────────────────────────────

// folderSlipHit is the typo rung: a segment that is within bound slips of what
// was typed. It is scanned from the leaf upwards like the segment rung, and the
// FEWEST slips wins before the nearest segment does.
//
// It is only ever reached when every rung above it answered nothing, so the
// cost is paid on exactly the queries that would otherwise show an empty list.
func folderSlipHit(f folderFolded, needle string, bound int) (folderHit, bool) {
	if bound <= 0 {
		return folderHit{}, false
	}
	want := utf8.RuneCountInString(needle)
	best, bestLead, bestAt, found := 0, 0, 0, false
	keep := func(slips, lead, at int) {
		if found && (slips > best || (slips == best && lead >= bestLead)) {
			return
		}
		best, bestLead, bestAt, found = slips, lead, at, true
	}
	for at := len(f.starts) - 1; at >= 0; at-- {
		seg := f.segment(at)
		runes := []rune(seg)
		// A NAME OF A DIFFERENT LENGTH CANNOT BE CLOSE. Each slip changes a
		// length by at most one, so a segment further from the query's length
		// than the bound is skipped without the arithmetic being run at all —
		// which is what keeps this rung affordable over a whole list.
		if len(runes) >= want-bound && len(runes) <= want+bound {
			if slips, ok := folderSlips(seg, needle, bound); ok {
				keep(slips, 0, at)
			}
			continue
		}
		// AND A LONGER NAME IS MISTYPED AT ITS START. `afroge` is somebody
		// reaching for `aforge-v2`, which no whole-name comparison can see: the
		// name is three characters longer than the query and the query was not
		// finished. So the START of the name is compared too, at every length
		// the bound allows the query to have been — which is the name-lead rung
		// again, with the slips let in.
		if len(runes) > want+bound {
			for size := max(want-bound, 1); size <= want+bound && size <= len(runes); size++ {
				if slips, ok := folderSlips(string(runes[:size]), needle, bound); ok {
					keep(slips, 1, at)
					break
				}
			}
		}
	}
	if !found {
		return folderHit{}, false
	}
	away := len(f.starts) - 1 - bestAt
	return folderHit{tier: folderTierSlip, detail: best<<12 + bestLead<<10 + away<<2}, true
}

// folderSlips is how far apart two names are, counting A SWAPPED PAIR OF
// LETTERS AS ONE SLIP — the restricted Damerau edit distance, stopped as soon
// as it passes the bound. It answers false when they are further apart than
// that, so a caller never learns a distance it would not have used.
//
// The transposition is the whole reason this is not the plain edit distance.
// `afroge` for `aforge` is two substitutions to Levenshtein and one slip to a
// person, and treating it as two would put it level with `abcdef` typed at a
// six letter name — which is not a typo, it is a different word.
//
// It runs over runes and not bytes, because a directory name with an accent in
// it is a name somebody typed and may mistype.
func folderSlips(a, b string, bound int) (int, bool) {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(right) == 0 {
		n := max(len(left), len(right))
		return n, n <= bound
	}
	// Three rows are all the restricted distance ever reads: the row before
	// last is what a transposition looks back at.
	back := make([]int, len(right)+1)
	prev := make([]int, len(right)+1)
	row := make([]int, len(right)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(left); i++ {
		row[0] = i
		least := row[0]
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			step := min(prev[j]+1, row[j-1]+1)
			step = min(step, prev[j-1]+cost)
			if i > 1 && j > 1 && left[i-1] == right[j-2] && left[i-2] == right[j-1] {
				step = min(step, back[j-2]+1)
			}
			row[j] = step
			least = min(least, step)
		}
		// THE ROW'S BEST IS A FLOOR UNDER EVERYTHING BELOW IT. Once no cell in
		// a row is within the bound, no row after it can be either, and the
		// remaining work is arithmetic nobody will read.
		if least > bound {
			return 0, false
		}
		back, prev, row = prev, row, back
	}
	if prev[len(right)] > bound {
		return 0, false
	}
	return prev[len(right)], true
}

// ── ranking a whole list ────────────────────────────────────────────────────

// folderRankee is one candidate as the ranker needs to see it. It is a struct
// of its own rather than the picker's own row type so that this file can be
// wired into a surface without either of them owning the other's shape — the
// caller fills four fields and reads back indexes into what it handed over.
type folderRankee struct {
	// Show is the path as a person reads it, `~` and all. It is what the query
	// is scored against, because it is what is on screen.
	Show string
	// Layer is which source the candidate came from, lower being nearer — the
	// picker's own ladder, passed through untouched.
	Layer int
	// Rank is the position the candidate held in its own source's order, and is
	// the last word on ties.
	Rank int
	// Freq is recency × frequency of prior picks, higher being more used.
	Freq float64
}

// folderRanker ranks one candidate set against query after query. It holds the
// folded text and the per-pass scratch, so a keystroke costs one pass over the
// candidates and no allocation at all.
type folderRanker struct {
	cands []folderRankee
	fold  []folderFolded
	hit   []folderHit
	hits  []int
}

// load takes a candidate set and folds it once. The candidates are held by
// reference and must not be rewritten under the ranker; a changed set is a
// second load.
func (r *folderRanker) load(cands []folderRankee) {
	r.cands = cands
	if cap(r.fold) < len(cands) {
		r.fold = make([]folderFolded, len(cands))
		r.hit = make([]folderHit, len(cands))
	}
	r.fold, r.hit = r.fold[:len(cands)], r.hit[:len(cands)]
	for at, cand := range cands {
		r.fold[at] = foldFolder(cand.Show)
	}
	r.hits = r.hits[:0]
}

// rank answers the indexes of every candidate the query reaches, best first.
// The slice is the ranker's own and is overwritten by the next call.
//
// THE ORDER IS THE RUNG, THEN THE LAYER, THEN WHAT IS USED, THEN THE TIGHTNESS
// OF THE MATCH, THEN THE SOURCE'S OWN ORDER. See this file's header for why the
// tightness sits that far down.
func (r *folderRanker) rank(query string) []int {
	q := newFolderQuery(query)
	r.hits = r.hits[:0]
	for at := range r.cands {
		hit, ok := folderScore(q, r.fold[at])
		if !ok {
			continue
		}
		r.hit[at] = hit
		r.hits = append(r.hits, at)
	}
	sort.SliceStable(r.hits, func(a, b int) bool {
		return r.less(r.hits[a], r.hits[b])
	})
	return r.hits
}

// less is the whole ordering, one comparison per line.
func (r *folderRanker) less(a, b int) bool {
	if r.hit[a].tier != r.hit[b].tier {
		return r.hit[a].tier < r.hit[b].tier
	}
	if r.cands[a].Layer != r.cands[b].Layer {
		return r.cands[a].Layer < r.cands[b].Layer
	}
	if r.cands[a].Freq != r.cands[b].Freq {
		return r.cands[a].Freq > r.cands[b].Freq
	}
	if r.hit[a].detail != r.hit[b].detail {
		return r.hit[a].detail < r.hit[b].detail
	}
	return r.cands[a].Rank < r.cands[b].Rank
}
