// This file ports src/session/plandb-scheduler.ts:2471-4462 from swe-pro
// (commit 3b25a1a), excluding the child/self merge queue in merge.go.
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/project"
	"github.com/Agent-Field/swe-pro-go/internal/session/adaptiveflag"
	"github.com/Agent-Field/swe-pro-go/internal/session/contextpolicy"
	"github.com/Agent-Field/swe-pro-go/internal/session/contextrefs"
	"github.com/Agent-Field/swe-pro-go/internal/session/factsheet"
	"github.com/Agent-Field/swe-pro-go/internal/session/failuretriage"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafbriefing"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafdigest"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
	"github.com/Agent-Field/swe-pro-go/internal/session/loopguard"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergecoordinator"
	"github.com/Agent-Field/swe-pro-go/internal/session/outcomecache"
	"github.com/Agent-Field/swe-pro-go/internal/session/policyline"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

const rootIssueExcerptLimit = 2000
const denseSpecClauseThreshold = 5

var adaptiveCutsAtPackageLoad = adaptiveflag.AdaptiveCutsEnabled()

type realSchedulerClock struct{}

func (realSchedulerClock) Now() time.Time { return time.Now() }

func (realSchedulerClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SchedulerOptions wires the services that are still Effect services in the
// TypeScript source. PlanDB and process execution keep their real defaults.
type SchedulerOptions struct {
	Workspace        string
	Agents           AgentRegistry
	StepLoop         StepLoopClient
	Gate             ReviewGate
	Replanner        Replanner
	Planner          PlannerTranslator
	Pools            ModelPoolResolver
	Provider         ProviderResolver
	Capability       CapabilityReader
	Briefing         LeafBriefingBuilder
	OutcomeObserver  OutcomeObserver
	MergeStack       MergeStack
	DefaultMerge     MergeStackOptions
	MergeCoordinator *mergecoordinator.MergeCoordinator
	Clock            SchedulerClock
	AdaptiveCuts     *bool
	OutcomeCache     *bool
	FrontierEnabled  *bool
	LoopGuard        LoopGuardFactory
	Clauses          ClauseJudger

	PreserveRejectedWork func(leafoutcome.PreserveRejectedWorkArgs)
	EmitLeafOutcome      func(leafoutcome.EmitLeafOutcomeArgs)
}

// Scheduler owns injected services; all process-global source state remains
// in package-level mutexed registries.
type Scheduler struct {
	workspace        string
	agents           AgentRegistry
	stepLoop         StepLoopClient
	gate             ReviewGate
	replanner        Replanner
	planner          PlannerTranslator
	pools            ModelPoolResolver
	provider         ProviderResolver
	capability       CapabilityReader
	briefing         LeafBriefingBuilder
	outcomeObserver  OutcomeObserver
	mergeStack       MergeStack
	mergeCoordinator *mergecoordinator.MergeCoordinator
	clock            SchedulerClock
	adaptiveCuts     bool
	outcomeCache     bool
	frontier         bool
	loopGuard        LoopGuardFactory
	clauses          ClauseJudger
	planDB           planDBRunner
	runner           commandRunner
	envelopeReader   diskEnvelopeReader
	preserve         func(leafoutcome.PreserveRejectedWorkArgs)
	emit             func(leafoutcome.EmitLeafOutcomeArgs)
	quietMu          sync.RWMutex
	lastQuietReason  CycleQuietReason
}

// NewScheduler builds a phase-two scheduler. Gate, planner, and replanner are
// optional seams; a nil gate degrades to status=skipped and still performs the
// merge, matching a gate that explicitly elects not to review.
func NewScheduler(options SchedulerOptions) *Scheduler {
	adaptive := adaptiveCutsAtPackageLoad
	if options.AdaptiveCuts != nil {
		adaptive = *options.AdaptiveCuts
	}
	cacheEnabled := os.Getenv("CODEAF_OUTCOME_CACHE") != "0"
	if options.OutcomeCache != nil {
		cacheEnabled = *options.OutcomeCache
	}
	frontier := computeFrontierEnabled(adaptive, os.Getenv("CODEAF_FRONTIER"), os.Getenv("CODEAF_LARGE_BAND") == "1")
	if options.FrontierEnabled != nil {
		frontier = *options.FrontierEnabled
	}
	clock := options.Clock
	if clock == nil {
		clock = realSchedulerClock{}
	}
	coordinator := options.MergeCoordinator
	if coordinator == nil {
		coordinator = processMergeCoordinator
	}
	stack := options.MergeStack
	if stack == nil {
		stack = newDefaultMergeStack(options.DefaultMerge)
	}
	guardFactory := options.LoopGuard
	if guardFactory == nil {
		guardFactory = loopguard.CreateLoopGuard
	}
	preserve := options.PreserveRejectedWork
	if preserve == nil {
		preserve = leafoutcome.PreserveRejectedWork
	}
	emit := options.EmitLeafOutcome
	if emit == nil {
		emit = leafoutcome.EmitLeafOutcome
	}
	return &Scheduler{
		workspace: options.Workspace, agents: options.Agents, stepLoop: options.StepLoop,
		gate: options.Gate, replanner: options.Replanner, planner: options.Planner,
		pools: options.Pools, provider: options.Provider, capability: options.Capability,
		briefing:        options.Briefing,
		outcomeObserver: options.OutcomeObserver, mergeStack: stack,
		mergeCoordinator: coordinator, clock: clock, adaptiveCuts: adaptive,
		outcomeCache: cacheEnabled, frontier: frontier, loopGuard: guardFactory,
		clauses: options.Clauses,
		planDB:  nativePlanDBRunner{}, runner: osCommandRunner{},
		preserve: preserve, emit: emit,
	}
}

func computeFrontierEnabled(adaptive bool, flag string, largeBand bool) bool {
	if !adaptive {
		return false
	}
	if flag == "0" {
		return false
	}
	if flag == "1" {
		return true
	}
	return largeBand
}

type failCascadeResult struct {
	Failed    string
	Cancelled []string
	Softened  []string
}

// failTaskCascade ports plandb-scheduler.ts:708-786. The cwd and dbPath
// arguments remain ceremonial because the in-process PlanDB bridge ignores
// both (LB-20).
func failTaskCascade(runner planDBRunner, workspace, dbPath, taskID, reason string) failCascadeResult {
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	_, _ = workspace, dbPath
	runner.Run([]string{
		"plandb", "task", "fail", taskID, "--error", compactJS(reason, 400), "--json",
	})
	unlocks := runner.Run([]string{"plandb", "what-unlocks", taskID, "--json"})
	var successors []*plandb.Task
	_ = json.Unmarshal(unlocks.Stdout, &successors)
	result := failCascadeResult{
		Failed: taskID, Cancelled: []string{}, Softened: []string{},
	}
	hard := os.Getenv("CODEAF_CASCADE_HARD") == "1"
	for _, successor := range successors {
		if successor == nil ||
			(successor.Status != plandb.StatusPending && successor.Status != plandb.StatusReady) {
			continue
		}
		if hard {
			cancelled := runner.Run([]string{
				"plandb", "task", "cancel", successor.ID,
				"--reason", "predecessor " + taskID + " failed", "--json",
			})
			if cancelled.Code == 0 {
				result.Cancelled = append(result.Cancelled, successor.ID)
			}
			continue
		}
		softenedStatus := string(successor.Status)
		if successor.Status == plandb.StatusReady {
			reopened := runner.Run([]string{"plandb", "task", "reopen", successor.ID})
			if reopened.Code == 0 {
				softenedStatus = "pending"
			}
		}
		body := "Predecessor " + taskID + " failed (" + compactJS(reason, 160) +
			"). Softened to " + softenedStatus +
			" (not hard-cancelled) so a later replan/resume can revive this dependent."
		added := runner.Run([]string{
			"plandb", "context", body, "--kind", "blocked", "--task", successor.ID, "--json",
		})
		if added.Code == 0 {
			result.Softened = append(result.Softened, successor.ID)
		}
	}
	return result
}

type dispatchItem struct {
	task          *plandb.Task
	agentID       string
	modelOverride *capabilityModelRef
}

// capabilityModelRef avoids a second exported override type while keeping the
// dispatch item independent of phase-one ModelAssignment storage.
type capabilityModelRef struct {
	ProviderID string
	ModelID    string
}

func (s *Scheduler) dispatchOne(ctx context.Context, input SchedulerInput, item dispatchItem, aimd *lockedAimd) (result DispatchResult) {
	task := item.task
	agentID := item.agentID
	if task == nil {
		errText := "scheduler dispatch error: nil task"
		return DispatchResult{AgentID: agentID, Success: false, Error: &errText, SubagentType: "fixer"}
	}
	start := s.clock.Now()
	subagentType := suggestedAgent(task)
	defer func() {
		if recovered := recover(); recovered != nil {
			message := fmt.Sprint(recovered)
			schedulerLog.Error("dispatch failed", map[string]any{"taskID": task.ID, "error": compactJS(message, 500)})
			failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, "scheduler dispatch error: "+message)
			message = compactJS(message, 500)
			result = DispatchResult{
				TaskID: task.ID, SubagentType: subagentType, AgentID: agentID,
				Success: false, Error: &message,
			}
		}
	}()

	schedulerLog.Info("scheduler dispatched", map[string]any{"taskID": task.ID})
	fileScope := policyline.ParseFileScope(policyline.PolicyLine(task.Description, "file_scope"))
	currentBranch := s.detectCurrentBranch(ctx)
	isDirectChild := task.ParentTaskID != nil && *task.ParentTaskID == input.RootTaskID
	parentID := ""
	if task.ParentTaskID != nil {
		parentID = *task.ParentTaskID
	}
	parentBranch := currentBranch
	parentWorktree := s.workspace
	if !isDirectChild {
		parentBranch = "plandb/" + parentID
		parentWorktree = filepath.Join(s.workspace, ".plandb", "wt-"+parentID)
	}
	baseRef, mergeAt, targetBranch := parentBranch, parentWorktree, parentBranch
	if !isDirectChild {
		ref := s.runner.Run(ctx, []string{"git", "rev-parse", "--verify", parentBranch}, runOptions{Cwd: s.workspace})
		_, statErr := os.Stat(parentWorktree)
		if ref.Code != 0 || statErr != nil {
			schedulerLog.Info("hierarchical fallback: parent unavailable, branching flat from main", map[string]any{
				"taskID": task.ID, "parentTaskID": parentID, "parentBranch": parentBranch,
			})
			baseRef, mergeAt, targetBranch = currentBranch, s.workspace, currentBranch
		}
	}

	worktreePolicy := policyline.PolicyLine(task.Description, "worktree")
	skipWorktree := worktreePolicy != nil && strings.ToLower(*worktreePolicy) == "none"
	worktree := &WorktreeAllocation{
		Path: s.workspace, BaseSHA: "", BaseRef: baseRef, Backend: WorktreeBackendGit,
	}
	if !skipWorktree {
		worktree = AllocateWorktree(ctx, s.workspace, task.ID, baseRef, AllocateWorktreeOptions{runner: s.runner})
	}
	if worktree == nil {
		reason := "worktree allocation failed"
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, reason)
		return failedDispatch(task.ID, subagentType, agentID, reason)
	}

	agent, agentErr := s.resolveAgent(ctx, subagentType)
	if agentErr != nil {
		panic(agentErr)
	}
	if agent == nil {
		reason := "unknown subagent type: " + subagentType
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, reason)
		return failedDispatch(task.ID, subagentType, agentID, reason)
	}
	if agent.Name == "" {
		agent.Name = subagentType
	}

	tier := pickModelTier(subagentType)
	model, providerID, modelID := s.resolveDispatchModel(ctx, *agent, subagentType, tier, item.modelOverride)
	if model == nil {
		fullReason := "scheduler: no resolvable model in tier " + string(tier)
		// LB-13: this path deliberately bypasses failTaskCascade.
		s.planDB.Run([]string{"plandb", "task", "fail", task.ID, "--error", fullReason, "--json"})
		return failedDispatch(task.ID, subagentType, agentID, "no resolvable model in tier "+string(tier))
	}

	rootExcerpt, rootSource := s.fetchRootIssue(input.RootTaskID)
	deps := fetchDepResults(s.planDB, s.workspace, input.DBPath, input.ProjectID, task.ID)
	inherited := s.inheritedContexts(input, task)
	description := taskDescription(task)
	taskBand := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description: description, Tags: task.Tags, DependencyFanIn: &deps.FanIn,
	})

	if s.outcomeCache && !skipWorktree {
		if hit := s.lookupOutcomeCache(ctx, task, modelID); hit != nil {
			s.runner.Run(ctx, []string{"git", "worktree", "remove", "--force", worktree.Path}, runOptions{Cwd: s.workspace})
			s.runner.Run(ctx, []string{"git", "branch", "-D", "plandb/" + task.ID}, runOptions{Cwd: s.workspace})
			reused := hit.Outcome
			reused.TaskID = task.ID
			reused.Timestamp = jscompat.JSNumber(s.clock.Now().UnixMilli())
			s.emitOutcomeRecord(input, task.ID, reused)
			s.planDB.Run([]string{
				"plandb", "done", task.ID, "--agent", agentID, "--result",
				"Reused a previously VERIFIED-pass outcome (outcome-cache hit); dispatch skipped.", "--json",
			})
			summary := "outcome-cache hit: reused verified pass; dispatch skipped"
			verdict := VerdictPass
			return DispatchResult{
				TaskID: task.ID, SubagentType: subagentType, AgentID: agentID,
				Success: true, Summary: &summary,
				Gate: &DispatchGate{Status: GatePass, Verdict: &verdict},
			}
		}
	}

	factSheet := schedulerFactSheets.getOrGenerate(s.workspace)
	briefing := s.buildBriefing(input, task, fileScope, taskBand, isDirectChild && tier == ModelTierHigh)
	contractLines, rootDemandLines := s.leafClauseLines(ctx, task, rootSource)
	taskPrompt := buildTaskPrompt(task)
	var refs leafbriefing.ContextRefs
	if s.adaptiveCuts {
		refs = contextrefs.CreateContextRefs(task.ID + "-0")
	}
	reminderInput := leafReminderInput{
		Input: input, Workspace: s.workspace, Task: task, Agent: *agent, Worktree: worktree,
		FileScope: fileScope, RootExcerpt: rootExcerpt, DepLines: deps.Lines,
		InheritedContexts: inherited, FactSheet: factSheet, Briefing: briefing, Refs: refs,
		ContractLines: contractLines, RootDemandLines: rootDemandLines,
	}
	systemReminder := buildLeafReminder(reminderInput)

	guardOptions := leafLoopGuardOptions()
	guard := s.loopGuard(guardOptions)
	var liveGuard loopguard.LoopGuard
	if s.adaptiveCuts {
		liveGuard = guard
	}
	turnMonitor := newLeafTurnMonitor(liveGuard)
	attempts := []LeafRunResult{}
	runRequest := LeafRunRequest{
		ParentSessionID: input.ParentSessionID,
		SessionTitle:    task.Title + " (@" + agent.Name + " subagent, scheduler)",
		Workspace:       s.workspace, Worktree: worktree.Path, DBPath: input.DBPath,
		ProjectID: input.ProjectID, RootTaskID: input.RootTaskID, TaskID: task.ID,
		Agent: *agent, ProviderID: providerID, ModelID: modelID,
		SystemReminder: systemReminder, Prompt: embedSchedulerContext(refs, "task-description", taskPrompt),
		AfterTurn: turnMonitor.Observe,
	}
	runResult := s.callLeaf(ctx, runRequest, task.ID, subagentType)
	attempts = append(attempts, runResult)
	partCount, assistantErr := len(runResult.Parts), leafAssistantError(runResult)

	if partCount == 0 && assistantErr == "ProviderModelNotFoundError" {
		s.preserveWorktree(task.ID, worktree)
		s.runner.Run(ctx, []string{"git", "-C", worktree.Path, "reset", "--hard", worktree.BaseSHA}, runOptions{})
		runRequest.Attempt = 0
		runResult = s.callLeaf(ctx, runRequest, task.ID, subagentType)
		attempts = append(attempts, runResult)
		partCount, assistantErr = len(runResult.Parts), leafAssistantError(runResult)
	}

	freshRetries := 0
	loopStopReason := turnMonitor.StopReason()
	var lastInspection *attemptInspection
	inRunTestsPassed := false
	if runResult.TestPassed != nil {
		inRunTestsPassed = *runResult.TestPassed
	}
	var splitGroups [][]string
	if s.adaptiveCuts {
		for {
			inspectionGuard := guard
			if turnMonitor.Observed() {
				inspectionGuard = nil
			}
			inspection := inspectLeafAttempt(taskBand, attempts, runResult, freshRetries, inspectionGuard)
			lastInspection = &inspection
			if reason := turnMonitor.StopReason(); reason != "" {
				loopStopReason = reason
				break
			}
			if inspection.LoopStopReason != "" {
				loopStopReason = inspection.LoopStopReason
				break
			}
			if s.frontier && inspection.Triage.Remedy == failuretriage.RemedySplit {
				splitGroups = separableScopeGroups(fileScope, 4)
				if len(splitGroups) >= 2 {
					break
				}
			}
			// A fresh-context retry resets the worktree to base. Never do that
			// over real work.
			//
			// Everything driving this decision measures trajectory COST, not
			// correctness: contextpolicy reads turns/toolErrors/repairRounds,
			// and classifyFailure's DEFAULT verdict is DiagnosisUnknown ("no
			// strong heuristic matched") carrying RemedyFreshContextRetry. So
			// the absence of any evidence of a problem is itself enough to
			// discard a finished patch.
			//
			// node-semver-775 lost two correct patches to exactly that: its
			// ledger reads outcome=unknown for both attempts, each one a
			// `git reset --hard` over a working fix, and the run then hit the
			// wall clock mid-third-attempt and delivered an empty diff.
			//
			// A leaf that produced a diff has something the review gate can
			// judge, so send it there instead. The loop stays what it is meant
			// to be — a rescue for attempts that produced nothing.
			if len(listChangedFiles(s.runner, ctx, worktree.Path, worktree.BaseSHA)) > 0 {
				schedulerLog.Info(
					"leaf produced changes; skipping fresh-context retry and routing to the gate",
					map[string]any{
						"taskID":  string(task.ID),
						"remedy":  string(inspection.Triage.Remedy),
						"outcome": string(inspection.Triage.Diagnosis),
					})
				break
			}
			canRetry := freshRetries < int(contextpolicy.MaxFreshRetries)
			wantsBriefing := inspection.Triage.Remedy == failuretriage.RemedyAddContextRetry
			if inspection.Retry.Mode == contextpolicy.ModeGiveUp ||
				(!canRetry && inspection.Assessment.Verdict != contextpolicy.VerdictHealthy) {
				break
			}
			if !canRetry ||
				(!wantsBriefing && inspection.Retry.Mode != contextpolicy.ModeFreshContext) {
				break
			}
			freshRetries++
			s.preserveWorktree(task.ID, worktree)
			rejected := filepath.Join(s.workspace, ".codeaf", "rejected-work", safeTaskID(task.ID)+".patch")
			evidence := rejected
			ledgers.AppendAttempt(s.workspace, task.ID, ledgers.AttemptInput{
				Approach: lastApproach(runResult, inspection.Assessment.Reason),
				Outcome:  string(inspection.Triage.Diagnosis), Evidence: &evidence,
			})
			s.runner.Run(ctx, []string{"git", "-C", worktree.Path, "reset", "--hard", worktree.BaseSHA}, runOptions{})
			distilled := contextpolicy.BuildDistilledBrief(contextpolicy.BuildDistilledBriefInput{
				TaskDescription:     taskPrompt,
				FailureSignals:      append([]string{inspection.Assessment.Reason}, inspection.Triage.Signals...),
				AttemptedApproaches: attemptedApproaches(runResult),
				Ledger:              ledgers.ReadAttempts(s.workspace, task.ID),
				RejectedPatchPath:   rejected, Workspace: s.workspace,
			})
			runRequest.Attempt = freshRetries
			runRequest.SessionTitle = task.Title + " (@" + agent.Name + " subagent, scheduler) (fresh-context retry " + strconv.Itoa(freshRetries) + ")"
			refs = contextrefs.CreateContextRefs(task.ID + "-" + strconv.Itoa(freshRetries))
			reminderInput.Refs = refs
			runRequest.SystemReminder = buildLeafReminder(reminderInput)
			promptLines := []string{
				embedSchedulerContext(refs, "task-description", taskPrompt),
				"", "## Fresh-context retry", distilled,
			}
			if briefing != nil {
				promptLines = append(promptLines, "", leafbriefing.FormatLeafBriefing(leafbriefing.FormatLeafBriefingArgs{
					Workspace: s.workspace, Sections: *briefing, Refs: refs,
				}))
			}
			runRequest.Prompt = strings.Join(promptLines, "\n")
			runResult = s.callLeaf(ctx, runRequest, task.ID, subagentType)
			attempts = append(attempts, runResult)
			partCount, assistantErr = len(runResult.Parts), leafAssistantError(runResult)
			if runResult.TestPassed != nil {
				inRunTestsPassed = *runResult.TestPassed
			}
		}
	}

	resultText := lastText(runResult.Parts)
	if len(splitGroups) >= 2 {
		s.preserveWorktree(task.ID, worktree)
		splitChildren := s.trySplitLeaf(task, splitGroups, input.DBPath)
		reason := "too-hard: split into " + strconv.Itoa(len(splitGroups)) + " file-scoped parts"
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, reason)
		s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
			attempts, GateFail, 0, false, nil, inRunTestsPassed)
		errText := "split-requested: " + strconv.Itoa(len(splitGroups)) + " parts (" +
			strconv.Itoa(splitChildren) + " created in place)"
		return failedDispatch(task.ID, subagentType, agentID, errText)
	}

	contextGiveUp := lastInspection != nil && lastInspection.Retry.Mode == contextpolicy.ModeGiveUp
	if loopStopReason != "" || contextGiveUp {
		reason := loopStopReason
		if reason == "" && lastInspection != nil {
			reason = lastInspection.Retry.Reason
		}
		if reason == "" {
			reason = "adaptive context policy gave up"
		}
		// Always keep the quarantine copy, whichever way this goes.
		s.preserveWorktree(task.ID, worktree)
		// The adaptive guard is a verdict on CONTEXT health — turns, tool
		// errors, repair rounds — not on the work product. It never reads the
		// diff. Discarding a leaf that overran its band's turn budget but left
		// a real patch throws away the only thing the run produced, and it did:
		// urfave/cli #2263 and node-semver #775 both ended with an empty
		// delivered diff while a correct patch sat in .codeaf/rejected-work/.
		// When there is a diff, fall through and let the review gate — the one
		// component that actually judges patches — decide whether it ships.
		// A loop-guard stop is different: a leaf genuinely spinning is not made
		// trustworthy by having touched files, so that still fails closed.
		salvageable := loopStopReason == "" && !skipWorktree &&
			len(listChangedFiles(s.runner, ctx, worktree.Path, worktree.BaseSHA)) > 0
		if !salvageable {
			failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, "adaptive leaf guard: "+reason)
			s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
				attempts, GateFail, 0, false, nil, inRunTestsPassed)
			return failedDispatch(task.ID, subagentType, agentID, reason)
		}
		schedulerLog.Info("adaptive leaf guard salvage: routing guarded work to the review gate",
			map[string]any{"taskID": string(task.ID), "reason": compactJS(reason, 200)})
	}

	if partCount == 0 || assistantErr != "" {
		reason := assistantErr
		if reason == "" {
			reason = "empty dispatch result"
		}
		// Deliberately no preserveRejectedWork and no emitOutcome.
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, "scheduler: "+reason)
		return failedDispatch(task.ID, subagentType, agentID, reason)
	}

	if skipWorktree {
		upward := utf16Prefix(resultText, 2000)
		if s.adaptiveCuts {
			var decisions []string
			if upward != "" {
				decisions = []string{compactJS(upward, 200)}
			}
			upward = leafdigest.BuildLeafDigest(leafdigest.LeafDigestInput{
				TaskID: task.ID, Verdict: "pass", ChangedFiles: []string{}, KeyDecisions: decisions,
			})
		}
		// Deliberately no --agent, gate, merge, or outcome.
		s.planDB.Run([]string{"plandb", "done", task.ID, "--result", upward, "--json"})
		summary := upward
		if !s.adaptiveCuts {
			summary = utf16Prefix(resultText, 500)
		}
		return DispatchResult{
			TaskID: task.ID, SubagentType: subagentType, AgentID: agentID,
			Success: true, Summary: &summary,
		}
	}

	gateResult, gateErr := s.runGate(ctx, GateInput{
		Workspace: s.workspace, DBPath: input.DBPath, ProjectID: input.ProjectID,
		RootTaskID: input.RootTaskID, RootExcerpt: rootExcerpt, Task: task,
		WorktreePath: worktree.Path, Branch: "plandb/" + task.ID, BaseSHA: worktree.BaseSHA,
		Summary: resultText, ParentSessionID: input.ParentSessionID,
		SubagentType: subagentType, PromptOps: input.PromptOps,
	})
	if gateErr != nil {
		panic(gateErr)
	}
	if gateResult.FinalWorktreePath == "" {
		gateResult.FinalWorktreePath = worktree.Path
	}
	if gateResult.FinalBranch == "" {
		gateResult.FinalBranch = "plandb/" + task.ID
	}
	deliverUnjudged := gateResult.Unadjudicated &&
		(gateResult.Status == GateFail || gateResult.Status == GateEscalated) &&
		len(listChangedFiles(s.runner, ctx, gateResult.FinalWorktreePath, worktree.BaseSHA)) > 0

	if gateResult.Status == GateEscalated && !deliverUnjudged {
		if s.replanner != nil && s.replanner.ShouldReplan(ctx, s.workspace) {
			replanned, err := s.replanner.Replan(ctx, ReplanInput{
				Workspace: s.workspace, DBPath: input.DBPath,
				ParentSessionID: input.ParentSessionID, PromptOps: input.PromptOps,
				UserGoal: fallbackString(rootExcerpt, "(root excerpt unavailable)"),
				TaskID:   task.ID, SessionKey: task.ID + "-" + strconv.FormatInt(s.clock.Now().UnixMilli(), 10),
			})
			if err != nil {
				panic(err)
			}
			if replanned.Abort {
				s.preservePath(task.ID, gateResult.FinalWorktreePath, worktree.BaseSHA)
				reason := "replanner abort: " + replanned.Summary
				failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, reason)
				s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
					attempts, GateEscalated, gateResult.RepairAttempts, false,
					gateResult.Verdict, inRunTestsPassed)
				gateReason := replanned.Summary
				dispatch := failedDispatch(task.ID, subagentType, agentID, reason)
				dispatch.Gate = &DispatchGate{Status: GateFail, Reason: &gateReason}
				return dispatch
			}
		}
		s.preservePath(task.ID, gateResult.FinalWorktreePath, worktree.BaseSHA)
		cascadeReason := fallbackString(gateResult.AdvisorBlocker, fallbackString(gateResult.Reason, "advisor escalated"))
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, "escalated: "+cascadeReason)
		s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
			attempts, GateEscalated, gateResult.RepairAttempts, false,
			gateResult.Verdict, inRunTestsPassed)
		errText := "escalated: " + fallbackString(gateResult.Reason, "advisor escalated")
		dispatch := failedDispatch(task.ID, subagentType, agentID, errText)
		dispatch.Gate = dispatchGateFromResult(gateResult, GateFail)
		return dispatch
	}

	if gateResult.Status == GateFail && !deliverUnjudged {
		blocker := buildGateBlocker(task, gateResult)
		s.preservePath(task.ID, gateResult.FinalWorktreePath, worktree.BaseSHA)
		s.planDB.Run([]string{"plandb", "context", blocker, "--kind", "blocker", "--task", task.ID})
		recordGateFailure(input.RootTaskID, task, gateResult, s.clock.Now())
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID,
			"quality gate: "+fallbackString(gateResult.Reason, "review failed"))
		s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
			attempts, GateFail, gateResult.RepairAttempts, false,
			gateResult.Verdict, inRunTestsPassed)
		errText := "quality gate: " + fallbackString(gateResult.Reason, "fail")
		dispatch := failedDispatch(task.ID, subagentType, agentID, errText)
		dispatch.Gate = dispatchGateFromResult(gateResult, GateFail)
		return dispatch
	}
	if deliverUnjudged {
		s.preservePath(task.ID, gateResult.FinalWorktreePath, worktree.BaseSHA)
		blocker := buildGateBlocker(task, gateResult) + "\nInfrastructure cause: " +
			fallbackString(gateResult.Reason, "review gate could not obtain a judgement") +
			"; no component read this diff."
		s.planDB.Run([]string{"plandb", "context", blocker, "--kind", "blocker", "--task", task.ID})
		recordGateFailure(input.RootTaskID, task, gateResult, s.clock.Now())
		schedulerLog.Info("gate could not adjudicate; delivering unjudged leaf work", map[string]any{
			"taskID": task.ID, "status": gateResult.Status,
		})
	}

	language, _ := s.provider.GetLanguage(ctx, model)
	mergeResult, reconcileNote, deferred := s.mergeLeaf(ctx, mergeLeafInput{
		SchedulerInput: input, Task: task, Worktree: worktree,
		FinalWorktree: gateResult.FinalWorktreePath, FinalBranch: gateResult.FinalBranch,
		IsDirectChild: isDirectChild, ParentID: parentID, ParentBranch: parentBranch,
		ParentWorktree: parentWorktree, MergeAt: mergeAt, TargetBranch: targetBranch,
		RootExcerpt: rootExcerpt, Language: language, Aimd: aimd,
	})
	if !deferred && !mergeResult.OK {
		if aimd != nil {
			aimd.observe(false)
		}
		failTaskCascade(s.planDB, s.workspace, input.DBPath, task.ID, "merge failed: "+mergeResult.Summary)
		recordMergeFailure(input.RootTaskID, task, mergeResult, s.clock.Now())
		s.planDB.Run([]string{
			"plandb", "context", buildMergeBlocker(task, targetBranch, worktree.Path, mergeResult),
			"--kind", "blocker", "--task", task.ID, "--json",
		})
		s.preservePath(task.ID, gateResult.FinalWorktreePath, worktree.BaseSHA)
		s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
			attempts, gateResult.Status, gateResult.RepairAttempts, true,
			gateResult.Verdict, inRunTestsPassed)
		return failedDispatch(task.ID, subagentType, agentID, mergeResult.Summary)
	}
	if !deferred && aimd != nil {
		aimd.observe(true)
	}

	summary := compactJS(resultText+reconcileNote, 4000)
	upward := summary
	if s.adaptiveCuts {
		changed := listChangedFiles(s.runner, ctx, gateResult.FinalWorktreePath, worktree.BaseSHA)
		var testSummary *string
		if inRunTestsPassed {
			value := "in-run tests: passed"
			testSummary = &value
		} else if runResult.TestPassed != nil && !*runResult.TestPassed {
			value := "in-run tests: failed"
			testSummary = &value
		}
		var failureSignals []string
		if gateResult.Reason != "" {
			failureSignals = []string{compactJS(gateResult.Reason, 200)}
		}
		var keyDecisions []string
		if jscompat.Trim(reconcileNote) != "" {
			keyDecisions = []string{compactJS(jscompat.Trim(reconcileNote), 160)}
		}
		upward = leafdigest.BuildLeafDigest(leafdigest.LeafDigestInput{
			TaskID: task.ID, Verdict: gateVerdictText(gateResult), ChangedFiles: changed,
			TestSummary: testSummary, FailureSignals: failureSignals, KeyDecisions: keyDecisions,
		})
	}

	// LB-15: deferred work is marked done before its queued merge happens.
	s.planDB.Run([]string{"plandb", "done", task.ID, "--agent", agentID, "--result", upward, "--json"})
	if gateResult.Status == GatePass && (gateResult.Done == nil || *gateResult.Done) {
		body := strings.Join([]string{
			"Leaf " + task.ID + " (" + task.Title + ") frozen by review-gate.",
			"Reviewer signaled done=true: contract is fully delivered.",
			"Replanner / audit-fix MUST NOT re-target this leaf.",
		}, "\n")
		s.planDB.Run([]string{"plandb", "context", body, "--kind", "frozen", "--task", task.ID, "--json"})
	}
	s.writeHandoffs(input, task, upward)
	outcome := s.emitDispatchOutcome(input, task, providerID, modelID, deps.FanIn, start,
		attempts, gateResult.Status, gateResult.RepairAttempts, false,
		gateResult.Verdict, inRunTestsPassed)
	if s.outcomeCache && mergeAt == s.workspace && gateResult.Status == GatePass && outcome != nil {
		s.recordOutcomeCache(ctx, task, modelID, *outcome, gateResult.Verdict, inRunTestsPassed)
	}
	dispatch := DispatchResult{
		TaskID: task.ID, SubagentType: subagentType, AgentID: agentID,
		Success: true, Summary: dispatchStringPointer(func() string {
			if s.adaptiveCuts {
				return upward
			}
			return summary
		}()),
		Gate: dispatchGateFromResult(gateResult, gateResult.Status),
	}
	return dispatch
}

func (s *Scheduler) resolveAgent(ctx context.Context, name string) (*AgentInfo, error) {
	if s.agents == nil {
		return &AgentInfo{Name: name}, nil
	}
	return s.agents.Get(ctx, name)
}

func (s *Scheduler) resolveDispatchModel(
	ctx context.Context,
	agent AgentInfo,
	subagent string,
	tier ModelTier,
	override *capabilityModelRef,
) (model any, providerID, modelID string) {
	if s.provider == nil {
		return nil, "", ""
	}
	if override != nil {
		if value, err := s.provider.GetModel(ctx, override.ProviderID, override.ModelID); err == nil && value != nil {
			providerID, modelID = override.ProviderID, override.ModelID
			return value, providerID, modelID
		}
	}
	pool := []ModelCandidate{}
	if filtered, ok := s.pools.(FilteredModelPoolResolver); ok {
		pool = filtered.CandidatesForTierFiltered(tier, subagent)
	} else if s.pools != nil {
		pool = s.pools.CandidatesForTier(tier)
	}
	for _, candidate := range pool {
		pid, mid, ok := splitModelID(candidate.ID)
		if !ok {
			continue
		}
		if value, err := s.provider.GetModel(ctx, pid, mid); err == nil && value != nil {
			return value, pid, mid
		}
	}
	if agent.Model != nil {
		pid, mid := modelIdentity(agent.Model)
		if mid != "" {
			return agent.Model, pid, mid
		}
	}
	return nil, "", ""
}

func modelIdentity(model any) (string, string) {
	switch value := model.(type) {
	case ProviderModel:
		id := value.ID
		if id == "" {
			id = value.ModelID
		}
		return value.ProviderID, id
	case *ProviderModel:
		if value == nil {
			return "", ""
		}
		return modelIdentity(*value)
	case map[string]any:
		provider, _ := value["providerID"].(string)
		id, _ := value["id"].(string)
		if id == "" {
			id, _ = value["modelID"].(string)
		}
		return provider, id
	}
	rv := reflect.ValueOf(model)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", ""
	}
	fieldString := func(name string) string {
		field := rv.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
		return ""
	}
	id := fieldString("ID")
	if id == "" {
		id = fieldString("ModelID")
	}
	return fieldString("ProviderID"), id
}

func pickModelTier(subagent string) ModelTier {
	if baked.TierFor(subagent) == baked.TierLow {
		return ModelTierLow
	}
	return ModelTierHigh
}

func (s *Scheduler) detectCurrentBranch(ctx context.Context) string {
	branch := s.runner.Run(ctx, []string{"git", "symbolic-ref", "--short", "HEAD"}, runOptions{Cwd: s.workspace})
	if value := jscompat.Trim(string(branch.Stdout)); value != "" {
		return value
	}
	head := s.runner.Run(ctx, []string{"git", "rev-parse", "HEAD"}, runOptions{Cwd: s.workspace})
	if value := jscompat.Trim(string(head.Stdout)); value != "" {
		return value
	}
	return "main"
}

func (s *Scheduler) fetchRootIssue(rootTaskID string) (string, string) {
	show := s.planDB.Run([]string{"plandb", "show", rootTaskID, "--json"})
	var root plandb.Task
	if json.Unmarshal(show.Stdout, &root) != nil {
		return "", ""
	}
	body := stripPolicyLines(taskDescription(&root))
	body = stripHarnessRoot(body)
	body = regexp.MustCompile(`(?im)^User request:`+jsSpacePattern+`*`).ReplaceAllString(body, "")
	body = jscompat.Trim(body)
	if body == "" {
		return "", ""
	}
	return compactJS(body, rootIssueExcerptLimit), body
}

func (s *Scheduler) inheritedContexts(input SchedulerInput, task *plandb.Task) []string {
	res := s.planDB.Run([]string{"plandb", "contexts", "--json"})
	var contexts []*plandb.ContextEntry
	if json.Unmarshal(res.Stdout, &contexts) != nil {
		return []string{}
	}
	wanted := map[string]struct{}{input.RootTaskID: {}, task.ID: {}}
	filtered := make([]*plandb.ContextEntry, 0, len(contexts))
	for _, entry := range contexts {
		if entry == nil {
			continue
		}
		if entry.TaskID == nil {
			filtered = append(filtered, entry)
			continue
		}
		if _, ok := wanted[*entry.TaskID]; ok {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) > 8 {
		// LB-5: contexts are newest-first, so this keeps the oldest eight.
		filtered = filtered[len(filtered)-8:]
	}
	lines := make([]string, 0, len(filtered))
	for _, entry := range filtered {
		kind := entry.Kind
		if kind == "" {
			kind = "context"
		}
		taskSuffix := ""
		if entry.TaskID != nil {
			taskSuffix = " " + *entry.TaskID
		}
		lines = append(lines, "- ["+kind+taskSuffix+"] "+compactJS(entry.Content, 700))
	}
	return lines
}

func (s *Scheduler) buildBriefing(
	input SchedulerInput,
	task *plandb.Task,
	fileScope []string,
	band sizeband.SizeBand,
	strongRootCut bool,
) *leafbriefing.LeafBriefingSections {
	if !s.adaptiveCuts || s.briefing == nil || strongRootCut ||
		bandRank(band) < bandRank(sizeband.BandM) {
		return nil
	}
	focus := append([]string(nil), fileScope...)
	if len(focus) == 0 {
		issue := policyline.PolicyLine(task.Description, "issue_file")
		if issue != nil {
			path := *issue
			if !filepath.IsAbs(path) {
				path = filepath.Join(s.workspace, path)
			}
			if raw, err := os.ReadFile(path); err == nil {
				focus = leafbriefing.ExtractFocusPaths(string(raw))
			}
		}
	}
	budget := 1500.0
	return s.briefing.BuildLeafBriefingSections(leafbriefing.BuildLeafBriefingSectionsArgs{
		Workspace: s.workspace, RunKey: input.ProjectID, FocusPaths: focus, BudgetChars: &budget,
	})
}

type factSheetCache struct {
	mu     sync.Mutex
	values map[string]string
}

var schedulerFactSheets = factSheetCache{values: map[string]string{}}

func (c *factSheetCache) getOrGenerate(workspace string) string {
	c.mu.Lock()
	value, ok := c.values[workspace]
	c.mu.Unlock()
	if ok {
		return value
	}
	value = factsheet.GenerateFactSheet(factsheet.GenerateFactSheetOpts{RootDir: workspace}).Markdown
	c.mu.Lock()
	c.values[workspace] = value
	c.mu.Unlock()
	return value
}

type leafReminderInput struct {
	Input             SchedulerInput
	Workspace         string
	Task              *plandb.Task
	Agent             AgentInfo
	Worktree          *WorktreeAllocation
	FileScope         []string
	RootExcerpt       string
	DepLines          []string
	InheritedContexts []string
	FactSheet         string
	Briefing          *leafbriefing.LeafBriefingSections
	Refs              leafbriefing.ContextRefs
	ContractLines     []string
	RootDemandLines   []string
}

func embedSchedulerContext(refs leafbriefing.ContextRefs, kind, content string) string {
	if refs == nil {
		return content
	}
	registration := refs.Register(kind, content)
	if registration.RefID == "" {
		return content
	}
	if registration.FirstMention {
		return registration.RefID + "\n" + content
	}
	return refs.Render(registration.RefID)
}

func buildLeafReminder(in leafReminderInput) string {
	task := in.Task
	lines := []string{
		"<system-reminder>",
		"PlanDB scheduler dispatched this subagent.",
		"Project: " + in.Input.ProjectID,
		"Root task: " + in.Input.RootTaskID,
		"Assigned PlanDB task: " + task.ID,
		"",
	}
	if in.FactSheet != "" {
		lines = append(lines, in.FactSheet, "")
	}
	if in.RootExcerpt != "" {
		lines = append(lines, "Root user request:", embedSchedulerContext(
			in.Refs, "plan-context:root-excerpt", in.RootExcerpt,
		))
		lines = append(lines, in.RootDemandLines...)
		lines = append(lines, "")
	}
	lines = append(lines, in.ContractLines...)
	if len(in.ContractLines) > 0 {
		lines = append(lines, "")
	}
	if len(in.DepLines) > 0 {
		lines = append(lines, "Upstream dependency results (feeds_into):")
		lines = append(lines, in.DepLines...)
		lines = append(lines, "")
	}
	if len(in.InheritedContexts) > 0 {
		lines = append(lines, "Relevant durable PlanDB context:")
		lines = append(lines, embedSchedulerContext(
			in.Refs, "plan-context:durable", strings.Join(in.InheritedContexts, "\n"),
		), "")
	}
	if in.Briefing != nil {
		lines = append(lines, "Weak-model repository briefing (read before searching):")
		lines = append(lines, leafbriefing.FormatLeafBriefing(leafbriefing.FormatLeafBriefingArgs{
			Workspace: in.Workspace, Sections: *in.Briefing, Refs: in.Refs,
		}), "")
	}
	role := policyline.PolicyLine(task.Description, "task_role")
	parallel := policyline.PolicyLine(task.Description, "parallel")
	contextInputs := policyline.PolicyLine(task.Description, "context_inputs")
	outputs := policyline.PolicyLine(task.Description, "outputs")
	acceptance := policyline.PolicyLine(task.Description, "acceptance")
	if role != nil || parallel != nil || contextInputs != nil || outputs != nil || acceptance != nil {
		lines = append(lines, "Leaf contract (from PlanDB description):")
		lines = appendPolicyReminder(lines, "task_role", role)
		lines = appendPolicyReminder(lines, "parallel", parallel)
		lines = appendPolicyReminder(lines, "context_inputs", contextInputs)
		lines = appendPolicyReminder(lines, "outputs", outputs)
		lines = appendPolicyReminder(lines, "acceptance", acceptance)
		lines = append(lines, "")
	}
	lines = append(lines,
		"Isolated worktree for this leaf:",
		"  worktree path: "+in.Worktree.Path,
		"  base commit: "+in.Worktree.BaseSHA,
		"  branch: plandb/"+task.ID,
	)
	if len(in.FileScope) > 0 {
		lines = append(lines,
			"  file_scope (advisory): "+strings.Join(in.FileScope, ", "),
			"LEAF FENCE: Write only files in `file_scope`. Treat dependency outputs as\n"+
				"contracts, not permission to edit their files. If a required shared-file change\n"+
				"is outside scope, report the exact path and contract mismatch; create a narrow\n"+
				"child/join only when permitted. Do not silently widen scope or repair a\n"+
				"sibling’s file.",
		)
	}
	lines = append(lines,
		"Your cwd is the worktree. Read, write, and run shell commands freely inside it.",
		"Relative paths resolve to the worktree (the harness redirects absolute project paths into the worktree as well).",
		"When you finish, the merge worker integrates your branch back into the parent's branch (or main if root is the parent).",
		"If your scope grows, add children via `plandb add` — they will be dispatched into their own worktrees branched from yours.",
	)
	allowsTask, allowsPlanDB := permissionAllows(in.Agent.Permission, "task"), permissionAllows(in.Agent.Permission, "plandb")
	usage := planDBUsageLines(task.ID, allowsPlanDB, allowsTask)
	if len(usage) > 0 {
		lines = append(lines, "")
		lines = append(lines, usage...)
	}
	lines = append(lines, "", "Return a compact final result; the scheduler will mark your task done.", "</system-reminder>")
	return strings.Join(lines, "\n")
}

// CoderClauseContractLines ports plandb-scheduler.ts:189-200.
func CoderClauseContractLines(clauses []string) []string {
	if len(clauses) == 0 {
		return []string{}
	}
	lines := []string{
		"",
		"# Verification contract — spec clause inventory (implement AND self-check every item)",
		"This is the exact checklist your work will be audited against. Each line is a",
		"distinct behavioral demand extracted from the spec. Implement every one, and",
		"before you declare done verify each against your changes — the auditor probes",
		"the same list (including pairwise interactions). Do not drop or narrow any item.",
	}
	for index, clause := range clauses {
		lines = append(lines, strconv.Itoa(index+1)+". "+clause)
	}
	return lines
}

// RootClauseListLines ports plandb-scheduler.ts:206-212.
func RootClauseListLines(clauses []string) []string {
	if len(clauses) == 0 {
		return []string{}
	}
	lines := []string{"Complete spec demand list (survives excerpt truncation):"}
	for index, clause := range clauses {
		lines = append(lines, strconv.Itoa(index+1)+". "+clause)
	}
	return lines
}

func (s *Scheduler) leafClauseLines(
	ctx context.Context, task *plandb.Task, rootSource string,
) ([]string, []string) {
	if !s.adaptiveCuts || s.clauses == nil {
		return []string{}, []string{}
	}
	taskSpec := rootSource
	taskSpecIsRoot := true
	if issue := policyline.PolicyLine(task.Description, "issue_file"); issue != nil && *issue != "" {
		path := *issue
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.workspace, path)
		}
		if raw, err := os.ReadFile(path); err == nil {
			taskSpec = string(raw)
			taskSpecIsRoot = false
		}
	}
	contract := []string{}
	if taskSpec != "" {
		if judged, err := s.clauses.JudgeSpecClauses(ctx, taskSpec, s.workspace); err == nil &&
			judged.Count >= denseSpecClauseThreshold {
			contract = CoderClauseContractLines(judged.Clauses)
		}
	}
	demands := []string{}
	if rootSource != "" && !(taskSpecIsRoot && len(contract) > 0) {
		if judged, err := s.clauses.JudgeSpecClauses(ctx, rootSource, s.workspace); err == nil && judged.Count >= 1 {
			demands = RootClauseListLines(judged.Clauses)
		}
	}
	return contract, demands
}

func appendPolicyReminder(lines []string, name string, value *string) []string {
	if value != nil {
		lines = append(lines, "  "+name+": "+*value)
	}
	return lines
}

func permissionAllows(rules []PermissionRule, permission string) bool {
	for _, rule := range rules {
		if rule.Permission == permission && rule.Action == "allow" {
			return true
		}
	}
	return false
}

func planDBUsageLines(taskID string, allowsPlanDB, allowsTask bool) []string {
	lines := []string{}
	if allowsPlanDB {
		lines = append(lines,
			"PlanDB usage within your assigned task:",
			"- plandb add: create child packages under "+taskID+" for substantial sub-work you discover.",
			"- plandb context: record durable discoveries (kind: discovery|decision|constraint|blocker).",
			"- plandb amend "+taskID+" --prepend: append notes/findings to your own task description.",
			"- plandb amend <future-task-id> --prepend: when you learn something a downstream task needs, amend that task.",
			"- plandb split "+taskID+" --into 'A, B, C': split this task if it grew larger than scoped.",
		)
	}
	if allowsTask {
		lines = append(lines, "- task tool: spawn nested subagents; harness creates a child package under your assigned task.")
	}
	if allowsPlanDB {
		lines = append(lines,
			"Do not use bash to run PlanDB commands; use the plandb tool.",
			"Do not micro-fragment: one PlanDB task per coherent unit of work, not one per tool call.",
		)
	}
	return lines
}

const jsSpacePattern = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var policyNames = map[string]struct{}{
	"access": {}, "parallel": {}, "worktree": {}, "file_scope": {},
	"task_role": {}, "context_inputs": {}, "outputs": {}, "agent": {},
	"acceptance": {}, "session_id": {}, "message_id": {}, "parent_session_id": {},
	"subagent_type": {}, "command": {},
}

func stripPolicyLines(description string) string {
	if description == "" {
		return ""
	}
	raw := strings.Split(description, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := jscompat.Trim(line)
		colon := strings.IndexByte(trimmed, ':')
		if colon >= 0 {
			if _, ok := policyNames[strings.ToLower(trimmed[:colon])]; ok {
				continue
			}
		}
		out = append(out, line)
	}
	joined := strings.Join(out, "\n")
	for strings.Contains(joined, "\n\n\n") {
		joined = strings.ReplaceAll(joined, "\n\n\n", "\n\n")
	}
	return jscompat.Trim(joined)
}

func buildTaskPrompt(task *plandb.Task) string {
	lines := []string{}
	if task.Title != "" {
		lines = append(lines, "Task: "+task.Title)
	}
	issueFile := policyline.PolicyLine(task.Description, "issue_file")
	if issueFile != nil && *issueFile != "" {
		lines = append(lines, "",
			"Full spec: "+*issueFile,
			"Read this file FIRST — it is your source of truth for interface contracts, sibling-module dependencies, verbatim error strings, and testable acceptance criteria; do not infer requirements not present in it.",
			"Before declaring done, self-check every `## Acceptance criteria` bullet with per-bullet evidence — follow the Acceptance_Verification rule in your system prompt.",
		)
	}
	body := stripPolicyLines(taskDescription(task))
	body = removeRootIssueContext(body)
	body = regexp.MustCompile(`(?im)^`+jsSpacePattern+`*Delegated prompt:`+jsSpacePattern+`*`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?im)^`+jsSpacePattern+`*Harness-created[^\n\r\x{2028}\x{2029}]*?package\.`+jsSpacePattern+`*`).ReplaceAllString(body, "")
	body = jscompat.Trim(body)
	if body != "" {
		lines = append(lines, "")
		if issueFile != nil && *issueFile != "" {
			lines = append(lines, "Brief (use only if the issue file above is unavailable):")
		}
		lines = append(lines, body)
	}
	return strings.Join(lines, "\n")
}

func removeRootIssueContext(body string) string {
	start := -1
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		if strings.HasPrefix(jscompat.Trim(line), "Root issue context:") {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return body
	}
	end := len(body)
	if blank := strings.Index(body[start:], "\n\n"); blank >= 0 {
		end = start + blank
	}
	return body[:start] + body[end:]
}

func stripHarnessRoot(body string) string {
	start := strings.Index(body, "Harness-created root task")
	if start < 0 {
		return body
	}
	user := strings.Index(body[start:], "User request:")
	if user < 0 {
		return body[:start]
	}
	return body[:start] + body[start+user:]
}

func taskDescription(task *plandb.Task) string {
	if task == nil || task.Description == nil {
		return ""
	}
	return *task.Description
}

func (s *Scheduler) callLeaf(ctx context.Context, request LeafRunRequest, taskID, subagent string) LeafRunResult {
	if s.stepLoop == nil {
		return LeafRunResult{ErrorName: "steploop client unavailable"}
	}
	directory := request.Worktree
	if directory == "" {
		directory = request.Workspace
	}
	leafCtx := project.WithContext(ctx, project.InstanceContext{
		Directory: directory,
		Worktree:  request.Workspace,
		Project: project.Info{
			ID:        project.ID(request.ProjectID),
			Worktree:  request.Workspace,
			Sandboxes: []string{directory},
		},
		PlanDB: &project.PlanDB{
			DBPath: request.DBPath, ProjectID: request.ProjectID,
			RootTaskID: request.RootTaskID, TaskID: request.TaskID,
		},
	})
	result, err := s.stepLoop.RunLeaf(leafCtx, request)
	if err == nil {
		return result
	}
	message := err.Error()
	schedulerLog.Error("ops.prompt failed", map[string]any{
		"taskID": taskID, "subagent": subagent, "cause": compactJS(message, 800),
	})
	result.Parts = []LeafPart{{
		Type: "text", Text: "Scheduler dispatch error: " + compactJS(message, 500),
	}}
	return result
}

func leafAssistantError(result LeafRunResult) string {
	if result.ErrorMessage != "" {
		return result.ErrorMessage
	}
	return result.ErrorName
}

func lastText(parts []LeafPart) string {
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Type == "text" {
			return parts[i].Text
		}
	}
	return ""
}

type attemptInspection struct {
	Assessment     contextpolicy.ContextAssessment
	Retry          contextpolicy.RetryDecision
	Triage         failuretriage.FailureTriageResult
	LoopStopReason string
}

func inspectLeafAttempt(
	band sizeband.SizeBand,
	all []LeafRunResult,
	current LeafRunResult,
	freshRetries int,
	guard loopguard.LoopGuard,
) attemptInspection {
	messages := []*leafoutcome.SessionMessage{}
	for _, attempt := range all {
		messages = append(messages, attempt.Messages...)
	}
	stats := leafoutcome.AggregateSessionStats(messages)
	stop := ""
	costs := current.CallCosts
	if len(costs) == 0 && current.CostUSD > 0 {
		costs = []float64{current.CostUSD}
	}
	chargedLedger := len(costs) > 0
	for _, part := range current.Parts {
		if stop != "" || part.Type != "tool" || guard == nil {
			continue
		}
		cost := part.CostUSD
		if chargedLedger {
			cost = nil
		}
		verdict := guard.Observe(loopguard.LoopAction{
			Tool: part.Tool, ArgsKey: part.ArgsKey, CostUsd: cost,
		})
		if verdict.Status == loopguard.LoopStatusStop {
			if verdict.Reason != nil {
				stop = *verdict.Reason
			} else {
				stop = "loop guard stopped leaf"
			}
			break
		}
	}
	if stop == "" && guard != nil && chargedLedger {
		for index := range costs {
			verdict := guard.ObserveCost(&costs[index])
			if verdict.Status != loopguard.LoopStatusStop {
				continue
			}
			if verdict.Reason != nil {
				stop = *verdict.Reason
			} else {
				stop = "loop guard stopped leaf"
			}
			break
		}
	}
	assessment := contextpolicy.AssessContext(contextpolicy.AssessContextInput{
		Band: band, Turns: float64(stats.Turns), ToolErrors: float64(stats.ToolErrors),
		RepairRounds: float64(freshRetries),
	})
	text := lastText(current.Parts)
	triage := failuretriage.ClassifyFailure(failuretriage.FailureTriageInput{
		TranscriptTail: transcriptTail(current), TestOutput: &text,
		ToolErrors: float64(stats.ToolErrors), Turns: float64(stats.Turns),
		RepairRounds: float64(freshRetries),
	})
	return attemptInspection{
		Assessment: assessment, Retry: contextpolicy.RetryMode(assessment, float64(freshRetries)),
		Triage: triage, LoopStopReason: stop,
	}
}

func transcriptTail(result LeafRunResult) string {
	lines := []string{}
	for _, part := range result.Parts {
		switch part.Type {
		case "text":
			lines = append(lines, part.Text)
		case "tool":
			lines = append(lines, "tool "+part.Tool+": "+part.ArgsKey)
		}
	}
	// Deliberate divergence from plandb-scheduler.ts:849-860, which reaches for
	// the same `compact()` prefix helper it uses for log lines and therefore
	// returns the transcript HEAD under a name that promises the tail.
	//
	// Every consumer asks a recency question — classifyFailure's localization
	// and spec-misread checks both test "did the agent mention this file/symbol"
	// — and for a leaf that ran 65+ turns the opening 6000 units is its setup
	// chatter, long before the edits it is being judged on. node-semver-775
	// was triaged "localization" on both attempts because the symbol it had
	// just written (PRERELEASECOERCE) was absent from the session's opening,
	// and each verdict reset a correct patch to base.
	return tailJS(strings.Join(lines, "\n"), 6000)
}

// tailJS keeps the LAST limit UTF-16 units of text, marking elision at the
// front. It is compactJS's mirror image and shares its unit accounting so a
// surrogate pair is never split.
func tailJS(text string, limit int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	start := len(units) - (limit - 3)
	if start < 0 {
		start = 0
	}
	// Never begin on a low surrogate; that would decode to U+FFFD.
	if start > 0 && start < len(units) &&
		units[start] >= 0xDC00 && units[start] <= 0xDFFF {
		start++
	}
	return "..." + string(utf16.Decode(units[start:]))
}

func attemptedApproaches(result LeafRunResult) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, part := range result.Parts {
		if part.Type != "tool" {
			continue
		}
		value := compactJS(part.Tool+" "+part.ArgsKey, 180)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return out
}

func lastApproach(result LeafRunResult, reason string) string {
	approaches := attemptedApproaches(result)
	if len(approaches) > 0 && jscompat.Trim(approaches[len(approaches)-1]) != "" {
		return jscompat.Trim(approaches[len(approaches)-1])
	}
	if jscompat.Trim(reason) != "" {
		return jscompat.Trim(reason)
	}
	return "no meaningful action recorded"
}

func leafLoopGuardOptions() loopguard.LoopGuardOptions {
	var options loopguard.LoopGuardOptions
	if raw := os.Getenv("CODEAF_LEAF_MAX_ACTIONS"); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value > 0 {
			value = float64(int(value))
			options.MaxActions = &value
		}
	}
	if raw := os.Getenv("CODEAF_LEAF_MAX_COST_USD"); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value > 0 {
			options.MaxCostUsd = &value
		}
	}
	return options
}

func separableScopeGroups(scope []string, maxGroups int) [][]string {
	unique := []string{}
	seen := map[string]struct{}{}
	for _, raw := range scope {
		value := jscompat.Trim(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	if len(unique) < 2 {
		return nil
	}
	if maxGroups < 2 {
		maxGroups = 2
	}
	n := min(len(unique), maxGroups)
	groups := make([][]string, n)
	for index, value := range unique {
		// LB-8: n-1 singleton groups and all remainder in the last group.
		group := min(index, n-1)
		groups[group] = append(groups[group], value)
	}
	return groups
}

func (s *Scheduler) trySplitLeaf(task *plandb.Task, groups [][]string, dbPath string) int {
	specs := make([]plandb.SplitSpec, 0, len(groups))
	childTags := []string{}
	for _, tag := range task.Tags {
		if !strings.HasPrefix(strings.ToLower(tag), "scope:") {
			childTags = append(childTags, tag)
		}
	}
	for _, group := range groups {
		description := "file_scope: " + strings.Join(group, ", ") + "\n" + taskDescription(task)
		description = jscompat.Trim(description) + "\n\n(split from oversized leaf " + task.ID + "; too-hard by diagnosis)"
		specs = append(specs, plandb.SplitSpec{
			Title:       task.Title + " [files: " + strings.Join(group, " + ") + "]",
			Description: description, Tags: childTags,
		})
	}
	children, err := plandb.GetPlanDB().SplitTaskWithSpecs(task.ID, specs)
	if err == nil {
		return len(children)
	}
	bodyLines := []string{"Too-hard leaf " + task.ID + ": split into " + strconv.Itoa(len(specs)) + " file-scoped parts."}
	for _, spec := range specs {
		bodyLines = append(bodyLines, "- "+spec.Title)
	}
	bodyLines = append(bodyLines, "Realize via replanner split op / frontier tick (leaf was running; in-place split rejected).")
	s.planDB.Run([]string{"plandb", "context", strings.Join(bodyLines, "\n"), "--kind", "cut", "--task", task.ID, "--json"})
	_ = dbPath
	return 0
}

func (s *Scheduler) preserveWorktree(taskID string, worktree *WorktreeAllocation) {
	s.preservePath(taskID, worktree.Path, worktree.BaseSHA)
}

func (s *Scheduler) preservePath(taskID, path, baseSHA string) {
	s.preserve(leafoutcome.PreserveRejectedWorkArgs{
		Workspace: s.workspace, Worktree: path, TaskID: taskID, BaseSha: &baseSHA,
	})
}

func (s *Scheduler) runGate(ctx context.Context, input GateInput) (GateResult, error) {
	if s.gate == nil {
		return GateResult{
			Status: GateSkipped, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
		}, nil
	}
	return s.gate.GateLeaf(ctx, input)
}

func (s *Scheduler) emitDispatchOutcome(
	input SchedulerInput,
	task *plandb.Task,
	providerID, modelID string,
	dependencyFanIn float64,
	start time.Time,
	attempts []LeafRunResult,
	status GateStatus,
	repairRounds float64,
	mergeConflict bool,
	verdict *ReviewVerdict,
	testsPassed bool,
) *leafoutcome.LeafOutcome {
	messages := []*leafoutcome.SessionMessage{}
	if s.adaptiveCuts {
		for _, attempt := range attempts {
			messages = append(messages, attempt.Messages...)
		}
	} else if len(attempts) > 0 {
		messages = append(messages, attempts[len(attempts)-1].Messages...)
	}
	stats := leafoutcome.AggregateSessionStats(messages)
	description := taskDescription(task)
	fanIn := dependencyFanIn
	var audit any
	if verdict != nil {
		audit = verdict.Raw
		if audit == nil {
			audit = reviewVerdictMap(*verdict)
		}
	}
	var evidence *leafoutcome.LeafOutcomeEvidence
	if s.adaptiveCuts {
		evidence = leafoutcome.AuditEvidenceFromVerdict(audit, testsPassed)
	}
	outcome := leafoutcome.BuildLeafOutcome(leafoutcome.BuildLeafOutcomeInput{
		TaskID: task.ID, ProviderID: providerID, ModelID: modelID,
		Tags: task.Tags, Description: &description, DependencyFanIn: &fanIn,
		GateStatus:   leafoutcome.GateStatus(status),
		RepairRounds: jscompat.JSNumber(repairRounds), Turns: stats.Turns,
		ToolErrors: stats.ToolErrors, CostUsd: stats.CostUsd,
		WallMs:        jscompat.JSNumber(s.clock.Now().Sub(start).Milliseconds()),
		MergeConflict: mergeConflict, Evidence: evidence,
	})
	s.emitOutcomeRecord(input, task.ID, outcome)
	return &outcome
}

func (s *Scheduler) emitOutcomeRecord(input SchedulerInput, taskID string, outcome leafoutcome.LeafOutcome) {
	s.emit(leafoutcome.EmitLeafOutcomeArgs{
		Workspace: s.workspace, Outcome: outcome,
		WriteContext: func(body string) error {
			result := s.planDB.Run([]string{
				"plandb", "context", body, "--kind", "outcome", "--task", taskID,
			})
			if result.Code != 0 {
				return errors.New(string(result.Stderr))
			}
			return nil
		},
	})
	if s.outcomeObserver != nil {
		s.outcomeObserver.Observe(outcome)
	}
	_ = input
}

func reviewVerdictMap(verdict ReviewVerdict) map[string]any {
	bugs := make([]any, 0, len(verdict.Bugs))
	for _, bug := range verdict.Bugs {
		bugs = append(bugs, map[string]any{
			"severity": bug.Severity, "file": bug.File, "line": bug.Line, "detail": bug.Detail,
		})
	}
	return map[string]any{
		"verdict": string(verdict.Verdict), "confidence": string(verdict.Confidence),
		"bugs": bugs, "repair_hints": verdict.RepairHints, "evidence": verdict.Evidence,
	}
}

func buildGateBlocker(task *plandb.Task, gate GateResult) string {
	verdict := gate.Verdict
	confidence := ""
	if verdict != nil && verdict.Confidence != "" {
		confidence = "confidence=" + string(verdict.Confidence)
	}
	lines := []string{
		"Quality gate FAILED for task " + task.ID + " after " + confidence + ".",
		"Reason: " + fallbackString(gate.Reason, "review verdict=fail"),
	}
	if verdict != nil && verdict.SpecCoverage != "" {
		lines = append(lines, "Spec coverage: "+compactJS(verdict.SpecCoverage, 300))
	}
	if verdict != nil && len(verdict.Bugs) > 0 {
		count := min(10, len(verdict.Bugs))
		bugs := make([]string, 0, count)
		for _, bug := range verdict.Bugs[:count] {
			location := "(no file)"
			if bug.File != "" {
				location = bug.File
				if bug.Line != nil && fmt.Sprint(bug.Line) != "" {
					location += ":" + fmt.Sprint(bug.Line)
				}
			}
			bugs = append(bugs, "  - ["+bug.Severity+"] "+location+": "+compactJS(bug.Detail, 160))
		}
		lines = append(lines, "Reviewer bugs (top "+strconv.Itoa(count)+"):\n"+strings.Join(bugs, "\n"))
	}
	if verdict != nil && len(verdict.RepairHints) > 0 {
		count := min(10, len(verdict.RepairHints))
		hints := make([]string, 0, count)
		for _, hint := range verdict.RepairHints[:count] {
			hints = append(hints, "  - "+compactJS(hint, 200))
		}
		lines = append(lines, "Repair hints from reviewer:\n"+strings.Join(hints, "\n"))
	}
	if verdict != nil && verdict.Evidence != "" {
		lines = append(lines, "Reviewer evidence: "+compactJS(verdict.Evidence, 400))
	}
	lines = append(lines,
		"Per-attempt verdicts: `plandb contexts --task "+task.ID+" --kind review`",
		"Worktree preserved at "+gate.FinalWorktreePath+" for inspection.",
		"To retry: address the bugs above, then re-add the task or split into smaller leaves.",
	)
	return strings.Join(lines, "\n")
}

func recordGateFailure(root string, task *plandb.Task, gate GateResult, now time.Time) {
	attempt := jscompat.JSNumber(gate.RepairAttempts)
	failure := &ledgers.LeafFailure{
		TaskID: task.ID, Title: fallbackString(task.Title, task.ID),
		Reason:    fallbackString(gate.Reason, "review verdict=fail"),
		Attempt:   &attempt,
		Timestamp: jscompat.JSNumber(now.UnixMilli()),
	}
	if gate.Verdict != nil {
		if gate.Verdict.Confidence != "" {
			confidence := string(gate.Verdict.Confidence)
			failure.Confidence = &confidence
		}
		for _, bug := range gate.Verdict.Bugs {
			var severity, file *string
			var line *jscompat.JSNumber
			if bug.Severity != "" {
				value := bug.Severity
				severity = &value
			}
			if bug.File != "" {
				value := bug.File
				file = &value
			}
			switch value := bug.Line.(type) {
			case float64:
				number := jscompat.JSNumber(value)
				line = &number
			case int:
				number := jscompat.JSNumber(value)
				line = &number
			case jscompat.JSNumber:
				number := value
				line = &number
			}
			failure.Bugs = append(failure.Bugs, ledgers.FailureBug{
				Severity: severity, File: file, Line: line, Detail: bug.Detail,
			})
		}
		failure.RepairHints = append([]string(nil), gate.Verdict.RepairHints...)
	}
	ledgers.RecordLeafFailure(root, failure)
}

func recordMergeFailure(root string, task *plandb.Task, result MergeResult, now time.Time) {
	hints := []string{
		"Multiple sibling leaves likely edited the same file; consider sequencing them or merging the leaves themselves before retrying.",
	}
	if result.ConflictPath != "" {
		hints = append(hints, "Conflict report: "+result.ConflictPath)
	}
	ledgers.RecordLeafFailure(root, &ledgers.LeafFailure{
		TaskID: task.ID, Title: fallbackString(task.Title, task.ID),
		Reason: "merge failed: " + result.Summary, RepairHints: hints,
		Timestamp: jscompat.JSNumber(now.UnixMilli()),
	})
}

func dispatchGateFromResult(gate GateResult, status GateStatus) *DispatchGate {
	result := &DispatchGate{Status: status}
	if gate.Verdict != nil {
		verdict := gate.Verdict.Verdict
		confidence := gate.Verdict.Confidence
		result.Verdict = &verdict
		result.Confidence = &confidence
	}
	if gate.RepairAttempts != 0 {
		value := jscompat.JSNumber(gate.RepairAttempts)
		result.RepairAttempts = &value
	}
	if gate.Reason != "" {
		result.Reason = &gate.Reason
	}
	if gate.AdvisorAction != "" {
		result.AdvisorAction = &gate.AdvisorAction
	}
	return result
}

func (s *Scheduler) writeHandoffs(input SchedulerInput, task *plandb.Task, payload string) {
	taskBody := strings.Join([]string{
		"Scheduler-dispatched task " + task.ID + " (" + task.Title + ") completed.", "", payload,
	}, "\n")
	s.planDB.Run([]string{"plandb", "context", taskBody, "--kind", "handoff", "--task", task.ID, "--json"})
	parent := input.RootTaskID
	if task.ParentTaskID != nil {
		parent = *task.ParentTaskID
	}
	if parent != "" && parent != task.ID {
		parentBody := strings.Join([]string{
			"Scheduler-dispatched task " + task.ID + " (" + task.Title + ") handoff for parent " + parent + ".",
			"", payload,
		}, "\n")
		s.planDB.Run([]string{"plandb", "context", parentBody, "--kind", "handoff", "--task", parent, "--json"})
	}
}

func (s *Scheduler) lookupOutcomeCache(ctx context.Context, task *plandb.Task, modelID string) *outcomecache.CachedOutcome {
	tree := outcomecache.ComputeTreeState(func(argv []string) (outcomecache.ExecResult, error) {
		result := s.runner.Run(ctx, argv, runOptions{Cwd: s.workspace})
		return outcomecache.ExecResult{Stdout: string(result.Stdout), ExitCode: float64(result.Code)}, nil
	})
	if tree == nil {
		return nil
	}
	return outcomecache.LookupVerifiedOutcome(s.workspace, outcomecache.LookupOpts{
		BriefText: task.Title + "\n" + taskDescription(task), TreeState: *tree, ModelID: modelID,
	})
}

func (s *Scheduler) recordOutcomeCache(
	ctx context.Context,
	task *plandb.Task,
	modelID string,
	outcome leafoutcome.LeafOutcome,
	verdict *ReviewVerdict,
	testsPassed bool,
) {
	tree := outcomecache.ComputeTreeState(func(argv []string) (outcomecache.ExecResult, error) {
		result := s.runner.Run(ctx, argv, runOptions{Cwd: s.workspace})
		return outcomecache.ExecResult{Stdout: string(result.Stdout), ExitCode: float64(result.Code)}, nil
	})
	if tree == nil {
		return
	}
	var raw any
	if verdict != nil {
		raw = verdict.Raw
		if raw == nil {
			raw = reviewVerdictMap(*verdict)
		}
	}
	outcome.Evidence = leafoutcome.AuditEvidenceFromVerdict(raw, testsPassed)
	outcomecache.RecordVerifiedOutcome(s.workspace, outcomecache.RecordOpts{
		BriefText: task.Title + "\n" + taskDescription(task), TreeState: *tree,
		ModelID: modelID, Outcome: outcome,
	})
}

func listChangedFiles(runner commandRunner, ctx context.Context, worktree, baseSHA string) []string {
	if worktree == "" || baseSHA == "" {
		return []string{}
	}
	result := runner.Run(ctx, []string{"git", "diff", "--name-only", baseSHA}, runOptions{Cwd: worktree})
	lines := strings.Split(string(result.Stdout), "\n")
	out := []string{}
	for _, line := range lines {
		if value := jscompat.Trim(line); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func failedDispatch(taskID, subagent, agentID, reason string) DispatchResult {
	return DispatchResult{
		TaskID: taskID, SubagentType: subagent, AgentID: agentID,
		Success: false, Error: &reason,
	}
}

func fallbackString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func dispatchStringPointer(value string) *string { return &value }

func safeTaskID(value string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(value, "_")
}

func utf16Prefix(value string, limit int) string {
	if limit < 0 {
		return ""
	}
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}

func gateVerdictText(gate GateResult) string {
	if gate.Verdict != nil && gate.Verdict.Verdict != "" {
		return string(gate.Verdict.Verdict)
	}
	return string(gate.Status)
}

func bandRank(band sizeband.SizeBand) int {
	switch band {
	case sizeband.BandXS:
		return 0
	case sizeband.BandS:
		return 1
	case sizeband.BandM:
		return 2
	case sizeband.BandL:
		return 3
	case sizeband.BandXL:
		return 4
	default:
		return 0
	}
}
