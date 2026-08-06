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

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// JobSketch is one settled job as the retrospective sees it: what was asked
// in the user's words, what came back, and how long ago.
type JobSketch struct {
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
		recorded, err := r.store.RecordFact(store.RootID, fact.Scope, fact.Kind, clipFactBody(fact.Body))
		if err == nil && fact.Replaces > 0 {
			_ = r.store.SupersedeFact(fact.Replaces, recorded.Seq)
		}
	}
}

// settledJobSketches renders the newest finished top-level jobs, newest
// first. Folded jobs contribute their digests — the retrospective reads the
// filed history, not the raw archive.
func (r *Reconciler) settledJobSketches(now time.Time) ([]JobSketch, int) {
	nodes, err := r.store.ActiveNodes()
	if err != nil {
		return nil, 0
	}
	usageByJob, err := r.store.TopLevelJobUsage()
	if err != nil {
		return nil, 0
	}
	sketches := make([]JobSketch, 0, reflectionJobLimit)
	settledJobs := 0
	for index := len(nodes) - 1; index >= 0; index-- {
		node := nodes[index]
		if node.Parent != store.RootID {
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
