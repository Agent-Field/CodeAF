// Command codeaf ports swe-pro/src/cli/cmd/run.ts and resume.ts at commit 3b25a1a.
package codeaf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/afield"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/modelsdev"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/supervisor"
)

var version = "dev"

// defaultControlPlaneURL is where a local `af dev` control plane listens;
// used when neither CODEAF_CP_URL (serve-injected) nor AGENTFIELD_URL is set.
const defaultControlPlaneURL = "http://localhost:8080"

// probeControlPlane health-checks the control plane with retries so a
// transiently slow network or briefly busy (or restarting) control plane
// does not refuse a legitimate run; only a plane that stays unreachable
// across every attempt fails the gate. Worst case ≈ 9s: three 2s probes
// with 1s and 2s backoffs between them.
func probeControlPlane(ctx context.Context, reporter *afield.Reporter) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = reporter.Probe(probeCtx)
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}

// aforge-embed: the control-plane gate, made skippable. Upstream the gate is
// unconditional for the real backend — codeaf is not a standalone product and
// every run is mirrored onto AgentField — but an embedded run is driven by
// aforge's subharness rather than by an AgentField reasoner, and requiring a
// reachable control plane would make the engine unusable in-process. The
// literal value "off" in CODEAF_CP_URL turns the gate off; every other value,
// including the empty one, keeps the upstream condition byte-for-byte.
func controlPlaneEnabled(baseURL string, injected backend) bool {
	if baseURL == "off" {
		return false
	}
	return injected == nil || baseURL != ""
}

// aforge-embed: the shipped binary's `func main()` becomes `Main`, the one
// exported symbol of this package, so the aforge binary can BE codeaf in a
// child process (cmd/aforge/swepro.go). The body is main()'s verbatim, with
// os.Exit replaced by a returned code: passing a nil `injected` backend is
// what makes runCLI construct the real OpenRouter backend, and every startup
// semantic the binary had — the control-plane gate, the OPENROUTER_API_KEY
// hard-require, the models.dev catalog wiring, the auto-resume supervisor —
// lives inside runCLI and is therefore unchanged.
func Main(argv []string) int {
	if err := runCLI(context.Background(), argv, nil, os.Stdout, os.Stderr); err != nil {
		var exit *cliExitError
		if errors.As(err, &exit) {
			return exit.code
		}
		fmt.Fprintln(os.Stderr, "codeaf:", err)
		return 1
	}
	return 0
}

func runCLI(
	ctx context.Context,
	argv []string,
	injected backend,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(argv) > 0 && argv[0] == "serve" {
		return runServe(ctx, stdout, stderr)
	}
	args, err := parseArgs(argv)
	if err != nil {
		return err
	}
	if args.Help || args.Command == "help" {
		_, _ = io.WriteString(stdout, usage())
		return nil
	}
	if args.Version || args.Command == "version" {
		_, _ = fmt.Fprintln(stdout, version)
		return nil
	}
	switch args.Command {
	case "portfolio":
		return runPortfolio(ctx, args, stdout, stderr, defaultPortfolioDeps())
	case "arch", "review":
		return fmt.Errorf(
			"%q is not available in this Go port; use codeaf run or codeaf resume",
			args.Command,
		)
	case "run", "resume":
	default:
		return fmt.Errorf("unknown command: %s", args.Command)
	}
	if args.TUI {
		_, _ = io.WriteString(
			stderr,
			"[codeaf] --tui is unsupported in the Go port; omit --tui to use the supported headless NDJSON event stream.\n",
		)
		return &cliExitError{code: 1}
	}
	workspace := args.Directory
	if workspace == "" {
		workspace, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return err
	}
	loadedConfig, err := loadCodeafConfig(workspace)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	events := newEventWriter(stdout)
	client := injected
	if client == nil {
		client = defaultBackend(args.Variant)
		loadedConfig.applyBackend(client.(*openRouterBackend))
	}
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: client, Config: loadedConfig, Events: events, Notes: stderr,
	})
	defer runner.runtime.Close()

	goal := args.Message
	options := pipelineOptions{}
	if args.Command == "resume" {
		if os.Getenv("PLANDB_DB") == "" {
			_ = os.Setenv("PLANDB_DB", filepath.Join(workspace, ".plandb.db"))
		}
		plandb.OpenPlanDB(plandb.PlanDBPath(workspace))
		checkpoint := rehydrateCheckpoint(workspace)
		if !checkpoint.HasCheckpoint {
			_, _ = fmt.Fprintf(
				stderr,
				"[codeaf] resume: no resumable checkpoint found in %s; nothing to continue.\n",
				workspace,
			)
			return nil
		}
		file := readResumeCheckpoint(workspace)
		if goal == "" && file != nil {
			goal = file.Goal
		}
		if goal == "" {
			return errors.New(
				`resume could not recover the goal; pass it explicitly: codeaf resume "<prompt>"`,
			)
		}
		stale := releaseStaleClaims()
		ledger := ledgers.ReadAttempts(workspace, ledgers.AuditFixKey(goal))
		options.Resume = true
		options.ResumeSeed = buildResumeSeed(goal, checkpoint, ledger, stale)
		options.LastCycle = checkpoint.LastCycle
		if file != nil {
			if file.WallStartTS != nil {
				value := time.UnixMilli(int64(*file.WallStartTS))
				options.WallStart = &value
			}
			if file.CostSpentUSD != nil {
				options.PriorCost = *file.CostSpentUSD
			}
		}
	}
	if goal == "" {
		return errors.New("missing message; pass a prompt as a positional argument")
	}
	if len(splitPool(args.High)) == 0 {
		return errors.New("--high MODEL is required")
	}
	// codeaf is not a standalone product: run/resume require a reachable
	// AgentField control plane and every run is mirrored onto it. Injected
	// backends exist only for in-process tests (the shipped binary cannot
	// construct one); they keep the pre-gate behavior — bridge only when
	// CODEAF_CP_URL is set, degrade gracefully when it is unreachable — so
	// parity tests stay hermetic.
	var bridge *cpBridge
	baseURL := strings.TrimSpace(os.Getenv("CODEAF_CP_URL"))
	if controlPlaneEnabled(baseURL, injected) {
		if baseURL == "" {
			baseURL = strings.TrimSpace(os.Getenv("AGENTFIELD_URL"))
		}
		if baseURL == "" {
			baseURL = defaultControlPlaneURL
		}
		nodeID := os.Getenv("CODEAF_CP_NODE")
		if nodeID == "" {
			nodeID = "swe-pro-go"
		}
		runID := os.Getenv("CODEAF_CP_RUN")
		if runID == "" {
			runID = fmt.Sprintf("codeaf-%d", time.Now().UnixMilli())
		}
		reporter := afield.NewReporter(afield.Options{
			BaseURL: baseURL,
			RunID:   runID,
			NodeID:  nodeID,
			Logf: func(format string, values ...any) {
				_, _ = fmt.Fprintf(stderr, format+"\n", values...)
			},
		})
		probeErr := probeControlPlane(ctx, reporter)
		if probeErr != nil {
			reporter.Close()
			if injected == nil {
				return fmt.Errorf(
					"codeaf requires a running AgentField control plane and cannot "+
						"be used standalone: probe of %s failed (%v); start one "+
						"(af dev) or point AGENTFIELD_URL at it",
					baseURL, probeErr,
				)
			}
			_, _ = fmt.Fprintf(
				stderr, "[codeaf] control plane disabled: %v\n", probeErr,
			)
		} else {
			bridge = newCPBridge(
				reporter, os.Getenv("CODEAF_CP_PARENT"), goal,
			)
			bridge.setRootInput(map[string]any{
				"dir":        workspace,
				"high_model": args.High,
				"low_model":  args.Low,
				"hard":       args.Hard,
				"frontier":   args.Frontier,
			})
			events.setHook(bridge.consume)
			defer reporter.Close()
		}
	}

	// Fail closed before any phase runs. Without a key the classifier falls
	// back on an unparseable response and architecture fails, so a run burns
	// three stages rediscovering a precondition visible at startup. Checked
	// after the control-plane gate so its refusal keeps precedence, and only
	// for the real backend — injected ones are test doubles that need no key.
	if injected == nil && os.Getenv("OPENROUTER_API_KEY") == "" {
		return errors.New(
			"OPENROUTER_API_KEY is not set in the environment; " +
				"export it before running codeaf",
		)
	}
	if injected == nil {
		catalogClient, catalogErr := modelsdev.NewFromEnv(version)
		if catalogErr != nil {
			return fmt.Errorf("model catalog: %w", catalogErr)
		}
		catalog, catalogErr := catalogClient.Get(ctx)
		if catalogErr != nil {
			return fmt.Errorf("model catalog: %w", catalogErr)
		}
		client.(*openRouterBackend).catalog = catalog
		catalogClient.StartRefresh(ctx, func(refreshErr error) {
			_, _ = fmt.Fprintf(stderr, "[codeaf] failed to fetch models.dev: %v\n", refreshErr)
		})
	}

	runner.cpBridge = bridge
	stopLeafPoller := func() {}
	if bridge != nil {
		stopLeafPoller = bridge.startLeafPoller(ctx)
	}
	defer stopLeafPoller()
	result, runErr := runner.run(ctx, goal, options)
	stopLeafPoller()
	result, runErr = classifyRunError(runner, result, runErr)
	// A refused request produced no work, so there is nothing to continue;
	// leaving a checkpoint would make `codeaf resume` offer to re-run a goal
	// the intake gate already rejected.
	if result.Status != "pass" && result.Status != "refused" {
		wall := float64(result.WallStart.UnixMilli())
		cost := result.CostUSD
		writeTerminalCheckpoint(workspace, resumeCheckpointFile{
			Goal: goal, SessionID: runner.sessionID,
			FinalStatus: result.Status, Cycle: float64(result.Cycle),
			Reason: result.Reason, WallStartTS: &wall, CostSpentUSD: &cost,
		})
	}
	events.emit(event{
		Type: "terminal", Status: result.Status, Message: result.Reason,
		SessionID: runner.sessionID,
		Data: map[string]any{
			"cycle": result.Cycle, "cost_usd": result.CostUSD,
			"project_id": result.ProjectID, "root_task_id": result.RootID,
		},
	})
	if runErr != nil {
		return runErr
	}
	if result.Status == "fail" || result.Status == "escalated" {
		if supervisor.AutoResumeEnabled(nil) && os.Getenv("CODEAF_SUPERVISED") != "1" {
			_, superviseErr := runAutoResume(
				ctx, args, workspace, events, stderr,
			)
			if superviseErr != nil {
				return superviseErr
			}
		}
		return nil
	}
	return nil
}

func runAutoResume(
	ctx context.Context,
	args cliArgs,
	workspace string,
	events *eventWriter,
	stderr io.Writer,
) (supervisor.SupervisorOutcome, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	reader := supervisor.VerdictReaderFunc(func(workspace string) (*supervisor.DiskVerdict, error) {
		verdict, readErr := auditorgate.ReadVerdictFile(workspace)
		if readErr != nil || verdict == nil {
			return nil, readErr
		}
		blockers := make([]any, len(verdict.Blockers))
		for index, blocker := range verdict.Blockers {
			blockers[index] = blocker
		}
		return &supervisor.DiskVerdict{
			Verdict:  supervisor.VerdictStatus(verdict.Verdict),
			Blockers: blockers,
		}, nil
	})
	snapshot := func() (supervisor.RatchetSnapshot, error) {
		return supervisor.ReadRatchetSnapshot(workspace, reader)
	}
	result, err := supervisor.RunSupervisor(supervisor.SupervisorDeps{
		ResumeOnce: func(_ int) error {
			childArgs := []string{
				"resume", "--dir", workspace, "--high", args.High,
				"--low", args.Low, "--frontier", args.Frontier,
				"--variant", args.Variant, "--format", args.Format,
			}
			if args.PRReady {
				childArgs = append(childArgs, "--pr-ready")
			}
			if args.Hard {
				childArgs = append(childArgs, "--hard")
			}
			if args.MaxCost != nil {
				childArgs = append(childArgs, "--max-cost", fmt.Sprint(*args.MaxCost))
			}
			if args.MaxHours != nil {
				childArgs = append(childArgs, "--max-hours", fmt.Sprint(*args.MaxHours))
			}
			command := exec.CommandContext(ctx, executable, childArgs...)
			command.Stdout, command.Stderr = os.Stdout, stderr
			command.Env = append(os.Environ(), "CODEAF_SUPERVISED=1")
			return command.Run()
		},
		Snapshot: snapshot,
		BudgetExhausted: func() supervisor.BudgetExhaustion {
			file := readResumeCheckpoint(workspace)
			if file != nil && strings.HasPrefix(file.FinalStatus, "budget-exhausted") {
				reason := file.Reason
				return supervisor.BudgetExhaustion{Yes: true, Reason: &reason}
			}
			return supervisor.BudgetExhaustion{}
		},
		Log: func(message string) {
			_, _ = io.WriteString(stderr, message)
		},
	}, nil)
	if err != nil {
		return "", err
	}
	events.emit(event{
		Type: "supervisor", Stage: "auto-resume", Status: string(result.Outcome),
		Message: result.Reason, Data: map[string]any{"attempts": result.Attempts},
	})
	return result.Outcome, nil
}

// classifyRunError maps a pipeline error onto the terminal result.
// run.ts:1628-1630 checkpoints and exits 0 on a budget stop no matter which
// phase crossed the ceiling (run T2 hit it inside the audit-fix loop, whose
// callers do not map the sentinel). Only harness errors crash.
func classifyRunError(runner *pipeline, result pipelineResult, runErr error) (pipelineResult, error) {
	if runErr == nil {
		return result, nil
	}
	result.CostUSD = runner.totalCost()
	result.WallStart = runner.wallStart
	if errors.Is(runErr, errRunBudget) {
		result.Status = "budget-exhausted"
		if exhausted, reason := runner.budgetExhausted(); exhausted && reason != "" {
			result.Reason = reason
		} else {
			result.Reason = runErr.Error()
		}
		return result, nil
	}
	result.Status = "crashed"
	result.Reason = runErr.Error()
	return result, runErr
}
