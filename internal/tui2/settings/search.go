package settings

import (
	"sort"
	"strconv"
	"strings"
)

// Global fuzzy search across all tabs (8.2.19). Any printable character starts
// it, so the search is not a mode the user enters — it is what typing means
// here, and the hint line says so before the first keystroke.
//
// The scorer is the same shape as internal/registry's (an in-order
// subsequence, scored by how early each letter lands, with a flat bonus for a
// prefix), deliberately: a user who has learned how the ctrl+k palette ranks
// rows should not have to learn a second ranking here. It is restated rather
// than borrowed because the corpus is different — a settings row is four
// fields, not two, and this one has to report WHERE it matched so the label
// can highlight the letters (5.18's fzf-style idiom) and so a row that matched
// only on its hint sorts under one that matched its name.

// match is one row that answered a query.
type match struct {
	row   int
	score int
	// hits are the byte offsets in the row's label that the query landed on,
	// for highlighting. Empty when the match came from the hint, the group or
	// the key instead — highlighting a letter that is not there would be a
	// lie about why the row is in the list.
	hits []int
}

// Field penalties. A name match beats a group match beats a key match beats a
// hint match, at any score, because a user typing "budget" means the row
// called budget and not the four rows whose hints mention money.
const (
	penaltyGroup = 500
	penaltyKey   = 900
	penaltyHint  = 1500
)

// search scores every visible-by-gate row against the query and returns the
// matches, best first, stable on ties so equal rows keep registry order.
func (m *Model) search(query string) []match {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	matches := make([]match, 0, len(m.rows))
	for index, r := range m.rows {
		if !m.shown(r) {
			continue
		}
		best, hits, ok := scoreRow(r, needle)
		if !ok {
			continue
		}
		matches = append(matches, match{row: index, score: best, hits: hits})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score < matches[j].score })
	return matches
}

func scoreRow(r row, needle string) (int, []int, bool) {
	best, hits, found := 0, []int(nil), false

	if score, at, ok := scoreSubsequence(strings.ToLower(r.setting.Label), needle); ok {
		best, hits, found = score, at, true
	}
	consider := func(haystack string, penalty int) {
		score, _, ok := scoreSubsequence(strings.ToLower(haystack), needle)
		if !ok {
			return
		}
		score += penalty
		if !found || score < best {
			// A weaker field won: the label's highlight positions no longer
			// explain the match, so they go.
			best, found = score, true
			if penalty > 0 {
				hits = nil
			}
		}
	}
	consider(r.group, penaltyGroup)
	consider(r.setting.Key, penaltyKey)
	consider(r.setting.Hint, penaltyHint)

	return best, hits, found
}

// scoreSubsequence matches needle into haystack in order, not necessarily
// contiguously, and returns the sum of the positions each letter landed at —
// so the same letters found early in a short label beat them found late in a
// long hint. A needle that prefixes the haystack takes a flat bonus, which is
// what makes "doc" rank "document engine" above every row whose hint happens
// to contain a d, an o and a c.
//
// Both strings are read a byte at a time. Every label, key and category in
// internal/config is ASCII (its own tests hold that line), and a hint that is
// not still matches correctly on its ASCII letters — a multi-byte rune simply
// cannot be a needle byte, so the worst case is a hint that does not match,
// never a wrong offset into a label.
func scoreSubsequence(haystack, needle string) (int, []int, bool) {
	if needle == "" {
		return 0, nil, true
	}
	position, score := 0, 0
	hits := make([]int, 0, len(needle))
	for index := 0; index < len(needle); index++ {
		wanted := needle[index]
		if wanted == ' ' {
			// A space in the query is a word boundary the user typed, not a
			// letter to find: "chat w" should find "chat width".
			continue
		}
		found := false
		for position < len(haystack) {
			if haystack[position] == wanted {
				score += position
				hits = append(hits, position)
				position++
				found = true
				break
			}
			position++
		}
		if !found {
			return 0, nil, false
		}
	}
	if len(hits) == 0 {
		return 0, nil, true
	}
	if len(needle) <= len(haystack) && haystack[:len(needle)] == needle {
		score -= 100
	}
	return score, hits, true
}

// filterBreadcrumb is what the tab bar becomes while a search is running
// (8.2.19: "the tab bar becomes a filter breadcrumb"). It names the groups the
// results came from, in registry order, with a count each — so the result list
// still reads as navigation across the tabs rather than as a flat list that
// lost its structure.
func (m *Model) filterBreadcrumb() []string {
	counts := make(map[string]int, len(m.tabs))
	for _, index := range m.visible {
		counts[m.rows[index].group]++
	}
	crumbs := make([]string, 0, len(m.tabs))
	for _, title := range m.tabs {
		if counts[title] == 0 {
			continue
		}
		crumbs = append(crumbs, title+" "+strconv.Itoa(counts[title]))
	}
	return crumbs
}
