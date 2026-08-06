// The retrospective is the loop no single job can close: patterns visible
// only across the series — a need that keeps recurring, a correction the user
// keeps making, an approach that consistently works or consistently costs too
// much. Nothing here names any particular kind of job; the content of what is
// learned is entirely emergent from what actually happened.
package resident

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// JobSketch is one settled job as the retrospective sees it: what was asked
// in the user's words, what came back, and how long ago.
type JobSketch struct {
	Title   string
	Ask     string
	Outcome string
	Age     string
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
	now := time.Now()
	if !r.lastReflection.IsZero() && now.Sub(r.lastReflection) < reflectionInterval {
		return
	}
	jobs := r.settledJobSketches(now)
	if len(jobs) < reflectionMinJobs || len(jobs) < r.lastReflectedJobs+reflectionMinNew {
		return
	}
	r.lastReflection = now
	r.lastReflectedJobs = len(jobs)

	learned, err := r.reflect(ctx, jobs)
	if err != nil {
		return
	}
	if len(learned) > reflectionFactLimit {
		learned = learned[:reflectionFactLimit]
	}
	for _, fact := range learned {
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
func (r *Reconciler) settledJobSketches(now time.Time) []JobSketch {
	nodes, err := r.store.ActiveNodes()
	if err != nil {
		return nil
	}
	sketches := make([]JobSketch, 0, reflectionJobLimit)
	for index := len(nodes) - 1; index >= 0 && len(sketches) < reflectionJobLimit; index-- {
		node := nodes[index]
		if node.Parent != store.RootID {
			continue
		}
		settled := node.FoldRoot || node.Status == store.Done || node.Status == store.Failed || node.Status == store.Cancelled
		if !settled {
			continue
		}
		outcome := strings.TrimSpace(node.Summary)
		if outcome == "" {
			outcome = strings.TrimSpace(node.FoldDigest)
		}
		if outcome == "" {
			outcome = strings.TrimSpace(node.Error)
		}
		sketches = append(sketches, JobSketch{
			Title:   strings.TrimSpace(node.Title),
			Ask:     clipLabel(node.Provenance.Intent, reflectionAskBytes),
			Outcome: clipLabel(outcome, reflectionOutBytes),
			Age:     store.AgeLabel(node.FinishedAt, now),
		})
	}
	return sketches
}
