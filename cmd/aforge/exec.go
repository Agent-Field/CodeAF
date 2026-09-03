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
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

func runExec(args []string) error {
	flags := commandFlags("exec")
	workspace := flags.String("w", ".", "workspace directory")
	system := flags.String("system", "", "working method for the agent")
	maxTurns := flags.Int("turns", 200, "runaway backstop on agent iterations (env AFORGE_EXEC_TURNS)")
	maxTokens := flags.Int("budget", 150000, "token budget for the agent (env AFORGE_EXEC_BUDGET)")
	timeout := flags.Int("timeout", 0, "hard wall in seconds (env AFORGE_EXEC_TIMEOUT; default: scale from the token budget)")
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", "accepted for headless model-pin parity; exec performs no planning")
	contextFill := flags.Int("context-fill", 0, "context compaction threshold in percent (default 60)")
	completionReserve := flags.Int("completion-reserve", 0, "tokens reserved for each answer and its reasoning")
	asJSON := flags.Bool("json", false, jsonFlagHelp)
	output := flags.String("o", "", "write the machine-readable result to this file")
	debug := flags.Bool("debug", false,
		"keep the full record of this run — call bodies, tool calls and the choices made — "+
			"in a folder of its own under the state root (env AFORGE_DEBUG)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	// THE RUN ID IS MINTED AT THE DOOR, once per invocation and before anything
	// can make a call, so every record this run leaves names the same run. The
	// folder is announced on the way out and only when something was written.
	if *debug {
		trace.Enable()
	}
	traced := openDebugRecord("exec", *model, *workspace)
	defer trace.Announce(traced, os.Stderr)
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
	prompt, err := readText(flags.Name(), flags.Args())
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	// Both seats are resolved through the one ladder even here, where only one
	// of them is ever sat in: --plan-model is accepted for parity, and a door
	// that took the flag and then resolved it differently from every other door
	// would be the parity it claims in name only. Only the work seat is printed,
	// because only the work seat runs anything.
	seats := config.ResolveSeats(settings.ProfileDir, *model, *planModel)
	applySeats(&settings, seats)
	fmt.Fprintln(os.Stderr, seats.Work.Report())
	modelCatalog := sharedCatalog(settings)
	settings.Models = modelCatalog
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)

	ctx, stopSignals := signal.NotifyContext(traced, os.Interrupt, syscall.SIGTERM)
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
		// The same sentence the envelope carries, and the same one either way:
		// a caller reading stderr and a caller reading --json must not be told
		// two different things about one failure (plainwords.go).
		fmt.Fprintln(os.Stderr, "error:", execFailureWords(runErr))
	}

	envelope := buildExecEnvelope(outcome, runErr, settings.Model)
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
			return execExit(outcome, runErr)
		}
		return outputErr
	}

	if runErr != nil {
		return execExit(outcome, runErr)
	}
	if code := execExit(outcome, nil); code != exitDone {
		return code
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
	// The shape is the generalist's, asked for and never worked out again:
	// exec.SubharnessInfo.Deadline is the one place in the process that knows
	// the floor and the per-token scaling.
	return exec.SubharnessFor(exec.LinearSubharness).Deadline(maxTokens)
}

// execStop says how this run ended, in the ONE vocabulary all three headless
// verbs speak (envelope.go). It is the whole of exec's opinion about its own
// ending; what that costs the process is the ladder's business.
//
// The executor's own five reasons pass through under their own names, because
// harnesses read those words out of `--json` and they must not move. The two
// readings this function adds are the ones a raw StopReason cannot make:
//
//   - "done" with nothing to show is INCOMPLETE. The loop stopped asking for
//     tools and produced no text, which is a run that did not finish however
//     calmly it ended. This is the old exit 6, and the field it changes is the
//     one value in `stop` that moves in this change.
//   - a run that came back with an error and no reason of its own could not be
//     run at all. Where it DID name a reason — the wall, the budget — that
//     reason is kept, because the error is what the wall left behind and not
//     what stopped it.
func execStop(outcome *exec.Outcome, runErr error) stopReason {
	if outcome == nil {
		return stopError
	}
	stop := stopReason(outcome.Stop)
	switch outcome.Stop {
	case exec.StopDone:
		if strings.TrimSpace(outcome.Text) == "" {
			stop = stopIncomplete
		}
	case exec.StopBudget, exec.StopTurnCap, exec.StopDeadline, exec.StopError:
		// Already one of the shared words, spelled identically.
	default:
		// promote, paused, cancelled, empty, split, overrun: it ran, and this
		// is not a rung of its own. The word itself survives in `stop`.
	}
	if runErr != nil && stop == stopDone {
		return stopError
	}
	return stop
}

// execExit is exec's one exit decision, and the ESCAPE HATCH lives in it.
func execExit(outcome *exec.Outcome, runErr error) exitStatus {
	if legacyExitCodes() {
		// EXACTLY THE OLD NUMBERS, taken from the old code path and nothing
		// else: a run that came back with an error was 5 whatever its stop
		// reason said, and everything else was execLegacyExitCode's table.
		if runErr != nil {
			return exitStatus(5)
		}
		return exitStatus(execLegacyExitCode(outcome.Stop, outcome.Text))
	}
	return exitFor(execStop(outcome, runErr))
}

// execLegacyExitCode is `aforge exec`'s exit table AS IT WAS, kept for one
// release behind AFORGE_EXIT_CODES=legacy and reached from nowhere else. It is
// deliberately left exactly as it was written rather than rebuilt out of the
// ladder: its whole job is to be the old numbers, and a version of it derived
// from the new table would stop being that the first time the table moved.
func execLegacyExitCode(stop exec.StopReason, text string) int {
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

// buildExecEnvelope maps what the linear executor knows onto the one machine
// contract every headless verb returns (envelope.go), so the object written to
// --json, the object written to -o and the sentence printed on stderr cannot
// drift apart — and so that a tool that reads `aforge do --json` reads this
// without being rewritten.
func buildExecEnvelope(outcome *exec.Outcome, runErr error, model string) resultEnvelope {
	if outcome == nil {
		outcome = &exec.Outcome{Stop: exec.StopError}
	}
	artifacts := make([]string, len(outcome.Artifacts))
	copy(artifacts, outcome.Artifacts)
	return buildResultEnvelope(runResult{
		Stop:      execStop(outcome, runErr),
		Answer:    outcome.Text,
		Files:     artifacts,
		Error:     execFailureWords(runErr),
		SpendUSD:  outcome.Usage.Cost,
		TokensIn:  outcome.Usage.PromptTokens,
		TokensOut: outcome.Usage.CompletionTokens,
		Seconds:   outcome.Elapsed.Seconds(),
		Model:     model,
		Steps:     outcome.Turns,
		Extra:     legacyExecFields(outcome),
	})
}

// execFailureWords is one failure said once, in words a person can act on.
//
// exec runs a single leaf, so the node id every error inside it is wrapped with
// is machinery here — there is only ever the one node, and naming it in front
// of the cause pushes the cause off the front of the line.
func execFailureWords(runErr error) string {
	if runErr == nil {
		return ""
	}
	return plainWords(strings.TrimPrefix(strings.TrimSpace(runErr.Error()), "node "+execNodeKey+": "))
}

// execNodeKey names the single leaf `aforge exec` runs. It is spelled once so
// the call log, the artifact bucket and the flight recorder cannot disagree
// about who did the work.
const execNodeKey = "task-1"

func execTask(prompt, system, root string) exec.Task {
	title, _, _ := strings.Cut(prompt, "\n")
	return exec.Task{
		NodeID: 1,
		// The one node this command runs, named rather than left to the
		// number. `aforge exec` has a single leaf, and every model call it
		// makes is that leaf's work; without a key the call log's node column
		// was blank for the whole run, so a person reading the record after it
		// could not tell an exec row from a row with no work behind it at all.
		NodeKey:  execNodeKey,
		Title:    strings.TrimSpace(title),
		Brief:    prompt + "\n\nWorkspace root (your working directory): " + root,
		Contract: system,
	}
}
