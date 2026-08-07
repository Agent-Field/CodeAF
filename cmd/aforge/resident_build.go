package main

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
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
func newResidentReconciler(settings config.Config, graph *store.Store,
	chatClient, taskClient, planClient *liveClient, plans *jobPlans,
	resolveModel func(head.ModelWords) head.WorkModelChoice) *resident.Reconciler {
	compiler := head.NewCompiler(chatClient)
	if resolveModel != nil {
		compiler = compiler.WithModelResolver(resolveModel)
	}
	return resident.New(graph,
		compileIntent(settings, compiler, taskClient),
		planSubtree(settings, planClient, taskClient, plans, graph),
	).
		WithDistiller(distillFacts(settings, chatClient, graph)).
		WithConsolidator(consolidateFacts(settings, chatClient, graph)).
		WithTitler(titleGoal(settings, chatClient)).
		WithReflector(reflectAcrossJobs(settings, chatClient, graph)).
		WithCharterProposals().
		WithTerritoryDigester(digestTerritory(settings, chatClient)).
		WithWatchEngine(settings.DailyBudgetUSD, checkSentinel(settings, chatClient)).
		WithOverrunPlanner(settings.DailyBudgetUSD, replanRemainder(settings, planClient, taskClient, plans, graph)).
		WithPracticeLoop(settings.PracticeBudgetUSD, settings.PracticeIdle)
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
func compileIntent(settings config.Config, compiler *head.Compiler, taskClient *liveClient) resident.CompileFunc {
	return func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
		augmented := graphContext
		if sk := selfKnowledge(settings, taskClient.Model()); sk != "" {
			augmented += "\n\nMeasured execution costs (this system's own measured history):\n" + sk
		}
		brief, err := compiler.Compile(settings.Context(ctx, "compile"), instruction, augmented)
		if err != nil {
			return resident.Compiled{}, err
		}
		return resident.Compiled{
			Goal:            brief.Goal,
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
		}, nil
	}
}
