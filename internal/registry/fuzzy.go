package registry

import "sort"

// Match pairs an entry with its score against one fuzzy query. Lower is a
// better match — the same convention internal/tui/commands.go's own
// fuzzyScore already uses for the model and cancel-target pickers, kept here
// so a render surface that knows that idiom does not have to learn a second
// one for the registry's rows.
type Match struct {
	Entry Entry
	Score int
}

// FuzzyMatch scores every entry in scope against query and returns the ones
// that match, best first, stable on ties. An empty query matches every entry
// in scope at score 0 — the palette's own convention that an untyped filter
// is not a filter, just the unfiltered list.
//
// It is not reused from internal/tui/commands.go's fuzzyScore: that function
// is correct and does the same job, but it is unexported package-private
// state, and the dependency direction this package holds to is tui →
// registry, never the reverse — importing tui from here to borrow one
// function would invert it. The algorithm is restated instead, verbatim in
// behavior: an in-order subsequence match scored by how early each letter
// lands, with a flat bonus for a leading match.
func FuzzyMatch(scope Scope, query string) []Match {
	needle := toLower(query)
	matches := make([]Match, 0, len(entries))
	for _, entry := range entries {
		if !entry.Scope.Has(scope) {
			continue
		}
		verbScore, verbOK := scoreSubsequence(entry.lowerVerb, needle)
		descScore, descOK := scoreSubsequence(entry.lowerDescription, needle)
		switch {
		case verbOK && descOK:
			matches = append(matches, Match{Entry: entry, Score: min(verbScore, descScore)})
		case verbOK:
			matches = append(matches, Match{Entry: entry, Score: verbScore})
		case descOK:
			matches = append(matches, Match{Entry: entry, Score: descScore})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score < matches[j].Score })
	return matches
}

// scoreSubsequence is the bounded, allocation-light scorer behind FuzzyMatch:
// needle must appear in haystack in order, not necessarily contiguous. The
// score is the sum of the position each letter was found at, so a query
// found early in a short word beats the same letters found late in a long
// one; a query that prefixes haystack outright gets a flat bonus, so typing
// "can" ranks "cancel" ahead of "expedite" even though both contain the
// letters somewhere.
//
// Both strings are read a byte at a time rather than decoded as runes. Every
// verb and description seeded in catalog.go is plain ASCII (checked by
// TestCatalogTextIsASCII), so byte indexing is exact for this catalog and
// costs no rune-slice allocation on a path a palette calls once per
// keystroke.
func scoreSubsequence(haystack, needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	position, score := 0, 0
	for index := 0; index < len(needle); index++ {
		wanted := needle[index]
		found := false
		for position < len(haystack) {
			if haystack[position] == wanted {
				score += position
				position++
				found = true
				break
			}
			position++
		}
		if !found {
			return 0, false
		}
	}
	if len(needle) <= len(haystack) && haystack[:len(needle)] == needle {
		score -= 100
	}
	return score, true
}
