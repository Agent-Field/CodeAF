// Taste is what the user keeps correcting. The loop is deliberately the
// quietest one in the resident: corrections the distiller already wrote down
// are aggregated at the settle seam into a candidate rule, a candidate rule
// annotates one delivery with a single question that never holds anything up,
// and the answers stand the rule up or let it go. Nothing here calls a model,
// and nothing here renders — the question is an ordinary structured askback and
// the receipt is an ordinary learning moment.
package resident

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// TastePromotionConfidence is the shrunk standing a candidate must reach
	// before the delivery gate is held to it. Under the store's shared n/(n+8)
	// prior three unanimous confirmations clear it (0.636) and two do not
	// (0.600), so a rule is stood up by a third agreement — never by the pair
	// that merely named it.
	TastePromotionConfidence = 0.63
	// TasteDemotionKeeps is how many times the user has to say the delivery was
	// right as it was before an active rule steps back down. Once is an
	// exception; twice is the rule being wrong about them.
	TasteDemotionKeeps = 2
	// TasteSimilarityFloor is the overlap score below which two corrections are
	// not the same correction. It is the pass mark of the same scorer that
	// decides whether two scope names mean one thing; below it a pair shares
	// only the grammar every preference line is written in.
	TasteSimilarityFloor = 50
	// TasteNeutralPrior is what a rule with no verdicts is worth. Nobody has
	// agreed or refused yet, so the shrinkage pulls it toward a coin flip.
	TasteNeutralPrior = 0.5

	// tasteAskWindow is how long an unanswered annotation stays relevant. The
	// question is about a delivery the user is looking at now; a day later it is
	// a quiz about work they have moved on from, so it retires itself.
	tasteAskWindow = 6 * time.Hour
	// tasteCorrectionScan bounds the aggregation pass. Taste is a slow pattern;
	// it only ever needs the recent end of the notebook.
	tasteCorrectionScan = 24
	// tasteEvidenceLimit bounds one rule's evidence scan. A rule with more
	// corrections behind it than this is already past every promotion bar.
	tasteEvidenceLimit = 12
	// tasteBlockBytes bounds the settled-taste block the delivery gate reads,
	// which sits ahead of the notebook digest and must not crowd it out.
	tasteBlockBytes = 512
)

// tasteKeepLabel is the calm default: the delivery was right as it was.
const tasteKeepLabel = "keep it this way"

// tasteMeantLabel is the user agreeing the rule is what they wanted all along.
const tasteMeantLabel = "this is what I meant"

// TasteStanding is one shelf's evidence, derived rather than stored: the
// corrections that still say the rule, the answers that agreed, and the answers
// that did not. Rebuild replays the facts and the questions; this recomputes.
type TasteStanding struct {
	Scope string
	// For counts the corrections behind the rule plus every "this is what I
	// meant"; Against counts every "keep it this way".
	For     int
	Against int
	// KeepsSince counts only the refusals that landed after the rule's current
	// standing was recorded, which is what a demotion may act on.
	KeepsSince int
	// Confidence is For/(For+Against) shrunk toward the neutral prior by the
	// same n/(n+8) rule every other sensor in the store is judged by.
	Confidence float64
}

// TasteStandingOf projects one rule's evidence. The rule's own line is never
// its own evidence, and a superseded standing's verdicts still count — they
// belong to the shelf, not to the row that happened to hold it.
func TasteStandingOf(graph *store.Store, rule store.Fact) (TasteStanding, error) {
	standing := TasteStanding{Scope: rule.Scope, Confidence: TasteNeutralPrior}
	if graph == nil {
		return standing, nil
	}
	corrections, err := similarCorrections(graph, rule.Body, rule.Seq)
	if err != nil {
		return standing, err
	}
	standing.For = len(corrections)
	answers, err := graph.TasteAnswers()
	if err != nil {
		return standing, err
	}
	for _, answer := range answers {
		if answer.Scope != rule.Scope {
			continue
		}
		if answer.Answer == store.TasteAnswerMeant {
			standing.For++
			continue
		}
		standing.Against++
		if answer.Seq > rule.Seq {
			standing.KeepsSince++
		}
	}
	if total := standing.For + standing.Against; total > 0 {
		standing.Confidence = store.ShrunkRate(
			float64(standing.For)/float64(total), TasteNeutralPrior, total)
	}
	return standing, nil
}

// similarCorrections is the aggregation's two-stage read: the notebook's BM25
// index narrows the field to the lines that share words, and the token-overlap
// scorer the scope-merge pass already uses decides which of them are the same
// correction said twice. BM25 alone cannot: until the notebook is large, every
// taste word sits in most of its lines and the idf term goes to nothing.
func similarCorrections(graph *store.Store, body string, excludeSeq int64) ([]store.Fact, error) {
	neighbours, err := graph.NeighbouringCorrections(body, excludeSeq, tasteEvidenceLimit)
	if err != nil {
		return nil, err
	}
	anchor := tasteTokens(body)
	similar := make([]store.Fact, 0, len(neighbours))
	for _, neighbour := range neighbours {
		if normalizedTokenSimilarity(anchor, tasteTokens(neighbour.Body)) >= TasteSimilarityFloor {
			similar = append(similar, neighbour)
		}
	}
	return similar, nil
}

// tasteTokens is the scope-merge tokenizer aimed at a sentence instead of a
// shelf name: fold case, keep letters and digits, singularize, deduplicate.
func tasteTokens(body string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(body)), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})
	seen := make(map[string]bool, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := singularScopeToken(field)
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

// TasteBlock renders the rules the gate is actually held to. Only active rules
// appear: a candidate has not earned the right to fail anyone's work.
func TasteBlock(graph *store.Store) string {
	if graph == nil {
		return ""
	}
	rules, err := graph.TasteRules(store.FactActive)
	if err != nil || len(rules) == 0 {
		return ""
	}
	var block strings.Builder
	for _, rule := range rules {
		line := "- " + firstLine(rule.Body) + "\n"
		// One long rule must not end the block. Breaking here let a single
		// verbose rule swallow the budget for every shorter rule behind it —
		// which, in the order these arrive, meant the gate silently stopped
		// being held to rules it had genuinely earned. Skipping the line that
		// does not fit and carrying on spends the budget on as many rules as it
		// can hold.
		if block.Len()+len(line) > tasteBlockBytes {
			continue
		}
		block.WriteString(line)
	}
	return strings.TrimSuffix(block.String(), "\n")
}

// AnnotateDelivery queues at most one quiet taste question against a landing
// deliverable and reports whether it asked. The delivery never waits on it: the
// question is queued for the next natural moment, which is the very message the
// deliverable is announced in, and it expires on its own if nobody answers.
func AnnotateDelivery(graph *store.Store, node store.Node) (store.AgentQuestion, bool, error) {
	sessionID := strings.TrimSpace(node.Provenance.SessionID)
	if graph == nil || sessionID == "" {
		return store.AgentQuestion{}, false, nil
	}
	rule, found, err := relevantTasteCandidate(graph, node)
	if err != nil || !found {
		return store.AgentQuestion{}, false, err
	}
	prompt := firstLine(rule.Body) + " — or keep it the way I just did it?"
	options := []store.QuestionOption{
		{Label: tasteKeepLabel, Value: store.TasteOptionValue(store.TasteAnswerKeep, rule.Scope)},
		{Label: tasteMeantLabel, Value: store.TasteOptionValue(store.TasteAnswerMeant, rule.Scope)},
	}
	ask, _, err := graph.ShouldAsk(store.QuestionCategoryTaste)
	if err != nil {
		return store.AgentQuestion{}, false, err
	}
	if !ask {
		// The measured answer: this user keeps what they are given. Journal the
		// skipped ask so the same machinery notices if that ever changes.
		return store.AgentQuestion{}, false, graph.RecordAssumedWithDefault(
			store.QuestionCategoryTaste, tasteKeepLabel, sessionID, prompt)
	}
	allowFree := true
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: sessionID,
		Text: boundMessage(store.QuestionMessageBody(prompt, options, store.QuestionConfig{
			Kind: store.QuestionChoose, Category: store.QuestionCategoryTaste,
			Default: "1", AllowFree: &allowFree,
		})),
		// Deliberately not anchored to the node: a node-anchored question retires
		// the moment its job settles, and this one is asked because the job just
		// did. Its own relevance window is what retires it.
		Urgency: store.QuestionNextNaturalMoment, Options: options,
		Category: store.QuestionCategoryTaste, DefaultAnswer: "1",
		ExpiresAt: time.Now().Add(graph.SilenceConsentWait(tasteAskWindow)),
	})
	if err != nil {
		return store.AgentQuestion{}, false, err
	}
	return question, true, nil
}

// relevantTasteCandidate picks the one unproven rule this delivery could be
// evidence for, by scope — the notebook's own primary retrieval signal. A shelf
// with a question already in flight is left alone.
func relevantTasteCandidate(graph *store.Store, node store.Node) (store.Fact, bool, error) {
	rules, err := graph.TasteRules(store.FactCandidate)
	if err != nil || len(rules) == 0 {
		return store.Fact{}, false, err
	}
	asking, err := shelvesBeingAsked(graph)
	if err != nil {
		return store.Fact{}, false, err
	}
	cues := make(map[string]bool)
	for _, cue := range ExtractCues(strings.TrimSpace(node.Provenance.Intent) + "\n" +
		strings.TrimSpace(node.Brief) + "\n" + strings.TrimSpace(node.Summary)) {
		cues[strings.ToLower(cue)] = true
	}
	for _, rule := range rules {
		subject, ok := store.TasteSubject(rule.Scope)
		if !ok || asking[rule.Scope] || !cues[subject] {
			continue
		}
		return rule, true, nil
	}
	return store.Fact{}, false, nil
}

func shelvesBeingAsked(graph *store.Store) (map[string]bool, error) {
	questions, err := graph.UnresolvedQuestions(200)
	if err != nil {
		return nil, err
	}
	asking := make(map[string]bool)
	for _, question := range questions {
		if question.Category != store.QuestionCategoryTaste {
			continue
		}
		for _, option := range question.Options {
			if _, scope, ok := store.DecodeTasteOption(option.Value); ok {
				asking[scope] = true
			}
		}
	}
	return asking, nil
}

// settleTasteLocked is the settle seam's half of the loop: repeat corrections
// become a candidate rule, and the evidence already gathered stands rules up or
// lets them go. Everything it reads is journal-derived, so a rebuilt store
// reaches the same standing.
func (r *Reconciler) settleTasteLocked() {
	if r == nil || r.store == nil {
		return
	}
	r.birthTasteCandidate()
	r.restandTasteRules()
}

// birthTasteCandidate opens at most one shelf per pass. Two corrections of the
// same shape are the bar: one is an instruction, two are a pattern.
func (r *Reconciler) birthTasteCandidate() {
	corrections, err := r.store.CorrectionFacts(tasteCorrectionScan)
	if err != nil {
		return
	}
	open, err := r.store.TasteRules("")
	if err != nil {
		return
	}
	for _, correction := range corrections {
		// A correction that is already one shelf's evidence must not open a
		// second shelf saying the same thing in the user's other words.
		if store.TasteScope(correction.Scope, correction.Body) == "" ||
			coveredByTasteRule(open, correction) {
			continue
		}
		repeats, err := similarCorrections(r.store, correction.Body, correction.Seq)
		if err != nil {
			return
		}
		if len(repeats)+1 < store.TasteRepeatCorrections {
			continue
		}
		if _, err := r.store.RecordTasteCandidate(correction.NodeID, correction.Scope, correction.Body); err != nil {
			return
		}
		return
	}
}

// coveredByTasteRule reports whether an open shelf already stands for this
// correction, by the same similarity the shelf's own evidence is counted with.
func coveredByTasteRule(rules []store.Fact, correction store.Fact) bool {
	tokens := tasteTokens(correction.Body)
	for _, rule := range rules {
		if normalizedTokenSimilarity(tasteTokens(rule.Body), tokens) >= TasteSimilarityFloor {
			return true
		}
	}
	return false
}

// restandTasteRules moves every shelf its evidence has moved. Promotion is the
// shrunk standing clearing its bar; demotion is the user having said twice that
// the delivery was right as it was.
func (r *Reconciler) restandTasteRules() {
	rules, err := r.store.TasteRules("")
	if err != nil {
		return
	}
	for _, rule := range rules {
		standing, err := TasteStandingOf(r.store, rule)
		if err != nil {
			continue
		}
		switch {
		case rule.Status == store.FactCandidate && standing.Confidence >= TastePromotionConfidence:
			if _, err := r.store.PromoteTasteRule(rule.Seq); err == nil {
				r.queueLearningMoment(rule.NodeID, settledTasteMoment(rule))
			}
		case rule.Status == store.FactActive && standing.KeepsSince >= TasteDemotionKeeps:
			if _, err := r.store.DemoteTasteRule(rule.Seq); err == nil {
				r.queueLearningMoment(rule.NodeID, releasedTasteMoment(rule))
			}
		}
	}
}

func settledTasteMoment(rule store.Fact) learningMomentItem {
	return learningMomentItem{headline: "⚖ settled: " + firstLine(rule.Body) + " — I'll hold myself to it"}
}

// releasedTasteMoment is the settled receipt's mirror, said in the vocabulary
// the notebook already uses when a belief stops being one.
func releasedTasteMoment(rule store.Fact) learningMomentItem {
	return letGoMoment(firstLine(rule.Body) + " — you'd rather I didn't")
}
