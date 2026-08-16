// Package scheduler ports
// src/session/plandb-scheduler.ts from swe-pro (commit 3b25a1a).
//
// Worktree allocation/garbage collection, dispatch-envelope derivation,
// ready-candidate ordering, model-pool pre-warming, dispatch, review/merge
// integration, and overlapping scheduler-cycle orchestration live here.
package scheduler

import (
	"context"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolationfurrow"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafbriefing"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/loopguard"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
)

const (
	schedulerAutoTag   = "auto:scheduler"
	defaultMaxParallel = 20
)

// SchedulerInput mirrors plandb-scheduler.ts:1618-1625. MaxParallel is a
// pointer because the TS `??` default keeps an explicit zero.
type SchedulerInput struct {
	RootTaskID      string    `json:"rootTaskID"`
	ProjectID       string    `json:"projectID"`
	DBPath          string    `json:"dbPath"`
	ParentSessionID string    `json:"parentSessionID"`
	PromptOps       PromptOps `json:"-"`
	MaxParallel     *float64  `json:"maxParallel,omitempty"`
}

// GateStatus is DispatchResult.gate.status from
// plandb-scheduler.ts:1636-1644.
type GateStatus string

const (
	GatePass        GateStatus = "pass"
	GateFail        GateStatus = "fail"
	GateSkipped     GateStatus = "skipped"
	GateDonePartial GateStatus = "done_partial"
	GateEscalated   GateStatus = "escalated"
)

// GateVerdict is the optional binary gate verdict.
type GateVerdict string

const (
	VerdictPass GateVerdict = "pass"
	VerdictFail GateVerdict = "fail"
)

// GateConfidence is the optional gate confidence.
type GateConfidence string

const (
	ConfidenceHigh   GateConfidence = "high"
	ConfidenceMedium GateConfidence = "medium"
	ConfidenceLow    GateConfidence = "low"
)

// DispatchGate mirrors the nested gate object at
// plandb-scheduler.ts:1636-1644.
type DispatchGate struct {
	Status         GateStatus         `json:"status"`
	Verdict        *GateVerdict       `json:"verdict,omitempty"`
	Confidence     *GateConfidence    `json:"confidence,omitempty"`
	RepairAttempts *jscompat.JSNumber `json:"repairAttempts,omitempty"`
	Reason         *string            `json:"reason,omitempty"`
	AdvisorAction  *string            `json:"advisorAction,omitempty"`
}

// DispatchResult mirrors plandb-scheduler.ts:1627-1645.
type DispatchResult struct {
	TaskID       string        `json:"taskID"`
	SubagentType string        `json:"subagentType"`
	AgentID      string        `json:"agentID"`
	Success      bool          `json:"success"`
	Summary      *string       `json:"summary,omitempty"`
	Error        *string       `json:"error,omitempty"`
	Gate         *DispatchGate `json:"gate,omitempty"`
}

// SkippedTask is one SchedulerCycleResult.skipped row.
type SkippedTask struct {
	TaskID string `json:"taskID"`
	Reason string `json:"reason"`
}

// CycleQuietReason explains why a scheduler cycle with no dispatches was
// quiet. Callers use this to distinguish resource backpressure from an idle
// graph and a parallel window occupied by genuinely live dispatches.
type CycleQuietReason string

const (
	CycleQuietNone         CycleQuietReason = ""
	CycleQuietPaused       CycleQuietReason = "paused"
	CycleQuietWindowFull   CycleQuietReason = "window-full"
	CycleQuietNothingReady CycleQuietReason = "nothing-ready"
	CycleQuietClaimsLost   CycleQuietReason = "claims-unavailable"
)

// SchedulerCycleResult mirrors plandb-scheduler.ts:1647-1651.
type SchedulerCycleResult struct {
	Dispatched  []DispatchResult  `json:"dispatched"`
	Scanned     jscompat.JSNumber `json:"scanned"`
	Skipped     []SkippedTask     `json:"skipped"`
	QuietReason CycleQuietReason  `json:"quietReason,omitempty"`
}

// WorktreeBackend is allocateWorktree's backend discriminator at
// plandb-scheduler.ts:1193.
type WorktreeBackend string

const (
	WorktreeBackendGit    WorktreeBackend = "worktree"
	WorktreeBackendFurrow WorktreeBackend = "furrow"
)

// WorktreeAllocation mirrors allocateWorktree's successful return object.
type WorktreeAllocation struct {
	Path    string          `json:"path"`
	BaseSHA string          `json:"baseSha"`
	BaseRef string          `json:"baseRef"`
	Backend WorktreeBackend `json:"backend"`
}

// ModelTier is the scheduler-facing subset of router ModelTier. The router's
// concrete candidate type is intentionally hidden behind modelPoolResolver
// until phase-two router integration.
type ModelTier string

const (
	ModelTierHigh ModelTier = "high"
	ModelTierLow  ModelTier = "low"
)

// ModelCandidate is the only pool-candidate field consumed in phase one.
type ModelCandidate struct {
	ID string `json:"id"`
}

// ModelAssignment is one model override selected by the HEFT/slack block.
type ModelAssignment struct {
	TaskID string              `json:"taskID"`
	Model  capability.ModelRef `json:"model"`
}

// CandidatePlan is the phase-one output consumed by the phase-two claim loop.
type CandidatePlan struct {
	Candidates  []*plandb.Task
	Prioritized []*plandb.Task
	Ordered     []*plandb.Task
	Assignments []ModelAssignment
	Skipped     []SkippedTask
	Scanned     int
}

// PromptOps is the planner/replanner/merger TaskPromptOps boundary carried by
// SchedulerInput (plandb-scheduler.ts:1618-1624). Leaf execution itself uses
// StepLoopClient.
type PromptOps interface {
	ResolvePromptParts(ctx context.Context, template string) ([]any, error)
	Prompt(ctx context.Context, input any) (any, error)
	Cancel(ctx context.Context, sessionID string) error
}

// promptOps keeps the phase-one private spelling source-compatible with tests
// written before the seam became part of the public phase-two surface.
type promptOps = PromptOps

// commandRunner is the nothrow Process.run seam used by
// plandb-scheduler.ts:1092-1284. Every process outcome, including spawn
// failure, is represented as a result rather than an error.
type commandRunner interface {
	Run(ctx context.Context, argv []string, opts runOptions) runResult
}

type runOptions struct {
	Cwd string
	Env map[string]string
}

type runResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// planDBRunner is the intentionally cwd/dbPath-blind in-process bridge from
// plandb-scheduler.ts:887-889.
type planDBRunner interface {
	Run(argv []string) plandb.RunResult
}

// diskEnvelopeReader is gatherDispatchEnvelope's measurement boundary
// (plandb-scheduler.ts:126-144). The error exists so an injected measurement
// can reproduce the source's catch-all path; the real resource guard is
// fail-open and returns nil error.
type diskEnvelopeReader interface {
	CheckDiskEnvelope(path string) (resourceguard.DiskEnvelope, error)
}

// furrowForker and furrowFactory are allocateWorktree's adapter seam from
// plandb-scheduler.ts:1188-1191 and 1221-1248.
type furrowForker interface {
	Fork(workspace, name string) isolationfurrow.ForkResult
}

type furrowFactory func() furrowForker

// worktreeHygiene is the narrow phase boundary for the two out-of-bundle
// post-create helpers called at plandb-scheduler.ts:1272-1284.
type worktreeHygiene interface {
	EnsureCodeafExcluded(ctx context.Context, worktree string) error
	SuppressCaseCollisions(ctx context.Context, worktree string) error
}

// modelPoolResolver is the router seam consumed by the HEFT and pre-warm
// blocks at plandb-scheduler.ts:1947-1949 and 2103-2118.
type modelPoolResolver interface {
	CandidatesForTier(tier ModelTier) []ModelCandidate
}

// providerResolver is the provider-model/language seam consumed sequentially
// by plandb-scheduler.ts:2122-2129.
type providerResolver interface {
	GetModel(ctx context.Context, providerID, modelID string) (any, error)
	GetLanguage(ctx context.Context, model any) (any, error)
}

// CapabilityReader is the read-only portion of CapabilityTracker used by
// plandb-scheduler.ts:1981-1989 and 2028-2063.
type CapabilityReader interface {
	MaxReliableBand(model capability.ModelRef, threshold ...float64) sizeband.SizeBand
}

type capabilityReader = CapabilityReader

// leafOutcomeReader is the best-effort JSONL boundary used by
// plandb-scheduler.ts:1960-1965.
type leafOutcomeReader interface {
	ReadLeafOutcomes(workspace string) []leafoutcome.LeafOutcome
}

// PermissionRule is the small child-agent permission projection consumed by
// dispatchOne. The full agent/config services remain outside this package.
type PermissionRule struct {
	Permission string
	Pattern    string
	Action     string
}

// AgentInfo is the scheduler projection of one registered subagent.
type AgentInfo struct {
	Name       string
	Model      any
	Permission []PermissionRule
}

// AgentRegistry replaces Agent.Service for dispatchOne.
type AgentRegistry interface {
	Get(ctx context.Context, name string) (*AgentInfo, error)
}

// ProviderModel is the portable model shape accepted from ProviderResolver.
// ProviderResolver may also return a map or another struct with these fields;
// this concrete type is convenient for embedders and tests.
type ProviderModel struct {
	ProviderID string
	ID         string
	ModelID    string
	Value      any
}

// ProviderResolver is the provider model/language seam shared by prewarm,
// dispatch, and conflict recovery.
type ProviderResolver interface {
	GetModel(ctx context.Context, providerID, modelID string) (any, error)
	GetLanguage(ctx context.Context, model any) (any, error)
}

// ModelPoolResolver is the router tier boundary. Filtered pools are optional:
// a value implementing FilteredModelPoolResolver gets the role-aware source
// behavior; other implementations fall back to CandidatesForTier.
type ModelPoolResolver interface {
	CandidatesForTier(tier ModelTier) []ModelCandidate
}

// FilteredModelPoolResolver is the role-aware router extension.
type FilteredModelPoolResolver interface {
	ModelPoolResolver
	CandidatesForTierFiltered(tier ModelTier, forSlot string) []ModelCandidate
}

// LeafPart is one observable part returned by the leaf step loop. Tool fields
// are the loop-guard projection; Text is the model-visible assistant text.
type LeafPart struct {
	Type    string
	Text    string
	Tool    string
	ArgsKey string
	Status  string
	CostUSD *float64
}

// LeafRunRequest is the complete scheduler-to-step-loop call. A concrete
// adapter may create/persist the child session before invoking steploop.Loop;
// tests can script the result directly at this seam.
type LeafRunRequest struct {
	ParentSessionID string
	SessionTitle    string
	Attempt         int
	Workspace       string
	Worktree        string
	DBPath          string
	ProjectID       string
	RootTaskID      string
	TaskID          string
	Agent           AgentInfo
	ProviderID      string
	ModelID         string
	SystemReminder  string
	Prompt          string
	AfterTurn       func(context.Context, LeafTurnObservation) error
}

// LeafTurnObservation is one completed provider turn, exposed before the
// engine decides whether to make another call.
type LeafTurnObservation struct {
	Finish  string
	Parts   []LeafPart
	CostUSD float64
}

// LeafRunResult is the durable observation of one step-loop attempt.
// ErrorMessage wins over ErrorName, reproducing
// error.data.message ?? error.name (LB-17).
type LeafRunResult struct {
	SessionID string
	Parts     []LeafPart
	Messages  []*leafoutcome.SessionMessage
	// CostUSD and CallCosts preserve the engine ledger even when a call has no
	// tool part. CallCosts is the authoritative guard input; CostUSD is the
	// aggregate fallback for adapters that cannot expose individual calls.
	CostUSD      float64
	CallCosts    []float64
	ErrorName    string
	ErrorMessage string
	TestPassed   *bool
}

// StepLoopClient is dispatchOne's leaf-execution seam over
// internal/engine/steploop. It intentionally owns session setup because the
// Go step loop expects its initial user message to exist in the Store.
type StepLoopClient interface {
	RunLeaf(ctx context.Context, input LeafRunRequest) (LeafRunResult, error)
}

// ReviewBug and ReviewVerdict are the review-gate fields surfaced in blocker
// contexts and DispatchResult.
type ReviewBug struct {
	Severity string
	File     string
	Line     any
	Detail   string
}

type ReviewVerdict struct {
	Verdict      GateVerdict
	Confidence   GateConfidence
	SpecCoverage string
	Bugs         []ReviewBug
	RepairHints  []string
	Evidence     string
	Raw          any
}

// GateResult is the scheduler-facing review-gate union. Done nil means the
// source's omitted field, whose pass-path default is true.
type GateResult struct {
	Status            GateStatus
	Verdict           *ReviewVerdict
	RepairAttempts    float64
	Reason            string
	AdvisorAction     string
	AdvisorBlocker    string
	FinalWorktreePath string
	FinalBranch       string
	Done              *bool
	// Deliberate Go-only fidelity addition: unlike the TypeScript gate, the Go
	// port can fabricate verdicts after cancelled git or advisor plumbing.
	Unadjudicated bool // the gate could not obtain a judgement; nothing read this diff
}

type GateInput struct {
	Workspace       string
	DBPath          string
	ProjectID       string
	RootTaskID      string
	RootExcerpt     string
	Task            *plandb.Task
	WorktreePath    string
	Branch          string
	BaseSHA         string
	Summary         string
	ParentSessionID string
	SubagentType    string
	PromptOps       PromptOps
}

// ReviewGate is deliberately narrow until review-gate is ported.
type ReviewGate interface {
	GateLeaf(ctx context.Context, input GateInput) (GateResult, error)
}

// Replanner is dispatchOne's escalation seam until replan-gate lands.
type Replanner interface {
	ShouldReplan(ctx context.Context, workspace string) bool
	Replan(ctx context.Context, input ReplanInput) (ReplanResult, error)
}

type ReplanInput struct {
	Workspace       string
	DBPath          string
	ParentSessionID string
	PromptOps       PromptOps
	UserGoal        string
	TaskID          string
	SessionKey      string
}

type ReplanResult struct {
	Abort   bool
	Summary string
}

// PlannerTranslator is the frontier micro-replan seam. Its implementation
// owns translate + apply so the scheduler can preserve the tick guard and
// terminal status without porting either agent in this bundle.
type PlannerTranslator interface {
	RunFrontierTick(ctx context.Context, input FrontierTickInput) (FrontierTickResult, error)
}

type FrontierTickInput struct {
	Workspace       string
	DBPath          string
	ProjectID       string
	RootTaskID      string
	ParentSessionID string
	PromptOps       PromptOps
	Trigger         string
	FrontierContext string
	Tick            int
}

type FrontierTickResult struct {
	Status          string
	TaskCount       int
	ResidualUpdated bool
}

// LeafBriefingBuilder is satisfied by *leafbriefing.Builder.
type LeafBriefingBuilder interface {
	BuildLeafBriefingSections(args leafbriefing.BuildLeafBriefingSectionsArgs) *leafbriefing.LeafBriefingSections
}

// OutcomeObserver is invoked only after EmitLeafOutcome has persisted the
// record. CapabilityTracker adapters can implement this with a mutex.
type OutcomeObserver interface {
	Observe(outcome leafoutcome.LeafOutcome)
}

// ClauseJudger is the shared low-tier clause inventory seam used by coder
// reminders and the post-merge auditor.
type ClauseJudger interface {
	JudgeSpecClauses(context.Context, string, string) (specclauses.SpecClauseJudgment, error)
}

// SchedulerClock makes the fan-out stagger and wall-time accounting
// deterministic without changing the production clock.
type SchedulerClock interface {
	Now() time.Time
	Sleep(ctx context.Context, delay time.Duration) error
}

// MergeRequest and MergeResult form the leaf-merger/recovery stack seam.
type MergeRequest struct {
	Workspace       string
	Worktree        string
	MergeAt         string
	TaskID          string
	TaskTitle       string
	TargetBranch    string
	SourceBranch    string
	ParentSessionID string
	PromptOps       PromptOps
	UserGoal        string
	Language        any
}

type MergeResult struct {
	OK           bool
	Summary      string
	ConflictPath string
}

type MergeStack interface {
	Merge(ctx context.Context, input MergeRequest) (MergeResult, error)
}

// LoopGuardFactory permits exact scripted terminal-path tests while the
// production default remains loopguard.CreateLoopGuard.
type LoopGuardFactory func(options loopguard.LoopGuardOptions) loopguard.LoopGuard
