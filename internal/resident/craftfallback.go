// A provisional craft run is an experiment the user is lent, not one they
// signed up for. craftmind's three-state evidence rule gives a never-run
// workflow exactly one run at the decisive bar, and says so out loud — "first
// time working this way; say 'from scratch' if you'd rather I plan it". The
// second half of that bargain was never built. When the experiment failed, the
// job settled failed, the craft went behind the overwhelming bar, and the
// person's REQUEST died with the experiment: they asked for something, we tried
// a shortcut we had never tried, the shortcut broke, and the answer to their
// question was a stack trace.
//
// This file is the other half. A provisional run that fails replans the same ask
// the ordinary way — compiled and planned, with the shelf never consulted — and
// says so in one line. The evidence still counts against the craft; that part
// already worked and is untouched here.
//
// THREE THINGS THIS MUST NOT BECOME.
//
//  1. A second replan authority. It borrows the overrun path's governors whole:
//     the same lineage arithmetic, the same growJob gate, the same round cap and
//     job ceiling and daily rail. A fallback that spent outside the governors
//     would be the 27-round incident with a new name on it.
//
//  2. A loop. The replacement is planned WITHOUT a craft, so its provenance
//     carries no craft reference, so a second failure cannot reach this code at
//     all. The round cap is the belt behind that brace.
//
//  3. A way to hide real news. A PROVEN craft failing is a fact about a way of
//     working the person relies on, and quietly replanning around it would be the
//     system covering for itself. Only the unproven draft gets rescued.
package resident

import (
	"context"
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// GrowCraftFallback is why the job grew, in the journal's own vocabulary. A
// battery asking "did a learned way of working ever cost a round" is asking for
// exactly this column.
const GrowCraftFallback = "craft-fallback"

// CraftFallbackLine is what the person hears. It names what happened in their
// terms — a way of working, not a workflow file; a first try, not a survival
// record — and it names what is happening instead, in the present tense,
// because by the time this is read the fresh plan is already spliced.
const CraftFallbackLine = "your learned way didn't survive its first try here — planning it fresh"

// craftFallback replans the ask that a provisional craft run failed to answer.
//
// It reports whether a fresh plan actually landed, which is the only thing the
// caller can say out loud: a refused governor, an absent planner and an ask
// nobody recorded all mean the failure is announced exactly as it would have
// been before this existed.
//
// Every refusal here is silent and honest by construction. Nothing is retried,
// nothing is deferred, and no state is written that a later pass would read: the
// failure event fires once, this runs once, and if it does not splice then the
// job is simply as failed as it was.
func (r *Reconciler) craftFallback(ctx context.Context, node store.Node) bool {
	if r == nil || r.store == nil || r.overrunPlan == nil {
		return false
	}
	// Only a whole job. A leaf inside a craft run has the run's own repair
	// rounds above it (CraftRunner.round), and the job root is the only node
	// whose death means the ask went unanswered — which is the same boundary
	// recordCraftOutcome charges the craft at.
	if node.Parent != store.RootID {
		return false
	}
	reference := strings.TrimSpace(node.Provenance.Craft)
	if reference == "" {
		return false
	}
	// The ask in the person's own words. Without it there is nothing to replan
	// FROM: the craft's subtree is the workflow's briefs, not the request, and
	// planning from a step brief would answer a question nobody asked.
	ask := firstLine(node.Provenance.Intent)
	if ask == "" {
		return false
	}
	// Proven or unproven, read the same way recognition reads it: the bare name,
	// so a craft that has ever landed anything under any version counts as
	// proven. A proven craft failing is real news and it travels as a failure.
	//
	// The read is safe after recordCraftOutcome has already charged this run,
	// because a failure only ever increments Against — For, which is the whole
	// of this question, is untouched by the run that is failing now.
	name := reference
	if cut := strings.LastIndex(reference, "@"); cut > 0 {
		name = reference[:cut]
	}
	if r.craftProven(name) {
		return false
	}

	// From here down this is the overrun path's own machinery, called by its own
	// names. The prefix carries the round — nextOverrunPrefix numbers past the
	// highest round that actually spliced, so a refusal never consumes one — and
	// the governor is told that number rather than deriving a second one that
	// could disagree.
	prefix, err := nextOverrunPrefix(r.store, node.ID)
	if err != nil {
		log.Printf("craft fallback %s: %v", node.ID, err)
		return false
	}
	lineage, round := OverrunLineage(prefix)
	request := GrowRequest{
		JobRoot: node.ID, Node: node, Lineage: lineage,
		Reason: GrowCraftFallback, Round: round, DailyBudgetUSD: r.dailyBudgetUSD,
		// Quiet, because the refusal this path can draw is not a handover. Every
		// other grower is adding to work that is still alive and owes its reader
		// "handing over what's done"; here the work is already dead, the reader
		// is about to be told so in one composed sentence, and a governor's
		// notice on top of it would be the machinery narrating its own restraint.
		Quiet: true,
	}
	verdict, err := growJob(ctx, r.store, nil, request)
	if err != nil || !verdict.Allow {
		return false
	}

	// The ordinary planner, and the shelf is not consulted — not suppressed by a
	// flag, simply never asked, which is the only form of suppression that
	// cannot be undone by a later edit to the matcher. This is the same seam the
	// overrun path plans its remainders through: it takes a goal and a prefix,
	// it is the reconciler's own installed planner, and its clamps (flat, no
	// ensemble, a bounded node count) are exactly the right shape for a plan
	// that is spending a failed job's remaining budget.
	planCtx := withPlanAnchor(ctx, PlanAnchor{
		NodeID: node.ID, SessionID: node.Provenance.SessionID,
	})
	subtree, err := r.overrunPlan(planCtx, ask, prefix)
	if err != nil {
		log.Printf("craft fallback %s: %v", node.ID, err)
		return false
	}
	if len(subtree.Nodes) == 0 {
		return false
	}
	// The ceiling twice, as the overrun path reads it: on the way in it could
	// only ask whether there was room for anything at all, and here it asks the
	// exact question now that the plan's size is known. The free caps only — the
	// paid one was asked on the way in and its answer has not changed.
	exact := request
	exact.Adding = len(subtree.Nodes)
	exact.Rechecking = true
	exact.DailyBudgetUSD = 0
	if recheck, err := growJob(ctx, r.store, nil, exact); err == nil && !recheck.Allow {
		return false
	}

	// A new top-level job beside the dead one, which is what the overrun path
	// does with an exhausted job root and for the same delivery law: a subtree
	// parented INTO the settled job would finish correctly and be announced to
	// nobody.
	//
	// It is the user's work and not the machine's second thought — they asked
	// for this and are still owed it — so it is spliced with the origin and the
	// retry link a restart carries, and it inherits the model choices the person
	// made. What it deliberately does NOT inherit is the craft: an empty Craft
	// is the whole non-loop guarantee, because this code only ever runs for a
	// node whose provenance names one.
	provenance := store.Provenance{
		Origin:      store.OriginUser,
		SessionID:   node.Provenance.SessionID,
		Intent:      node.Provenance.Intent,
		RetryOf:     node.ID,
		WorkModel:   node.Provenance.WorkModel,
		PlanModel:   node.Provenance.PlanModel,
		RunModel:    node.Provenance.RunModel,
		Attachments: append([]string(nil), node.Provenance.Attachments...),
	}
	if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
		log.Printf("craft fallback %s: %v", node.ID, err)
		return false
	}
	admitGrowth(r.store, request, verdict, len(subtree.Nodes))
	return true
}

// craftFallbackNext is what the failure row says happens now. It is the caller's
// half of [Failed]: only the code that tried the fallback knows whether one is
// running, and a row that promised a fresh plan that a governor had refused
// would be the single worst sentence this file could produce.
func craftFallbackNext(replanned bool) string {
	if replanned {
		return CraftFallbackLine
	}
	return ""
}
