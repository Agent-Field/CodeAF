//go:build !windows

// This file is the pipeline driver: budget, run base and workspace
// preparation around the solo run in solo.go.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/runbudget"
	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

type pipelineDeps struct {
	Backend backend
	Config  *seniorDevConfig
	Events  *eventWriter
	Notes   io.Writer
	Now     func() time.Time
	Sleep   func(context.Context, time.Duration) error
}

type pipeline struct {
	args      cliArgs
	workspace string
	sessionID string
	runtime   *runtimeAdapter
	pool      poolResolver
	events    *eventWriter
	notes     io.Writer
	// recorder identifies, compares, freezes and restores the tree. Set in
	// prepareWorkspace, once the workspace path is absolute.
	recorder  workspaceRecorder
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
	wallStart time.Time
	priorCost float64
	budget    runbudget.RunBudget
	budgetRun *runbudget.BudgetTracker

	budgetCost float64
	// rescuePath is the durable place later edits are copied before a restore.
	rescuePath     string
	rescueCount    int
	rescueDeleted  bool
	rescueManifest string

	fingerprintMu    sync.Mutex
	fingerprintFiles map[string]worktreeFileFingerprint
	fingerprintNonce uint64

	// verificationTimeouts remembers entrypoints that hung at the verification
	// ceiling so a second pass does not pay the full ceiling again for an
	// identical command against an unchanged tree.
	verificationTimeouts map[string]timedOutEntrypoint
	// verifyForTest overrides the project verification the finalizer runs.
	// Nil in production; a seam for tests, which have no discoverable project
	// entrypoints to verify.
	verifyForTest func(context.Context) projectVerificationResult
	// turnForTest overrides soloTurn. Nil in production; a seam for tests,
	// which have no model to converse with.
	turnForTest func(ctx context.Context, goal, prompt string) (turnResult, error)
	// lastVerify remembers the most recent completed full verification and
	// the git tree it measured, so the finalizer can judge an unchanged tree
	// on the last verdict (rememberVerifiedTree in workspace_git.go).
	lastVerify        *projectVerificationResult
	lastVerifyTreeSHA string
}

type pipelineResult struct {
	Status  string
	Reason  string
	BaseSHA string
	CostUSD float64
	// Terminal is the solo run's own account of how it ended: whether it
	// submitted, its stated reason, nudge count, the frozen tree, and what
	// verification observed. It is emitted verbatim on the single terminal
	// event; see persistTerminalResult.
	Terminal  map[string]any
	WallStart time.Time
}

var errWallClockBudget = errors.New("wall-clock budget exhausted")

// errRunBudget marks a mid-dispatch budget stop. Exhaustion is an ordinary
// ending that exits 0, so this must not surface as a crash.
var errRunBudget = errors.New("run budget exhausted")

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
	router := initRunRouter(args, deps.Events)
	if aware, ok := deps.Backend.(adaptiveRouterBackend); ok {
		aware.setAdaptiveRouter(router)
	}
	runtime := newConfiguredRuntime(workspace, deps.Backend, deps.Config)
	runtime.now = now
	runtime.events = deps.Events
	if deps.Events != nil && runtime.bus != nil {
		runtime.unsubscribeEvents = runtime.bus.SubscribeAllCallback(deps.Events.busEvent)
	}
	notes := deps.Notes
	if notes == nil {
		notes = io.Discard
	}
	return &pipeline{
		args: args, workspace: workspace,
		// Replaced in prepareWorkspace once the path is absolute. Set here so
		// a pipeline is never half-built: every method that touches the tree
		// has a recorder to ask.
		recorder: newWorkspaceRecorder(args, workspace, func(message string) {
			_, _ = io.WriteString(notes, message)
		}),
		sessionID: runtime.nextID("session"), runtime: runtime, pool: pool,
		events: deps.Events, notes: notes,
		now: now, sleep: sleep, wallStart: now(),
		budget: runbudget.ResolveRunBudget(&runbudget.RunBudgetFlags{
			MaxCost: args.MaxCost, MaxHours: args.MaxHours,
		}, nil),
	}
}

func (runner *pipeline) run(
	ctx context.Context, goal string,
) (result pipelineResult, runErr error) {
	result = runner.initializeRun()
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
	baseSHA, done, err := runner.prepareRunBase(ctx, &result)
	if err != nil {
		return result, err
	}
	if done {
		return result, nil
	}
	contract := map[string]any{
		"base_sha":               baseSHA,
		"high_models":            runner.pool.values(baked.TierHigh),
		"low_models":             runner.pool.values(baked.TierLow),
		"frontier_models":        runner.pool.values(baked.TierFrontier),
		"entry_agent":            "coder",
		"senior_dev_environment": safeSeniorDevEnvironment(),
		// Which promises the run is keeping about the tree, and how. A reader
		// comparing two runs needs this before it compares anything else.
		"workspace_recorder": runner.recorder.Kind(),
	}
	runner.events.stage("run-contract", "ready", contract)
	defer runner.emitPatchSummary(baseSHA)
	if err := runner.runtime.ensureRootSession(
		ctx, runner.sessionID, prefixUTF16(goal, 60), "coder",
	); err != nil {
		return result, err
	}
	outcome, err := runner.runSolo(ctx, goal, baseSHA)
	result.CostUSD = runner.totalCost()
	// Captured before the error check: ship now runs on every ending, so even a
	// wall-clock kill leaves the run's own account of what it did, and that
	// account is the terminal event's payload.
	result.Terminal = outcome.TerminalData
	if err != nil {
		return result, err
	}
	result.Status, result.Reason = soloResultStatus(outcome)
	if exhausted, reason := runner.budgetExhausted(); exhausted && result.Status != "pass" {
		result.Status, result.Reason = "budget-exhausted", reason
	}
	result.CostUSD = runner.totalCost()
	return result, nil
}

// soloResultStatus projects the run's own vocabulary onto the pass/fail
// statuses of the terminal event. The distinctions the solo pipeline draws --
// unverified because an entrypoint hung, unsubmitted because the model never
// declared done -- are not lost: they are the reason string, and the terminal
// event carries them structurally.
func soloResultStatus(outcome soloOutcome) (string, string) {
	reason := outcome.SubmissionReason
	switch outcome.Status {
	case "pass":
		return "pass", reason
	case "pass-unverified":
		return "pass", "submitted; verification did not complete"
	case "unsubmitted":
		return "fail", "the run ended without submitting"
	default:
		if reason == "" {
			reason = "the submitted candidate did not verify"
		}
		return "fail", reason
	}
}

// resolveRunBase is the commit every patch in this run is measured against.
// A run starts from wherever HEAD is: there is no inherited base, because
// there is no second process that could have moved the tree first.
func (runner *pipeline) resolveRunBase(ctx context.Context) (string, error) {
	return runner.recorder.Base(ctx)
}

func safeSeniorDevEnvironment() map[string]string {
	result := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, "SENIOR_DEV_") {
			continue
		}
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			result[name] = "<redacted>"
			continue
		}
		result[name] = value
	}
	return result
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
	runner.recorder = newWorkspaceRecorder(runner.args, absolute, runner.note)
	if err := runner.recorder.Prepare(ctx); err != nil {
		return err
	}
	if !runner.recorder.CommitsOnWrite() {
		// The recorder keeps its own copies of the tree, so a per-write commit
		// buys nothing -- and under --in-place the workspace may be a
		// repository this run has no business writing history into.
		util.DisableEagerCommit()
	}
	runner.events.stage("bootstrap", "ready", map[string]any{
		"workspace": absolute, "recorder": runner.recorder.Kind(),
	})
	return nil
}

func (runner *pipeline) note(message string) {
	_, _ = io.WriteString(runner.notes, message)
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
	return newWorktreeFingerprinter(runner, ctx).fingerprint()
}

func (runner *pipeline) overBudgetFingerprint() string {
	runner.fingerprintNonce++
	return fmt.Sprintf("changed:worktree-fingerprint-budget:%d", runner.fingerprintNonce)
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

// splitModelID splits a "provider/model" reference on its first slash. A
// reference without a slash is all provider and no model.
func splitModelID(value string) (providerID, modelID string) {
	providerID, modelID, _ = strings.Cut(value, "/")
	return providerID, modelID
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

// prefixUTF16 truncates value to limit UTF-16 code units.
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
