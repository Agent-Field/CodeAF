package wsdiscover

import "sort"

const (
	// sessionSpread is how many BM25 rows to read before unique-by-session.
	// A 10k corpus of two-turn mailers filled a passage-level page with one
	// family; the original signed-in decision never entered SearchEvidence.
	sessionSpread = 8
	// sessionExtra keeps later turns of a selected session so ranking still
	// sees "abandon" beside the closer "mail customers" passage.
	sessionExtra  = 3
	searchWideMax = 2048
)

func queryFits(query []float32, dim int) bool {
	return dim > 0 && len(query) == dim
}

func wideLimit(limit int) int {
	if limit < 1 {
		limit = 20
	}
	wide := limit * sessionSpread
	if wide > searchWideMax {
		return searchWideMax
	}
	return wide
}

type scoredPassage struct {
	p Passage
	s float64
}

func scoreSimilar(candidates []Passage, query []float32, model, version string, dim int) []Passage {
	hits := make([]scoredPassage, 0, len(candidates))
	for _, p := range candidates {
		if !sameEmbedding(p, model, version, dim) {
			continue
		}
		hits = append(hits, scoredPassage{p: p, s: cosine(query, p.Vector)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	out := make([]Passage, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}

// preferSessions takes ranked passages and keeps `limit` sessions, plus a few
// extra turns from those sessions. Truncating to `limit` passages first is
// what left A4 originals out when 80 two-turn abandoned mailers were closer.
func preferSessions(rows []Passage, limit, extra int) []Passage {
	if limit < 1 {
		return nil
	}
	if extra < 0 {
		extra = 0
	}
	seen := map[string]int{}
	first := make([]Passage, 0, limit)
	more := make([]Passage, 0)
	for _, p := range rows {
		n := seen[p.SessionID]
		if n == 0 {
			if len(first) >= limit {
				continue
			}
			seen[p.SessionID] = 1
			first = append(first, p)
			continue
		}
		if n > extra {
			continue
		}
		seen[p.SessionID] = n + 1
		more = append(more, p)
	}
	return append(first, more...)
}
