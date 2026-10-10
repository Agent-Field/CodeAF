// The retrospective is the loop no single job can close: patterns visible
// only across the series — a need that keeps recurring, a correction the user
// keeps making, an approach that consistently works or consistently costs too
// much. Nothing here names any particular kind of job; the content of what is
// learned is entirely emergent from what actually happened.
package resident

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/store"
)

// JobSketch is one settled job as the retrospective sees it: what was asked
// in the user's words, what came back, and how long ago.
type JobSketch struct {
	Origin           store.Origin
	Title            string
	Ask              string
	Outcome          string
	Age              string
	NodeCount        int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	SurpriseTokens   int
	ExpectedTokens   int
	Surprise         *float64
}

// CostSummary renders the structural size and measured spend compactly for
// the reflector prompt.
func (j JobSketch) CostSummary() string {
	nodes := "nodes"
	if j.NodeCount == 1 {
		nodes = "node"
	}
	cost := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", j.Cost), "0"), ".")
	if cost == "" {
		cost = "0"
	}
	summary := fmt.Sprintf("%d %s · %d tok · $%s", j.NodeCount, nodes,
		j.PromptTokens+j.CompletionTokens, cost)
	if j.Surprise == nil {
		return summary
	}
	comparison := fmt.Sprintf("typical leaf miss ±%.0f%%", 100**j.Surprise)
	switch {
	case j.ExpectedTokens > 0 && j.SurpriseTokens > j.ExpectedTokens:
		comparison = fmt.Sprintf("%.1f× over", float64(j.SurpriseTokens)/float64(j.ExpectedTokens))
	case j.ExpectedTokens > 0 && j.SurpriseTokens < j.ExpectedTokens && j.SurpriseTokens > 0:
		comparison = fmt.Sprintf("%.1f× under", float64(j.ExpectedTokens)/float64(j.SurpriseTokens))
	case j.ExpectedTokens > 0 && j.SurpriseTokens == j.ExpectedTokens:
		comparison = "on prediction"
	}
	return fmt.Sprintf("%s, predicted %d tok — %s", summary, j.ExpectedTokens, comparison)
}

// ReflectFunc looks across recent jobs for what only the series reveals and
// returns it as notebook memories. An empty return is the common, correct
// answer.
type ReflectFunc func(ctx context.Context, jobs []JobSketch) ([]Learned, error)

// WithReflector enables the periodic retrospective.
func (r *Reconciler) WithReflector(reflect ReflectFunc) *Reconciler {
	r.reflect = reflect
	return r
}

const (
	// reflectionInterval paces the retrospective; reflectionMinJobs and
	// reflectionMinNew gate it on having enough history to hold a pattern
	// and something new since last time. Sketch bounds keep one reflection
	// call cheap regardless of how much history exists.
	reflectionInterval  = 30 * time.Minute
	reflectionMinJobs   = 3
	reflectionMinNew    = 1
	reflectionJobLimit  = 12
	reflectionAskBytes  = 300
	reflectionOutBytes  = 400
	reflectionFactLimit = 4
)

// reflectOnJobs runs at most once per interval, over the newest settled
// top-level jobs. Best effort throughout: a failure leaves ordinary
// reconciliation untouched and the next interval tries again.
func (r *Reconciler) reflectOnJobs(ctx context.Context) {
	if r.reflect == nil {
		return
	}
	now := r.now()
	watermark, reflected, err := r.store.RetrospectiveWatermark()
	if err != nil {
		return
	}
	if reflected && now.Sub(watermark.At) < reflectionInterval {
		return
	}
	jobs, settledJobs := r.settledJobSketches(now)
	if settledJobs < reflectionMinJobs || reflected && settledJobs < watermark.SettledJobs+reflectionMinNew {
		return
	}
	if _, err := r.store.CheckpointRetrospective(settledJobs); err != nil {
		return
	}
	_, _ = r.store.ProjectTraits(now)
	_ = r.metaRetrospect()
	r.maintainTerritories(ctx, now)
	// The unprompted proposal — a long-running service to question — rides the
	// learned proposal cadence, so it is an interruption paced like one. It had
	// company once: a recurring ask offered as a standing rule, a watch that
	// never found anything questioned, and the offer to stop watching with no
	// terminal open. All three went with the v1 scheduler.
	runs, _ := r.store.RetrospectiveRuns()
	cadence := r.store.ProposalCadenceRuns()
	if cadence <= 1 || runs%cadence == 0 {
		r.proposeServiceHygiene(now)
	}

	learned, err := r.reflect(ctx, jobs)
	if err != nil {
		return
	}
	if len(learned) > reflectionFactLimit {
		learned = learned[:reflectionFactLimit]
	}
	for _, fact := range learned {
		if fact.Skill != nil || fact.Kind == store.FactSkill {
			continue
		}
		if strings.TrimSpace(fact.Body) == "" {
			continue
		}
		if r.alreadyLearned(fact) {
			continue
		}
		recorded, err := r.store.RecordFactFrom(store.FactWriterDistiller, store.RootID, fact.Scope, fact.Kind, clipFactBody(fact.Body))
		if err == nil && fact.Replaces > 0 {
			_ = r.store.SupersedeFact(fact.Replaces, recorded.Seq)
		}
	}
}

// alreadyLearned is the distiller being told it has said this before.
//
// The store already refuses an exact repeat, and exact is not how a model
// repeats itself. Measured on the user's own notebook, ONE lesson — that a
// delivered message must carry the finished thing written out rather than a
// pointer to where it was filed — was learned five separate times, each wording
// a little different from the last, each superseding the one before it. The
// chain is what proves the lesson was never the missing piece: the same failure
// kept happening for reasons a notebook line cannot fix, and every pass paid a
// write to say the same sentence again.
//
// So the comparison is against the LINEAGE, not the active view. The active view
// shows one line where the notebook actually holds five, so a writer reading it
// cannot tell a genuinely new belief from the sixth copy of an old one. A near
// duplicate is dropped silently: nothing is lost, because the belief it repeats
// is already standing and already retrieved.
//
// It is deliberately narrow. Only the standing user-scope beliefs — the ones
// that come back as advice on every job — are guarded, because that is where
// repetition compounds; a per-repository or per-tool line is cheap and local,
// and refusing one because it rhymes with an old one would cost real knowledge.
func (r *Reconciler) alreadyLearned(fact Learned) bool {
	scope := strings.TrimSpace(fact.Scope)
	if r == nil || r.store == nil || scope == "" || fact.Unsettled != nil {
		return false
	}
	if scope != "user" && !strings.HasPrefix(scope, "user:") {
		return false
	}
	known, err := r.store.FactLineage(scope, learnedLineageLimit)
	if err != nil {
		return false
	}
	candidate := learnedWords(fact.Body)
	if len(candidate) < learnedMinWords {
		return false
	}
	for _, prior := range known {
		if prior.Kind != fact.Kind {
			continue
		}
		if sameLesson(candidate, learnedWords(prior.Body)) {
			return true
		}
	}
	return false
}

const (
	// learnedLineageLimit bounds the read. A scope with more standing beliefs
	// than this has a different problem than repetition.
	learnedLineageLimit = 200
	// learnedMinWords is the shortest line worth comparing. Two four-word
	// preferences that share three words are not the same belief, they are two
	// short sentences in one language.
	learnedMinWords = 6
	// learnedOverlap is how much of the shorter line has to be in the longer one
	// before they are the same lesson said twice, and it sits where it does
	// because the two populations do not overlap. Measured over the five real
	// wordings of the relearned lesson and three unrelated user preferences:
	// every same-lesson pair scores 0.42 to 0.87, and every unrelated pair
	// scores 0.00 to 0.14. The line goes in the empty band between them, nearer
	// the top of it, because the error it prevents costs a duplicate row and the
	// error it would cause by firing wrongly costs a belief nobody learns.
	learnedOverlap = 0.5
)

// sameLesson reports whether two beliefs are the same thing said differently.
// The measure is how much of the SHORTER line the longer one already contains,
// rather than a symmetric one: a re-derivation is usually the same sentence with
// a clause added or dropped, and a symmetric score reads that as two beliefs.
func sameLesson(candidate, prior map[string]bool) bool {
	if len(candidate) < learnedMinWords || len(prior) < learnedMinWords {
		return false
	}
	shared := 0
	for word := range candidate {
		if prior[word] {
			shared++
		}
	}
	shorter := len(candidate)
	if len(prior) < shorter {
		shorter = len(prior)
	}
	return float64(shared)/float64(shorter) >= learnedOverlap
}

// learnedWords is a belief as a bag of the words that carry its meaning:
// lowercased, punctuation dropped, and the grammar words that every English
// sentence shares left out, so two wordings of one lesson meet.
func learnedWords(body string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(body), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if learnedGrammar[word] || len(word) < 3 {
			continue
		}
		// The smallest reduction that makes two wordings of one sentence meet:
		// plurals and the gerund. It is not a linguistic claim.
		switch {
		case len(word) > 5 && strings.HasSuffix(word, "ing"):
			word = word[:len(word)-3]
		case len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss"):
			word = word[:len(word)-1]
		}
		words[word] = true
	}
	return words
}

// learnedGrammar are the words a sentence needs and a belief does not.
var learnedGrammar = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "this": true, "with": true,
	"from": true, "into": true, "not": true, "but": true, "are": true, "was": true,
	"were": true, "been": true, "has": true, "have": true, "had": true, "its": true,
	"their": true, "them": true, "they": true, "you": true, "your": true, "his": true,
	"her": true, "our": true, "who": true, "whom": true, "which": true, "when": true,
	"where": true, "what": true, "than": true, "then": true, "there": true, "here": true,
	"all": true, "any": true, "some": true, "each": true, "such": true, "only": true,
	"also": true, "just": true, "over": true, "under": true, "about": true, "after": true,
	"before": true, "because": true, "should": true, "would": true, "could": true,
	"will": true, "can": true, "may": true, "must": true, "does": true, "did": true,
	"how": true, "why": true, "one": true, "two": true, "very": true, "more": true,
	"most": true, "much": true, "many": true, "other": true, "same": true, "own": true,
}

// settledJobSketches renders the newest finished top-level jobs, newest
// first. Folded jobs contribute their digests — the retrospective reads the
// filed history, not the raw archive.
func (r *Reconciler) settledJobSketches(now time.Time) ([]JobSketch, int) {
	nodes, err := r.store.Nodes()
	if err != nil {
		return nil, 0
	}
	usageByJob, err := r.store.TopLevelJobUsage()
	if err != nil {
		return nil, 0
	}
	territories := make(map[string]bool)
	for _, node := range nodes {
		if store.IsOrganizationalGroup(node.Group) {
			territories[node.ID] = true
		}
	}
	sketches := make([]JobSketch, 0, reflectionJobLimit)
	settledJobs := 0
	for index := len(nodes) - 1; index >= 0; index-- {
		node := nodes[index]
		if store.IsOrganizationalGroup(node.Group) ||
			node.Parent != store.RootID && !territories[node.Parent] {
			continue
		}
		settled := node.FoldRoot || node.Status == store.Done || node.Status == store.Failed || node.Status == store.Cancelled
		if !settled {
			continue
		}
		settledJobs++
		if len(sketches) == reflectionJobLimit {
			continue
		}
		outcome := strings.TrimSpace(node.Summary)
		if outcome == "" {
			outcome = strings.TrimSpace(node.FoldDigest)
		}
		if outcome == "" {
			outcome = strings.TrimSpace(node.Error)
		}
		usage := usageByJob[node.ID]
		sketches = append(sketches, JobSketch{
			Origin:           node.Provenance.Origin,
			Title:            strings.TrimSpace(node.Title),
			Ask:              clipLabel(node.Provenance.Intent, reflectionAskBytes),
			Outcome:          clipLabel(outcome, reflectionOutBytes),
			Age:              store.AgeLabel(node.FinishedAt, now),
			NodeCount:        usage.NodeCount,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			Cost:             usage.Cost,
			SurpriseTokens:   usage.SurpriseTokens,
			ExpectedTokens:   usage.ExpectedTokens,
			Surprise:         usage.Surprise,
		})
	}
	return sketches, settledJobs
}
