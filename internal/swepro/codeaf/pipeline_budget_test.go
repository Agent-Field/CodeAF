package codeaf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

type retryAfterRoundTripper struct{}

func (retryAfterRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"60"}},
		Body:       io.NopCloser(strings.NewReader("slow down")),
		Request:    request,
	}, nil
}

func TestRunDeadlineInterruptsBackendBackoffAsBudgetExhaustion(t *testing.T) {
	// Validation contract 2: the cumulative run deadline reaches an in-flight
	// provider retry sleep and returns the terminal budget classification promptly.
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("PLANDB_DB", "")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)

	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(workspace+"/README.md", "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	var backingOff atomic.Bool
	backend := &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: retryAfterRoundTripper{}},
		sleep: func(ctx context.Context, duration time.Duration) error {
			backingOff.Store(true)
			return sleepContext(ctx, duration)
		},
	}
	// The provider's Retry-After is 60s, so the deadline always lands inside the
	// backoff; the budget only has to outlast pipeline setup (git, PlanDB,
	// durable storage), which can take seconds on a loaded machine.
	maxHours := 8.0 / 3600.0
	args := cliArgs{
		EntryAgent: "coder", High: "openrouter/vendor/model", MaxHours: &maxHours,
	}
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	priorWallStart := time.Now()
	started := time.Now()
	result, err := runner.run(context.Background(), "test deadline", pipelineOptions{
		WallStart: &priorWallStart,
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("run returned error instead of budget result: %v", err)
	}
	if !backingOff.Load() {
		t.Fatal("deadline expired before the provider entered retry backoff")
	}
	if result.Status != "budget-exhausted" ||
		!strings.Contains(result.Reason, "wall ") ||
		!strings.Contains(result.Reason, ">= budget 8s") {
		t.Fatalf("result = %#v", result)
	}
	// The 60s Retry-After backoff would dominate if the deadline did not cut it
	// short, so anything near the budget proves prompt interruption.
	if elapsed > 20*time.Second {
		t.Fatalf("deadline did not interrupt backoff promptly: %s", elapsed)
	}
}

func TestPipelineRunBudgetHonorsEnvPrecedenceAndResumeLineage(t *testing.T) {
	// Round 3 contract 5: CLI values beat CODEAF_MAX_*, and a resumed run uses
	// the original wall start plus prior spend instead of opening a fresh budget.
	t.Setenv("CODEAF_MAX_COST_USD", "5")
	t.Setenv("CODEAF_MAX_WALL_H", "2")
	cliCost := 7.0
	runner := newPipeline(cliArgs{MaxCost: &cliCost}, t.TempDir(), pipelineDeps{
		Backend: nil, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	if runner.budget.MaxCostUSD == nil || *runner.budget.MaxCostUSD != 7 {
		t.Fatalf("CLI max cost did not win: %#v", runner.budget)
	}
	if runner.budget.MaxWallMS == nil || *runner.budget.MaxWallMS != 2*3_600_000 {
		t.Fatalf("wall env was not resolved: %#v", runner.budget)
	}

	zero := 0.0
	resumed := newPipeline(cliArgs{MaxCost: &zero}, t.TempDir(), pipelineDeps{
		Backend: nil, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	resumed.wallStart = time.Now().Add(-3 * time.Hour)
	resumed.priorCost = 4.75
	resumed.ensureBudgetTracker()
	if resumed.budget.MaxCostUSD == nil || *resumed.budget.MaxCostUSD != 5 {
		t.Fatalf("non-positive CLI value should fall through to env: %#v", resumed.budget)
	}
	if got := resumed.totalCost(); got != 4.75 {
		t.Fatalf("restored prior cost = %v, want 4.75", got)
	}
	if exhausted, reason := resumed.budgetExhausted(); !exhausted ||
		!strings.Contains(reason, "wall 10800s >= budget 7200s") {
		t.Fatalf("resume lineage did not retain original start: exhausted=%v reason=%q", exhausted, reason)
	}
	resumed.runtime.addCost(0.25)
	if got := resumed.totalCost(); got != 5 {
		t.Fatalf("resumed cumulative cost = %v, want 5", got)
	}
}

func TestDispatchBudgetExhaustionCheckpointsInsteadOfCrashing(t *testing.T) {
	// Live-caught parity gap: run.ts:1628-1630 checkpoints and exits 0 when the
	// run budget is exhausted mid-dispatch; Go surfaced it as a crash.
	if !errors.Is(fmt.Errorf("%w: cost budget exhausted at $0.76", errRunBudget), errRunBudget) {
		t.Fatal("errRunBudget must be identifiable through wrapping")
	}
	maxCost := 0.01
	runner := &pipeline{
		args:      cliArgs{MaxCost: &maxCost},
		now:       time.Now,
		wallStart: time.Now(),
		events:    newEventWriter(io.Discard),
		notes:     io.Discard,
		runtime:   newRuntime(t.TempDir(), nil),
	}
	runner.runtime.addCost(5.0)
	err := runner.dispatchUntilQuiet(context.Background(), "p-x", "t-x", "")
	if !errors.Is(err, errRunBudget) {
		t.Fatalf("expected errRunBudget sentinel, got %v", err)
	}
}

func TestRootSchedulerBudgetExhaustionReturnsTerminalWithPlanIDs(t *testing.T) {
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	childAdded := false
	auditCalls := 0
	backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
		switch request.Agent {
		case "root-orchestrator":
			if !childAdded {
				childAdded = true
				if _, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
					Title: "open child", Project: request.PlanDB.ProjectID,
					Parent: plandb.TaskID(request.PlanDB.RootTaskID), CustomID: "budget-child",
				}); err != nil {
					return turnResult{}, err
				}
			}
			return turnResult{Text: "work dispatched", CostUSD: 2}, nil
		case "auditor", "auditor-light":
			auditCalls++
			return turnResult{Text: "pass"}, nil
		default:
			return turnResult{Text: "done"}, nil
		}
	})
	maxCost := 1.0
	runner := newPipeline(cliArgs{
		EntryAgent: "root-orchestrator", High: "provider/high", MaxCost: &maxCost,
	}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)

	result, err := runner.run(context.Background(), "implement", pipelineOptions{})
	if err != nil {
		t.Fatalf("budget exhaustion returned an error: %v", err)
	}
	if result.Status != "budget-exhausted" || result.ProjectID == "" || result.RootID == "" {
		t.Fatalf("result = %#v, want budget terminal with plan IDs", result)
	}
	if auditCalls != 0 || strings.Contains(result.Reason, "stall") {
		t.Fatalf("budget route audited or stalled: result=%#v auditCalls=%d", result, auditCalls)
	}
}
