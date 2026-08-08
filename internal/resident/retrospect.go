// The retrospective is the loop no single job can close: patterns visible
// only across the series — a need that keeps recurring, a correction the user
// keeps making, an approach that consistently works or consistently costs too
// much. Nothing here names any particular kind of job; the content of what is
// learned is entirely emergent from what actually happened.
package resident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
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

// WithCharterProposals lets the resident retrospective surface recurring asks
// through the explicit ratification door. Non-chat embedding paths stay inert.
func (r *Reconciler) WithCharterProposals() *Reconciler {
	r.proposeCharters = true
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
	// Both unprompted proposals ride the one learned cadence: a charter to
	// ratify and a long-running service to question are the same kind of
	// interruption, so a user who declines them is nudged less about both.
	runs, _ := r.store.RetrospectiveRuns()
	cadence := r.store.ProposalCadenceRuns()
	onCadence := cadence <= 1 || runs%cadence == 0
	if r.proposeCharters && onCadence {
		r.proposeRecurringCharter(jobs)
	}
	if onCadence {
		r.proposeServiceHygiene(now)
		r.proposeCharterHygiene(now)
		r.proposeStandingWatchStandDown()
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
		recorded, err := r.store.RecordFactFrom(store.FactWriterDistiller, store.RootID, fact.Scope, fact.Kind, clipFactBody(fact.Body))
		if err == nil && fact.Replaces > 0 {
			_ = r.store.SupersedeFact(fact.Replaces, recorded.Seq)
		}
	}
}

// proposeRecurringCharter turns at most one three-occurrence ask shape into a
// default-declined standing proposal. Recognition is deterministic; the
// ordinary reflection call remains responsible only for notebook learning.
func (r *Reconciler) proposeRecurringCharter(jobs []JobSketch) {
	type recurrence struct {
		count int
		ask   string
	}
	shapes := make(map[string]recurrence)
	for _, job := range jobs {
		if job.Origin != store.OriginUser {
			continue
		}
		shape := recurringAskShape(job.Ask)
		if shape == "" {
			continue
		}
		current := shapes[shape]
		current.count++
		if current.ask == "" {
			current.ask = strings.TrimSpace(job.Ask)
		}
		shapes[shape] = current
	}
	bestShape, best := "", recurrence{}
	for shape, candidate := range shapes {
		if candidate.count < 3 {
			continue
		}
		if candidate.count > best.count || candidate.count == best.count && (bestShape == "" || shape < bestShape) {
			bestShape, best = shape, candidate
		}
	}
	if bestShape == "" {
		return
	}
	charters, err := r.store.Charters()
	if err != nil {
		return
	}
	for _, charter := range charters {
		if charter.ProposalShape != bestShape {
			continue
		}
		// The shape already has a charter, so nothing new is proposed — but a
		// proposal that was minted while nobody was at the surface never got
		// its question, and this is the pass that notices.
		if charter.Status == store.CharterProposed {
			r.askCharterProposal(charter)
		}
		return
	}
	declined, err := r.store.CharterProposalDeclined(bestShape)
	if err != nil || declined {
		return
	}
	sum := sha256.Sum256([]byte(bestShape))
	id := "charter-proposal-" + hex.EncodeToString(sum[:6])
	charter, err := store.NewCharter(
		id,
		best.ask,
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{
			Condition: "Check whether this recurring request is due again",
			Cadence:   24 * time.Hour,
		}},
		"Has the recurring need returned or is the invariant threatened?",
		store.CharterAction{Template: best.ask},
		store.CharterRails{PerFiringBudgetUSD: 0.25, MaxFiringsPerDay: 1},
		store.CharterProposed,
		store.Ratification{},
	)
	if err != nil {
		return
	}
	if err := r.store.CreateCharter(charter.WithProposalShape(bestShape)); err != nil {
		return
	}
	stored, found, err := r.store.Charter(charter.ID)
	if err != nil || !found {
		return
	}
	r.askCharterProposal(stored)
}

// askCharterProposal is the door the proposal never had.
//
// The detector was complete: it noticed three near-identical asks, compiled a
// standing rule for them, and filed it as CharterProposed — where nothing could
// ratify it, the decline was dead code, and DueCharters filters to active, so
// it could never fire either. A standing goal nobody can accept is a note to
// self.
//
// Everything it needs already exists and is used verbatim: the ratification
// option codes the head already decodes, so a plain "yes" ratifies through the
// same path a user-uttered charter takes, and "no" retires the proposal —
// which, for a charter that is still only proposed, is a decline, and the
// decline is what stops the same shape being offered again forever.
//
// Next-natural-moment, never blocking. This is the resident volunteering an
// observation about the user's own habits; it waited three occurrences to say
// it and it can wait for a gap in the conversation. It is asked into whichever
// session is live, and if nobody is there it is not asked at all — the charter
// row is durable and the next retrospective picks the question back up.
func (r *Reconciler) askCharterProposal(charter store.Charter) {
	pending, err := r.store.UnresolvedQuestions(200)
	if err != nil {
		return
	}
	for _, question := range pending {
		if question.OriginCharterID == charter.ID {
			return
		}
	}
	seen, found, err := r.store.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached {
		return
	}
	session := strings.TrimSpace(seen.SessionID)
	if session == "" {
		return
	}

	rails := charter.Rails()
	fires := charter.Watch.Spoken()
	prompt := fmt.Sprintf("This keeps coming back: %s\nI can make it standing — %s, about $%.2f a run, at most %d a day.\nWant me to?",
		clipLabel(firstLine(charter.Invariant), 160), fires,
		rails.PerFiringBudgetUSD, rails.MaxFiringsPerDay)
	options := []store.QuestionOption{
		{Label: "yes, stand this up", Value: "charter:ratify:" + charter.ID},
		{Label: "no, not standing", Value: "charter:retire:" + charter.ID},
	}
	allowFree := true
	// Default to the no. An offer the user did not ask for must not become a
	// standing spend because they pressed enter to get past it.
	text := store.QuestionMessageBody(prompt, options, store.QuestionConfig{
		Kind: store.QuestionChoose, Category: store.QuestionCategoryCharterRatification,
		Default: "2", AllowFree: &allowFree,
	})
	if _, err := r.askQuestionLocked(store.AgentQuestion{
		SessionID: session, Text: text, OriginCharterID: charter.ID,
		Urgency: store.QuestionNextNaturalMoment, Category: store.QuestionCategoryCharterRatification,
		DefaultAnswer: "2", Options: options,
	}); err != nil {
		log.Printf("charter proposal %s: %v", charter.ID, err)
	}
}

const (
	// charterHygieneQuiet is how long a watch must have checked and found
	// nothing before the retrospective wonders aloud whether it is still
	// wanted, and charterHygieneChecks how many of those checks it takes.
	//
	// Both are larger than the service equivalents on purpose. A service up for
	// three days is unusual; a watch that finds nothing for three days is doing
	// its job — most of a standing watch's life is correctly quiet, and asking
	// about that would punish the watch for being the thing the user asked for.
	// Two weeks with a couple of dozen checks and not one firing is a different
	// claim: this watch has had every chance to be useful and has not been.
	charterHygieneQuiet   = 14 * 24 * time.Hour
	charterHygieneChecks  = 20
	charterHygienePerPass = 1
)

// proposeCharterHygiene is the missing third call site of the retrospective's
// own cadence gate. A service up for 72 hours with nobody near it is asked
// "still needed?"; a watch that has checked two hundred mornings and never once
// found anything was never questioned by anybody. Same shape, same discipline:
// once per watch, defaulting to keep, and only when the user has gone quiet, so
// the noticing never arrives in the middle of something.
func (r *Reconciler) proposeCharterHygiene(now time.Time) {
	charters, err := r.store.Charters(store.CharterActive)
	if err != nil {
		return
	}
	asked := 0
	for _, charter := range charters {
		if asked >= charterHygienePerPass {
			return
		}
		if store.IsPracticeCharter(charter) || charter.LastChecked.IsZero() {
			continue
		}
		// The most recent thing that counts as usefulness. A firing is the
		// watch doing its job; for a watch that has never fired at all, the
		// clock starts when it was created.
		fired, err := r.store.CharterLastFired(charter.ID)
		if err != nil {
			continue
		}
		since := charter.CreatedAt
		if fired.After(since) {
			since = fired
		}
		if since.IsZero() || now.Sub(since) < charterHygieneQuiet {
			continue
		}
		checks, err := r.store.CharterCheckCount(charter.ID, since)
		if err != nil || checks < charterHygieneChecks {
			continue
		}
		nudged, err := r.store.CharterHygieneAsked(charter.ID)
		if err != nil || nudged {
			continue
		}
		session := strings.TrimSpace(charter.SessionID)
		if session == "" {
			session = strings.TrimSpace(charter.Ratification.SessionID)
		}
		if session == "" {
			continue
		}
		quiet, err := r.store.SessionQuietSince(session, now.Add(-serviceHygieneQuiet))
		if err != nil || !quiet {
			continue
		}
		if err := r.askCharterHygiene(charter, session, checks); err == nil {
			asked++
		}
	}
}

func (r *Reconciler) askCharterHygiene(charter store.Charter, session string, checks int) error {
	allowFree := true
	options := []store.QuestionOption{
		{Label: "keep", Value: store.CharterHygieneKeepValue(charter.ID)},
		{Label: "stop it", Value: "charter:retire:" + charter.ID},
	}
	prompt := fmt.Sprintf("%s — I've checked %d times and found nothing worth firing on. Still want it?",
		clipLabel(firstLine(charter.Invariant), 120), checks)
	text := store.QuestionMessageBody(prompt, options,
		store.QuestionConfig{Kind: store.QuestionConfirm, Category: store.QuestionCategoryStandingHygiene,
			Default: "1", AllowFree: &allowFree})
	question, err := r.askQuestionLocked(store.AgentQuestion{
		SessionID: session, Text: text, OriginCharterID: charter.ID,
		Urgency: store.QuestionWhenever, Category: store.QuestionCategoryStandingHygiene,
		DefaultAnswer: "1", Options: options,
	})
	if err != nil {
		return err
	}
	_, err = r.store.SurfaceQuestion(question.Seq)
	return err
}

func recurringAskShape(ask string) string {
	var shape strings.Builder
	space, number := false, false
	for _, char := range strings.ToLower(strings.TrimSpace(ask)) {
		switch {
		case unicode.IsDigit(char):
			if !number {
				shape.WriteByte('#')
			}
			number, space = true, false
		case unicode.IsLetter(char):
			if space && shape.Len() > 0 {
				shape.WriteByte(' ')
			}
			shape.WriteRune(char)
			space, number = false, false
		default:
			space, number = true, false
		}
	}
	return strings.TrimSpace(shape.String())
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
