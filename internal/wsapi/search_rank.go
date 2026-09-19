package wsapi

import (
	"sort"
	"strings"
)

const (
	rrfK               = 60
	searchPoolMin      = 40
	searchPoolMax      = 160
	wordingCoverageMax = 0.25
	lexRRFWeight       = 2.0
	coverWeight        = 0.3
)

// searchPool is how many lexical and embedding candidates to gather before
// ranking. Top-k from one list used to fill the whole answer: paraphrase
// restatements of the query occupied every A4 slot, and AND-of-all-terms
// missed A7/global. The pool has to be wider than the page.
func searchPool(limit int) int {
	pool := limit * 8
	if pool < searchPoolMin {
		pool = searchPoolMin
	}
	if pool > searchPoolMax {
		pool = searchPoolMax
	}
	if pool < limit {
		return limit
	}
	return pool
}

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	})
	kept := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		if _, stop := queryStop[field]; len(field) < 3 || stop {
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
	return kept
}

// queryStop matches wsdiscover's FTS glue list so coverage and MATCH agree.
var queryStop = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "but": {}, "are": {}, "was": {},
	"you": {}, "all": {}, "can": {}, "had": {}, "her": {}, "his": {},
	"its": {}, "our": {}, "this": {}, "that": {}, "from": {}, "with": {},
}

func coverageOf(terms []string, texts ...string) float64 {
	if len(terms) == 0 {
		return 0
	}
	hay := strings.ToLower(strings.Join(texts, " "))
	hit := 0
	for _, term := range terms {
		if strings.Contains(hay, term) {
			hit++
		}
	}
	return float64(hit) / float64(len(terms))
}

func rrf(rank int) float64 {
	if rank < 1 {
		return 0
	}
	return 1 / float64(rrfK+rank)
}

type sessionCand struct {
	hit              SearchHit
	lexRank, embRank int
	texts            []string
}

func sessionKey(hit SearchHit) string {
	if hit.SessionID != "" {
		return hit.SessionID
	}
	return hit.Ref
}

func stampedKind(hit SearchHit, kind string) SearchHit {
	if hit.ScoreKind != "" {
		return hit
	}
	if kind != "emb" {
		hit.ScoreKind = ScoreBM25
		return hit
	}
	if hit.Degraded {
		hit.ScoreKind = ScoreExpansion
		return hit
	}
	hit.ScoreKind = ScoreEmbed
	return hit
}

func noteHit(by map[string]*sessionCand, hit SearchHit, kind string, rank int) {
	key := sessionKey(hit)
	c := by[key]
	if c == nil {
		c = &sessionCand{hit: stampedKind(hit, kind)}
		by[key] = c
	}
	c.texts = append(c.texts, hit.Passage)
	if kind == "lex" && c.lexRank == 0 {
		c.lexRank = rank
	}
	if kind == "emb" && c.embRank == 0 {
		c.embRank = rank
		c.hit = stampedKind(hit, kind)
	}
}

func collectSessions(lexical, embed []SearchHit) []*sessionCand {
	by := map[string]*sessionCand{}
	for i, hit := range lexical {
		noteHit(by, hit, "lex", i+1)
	}
	for i, hit := range embed {
		noteHit(by, hit, "emb", i+1)
	}
	out := make([]*sessionCand, 0, len(by))
	for _, c := range by {
		out = append(out, c)
	}
	return out
}

func sessionScore(c *sessionCand, cover float64) float64 {
	return lexRRFWeight*rrf(c.lexRank) + rrf(c.embRank) + coverWeight*cover
}

// querySeeksOriginals is the J09 shape: a new ask ("who may fetch the emailed
// PDF") looking for an earlier decision that used different words. Keyword
// phrases such as "No, the other one" are looking for those words, not for a
// different-wording neighbour.
func querySeeksOriginals(query string) bool {
	padded := " " + strings.ToLower(query) + " "
	for _, cue := range []string{" who ", " what ", " why ", " how ", " may ", " can ", " should ", " allowed "} {
		if strings.Contains(padded, cue) {
			return true
		}
	}
	return false
}

type scoredHit struct {
	hit     SearchHit
	score   float64
	cover   float64
	embRank int
}

func originalNeighbor(row scoredHit) bool {
	return row.embRank > 0 && row.cover < wordingCoverageMax
}

func betterHit(i, j scoredHit, seekOriginals bool) bool {
	if seekOriginals {
		io, jo := originalNeighbor(i), originalNeighbor(j)
		if io != jo {
			return io
		}
	}
	if i.score != j.score {
		return i.score > j.score
	}
	return sessionKey(i.hit) < sessionKey(j.hit)
}

// rankEvidence is the one hybrid ranking. Lexical restatements of a new ask
// used to occupy every slot (A4). Short corrections and buried notes used to
// lose to a topical majority (A7, global). A question-shaped ask prefers
// semantically close passages that do not restate it; a keyword ask prefers
// sessions that cover its distinctive words.
func rankEvidence(query string, lexical, embed []SearchHit, limit int) []SearchHit {
	if limit < 1 {
		limit = defaultSearchLimit
	}
	terms := queryTerms(query)
	cands := collectSessions(lexical, embed)
	rows := make([]scoredHit, 0, len(cands))
	for _, c := range cands {
		cover := coverageOf(terms, c.texts...)
		rows = append(rows, scoredHit{
			hit: c.hit, score: sessionScore(c, cover),
			cover: cover, embRank: c.embRank,
		})
	}
	seek := querySeeksOriginals(query)
	sort.SliceStable(rows, func(i, j int) bool {
		return betterHit(rows[i], rows[j], seek)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]SearchHit, len(rows))
	for i, row := range rows {
		out[i] = row.hit
	}
	return out
}
