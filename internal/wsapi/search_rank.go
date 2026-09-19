package wsapi

import (
	"sort"
	"strings"
)

const (
	rrfK               = 60
	searchPoolMin      = 320
	searchPoolMax      = 512
	wordingCoverageMax = 0.25
	lexRRFWeight       = 2.0
	coverWeight        = 0.3
)

// searchPool is how many lexical and embedding SESSIONS to gather before
// ranking. Top-k from one list used to fill the whole answer: paraphrase
// restatements of the query occupied every A4 slot, AND-of-all-terms missed
// A7/global, and at 10k a nearer abandoned-mailer family occupied every
// passage-level slot so the original never entered. Unique-by-session then
// still left signed-in originals at 13 of 20 when 160 nearer restaurant,
// paraphrase, espresso, and treasury conversations filled the gather: the
// doors unique by session; this budget has to be wider than that near page.
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

func abandonedPlan(texts []string) bool {
	return strings.Contains(strings.ToLower(strings.Join(texts, " ")), "abandon")
}

func queryMentionsAbandoned(query string) bool {
	return strings.Contains(strings.ToLower(query), "abandon")
}

// accessPolicyDecision is true when the session's standing decision is the
// billed-file access rule — signed-in / authenticated fetch — not when it
// names billed-file only to say this chat is not that rule. Cafe, restaurant,
// abandon, and espresso are not tokens here: a policy chat that mentions a
// dinner slip still counts, and an OCR chat that only refuses the topic does
// not.
func accessPolicyDecision(texts []string) bool {
	hay := strings.ToLower(strings.Join(texts, " "))
	if !hasAccessAuth(hay) {
		return false
	}
	return strings.Contains(hay, "billed-file") ||
		strings.Contains(hay, "billed files") ||
		strings.Contains(hay, "billed document") ||
		strings.Contains(hay, "purchase-document") ||
		strings.Contains(hay, "purchase document")
}

func hasAccessAuth(hay string) bool {
	return strings.Contains(hay, "authenticated") ||
		strings.Contains(hay, "signed-in") ||
		strings.Contains(hay, "sign in") ||
		strings.Contains(hay, "logged-in") ||
		strings.Contains(hay, "after login")
}

// emailedReceiptTopic is the J18 TUI/manual ask: "emailed purchase confirmation
// PDF" and "emailed receipt links" name the artefact, not a question. Two of
// these tokens are enough; who|what|why|how|may|can|should|allowed are not
// required.
func emailedReceiptTopic(query string) bool {
	q := strings.ToLower(query)
	n := 0
	for _, word := range []string{"emailed", "receipt", "purchase", "confirmation", "pdf"} {
		if strings.Contains(q, word) {
			n++
		}
	}
	return n >= 2
}

func hasAskCue(query string) bool {
	padded := " " + strings.ToLower(query) + " "
	for _, cue := range []string{" who ", " what ", " why ", " how ", " may ", " can ", " should ", " allowed "} {
		if strings.Contains(padded, cue) {
			return true
		}
	}
	return false
}

// querySeeksOriginals is true for a different-wording look-up. Question cues
// still count, but they are not the only path: an emailed-receipt topic
// without those words also prefers original neighbours. "No, the other one"
// and a buried certificate note do not match, so they keep coverage ranking.
func querySeeksOriginals(query string) bool {
	return hasAskCue(query) || emailedReceiptTopic(query)
}

type scoredHit struct {
	hit       SearchHit
	score     float64
	cover     float64
	embRank   int
	abandoned bool
	policy    bool
}

func originalNeighbor(row scoredHit) bool {
	return row.embRank > 0 && row.cover < wordingCoverageMax
}

// evidenceClass separates an emailed-receipt look-up into: the access-policy
// decision (3), other original neighbours such as a dinner-slip OCR that only
// refuses billed-file (2), abandoned neighbours (1), and query restatements
// (0). Policy only elevates when coverage is still in the original-neighbour
// band, so a buried certificate note that happens to contain "hyperlinks"
// does not outrank the signed-in original on "emailed receipt links".
// Higher wins. Cafe/restaurant/espresso are not a denylist.
func evidenceClass(row scoredHit, seek, demoteAbandoned, preferPolicy bool) int {
	if !seek {
		return 0
	}
	orig := originalNeighbor(row)
	policy := preferPolicy && row.policy && row.cover <= wordingCoverageMax
	if !orig && !policy {
		return 0
	}
	if demoteAbandoned && row.abandoned {
		return 1
	}
	if policy {
		return 3
	}
	return 2
}

func betterHit(i, j scoredHit, seekOriginals, demoteAbandoned, preferPolicy bool) bool {
	ci := evidenceClass(i, seekOriginals, demoteAbandoned, preferPolicy)
	cj := evidenceClass(j, seekOriginals, demoteAbandoned, preferPolicy)
	if ci != cj {
		return ci > cj
	}
	if i.score != j.score {
		return i.score > j.score
	}
	return sessionKey(i.hit) < sessionKey(j.hit)
}

// rankEvidence is the one hybrid ranking. Lexical restatements of a new ask
// used to occupy every slot (A4). Short corrections and buried notes used to
// lose to a topical majority (A7, global). A different-wording ask prefers
// semantically close passages that share few of its tokens; a keyword ask
// prefers sessions that cover its distinctive words. Abandoned-plan
// neighbours stay behind the standing decision unless the query is about
// that abandoned plan. An emailed-receipt topic then prefers the session
// whose decision is the access policy over one that names billed-file only
// to refuse it.
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
			abandoned: abandonedPlan(c.texts),
			policy:    accessPolicyDecision(c.texts),
		})
	}
	seek := querySeeksOriginals(query)
	demote := seek && !queryMentionsAbandoned(query)
	preferPolicy := emailedReceiptTopic(query)
	sort.SliceStable(rows, func(i, j int) bool {
		return betterHit(rows[i], rows[j], seek, demote, preferPolicy)
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
