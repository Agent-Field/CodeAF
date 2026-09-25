package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/exec"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func runExec(args []string) error {
	flags := commandFlags("exec")
	workspace := flags.String("dir", ".", "the directory to work in, edited in place")
	shorthandFlag(flags, "w", "dir")
	system := flags.String("system", "", "working method for the agent")
	// `--turns` and `--budget` are `--max-turns` and `--token-budget` now, on
	// this door and on `codeaf plan run` alike. `budget` is a word about MONEY
	// everywhere else in this product — CODEAF_DAILY_BUDGET, /budget,
	// --max-cost — so `--budget 150000` read as $150,000 exactly once, and the
	// once was enough. The bound is unchanged; only its spelling is.
	// A BAD COUNT IS REFUSED WITH A SENTENCE ABOUT THE FLAG, exactly as
	// `logs --tail` already refuses one (count.go). These two answered
	// `invalid value "notanumber" for flag -turns: parse error` — [strconv]'s
	// word for it, reaching a person through two layers, neither of which
	// wrote it for anybody to read: it says nothing about what the flag takes
	// and nothing to do next. The hidden old spellings write through to these,
	// so `--turns` and `--budget` are refused in the same words.
	maxTurns := newCountFlag(flags, "max-turns", 200, "turns to allow",
		"runaway backstop on agent iterations (env CODEAF_EXEC_TURNS)")
	renamedFlag(flags, "turns", "max-turns")
	// THE DEFAULT IS THE EXECUTOR'S OWN GRANT, not a number restated at this
	// door. It was spelled here, in run.go and in the chat surface, so four
	// places had to be recalibrated together and the help text could tell a
	// person a figure the loop no longer used.
	maxTokens := newCountFlag(flags, "token-budget", exec.DefaultLeafTokens, "tokens to allow",
		"token budget for this run (env CODEAF_EXEC_BUDGET)")
	renamedFlag(flags, "budget", "token-budget")
	// A DURATION FLAG TAKES A DURATION, on every door that has one. This was an
	// integer of seconds while `codeaf do --timeout 15m` worked, so the same
	// flag with the same job took two types and the difference showed up at the
	// door of a long unattended run (wall.go). A bare number is still seconds.
	wall := wallFlag{}
	flags.Var(&wall, "timeout", "hard wall, as a duration such as 15m or 2h (a bare number is seconds); "+
		"env CODEAF_EXEC_TIMEOUT; unset, it scales from the token budget")
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
	debug := flags.Bool("debug", false, debugFlagHelp())
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
	if err := applyExecEnv(flags, env.Value, maxTurns, maxTokens, &wall); err != nil {
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
	useAutoSeats(settings)
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
	deadline := execDeadline(*maxTokens, wall.wall)
	if wall.wall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}
	ctx = settings.Context(ctx, prompt)
	execCtx := typedDoorContext(settings.ExecContext(ctx))

	space, err := exec.NewWorkspace(*workspace)
	if err != nil {
		return err
	}
	web := exec.NewWeb()
	if web == nil {
		fmt.Fprintln(os.Stderr, "note: EXA_API_KEY unset — the web tool will be unavailable")
	}

	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline).
		WithAssistedBy(config.AssistedByModelAt(settings.ProfileDir, settings.Model)).
		WithContextLength(modelCatalog.ContextLength(settings.Model))
	// NO OUTCOME IS CARRIED AS NO OUTCOME, all the way to the ladder. This used
	// to substitute an `exec.Outcome{Stop: exec.StopError}` here, which threw
	// away the one fact exit 1 is about — whether anything ran at all — and
	// made a run that never started indistinguishable from one that failed at
	// turn nine. [execStop] is the only reader of that difference now.
	outcome, runErr := linear.Run(execCtx, execTask(prompt, *system, space.Root()))
	if runErr != nil {
		// The same sentence the envelope carries, and the same one either way:
		// a caller reading stderr and a caller reading --json must not be told
		// two different things about one failure (plainwords.go).
		fmt.Fprintln(os.Stderr, "error:", execFailureWords(runErr))
	}

	// THE RUN LEAVES A PENDING JUDGE RECORD AT ITS TAIL. A headless run builds
	// no session graph and so has no live landing hook; the pool's restart-time
	// sweep is what scores it, and this one line is the only thing that survives
	// the process to reach that sweep. NOTHING WAITS ON A JUDGE: the write is one
	// O_APPEND of one line and the process exits at once, exactly as a chat
	// task's landing is judged off the turn's own road. A pool that forbids
	// reading is asked for nothing, and a run that never ran — or broke with
	// nothing to show — leaves no record, because a landing with no report is a
	// question with nothing to score.
	if outcome != nil && (runErr == nil || strings.TrimSpace(outcome.Text) != "" || len(outcome.Artifacts) > 0) &&
		config.ModelPoolAt(settings.ProfileDir).CanRead() {
		landing := session.TaskLanding{
			// The restart sweep dedups on the id (pool/judged/<id>-<attempt>), so
			// it has to be unique per run: the wall clock in nanoseconds is the
			// one thing two runs of this process cannot share.
			ID:      uint64(time.Now().UnixNano()),
			State:   session.TaskUnverified,
			Brief:   prompt,
			Report:  outcome.Text,
			Wrote:   outcome.Artifacts,
			Changed: len(outcome.Artifacts),
			Worker:  settings.Model,
			CostUSD: outcome.Usage.Cost,
			Tokens:  outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens,
		}
		if err := writePendingLanding(settings.ProfileDir, "exec", landing); err != nil && trace.Enabled() {
			log.Printf("exec: pending landing: %v", err)
		}
	}

	// AN EXEC RUN'S WORKER SPEND REACHES THE LEDGER, under the run's own root,
	// the way a chat seat's calls do — so the status row, the run cap and the
	// pool's accounting see what this run spent instead of a machine that looks
	// to have spent nothing. The row is minted HERE and not inside the runner,
	// and that is forced rather than chosen: internal/session imports
	// internal/exec (beltfacts.go), so internal/exec cannot reach the ledger's
	// package without a cycle. The row's shape is a seat's exactly; only its
	// grain differs, one row per run where a seat writes one per call.
	recordExecUsage(settings.Model, outcome, trace.RunFrom(traced), space.Root())

	envelope := buildExecEnvelope(outcome, runErr, settings.Model, trace.RunFrom(traced), space.Root())
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
		// The envelope's own answer, so the person reading stdout and the script
		// reading `--json` are handed the same string rather than two readings
		// of the same facts — and so a run that came back with no outcome at all
		// prints nothing instead of dereferencing one.
		if _, err := fmt.Fprintln(os.Stdout, envelope.Answer); err != nil {
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

// recordExecUsage puts one exec run's worker spend on this machine's ledger,
// under the run's own root.
//
// THE TWO NAMES ARE THE WORKER'S, because that is what this run is: one leaf
// doing the work, like a task node's own turns. The role says so in the
// ledger's own vocabulary ([roles.RoleWorker]) and the seat is the one that does
// the work ([session.SeatWorker]) rather than the role's registered tier — the
// low seat an adaptive run's many small nodes sit on — because the seat names
// the chair this run actually ran in.
//
// AND THE CALLS ARE THE RUN'S OWN REQUEST COUNT, not one. The row is one per
// run rather than one per call (the row is minted at the door because
// internal/session imports internal/exec, so the runner cannot reach the
// ledger's package without a cycle), so [session.UsageLine.Calls] carries the
// whole run's requests and the tokens and dollars beside it are the whole run's
// spend: a reader summing the Calls column gets the run's true request count
// instead of the count of rows.
//
// A RUN THAT MADE NO CALL LEAVES NOTHING. [session.RecordUsage] refuses a row
// whose cost and tokens are all zero — a row that looks measured and is not —
// so the empty outcome of a run that priced nothing, and the nil outcome of one
// that never started, both write no line.
//
// THE WRITER IS ASYNC ([session.RecordUsage]), so the flush is what makes the
// row outlive the process: exec exits the moment this returns, exactly as the
// chat surface waits on its way out ([v3Process.closeAll]).
func recordExecUsage(model string, outcome *exec.Outcome, root, workspace string) {
	if outcome == nil {
		return
	}
	line := session.UsageLine{
		Model:     model,
		Calls:     outcome.Usage.Calls,
		Input:     outcome.Usage.PromptTokens,
		Output:    outcome.Usage.CompletionTokens,
		USD:       outcome.Usage.Cost,
		Root:      root,
		Workspace: workspace,
	}
	session.RecordUsage(session.UsageLedgerPath(), session.TagUsage(line, roles.RoleWorker, session.SeatWorker))
	session.CloseUsage()
}

// execEnvFallbacks are the three exec walls a wrapper can set once, in the
// environment, instead of threading onto every invocation — the same way
// CODEAF_MODEL is set once rather than passed per call. The caller that reached
// for exec is usually a harness whose per-call arguments are the prompt and the
// workspace and nothing else; walls belong to the campaign, not to the errand.
var execEnvFallbacks = []struct {
	flag     string
	variable string
	what     string
}{
	{flag: "max-turns", variable: "CODEAF_EXEC_TURNS", what: "turn cap"},
	{flag: "token-budget", variable: "CODEAF_EXEC_BUDGET", what: "token budget"},
	{flag: "timeout", variable: "CODEAF_EXEC_TIMEOUT", what: "duration or number of seconds"},
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

// execDeadline is the room this run gets: the wall the caller typed if they
// typed one, and otherwise the generalist's shape asked for by name.
//
// IT TAKES THE WALL AS THE DURATION IT ALREADY IS. It took an integer of
// seconds while `--timeout` was an integer of seconds, and when the flag grew
// units (wall.go) the call site kept the old door by dividing the duration back
// down — `execDeadline(*maxTokens, int(wall.wall/time.Second))` — which is a
// leaf's room being worked out at the dispatch site, the one thing
// `TestOnlyTheSubharnessTableSizesALeafsRoom` exists to refuse. It also lost
// everything under a second on the way through, so a wall below one second
// truncated to zero and fell through to the table's fifteen minutes: the
// opposite of what was typed.
func execDeadline(maxTokens int, wall time.Duration) time.Duration {
	if wall > 0 {
		return wall
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
//     calmly it ended. This is the old exit 6.
//   - AN OUTCOME THAT EXISTS MEANS IT RAN, and `exec.StopError` is an outcome.
//     The executor writes it when a model call fails mid-loop — at turn nine, on
//     a run that has already spent money and may be holding half an answer — and
//     mapping it to `error` put that run on exit 1, the rung whose whole meaning
//     is that nothing was attempted. A script reading the ladder retried it as a
//     startup failure or threw its evidence away. It is `incomplete`: it ran,
//     and part of the work does not stand.
//
// So the rule this function keeps, and [TestOnlyARunWithNoOutcomeAtAllCouldNotBeRunAtAll]
// pins: `stopError` COMES BACK FROM NO OUTCOME AND FROM NOTHING ELSE. Every
// refusal that really is exit 1 — no key, a flag that would not parse, a
// workspace that would not open — is returned from [runExec] before the
// executor is ever reached, and never arrives here at all.
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
	case exec.StopError:
		// It ran and it broke. The provider's own sentence is not lost: it goes
		// to `incomplete` in the envelope below, beside whatever text the run
		// had managed by then.
		stop = stopIncomplete
	case exec.StopBudget, exec.StopTurnCap, exec.StopDeadline:
		// Already one of the shared words, spelled identically.
	default:
		// promote, paused, cancelled, empty, split, overrun: it ran, and this
		// is not a rung of its own. The word itself survives in `stop`.
	}
	if runErr != nil && stop == stopDone {
		// A finished run handed back with an error beside it: it ran, so this
		// is the one thing a caller must not read as "it never started".
		return stopIncomplete
	}
	return stop
}

// execExit is exec's one exit decision, and the ESCAPE HATCH lives in it.
func execExit(outcome *exec.Outcome, runErr error) exitStatus {
	if legacyExitCodes() {
		// EXACTLY THE OLD NUMBERS, taken from the old code path and nothing
		// else: a run that came back with an error was 5 whatever its stop
		// reason said, and everything else was execLegacyExitCode's table. The
		// rung `exec.StopError` lands on above MOVED and this did not, which is
		// the entire purpose of the hatch.
		if runErr != nil || outcome == nil {
			return exitStatus(5)
		}
		return exitStatus(execLegacyExitCode(outcome.Stop, outcome.Text))
	}
	return exitFor(execStop(outcome, runErr))
}

// execLegacyExitCode is `codeaf exec`'s exit table AS IT WAS, kept for one
// release behind CODEAF_EXIT_CODES=legacy and reached from nowhere else. It is
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
// drift apart — and so that a tool that reads `codeaf do --json` reads this
// without being rewritten.
//
// THE STOP IS DERIVED ONCE AND `error` FOLLOWS IT. `error` is documented in
// envelope.go as "why the run did not produce an answer", and "empty on every
// run that produced one" — so it may only be filled on the stop that means the
// run could not be run at all. It used to be filled from `runErr`
// unconditionally, while [execStop] deliberately KEEPS `budget`, `turn-cap` and
// `deadline` when the executor hands back an outcome and an error together. A
// budget stop that produced partial text therefore published an answer AND an
// error at once, and a script following the written contract either threw the
// partial answer away or reported a startup failure that never happened.
//
// The limit's own sentence is not lost: it goes to `incomplete`, which is
// already the name `codeaf run` publishes "the reason it did not finish" under
// ([subharnessRun.sayEnvelope]), so the two verbs say one thing one way rather
// than growing a second word for it.
func buildExecEnvelope(outcome *exec.Outcome, runErr error, model, run, workspace string) resultEnvelope {
	// THE STOP IS READ OFF THE OUTCOME BEFORE THE OUTCOME IS INVENTED. A nil
	// outcome is the one thing that means "it never ran", so substituting an
	// empty one first would erase the fact the rung is about.
	stop := execStop(outcome, runErr)
	if outcome == nil {
		outcome = &exec.Outcome{}
	}
	artifacts := make([]string, len(outcome.Artifacts))
	copy(artifacts, outcome.Artifacts)
	said := execFailureWords(runErr)
	failure := ""
	extra := legacyExecFields(outcome)
	switch {
	case stop == stopError:
		failure = said
	case said != "":
		extra[envelopeIncomplete] = said
	}
	// THE WORK'S ADDRESS AND ITS VERDICT, in the one vocabulary #1182 gave
	// `do`. The branch is named wherever a verdict is: the workspace is the
	// only address this door's work can have, and a run whose git cannot answer
	// names none without losing the word beside it.
	verdict := execVerdict(outcome, runErr)
	branch := ""
	if verdict != "" {
		branch = keptBranchIn(workspace)
	}
	return buildResultEnvelope(runResult{
		Stop:       stop,
		Answer:     outcome.Text,
		Files:      artifacts,
		Error:      failure,
		SpendUSD:   outcome.Usage.Cost,
		TokensIn:   outcome.Usage.PromptTokens,
		TokensOut:  outcome.Usage.CompletionTokens,
		Seconds:    outcome.Elapsed.Seconds(),
		Model:      model,
		Steps:      outcome.Turns,
		Run:        run,
		KeptBranch: branch,
		Verdict:    verdict,
		// `exec` does not plan and cannot grow, so `rounds` is left at the zero
		// the contract documents as an absent measurement — the key is there
		// for a caller that reads one object shape across all three verbs.
		Calls: calllog.CallsFor(run),
		Extra: extra,
	})
}

// execVerdict says the record's own word for where this run's work stands, in
// the vocabulary #1182 gave `do` and #1184 gave the chat tasks text: `failed`
// for a run that broke with nothing to show, `unverified` for one that produced
// work nobody has judged.
//
// THE WORD IS THE LANDING'S WORD, decided by the same condition. An exec run
// leaves a pending judge record whenever it ran and has anything to show — the
// ordinary end — and that record's state is `session.TaskUnverified`, because
// nobody has judged it. A run the condition refuses — one that never started,
// or broke with no text and no artifacts — leaves no landing and no work, and
// the record's word for that is `session.TaskFailed`. Reading the word off the
// condition the landing already uses is what keeps the two from ever
// disagreeing: a caller that sees `verdict: unverified` knows the pending file
// holds this run's row.
func execVerdict(outcome *exec.Outcome, runErr error) string {
	if outcome == nil {
		return string(session.TaskFailed)
	}
	if runErr != nil && strings.TrimSpace(outcome.Text) == "" && len(outcome.Artifacts) == 0 {
		return string(session.TaskFailed)
	}
	return string(session.TaskUnverified)
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

// execNodeKey names the single leaf `codeaf exec` runs. It is spelled once so
// the call log, the artifact bucket and the flight recorder cannot disagree
// about who did the work.
const execNodeKey = "task-1"

// typedDoorContext says who the calls of a command a person typed are made for.
//
// THE MEASURED FAILURE (2026-09-13). Under `simple` the talk pin rides only the
// calls somebody is reading (internal/provider's drawLaneChoice asks the role's
// Visible), and a headless door that stamped nothing ran its leaf as
// [lane.RoleUnknown] — a hidden background errand — so a pinned `codeaf exec`
// went out with no machine named while the row still said one. `codeaf do` and
// `codeaf plan new` are the same shape: one command, one person waiting on it,
// and cmd/codeaf's lanepin_doors_test names all three as the doors whose first
// request must carry the pin.
//
// IT IS A LEAF AND NOT THE TALK, because each of these is one leaf's work and
// not a conversation: there is no turn loop, no room and no transcript here,
// and [lane.RoleLeafAttached] is exactly "that leaf, with somebody in front of
// it".
//
// AND THE DOOR IS THE MARK. Nothing in this binary launches these commands as
// a child — every spawner builds its leaves in process (subharness.go's
// buildLinear, chatv3_subharness.go's registry, the subharness's own asks in
// subharness_env.go, which names [lane.RoleLeafUnattended] itself) and none of
// them reaches this function. So there is no env var or flag to key on and
// none is invented: arriving here IS the fact that a person typed the command.
//
// IT SAYS THE FACT TWICE BECAUSE TWO THINGS READ IT: the leaf this door runs
// itself takes the role off its context, and everything `codeaf do` runs
// through the session's own executor — its planning pass, its nodes — takes
// the fact from the process-wide latch (internal/provider's readByAPerson and
// internal/session's someoneIsWatching), which no context reaches.
func typedDoorContext(ctx context.Context) context.Context {
	provider.SetPersonAtTheDoor(true)
	return provider.WithRole(ctx, lanes.RoleLeafAttached)
}

func execTask(prompt, system, root string) exec.Task {
	title, _, _ := strings.Cut(prompt, "\n")
	return exec.Task{
		NodeID: 1,
		// The one node this command runs, named rather than left to the
		// number. `codeaf exec` has a single leaf, and every model call it
		// makes is that leaf's work; without a key the call log's node column
		// was blank for the whole run, so a person reading the record after it
		// could not tell an exec row from a row with no work behind it at all.
		NodeKey:  execNodeKey,
		Title:    strings.TrimSpace(title),
		Brief:    prompt + "\n\nWorkspace root (your working directory): " + root,
		Contract: system,
	}
}
