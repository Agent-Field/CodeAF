// This file ports the pipeline driver from swe-pro/src/cli/cmd/run.ts:367-2930.
package codeaf

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/adaptiveflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/architecturegate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/basecontractcheck"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/caprecap"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/contract"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/cutpolicy"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/factsheet"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/fixgenerator"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/frontierplanning"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/hardmode"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/inputclassifier"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/issuewriterphase"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/knobs"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafbriefing"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	observerpkg "github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/plannertranslate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/productgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/prreadyphase"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/replangate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/reviewgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/runbudget"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scriptrunner"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specidentifiers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/stalereaper"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/stucksilence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/validity"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/util"
)

type pipelineDeps struct {
	Backend            backend
	Config             *codeafConfig
	Events             *eventWriter
	Notes              io.Writer
	CPBridge           *cpBridge
	Now                func() time.Time
	Sleep              func(context.Context, time.Duration) error
	ObserverDispatcher observerpkg.Dispatcher
}

type pipeline struct {
	args               cliArgs
	workspace          string
	dbPath             string
	sessionID          string
	runtime            *runtimeAdapter
	pool               poolResolver
	models             phaseModels
	events             *eventWriter
	notes              io.Writer
	cpBridge           *cpBridge
	now                func() time.Time
	sleep              func(context.Context, time.Duration) error
	wallStart          time.Time
	priorCost          float64
	budget             runbudget.RunBudget
	budgetRun          *runbudget.BudgetTracker
	budgetCost         float64
	capability         *capability.CapabilityTracker
	restoredAuditCycle float64
	observer           *observerpkg.Registry
	observerDispatcher observerpkg.Dispatcher

	runSchedulerCycle   func(context.Context, *scheduler.Scheduler, scheduler.SchedulerInput) (scheduler.SchedulerCycleResult, error)
	listPlanTasks       func(*plandb.ListTasksFilter) []*plandb.Task
	inFlightDispatchIDs func() []string
	fingerprintMu       sync.Mutex
	fingerprintFiles    map[string]worktreeFileFingerprint
	fingerprintNonce    uint64

	// verificationTimeouts remembers entrypoints that hung at the harness
	// ceiling so a later audit cycle does not pay the full ceiling again for
	// an identical command against an unchanged tree.
	verificationTimeouts map[string]timedOutEntrypoint

	entryAgent           string
	rootCutLeaf          bool
	rootCutBand          string
	predictedIdentifiers []specidentifiers.SpecIdentifier
	frontierPlanning     *frontierplanning.FrontierPlanningLedger
	initialPlanBlock     string
}

type pipelineOptions struct {
	Resume     bool
	ResumeSeed string
	WallStart  *time.Time
	PriorCost  float64
	LastCycle  float64
}

type rootPlan struct {
	ProjectID        string
	RootID           string
	ProductPath      string
	ArchitecturePath string
	Prepopulated     bool
	TaskCount        int
	EdgeCount        int
	IssueCount       int
}

type pipelineResult struct {
	Status    string
	Reason    string
	Cycle     int
	ProjectID string
	RootID    string
	CostUSD   float64
	WallStart time.Time
}

var errWallClockBudget = errors.New("wall-clock budget exhausted")

// errRunBudget marks a mid-dispatch budget stop. run.ts:1628-1630 checkpoints
// and exits 0 on exhaustion, so this must not surface as a crash.
var errRunBudget = errors.New("run budget exhausted")

// errRootDrainStalled is a resumable terminal work failure. It is deliberately
// distinct from an ordinary error so the CLI can emit fail/exit 0 while keeping
// crashed reserved for failures in the harness itself.
var errRootDrainStalled = errors.New("root graph drain stalled")

func newPipeline(args cliArgs, workspace string, deps pipelineDeps) *pipeline {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	sleep := deps.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	pool := poolResolver{
		high: splitPool(args.High), low: splitPool(args.Low),
		frontier: splitPool(args.Frontier),
	}
	router := initRunRouter(args)
	if aware, ok := deps.Backend.(adaptiveRouterBackend); ok {
		aware.setAdaptiveRouter(router)
	}
	runtime := newConfiguredRuntime(workspace, deps.Backend, deps.Config)
	runtime.pool = pool
	runtime.now = now
	if deps.Events != nil && runtime.bus != nil {
		runtime.unsubscribeEvents = runtime.bus.SubscribeAllCallback(deps.Events.busEvent)
	}
	notes := deps.Notes
	if notes == nil {
		notes = io.Discard
	}
	return &pipeline{
		args: args, workspace: workspace, dbPath: plandb.PlanDBPath(workspace),
		sessionID: runtime.nextID("session"), runtime: runtime, pool: pool,
		models: phaseModels{pool: pool}, events: deps.Events,
		notes: notes, cpBridge: deps.CPBridge,
		now: now, sleep: sleep, wallStart: now(),
		observerDispatcher: deps.ObserverDispatcher,
		frontierPlanning:   frontierplanning.CreateFrontierPlanningLedger(),
		budget: runbudget.ResolveRunBudget(&runbudget.RunBudgetFlags{
			MaxCost: args.MaxCost, MaxHours: args.MaxHours,
		}, nil),
	}
}

func (runner *pipeline) run(
	ctx context.Context, goal string, options pipelineOptions,
) (result pipelineResult, runErr error) {
	runner.frontierPlanning = frontierplanning.CreateFrontierPlanningLedger()
	runner.initialPlanBlock = ""
	result = pipelineResult{Status: "crashed", WallStart: runner.wallStart}
	if options.WallStart != nil {
		runner.wallStart = *options.WallStart
		result.WallStart = runner.wallStart
	}
	runner.priorCost = options.PriorCost
	runner.restoredAuditCycle = options.LastCycle
	runner.budgetRun = runbudget.MakeBudgetTracker(
		runner.budget, float64(runner.wallStart.UnixMilli()), runner.priorCost,
	)
	runner.budgetCost = 0
	if runbudget.IsBounded(runner.budget) {
		cost := "cost=unbounded"
		if runner.budget.MaxCostUSD != nil {
			cost = fmt.Sprintf("maxCost=$%v", *runner.budget.MaxCostUSD)
		}
		wall := "wall=unbounded"
		if runner.budget.MaxWallMS != nil {
			wall = fmt.Sprintf("maxWall=%vh", math.Round(*runner.budget.MaxWallMS/36_000)/100)
		}
		restored := ""
		if runner.priorCost > 0 {
			restored = fmt.Sprintf(" (restored: $%.4f already spent)", runner.priorCost)
		}
		runner.note("[codeaf] run budget: " + cost + " " + wall + restored + "\n")
	}
	if runner.budget.MaxWallMS != nil {
		limit := time.Duration(*runner.budget.MaxWallMS * float64(time.Millisecond))
		deadline := runner.wallStart.Add(limit)
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadlineCause(ctx, deadline, errWallClockBudget)
		defer cancel()
		defer func() {
			if !errors.Is(context.Cause(ctx), errWallClockBudget) || result.Status == "pass" {
				return
			}
			result.Status = "budget-exhausted"
			_, result.Reason = runner.budgetExhausted()
			if result.Reason == "" {
				result.Reason = errWallClockBudget.Error()
			}
			result.CostUSD = runner.totalCost()
			runErr = nil
		}()
	}
	if runner.args.Hard {
		_ = os.Setenv("CODEAF_HARD", "1")
	}
	if err := runner.prepareWorkspace(ctx); err != nil {
		return result, err
	}
	if exhausted, reason := runner.budgetExhausted(); exhausted {
		result.Status, result.Reason = "budget-exhausted", reason
		result.CostUSD = runner.totalCost()
		return result, nil
	}
	baseSHA := gitOutput(ctx, runner.workspace, "rev-parse", "HEAD")
	if baseSHA == "" {
		return result, errors.New("codeaf run requires a git repository with at least one commit")
	}

	var projectID, rootID string
	var plan rootPlan
	runner.entryAgent = runner.args.EntryAgent
	rootCut := false
	if !options.Resume &&
		runner.args.EntryAgent == baked.EntryAgent {
		rootCut = runner.shouldRootCut(goal)
		runner.rootCutLeaf = rootCut
		if rootCut {
			runner.entryAgent = "coder"
		}
	}
	title := prefixUTF16(goal, 60)
	if err := runner.runtime.ensureRootSession(
		ctx, runner.sessionID, title, runner.entryAgent,
	); err != nil {
		return result, err
	}
	// TS run.ts:599-624 constructs and root-tracks the sidecar immediately
	// after creating the entry session. The helper is a nil-only no-op unless
	// CODEAF_OBSERVER is explicitly enabled.
	runner.startObserver(ctx, goal)
	defer runner.stopObserver()

	decomposed := options.Resume ||
		(runner.args.EntryAgent == baked.EntryAgent && !rootCut)
	rootPrompt := goal
	if options.Resume {
		runner.note("[codeaf] resume: re-entering audit-fix loop over persisted checkpoint state\n")
		projectID, rootID = discoverRunRoot()
		if projectID == "" || rootID == "" {
			var err error
			plan, err = runner.bootstrapRootPlan(goal)
			if err != nil {
				return result, err
			}
			projectID, rootID = plan.ProjectID, plan.RootID
		}
		if options.ResumeSeed != "" {
			rootPrompt = options.ResumeSeed
			plandb.GetPlanDB().AddContext(options.ResumeSeed, plandb.AddContextOpts{
				Project: projectID, TaskID: rootID, Kind: "resume",
			})
		}
		runner.events.stage("resume", "rehydrated", map[string]any{
			"project_id": projectID, "root_task_id": rootID,
		})
	} else if decomposed {
		runner.events.stage("root-cut", "decompose", nil)
		var err error
		if os.Getenv("CODEAF_PRE_GATES") == "0" {
			runner.events.stage("pre-gates", "disabled", nil)
		} else {
			plan, err = runner.planAndPopulate(ctx, goal)
		}
		if err != nil {
			return result, err
		}
		projectID, rootID = plan.ProjectID, plan.RootID
	} else if runner.args.EntryAgent == baked.EntryAgent {
		runner.events.stage("root-cut", "selected", nil)
	}

	if os.Getenv("CODEAF_VALIDITY") != "0" && !options.Resume {
		if runner.runIntakeValidityGate(ctx, goal) {
			// run.ts:1091-1109 exits 0 before producing verification. Go's NDJSON
			// terminal still needs a truthful status, so reserve pass for the
			// process-verified path below and classify this deliberate no-op.
			result.Status = "refused"
			result.Reason = "issue judged clearly invalid"
			result.CostUSD = runner.totalCost()
			runner.note("[codeaf] exiting (process.exit 0)\n")
			return result, nil
		}
	}
	if !options.Resume {
		runner.runConventionScout(ctx, goal)
		runner.initialPlanBlock = runner.runPlanArbitration(ctx, goal)
	}
	if decomposed && !options.Resume {
		if plan.ProjectID == "" || plan.RootID == "" {
			bootstrap, err := runner.bootstrapRootPlan(goal)
			if err != nil {
				return result, err
			}
			bootstrap.ProductPath = plan.ProductPath
			bootstrap.ArchitecturePath = plan.ArchitecturePath
			plan = bootstrap
		}
		projectID, rootID = plan.ProjectID, plan.RootID
		rootPrompt = buildRootOrchestratorPrompt(goal, plan)
	}

	if decomposed {
		pump := scheduler.NewScheduler(runner.schedulerOptions(
			adaptiveflag.AdaptiveCutsEnabled(),
		))
		runner.events.stage("root-orchestrator", "running", nil)
		if err := runner.runRootOrchestrator(
			ctx, rootPrompt, projectID, rootID, runner.rootScheduler(pump),
		); err != nil {
			if errors.Is(err, errRunBudget) {
				_, reason := runner.budgetExhausted()
				if reason == "" {
					reason = err.Error()
				}
				result.Status, result.Reason = "budget-exhausted", reason
				result.CostUSD = runner.totalCost()
				result.ProjectID, result.RootID = projectID, rootID
				return result, nil
			}
			if errors.Is(err, errRootDrainStalled) {
				result.Status, result.Reason = "fail", err.Error()
				result.CostUSD = runner.totalCost()
				result.ProjectID, result.RootID = projectID, rootID
				return result, nil
			}
			return result, err
		}
		runner.events.stage("root-orchestrator", "completed", nil)
		if exhausted, reason := runner.budgetExhausted(); exhausted {
			result.Status, result.Reason = "budget-exhausted", reason
			result.CostUSD = runner.totalCost()
			result.ProjectID, result.RootID = projectID, rootID
			return result, nil
		}
	} else if runner.args.EntryAgent != baked.EntryAgent {
		runner.events.stage("entry-agent", "running", map[string]any{"agent": runner.args.EntryAgent})
		if err := runner.runDirectLeaf(ctx, goal, runner.args.EntryAgent, "", ""); err != nil {
			return result, err
		}
		runner.events.stage("entry-agent", "completed", nil)
	} else if rootCut {
		if err := runner.runDirectLeaf(ctx, goal, "coder", "", ""); err != nil {
			return result, err
		}
	}

	finalAudit, cycles, err := runner.auditFixLoop(
		ctx, goal, baseSHA, projectID, rootID,
	)
	result.Cycle = cycles
	result.ProjectID, result.RootID = projectID, rootID
	result.CostUSD = runner.totalCost()
	if err != nil {
		return result, err
	}
	switch finalAudit.Status {
	case auditorgate.StatusPass:
		result.Status = "pass"
	case auditorgate.StatusSkipped:
		result.Status = "pass"
		if finalAudit.Reason != nil {
			result.Reason = *finalAudit.Reason
		}
	case auditorgate.StatusEscalated:
		result.Status = "escalated"
	default:
		result.Status = "fail"
		if finalAudit.Reason != nil {
			result.Reason = *finalAudit.Reason
		} else if finalAudit.Verdict != nil && len(finalAudit.Verdict.Blockers) > 0 {
			result.Reason = finalAudit.Verdict.Blockers[0].Detail
		}
	}
	verificationJustifiedPass := result.Status == "pass"
	verifiedTree, haveVerifiedTree := "", false
	if verificationJustifiedPass {
		verifiedTree, haveVerifiedTree = runner.worktreeFingerprint(ctx)
	}
	runner.runHygieneCleanup(ctx, baseSHA, finalAudit.Status)
	scheduler.ForceSweepWorktrees(ctx, runner.workspace, runner.dbPath, projectID)
	if exhausted, reason := runner.budgetExhausted(); exhausted && result.Status != "pass" {
		result.Status, result.Reason = "budget-exhausted", reason
	}

	if result.Status == "pass" && runner.args.PRReady {
		runner.events.stage("pr-ready", "running", nil)
		prResult, prErr := prreadyphase.RunPRReadyPhase(ctx, prreadyphase.Input{
			Workspace: runner.workspace, ParentSessionID: runner.sessionID,
			PromptOps: runner.runtime, UserPrompt: goal, BaseSHA: baseSHA,
		}, prreadyphase.Dependencies{
			Models: runner.models, Sessions: runner.runtime,
			NewMessageID: func() string { return runner.runtime.nextID("message") },
		})
		if prErr != nil {
			return result, prErr
		}
		runner.events.stage("pr-ready", prResult.Status, nil)
		if prResult.Status == "planner-failed" || prResult.Status == "formatter-failed" {
			result.Status = "fail"
			result.Reason = "pr-ready phase " + prResult.Status
			if prResult.Reason != nil {
				result.Reason += ": " + *prResult.Reason
			}
		}
	}
	if verificationJustifiedPass {
		finalTree, haveFinalTree := runner.worktreeFingerprint(ctx)
		if !haveVerifiedTree || !haveFinalTree {
			result.Status = "fail"
			result.Reason = "could not establish final worktree state after post-audit phases"
		} else if finalTree != verifiedTree {
			// PR-ready and hygiene run after the audit. A prior pass only describes
			// the pre-format tree, so repeat the same process-derived verification
			// floor against the final files before preserving pass.
			const maxStableReverifications = 2
			beforeVerification := finalTree
			for attempt := 1; attempt <= maxStableReverifications; attempt++ {
				runner.note("[codeaf] post-audit tree changed — re-running full project verification\n")
				verification := runner.runProjectVerification(ctx)
				if verification.Failed != nil {
					result.Status = "fail"
					result.Reason = "post-audit verification failed: " + verification.Failure
					break
				}
				afterVerification, haveAfterVerification := runner.worktreeFingerprint(ctx)
				if !haveAfterVerification {
					result.Status = "fail"
					result.Reason = "could not establish worktree state after post-audit verification"
					break
				}
				if afterVerification == beforeVerification {
					break
				}
				if attempt == maxStableReverifications {
					result.Status = "fail"
					result.Reason = "post-audit verification is self-mutating or exceeded the fingerprint budget: the worktree could not stabilize during bounded re-verification"
					break
				}
				beforeVerification = afterVerification
			}
		}
	}
	result.CostUSD = runner.totalCost()
	return result, nil
}

func (runner *pipeline) prepareWorkspace(ctx context.Context) error {
	absolute, err := filepath.Abs(runner.workspace)
	if err != nil {
		return err
	}
	runner.workspace = absolute
	if info, err := os.Stat(absolute); err != nil || !info.IsDir() {
		return fmt.Errorf("workspace is not a directory: %s", absolute)
	}
	if gitOutput(ctx, absolute, "rev-parse", "--show-toplevel") == "" {
		return fmt.Errorf("workspace is not a git repository: %s", absolute)
	}
	// project.ts:286 excludes .codeaf/.plandb artifacts on the MAIN workspace
	// at bootstrap (non-fatal); without it a root-cut run's eager-commits
	// sweep harness artifacts into the repo history.
	if _, err := util.EnsureCodeafExcluded(ctx, absolute); err != nil {
		runner.note("[codeaf] ensureCodeafExcluded failed (non-fatal): " + err.Error() + "\n")
	}
	if os.Getenv("PLANDB_DB") == "" {
		_ = os.Setenv("PLANDB_DB", filepath.Join(absolute, ".plandb.db"))
		runner.dbPath = plandb.PlanDBPath(absolute)
	}
	plandb.OpenPlanDB(runner.dbPath)
	runner.events.stage("bootstrap", "ready", map[string]any{
		"workspace": absolute, "db_path": runner.dbPath,
	})
	return nil
}

func (runner *pipeline) shouldRootCut(goal string) bool {
	if !adaptiveflag.AdaptiveCutsEnabled() {
		return false
	}
	band := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description: goal, Tags: []string{},
	})
	runner.rootCutBand = string(band)
	model := firstModel(runner.pool.high)
	identity := agentjsonModel(model)
	tracker := runner.capabilityTracker(true)
	reliable := tracker.MaxReliableBand(capability.ModelRef{
		ProviderID: identity.ProviderID, ModelID: identity.ModelID,
	})
	return cutpolicy.ShouldRootCut(cutpolicy.ShouldRootCutInput{
		Band: band, Reliable: reliable, HardMode: hardmode.IsHardMode(),
	})
}

func (runner *pipeline) capabilityTracker(adaptive bool) *capability.CapabilityTracker {
	if runner.capability != nil {
		return runner.capability
	}
	defaultTier := capability.TierLow
	runner.capability = capability.CapabilityFromRun(
		filepath.Join(runner.workspace, ".codeaf", "outcomes.jsonl"),
		&capability.CapabilityOptions{
			AdaptiveCutsEnabled: &adaptive,
			TierMap:             buildCapabilityTierMap(runner.pool),
			DefaultTier:         &defaultTier,
		},
	)
	return runner.capability
}

type oneShotPromptModel struct {
	ModelID    string
	ProviderID string
}

type oneShotTextPart struct {
	Type string
	Text string
}

type oneShotToolSettings struct {
	Bash       bool `json:"bash"`
	Read       bool `json:"read"`
	Glob       bool `json:"glob"`
	Grep       bool `json:"grep"`
	Edit       bool `json:"edit"`
	Write      bool `json:"write"`
	ApplyPatch bool `json:"apply_patch"`
}

type oneShotPromptRequest struct {
	MessageID string
	SessionID string
	Model     oneShotPromptModel
	Agent     string
	Tools     *oneShotToolSettings
	Parts     []any
	Workspace string
}

// runIntakeValidityGate ports run.ts:1055-1114. The verdict judge is
// deliberately fail-open: child-session creation, timeout, prompt, and parse
// failures all become a nil verdict and let the run proceed.
func (runner *pipeline) runIntakeValidityGate(ctx context.Context, goal string) bool {
	runner.note("[codeaf] intake validity gate: dispatching validity-judge\n")
	verdict := runner.dispatchValidityJudge(ctx, validity.BuildIntakeValidityPrompt(
		validity.IntakeValidityPromptInput{TaskText: goal},
	))
	if verdict == nil {
		runner.note("[codeaf] intake validity: no parseable verdict — proceeding normally\n")
		return false
	}

	decision := validity.ApplyValidityPolicy(*verdict, validity.PhaseIntake)
	runner.note(fmt.Sprintf(
		"[codeaf] intake validity: status=%s confidence=%s → %s\n",
		verdict.Status, verdict.Confidence, decision.Mode,
	))
	runner.appendDecision(
		ledgers.DecisionTriage,
		"intake-validity",
		string(decision.Mode),
		prefixUTF16(verdict.Evidence, 200),
		"",
	)
	if decision.Mode != validity.ModeHaltInvalid {
		return false
	}

	runner.note("[codeaf] " + decision.Note + "\n")
	runner.note("[codeaf] intake evidence: " + prefixUTF16(verdict.Evidence, 400) + "\n")
	runner.note("[codeaf] halting run: issue judged clearly invalid — NOT dispatching the coder\n")
	return true
}

// dispatchValidityJudge ports the one-shot validity-judge child session from
// run.ts:682-714. The frontier head is preferred and HIGH is the fallback.
func (runner *pipeline) dispatchValidityJudge(
	ctx context.Context, promptText string,
) *validity.ValidityVerdict {
	modelID := firstModel(runner.pool.frontier)
	if modelID == "" {
		modelID = firstModel(runner.pool.high)
	}
	model := agentjsonModel(modelID)
	sessionID, err := runner.runtime.Create(ctx, runner.sessionID, "validity-judge")
	if err != nil {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	response, err := runner.runtime.Prompt(timeoutCtx, oneShotPromptRequest{
		MessageID: runner.runtime.nextID("message"),
		SessionID: sessionID,
		Model: oneShotPromptModel{
			ModelID: model.ModelID, ProviderID: model.ProviderID,
		},
		Agent: "validity-judge",
		Tools: &oneShotToolSettings{},
		Parts: []any{oneShotTextPart{
			Type: "text", Text: promptText,
		}},
		Workspace: runner.workspace,
	})
	if err != nil {
		return nil
	}
	result, ok := response.(turnResult)
	if !ok {
		return nil
	}
	return validity.ParseValidityVerdict(turnText(result))
}

func turnText(result turnResult) string {
	lines := make([]string, 0, len(result.Parts))
	for _, part := range result.Parts {
		if part.Type == "text" {
			lines = append(lines, part.Text)
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return result.Text
}

func (runner *pipeline) appendDecision(
	kind ledgers.DecisionKind,
	taskID string,
	chosen string,
	reason string,
	knobsHash string,
) {
	band := runner.rootCutBand
	if band == "" {
		band = "unknown"
	}
	now := jscompat.JSNumber(float64(runner.now().UnixMilli()))
	model := agentjsonModel(firstModel(runner.pool.high))
	ledgers.AppendDecision(ledgers.AppendDecisionArgs{
		Workspace: runner.workspace,
		Decision: ledgers.BuildDecision(ledgers.BuildDecisionInput{
			Kind: kind, TaskID: taskID, Model: model.ModelID,
			Band: band, ReliableBand: "unknown", Chosen: chosen,
			Reason: reason, KnobsHash: knobsHash, Now: &now,
		}),
	})
}

func (runner *pipeline) note(message string) {
	_, _ = io.WriteString(runner.notes, message)
}

func (runner *pipeline) runDirectLeaf(
	ctx context.Context, goal, agent, projectID, rootID string,
) error {
	markdown, ok := baked.GetBakedAgent(agent)
	if !ok {
		if prompt, configured := runner.runtime.config.agent(agent)["prompt"].(string); configured {
			markdown = prompt
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("unknown entry agent: %s", agent)
	}
	model := agentjsonModel(firstModel(runner.pool.high))
	prompt := goal
	if runner.rootCutLeaf && agent == "coder" {
		prompt = buildRootCutPrompt(goal, runner.workspace)
	}
	if agent == "coder" && runner.initialPlanBlock != "" {
		prompt += "\n\n" + runner.initialPlanBlock
	}
	ctx = project.WithContext(ctx, project.InstanceContext{
		Directory: runner.workspace, Worktree: runner.workspace,
		Project: project.Info{
			ID: project.ID(projectID), Worktree: runner.workspace,
		},
		PlanDB: &project.PlanDB{
			DBPath: runner.dbPath, ProjectID: projectID, RootTaskID: rootID,
		},
	})
	configured, err := runner.runtime.config.configureTurn(turn{
		SessionID: runner.sessionID, SessionTitle: goal,
		Agent: agent, AgentMarkdown: markdown, Workspace: runner.workspace,
		ProviderID: model.ProviderID, ModelID: model.ModelID, Prompt: prompt,
	})
	if err != nil {
		return err
	}
	configured.LowModels = runner.pool.values("low")
	configured.Reminder = "Implement the request end-to-end, verify it, and leave the workspace ready for audit."
	configured.Tools = runner.runtime.definitionsFor(configured.ProviderID, configured.ModelID, agent, nil)
	configured.Execute = runner.runtime.registry.Execute
	configured.SystemInstructions = runner.runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runner.runtime.registry.SystemInstructions
	configured.AfterAssistant = runner.runtime.registry.ClearInstructionClaims
	response, err := runner.runtime.runTurn(ctx, configured)
	runner.runtime.observeTurn(agent, response)
	runner.runtime.addCost(response.CostUSD)
	if err != nil {
		return err
	}
	if strings.TrimSpace(response.Text) == "" && len(response.Parts) == 0 {
		return errors.New("entry agent returned no result")
	}
	return nil
}

func buildRootCutPrompt(goal, workspace string) string {
	factSheet := factsheet.GenerateFactSheet(
		factsheet.GenerateFactSheetOpts{RootDir: workspace},
	).Markdown
	lines := []string{
		"## Definition of done (root-cut fast path)",
		"",
		"This whole request is assigned to you as a single leaf — the harness",
		"judged one strong model can finish it end-to-end without decomposition.",
		"The plandb task-graph tool is disabled for this session (there is no",
		"graph to maintain); do not attempt to call it. Deliver directly:",
		"  - Implement exactly what the request above asks — no more, no less.",
		"  - Build/compile the change and run any relevant tests before finishing.",
		"  - In your final message, summarize what changed and the evidence it works.",
		"",
		"## Acceptance contract (required first step)",
		"",
		"BEFORE you edit any code, define what \"proven done\" means as a single",
		"machine-checkable command, and register it. Concretely:",
		"  1. Write the narrowest acceptance check that FAILS on the current tree",
		"     — a targeted test file or a small script (e.g. a single jest/vitest/",
		"     pytest file, NOT a repo-wide build). It must exercise the exact",
		"     behavior this request asks for.",
		"  2. Register it by writing `.codeaf/contract.json`:",
		"       {\"command\": \"<shell command, e.g. npx jest test/foo.test.js>\",",
		"        \"paths\": [\"<file(s) the CHECK ITSELF lives in>\"],",
		"        \"asserted_paths\": [\"<file(s) the check asserts ABOUT>\"]}",
		"     The two lists are different things and must not overlap:",
		"       - `paths` = the test/script files. The harness copies these into",
		"         a pristine checkout of the starting commit to confirm your",
		"         check really fails there.",
		"       - `asserted_paths` = what you will create or change to make the",
		"         check pass — the deliverable, the fix target. The harness",
		"         NEVER copies these, so the starting commit stays honest.",
		"     DON'T: for \"create a file hello.txt containing X\" with a script",
		"     test-hello.sh that reads it, the ONLY correct split is",
		"     `\"paths\": [\"test-hello.sh\"], \"asserted_paths\": [\"hello.txt\"]`.",
		"     Listing hello.txt in `paths` copies your answer into the starting",
		"     commit, the check passes there, and the harness concludes the work",
		"     was already done and cancels it. Put a file in exactly one list.",
		"  3. Run that command once now and confirm it FAILS (red) — this proves",
		"     the check actually detects the missing behavior.",
		"  4. Only then implement, until the same command PASSES (green).",
		"The harness runs this exact command itself at completion; the run cannot",
		"finish while it fails. Keep it narrow and fast — a check that needs a",
		"full repo build is the wrong check.",
	}
	if factSheet != "" {
		lines = append(lines, "", factSheet)
	}
	lines = append(lines, "", scriptrunner.BuildScriptGuidance())
	return goal + "\n\n" + strings.Join(lines, "\n")
}

func (runner *pipeline) planAndPopulate(
	ctx context.Context, goal string,
) (rootPlan, error) {
	dependencies := runner.agentJSON()
	runner.events.stage("classifier", "running", nil)
	classified, classifyErr := inputclassifier.Dispatch(ctx, inputclassifier.Input{
		Workspace: runner.workspace, ParentSessionID: runner.sessionID,
		UserPrompt: goal,
	}, dependencies)
	classification := inputclassifier.Fallback
	if classifyErr == nil {
		classification = classified.Data
	}
	runner.events.stage("classifier", classification.Class, map[string]any{
		"reason": classification.Reason,
	})

	var productPath *string
	if classification.Class == "vague" {
		runner.events.stage("product", "running", nil)
		product, err := productgate.RunProductGate(ctx, productgate.Input{
			Workspace: runner.workspace, ParentSessionID: runner.sessionID,
			PromptOps: runner.runtime, UserPrompt: goal,
		}, productgate.Dependencies{
			Models: runner.models, Sessions: runner.runtime,
			NewMessageID: func() string { return runner.runtime.nextID("message") },
		})
		if err != nil {
			runner.note("[codeaf] product-gate failed; continuing without PRD\n")
			runner.events.stage("product", "failed", nil)
		} else if product.Status == "wrote" {
			productPath = &product.PRDPath
			runner.events.stage("product", product.Status, nil)
		} else {
			runner.events.stage("product", product.Status, nil)
		}
	}
	if classification.Class == "trivial" {
		return rootPlan{}, nil
	}

	mode := architecturegate.ModeSinglePass
	if classification.Class == "vague" {
		mode = architecturegate.ModeFull
	}
	runner.events.stage("architecture", "running", map[string]any{"mode": mode})
	architecture, err := architecturegate.RunArchitectureGate(ctx, architecturegate.Input{
		Workspace: runner.workspace, ParentSessionID: runner.sessionID,
		PromptOps: runner.runtime, Mode: mode, UserPrompt: goal,
		PRDPath: productPath,
	}, architecturegate.Dependencies{
		Models: runner.models, Sessions: runner.runtime,
		AgentJSON:    dependencies,
		NewMessageID: func() string { return runner.runtime.nextID("message") },
		LookupEnv:    os.LookupEnv, Observer: runner.observerTracker(),
	})
	if err != nil {
		runner.note("[codeaf] architecture-gate crashed; root-orchestrator will plan from scratch\n")
		runner.events.stage("architecture", "failed", nil)
		plan := rootPlan{}
		if productPath != nil {
			plan.ProductPath = *productPath
		}
		return plan, nil
	}
	runner.events.stage("architecture", architecture.Status, nil)
	if architecture.Status == "failed" {
		runner.note("[codeaf] architecture-gate failed; root-orchestrator will plan from scratch\n")
		plan := rootPlan{}
		if productPath != nil {
			plan.ProductPath = *productPath
		}
		return plan, nil
	}

	runner.events.stage("planner", "running", nil)
	translated, translateErr := plannertranslate.DispatchPlannerTranslate(
		ctx, plannertranslate.DispatchPlannerTranslateInput{
			Workspace: runner.workspace, ParentSessionID: runner.sessionID,
			PromptOps: runner.runtime,
		}, dependencies,
	)
	var dag *plannertranslate.DAGData
	if translateErr == nil && len(translated.Data.Tasks) > 0 {
		value := translated.Data
		dag = &value
	}
	if dag == nil {
		dag = plannertranslate.ParseArchitectureToDAG(runner.workspace)
	}
	if dag == nil || len(dag.Tasks) == 0 {
		runner.note("[codeaf] planner-translate produced no executable DAG; root-orchestrator will plan from scratch\n")
		runner.events.stage("planner", "fallback-root", nil)
		plan := rootPlan{}
		if productPath != nil {
			plan.ProductPath = *productPath
		}
		plan.ArchitecturePath = architecture.ArchitecturePath
		return plan, nil
	}
	runner.events.stage("planner", "translated", map[string]any{"tasks": len(dag.Tasks)})

	plan, err := runner.bootstrapRootPlan(goal)
	if err != nil {
		return rootPlan{}, err
	}
	if productPath != nil {
		plan.ProductPath = *productPath
	}
	plan.ArchitecturePath = architecture.ArchitecturePath

	issueTasks := make([]issuewriterphase.DAGTaskInput, 0, len(dag.Tasks))
	for _, task := range dag.Tasks {
		kind := task.Kind
		deps := make([]issuewriterphase.DAGDepInput, 0, len(task.Deps))
		for _, dependency := range task.Deps {
			depKind := dependency.Kind
			deps = append(deps, issuewriterphase.DAGDepInput{
				FromTask: dependency.FromTask, Kind: &depKind,
			})
		}
		issueTasks = append(issueTasks, issuewriterphase.DAGTaskInput{
			TaskKey: task.TaskKey, Title: task.Title, Kind: &kind,
			Tags: task.Tags, Deps: deps,
		})
	}
	issuesDir := filepath.Join(runner.workspace, ".codeaf", "issues")
	runner.events.stage("issue-writer", "running", map[string]any{"tasks": len(issueTasks)})
	issues, issueErr := issuewriterphase.DispatchIssueWriterPhase(
		ctx, issuewriterphase.Input{
			Workspace: runner.workspace, ParentSessionID: runner.sessionID,
			PromptOps: runner.runtime, Tasks: issueTasks, IssuesDir: issuesDir,
			ArchPath:    architecture.ArchitecturePath,
			ProductPath: filepath.Join(runner.workspace, ".codeaf", "plan", "product.md"),
		}, issuewriterphase.Dependencies{
			Models: runner.models, Sessions: runner.runtime,
			NewMessageID: func() string { return runner.runtime.nextID("message") },
			LookupEnv:    os.LookupEnv,
		},
	)
	if issueErr != nil {
		runner.note("[codeaf] issue-writer phase crashed; falling back to inline descriptions\n")
		runner.events.stage("issue-writer", "failed", nil)
	} else {
		if issues.WrittenByTaskKey != nil {
			for index, task := range dag.Tasks {
				if path, ok := issues.WrittenByTaskKey.Get(task.TaskKey); ok {
					dag.Tasks[index].Description = "issue_file: " + path + "\n\n" + task.Description
				}
			}
		}
		plan.IssueCount = issues.Written
		runner.events.stage("issue-writer", "completed", map[string]any{
			"written": issues.Written, "failed": len(issues.Failed),
		})
	}

	applied := plannertranslate.ApplyTranslatedDAG(
		plannertranslate.ApplyTranslatedDAGInput{
			DAG: *dag, Workspace: runner.workspace, DBPath: runner.dbPath,
			ProjectID: plan.ProjectID, RootTaskID: plan.RootID,
		},
	)
	if !applied.OK {
		reason := "planner DAG apply failed; root-orchestrator will plan from scratch"
		if applied.Reason != nil {
			reason = *applied.Reason + "; root-orchestrator will plan from scratch"
		}
		runner.note("[codeaf] " + reason + "\n")
		runner.events.stage("plan-apply", "failed", map[string]any{"reason": reason})
		return plan, nil
	}
	runner.events.stage("plan-apply", "completed", map[string]any{
		"tasks": applied.TaskCount, "edges": applied.EdgeCount,
		"root_task_id": plan.RootID, "project_id": plan.ProjectID,
	})
	plan.Prepopulated = true
	plan.TaskCount = applied.TaskCount
	plan.EdgeCount = applied.EdgeCount
	return plan, nil
}

func (runner *pipeline) dispatchUntilQuiet(
	ctx context.Context, projectID, rootID, resumeSeed string,
) error {
	// Frozen TS src/cli/cmd/run.ts:1287-1532 treats the dispatch loop as the
	// boundary before audit: active PlanDB work is pumped until drained, subject
	// to CODEAF_DISPATCH_MAX_LOOPS. Hitting that bound cannot authorize audit.
	adaptive := adaptiveflag.AdaptiveCutsEnabled()
	schedulerOptions := runner.schedulerOptions(adaptive)
	dispatcher := scheduler.NewScheduler(schedulerOptions)
	maxParallel := envFloat("CODEAF_MAX_PARALLEL", 20)
	input := scheduler.SchedulerInput{
		RootTaskID: rootID, ProjectID: projectID, DBPath: runner.dbPath,
		ParentSessionID: runner.sessionID, PromptOps: runner.runtime,
		MaxParallel: &maxParallel,
	}
	quietSince := runner.now()
	dispatchStarted := quietSince
	lastSignature := ""
	lastActive := -1
	unchangedIterations := 0.0
	maxLoops := envInt("CODEAF_DISPATCH_MAX_LOOPS", 8)
	if os.Getenv("CODEAF_DISPATCH_LOOP") == "0" {
		maxLoops = 1
	}
	for cycle := 1; cycle <= maxLoops; cycle++ {
		if exhausted, reason := runner.budgetExhausted(); exhausted {
			return fmt.Errorf("%w: %s", errRunBudget, reason)
		}
		listTasks := runner.listPlanTasks
		if listTasks == nil {
			listTasks = plandb.GetPlanDB().ListTasks
		}
		tasks := listTasks(&plandb.ListTasksFilter{Project: projectID})
		open := openDescendantTasksFrom(tasks, rootID)
		if len(open) == 0 {
			return nil
		}

		live := map[string]struct{}{}
		for _, taskID := range runner.liveDispatchIDs() {
			live[taskID] = struct{}{}
		}
		eligible := map[string]struct{}{}
		for _, task := range open {
			eligible[task.ID] = struct{}{}
		}
		stale := releaseStaleClaimsExcept(eligible, live)
		released := append(append([]string{}, stale.Claimed...), stale.Running...)
		if len(released) > 0 {
			runner.note(fmt.Sprintf(
				"[codeaf] dispatch-loop: requeued %d stale claimed/running task(s) with no live dispatch: %s\n",
				len(released), strings.Join(released, ", "),
			))
			runner.events.stage("stale-reaper", "released", map[string]any{"tasks": released})
		}
		runner.events.stage("scheduler", "cycle", map[string]any{"cycle": cycle})
		runCycle := runner.runSchedulerCycle
		if runCycle == nil {
			runCycle = scheduler.RunSchedulerCycle
		}
		cycleResult, err := runCycle(ctx, dispatcher, input)
		if err != nil {
			return err
		}
		failed := 0
		for _, dispatch := range cycleResult.Dispatched {
			if !dispatch.Success {
				failed++
			}
		}
		runner.events.stage("scheduler", "cycle-complete", map[string]any{
			"cycle": cycle, "dispatched": len(cycleResult.Dispatched),
			"failed": failed, "scanned": float64(cycleResult.Scanned),
		})
		tasks = listTasks(&plandb.ListTasksFilter{Project: projectID})
		open = openDescendantTasksFrom(tasks, rootID)
		if len(open) == 0 {
			return nil
		}
		active, ready := activeTaskCounts(tasks, rootID)
		liveIDs := runner.liveDispatchIDs()
		if len(cycleResult.Dispatched) == 0 {
			if impossible, diagnosis := dependencyImpossibleDrain(open, tasks, plandb.GetPlanDB().AllDependencies()); impossible && liveOpenDispatchCount(open, liveIDs) == 0 {
				reason := "dependency-impossible: " + diagnosis
				runner.note("[codeaf] dispatch-loop: " + reason + "\n")
				runner.events.stage("scheduler", "dependency-impossible", map[string]any{
					"reason": reason, "open_tasks": openTaskData(open),
				})
				return newRootDrainStallError(open, reason)
			}
		}
		if adaptive && os.Getenv("CODEAF_NO_SILENCE") == "" {
			if active == lastActive {
				unchangedIterations++
			} else {
				unchangedIterations = 0
			}
			lastToolMS, activityErr := runner.runtime.latestToolEventMS(ctx, runner.sessionID)
			if activityErr != nil {
				return activityErr
			}
			nowMS := float64(runner.now().UnixMilli())
			if stucksilence.IsLeafStuck(stucksilence.StuckSilenceInput{
				NowMS: nowMS, StartedAtMS: float64(dispatchStarted.UnixMilli()),
				LastToolEventMS: lastToolMS, UnchangedIterations: unchangedIterations,
			}) {
				reason := fmt.Sprintf(
					"no tool event for %ds (threshold %ds)",
					int((nowMS-lastActivityMS(lastToolMS, dispatchStarted))/1000),
					stucksilence.StuckSilenceMS/1000,
				)
				if unchangedIterations >= stucksilence.StuckHardCap {
					reason = fmt.Sprintf(
						"hard cap (%d unchanged iterations)",
						stucksilence.StuckHardCap,
					)
				}
				runner.note("[codeaf] dispatch-loop: " + reason + " — graph still open\n")
				runner.events.stage("scheduler", "stuck-silence", map[string]any{"reason": reason})
				return newRootDrainStallError(open, reason)
			}
			lastActive = active
		}
		if len(cycleResult.Dispatched) > 0 {
			quietSince = runner.now()
			lastSignature = ""
			continue
		}

		live = map[string]struct{}{}
		for _, taskID := range liveIDs {
			live[taskID] = struct{}{}
		}
		signature := taskSignature(tasks)
		if signature != lastSignature {
			lastSignature, quietSince = signature, runner.now()
		}
		if runner.now().Sub(quietSince) >= stalereaper.QuietStaleReleaseMS*time.Millisecond {
			stale := stalereaper.SelectStaleActiveTasks(stalereaper.StaleReapInput{
				Tasks: staleRows(tasks), NowMS: float64(runner.now().UnixMilli()),
				LiveTaskIDs: live,
			})
			for _, taskID := range stale {
				plandb.GetPlanDB().ReleaseTask(taskID)
			}
			if len(stale) > 0 {
				runner.events.stage("stale-reaper", "released", map[string]any{"tasks": stale})
				quietSince = runner.now()
				continue
			}
		}
		runner.events.stage("scheduler", "quiet-wait", map[string]any{
			"open": len(open), "active": active, "ready": ready,
			"resume_seed": resumeSeed != "",
		})
		if err := runner.sleep(ctx, time.Second); err != nil {
			return err
		}
	}
	listTasks := runner.listPlanTasks
	if listTasks == nil {
		listTasks = plandb.GetPlanDB().ListTasks
	}
	open := openDescendantTasksFrom(
		listTasks(&plandb.ListTasksFilter{Project: projectID}), rootID,
	)
	if len(open) == 0 {
		return nil
	}
	reason := fmt.Sprintf("hit dispatch bound CODEAF_DISPATCH_MAX_LOOPS=%d", maxLoops)
	runner.note("[codeaf] dispatch-loop: " + reason + " with open work\n")
	runner.events.stage("scheduler", "exhausted", map[string]any{
		"max_loops": maxLoops, "open_tasks": openTaskData(open),
	})
	return newRootDrainStallError(open, reason)
}

// dependencyImpossibleDrain is deliberately conservative: it stops only when
// every open row is pending and has a failed/cancelled hard dependency on
// itself or an ancestor. Any ready/active row or pending row without a known
// terminal blocker keeps the normal bounded drain alive.
func dependencyImpossibleDrain(
	open, all []*plandb.Task, dependencies []plandb.Dependency,
) (bool, string) {
	if len(open) == 0 {
		return false, ""
	}
	byID := make(map[string]*plandb.Task, len(all))
	for _, task := range all {
		byID[task.ID] = task
	}
	hardDeps := map[string][]string{}
	for _, dependency := range dependencies {
		if dependency.Kind == plandb.DepFeedsInto || dependency.Kind == plandb.DepBlocks {
			hardDeps[dependency.ToTask] = append(hardDeps[dependency.ToTask], dependency.FromTask)
		}
	}
	details := make([]string, 0, len(open))
	for _, task := range open {
		if task.Status != plandb.StatusPending {
			return false, ""
		}
		blockers := []string{}
		seen := map[string]bool{}
		for current, depth := task.ID, 0; current != "" && !seen[current] && depth < 32; depth++ {
			seen[current] = true
			for _, dependencyID := range hardDeps[current] {
				dependency := byID[dependencyID]
				if dependency != nil && (dependency.Status == plandb.StatusFailed || dependency.Status == plandb.StatusCancelled) {
					blockers = append(blockers, fmt.Sprintf("%s (%s)", dependency.ID, dependency.Status))
				}
			}
			currentTask := byID[current]
			if currentTask == nil || currentTask.ParentTaskID == nil {
				break
			}
			current = *currentTask.ParentTaskID
		}
		if len(blockers) == 0 {
			return false, ""
		}
		sort.Strings(blockers)
		details = append(details, fmt.Sprintf("%s blocked by %s", task.ID, strings.Join(blockers, ", ")))
	}
	sort.Strings(details)
	return true, strings.Join(details, "; ")
}

func lastActivityMS(lastTool *float64, started time.Time) float64 {
	if lastTool != nil {
		return *lastTool
	}
	return float64(started.UnixMilli())
}

func (runner *pipeline) schedulerOptions(adaptive bool) scheduler.SchedulerOptions {
	tracker := runner.capabilityTracker(adaptive)
	review := reviewgate.New(reviewgate.Dependencies{
		AgentJSON: runner.agentJSON(), NewID: runner.runtime.nextID,
	})
	replanner := &replangate.SchedulerService{Dependencies: replangate.Dependencies{
		AgentJSON: runner.agentJSON(),
		NowMillis: func() int64 { return runner.now().UnixMilli() }, LookupEnv: os.LookupEnv,
	}}
	planner := &plannertranslate.SchedulerService{AgentJSON: runner.agentJSON()}
	schedulerOptions := scheduler.SchedulerOptions{
		Workspace: runner.workspace, Agents: bakedAgentRegistry{config: runner.runtime.config}, StepLoop: runner.runtime, Gate: review,
		Replanner: replanner, Planner: planner,
		Pools: schedulerPools{pool: runner.pool}, Provider: schedulerProvider{},
		Capability: tracker, Briefing: leafbriefing.NewDefaultBuilder(), AdaptiveCuts: &adaptive,
		Clauses: liveClauseJudger{runner: runner},
		DefaultMerge: scheduler.MergeStackOptions{
			MergerDispatcher: runtimeMergerDispatcher{runtime: runner.runtime},
			RecoveryClient:   runtimeMergeRecoveryClient{runtime: runner.runtime},
		},
	}
	schedulerOptions.OutcomeObserver = scheduler.BroadcastOutcomeObservers(
		runner.cpBridge, capability.NewOutcomeObserver(tracker),
	)
	return schedulerOptions
}

func missingContractFailure() auditorgate.GateResult {
	step := float64(0)
	reason := "acceptance contract missing or invalid"
	verdict := auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictFail,
		Blockers: []auditorgate.Blocker{{
			Step: &step,
			Detail: "acceptance contract missing or invalid: " + contract.ContractPath +
				" is mandatory on the root-cut path",
		}},
		RepairHints: []string{
			"Write a valid `" + contract.ContractPath + "` with a narrow command that " +
				"failed before the fix and passes now.",
		},
	}
	return auditorgate.GateResult{
		Status: auditorgate.StatusFail, Verdict: &verdict, Reason: &reason,
	}
}

func failingContractFailure(
	registered contract.Contract, result contract.ContractResult,
) auditorgate.GateResult {
	step := float64(0)
	reason := "acceptance contract failing"
	output := suffixUTF16(result.TailOutput, 400)
	if output == "" {
		output = "(no output)"
	}
	verdict := auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictFail,
		Blockers: []auditorgate.Blocker{{
			Step: &step, Detail: "acceptance contract failing: " + output,
		}},
		RepairHints: []string{
			"The acceptance contract you registered (`" + registered.Command +
				"`) must exit 0 before this run can finish. Make it pass, then finish.",
		},
	}
	return auditorgate.GateResult{
		Status: auditorgate.StatusFail, Verdict: &verdict, Reason: &reason,
	}
}

func buildCapabilityTierMap(pool poolResolver) map[string]capability.ModelTierName {
	tiers := map[string]capability.ModelTierName{}
	for _, value := range pool.values("low") {
		tiers[agentjsonModel(value).ModelID] = capability.TierLow
	}
	for _, value := range pool.values("high") {
		tiers[agentjsonModel(value).ModelID] = capability.TierHigh
	}
	return tiers
}

func (runner *pipeline) auditFixLoop(
	ctx context.Context, goal, baseSHA, projectID, rootID string,
) (auditorgate.GateResult, int, error) {
	maxCycles := int(fixgenerator.MaxAuditFixCycles()) + 1
	if rootID == "" {
		maxCycles = 1
	}
	runKnobs := knobs.ResolveProjectKnobs(runner.workspace, nil)
	runKnobsHash := knobs.KnobsSnapshotHash(runKnobs.Values)
	maxCleanupCycles := auditconvergence.AUDIT_CLEANUP_MAX_CYCLES_DEFAULT
	if resolved, ok := runKnobs.Values.Get("AUDIT_CLEANUP_MAX_CYCLES"); ok {
		maxCleanupCycles = resolved
	}
	history := []*auditconvergence.ConvergenceCycle{}
	baseContractCheckDone := false
	previousTreeFingerprint := ""
	havePreviousTreeFingerprint := false
	var contractReviewBlockText *string
	var final auditorgate.GateResult
	persistedCycle := fixgenerator.ReadAuditCycles(runner.workspace)
	if runner.restoredAuditCycle > persistedCycle {
		persistedCycle = runner.restoredAuditCycle
	}
	startCycle := int(math.Floor(persistedCycle)) + 1
	if rootID == "" {
		startCycle = 1
	}
	if startCycle > maxCycles {
		maxCycles = startCycle
	}
	for cycle := startCycle; cycle <= maxCycles; cycle++ {
		if os.Getenv("CODEAF_AUDITOR") == "0" {
			verification := runner.runProjectVerification(ctx)
			if verification.Failed != nil {
				failure := projectVerificationFailure(verification)
				if failure.Verdict != nil {
					if err := auditorgate.PersistVerdict(runner.workspace, *failure.Verdict); err != nil {
						return failure, cycle, err
					}
				}
				return failure, cycle, nil
			}
			value := projectVerificationPassVerdict(verification)
			if err := auditorgate.PersistVerdict(runner.workspace, value); err != nil {
				return auditorgate.GateResult{}, cycle, err
			}
			return auditorgate.GateResult{
				Status: auditorgate.StatusPass, Verdict: &value,
			}, cycle, nil
		}

		var registered *contract.Contract
		var contractResult *contract.ContractResult
		var contractEvidence *string
		var contractFailure *auditorgate.GateResult
		if runner.rootCutLeaf && os.Getenv("CODEAF_CONTRACT") != "0" {
			registered = contract.ReadContract(runner.workspace)
			if registered == nil {
				value := missingContractFailure()
				contractFailure = &value
				runner.note(
					"[codeaf] acceptance contract MISSING OR INVALID " +
						"(" + contract.ContractPath + " is required)\n",
				)
			} else {
				value := contract.RunContract(runner.workspace, *registered)
				contractResult = &value
				timeoutNote := ""
				if value.TimedOut {
					timeoutNote = ", TIMED OUT"
				}
				status := "FAILING"
				if value.Pass {
					status = "PASSED"
				}
				runner.note(
					"[codeaf] acceptance contract " + status +
						" (exit=" + jscompat.FormatNumber(value.ExitCode) +
						", " + jscompat.FormatNumber(value.DurationMs) + "ms" +
						timeoutNote + ")\n",
				)
				if value.Pass {
					evidence := contract.ContractEvidenceBlock(*registered, value)
					contractEvidence = &evidence
				} else {
					failure := failingContractFailure(*registered, value)
					contractFailure = &failure
				}
			}
		}

		// TS src/baked/agents/auditor.md Step 2/2b requires the primary build
		// and standard test entrypoints, not a collection of targeted checks.
		// Enforce that protocol as a machine floor before the model audit so an
		// omitted root suite can never be accepted (runF parity regression).
		verification := projectVerificationResult{}
		var verificationFailure *auditorgate.GateResult
		if contractFailure == nil {
			verification = runner.runProjectVerification(ctx)
			if verification.Failed != nil {
				failure := projectVerificationFailure(verification)
				verificationFailure = &failure
				runner.note(
					"[codeaf] full project verification failing — skipping audit LLM, routing to repair\n",
				)
			}
		}

		treeFingerprint, haveTreeFingerprint := runner.auditTreeFingerprint(ctx)
		treeUnchanged := havePreviousTreeFingerprint && haveTreeFingerprint &&
			treeFingerprint == previousTreeFingerprint
		if haveTreeFingerprint {
			previousTreeFingerprint = treeFingerprint
			havePreviousTreeFingerprint = true
		}
		if contractReviewBlockText == nil {
			if block, attempted := runner.runContractReview(ctx, goal, registered, contractResult); attempted {
				contractReviewBlockText = &block
			}
		}
		if contractEvidence != nil && contractReviewBlockText != nil && *contractReviewBlockText != "" {
			combined := *contractEvidence + "\n\n" + *contractReviewBlockText
			contractEvidence = &combined
		}

		if registered != nil &&
			contractResult != nil &&
			contractResult.Pass &&
			cycle == 1 &&
			!baseContractCheckDone &&
			baseSHA != "" &&
			os.Getenv("CODEAF_VALIDITY") != "0" {
			baseContractCheckDone = true
			baseResult, baseCopies := runner.runBaseContractCheck(ctx, baseSHA, *registered)
			if baseResult != nil && baseResult.Pass {
				runner.note(
					"[codeaf] contract-cannot-fail: contract PASSES at base too — the acceptance " +
						"check never failed anywhere; adjudicating staleness\n",
				)
				for _, fact := range baseCopies {
					if !fact.ExistedAtBase {
						runner.note(
							"[codeaf] contract-cannot-fail: " + fact.Path +
								" did not exist at base and was copied in for the check\n",
						)
					}
				}
				staleVerdict := runner.dispatchValidityJudge(
					ctx,
					validity.BuildStalenessPrompt(validity.StalenessPromptInput{
						TaskText: goal, ContractCommand: registered.Command,
						ContractOutput: baseResult.TailOutput,
						CopiedFiles:    baseCopyEvidence(baseCopies),
					}),
				)
				if staleVerdict == nil {
					runner.note("[codeaf] staleness: no parseable verdict — proceeding normally\n")
				} else {
					decision := validity.ApplyValidityPolicy(
						*staleVerdict, validity.PhaseContract,
					)
					runner.note(fmt.Sprintf(
						"[codeaf] staleness verdict: status=%s confidence=%s → %s\n",
						staleVerdict.Status, staleVerdict.Confidence, decision.Mode,
					))
					runner.appendDecision(
						ledgers.DecisionTriage,
						"contract-staleness",
						string(decision.Mode),
						prefixUTF16(staleVerdict.Evidence, 200),
						"",
					)
					if decision.Mode == validity.ModeStaleReport {
						runner.note("[codeaf] " + decision.Note + "\n")
						runner.note(
							"[codeaf] re-scoping the deliverable to report + regression test " +
								"(no behavioral change) and re-prompting the coder once\n",
						)
						if err := runner.repromptStaleReport(ctx, *staleVerdict); err != nil {
							runner.note(
								"[codeaf] stale-report re-prompt failed: " +
									prefixUTF16(err.Error(), 200) + "\n",
							)
						}
						// The TypeScript `continue` happens before the audit and
						// therefore does not consume an audit-cycle number.
						cycle--
						continue
					}
				}
			} else {
				status := "check skipped (worktree/copy step failed)"
				if baseResult != nil {
					status = "contract FAILS (bug reproduces at base) — proceeding normally"
				}
				runner.note("[codeaf] contract-cannot-fail: base " + status + "\n")
			}
		}

		runner.events.stage("audit", "running", map[string]any{"cycle": cycle})
		auditCycle := float64(cycle)
		gateInput := auditorgate.GateInput{
			Workspace: runner.workspace, UserPrompt: goal,
			ParentSessionID: runner.sessionID, BaseSHA: &baseSHA,
			AuditCycle: &auditCycle, ContractEvidence: contractEvidence,
			ContractPassed: contractResult != nil && contractResult.Pass,
			History:        history, MaxCleanupCycles: &maxCleanupCycles,
			FixedPoint: &auditconvergence.FixedPointEvidence{
				TreeUnchanged:  treeUnchanged,
				ContractPassed: contractResult != nil && contractResult.Pass,
			},
		}
		if verification.Prompt != "" {
			gateInput.VerificationEvidence = &verification.Prompt
		}
		if runner.rootCutBand != "" {
			band := runner.rootCutBand
			gateInput.SizeBand = &band
		}
		if runner.rootCutLeaf {
			light := true
			gateInput.Light = &light
		}
		var audit auditorgate.GateResult
		if contractFailure != nil {
			runner.note("[codeaf] acceptance contract failing — skipping audit LLM, routing to repair\n")
			audit = *contractFailure
		} else if verificationFailure != nil {
			audit = *verificationFailure
		} else {
			var err error
			audit, err = auditorgate.GateSession(
				ctx, gateInput, runner.auditorDependencies(),
			)
			if err != nil {
				return final, cycle, err
			}
		}
		if audit.Status == auditorgate.StatusPass && hardmode.IsHardMode() {
			runner.note("[codeaf] hard mode: pass verdict — running independent confirmation audit\n")
			full := false
			confirmInput := gateInput
			confirmInput.Light = &full
			confirm, confirmErr := auditorgate.GateSession(
				ctx, confirmInput, runner.auditorDependencies(),
			)
			if confirmErr != nil {
				runner.note("[codeaf] confirmation audit crashed; keeping the original pass\n")
			} else {
				before, after := auditBlockerCount(audit), auditBlockerCount(confirm)
				ledgers.AppendCycleRecord(runner.workspace, ledgers.CycleInput{
					Kind: ledgers.CycleConfirmation, Cycle: auditCycle,
					BlockersBefore: float64(before), BlockersAfter: floatPointer(float64(after)),
					Verdict: string(confirm.Status),
				})
				originalBlockers := auditBlockerDetails(audit)
				confirmBlockers := auditBlockerDetails(confirm)
				if len(originalBlockers) > 0 && len(confirmBlockers) > 0 {
					overlap := caprecap.MatchFindings(originalBlockers, confirmBlockers)
					estimate := caprecap.EstimateResidualDefects(caprecap.ResidualInput{
						Sample1: jscompat.JSNumber(len(originalBlockers)),
						Sample2: jscompat.JSNumber(len(confirmBlockers)),
						Overlap: jscompat.JSNumber(overlap),
					})
					runner.note(fmt.Sprintf(
						"[codeaf] capture-recapture: ~%v residual defects estimated (n1=%d n2=%d overlap=%d)\n",
						estimate.EstimatedResidual, len(originalBlockers), len(confirmBlockers), overlap,
					))
				}
				if confirm.Status == auditorgate.StatusFail && confirm.Verdict != nil &&
					len(confirm.Verdict.Blockers) > 0 {
					runner.note("[codeaf] confirmation audit dissents (fail); continuing fix loop with its blockers\n")
					audit = confirm
				} else if confirm.Status == auditorgate.StatusFail {
					runner.note("[codeaf] confirmation audit dissents but has no actionable blockers; keeping the original pass\n")
				} else {
					runner.note("[codeaf] confirmation audit agrees; pass verdict confirmed\n")
				}
			}
		}
		if audit.Verdict == nil && audit.Status == auditorgate.StatusSkipped &&
			len(verification.Commands) > 0 {
			value := projectVerificationPassVerdict(verification)
			audit.Verdict = &value
		}
		verificationRefuted := false
		if audit.Verdict != nil && len(verification.Commands) > 0 {
			reconciled, refuted := auditorgate.ReconcileHarnessVerification(
				*audit.Verdict, verification.Commands,
			)
			audit.Verdict = &reconciled
			if refuted {
				// run.ts:1905-1947 treats the gate result as the effective
				// cycle outcome. Keep status aligned when process evidence proves
				// that the sole Step 2 blocker asserted a check never happened.
				audit.Status = auditorgate.StatusPass
				audit.Reason = nil
				verificationRefuted = true
				runner.note("[codeaf] harness verification refuted the auditor's sole not-verified blocker\n")
			}
		}
		audit = runner.applyAuditGuards(ctx, goal, baseSHA, audit)
		if audit.Verdict != nil {
			if err := auditorgate.PersistVerdict(runner.workspace, *audit.Verdict); err != nil {
				return audit, cycle, err
			}
			if verificationRefuted {
				blockers := make([]string, 0, len(audit.Verdict.Blockers))
				for _, blocker := range audit.Verdict.Blockers {
					blockers = append(blockers, blocker.Detail)
				}
				ledgers.ReconcileVerdictBlockers(runner.workspace, auditCycle, blockers)
				if head := gitOutput(ctx, runner.workspace, "rev-parse", "HEAD"); head != "" {
					_ = auditorgate.WriteAuditProvenance(runner.workspace, auditorgate.AuditProvenance{
						AuditSHA: head, AuditCycle: auditCycle, Verdict: *audit.Verdict,
					})
				}
			}
		}
		if audit.Verdict != nil {
			assessment := auditorgate.AssessVerdictConvergence(
				*audit.Verdict, history, gateInput.FixedPoint, gateInput.MaxCleanupCycles,
			)
			audit.Convergence = &assessment
		}
		final = audit
		if audit.Convergence != nil {
			runner.appendDecision(
				ledgers.DecisionConvergence,
				"session-audit",
				string(audit.Convergence.Action),
				audit.Convergence.Reason,
				runKnobsHash,
			)
		}
		if audit.Verdict != nil {
			history = append(history, convergenceCycle(*audit.Verdict))
		}
		status := string(audit.Status)
		runner.events.stage("audit", status, map[string]any{"cycle": cycle})
		blockers := 0
		if audit.Verdict != nil {
			blockers = len(audit.Verdict.Blockers)
		}
		after := float64(blockers)
		cost := runner.totalCost()
		wall := float64(runner.now().Sub(runner.wallStart).Milliseconds())
		ledgers.AppendCycleRecord(runner.workspace, ledgers.CycleInput{
			Kind: ledgers.CycleKind("audit-fix"), Cycle: auditCycle,
			BlockersBefore: float64(blockers), Verdict: status,
			BlockersAfter: &after, CostUsdApprox: &cost, WallMs: &wall,
		})
		if audit.Status == auditorgate.StatusPass ||
			audit.Status == auditorgate.StatusSkipped ||
			audit.Status == auditorgate.StatusEscalated ||
			audit.Verdict == nil || rootID == "" {
			return final, cycle, nil
		}
		if cycle >= maxCycles {
			return final, cycle, nil
		}
		if exhausted, _ := runner.budgetExhausted(); exhausted {
			return final, cycle, nil
		}
		fixVerdictForGen := runner.withRootCauseDiagnosis(
			ctx, goal, baseSHA, cycle, *audit.Verdict, contractResult,
		)
		runner.events.stage("fix-generator", "running", map[string]any{"cycle": cycle})
		decision, err := fixgenerator.DispatchFixGenerator(
			ctx, fixgenerator.DispatchInput{
				Workspace: runner.workspace, ParentSessionID: runner.sessionID,
				UserGoal: goal, Verdict: fixVerdictForGen, Cycle: auditCycle,
			}, fixgenerator.Dependencies{
				AgentJSON: runner.agentJSON(), PlanDB: fixgenerator.NativePlanDBRunner{},
			},
		)
		if err != nil {
			return final, cycle, err
		}
		added := runner.applyFixes(projectID, rootID, decision.Data)
		runner.events.stage("fix-generator", string(decision.Data.Action), map[string]any{
			"added": added,
		})
		if added == 0 {
			return final, cycle, nil
		}
		if err := fixgenerator.WriteAuditCycles(runner.workspace, float64(cycle)); err != nil {
			return final, cycle, err
		}
		pump := scheduler.NewScheduler(runner.schedulerOptions(
			adaptiveflag.AdaptiveCutsEnabled(),
		))
		if err := runner.runRootOrchestrator(
			ctx, buildAuditFixRootPrompt(cycle, *audit.Verdict),
			projectID, rootID, runner.rootScheduler(pump),
		); err != nil {
			return final, cycle, err
		}
	}
	return final, maxCycles, nil
}

func (runner *pipeline) repromptStaleReport(
	ctx context.Context, verdict validity.ValidityVerdict,
) error {
	model := agentjsonModel(firstModel(runner.pool.high))
	_, err := runner.runtime.Prompt(ctx, oneShotPromptRequest{
		MessageID: runner.runtime.nextID("message"),
		SessionID: runner.sessionID,
		Model: oneShotPromptModel{
			ModelID: model.ModelID, ProviderID: model.ProviderID,
		},
		Agent: runner.entryAgent,
		Parts: []any{oneShotTextPart{
			Type: "text", Text: validity.StaleReportContextBlock(verdict),
		}},
		Workspace: runner.workspace,
	})
	return err
}

// baseCopyEvidence renders the copy facts for the staleness prompt, newest
// concern first: a file that did not exist at base is the one that can make the
// check pass for the wrong reason.
func baseCopyEvidence(facts []baseCopyFact) []validity.CopiedFile {
	if len(facts) == 0 {
		return nil
	}
	out := make([]validity.CopiedFile, 0, len(facts))
	for _, fact := range facts {
		out = append(out, validity.CopiedFile{
			Path: fact.Path, ExistedAtBase: fact.ExistedAtBase,
		})
	}
	return out
}

// baseCopyFact records whether a file copied into the base worktree existed at
// the base commit, so the staleness judge can tell a legitimately new
// regression test from a deliverable that was handed to the check.
type baseCopyFact struct {
	Path          string
	ExistedAtBase bool
}

// runBaseContractCheck mirrors run.ts:1739-1771. Every filesystem, git, copy,
// and process failure folds to nil so uncertainty can never manufacture a
// staleness verdict. It also returns what it copied into the base worktree, so
// a PASSING base result can be adjudicated against how that pass was obtained.
func (runner *pipeline) runBaseContractCheck(
	ctx context.Context, baseSHA string, registered contract.Contract,
) (*contract.ContractResult, []baseCopyFact) {
	var copied []baseCopyFact
	worktree, err := os.MkdirTemp("", "codeaf-basewt-")
	if err != nil {
		return nil, copied
	}
	defer func() { _ = os.RemoveAll(worktree) }()

	add := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktree, baseSHA)
	add.Dir = runner.workspace
	if err := add.Run(); err != nil {
		return nil, copied
	}
	defer func() {
		remove := exec.Command(
			"git", "worktree", "remove", "--force", worktree,
		)
		remove.Dir = runner.workspace
		_ = remove.Run()
	}()

	entries, excluded := basecontractcheck.PlanContractCopiesWithExclusions(
		basecontractcheck.PlanContractCopiesInput{
			Workspace: runner.workspace, Worktree: worktree,
			Paths: registered.Paths, AssertedPaths: registered.AssertedPaths,
		},
	)
	for _, rel := range excluded {
		runner.note(
			"[codeaf] base contract check: NOT copying " + rel +
				" into the base worktree — the contract declares it asserted " +
				"(listed in both paths and asserted_paths)\n",
		)
	}
	basecontractcheck.ApplyContractCopies(entries)
	// Whether each copied file existed at the base commit is the fact that
	// separates "the bug never reproduced" from "we handed the check its
	// answer". A file absent at base and copied in is normal for a freshly
	// written regression test and fatal for a deliverable, and only the
	// adjudicating judge can tell which one it is looking at.
	copied = runner.describeBaseCopies(ctx, baseSHA, entries)
	registered.Command = pinContractToWorktreeSources(worktree, registered.Command)
	result := contract.RunContract(worktree, registered)
	return &result, copied
}

// describeBaseCopies reports, for each file copied into the base worktree,
// whether that path existed at the base commit.
func (runner *pipeline) describeBaseCopies(
	ctx context.Context, baseSHA string, entries []basecontractcheck.CopyPlanEntry,
) []baseCopyFact {
	facts := make([]baseCopyFact, 0, len(entries))
	for _, entry := range entries {
		check := exec.CommandContext(ctx, "git", "cat-file", "-e", baseSHA+":"+entry.Rel)
		check.Dir = runner.workspace
		facts = append(facts, baseCopyFact{
			Path: entry.Rel, ExistedAtBase: check.Run() == nil,
		})
	}
	return facts
}

// pinContractToWorktreeSources makes the base worktree's sources win over an
// editable install of the project under test.
//
// `pip install -e .` — the standard dev setup, and what the benchmark harness
// does — drops a .pth file into site-packages holding the ABSOLUTE path of the
// main checkout's source root. Running the contract with cwd set to a detached
// base worktree does not change that: `import werkzeug` still resolves to the
// main checkout, which by this point contains the agent's fix. The base check
// then passes, the harness concludes the acceptance check "never failed
// anywhere", the staleness judge rules the issue already fixed, and the run is
// re-scoped to "report + regression test, no behavioral change" — dropping the
// fix for a bug that was entirely real. werkzeug-3146 did exactly this.
//
// PYTHONPATH entries precede site-packages in sys.path, so prepending the
// worktree's source roots restores the intended reading. The export is scoped
// to this one command (contract commands run under `bash -lc`) rather than the
// harness process, so concurrent runs cannot see it.
func pinContractToWorktreeSources(worktree, command string) string {
	isPython := false
	for _, marker := range []string{"pyproject.toml", "setup.py", "setup.cfg", "tox.ini"} {
		if _, err := os.Stat(filepath.Join(worktree, marker)); err == nil {
			isPython = true
			break
		}
	}
	if !isPython {
		return command
	}
	roots := []string{}
	if info, err := os.Stat(filepath.Join(worktree, "src")); err == nil && info.IsDir() {
		roots = append(roots, filepath.Join(worktree, "src"))
	}
	roots = append(roots, worktree)
	quoted := make([]string, 0, len(roots))
	for _, root := range roots {
		quoted = append(quoted, strings.ReplaceAll(root, `'`, `'\''`))
	}
	return "export PYTHONPATH='" + strings.Join(quoted, ":") +
		"'${PYTHONPATH:+:$PYTHONPATH}; " + command
}

func (runner *pipeline) auditTreeFingerprint(ctx context.Context) (string, bool) {
	head := gitOutput(ctx, runner.workspace, "rev-parse", "HEAD")
	if head == "" {
		return "", false
	}
	status := gitOutput(ctx, runner.workspace, "status", "--porcelain")
	return head + "|" + status, true
}

const (
	worktreeFingerprintMaxFiles = 4096
	worktreeFingerprintMaxBytes = 8 * 1024 * 1024
	worktreeFingerprintTimeout  = 2 * time.Second
)

type worktreeFileFingerprint struct {
	Mode        os.FileMode
	Size        int64
	ModTimeNano int64
	Missing     bool
	ContentHash string
}

// worktreeFingerprint hashes the content and modes of every tracked or
// unignored file. Unlike `git status --porcelain`, it detects a formatter
// changing the bytes of an already-modified file; unlike HEAD+diff, it does not
// mistake a history-only rewrite with an identical checked-out tree for a
// source mutation. File count, bytes, and wall time are bounded. Metadata lets
// unchanged files reuse their prior content hash; only new or metadata-changed
// files are read again.
func (runner *pipeline) worktreeFingerprint(ctx context.Context) (string, bool) {
	runner.fingerprintMu.Lock()
	defer runner.fingerprintMu.Unlock()
	fingerprintCtx, cancel := context.WithTimeout(ctx, worktreeFingerprintTimeout)
	defer cancel()
	command := exec.CommandContext(fingerprintCtx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Dir = runner.workspace
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return "", false
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, worktreeFingerprintMaxBytes+1))
	if len(raw) > worktreeFingerprintMaxBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return runner.overBudgetFingerprint(), true
	}
	waitErr := command.Wait()
	if fingerprintCtx.Err() != nil && ctx.Err() == nil {
		return runner.overBudgetFingerprint(), true
	}
	if readErr != nil || waitErr != nil {
		return "", false
	}
	paths := strings.Split(string(raw), "\x00")
	if len(paths) > 0 && paths[len(paths)-1] == "" {
		paths = paths[:len(paths)-1]
	}
	sort.Strings(paths)
	if len(paths) > worktreeFingerprintMaxFiles {
		return runner.overBudgetFingerprint(), true
	}
	remainingBytes := int64(worktreeFingerprintMaxBytes - len(raw))
	digest := sha256.New()
	nextFiles := make(map[string]worktreeFileFingerprint, len(paths))
	for _, relative := range paths {
		if fingerprintCtx.Err() != nil {
			if ctx.Err() == nil {
				return runner.overBudgetFingerprint(), true
			}
			return "", false
		}
		path := filepath.Join(runner.workspace, filepath.FromSlash(relative))
		info, statErr := os.Lstat(path)
		_, _ = digest.Write([]byte(relative + "\x00"))
		if os.IsNotExist(statErr) {
			state := worktreeFileFingerprint{Missing: true}
			nextFiles[relative] = state
			_, _ = digest.Write([]byte("missing\x00" + state.ContentHash + "\x00"))
			continue
		}
		if statErr != nil {
			return "", false
		}
		state := worktreeFileFingerprint{
			Mode: info.Mode(), Size: info.Size(), ModTimeNano: info.ModTime().UnixNano(),
		}
		cached, haveCached := runner.fingerprintFiles[relative]
		metadataUnchanged := haveCached && !cached.Missing && cached.Mode == state.Mode &&
			cached.Size == state.Size && cached.ModTimeNano == state.ModTimeNano
		if metadataUnchanged {
			state.ContentHash = cached.ContentHash
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if !metadataUnchanged {
				target, linkErr := os.Readlink(path)
				if linkErr != nil {
					return "", false
				}
				remainingBytes -= int64(len(target))
				if remainingBytes < 0 {
					return runner.overBudgetFingerprint(), true
				}
				sum := sha256.Sum256([]byte(target))
				state.ContentHash = fmt.Sprintf("%x", sum[:])
			}
		case info.Mode().IsRegular():
			if !metadataUnchanged {
				contentHash, consumed, ok := boundedFileHash(fingerprintCtx, path, remainingBytes)
				if !ok {
					return runner.overBudgetFingerprint(), true
				}
				remainingBytes -= consumed
				state.ContentHash = contentHash
			}
		}
		nextFiles[relative] = state
		_, _ = digest.Write([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00", state.Mode, state.Size, state.ModTimeNano, state.ContentHash)))
	}
	runner.fingerprintFiles = nextFiles
	return fmt.Sprintf("%x", digest.Sum(nil)), true
}

func (runner *pipeline) overBudgetFingerprint() string {
	runner.fingerprintNonce++
	return fmt.Sprintf("changed:worktree-fingerprint-budget:%d", runner.fingerprintNonce)
}

func boundedFileHash(ctx context.Context, path string, remaining int64) (string, int64, bool) {
	if remaining < 0 {
		return "", 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer file.Close()
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	limited := io.LimitReader(file, remaining+1)
	consumed := int64(0)
	for {
		if ctx.Err() != nil {
			return "", consumed, false
		}
		read, readErr := limited.Read(buffer)
		if read > 0 {
			consumed += int64(read)
			_, _ = digest.Write(buffer[:read])
			if consumed > remaining {
				return "", consumed, false
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", consumed, false
		}
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), consumed, true
}

func auditBlockerCount(result auditorgate.GateResult) int {
	if result.Verdict == nil {
		return 0
	}
	return len(result.Verdict.Blockers)
}

func auditBlockerDetails(result auditorgate.GateResult) []string {
	if result.Verdict == nil {
		return nil
	}
	out := make([]string, 0, len(result.Verdict.Blockers))
	for _, blocker := range result.Verdict.Blockers {
		prefix := ""
		if blocker.File != nil {
			prefix = *blocker.File
			if blocker.Line != nil {
				prefix += ":" + jscompat.FormatNumber(*blocker.Line)
			}
			prefix += " — "
		}
		out = append(out, prefix+blocker.Detail)
	}
	return out
}

func floatPointer(value float64) *float64 { return &value }

func convergenceCycle(
	verdict auditorgate.AuditorVerdict,
) *auditconvergence.ConvergenceCycle {
	blockers := make([]*auditconvergence.SeverityBlocker, 0, len(verdict.Blockers))
	for _, blocker := range verdict.Blockers {
		blockers = append(blockers, &auditconvergence.SeverityBlocker{
			File: blocker.File, Line: blocker.Line, Step: blocker.Step,
			Detail: blocker.Detail, Severity: blocker.Severity,
		})
	}
	partition := auditconvergence.PartitionBlockers(blockers)
	keys := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		keys = append(keys, auditconvergence.BlockerKey(blocker))
	}
	return &auditconvergence.ConvergenceCycle{
		CorrectnessCount: float64(len(partition.Correctness)),
		HygieneCount:     float64(len(partition.Hygiene)),
		PolishCount:      float64(len(partition.Polish)),
		BlockerKeys:      keys,
	}
}

func (runner *pipeline) applyFixes(
	projectID, rootID string, decision fixgenerator.FixGeneratorDecision,
) int {
	if decision.Action != fixgenerator.ActionDispatchFixes {
		return 0
	}
	added := 0
	for _, fix := range decision.Fixes {
		deps := []plandb.DepSpec{}
		for _, dependency := range fix.Deps {
			if plandb.GetPlanDB().GetTask(dependency) != nil {
				kind := plandb.DepFeedsInto
				deps = append(deps, plandb.DepSpec{TaskID: dependency, Kind: &kind})
			}
		}
		description := fix.Description
		if !strings.Contains(description, "agent:") {
			description = "agent: fixer\n" + description
		}
		if _, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
			Title: fix.Title, Kind: plandb.TaskKind(fix.Kind),
			Description: &description, Project: projectID, Parent: rootID,
			Deps: deps, Tags: []string{"agent:fixer", "audit:fix"},
		}); err == nil {
			added++
		}
	}
	return added
}

func (runner *pipeline) agentJSON() agentjson.Dependencies {
	return agentjson.Dependencies{
		Resolver: runner.pool, Client: runner.runtime, NewID: runner.runtime.nextID,
	}
}

func (runner *pipeline) totalCost() float64 {
	runner.ensureBudgetTracker()
	runtimeCost := runner.runtime.cost()
	if delta := runtimeCost - runner.budgetCost; delta > 0 {
		runner.budgetRun.AddCost(delta)
	}
	runner.budgetCost = runtimeCost
	return runner.budgetRun.CostUSD()
}

func (runner *pipeline) budgetExhausted() (bool, string) {
	runner.totalCost()
	exhausted := runner.budgetRun.Exhausted(float64(runner.now().UnixMilli()))
	if exhausted.Yes {
		reason := "run budget exhausted"
		if exhausted.Reason != nil {
			reason = *exhausted.Reason
		}
		return true, reason
	}
	return false, ""
}

func (runner *pipeline) ensureBudgetTracker() {
	if runner.budgetRun != nil {
		return
	}
	if !runbudget.IsBounded(runner.budget) {
		runner.budget = runbudget.ResolveRunBudget(&runbudget.RunBudgetFlags{
			MaxCost: runner.args.MaxCost, MaxHours: runner.args.MaxHours,
		}, nil)
	}
	runner.budgetRun = runbudget.MakeBudgetTracker(
		runner.budget, float64(runner.wallStart.UnixMilli()), runner.priorCost,
	)
}

func firstModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func agentjsonModel(value string) agentjson.Model {
	return agentjson.SplitModelID(value)
}

func gitOutput(ctx context.Context, workspace string, args ...string) string {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// prefixUTF16 is JavaScript string.slice(0, limit). A split surrogate is kept
// in WTF-8 form so ledgers.AppendDecision can stringify it as "\\ud8xx".
func prefixUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	units = units[:limit]
	out := make([]byte, 0, len(value))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		if unit >= 0xd800 && unit <= 0xdbff &&
			index+1 < len(units) &&
			units[index+1] >= 0xdc00 && units[index+1] <= 0xdfff {
			out = utf8.AppendRune(out, utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
			continue
		}
		if unit >= 0xd800 && unit <= 0xdfff {
			out = append(out,
				byte(0xe0|unit>>12),
				byte(0x80|(unit>>6)&0x3f),
				byte(0x80|unit&0x3f),
			)
			continue
		}
		out = utf8.AppendRune(out, rune(unit))
	}
	return string(out)
}

func suffixUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[len(units)-limit:]))
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func discoverRunRoot() (string, string) {
	tasks := plandb.GetPlanDB().ListTasks(nil)
	rootIDs := map[string]struct{}{}
	for _, task := range tasks {
		for _, tag := range task.Tags {
			if tag == "codeaf:root" {
				rootIDs[task.ID] = struct{}{}
				break
			}
		}
	}
	openRoot := map[string]bool{}
	for _, task := range tasks {
		if task.ParentTaskID == nil {
			continue
		}
		if _, ok := rootIDs[*task.ParentTaskID]; !ok {
			continue
		}
		switch task.Status {
		case plandb.StatusPending, plandb.StatusReady,
			plandb.StatusClaimed, plandb.StatusRunning:
			openRoot[*task.ParentTaskID] = true
		}
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		task := tasks[index]
		if openRoot[task.ID] {
			return task.ProjectID, task.ID
		}
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		task := tasks[index]
		if _, ok := rootIDs[task.ID]; ok {
			return task.ProjectID, task.ID
		}
	}
	return "", ""
}

func activeTaskCounts(tasks []*plandb.Task, rootID string) (int, int) {
	active, ready := 0, 0
	for _, task := range tasks {
		if task.ID == rootID {
			continue
		}
		switch task.Status {
		case plandb.StatusClaimed, plandb.StatusRunning:
			active++
		case plandb.StatusReady:
			ready++
		}
	}
	return active, ready
}

func taskSignature(tasks []*plandb.Task) string {
	rows := make([]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, task.ID+":"+string(task.Status)+":"+strconv.FormatInt(task.UpdatedAt, 10))
	}
	return strings.Join(rows, "|")
}

func staleRows(tasks []*plandb.Task) []*stalereaper.ReapTaskRow {
	out := make([]*stalereaper.ReapTaskRow, 0, len(tasks))
	for _, task := range tasks {
		var claimed, started any
		if task.ClaimedAt != nil {
			claimed = *task.ClaimedAt
		}
		if task.StartedAt != nil {
			started = *task.StartedAt
		}
		out = append(out, &stalereaper.ReapTaskRow{
			ID: task.ID, Status: string(task.Status), StartedAt: started,
			ClaimedAt: claimed, UpdatedAt: task.UpdatedAt,
		})
	}
	return out
}

func marshalData(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(encoded, &out)
	return out
}
