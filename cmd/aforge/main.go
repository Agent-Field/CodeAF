// Command aforge is an agent you talk to, and hand work to when you walk away.
//
//	aforge                       open the conversation this directory was having
//	aforge do "<task>"           hand it one job and read the answer on stdout
//	aforge plan new "<goal>"     write a plan to a file without running it
//
// The static plan pipeline it opened life as is four subcommands of `plan` now,
// and it is one feature of many rather than the product.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

func main() {
	os.Exit(execute())
}

// tuneForTheSurface raises the heap target for a command that is about to draw
// one, and IT IS CALLED FROM THE DISPATCH BELOW rather than from main.
//
// The default heap target collects several times before the surface is even
// drawn, and none of those collections free anything worth the pause: the launch
// path allocates a graph snapshot, a catalog, and a thread, and then keeps them.
// Trading a few megabytes of resident memory for those cycles is the right side
// of that bargain for a tool somebody is sitting in front of.
//
// IT IS THE WRONG SIDE FOR EVERY OTHER COMMAND, which is why this is not in
// main. `do`, `run`, `exec`, `engine` and a subharness run headless, often many
// at once on one machine and often for a long time, and nobody is waiting on a
// pause there — a resident set four times larger, multiplied by a fan-out, is a
// cost paid to shorten a pause no one can see. Those commands keep the Go
// default. An explicit GOGC still decides for both — this is a default, not a
// policy.
func tuneForTheSurface() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(400)
	}
}

// execute is the last line of defense. Everything below it absorbs its own
// faults; if one still reaches here the process must die, and it dies saying
// one calm sentence over a restored terminal instead of spilling a goroutine
// dump across the screen the user was working in.
func execute() (code int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			code = reportFault(os.Stderr, fmt.Sprint(recovered), debug.Stack())
		}
	}()
	// The model-call log's file descriptor goes back at the one exit every
	// command shares (internal/calllog). Nothing depends on it — every record is
	// written and flushed as it happens — but a process that closes what it
	// opened is a process whose logs directory can be removed on Windows and in
	// a test's temporary home.
	defer calllog.Close()
	// AND THE BELIEF WRITER RUNS FOR THE WHOLE PROCESS, beside the log above and
	// stopped at the same one exit.
	//
	// It is here rather than in a session because every command in this binary
	// measures lanes — `do` and a subharness never build a session at all — and
	// because it is the goroutine that owns the belief file's EXCLUSIVE LOCK.
	// Taking that lock anywhere a request can be waiting behind it is issue
	// #264, which cost a person twenty-nine silent minutes; `internal/lane`
	// runs no goroutine of its own by design, so somebody has to run this one
	// and this is the process's own line. The flush is registered after the
	// log's close and therefore runs before it, so a compaction that had to be
	// deferred still has somewhere to say so.
	beliefs, stopBeliefs := context.WithCancel(context.Background())
	defer func() {
		stopBeliefs()
		lanes.Flush()
	}()
	// AND THE LANE-SHEET BEAT RIDES THE SAME LIFETIME. It is started later, at
	// the one seam every surface measures through (lanebeat.go), and it is a
	// goroutine of exactly this shape: process-wide, nobody's request, stopped
	// at the same exit.
	laneBeatCtx = beliefs
	guard.Go("lanes/persist", func() { lanes.Persist(beliefs) })
	err := run()
	var status exitStatus
	switch {
	case err == nil:
		return 0
	case errors.As(err, &status):
		// A command that names its own exit code has already written everything
		// it has to say to the right stream. Printing "error:" after an honest
		// partial answer would only make it look like the answer was noise.
		return int(status)
	case errors.Is(err, tea.ErrProgramPanic):
		// bubbletea catches panics in its own loop and restores the terminal
		// before handing this back — so the screen is already the user's again
		// and the only thing missing is the sentence.
		return reportFault(os.Stderr, err.Error(), nil)
	case errors.Is(err, config.ErrNoAPIKey):
		// THE MOST COMMON FIRST-RUN FAILURE, said once and with the remedy, at
		// the ONE exit every command leaves through. `do` used to say this and
		// then repeat itself in machine form on the next line, while `exec`,
		// `plan`, `models` and `run` said only the machine half — so four
		// callers out of five were told the cause and not what to do about it.
		fmt.Fprintln(os.Stderr, "aforge needs a model to work with.")
		fmt.Fprintln(os.Stderr, "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.")
		return 1
	default:
		// And every other failure passes the one rule about what a person may
		// be shown: the cause, what to do about it, and no wrapped Go chain
		// (plainwords.go).
		fmt.Fprintln(os.Stderr, "error:", plainWords(err.Error()))
		return 1
	}
}

func run() error {
	if len(os.Args) < 2 {
		// No arguments opens the chat surface, and that surface is v3. The v2
		// surface and its --v2 door (flag and environment pin both) were removed
		// after v3 had been the default long enough that nothing opened them.
		tuneForTheSurface()
		return runChatV3(nil)
	}
	switch os.Args[1] {
	case "chat":
		tuneForTheSurface()
		return runChatV3(os.Args[2:])
	case "resume":
		// The chat surface, opened on the list of conversations this directory
		// has already had (internal/tui3's resume.go). It is a v3 door only:
		// the older surfaces have no session files to pick from.
		tuneForTheSurface()
		return runResumeV3(os.Args[2:])
	case "engine":
		// The far half of `aforge chat --host <host>`: the process ssh starts
		// on the other machine, speaking the wire protocol on its own pipes
		// (engine.go). It is DELIBERATELY ABSENT from the usage text below —
		// it is machinery a surface dials, not a thing a person runs, and a
		// command that draws nothing and reads no keys would only be a puzzle
		// in a list of commands that do.
		return runRemoteEngine(os.Args[2:])
	case "serve":
		// The other half of reaching this machine, for the machines ssh cannot
		// reach: it dials OUT to a relay and holds the connection open, so a
		// router or a firewall in front of this machine stops mattering. It
		// prints the name this machine answers to and a pairing code, and it
		// is a command a person runs and watches — which is why it is in the
		// usage text and `engine` is not (chatv3_at.go).
		return runServe(os.Args[2:])
	case "devices":
		// Who is allowed to open a conversation here, and the door for taking
		// that back. REVOKING IS THIS MACHINE'S DECISION AND ONLY THIS
		// MACHINE'S, which is why it is a command here rather than something a
		// surface can do down the wire (chatv3_at.go).
		return runDevices(os.Args[2:])
	case "do":
		return runDo(os.Args[2:])
	case "plan":
		// THE STATIC PIPELINE IS ONE NOUN WITH FOUR VERBS ON IT. A developer
		// plans work, shows the plan, revises the plan and runs it, and every
		// one of those reads as English with `plan` as its object — which is
		// what `graph` never did. `graph` is how the ENGINE thinks (nodes,
		// edges, a frontier) and stays inside the engine, where it is the right
		// word and where nobody reads it.
		return runPlanCommand(os.Args[2:])
	case "revise":
		// The old top-level spelling of `aforge plan revise`, kept working for
		// one release (rename.go).
		return renamedTo("revise <plan.json>", "plan revise <plan.json>",
			os.Args[2:], func(args []string) error { return runRevise("plan revise", args) })
	case "run":
		return runExecute(os.Args[2:])
	case "exec":
		return runExec(os.Args[2:])
	case "show":
		// The old top-level spelling of `aforge plan show`.
		return renamedTo("show <plan.json>", "plan show <plan.json>",
			os.Args[2:], func(args []string) error { return runShow("plan show", args) })
	case "models":
		return runModels(os.Args[2:])
	case "notebook":
		return runNotebook(os.Args[2:])
	case "competence":
		return runCompetence(os.Args[2:])
	case "services":
		return runServices(os.Args[2:])
	case "wake":
		return runWake(os.Args[2:])
	case "tick":
		// One bounded pass over the standing items — the reminders, watches and
		// routines a conversation left behind (tick.go). It is what the OS
		// timer runs, and it is DELIBERATELY ABSENT from the usage text below
		// for the same reason `engine` is: it draws nothing, asks nothing, and
		// on an ordinary machine prints nothing at all.
		return runTick(os.Args[2:])
	case "doctor":
		return runDoctor(os.Args[2:])
	case "logs":
		return runLogs(os.Args[2:])
	case "cache":
		return runCache(os.Args[2:])
	case "rebuild":
		return runRebuild(os.Args[2:])
	case "why":
		return runWhy(os.Args[2:])
	case "manual":
		// Everything aforge knows about itself, read straight (manual.go). It
		// is the same corpus the chat's manual tool reads, printed as it is
		// written rather than retold — and it is here rather than only there
		// because the questions people ask most are the ones they ask before
		// there is a key to make a model call with.
		return runManual(os.Args[2:])
	// Three spellings for one question, because three different callers ask it
	// and none of them should have to know which one this build prefers: the
	// agentfield Python doctor runs `aforge version`, the Go doctor runs
	// `aforge --version`, and a person types `-v`.
	case "version", "--version", "-v":
		return runVersion()
	case "-h", "--help", "help":
		return usage(os.Args[2:])
	default:
		return unknownCommand(os.Args[1])
	}
}

// The layout law, because a page nobody can read is a page nobody reads:
//
//   - NOTHING DRAWS WIDER THAN [helpWidth] CELLS. Eighty is the width a
//     terminal opens at, and this page used to run to a hundred and sixty-four
//     — so every second line was soft-wrapped mid-word by the terminal, at a
//     break the writer never chose, and the hanging indent stopped aligning
//     the moment it happened. A hundred and eight lines drew a hundred and
//     sixty-seven rows.
//   - A COMMAND'S SYNOPSIS BEGINS AT COLUMN 2 and folds, when it must, to
//     column 14 — under the verb, so the flags stay one column.
//   - ITS DESCRIPTION SITS UNDER IT AT [helpTextColumn], never beside it. A
//     right-hand column was tried and cannot survive eighty cells: the
//     synopses here carry whole flag lists, so the text column would start at
//     thirty on the short verbs and at zero on the long ones, which is the two
//     conventions this page already had.
//
// TestEveryHelpPageFitsAnEightyColumnTerminal holds the first of those.
const (
	helpWidth      = 80
	helpTextColumn = 6
)

// usageText is what `aforge --help` prints, and it is FIVE HEADED GROUPS AND
// FIVE EXAMPLES AND NOTHING ELSE.
//
// It used to be one flat list of twenty-three commands followed by a sixty-line
// environment table, so the last thing on a person's screen after asking what
// the commands are was AFORGE_CALL_LOG_BODIES, and the commands themselves had
// scrolled off the top. The table is a REFERENCE — it is consulted, never read
// — so it lives at `aforge help env` ([environmentText]) and the one line at
// the bottom here says so.
//
// The groups are ordered most-reached-for first rather than alphabetically,
// because a list nobody reads to the end is a list whose ordering is the whole
// design. Adjacent forms of one verb stay together.
//
// THE TABLE IS ALSO THE ONE SOURCE OF EVERY PER-COMMAND SYNOPSIS. `aforge do
// --help` lifts `do`'s lines straight out of it ([usageForCommand]), so a
// synopsis cannot go stale, and a group heading is written at column zero
// precisely so it ends a command's block rather than joining it.
//
// AND THE EXAMPLES ARE INDENTED FOUR, NOT TWO, FOR THE SAME READER. Two spaces
// is what a command row is written with, so an example beginning `  aforge do`
// was lifted into `aforge do --help` as though it were part of that command's
// synopsis — which is what happened the first time they were added.
var usageText = `aforge — an agent you talk to, and hand work to when you walk away

Talk to it — a surface you sit in front of
  aforge
      open the conversation this directory was last having
  aforge chat [--model slug] [--reasoning level] [--session path] [--yolo]
              [--host host[:path]] [--at name[:path]] [--once "text"]
              [--no-compact] [--one-model] [--no-host] [--debug]
      --no-host runs the conversation in this process rather than on this
      workspace's session host; --debug keeps the whole record of the run
  aforge resume
      pick an earlier conversation by name and open it — the same list is
      /resume inside the chat

Hand it work — nobody is watching, the answer is on stdout
  aforge do   "<task>" [--db path] [--keep] [--dir dir] [--timeout 15m]
              [--json] [--yes-spend] [--model slug] [--plan-model slug]
              [--context-fill 60] [--completion-reserve 65536] [--debug]
      do one task and exit — the same living agent the chat runs, with nobody
      watching. What you type is the goal, and it is run verbatim
  aforge exec ["<prompt>"] [--dir dir] [--system text] [--max-turns N]
              [--token-budget N] [--timeout 15m] [--model slug]
              [--context-fill N] [--completion-reserve N] [--json]
              [--out file] [--debug]
      run one worker for one pass, with no planning at all
  aforge run  <program> --input <file.json|-> [--dir dir] [--model slug]
              [--journal path] [--json]
      run one saved program: typed input in, its typed output on stdout. A
      question it was not told how to answer stops it rather than being guessed
  the three differ by how much thinking happens first: do plans and may split
  the job, exec does not plan, run follows a plan somebody saved. All three
  end the same way, and why is in --json's stop field:
  ` + foldedExitLadder(2, helpWidth) + `
  AFORGE_EXIT_CODES=legacy restores exec's old 2/3/4/5/6 for one release

Look at what happened — read-only, no key, nothing spent
  aforge why self [--db path]
      show today's self-spend receipts
  aforge why <task-id> [--db path]
      what one piece of work did — its turns, tools, arguments, how it ended
  aforge logs [--tail 40] [--follow] [--path] [--json] [--run id]
              [--call id] [--tag t] [--model m] [--node n] [--body id]
      every model call aforge made — what was asked, which lane answered, what
      came back. The filters are exact and combine; --json prints the rows as
      they are on disk, --body one call's bodies. AFORGE_CALL_LOG=off is off
  aforge models
      the models this machine will use, and what each has been measured at
  aforge doctor [--db path]
      is this install healthy, and where does it keep things
  aforge manual
      every page of aforge's own manual, one per line
  aforge manual <page> | "<question>"
      that page printed whole, or the sections that answer a question
  aforge version
      print the build this binary was cut from (--version and -v say the same)

Housekeeping — changes state on disk or on the network
  aforge cache
      what the shared build cache holds, and how big it is
  aforge cache clean [--yes]
      delete ~/.aforge/cache to free disk. It prints the size and path, then
      asks you to type "` + cacheCleanWord + `" — --yes skips that. Conversations are untouched
  aforge rebuild [--db path] [--yes]
      discard every derived table and replay the journal
  aforge serve [--workspace path] [--relay url]
      be reachable from your other devices without ssh, with a pairing code
  aforge devices
      list the devices paired with this machine
  aforge devices revoke <name> [--all]
      stop one device opening a conversation here; --all, every device of that
      name
  aforge notebook [--db path]
      what it has learned, and what it has been corrected on
  aforge notebook retract|restore <seq> [--db path]
  aforge competence [--db path] [--model slug]
      what it has been measured as good at
  aforge services [--db path]
      long-running processes it was asked to keep
  aforge services stop <name> [--db path]
  aforge wake [--db path] [--timeout 2m]
      run one full background pass by hand and exit
  aforge help env
      the environment table: every variable and its default

Plan work by hand — a plan you can read, edit and diff
  aforge plan new "<goal>" [--out plan.json] [--dir dir] [--json]
              [--instructions] [--passes auto|off|N] [--model slug]
              [--plan-model slug]
  aforge plan show <plan.json>
  aforge plan revise <plan.json> "<what happened>" [--done 1,2,3]
              [--out plan.json] [--model slug] [--plan-model slug]
  aforge plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json]
              [--yes-spend] [--model slug] [--plan-model slug]
      a plan written to a file, then executed exactly as written. It is not
      what most people want: nothing learnt mid-flight moves a frozen plan

Examples:
    aforge                                open the conversation you were having
    aforge do "add a health endpoint and a test for it"
    aforge do "summarise CHANGELOG.md" --json | jq -r .answer
    aforge logs --tail 20 --model anthropic/claude-opus-4
    aforge chat --host devbox:~/src/api   the chat here, the work over there

Every command answers ` + "`aforge <command> --help`" + ` with its own line and its flags.
The environment table is ` + "`aforge help env`" + ` — every variable and its default.`

// environmentText is the reference half of the old `--help`: every variable a
// person can set, and what it defaults to.
//
// IT IS A VAR AND NOT A CONST FOR ONE REASON: the dollar figures are the real
// defaults, interpolated from the constants that own them
// ([config.DefaultDailyBudgetUSD] and the rest). They were typed out by hand
// once, and every one of them was stale by the time somebody read it — which is
// the one-source-of-truth law's own worked example.
//
// IT KEEPS THE SAME EIGHTY-CELL LAW AS [usageText]. A reference table is the
// one page a person reads with their eyes rather than their memory, and this
// one ran to a hundred and sixteen cells: a variable's name in one column and
// its sentence soft-wrapped back under the name, which is the shape of a table
// that has stopped being one. The name is at column 2 and the sentence at
// column 23, or on the next line at column 23 when the name reaches past it.
var environmentText = `aforge — the environment

Every variable below is read at launch. A variable set here always wins over the
` + "`/settings`" + ` sheet in the chat, and that row reads read-only in the sheet rather
than fighting your shell.

  OPENROUTER_API_KEY   required
  AFORGE_MODEL         default ` + config.DefaultModel + `
  AFORGE_PLAN_MODEL    unset: the work model plans too. Set it to run planning,
                       replans, working methods and the delivery gate on a
                       stronger model while a smaller one does the steps;
                       --model and --plan-model do the same per run.
  AFORGE_MODELS        unset: one model, exactly as above. Set it to a panel
                       and calls cascade — cheapest model first, escalating
                       when a verifier catches a failure. Either a
                       comma-separated list of slugs, or a path to a JSON file:
                       AFORGE_MODELS=google/gemma-3-12b-it,~moonshotai/kimi-k2.6
                       AFORGE_MODELS=~/.aforge/models.json
                       Ratings accumulate in ~/.aforge/router-ledger.json
                       across runs; see them with ` + "`aforge models`" + `.
  AFORGE_REASONING     planning calls: off (default), low, medium, high
  AFORGE_EXEC_REASONING
                       executor calls: model default (unset), off, low, medium,
                       high
  AFORGE_EXEC_TIMEOUT  ` + "`aforge exec`" + ` only: hard wall when --timeout is not
                       passed, as a duration or a bare number of seconds.
                       AFORGE_EXEC_BUDGET and AFORGE_EXEC_TURNS do the same for
                       --token-budget and --max-turns. A flag that was typed
                       always wins; these exist so a harness can set the walls
                       once for a campaign instead of on every call.
  AFORGE_EXIT_CODES    ` + legacyExitCodesHelp + `
  AFORGE_MAX_DEPTH     2   how many levels of decomposition
  AFORGE_NODE_BUDGET   ` + strconv.Itoa(config.DefaultNodeBudget) + `  hard ceiling on total steps
  AFORGE_DAILY_BUDGET  ` + usageDollars(config.DefaultDailyBudgetUSD) + `  the day's spending limit in dollars (0 = unlimited)
  AFORGE_PLAN_CONSENT  ` + usageDollars(config.DefaultPlanConsentUSD) + `  a plan estimated above this quotes its price
                       and waits for your word (0 = never asks)
  AFORGE_IMAGE_MODEL   image-generation model (catalog-resolved by default)
  AFORGE_SPEECH_MODEL  speech-synthesis model (catalog-resolved by default)
  AFORGE_MUSIC_MODEL   music-generation model (catalog-resolved by default)
  AFORGE_VIDEO_MODEL   video-generation model (catalog-resolved by default)
  AFORGE_VISION_MODEL  image-inspection proxy (talk, work, catalog-resolved)
  AFORGE_DOC_ENGINE    auto (default), local, free or ocr document reading
  AFORGE_PRACTICE_BUDGET
                       ` + usageDollars(config.DefaultPracticeBudgetUSD) + `  daily self-practice carve-out (0 = disabled)
  AFORGE_PRACTICE_IDLE 20m  quiet period before self-practice
  AFORGE_BRIEF_AFTER   4h  minimum absence before an arrival brief (0 = always)
  AFORGE_MAX_HOURS     how many hours an unattended chat --yolo session may
                       carry its own work on (default none: it stops when the
                       model stops). --max-hours wins.
  AFORGE_MAX_COST      the same ceiling in dollars. --max-cost wins. Either one
                       alone is a budget; without one, --yolo is only the
                       approval posture it has always been.
  AFORGE_PREAUTHORIZE_SPEND
                       1 spends past the day's limit without stopping a
                       headless run to ask
  AFORGE_HOME          the whole state root — journal, workspace, CAS, craft,
                       profiles, catalog, skills (default ~/.aforge). Move it
                       to run a disposable store that touches nothing of yours.
  AFORGE_PROFILE_DIR   where measured behaviour is kept (default AFORGE_HOME)
  AFORGE_CALL_LOG      the model-call log (default <profile>/logs/calls.jsonl).
                       "off" writes nothing; any other value is the file to
                       write.
  AFORGE_CALL_LOG_BODIES=1
                       also record each call's whole request and response —
                       your prompts included. Off by default, and for one run
                       at a time.

The user-facing knobs above — budgets, rhythm, the document reader, the vision
and media slots — are also the ` + "`/settings`" + ` sheet in the chat, which persists
them to the profile's config.json.

Run ` + "`aforge --help`" + ` for every command.`

// usageDollars writes a default the way the table has always written it: the
// shortest form that is still the same number, so 500 stays 500 and 2.5 stays
// 2.5 rather than growing a trailing zero nobody typed.
func usageDollars(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// usage answers `aforge --help`, `-h` and `aforge help`. With `env` after it,
// it prints the environment table instead — which is where the table went when
// it stopped being two thirds of the front page.
func usage(args []string) error {
	// Through the same seam every per-command usage goes through (usage.go), so
	// help is one stream and one thing a test can read back.
	if len(args) > 0 && strings.TrimSpace(args[0]) == "env" {
		fmt.Fprintln(usageOut, environmentText)
		return nil
	}
	fmt.Fprintln(usageOut, usageText)
	return nil
}

// renamedTo runs an old spelling of a command and says, once and on stderr,
// what it is called now.
//
// ASKING AN OLD SPELLING FOR HELP SAYS NOTHING. `--help` runs nothing, prints
// the NEW spelling's own line out of the one table, and leaves with 0 — so a
// developer probing `aforge show --help` is shown `aforge plan show` and a
// Makefile that checks the binary is healthy still reads a clean stderr. The
// notice is about a run; there is no run.
func renamedTo(old, now string, args []string, door func([]string) error) error {
	if !askedForHelp(args) {
		sayRenamed(old, now)
	}
	return door(args)
}

// runPlanCommand is the four verbs of the static pipeline under the one noun
// they all act on, and the old top-level spelling of the first of them.
//
// `aforge plan "<goal>"` was the whole command; it is `aforge plan new
// "<goal>"` now, and the bare form still works for one release. The two are
// told apart by the word itself: a lone `new`, `show`, `revise` or `run` in the
// first position is a subcommand and anything else is the goal, which is the
// same reading `aforge cache clean` already has.
func runPlanCommand(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "new":
			return runPlanNew("plan new", args[1:])
		case "show":
			return runShow("plan show", args[1:])
		case "revise":
			return runRevise("plan revise", args[1:])
		case "run":
			return runGraph("plan run", args[1:])
		}
	}
	// `aforge plan --help` is a question about the group, so it answers with all
	// four lines rather than with `plan new`'s alone.
	if askedForHelp(args) {
		return commandHelp("plan")
	}
	if len(args) == 0 {
		// Nothing was spelled the old way, so there is nothing to say about a
		// spelling. `aforge plan` alone answers the way it always did — the
		// goal is missing, and here is the shape it wanted — reading a piped
		// goal first if one is there.
		return runPlanNew("plan new", args)
	}
	return renamedTo(`plan "<goal>"`, `plan new "<goal>"`, args,
		func(args []string) error { return runPlanNew("plan new", args) })
}

func runPlanNew(name string, args []string) error {
	flags := commandFlags(name)
	output := flags.String("out", "", "write the plan as JSON to this file")
	shorthandFlag(flags, "o", "out")
	asJSON := flags.Bool("json", false, "print the plan as JSON instead of a table")
	// `--brief` was the name of the thing this writes and not of what it does.
	// What it writes is a self-contained instruction for every step, which is
	// what the flag is called now.
	briefs := flags.Bool("instructions", false, "write a self-contained instruction for every step")
	renamedFlag(flags, "brief", "instructions")
	// A TRI-STATE IS WORDS, NEVER MAGIC INTEGERS. This was `--ensemble 0|-1|N`,
	// where 0 meant "decide for me" and -1 meant "never" — a code-shaped API in
	// which `--ensemble 1` had no meaning at all.
	passes := passesFlag{count: plan.EnsembleAuto}
	flags.Var(&passes, "passes", "how many independent passes to plan with and merge: auto, off, or a number from 2")
	renamedFlag(flags, "ensemble", "passes")
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", planModelFlagHelp)
	// The same --dir that `plan run` takes, and it means the same directory.
	// Planning happens before running, so there is no workspace yet unless the
	// person naming the goal also names the material it is about — which is
	// exactly when the material is worth looking at.
	workspace := flags.String("dir", "", "directory holding the material this goal is about, read once to ground the plan")
	shorthandFlag(flags, "w", "dir")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	ensemble := &passes.count
	goal, err := readText(flags.Name(), flags.Args())
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	seats := config.ResolveSeats(settings.ProfileDir, *model, *planModel)
	applySeats(&settings, seats)
	workClient, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(workClient)
	client, closePlanner, err := planningClient(settings, workClient)
	if err != nil {
		return err
	}
	defer closePlanner()
	ctx := settings.Context(context.Background(), goal)

	// The ruler in force comes from measured work when there is any; the
	// built-in prior is only the starting point.
	store := installMeasuredRulers(settings, settings.Model)

	if !*asJSON {
		// Every line of it is an aside: the answer this door gives is the PLAN,
		// and a preamble in front of it is what broke `aforge plan new "x"
		// --json | jq` (streams.go).
		fmt.Fprintf(aside, "goal:   %s\nmodel:  %s (reasoning: %s)\n", goal, settings.PlanModelResolved(), settings.Reasoning)
		fmt.Fprintln(aside, seats.Report())
		if settings.PlanSplit() {
			fmt.Fprintf(aside, "sized for: %s (the work model this ruler measures)\n", settings.Model)
		}
		if spread := store.Measure(); spread.Samples > 0 {
			calibrated := "built-in"
			if strings.TrimSpace(store.Anchors) != "" {
				calibrated = "calibrated"
			}
			fmt.Fprintf(aside, "ruler:  %s, from %d measured tasks (%d-%d turns, median %d)\n",
				calibrated, spread.Samples, spread.MinTurns, spread.MaxTurns, spread.Median)
		}
		fmt.Fprintln(aside)
	}
	report := func(pass string, elapsed time.Duration, detail string) {
		if !*asJSON {
			fmt.Fprintf(aside, "  %-8s %-22s %s\n", pass, detail, elapsed.Round(10*time.Millisecond))
		}
	}
	history := openDefaultHistory()
	if history != nil {
		defer history.Close()
	}
	graph, err := plan.Build(ctx, client, goal, plan.Options{
		Recall: recallHits(history, goal, groundRecallLimit),
		// Rendered here rather than inside the build, and rendered once: the
		// snapshot is frozen for the whole build, and an unset -w renders the
		// empty string, which leaves every prompt exactly as it was.
		Terrain:      plan.RenderTerrain(*workspace, goal),
		SpineSamples: settings.SpineSamples,
		// The window this document is planned through, so a later revision of
		// it is sized from the same fact.
		ContextTokens: settings.Models.ContextLength(settings.PlanModelResolved()),
		// What work of this kind has really cost here, for the passes that
		// decide whether to divide it. Empty on a machine with nothing measured,
		// which is what every prompt below has always been sent. See invoice.go.
		Invoice:    measuredInvoice(settings, settings.Model),
		MaxDepth:   settings.MaxDepth,
		NodeBudget: settings.NodeBudget,
		Briefs:     *briefs,
		Ensemble:   *ensemble,
		Report:     report,
		Progress:   headlessPlanProgress(os.Stderr),
		OnReady: func(node plan.Node, elapsed time.Duration) {
			if !*asJSON {
				fmt.Fprintf(aside, "    ready   %-22s %s\n", clip(node.Title, 22), elapsed.Round(10*time.Millisecond))
			}
		},
	})
	if graph == nil {
		return err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nwarning: %v\n", err)
	}
	gatePlanDivision(graph, goal)
	return emit(graph, *output, *asJSON)
}

// runRevise takes no -w and renders no terrain of its own, which is deliberate
// twice over. It has no workspace to name — it is handed a graph file and a
// sentence about what happened — and it does not need one: the graph it loads
// carries the terrain that was rendered when it was planned, and the reviser
// reads the same shared preamble every other pass does. What the reviser is
// actually missing is not the picture but the difference between that picture
// and the workspace now, and a delta is a different thing from a snapshot.
func runRevise(name string, args []string) error {
	flags := commandFlags(name)
	output := flags.String("out", "", "write the revised plan as JSON to this file")
	shorthandFlag(flags, "o", "out")
	asJSON := flags.Bool("json", false, "print the plan as JSON instead of a table")
	done := flags.String("done", "", "mark these step ids finished before revising")
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", "model that revises the plan, when different from the work model ("+planLadderHelp+")")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	rest := flags.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: aforge plan revise <plan.json> \"<what happened>\"")
	}
	data, err := os.ReadFile(rest[0])
	if err != nil {
		return err
	}
	graph, err := plan.Load(data)
	if err != nil {
		return err
	}
	event := strings.TrimSpace(strings.Join(rest[1:], " "))

	// Marking nodes finished by hand is how the frozen rule gets exercised
	// before an executor exists to set the state for real.
	for _, id := range parseIDs(*done) {
		if node := graph.Node(id); node != nil {
			node.State = plan.StateDone
		}
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	seats := config.ResolveSeats(settings.ProfileDir, *model, *planModel)
	applySeats(&settings, seats)
	workClient, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(workClient)
	client, closePlanner, err := planningClient(settings, workClient)
	if err != nil {
		return err
	}
	defer closePlanner()
	ctx := settings.Context(context.Background(), graph.Goal)

	if !*asJSON {
		fmt.Fprintf(aside, "goal:   %s\nevent:  %s\n", graph.Goal, event)
		fmt.Fprintf(aside, "%s\n\n", seats.Report())
	}
	start := time.Now()
	operations, usage, err := plan.Revise(ctx, client, graph, event)
	if err != nil {
		return err
	}
	graph.Usage.Calls += usage.Calls
	graph.Usage.Cost += usage.Cost

	if !*asJSON {
		fmt.Fprintf(aside, "  revise   %-22s %s\n\n", plural(len(operations), "operation"), time.Since(start).Round(10*time.Millisecond))
		renderOperations(operations)
	}
	return emit(graph, *output, *asJSON)
}

func runShow(name string, args []string) error {
	if askedForHelp(args) {
		return commandHelp(name)
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: aforge plan show <plan.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	graph, err := plan.Load(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(aside, "goal:   %s\n", graph.Goal)
	return emit(graph, "", false)
}

func emit(graph *plan.Graph, output string, asJSON bool) error {
	encoded, err := graph.JSON()
	if err != nil {
		return err
	}
	if output != "" {
		if err := os.WriteFile(output, encoded, 0o644); err != nil {
			return err
		}
	}
	if asJSON {
		fmt.Println(string(encoded))
		return nil
	}
	render(graph)
	if output != "" {
		// The receipt for a file is not the plan, so it is an aside: a person
		// who redirected the table wants the table in the file and the sentence
		// about it on their terminal.
		fmt.Fprintf(aside, "\nwritten to %s\n", output)
	}
	return nil
}

// readText is the prose a command was given: its positional arguments, or what
// was piped to it.
//
// TWO WAYS IN, AND THE SECOND ONE IS EXPLICIT. A lone `-` positional means "the
// text is on stdin", which is the convention every unix filter keeps, and no
// positional at all means the same thing WHEN NOTHING IS ATTACHED TO THE
// TERMINAL. The terminal check is what stops the third case being a hang: a
// person who typed `aforge do` with nothing after it used to get a process
// silently reading their keyboard forever, which reads exactly like a program
// that has crashed. They get the usage instead.
// The command's name is carried in so the miss can answer with THAT command's
// one line. It used to answer with the whole table — a hundred and twenty-seven
// lines, the environment included — for the sake of one missing quoted string,
// and the one line that mattered scrolled off the top of the terminal.
func readText(name string, args []string) (string, error) {
	if len(args) == 1 && args[0] == "-" {
		return readPipedText(name)
	}
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " ")), nil
	}
	if stdinIsTerminal(os.Stdin) {
		return "", noGoalGiven(name)
	}
	return readPipedText(name)
}

func readPipedText(name string) (string, error) {
	piped, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(piped))
	if text == "" {
		return "", noGoalGiven(name)
	}
	return text, nil
}

func noGoalGiven(name string) error {
	shape := usageForCommand(name)
	if shape == "" {
		return fmt.Errorf("no goal given\n\nrun `aforge --help` for every command.")
	}
	return fmt.Errorf("no goal given\n\n%s\n\nrun `aforge --help` for every command.", shape)
}

// reorder moves flags ahead of positional arguments. Go's flag package stops
// parsing at the first non-flag token, so `aforge plan "goal" -o out.json`
// would otherwise fold the flag into the goal text — silently, which is the
// worst way for it to fail.
//
// ── A BRIEF THAT BEGINS WITH "-" IS TEXT, NOT A FLAG ────────────────────────
//
// This used to decide by shape alone: a leading dash meant a flag. So
// `aforge do "- Update the display style property…"` — a brief written as a
// bullet list, which is how people write briefs — was moved into the flag
// section and the run died in one second with `flag provided but not defined:
// - Update the display style property…` and a usage dump. It happened to a real
// benchmark cell and cost the whole run.
//
// A shape cannot answer the question because two different things wear it. What
// answers it is the FLAG SET ITSELF, which is the one authority on which flags
// this command has, and which of them take a value:
//
//   - A token whose name this command declares is a flag, and it consumes the
//     token after it when the flag set says it is not a boolean. That fact used
//     to be a hand-written map at each of the seventeen call sites, which is one
//     source of truth per caller and therefore none.
//   - A token that cannot be a flag NAME is text. Flag names hold no whitespace,
//     so a bullet, a sentence and a multi-line brief are all text no matter what
//     they begin with — decided by structure, never by a list of shapes we have
//     seen briefs take.
//   - Anything else that looks like a flag and is not declared stays in the flag
//     section, so a typo (`-dbb`) is still refused by name rather than being
//     folded silently into the brief.
//
// `--` ends the flags, as it does everywhere, and one is emitted between the two
// sections so a positional that begins with a dash reaches Args() intact.
func reorder(flags *flag.FlagSet, args []string) []string {
	var named, positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			// Everything after the terminator is text by the caller's own
			// instruction, which outranks every reading below.
			positional = append(positional, args[index+1:]...)
			break
		}
		if !looksLikeFlag(argument) {
			positional = append(positional, argument)
			continue
		}
		named = append(named, argument)
		name := strings.TrimLeft(argument, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if takesAValue(flags, name) && index+1 < len(args) {
			index++
			named = append(named, args[index])
		}
	}
	// The terminator goes in unconditionally: a positional beginning with a dash
	// is exactly the case this whole function exists for, and it must not be
	// re-read as a flag by the parser downstream.
	return append(append(named, "--"), positional...)
}

// looksLikeFlag reports whether a token could be a flag at all — which is a
// question about its SHAPE as a name, and the only part of the decision the flag
// set cannot answer.
func looksLikeFlag(argument string) bool {
	if !strings.HasPrefix(argument, "-") || argument == "-" || argument == "--" {
		return false
	}
	name := strings.TrimLeft(argument, "-")
	if name == "" {
		return false
	}
	// A flag name is one word. Anything with a space, a tab or a newline in it is
	// prose that happens to open with a dash — a bullet, a diff hunk, a brief.
	return !strings.ContainsAny(name, " \t\r\n")
}

// takesAValue asks the flag set whether this flag consumes the token after it.
// An undeclared name answers false and is left for the parser to refuse by name;
// a boolean answers false because `-keep true` is not how a boolean flag is
// written and swallowing the next token would eat a positional.
func takesAValue(flags *flag.FlagSet, name string) bool {
	if flags == nil {
		return false
	}
	found := flags.Lookup(name)
	if found == nil {
		return false
	}
	boolean, ok := found.Value.(interface{ IsBoolFlag() bool })
	return !ok || !boolean.IsBoolFlag()
}

// applyModelFlags lets a headless invocation split the two roles per run:
// --model moves the work (and, unsplit, everything), --plan-model moves only
// the model that structures. Flags outrank the environment for this run.
func applyModelFlags(settings *config.Config, model, planModel string) {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		settings.Model = trimmed
	}
	if trimmed := strings.TrimSpace(planModel); trimmed != "" {
		settings.PlanModel = trimmed
	}
}

// THE TWO MODEL FLAGS SAY THE SAME THING AT EVERY DOOR, so they say it once.
//
// The wording they replaced was `(default AFORGE_MODEL)`, which named one rung
// of four and hid the two that decide most runs: a profile's crew, and this
// build's own default when nobody has said anything at all. A help string that
// names the whole ladder is the shortest place a person can learn that their
// crew reaches this command (config.ResolveSeats).
const (
	workLadderHelp    = "flag › AFORGE_MODEL › crew › default"
	planLadderHelp    = "flag › AFORGE_PLAN_MODEL › crew mastermind › the work model"
	modelFlagHelp     = "work model for this run (" + workLadderHelp + ")"
	planModelFlagHelp = "model that plans, when different from the work model (" + planLadderHelp + ")"
)

// yesSpendFlagHelp is what `--yes-spend` MEANS, said once, on both doors that
// carry it.
//
// `do` used to describe it as "approve a plan whose price crosses the consent
// threshold" and `plan run` as "preauthorize raising today's dollar rail when
// reached". Those read as two different decisions, so a developer who set the
// flag on both could not tell which one they had authorised — and it is one
// flag doing one thing: spending past a limit without stopping to ask. `rail`
// went with the second sentence; it is machinery vocabulary, and the thing it
// names is the day's spending limit.
const yesSpendFlagHelp = "spend past today's limit and past the plan-price question, without stopping to ask"

// storeFlagHelp is what `--db` names, said once on the eight doors that take it.
//
// Six of them said "path to the durable graph database", which is two words for
// one file and one of them — `graph` — is how the ENGINE thinks. A developer
// looking for where their data lives searches for a store, and `aforge doctor`
// now labels the same file that way.
const storeFlagHelp = "the store to work in"

// applySeats puts the ladder's answer where the rest of the process reads its
// two models.
//
// It is applyModelFlags' successor for the headless doors: the same two fields,
// filled from the WHOLE ladder — flag, environment, crew, default
// (config.ResolveSeats) — rather than from the flags alone with config.Load's
// environment reading underneath. One assignment per seat, so the models a
// door's receipt names and the clients it then builds cannot be different
// models.
func applySeats(settings *config.Config, seats config.Seats) {
	settings.Model = seats.Work.Model
	settings.PlanModel = seats.Plan.Model
}

// planningClient returns the client planning-class calls run on. With no plan
// split it is exactly the work client — nothing new is built, and the cleanup
// is a no-op — so the single-model path is byte-identical to before the slot
// existed.
func planningClient(settings config.Config, workClient router.Client) (router.Client, func(), error) {
	if !settings.PlanSplit() {
		return workClient, func() {}, nil
	}
	client, err := settings.ClientFor(settings.PlanModelResolved())
	if err != nil {
		return nil, nil, err
	}
	return client, func() { closeRouter(client) }, nil
}

func parseIDs(raw string) []int {
	var ids []int
	for _, field := range strings.Split(raw, ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
