package wsdiscover

import "strings"

// ftsMatchQuery turns free text into a safe FTS5 OR-query. AND of every token
// misses mixed-topic and multi-turn hits: a short correction and a buried
// access note never share one passage with the whole ask. Hostile syntax and
// queries with no usable term are empty, which SearchLexical treats as a miss.
func ftsMatchQuery(query string) string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	})
	kept := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		if _, stop := ftsStop[field]; len(field) < 3 || stop {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		kept = append(kept, field)
		if len(kept) == 32 {
			break
		}
	}
	return strings.Join(kept, " OR ")
}

// ftsStop are English glue words that would otherwise OR-match half the
// corpus ("the kitchen" vs "the other one") and drown a short correction.
var ftsStop = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "but": {}, "are": {}, "was": {},
	"you": {}, "all": {}, "can": {}, "had": {}, "her": {}, "his": {},
	"its": {}, "our": {}, "this": {}, "that": {}, "from": {}, "with": {},
}
