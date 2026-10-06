// Package orch is the PR-AF review orchestrator — the Go port of
// src/pr_af/orchestrator.py. It coordinates the multi-phase review pipeline:
// intake → anatomy → meta-selectors → review+layer (streaming) → coverage ‖
// consistency → synthesis → merge-gate → output. It owns the HITL revision loop,
// the streaming producer/consumer channel, the order-preserving fan-outs, the
// (inert) wall-clock budget gate, and the byte-exact Markdown output builders.
//
// Concurrency parity: Python's asyncio is cooperative single-threaded, so its
// shared orchestrator state needs no locking. Go runs the fan-outs on real
// goroutines, so every field mutated from a parallel closure (agentInvocations,
// totalCostUSD, costBreakdown, the adversary/cross-ref counters, budgetExhausted)
// is guarded by o.mu.
package orch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/praf/appx"

	"github.com/Agent-Field/codeaf/internal/praf/afx"
	"github.com/Agent-Field/codeaf/internal/praf/config"
	"github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/praf/prompts"
	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// ErrBadInput is the sentinel wrapping every ValueError-class failure Python's
// review() maps to HTTP 400: the "One of pr_url, diff_text, or repo_path is
// required" guard and the _compute_repo_diff failure. The node layer (T4.2)
// maps errors.Is(err, ErrBadInput) to 400 and everything else to 500 with the
// "review execution failed: " prefix.
var ErrBadInput = errors.New("bad input")

// errBudgetExhausted mirrors Python's BudgetExhaustedError (a RuntimeError). It
// is NOT a ValueError, so it maps to 500 — not ErrBadInput.
var errBudgetExhausted = errors.New("budget exhausted")

// errPRDataNotInitialized mirrors Python's RuntimeError("PR data not
// initialized") — a 500-class internal invariant failure.
var errPRDataNotInitialized = errors.New("PR data not initialized")

// App is the agent capability surface the orchestrator depends on
// (appx.App), the single seam every sub-package's narrower interface is fed
// from: harnessx.HarnessCaller (Harness), reasoners.AICaller / gates.AICaller
// (AI), and the progress notes (Note). pr-af's served node also paused here
// for a person's approval before posting; inside codeaf a review never posts
// from the pipeline (internal/praf's post command does, on the person's yes),
// so that verb is gone.
type App = appx.App

// LocalCaller is the SDK surface for same-process reasoner invocation with
// workflow tracking (Agent.CallLocal): each call builds a child execution
// context from ctx and emits running/succeeded/failed events to the control
// plane, so every pipeline phase renders as its own node in the run's DAG —
// the same orchestration graph the Python port produces via its tracked
// router-reasoner calls. Python's @router.reasoner() wrapper routes direct
// in-process calls through workflow instrumentation; CallLocal is the Go
// SDK's equivalent, and routing the phase seams through it is what keeps the
// Go node from collapsing into a single opaque `review` execution.
type LocalCaller interface {
	CallLocal(ctx context.Context, reasonerName string, input map[string]any) (any, error)
}

// Deps carries the injected capabilities. Divergence from design §C.6: the hax
// client is NOT a Deps field — Python builds it inside run() via
// build_hax_client_from_env (gated on pr_url && !dry_run), so the orchestrator
// mirrors that through the buildHaxClient seam. GH is the GitHub client
// interface (Python constructs GitHubClient() inline; Go injects it so tests can
// stub fetch/post).
type Deps struct {
	App    App
	GH     github.Client
	NodeID string

	// Local, when non-nil, routes every phase invocation through
	// LocalCaller.CallLocal so the control plane records one child execution
	// per phase (the pipeline DAG). nil falls back to plain in-process
	// function calls — the pre-DAG behavior unit tests and stub harnesses
	// rely on. Production (node/register.go) always wires the live agent.
	Local LocalCaller

	// SpentUSD, when non-nil, reports what the run has really spent so far,
	// metered by the caller from the harness and AI results. The reasoner
	// returns never carry cost_usd, so without it the cost cap is inert (the
	// node's long-standing behavior, kept when this is nil); with it the cost
	// cap trips on real spend and the review degrades the way the wall-clock
	// cap already makes it, keeping what it has. The one-shot delegate wires it.
	SpentUSD func() float64
}

// phaseOrder ports ReviewOrchestrator.PHASE_ORDER — the cost-breakdown /
// phases_completed key list.
var phaseOrder = []string{
	"intake", "anatomy", "meta_selectors", "review",
	"adversary", "cross_ref", "coverage", "synthesis", "output",
}

// Meta-selector configuration (schemas/pipeline.py MetaSelectorConfig — not
// ported to Go config, so bound here).
var enabledLenses = []string{"semantic", "mechanical", "systemic"}

const (
	adversaryBatchSize = 5
	maxAdversaryBatch  = 4
)

// Orchestrator holds one review's state and seams. New wires the default
// (production) seams; tests override individual fields to stub phases.
type Orchestrator struct {
	deps   Deps
	input  schemas.ReviewInput
	config config.ReviewConfig

	reviewID  string
	startedAt time.Time

	mu                        sync.Mutex // guards the counters below (mutated from fan-out goroutines)
	totalCostUSD              float64
	costBreakdown             map[string]float64
	agentInvocations          int
	budgetExhausted           bool
	durationCapTripped        bool // the wall-clock cap (not the cost cap) exhausted the budget
	reviewDimensionsAttempted int
	reviewDimensionsParseable int
	degradedDimensions        int

	// Single-threaded-written state (set before/after fan-outs, read after joins).
	prData                   *schemas.GitHubPRData
	intakeResult             *schemas.IntakeResult
	anatomyResult            *schemas.AnatomyResult
	metaSelectorResults      []schemas.MetaDimensionResult
	coverageIterations       int
	crossRefCount            int
	adversaryConfirmedCount  int
	adversaryChallengedCount int
	effectiveDepth           string

	patchesCache    []prompts.StrPair
	patchesCacheSet bool

	// clock is time.Since(startedAt) — indirected so budget tests can drive it.
	clock func() time.Duration

	// layerBatchHook, when set, is called with each batch the streaming layer
	// consumer receives — a test seam to prove the layer consumes as reviewers
	// complete (streaming), not after all of them finish (batching). nil in prod.
	layerBatchHook func([]schemas.ReviewFinding)

	// Control-flow seams (default to bound methods; V9 tests override).
	runIntakeFn       func(ctx context.Context) (schemas.IntakeResult, error)
	runAnatomyFn      func(ctx context.Context, intake schemas.IntakeResult) (schemas.AnatomyResult, error)
	resolveDepthFn    func(intake schemas.IntakeResult) string
	runReviewPhasesFn func(ctx context.Context, intake schemas.IntakeResult, anatomy schemas.AnatomyResult, depth, feedback string) (schemas.ReviewPlan, []schemas.ScoredFinding, error)
	generateOutputFn  func(ctx context.Context, scored []schemas.ScoredFinding, intake schemas.IntakeResult, anatomy schemas.AnatomyResult, plan schemas.ReviewPlan, post bool) (schemas.ReviewResult, error)
	cleanupFn         func()

	// Reasoner-call seams (default to reasoners.*; streaming/order tests override).
	rfns reasonerSeams
}

type dimensionParseStats struct {
	mu        sync.Mutex
	attempted int
	parseable int
	failed    int
}

type dimensionParseSnapshot struct {
	Attempted int
	Parseable int
	Failed    int
}

func (s *dimensionParseStats) recordAttempt() {
	s.mu.Lock()
	s.attempted++
	s.mu.Unlock()
}

func (s *dimensionParseStats) recordResult(schemaParseFailed bool) {
	s.mu.Lock()
	if schemaParseFailed {
		s.failed++
	} else {
		s.parseable++
	}
	s.mu.Unlock()
}

func (s *dimensionParseStats) snapshot() dimensionParseSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return dimensionParseSnapshot{Attempted: s.attempted, Parseable: s.parseable, Failed: s.failed}
}

func (o *Orchestrator) resetDimensionStats() {
	o.mu.Lock()
	o.reviewDimensionsAttempted = 0
	o.reviewDimensionsParseable = 0
	o.degradedDimensions = 0
	o.mu.Unlock()
}

func (o *Orchestrator) recordDimensionAttempt() {
	o.mu.Lock()
	o.reviewDimensionsAttempted++
	o.mu.Unlock()
}

func (o *Orchestrator) recordDimensionResult(schemaParseFailed bool) {
	o.mu.Lock()
	if schemaParseFailed {
		o.degradedDimensions++
	} else {
		o.reviewDimensionsParseable++
	}
	o.mu.Unlock()
}

func (o *Orchestrator) dimensionStats() dimensionParseSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return dimensionParseSnapshot{Attempted: o.reviewDimensionsAttempted, Parseable: o.reviewDimensionsParseable, Failed: o.degradedDimensions}
}

// reasonerSeams bundles the reasoner entry points the pipeline invokes so tests
// can inject latency/instrumentation without a live harness.
type reasonerSeams struct {
	intake         func(ctx context.Context, deps reasoners.Deps, in reasoners.IntakeInput) (map[string]any, error)
	anatomy        func(ctx context.Context, deps reasoners.Deps, in reasoners.AnatomyInput) (map[string]any, error)
	metaSemantic   func(ctx context.Context, deps reasoners.Deps, in reasoners.MetaInput) (map[string]any, error)
	metaMechanical func(ctx context.Context, deps reasoners.Deps, in reasoners.MetaInput) (map[string]any, error)
	metaSystemic   func(ctx context.Context, deps reasoners.Deps, in reasoners.MetaInput) (map[string]any, error)
	reviewDim      func(ctx context.Context, deps reasoners.Deps, in reasoners.ReviewDimensionInput) (map[string]any, error)
	postWorthiness func(ctx context.Context, deps reasoners.Deps, in reasoners.PostWorthinessInput) (map[string]any, error)
	evidenceVerify func(ctx context.Context, deps reasoners.Deps, in reasoners.EvidenceVerifierInput) (map[string]any, error)
	adversary      func(ctx context.Context, deps reasoners.Deps, in reasoners.AdversaryInput) (map[string]any, error)
	compoundFinder func(ctx context.Context, deps reasoners.Deps, in reasoners.CompoundFinderInput) (map[string]any, error)
	compoundDedup  func(ctx context.Context, deps reasoners.Deps, in reasoners.CompoundDedupInput) (map[string]any, error)
	coverageGate   func(ctx context.Context, deps reasoners.Deps, in reasoners.CoverageGateInput) (map[string]any, error)
	extractOblig   func(ctx context.Context, deps reasoners.Deps, in reasoners.ExtractObligationsInput) (map[string]any, error)
	verifyOblig    func(ctx context.Context, deps reasoners.Deps, in reasoners.VerifyObligationInput) (map[string]any, error)
}

func defaultReasonerSeams() reasonerSeams {
	return reasonerSeams{
		intake:         reasoners.IntakePhase,
		anatomy:        reasoners.AnatomyPhase,
		metaSemantic:   reasoners.MetaSemantic,
		metaMechanical: reasoners.MetaMechanical,
		metaSystemic:   reasoners.MetaSystemic,
		reviewDim:      reasoners.ReviewDimension,
		postWorthiness: reasoners.PostWorthinessGate,
		evidenceVerify: reasoners.EvidenceVerifier,
		adversary:      reasoners.AdversaryPhase,
		compoundFinder: reasoners.CompoundFinderPhase,
		compoundDedup:  reasoners.CompoundDedupPhase,
		coverageGate:   reasoners.CoverageGate,
		extractOblig:   reasoners.ExtractObligations,
		verifyOblig:    reasoners.VerifyObligation,
	}
}

// callLocalSeams routes every phase through local.CallLocal under its
// registered reasoner name (reasoners.Name*), so each invocation is tracked
// as a child execution on the control plane. The registered handler
// (node/register.go) afx.Binds the map back into the same typed input and
// calls the same reasoners.* function with the same Deps the direct seams
// use — behavior is identical, the DAG is the only addition.
func callLocalSeams(local LocalCaller) reasonerSeams {
	return reasonerSeams{
		intake:         viaLocal[reasoners.IntakeInput](local, reasoners.NameIntakePhase),
		anatomy:        viaLocal[reasoners.AnatomyInput](local, reasoners.NameAnatomyPhase),
		metaSemantic:   viaLocal[reasoners.MetaInput](local, reasoners.NameMetaSemantic),
		metaMechanical: viaLocal[reasoners.MetaInput](local, reasoners.NameMetaMechanical),
		metaSystemic:   viaLocal[reasoners.MetaInput](local, reasoners.NameMetaSystemic),
		reviewDim:      viaLocal[reasoners.ReviewDimensionInput](local, reasoners.NameReviewDimension),
		postWorthiness: viaLocal[reasoners.PostWorthinessInput](local, reasoners.NamePostWorthinessGate),
		evidenceVerify: viaLocal[reasoners.EvidenceVerifierInput](local, reasoners.NameEvidenceVerifier),
		adversary:      viaLocal[reasoners.AdversaryInput](local, reasoners.NameAdversaryPhase),
		compoundFinder: viaLocal[reasoners.CompoundFinderInput](local, reasoners.NameCompoundFinderPhase),
		compoundDedup:  viaLocal[reasoners.CompoundDedupInput](local, reasoners.NameCompoundDedupPhase),
		coverageGate:   viaLocal[reasoners.CoverageGateInput](local, reasoners.NameCoverageGate),
		extractOblig:   viaLocal[reasoners.ExtractObligationsInput](local, reasoners.NameExtractObligations),
		verifyOblig:    viaLocal[reasoners.VerifyObligationInput](local, reasoners.NameVerifyObligation),
	}
}

// viaLocal adapts one typed seam to a CallLocal invocation: typed input →
// afx.ToMap → CallLocal(name) → registered handler (afx.Bind → reasoners.*).
// The reasoners.Deps parameter is ignored — the handler closes over the
// node-level Deps built at registration, which point at the same live agent.
// Handlers return map[string]any; a nil result (impossible today: every
// reasoner returns a non-nil map or an error) surfaces as an empty map so
// callers keep their raw-map contract.
func viaLocal[T any](local LocalCaller, name string) func(context.Context, reasoners.Deps, T) (map[string]any, error) {
	return func(ctx context.Context, _ reasoners.Deps, in T) (map[string]any, error) {
		input, err := afx.ToMap(in)
		if err != nil {
			return nil, err
		}
		raw, err := local.CallLocal(ctx, name, input)
		if err != nil {
			return nil, err
		}
		out, ok := raw.(map[string]any)
		if !ok {
			if raw == nil {
				return map[string]any{}, nil
			}
			return nil, fmt.Errorf("orch: reasoner %s returned %T, want map[string]any", name, raw)
		}
		return out, nil
	}
}

// New constructs an Orchestrator with production seams. startedAt is set now so
// the wall-clock budget gate measures from construction (parity with Python's
// time.monotonic() in __init__).
func New(d Deps, in schemas.ReviewInput, cfg config.ReviewConfig) *Orchestrator {
	o := &Orchestrator{
		deps:           d,
		input:          in,
		config:         cfg,
		reviewID:       "rev_" + hex12(),
		startedAt:      time.Now(),
		costBreakdown:  make(map[string]float64, len(phaseOrder)),
		effectiveDepth: "standard",
		rfns:           defaultReasonerSeams(),
	}
	for _, p := range phaseOrder {
		o.costBreakdown[p] = 0.0
	}
	if d.Local != nil {
		o.rfns = callLocalSeams(d.Local)
	}
	o.clock = func() time.Duration { return time.Since(o.startedAt) }

	o.runIntakeFn = o.runIntake
	o.runAnatomyFn = o.runAnatomy
	o.resolveDepthFn = o.resolveDepth
	o.runReviewPhasesFn = o.runReviewPhases
	o.generateOutputFn = o.generateOutput
	o.cleanupFn = o.cleanupContextDir
	return o
}

// reasonerDeps builds the reasoner capability bundle from the single App seam.
func (o *Orchestrator) reasonerDeps() reasoners.Deps {
	return reasoners.Deps{Harness: o.deps.App, AI: o.deps.App}
}

// hex12 ports uuid4().hex[:12] — 6 random bytes rendered as 12 lowercase hex.
func hex12() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Run executes the pipeline once (orchestrator.py run(), without its HITL
// revision loop). ErrBadInput-wrapped errors mark a request the review could
// not take; anything else is an internal failure.
//
// THE REVIEW POSTS ONLY WHEN IT IS NOT A DRY RUN AND NAMES A PULL REQUEST.
// pr-af's served node put a person's approval between the review and the
// post; inside codeaf every review runs dry and the post is its own step on
// the person's yes (internal/praf's post command), so the loop that waited
// for that approval is gone.
func (o *Orchestrator) Run(ctx context.Context) (schemas.ReviewResult, error) {
	var zero schemas.ReviewResult

	intake, err := o.runIntakeFn(ctx)
	if err != nil {
		return zero, err
	}
	o.intakeResult = &intake
	reviewDepth := o.resolveDepthFn(intake)

	anatomy, err := o.runAnatomyFn(ctx, intake)
	if err != nil {
		return zero, err
	}
	o.anatomyResult = &anatomy

	// Review hints (the webhook's @-mention text, the brief's focus) reach the
	// reviewers through the guidance channel a re-review used.
	o.resetDimensionStats()
	plan, scored, err := o.runReviewPhasesFn(ctx, intake, anatomy, reviewDepth, o.mergeFeedback(o.config.Hints))
	if err != nil {
		return zero, err
	}
	post := strp(o.input.PrURL) != "" && !o.input.DryRun
	return o.finish(ctx, scored, intake, anatomy, plan, post)
}

// finish generates output (optionally posting) and cleans up the context dir.
func (o *Orchestrator) finish(
	ctx context.Context,
	scored []schemas.ScoredFinding,
	intake schemas.IntakeResult,
	anatomy schemas.AnatomyResult,
	plan schemas.ReviewPlan,
	post bool,
) (schemas.ReviewResult, error) {
	result, err := o.generateOutputFn(ctx, scored, intake, anatomy, plan, post)
	if err != nil {
		return schemas.ReviewResult{}, err
	}
	o.cleanupFn()
	return result, nil
}

// mergeFeedback ports _merge_feedback — collapse accumulated instructions with
// " | ", trimming empties.
func (o *Orchestrator) mergeFeedback(revisionHistory []string) string {
	items := make([]string, 0, len(revisionHistory))
	for _, instr := range revisionHistory {
		t := strings.TrimSpace(instr)
		if t != "" {
			items = append(items, t)
		}
	}
	return strings.Join(items, " | ")
}

// ---- budget / cost (inert cost, live wall-clock) ----

// budgetOrTimeoutExhausted ports _budget_or_timeout_exhausted. Cost stays 0
// (reasoner returns never carry cost_usd), so only the wall-clock check trips.
// Which cap tripped is recorded so budgetExhaustedMessage can word the failure
// honestly (§B.4 pair with Python's _budget_exhausted_message).
func (o *Orchestrator) budgetOrTimeoutExhausted(phase string) bool {
	elapsed := o.clock().Seconds()
	o.mu.Lock()
	defer o.mu.Unlock()
	if elapsed > float64(o.config.Budget.MaxDurationSeconds) {
		o.budgetExhausted = true
		o.durationCapTripped = true
		return true
	}
	if o.spentLocked() >= o.config.Budget.MaxCostUSD {
		o.budgetExhausted = true
		return true
	}
	phaseSpent := o.costBreakdown[phase]
	phaseCap, ok := o.config.Budget.PhaseBudgets[phase]
	if !ok {
		return false // absent phase → cap is +inf → never trips
	}
	return phaseSpent >= phaseCap
}

// budgetExhaustedMessage words the exhaustion by cause: the wall-clock cap gets
// an explicit timeout message (in the Go port cost never accrues, so this is
// the only cap that actually trips), the cost cap keeps the historical
// "Budget exhausted before <phase>" wording. Byte-identical to Python's
// _budget_exhausted_message (§B.4).
func (o *Orchestrator) budgetExhaustedMessage(phase string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.durationCapTripped {
		return fmt.Sprintf("Review time budget exceeded (max_duration_seconds=%d) before %s",
			o.config.Budget.MaxDurationSeconds, phase)
	}
	return "Budget exhausted before " + phase
}

// registerCost ports _register_cost∘_extract_cost. extractCost reads "cost_usd"
// off the reasoner return (always absent), so total stays 0.0.
func (o *Orchestrator) registerCost(phase string, resultRaw map[string]any) {
	cost, ok := extractCost(resultRaw)
	if !ok {
		return
	}
	o.mu.Lock()
	o.totalCostUSD += cost
	o.costBreakdown[phase] += cost
	o.mu.Unlock()
}

func (o *Orchestrator) incInvocations(n int) {
	o.mu.Lock()
	o.agentInvocations += n
	o.mu.Unlock()
}

func (o *Orchestrator) invocations() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.agentInvocations
}

func (o *Orchestrator) isBudgetExhausted() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.budgetExhausted
}

func (o *Orchestrator) totalCost() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.spentLocked()
}

// spentLocked is the run's spend: the reasoner-reported total, or the caller's
// metered figure when Deps.SpentUSD is set and higher. Callers hold o.mu.
func (o *Orchestrator) spentLocked() float64 {
	total := o.totalCostUSD
	if o.deps.SpentUSD != nil {
		if metered := o.deps.SpentUSD(); metered > total {
			total = metered
		}
	}
	return total
}

// extractCost ports _extract_cost: reads cost_usd off the raw map or its
// unwrapped payload. Always returns (0, false) in practice.
func extractCost(resultRaw map[string]any) (float64, bool) {
	if resultRaw == nil {
		return 0, false
	}
	if c, ok := asFloat(resultRaw["cost_usd"]); ok {
		return c, true
	}
	payload := unwrap(resultRaw)
	if payload != nil {
		if c, ok := asFloat(payload["cost_usd"]); ok {
			return c, true
		}
	}
	return 0, false
}

// resolveDepth ports _resolve_depth.
func (o *Orchestrator) resolveDepth(intake schemas.IntakeResult) string {
	if o.input.Depth != "auto" {
		return o.input.Depth
	}
	if _, ok := config.DepthProfiles[intake.ReviewDepth]; ok {
		return intake.ReviewDepth
	}
	if o.prData != nil && o.prData.Diff != "" {
		lineCount := len(strings.Split(o.prData.Diff, "\n"))
		// Python splitlines() does not count a trailing newline as an extra line;
		// mirror it.
		lineCount = countSplitlines(o.prData.Diff)
		// Under the smallest threshold → that threshold's depth.
		if len(config.AutoDepthThresholds) > 0 {
			minTh := config.AutoDepthThresholds[0]
			for _, th := range config.AutoDepthThresholds {
				if th.Lines < minTh.Lines {
					minTh = th
				}
			}
			if lineCount < minTh.Lines {
				return minTh.Depth
			}
			// Ascending scan: first threshold the count is under.
			for _, th := range config.AutoDepthThresholds {
				if lineCount < th.Lines {
					return th.Depth
				}
			}
		}
		return "deep"
	}
	return "standard"
}

// escalateDepth ports _escalate_depth.
func (o *Orchestrator) escalateDepth(currentDepth string) string {
	if currentDepth == "deep" {
		return "deep"
	}
	signals := 0
	if o.anatomyResult != nil {
		if len(o.anatomyResult.BlastRadius) > 10 {
			signals++
		}
		if len(o.anatomyResult.IntentGaps) > 0 {
			signals++
		}
		if len(o.anatomyResult.RiskSurfaces) > 3 {
			signals++
		}
		if o.anatomyResult.Stats.TotalAdditions > 500 {
			signals++
		}
	}
	if len(o.metaSelectorResults) > 0 {
		low := 0
		for _, m := range o.metaSelectorResults {
			if m.Confidence < 0.5 {
				low++
			}
		}
		if low >= 2 {
			signals++
		}
	}
	if signals >= 2 && currentDepth == "quick" {
		return "standard"
	}
	if signals >= 3 && currentDepth == "standard" {
		return "deep"
	}
	return currentDepth
}
