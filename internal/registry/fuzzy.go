package registry

import (
	"sort"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
)

// Match pairs an entry with its score against one fuzzy query. HIGHER is a
// better match — the one convention every picker on the chat surface ranks
// with (internal/fuzzy), which this list now answers in.
type Match struct {
	Entry Entry
	Score int
}

// FuzzyMatch scores every entry in scope against query and returns the ones
// that match, best first, stable on ties. An empty query matches every entry
// in scope at score 0 — the palette's own convention that an untyped filter
// is not a filter, just the unfiltered list.
//
// THE SCORER IS THE SHARED ONE (internal/fuzzy) and no longer this package's
// own: the verb and the description are the two fields an entry answers in,
// scored per term by whichever carries the word better, exactly the way the
// settings sheet and the model picker score their rows. The greedy
// position-sum walk this used to be — the first occurrence of each letter,
// summed, with a flat bonus for a prefix — answered A match and called it a
// ranking; the alignment finds the BEST one, so a word that prefixes an entry
// outranks the same letters scattered through a longer one by score rather
// than by a bonus bolted on top. The case law is the old one kept: the query
// is folded before it becomes terms, so a search here answers any casing, as
// it always did.
//
// No live caller remains outside this package's own tests — the pickers that
// once asked the registry now share the one matcher. The exported shape stays
// because the registry is the one list several surfaces could ask, and this
// is the door it answers in, so there is no second scorer beside the shared
// one to drift.
func FuzzyMatch(scope Scope, query string) []Match {
	// The fold is the package's own ASCII one: every id, verb, description and
	// alias seeded in catalog.go is plain ASCII (TestCatalogTextIsASCII holds
	// it), so the fold is exact, and the matcher reads the fields' own casing
	// for the character classes its bonus model pays for.
	terms := fuzzy.Terms(toLower(query))
	fields := make([]string, 2)
	matches := make([]Match, 0, len(entries))
	for _, entry := range entries {
		if !entry.Scope.Has(scope) {
			continue
		}
		fields[0], fields[1] = entry.Verb, entry.Description
		score, ok := fuzzy.ScoreFields(fields, terms)
		if !ok {
			continue
		}
		matches = append(matches, Match{Entry: entry, Score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	return matches
}
