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
	if r.proposeCharters {
		runs, _ := r.store.RetrospectiveRuns()
		cadence := r.store.ProposalCadenceRuns()
		if cadence <= 1 || runs%cadence == 0 {
			r.proposeRecurringCharter(jobs)
		}
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
		if charter.ProposalShape == bestShape {
			return
		}
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
	_ = r.store.CreateCharter(charter.WithProposalShape(bestShape))
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
