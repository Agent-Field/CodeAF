package resident

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// DefaultPracticeIdle is deliberately much longer than the reconciler poll:
	// practice is maintenance, never the next thing after a user's splice.
	DefaultPracticeIdle = 20 * time.Minute

	practicePollInterval      = time.Minute
	practiceMaxFiringsPerDay  = 2
	practiceSurpriseThreshold = 0.50
	practiceScoreThreshold    = 1.50
	practiceSurpriseGrace     = 5 * time.Second
)

// WithPracticeLoop enables curiosity-driven maintenance. A zero dollar carve-
// out disables it. The daily budget is split across the charter's two-firing
// cap; the global daily rail remains an additional admission boundary.
func (r *Reconciler) WithPracticeLoop(dailyBudgetUSD float64, idleFor time.Duration) *Reconciler {
	if dailyBudgetUSD <= 0 {
		r.practiceEnabled = false
		r.practiceBudget = 0
		return r
	}
	if idleFor < 0 {
		idleFor = DefaultPracticeIdle
	}
	r.practiceEnabled = true
	r.practiceBudget = dailyBudgetUSD
	r.practiceIdle = idleFor
	return r
}

type practiceCandidate struct {
	Question store.Fact
	Metric   store.ScopeSurprise
	Score    float64
	Rounds   int
}

func (r *Reconciler) practiceOnceLocked(ctx context.Context) error {
	if !r.practiceEnabled || r.practiceBudget <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.landPracticeOutcomes(); err != nil {
		return err
	}
	if err := r.scanSurpriseQuestions(); err != nil {
		return err
	}
	now := r.now()
	charter, err := r.ensurePracticeCharter(now)
	if err != nil {
		return err
	}
	if charter.Status != store.CharterActive {
		return nil
	}
	due := charter.NextDue.IsZero() || !charter.NextDue.After(now)
	if !due && !charter.WakePending {
		return nil
	}

	idle, err := r.store.UserIdle(now, r.practiceIdle)
	if err != nil {
		return err
	}
	candidates, err := r.practiceCandidates()
	if err != nil {
		return err
	}
	var candidate *practiceCandidate
	if len(candidates) > 0 {
		candidate = &candidates[0]
	}
	if charter.WakePending {
		if selected := questionSeqFromPracticeEvidence(charter.WakeEvidence); selected > 0 {
			candidate = candidateByQuestion(candidates, selected)
		}
		if !idle || candidate == nil {
			if !charter.SentinelYes {
				reason := "no executable question exceeds the practice threshold"
				if !idle {
					reason = "user-origin work is active or the quiet period has not elapsed"
				}
				return r.store.RecordSentinelCheck(charter.ID, store.SentinelCheck{
					WakeSeq: charter.WakeSeq, Yes: false, Line: reason,
				})
			}
			// A checked-yes wake survives a restart or a newly arrived user job.
			// Leave it pending; it will fire only after the idle gate is true again.
			return nil
		}
	} else {
		nextDue, err := store.NextWatchDue(charter.Watch, charter.NextDue)
		if err != nil {
			return err
		}
		state := store.CharterWatchState{NextDue: nextDue}
		if !idle || candidate == nil {
			return r.store.AdvanceCharterWatch(charter.ID, state)
		}
		evidence := fmt.Sprintf("question #%d score %.3f scope %s",
			candidate.Question.Seq, candidate.Score, candidate.Question.Scope)
		wakeSeq, err := r.store.BeginCharterWake(charter.ID, now, evidence, state)
		if err != nil {
			return err
		}
		charter.WakeSeq = wakeSeq
		charter.WakePending = true
		charter.WakeEvidence = evidence
	}
	if !charter.SentinelYes {
		if err := r.store.RecordSentinelCheck(charter.ID, store.SentinelCheck{
			WakeSeq: charter.WakeSeq, Yes: true,
			Line: fmt.Sprintf("idle; question score %.3f exceeds %.3f",
				candidate.Score, practiceScoreThreshold),
		}); err != nil {
			return err
		}
		charter.SentinelYes = true
	}

	subtree, err := r.planPractice(ctx, charter, *candidate)
	if err != nil {
		return err
	}
	_, err = r.store.FirePracticeCharter(charter.ID, charter.WakeSeq, subtree,
		candidate.Question.Seq, candidate.Metric.AverageSurprise,
		candidate.Metric.ExpectedTokens, r.dailyBudgetUSD, now)
	return err
}

func (r *Reconciler) ensurePracticeCharter(now time.Time) (store.Charter, error) {
	local := now.In(time.Local)
	id := "practice-loop-" + local.Format("2006-01-02")
	if existing, found, err := r.store.Charter(id); err != nil {
		return store.Charter{}, err
	} else if found {
		return existing, nil
	}
	expires := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0,
		local.Location()).AddDate(0, 0, 1)
	charter, err := store.NewCharter(
		id,
		"Use idle time to reduce a measured, execution-verifiable knowledge gap",
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{
			Condition: "The system is idle and an executable question exceeds the practice score threshold",
			Cadence:   practicePollInterval,
		}},
		"Deterministically require user idleness, execution verification, and a high-scoring open question",
		store.CharterAction{Template: "Practice the highest-scoring executable knowledge gap"},
		store.CharterRails{
			PerFiringBudgetUSD: r.practiceBudget / float64(practiceMaxFiringsPerDay),
			MaxFiringsPerDay:   practiceMaxFiringsPerDay,
			ExpiresAt:          &expires,
		},
		store.CharterActive,
		store.Ratification{Origin: store.OriginSelf, Evidence: "programmatic practice-loop policy"},
	)
	if err != nil {
		return store.Charter{}, err
	}
	charter = charter.WithProposalShape(store.PracticeCharterShape)
	if err := r.store.CreateCharter(charter); err != nil {
		if existing, found, readErr := r.store.Charter(id); readErr == nil && found {
			return existing, nil
		}
		return store.Charter{}, err
	}
	created, _, err := r.store.Charter(id)
	return created, err
}

func (r *Reconciler) scanSurpriseQuestions() error {
	metrics, err := r.scopeSurprisesLocked(profile.MinSamples)
	if err != nil {
		return err
	}
	for _, metric := range metrics {
		if metric.AverageSurprise < practiceSurpriseThreshold {
			continue
		}
		body := fmt.Sprintf("I didn't know how to predict and verify work in %s (mean residual %.0f%% across %d recent samples)",
			metric.Scope, metric.AverageSurprise*100, metric.Samples)
		if _, err := r.store.RecordQuestion(store.RootID, metric.Scope, body); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) practiceCandidates() ([]practiceCandidate, error) {
	questions, err := r.store.Questions(store.QuestionOpen, 100)
	if err != nil {
		return nil, err
	}
	metrics, err := r.scopeSurprisesLocked(1)
	if err != nil {
		return nil, err
	}
	byScope := make(map[string]store.ScopeSurprise, len(metrics))
	for _, metric := range metrics {
		byScope[metric.Scope] = metric
	}
	candidates := make([]practiceCandidate, 0, len(questions))
	for _, question := range questions {
		metric, ok := byScope[question.Scope]
		if !ok || metric.AverageSurprise < practiceSurpriseThreshold || metric.Allocation <= 0 ||
			!executionVerifiedScope(question.Scope, metric.Evidence) {
			continue
		}
		rounds, err := r.store.QuestionPractices(question.Seq)
		if err != nil {
			return nil, err
		}
		noReduction := 0
		for _, round := range rounds {
			if round.Reduced != nil && !*round.Reduced {
				noReduction++
			}
		}
		relevance := float64(metric.SettledJobs + metric.Territories)
		learningProgress := metric.Allocation / (1 + 0.25*float64(noReduction))
		score := relevance * learningProgress
		if score <= practiceScoreThreshold {
			continue
		}
		candidates = append(candidates, practiceCandidate{Question: question,
			Metric: metric, Score: score, Rounds: noReduction})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Metric.AverageSurprise != candidates[j].Metric.AverageSurprise {
			return candidates[i].Metric.AverageSurprise > candidates[j].Metric.AverageSurprise
		}
		return candidates[i].Question.Seq < candidates[j].Question.Seq
	})
	return candidates, nil
}

func executionVerifiedScope(scope, evidence string) bool {
	combined := strings.ToLower(scope + " " + evidence)
	for _, observational := range []string{
		"email", "e-mail", "mailbox", "inbox", "gmail", "outlook", "calendar",
		"slack", "teams message", "notification", "social feed",
	} {
		if strings.Contains(combined, observational) {
			return false
		}
	}
	for _, tool := range []string{
		"tool:go", "tool:pytest", "tool:npm", "tool:python", "tool:git",
		"tool:ffmpeg", "tool:sh", "tool:curl",
	} {
		if strings.HasPrefix(strings.ToLower(scope), tool) {
			return true
		}
	}
	for _, signal := range []string{
		"go test", "pytest", "npm test", "test suite", "unit test", "integration test",
		"build", "compile", "lint", "benchmark", "execute", "execution", "run.sh",
		"check.sh", "assert", "fixture", "parser", "data transform",
	} {
		if strings.Contains(combined, signal) {
			return true
		}
	}
	return false
}

func (r *Reconciler) planPractice(ctx context.Context, charter store.Charter,
	candidate practiceCandidate) (store.Subtree, error) {
	goal := fmt.Sprintf(`Practice question #%d in %s: %s

Create one small frontier exercise based on the known failure evidence. Write or identify an execution verifier first (build, test, or runnable check), run the exercise, then run the verifier. Keep all results in the journal/workspace; never address or post to the user's thread.`,
		candidate.Question.Seq, candidate.Question.Scope, candidate.Question.Body)
	prefix := firingPrefix(charter.ID, charter.WakeSeq)
	var subtree store.Subtree
	var err error
	if r.plan == nil {
		subtree = store.Subtree{Nodes: []store.NodeSpec{{
			ID: prefix, Brief: goal, Title: "Practice " + candidate.Question.Scope,
			Stage: 1, Group: store.PracticeGroup,
		}}}
	} else {
		planCtx := withPlanAnchor(ctx, PlanAnchor{NodeID: prefix})
		subtree, err = r.plan(planCtx, Compiled{Goal: goal, Scale: "task"})
		if err != nil {
			return store.Subtree{}, err
		}
		for index := range subtree.Nodes {
			subtree.Nodes[index].Group = store.PracticeGroup
		}
	}
	return subtree, nil
}

func (r *Reconciler) landPracticeOutcomes() error {
	rounds, err := r.store.IncompleteQuestionPractices()
	if err != nil || len(rounds) == 0 {
		return err
	}
	usageByJob, err := r.store.TopLevelJobUsage()
	if err != nil {
		return err
	}
	now := r.now()
	for _, round := range rounds {
		node, found, err := r.store.Node(round.JobID)
		if err != nil {
			return err
		}
		if !found || !terminal(node.Status) {
			continue
		}
		usage := usageByJob[round.JobID]
		if usage.Surprise != nil {
			if _, err := r.store.CompleteQuestionPractice(round.JobID, *usage.Surprise); err != nil {
				return err
			}
			continue
		}
		// Profile recording is detached from job completion. Give it one brief
		// reconciliation grace period, then derive the same normalized token
		// residual from journaled usage so every completed practice round lands.
		if node.FinishedAt.IsZero() || now.Sub(node.FinishedAt) < practiceSurpriseGrace ||
			round.ExpectedTokens <= 0 || usage.PromptTokens+usage.CompletionTokens <= 0 {
			continue
		}
		actual := usage.PromptTokens + usage.CompletionTokens
		result := math.Abs(float64(actual-round.ExpectedTokens)) /
			float64(max(round.ExpectedTokens, 1))
		if result > 10 {
			result = 10
		}
		if err := r.store.RecordSurprise(store.NodeSurprise{NodeID: round.JobID,
			ActualTokens: actual, ExpectedTokens: round.ExpectedTokens, Surprise: result}); err != nil {
			// A detached profile writer may have won the unique node row. The next
			// tick will read that journaled value and complete the round.
			continue
		}
		if _, err := r.store.CompleteQuestionPractice(round.JobID, result); err != nil {
			return err
		}
	}
	return nil
}

func questionSeqFromPracticeEvidence(evidence string) int64 {
	var seq int64
	if _, err := fmt.Sscanf(strings.TrimSpace(evidence), "question #%d", &seq); err != nil {
		return 0
	}
	return seq
}

func candidateByQuestion(candidates []practiceCandidate, seq int64) *practiceCandidate {
	for index := range candidates {
		if candidates[index].Question.Seq == seq {
			return &candidates[index]
		}
	}
	return nil
}
