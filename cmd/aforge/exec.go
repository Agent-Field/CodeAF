package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// execEnvelope is the machine contract for one-shot harness callers. It
// reports the linear executor's outcome rather than a resident graph summary.
type execEnvelope struct {
	Text      string          `json:"text"`
	Stop      exec.StopReason `json:"stop"`
	Usage     exec.Usage      `json:"usage"`
	Artifacts []string        `json:"artifacts"`
	Turns     int             `json:"turns"`
	ElapsedMS int64           `json:"elapsed_ms"`
}

func runExec(args []string) error {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	workspace := flags.String("w", ".", "workspace directory")
	system := flags.String("system", "", "working method for the agent")
	maxTurns := flags.Int("turns", 200, "runaway backstop on agent iterations (env AFORGE_EXEC_TURNS)")
	maxTokens := flags.Int("budget", 150000, "token budget for the agent (env AFORGE_EXEC_BUDGET)")
	timeout := flags.Int("timeout", 0, "hard wall in seconds (env AFORGE_EXEC_TIMEOUT; default: scale from the token budget)")
	model := flags.String("model", "", "work model for this run (default AFORGE_MODEL)")
	planModel := flags.String("plan-model", "", "accepted for headless model-pin parity; exec performs no planning")
	contextFill := flags.Int("context-fill", 0, "context compaction threshold in percent (default 60)")
	completionReserve := flags.Int("completion-reserve", 0, "tokens reserved for each answer and its reasoning")
	asJSON := flags.Bool("json", false, "print a machine-readable result")
	output := flags.String("o", "", "write the machine-readable result to this file")
	if err := flags.Parse(reorder(flags, args)); err != nil {
		return err
	}
	if err := applyExecEnv(flags, os.Getenv, maxTurns, maxTokens, timeout); err != nil {
		return err
	}
	if *maxTurns <= 0 || *maxTokens <= 0 {
		return fmt.Errorf("exec turns and budget must be positive")
	}
	if *timeout < 0 {
		return fmt.Errorf("exec timeout must not be negative")
	}
	if err := applyContextLaw(*contextFill, *completionReserve); err != nil {
		return err
	}
	prompt, err := readText(flags.Args())
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	applyModelFlags(&settings, *model, *planModel)
	modelCatalog := sharedCatalog(settings)
	settings.Models = modelCatalog
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	deadline := execDeadline(*maxTokens, *timeout)
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}
	ctx = settings.Context(ctx, prompt)
	execCtx := settings.ExecContext(ctx)

	space, err := exec.NewWorkspace(*workspace)
	if err != nil {
		return err
	}
	web := exec.NewWeb()
	if web == nil {
		fmt.Fprintln(os.Stderr, "note: EXA_API_KEY unset — the web tool will be unavailable")
	}

	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline).
		WithAttribution(settings.Attribution).
		WithContextLength(modelCatalog.ContextLength(settings.Model))
	outcome, runErr := linear.Run(execCtx, execTask(prompt, *system, space.Root()))
	if outcome == nil {
		outcome = &exec.Outcome{Stop: exec.StopError, Artifacts: []string{}}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "error:", runErr)
	}

	envelope := buildExecEnvelope(outcome)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	var outputErr error
	if *output != "" {
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			outputErr = err
		}
	}
	if *asJSON {
		if _, err := os.Stdout.Write(encoded); err != nil {
			outputErr = err
		}
	} else {
		if _, err := fmt.Fprintln(os.Stdout, outcome.Text); err != nil {
			outputErr = err
		}
	}
	if outputErr != nil {
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "error:", outputErr)
			return exitStatus(5)
		}
		return outputErr
	}

	if runErr != nil {
		return exitStatus(5)
	}
	if code := execExitCode(outcome.Stop, outcome.Text); code != 0 {
		return exitStatus(code)
	}
	return nil
}

// execEnvFallbacks are the three exec walls a wrapper can set once, in the
// environment, instead of threading onto every invocation — the same way
// AFORGE_MODEL is set once rather than passed per call. The caller that reached
// for exec is usually a harness whose per-call arguments are the prompt and the
// workspace and nothing else; walls belong to the campaign, not to the errand.
var execEnvFallbacks = []struct {
	flag     string
	variable string
	what     string
}{
	{flag: "turns", variable: "AFORGE_EXEC_TURNS", what: "turn cap"},
	{flag: "budget", variable: "AFORGE_EXEC_BUDGET", what: "token budget"},
	{flag: "timeout", variable: "AFORGE_EXEC_TIMEOUT", what: "number of seconds"},
}

// applyExecEnv fills in the walls the caller did not name.
//
// A flag that was typed always wins, and "typed" means typed: flag.Visit
// reports only the flags that actually appeared on the command line, so
// `--turns 200` is honoured as an explicit choice even though 200 is also the
// default. That distinction is the whole point — without it, an environment
// variable could not tell a default apart from a decision, and setting one
// would silently overrule the caller.
//
// A variable that is set but is not a number is an error rather than a shrug.
// The alternative is a harness that thinks it capped a run at 60 seconds
// because of a typo it will never see, and measures the wrong thing all night.
func applyExecEnv(flags *flag.FlagSet, getenv func(string) string, maxTurns, maxTokens, timeout *int) error {
	typed := make(map[string]bool, 3)
	flags.Visit(func(f *flag.Flag) { typed[f.Name] = true })
	targets := map[string]*int{"turns": maxTurns, "budget": maxTokens, "timeout": timeout}
	for _, fallback := range execEnvFallbacks {
		target, ok := targets[fallback.flag]
		if !ok || typed[fallback.flag] {
			continue
		}
		raw := strings.TrimSpace(getenv(fallback.variable))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s: %q is not a %s", fallback.variable, raw, fallback.what)
		}
		*target = value
	}
	return nil
}

func execDeadline(maxTokens, timeoutSeconds int) time.Duration {
	if timeoutSeconds > 0 {
		return time.Duration(timeoutSeconds) * time.Second
	}
	deadline := 15 * time.Minute
	if scaled := time.Duration(maxTokens/50_000) * time.Minute; scaled > deadline {
		deadline = scaled
	}
	return deadline
}

func execExitCode(stop exec.StopReason, text string) int {
	switch stop {
	case exec.StopDone:
		if strings.TrimSpace(text) == "" {
			return 6
		}
		return 0
	case exec.StopBudget:
		return 2
	case exec.StopTurnCap:
		return 3
	case exec.StopDeadline:
		return 4
	case exec.StopError:
		return 5
	default:
		return 5
	}
}

func buildExecEnvelope(outcome *exec.Outcome) execEnvelope {
	if outcome == nil {
		outcome = &exec.Outcome{Stop: exec.StopError}
	}
	artifacts := make([]string, len(outcome.Artifacts))
	copy(artifacts, outcome.Artifacts)
	return execEnvelope{
		Text:      outcome.Text,
		Stop:      outcome.Stop,
		Usage:     outcome.Usage,
		Artifacts: artifacts,
		Turns:     outcome.Turns,
		ElapsedMS: outcome.Elapsed.Milliseconds(),
	}
}

func execTask(prompt, system, root string) exec.Task {
	title, _, _ := strings.Cut(prompt, "\n")
	return exec.Task{
		NodeID:   1,
		Title:    strings.TrimSpace(title),
		Brief:    prompt + "\n\nWorkspace root (your working directory): " + root,
		Contract: system,
	}
}
