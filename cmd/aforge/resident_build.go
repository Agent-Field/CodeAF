package main

import (
	"context"
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// newResidentReconciler assembles the store-driven resident role shared by an
// interactive chat owner and a bounded wake pass. Surface concerns are added
// by chat after this returns; wake deliberately has no session-open or arrival
// brief side effects.
// The three clients divide by the kind of call: chatClient talks (compiling,
// distilling, titling, narrating — the resident's own small verdicts),
// planClient structures (the task graph, replans, contracts, the delivery
// gate), and taskClient executes leaves. By default plan follows work, so the
// split is dormant until a plan model is chosen.
// oneShotErrand says this reconciler serves `aforge do`: one errand, run once,
// with nobody who could answer a question about it. It is a fact about the
// surface, not a judgement about the work, and it travels to both halves that
// would otherwise have to guess it — the compiler's temporal classification and
// the reconciler's charter draft.
//
// It is the only signal the verbatim law needs, and it is the right one: the
// compile stage is unchanged for everybody, and the reconciler that applies the
// result is the one thing that already knows which surface asked. On this
// surface the submitted sentence survives the compile as the goal
// (resident.keepTheAskVerbatim); on every other one the compiler's reading and
// its declared assumptions are the point, and nothing here moves.
func newResidentReconciler(settings config.Config, graph *store.Store,
	chatClient, taskClient, planClient *liveClient, plans *jobPlans, terrainRoot string,
	resolveModel func(head.ModelWords) head.WorkModelChoice, oneShotErrand bool) *resident.Reconciler {
	compiler := head.NewCompiler(chatClient)
	if resolveModel != nil {
		compiler = compiler.WithModelResolver(resolveModel)
	}
	if oneShotErrand {
		compiler = compiler.WithOneShotErrands()
	}
	reconciler := resident.New(graph,
		compileIntent(settings, compiler, taskClient, planClient, plans, graph),
		planSubtree(settings, planClient, taskClient, plans, graph, terrainRoot),
	)
	if oneShotErrand {
		reconciler = reconciler.WithOneShotErrands()
	}
	return reconciler.
		// The two slots, asked at the moment a job is admitted rather than read
		// from the environment: what a run was launched with is not what it is
		// running on after a picker change, and a receipt that quotes the env var
		// is a receipt about the wrong process.
		WithModelsInForce(func() (string, string) {
			plan, work := "", ""
			if planClient != nil {
				plan = planClient.Model()
			}
			if taskClient != nil {
				work = taskClient.Model()
			}
			return plan, work
		}).
		WithDistiller(distillFacts(settings, chatClient, graph)).
		WithConsolidator(consolidateFacts(settings, chatClient, graph)).
		WithTitler(titleGoal(settings, chatClient)).
		WithReflector(reflectAcrossJobs(settings, chatClient, graph)).
		WithCharterProposals().
		WithTerritoryDigester(digestTerritory(settings, chatClient)).
		WithWatchEngine(settings.DailyBudgetUSD, checkSentinel(settings, chatClient)).
		WithOverrunPlanner(settings.DailyBudgetUSD, replanRemainder(settings, planClient, taskClient, plans, graph, terrainRoot)).
		WithCancelRethink(rethinkAfterCancel(settings, planClient, plans, graph)).
		WithPracticeLoop(settings.PracticeBudgetUSD, settings.PracticeIdle)
}

// rethinkAfterCancel is the settle watcher's half of the cancel journey: work
// the user withdrew before any worker started it. There is no leaf wrapper to
// speak for it — nothing ran — so the reconciler names the top of the cancelled
// subtree and this resolves it back to the plan document it belongs to. A job
// with no retained plan (a single spliced leaf, a rehydration that failed) has
// no remainder to reconsider and this quietly does nothing, which is the same
// answer the sentinel would have given more expensively.
func rethinkAfterCancel(settings config.Config, planClient *liveClient,
	plans *jobPlans, graph *store.Store) resident.CancelRethinkFunc {
	return func(ctx context.Context, node store.Node, reason string) {
		prefix, planGraph, _, workModel, _ := plans.lookup(node.ID)
		if planGraph == nil {
			return
		}
		plans.reviseAfterCancel(ctx, settings, planClient, graph, node, prefix, planGraph,
			node.Summary, reason, workModel)
	}
}

// compileIntent turns one user instruction into a compiled goal.
//
// The cache key is "compile" rather than the instruction, and that is the whole
// reason this is a named function. A cache key is an affinity handle: it asks
// the endpoint to send every call in one lineage back to the instance already
// holding that prefix. Compilation has no lineage — it is a single call — so
// keying it on the user's words minted a fresh key for every message and
// scattered compiles across providers, and the 5.5 KB compiler prompt was
// written cold every single time. One constant key keeps it warm from compile
// to compile, exactly as "distill", "gate", "narrate" and the rest already do.
// The standing-charter compiler runs on this same context and inherits it.
//
// plans is here for one reason and it is not planning: the compiler's structural
// reading of the ask — the thing scale is reconciled from — has no seat on
// resident.Compiled, because nothing between the compile and the splice acts on
// it. The journal wants it anyway, so it is left as a memo keyed on the goal
// both halves share. A nil registry simply drops it.
//
// planClient is here for the acceptance checklist and nothing else. THIS IS THE
// ONE PASS IN THE SYSTEM THAT HOLDS THE REQUEST VERBATIM: everything downstream
// holds the compiled goal, which is this program's reading of the ask. A
// checklist of what the person asked for has to be read off the person's own
// words or it is a standard we wrote for ourselves, and the gate may not hold
// anybody to one of those (docs/design/gate/ACCEPTANCE.md, and the grounding
// invariant in internal/revision/grounding.go that says the same thing about a
// review's findings).
func compileIntent(settings config.Config, compiler *head.Compiler, taskClient, planClient *liveClient,
	plans *jobPlans, graph *store.Store) resident.CompileFunc {
	return func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
		// A compile that had to be repaired to be read says so, against the job
		// root — the compile runs before the job has an id of its own, and the
		// run that lost four minutes to a silent stream lost them here and in the
		// planning pass behind it. See withRepairJournal.
		ctx = withRepairJournal(ctx, graph, store.RootID)
		augmented := graphContext
		if sk := selfKnowledge(settings, taskClient.Model()); sk != "" {
			augmented += "\n\nMeasured execution costs (this system's own measured history):\n" + sk
		}
		brief, err := compiler.Compile(settings.Context(ctx, "compile"), instruction, augmented)
		if err != nil {
			return resident.Compiled{}, err
		}
		if plans != nil {
			plans.noteReading(brief.Goal, brief.Structure)
		}
		return resident.Compiled{
			Accept:          acceptanceChecklist(ctx, settings, planClient, plans, graph, instruction),
			Constraints:     brief.Constraints,
			Goal:            brief.Goal,
			Title:           brief.Title,
			Contract:        brief.Contract,
			Parts:           brief.Parts,
			Assumptions:     brief.Assumptions,
			Scale:           brief.Scale,
			TrialOf:         brief.TrialOf,
			BuildsOn:        brief.BuildsOn,
			Question:        brief.Question,
			QuestionOptions: brief.QuestionOptions,
			Charter:         brief.Charter,
			ServiceIntent:   brief.ServiceIntent,
			WorkModel:       brief.WorkModel,
			ModelNote:       brief.ModelNote,
			Note:            brief.Note,
		}, nil
	}
}

// acceptanceChecklist reads the person's request for the behaviours it states.
//
// It is one call on the request alone, and everything about it is fail-quiet: no
// client, no request, a call that fails or a request that states nothing
// checkable all return an empty checklist, and every reader downstream of an
// empty checklist behaves exactly as it did before this existed. A CAPABILITY
// THAT CANNOT WORK IS ABSENT, NOT BROKEN.
//
// The spend is billed through journalPlanSpend like every other planning pass,
// against the spine, which is where the compile's own row lands. It used to be
// dropped: the plan client's snapshot is the router behind a wall and NOT
// behind the pool's usage journal, so nothing wrote this call's row, and a
// comment here claimed the compile's context billed it. Measured 2026-09-02: a
// run printed $0.0041 against $0.0076 in its call log, and the one row the
// usage table lacked was this call (#380). A ledger a call can miss is not the
// one ledger, and the receipt was honest about a bill that was short.
func acceptanceChecklist(ctx context.Context, settings config.Config, planClient *liveClient,
	plans *jobPlans, graph *store.Store, request string) []plan.Point {
	if planClient == nil || strings.TrimSpace(request) == "" {
		return nil
	}
	_, structuring := planClient.Snapshot()
	if structuring == nil {
		return nil
	}
	points, usage, err := plan.Acceptance(settings.Context(ctx, "compile"), structuring, request)
	journalPlanSpend(graph, plans, planClient, "", usage)
	if err != nil {
		log.Printf("note: could not read what the request asks for: %v", err)
		return nil
	}
	return points
}
