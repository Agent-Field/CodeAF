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
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

func runExec(args []string) error {
	flags := commandFlags("exec")
	workspace := flags.String("dir", ".", "the directory to work in, edited in place")
	shorthandFlag(flags, "w", "dir")
	system := flags.String("system", "", "working method for the agent")
	// `--turns` and `--budget` are `--max-turns` and `--token-budget` now, on
	// this door and on `aforge plan run` alike. `budget` is a word about MONEY
	// everywhere else in this product — AFORGE_DAILY_BUDGET, /budget,
	// --max-cost — so `--budget 150000` read as $150,000 exactly once, and the
	// once was enough. The bound is unchanged; only its spelling is.
	maxTurns := flags.Int("max-turns", 200, "runaway backstop on agent iterations (env AFORGE_EXEC_TURNS)")
	renamedFlag(flags, "turns", "max-turns")
	maxTokens := flags.Int("token-budget", 150000, "token budget for this run (env AFORGE_EXEC_BUDGET)")
	renamedFlag(flags, "budget", "token-budget")
	// A DURATION FLAG TAKES A DURATION, on every door that has one. This was an
	// integer of seconds while `aforge do --timeout 15m` worked, so the same
	// flag with the same job took two types and the difference showed up at the
	// door of a long unattended run (wall.go). A bare number is still seconds.
	wall := wallFlag{}
	flags.Var(&wall, "timeout", "hard wall, as a duration such as 15m or 2h (a bare number is seconds); "+
		"env AFORGE_EXEC_TIMEOUT; unset, it scales from the token budget")
	model := flags.String("model", "", modelFlagHelp)
	// `--plan-model` IS GONE FROM THIS DOOR. It was accepted "for headless
	// model-pin parity" and documented as doing nothing, which teaches a harness
	// author a wrong thing quietly: a flag list is read as a list of things that
	// have an effect, and somebody pins a planning model on a thousand calls and
	// measures the wrong thing. It is still parsed, so a script that passes it
	// keeps running, and it now says on stderr that it changed nothing.
	planModel := flags.String("plan-model", "", hiddenRenamed+"plan-model")
	contextFill := flags.Int("context-fill", 0,
		"how full a model's context window may get before it is compacted, in percent "+
			"(default "+strconv.Itoa(ctxbudget.DefaultFillPercent)+", clamped 10-90)")
	completionReserve := flags.Int("completion-reserve", 0,
		"tokens every call keeps free for its answer and its reasoning "+
			"(default "+strconv.Itoa(ctxbudget.DefaultCompletionReserveTokens)+")")
	asJSON := flags.Bool("json", false, jsonFlagHelp)
	output := flags.String("out", "", "write the machine-readable result to this file")
	shorthandFlag(flags, "o", "out")
	debug := flags.Bool("debug", false,
		"keep the full record of this run — call bodies, tool calls and the choices made — "+
			"in a folder of its own under the state root (env AFORGE_DEBUG)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	if typedFlags(flags)["plan-model"] {
		fmt.Fprintln(os.Stderr, "note: exec does not plan — --plan-model has no effect here.")
		*planModel = ""
	}
	// THE RUN ID IS MINTED AT THE DOOR, once per invocation and before anything
	// can make a call, so every record this run leaves names the same run. The
	// folder is announced on the way out and only when something was written.
	if *debug {
		trace.Enable()
	}
	traced := openDebugRecord("exec", *model, *workspace)
	defer trace.Announce(traced, os.Stderr)
	if err := applyExecEnv(flags, os.Getenv, maxTurns, maxTokens, &wall); err != nil {
		return err
	}
	if *maxTurns <= 0 || *maxTokens <= 0 {
		return fmt.Errorf("--max-turns and --token-budget must be positive")
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
	deadline := execDeadline(*maxTokens, int(wall.wall/time.Second))
	if wall.wall > 0 {
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
	{flag: "max-turns", variable: "AFORGE_EXEC_TURNS", what: "turn cap"},
	{flag: "token-budget", variable: "AFORGE_EXEC_BUDGET", what: "token budget"},
	{flag: "timeout", variable: "AFORGE_EXEC_TIMEOUT", what: "duration or number of seconds"},
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
func applyExecEnv(flags *flag.FlagSet, getenv func(string) string, maxTurns, maxTokens *int, wall *wallFlag) error {
	// TYPED IS READ THROUGH THE ALIASES (rename.go). A person who typed the old
	// `--budget` named the same wall as one who typed `--token-budget`, and an
	// environment variable that overruled the first and not the second would be
	// exactly the silent overrule this whole function is written to prevent.
	typed := typedFlags(flags)
	targets := map[string]*int{"max-turns": maxTurns, "token-budget": maxTokens}
	for _, fallback := range execEnvFallbacks {
		if typed[fallback.flag] {
			continue
		}
		raw := strings.TrimSpace(getenv(fallback.variable))
		if raw == "" {
			continue
		}
		if fallback.flag == "timeout" {
			// The wall reads the same spellings from the environment that it
			// reads from the flag, so a campaign that set `2m` in one place is
			// not refused in the other.
			if err := wall.Set(raw); err != nil {
				return fmt.Errorf("%s: %q is not a %s", fallback.variable, raw, fallback.what)
			}
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s: %q is not a %s", fallback.variable, raw, fallback.what)
		}
		*targets[fallback.flag] = value
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
