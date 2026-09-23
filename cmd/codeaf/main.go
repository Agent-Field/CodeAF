// Command codeaf is an agent you talk to, and hand work to when you walk away.
//
//	codeaf                       open the conversation this directory was having
//	codeaf do "<task>"           hand it one job and read the answer on stdout
//	codeaf plan new "<goal>"     write a plan to a file without running it
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
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/plan"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/router"
	"github.com/Agent-Field/codeaf/internal/telemetry"
	"github.com/Agent-Field/codeaf/internal/trace"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func main() {
	home.Adopt(log.Printf)
	os.Exit(execute())
}

// surfaceMaxProcs is the GOMAXPROCS a surface runs under on a machine bigger
// than this. It is not the machine's core count, and the number was chosen by
// counting what the runtime does with the ones above it, not by taste — see
// [tuneForTheSurface] for the census.
const surfaceMaxProcs = 8

// tuneForTheSurface caps the scheduler and raises the heap target for a command
// that is about to draw one, and IT IS CALLED FROM THE DISPATCH BELOW rather
// than from main.
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
//
// ── AND THE SCHEDULER IS THE SAME BARGAIN IN A DIFFERENT UNIT ───────────────
//
// GOMAXPROCS is the number of Ps the scheduler runs, and the Go runtime spends
// the machine's cores on that number whether or not a surface uses them: an idle
// session's engine host was measured holding ONE runtime GC-worker goroutine PER
// P. On a 20-core machine that was 20 of the process's 33 goroutines; at
// GOMAXPROCS=8 the same process held 8 workers and 21 goroutines, with its OS
// threads falling 13 to 11 (measured Sep 2026, `engine --daemon`, an empty
// workspace, before and after, two runs each side).
//
// A surface draws one conversation and waits on a network, and the parallelism
// that DOES want the whole machine is subprocesses — the tools a session runs,
// each with its own Ps — so the cores above the cap buy a waiting surface
// nothing and cost it a pool of idle runtime workers. Capping at 8 rather than
// lower is measured too: 20->8 removes 12 of the idle goroutines, and 8->4
// removes 4 more while halving what any in-process work may use.
//
// THE CAP HAS TO CROSS A PROCESS BOUNDARY, because a surface's far half is a
// SEPARATE `engine --daemon` this launch starts (enginehost.Spawn from
// chatv3_local.go) — its own process, so it inherits the environment and not
// this process's scheduler, and reads GOMAXPROCS at its own startup. Setting the
// variable is what carries the cap to it.
//
// AN EXPLICIT GOMAXPROCS STILL DECIDES, exactly as an explicit GOGC does just
// below: this is a default, not a policy. And a machine no bigger than the cap
// is left completely alone — not even the variable is set.
//
// ── AND A SOFT LIMIT IS THE OTHER HALF OF THE HEAP BARGAIN ──────────────────
//
// GOMAXPROCS caps a resource the runtime SPENDS. The raised GOGC below uncaps
// one it KEEPS: a heap target five times the live heap is a bargain with no
// ceiling of its own, and a surface left open for a day is exactly the shape
// that finds the ceiling by exhausting the machine instead. debug.SetMemoryLimit
// is the missing half — a SOFT limit over ALL runtime-managed memory, not the
// heap alone, that the collector works to stay under by running continuously
// rather than crossing it.
//
// THE LIMIT IS DERIVED FROM THE SMALLEST REAL BOUND, NOT FROM TASTE.
// [surfaceMemoryLimit] takes HALF of the tightest bound the machine and the
// process's cgroup give, under an absolute floor, and both halves of that are
// there because the failure this must never cause is a limit BELOW the live
// heap: a limit under the working set makes the collector thrash continuously,
// which is a worse failure than the unbounded growth it was added to prevent.
// Half a bound that can run a surface at all is far clear of the roughly 104 MB
// a surface's resident set was measured at, and the floor refuses the small
// machines where half of the bound would not be.
//
// PHYSICAL MEMORY ALONE IS THE WRONG BOUND INSIDE A CONTAINER, which is the
// usual reason to set GOMEMLIMIT at all. The machine's physical memory there is
// the HOST's, so a limit drawn from it lands far above what the process may
// actually use: it never binds, and the kernel OOM-kills instead of the
// collector working. So the cgroup the process runs in is read as well, and the
// SMALLEST finite bound wins. A fixed generous ceiling still loses — one number
// written by hand is wrong on a small machine and a large one at once. And a
// bound that cannot be read (physical memory unreadable, no cgroup files, every
// cgroup file `max`, or the cgroup v1 "no limit" sentinel) is not counted at
// all; if none is readable the surface sets nothing, exactly as it did before
// the cgroup read existed.
//
// AN EXPLICIT GOMEMLIMIT STILL DECIDES, exactly as an explicit GOGC and
// GOMAXPROCS do above — this is a default, not a policy. A machine whose memory
// cannot be read, or whose half would fall under the floor, has NOTHING set
// rather than a dangerous limit. And like the scheduler cap this crosses to the
// engine host as the variable: the child process reads GOMEMLIMIT at its own
// startup.
func tuneForTheSurface() {
	if os.Getenv("GOMAXPROCS") == "" {
		if n := surfaceProcs(runtime.NumCPU()); n < runtime.NumCPU() {
			runtime.GOMAXPROCS(n)
			os.Setenv("GOMAXPROCS", strconv.Itoa(n))
		}
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(400)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		if limit := surfaceMemoryLimit(hostTotalMemory(), hostCgroupMemoryLimit()); limit > 0 {
			debug.SetMemoryLimit(limit)
			os.Setenv("GOMEMLIMIT", strconv.FormatInt(limit, 10))
		}
	}
}

// surfaceProcs is the GOMAXPROCS a surface should run under on a machine with
// ncpu cores: the cap, or the machine's own count when it is no bigger than the
// cap. The caller reads it as "is this smaller than what we have" — on a machine
// that is already at or under the cap the answer is the machine, so nothing is
// capped and not even the variable is set.
func surfaceProcs(ncpu int) int {
	if ncpu <= surfaceMaxProcs {
		return ncpu
	}
	return surfaceMaxProcs
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
	// The anonymous usage counts get their one configured answer before any
	// command is dispatched, and their one session event at the exit every
	// command shares. Both are here, in execute, because nothing else in this
	// file is reached by all of chat, do, exec, run and `plan run` — and a
	// counter that missed a door would miscount the runs it was built to count.
	//
	// The CONFIG read must never become a failure of its own: a machine with
	// no profile yet, or a broken project file, is a machine `codeaf version`
	// still owes an answer to. Any error reads as "on" — the default — and
	// the run carries on.
	telemetry.Configure(telemetryConfiguredOff())
	telemetrySession := telemetryBegin()
	defer func() {
		telemetryEnd(telemetrySession, code)
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
		fmt.Fprintln(os.Stderr, "codeaf needs a model to work with.")
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
	codeupdate.CleanupOldRunning(os.Executable)
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
		// The far half of `codeaf chat --host <host>`: the process ssh starts
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
	case "plandb":
		// The plan store's CLI, through the same Main cmd/plandb builds into
		// bin/plandb (docs/design/plandb-cli/DESIGN.md). This door is the
		// fallback road when the sibling binary is not where a bash-belt
		// worker's shell can find it; Main answers the exit code directly.
		return exitStatus(plandb.Main(os.Args[2:]))
	case "revise":
		// The old top-level spelling of `codeaf plan revise`, kept working for
		// one release (rename.go).
		return renamedTo("revise <plan.json>", "plan revise <plan.json>",
			os.Args[2:], func(args []string) error { return runRevise("plan revise", args) })
	case "run":
		return runExecute(os.Args[2:])
	case "exec":
		return runExec(os.Args[2:])
	case "show":
		// The old top-level spelling of `codeaf plan show`.
		return renamedTo("show <plan.json>", "plan show <plan.json>",
			os.Args[2:], func(args []string) error { return runShow("plan show", args) })
	case "models":
		return runModels(os.Args[2:])
	case "connect":
		return runConnect(os.Args[2:])
	case "disconnect":
		return runDisconnect(os.Args[2:])
	case "pool":
		return runPool(os.Args[2:])
	case "notebook":
		return runNotebook(os.Args[2:])
	case "collections":
		return runCollections(os.Args[2:])
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
	case "telemetry":
		// The person's door onto the anonymous-usage pipe: what it is doing,
		// exactly what would leave, and the switch. It emits nothing itself.
		return runTelemetry(os.Args[2:])
	case "manual":
		// Everything codeaf knows about itself, read straight (manual.go). It
		// is the same corpus the chat's manual tool reads, printed as it is
		// written rather than retold — and it is here rather than only there
		// because the questions people ask most are the ones they ask before
		// there is a key to make a model call with.
		return runManual(os.Args[2:])
	case "patch":
		// The edit hand's exact-match replacement (patch.go). One old text in,
		// one file with it replaced out, a count in the refusal when the text is
		// not there exactly once.
		return runPatch(os.Args[2:])
	case "doc":
		// The billed document parse the read_document tool runs, printed
		// straight (doc.go) — free on a plain file, billed on a scan.
		return runDoc(os.Args[2:])
	case "web":
		// The search and fetch pair the belt's web verbs run (web.go).
		return runWeb(os.Args[2:])
	case "image":
		// The generator the generate_image tool runs, with the same spend
		// accounting (image.go).
		return runImage(os.Args[2:])
	// Three spellings for one question, because three different callers ask it
	// and none of them should have to know which one this build prefers: the
	// agentfield Python doctor runs `codeaf version`, the Go doctor runs
	// `codeaf --version`, and a person types `-v`.
	case "version", "--version", "-v":
		return runVersion()
	case "update":
		return runUpdate(os.Args[2:])
	case "-h", "--help", "help":
		return usage(os.Args[2:])
	default:
		// A PROGRAM THIS BUILD CARRIES IS A VERB OF ITS OWN: `codeaf senior-dev
		// <brief>` (carried.go). It is asked last, after every verb above, so
		// no program's name can shadow one of codeaf's own words.
		if program, ok := builtin.Find(os.Args[1]); ok {
			return runCarried(program, os.Args[2:])
		}
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

// usageText is what `codeaf --help` prints, and it is FIVE HEADED GROUPS AND
// FIVE EXAMPLES AND NOTHING ELSE.
//
// It used to be one flat list of twenty-three commands followed by a sixty-line
// environment table, so the last thing on a person's screen after asking what
// the commands are was CODEAF_CALL_LOG_BODIES, and the commands themselves had
// scrolled off the top. The table is a REFERENCE — it is consulted, never read
// — so it lives at `codeaf help env` ([environmentText]) and the one line at
// the bottom here says so.
//
// The groups are ordered most-reached-for first rather than alphabetically,
// because a list nobody reads to the end is a list whose ordering is the whole
// design. Adjacent forms of one verb stay together.
//
// THE TABLE IS ALSO THE ONE SOURCE OF EVERY PER-COMMAND SYNOPSIS. `codeaf do
// --help` lifts `do`'s lines straight out of it ([usageForCommand]), so a
// synopsis cannot go stale, and a group heading is written at column zero
// precisely so it ends a command's block rather than joining it.
//
// AND THE EXAMPLES ARE INDENTED FOUR, NOT TWO, FOR THE SAME READER. Two spaces
// is what a command row is written with, so an example beginning `  codeaf do`
// was lifted into `codeaf do --help` as though it were part of that command's
// synopsis — which is what happened the first time they were added.
// handWorkFooter is what `do`, `exec` and `run` have in common, said ONCE under
// the group rather than three times inside it — twelve of this page's lines used
// to be the same ladder printed under each verb, in the page whose own defect
// row was that half of it was an environment table.
//
// IT IS A NAMED CONSTANT BECAUSE TWO READERS NEED IT. `codeaf --help` prints it
// under the group, and [usageForCommand] appends it to each of the three
// per-command pages — which is where somebody writing a script actually looks,
// and where taking it out of the group's lines had silently removed it. One
// source of truth, two places it is read.
var handWorkFooter = `  the three differ by how much thinking happens first: do plans and may split
  the job, exec does not plan, run follows a plan somebody saved. None takes
  --yolo, and What do and run can still refuse is a plan whose price crosses
  your limit — --yes-spend answers that in advance. All three end the same
  way, and why is in --json's stop field:
  ` + foldedExitLadder(2, helpWidth) + `
  CODEAF_EXIT_CODES=legacy restores exec's old 2/3/4/5/6 for one release`

var usageText = `codeaf — an agent you talk to, and hand work to when you walk away

Talk to it — a surface you sit in front of
  codeaf
      open the conversation this directory was last having
  codeaf chat [--model slug] [--reasoning level] [--session path] [--yolo]
              [--host host[:path]] [--at name[:path]] [--once "text"]
              [--no-compact] [--one-model] [--no-host] [--debug]
      --no-host runs the conversation in this process rather than on this
      workspace's session host; --debug keeps the whole record of the run
  codeaf resume
      pick an earlier conversation by name and open it — /resume inside the chat

Hand it work — nobody is watching, the answer is on stdout
  codeaf do   "<task>" [--db path] [--keep] [--dir dir] [--timeout 15m]
              [--json] [--yes-spend] [--model slug] [--plan-model slug]
              [--context-fill 60] [--completion-reserve 65536] [--debug]
      do one task and exit — the same living agent the chat runs, unwatched
  codeaf exec ["<prompt>"] [--dir dir] [--system text] [--max-turns N]
              [--token-budget N] [--timeout 15m] [--model slug]
              [--context-fill N] [--completion-reserve N] [--json]
              [--out file] [--debug]
      run one worker for one pass, with no planning at all
  codeaf run  <program> --input <file.json|-> [--dir dir] [--model slug]
              [--journal path] [--json]
      run one saved program: typed input in, its typed output on stdout. A
      question it was not told how to answer stops it rather than being guessed
` + handWorkFooter + `

Look at what happened — read-only, no key, nothing spent
  codeaf why self [--db path]
      show today's self-spend receipts
  codeaf why <task-id> [--db path]
      what one piece of work did — its turns, tools, arguments, how it ended
  codeaf telemetry
      the anonymous usage counts: status, info, show, off, on
  codeaf logs [--tail 40] [--follow] [--path] [--json] [--run id]
              [--call id] [--tag t] [--model m] [--node n] [--body id]
      every model call codeaf made — what was asked, which lane answered, what
      came back; --json as on disk, --body one call, CODEAF_CALL_LOG=off is off
  codeaf models [--refresh]
      the models this machine will use, and what each has been measured at
  codeaf pool [show|status|verify] [--json] [--key key]
      the Model Pool: what is resolved and cached; verify fetches with a key
  codeaf doctor [--db path]
      is this install healthy, and where does it keep things
  codeaf manual
      every page of codeaf's own manual, one per line
  codeaf manual <page> | "<question>"
      that page printed whole, or the sections that answer a question
  codeaf version
      print the build this binary was cut from (--version and -v say the same)
Housekeeping — changes state on disk or on the network
  codeaf connect
      list the model services this profile knows and which are connected
  codeaf connect <service> [--no-browser] [--region intl|cn]
      connect one: openrouter and codex sign in in your browser; the others
      take a key on stdin, or ask for one without echo
  codeaf disconnect <service>
      forget a service and the key or sign-in behind it
  codeaf update [--check] [--stable|--rc|--dev|--staging] [--version tag]
      check or install a release; this build's own channel is the default
  codeaf cache
      what the shared build cache holds, and how big it is
  codeaf cache clean [--yes]
      delete the build cache; you type "` + cacheCleanWord + `" to confirm, --yes skips it
  codeaf rebuild [--db path] [--yes]
      discard everything codeaf worked out from the journal and replay it
  codeaf serve [--workspace path] [--relay url]
      be reachable from your other devices without ssh, with a pairing code
  codeaf devices
      list the devices paired with this machine
  codeaf devices revoke <name> [--all]
      stop one device opening a conversation here, --all every device of it
  codeaf notebook [--db path]
      what it has learned, and what it has been corrected on
  codeaf notebook retract|restore <seq> [--db path]
` + collectionsSummary + `
  codeaf competence [--db path] [--model slug]
      what it has been measured as good at
  codeaf services [--db path]
      long-running processes it was asked to keep
  codeaf services stop <name> [--db path]
  codeaf wake [--db path] [--timeout 2m]
      run one full background pass by hand and exit
  codeaf patch FILE --old TEXT --new TEXT | codeaf doc PATH [--pages A-B]
  codeaf web fetch URL | web search QUERY | codeaf image "PROMPT" --out PATH
  codeaf plandb <verb> [--db path] [--json]
  codeaf help env
      the environment table: every variable and its default

Plan work by hand — a plan you can read, edit and diff
  codeaf plan new "<goal>" [--out plan.json] [--dir dir] [--json]
              [--instructions] [--passes auto|off|N] [--plan-model slug]
  codeaf plan show <plan.json>
  codeaf plan revise <plan.json> "<what happened>" [--done 1,2,3]
              [--out plan.json] [--model slug] [--plan-model slug]
  codeaf plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json]
              [--yes-spend] [--model slug] [--plan-model slug]
      a plan written to a file, then run exactly as written — it learns nothing

Examples:
    codeaf                                open the conversation you were having
    codeaf do "add a health endpoint and a test for it"
    codeaf do "summarise CHANGELOG.md" --json | jq -r .answer
    codeaf logs --tail 20 --model anthropic/claude-opus-4
    codeaf chat --host devbox:~/src/api   the chat here, the work over there

Every command answers ` + "`codeaf <command> --help`" + ` with its own line and its flags.
The environment table is ` + "`codeaf help env`" + ` — every variable and its default.`

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
const legacyEnvironmentHelp = "AFORGE_* names are read for one release when CODEAF_* is unset or empty." // legacy-name

var environmentText = `codeaf — the environment

Every variable below is read at launch. A variable set here always wins over the
` + "`/settings`" + ` sheet in the chat, and that row reads read-only in the sheet rather
than fighting your shell.

` + legacyEnvironmentHelp + `

  OPENROUTER_API_KEY   a provider key, and the first of three places one is
                       looked for — this, then OPENAI_API_KEY, then the key
                       kept in your profile. Any one of them is enough, so a
                       machine set up in the chat needs no variable at all;
                       ` + "`codeaf doctor`" + ` names the one that answered.
  CODEAF_CODEX_ISSUER  ` + codexauth.DefaultIssuer + ` by default; the sign-in issuer
                       used by ` + "`codeaf connect codex`" + `.
  CODEAF_CODEX_BACKEND the backend used to list models and run codex turns.
                       Default: ` + codexauth.DefaultBackend + `
  CODEAF_MODEL         default ` + config.DefaultModel + `
  CODEAF_PLAN_MODEL    unset: the work model plans too. Set it to run planning,
                       replans, working methods and the delivery gate on a
                       stronger model while a smaller one does the steps;
                       --model and --plan-model do the same per run.
  CODEAF_CHECK_MODEL   unset: a check follows a plan seat pinned by flag or
                       environment, otherwise it uses the crew careful row.
                       Set it to choose the check model independently.
  CODEAF_MODELS        unset: one model, exactly as above. Set it to a panel
                       and calls cascade — cheapest model first, escalating
                       when a verifier catches a failure. Either a
                       comma-separated list of slugs, or a path to a JSON file:
                       CODEAF_MODELS=google/gemma-3-12b-it,~moonshotai/kimi-k2.6
                       CODEAF_MODELS=~/.codeaf/models.json
                       Ratings accumulate in ~/.codeaf/router-ledger.json
                       across runs; see them with ` + "`codeaf models`" + `.
  CODEAF_REASONING     planning calls: model default (unset), off, low, medium,
                       high
  CODEAF_EXEC_REASONING
                       executor calls: model default (unset), off, low, medium,
                       high
  CODEAF_EXEC_TIMEOUT  ` + "`codeaf exec`" + ` only: hard wall when --timeout is not
                       passed, as a duration or a bare number of seconds.
                       CODEAF_EXEC_BUDGET and CODEAF_EXEC_TURNS do the same for
                       --token-budget and --max-turns. A flag that was typed
                       always wins; these exist so a harness can set the walls
                       once for a campaign instead of on every call.
  CODEAF_EXIT_CODES    ` + legacyExitCodesHelp + `
  CODEAF_MAX_DEPTH     2   how many levels of decomposition
  CODEAF_NODE_BUDGET   ` + strconv.Itoa(config.DefaultNodeBudget) + `  hard ceiling on total steps
  CODEAF_DAILY_BUDGET  ` + usageDollars(config.DefaultDailyBudgetUSD) + `  the day's spending limit in dollars (0 = unlimited)
  CODEAF_PLAN_CONSENT  ` + usageDollars(config.DefaultPlanConsentUSD) + `  a plan estimated above this quotes its price
                       and waits for your word (0 = never asks)
  CODEAF_IMAGE_MODEL   image-generation model (catalog-resolved by default)
  CODEAF_SPEECH_MODEL  speech-synthesis model (catalog-resolved by default)
  CODEAF_MUSIC_MODEL   music-generation model (catalog-resolved by default)
  CODEAF_VIDEO_MODEL   video-generation model (catalog-resolved by default)
  CODEAF_VISION_MODEL  image-inspection proxy (talk, work, catalog-resolved)
  CODEAF_DOC_ENGINE    auto (default), local, free or ocr document reading
  CODEAF_PRACTICE_BUDGET
                       ` + usageDollars(config.DefaultPracticeBudgetUSD) + `  daily self-practice carve-out (0 = disabled)
  CODEAF_PRACTICE_IDLE 20m  quiet period before self-practice
  CODEAF_BRIEF_AFTER   4h  minimum absence before an arrival brief (0 = always)
  CODEAF_MAX_HOURS     how many hours an unattended chat --yolo session may
                       carry its own work on (default none: it stops when the
                       model stops). --max-hours wins.
  CODEAF_MAX_COST      the same ceiling in dollars. --max-cost wins. Either one
                       alone is a budget; without one, --yolo is only the
                       approval posture it has always been.
  CODEAF_PREAUTHORIZE_SPEND
                       1 spends past the day's limit without stopping a
                       headless run to ask
  CODEAF_HOME          the whole state root — journal, workspace, CAS, craft,
                       profiles, catalog, skills (default ~/.codeaf). Move it
                       to run a disposable store that touches nothing of yours.
  CODEAF_NO_UPDATE_CHECK
                       1 skips the launch check; /update and codeaf update
                       still work.
  CODEAF_PROFILE_DIR   where measured behaviour is kept (default CODEAF_HOME)
  CODEAF_CALL_LOG      the model-call log (default <profile>/logs/calls.jsonl).
                       "off" writes nothing; any other value is the file to
                       write.
  CODEAF_CALL_LOG_BODIES=1
                       also record each call's whole request and response —
                       your prompts included. Off by default, and for one run
                       at a time.
  CODEAF_TELEMETRY    on (default). off turns the anonymous usage counts off;
                       ` + "`codeaf telemetry`" + ` says what they are and what would
                       leave, DO_NOT_TRACK=1 does the same
  CODEAF_TELEMETRY_ENDPOINT
                       where the usage counts go
                       (default https://agentfield.ai/api/oss/codeaf/telemetry);
                       set to empty to turn them off entirely
  DO_NOT_TRACK=1     the ecosystem's own opt-out word, honoured as if it were
                       CODEAF_TELEMETRY=off

The user-facing knobs above — budgets, rhythm, the document reader, the vision
and media slots — are also the ` + "`/settings`" + ` sheet in the chat, which persists
them to the profile's config.json.

Run ` + "`codeaf --help`" + ` for every command.`

// usageDollars writes a default the way the table has always written it: the
// shortest form that is still the same number, so 500 stays 500 and 2.5 stays
// 2.5 rather than growing a trailing zero nobody typed.
func usageDollars(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// usage answers `codeaf --help`, `-h` and `codeaf help`. With `env` after it,
// it prints the environment table instead — which is where the table went when
// it stopped being two thirds of the front page.
func usage(args []string) error {
	// Through the same seam every per-command usage goes through (usage.go), so
	// help is one stream and one thing a test can read back.
	if len(args) > 0 && strings.TrimSpace(args[0]) == "env" {
		fmt.Fprintln(usageOut, environmentText)
		return nil
	}
	// The table with the programs this build carries in it (carried.go): a
	// verb nobody can find on the page that lists the verbs is a verb nobody
	// types.
	fmt.Fprintln(usageOut, frontPage())
	return nil
}

// renamedTo runs an old spelling of a command and says, once and on stderr,
// what it is called now.
//
// ASKING AN OLD SPELLING FOR HELP SAYS NOTHING. `--help` runs nothing, prints
// the NEW spelling's own line out of the one table, and leaves with 0 — so a
// developer probing `codeaf show --help` is shown `codeaf plan show` and a
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
// `codeaf plan "<goal>"` was the whole command; it is `codeaf plan new
// "<goal>"` now, and the bare form still works for one release. The two are
// told apart by the word itself: a lone `new`, `show`, `revise` or `run` in the
// first position is a subcommand and anything else is the goal, which is the
// same reading `codeaf cache clean` already has.
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
	// `codeaf plan --help` is a question about the group, so it answers with all
	// four lines rather than with `plan new`'s alone.
	if askedForHelp(args) {
		return commandHelp("plan")
	}
	if len(args) == 0 {
		// Nothing was spelled the old way, so there is nothing to say about a
		// spelling. `codeaf plan` alone answers the way it always did — the
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
	useAutoSeats(settings)
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
	// A PERSON TYPED THIS (exec.go's [typedDoorContext]), so the plan's own
	// calls carry the talk pin the way `codeaf exec`'s do.
	ctx := typedDoorContext(settings.Context(context.Background(), goal))

	// The ruler in force comes from measured work when there is any; the
	// built-in prior is only the starting point.
	store := installMeasuredRulers(settings, settings.Model)

	if !*asJSON {
		// Every line of it is an aside: the answer this door gives is the PLAN,
		// and a preamble in front of it is what broke `codeaf plan new "x"
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
		Terrain: plan.RenderTerrain(*workspace, goal),
		// The directory that terrain was drawn from, so the material the goal
		// and its nodes name is measured rather than guessed at. See
		// plan/reach.go.
		Workspace:    *workspace,
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
	if err := emit(graph, *output, *asJSON); err != nil {
		return err
	}
	// A GRAPH THAT STILL CARRIES A SIZING REFUSAL IS NOT A SETTLED PLAN, AND
	// THE DOOR MAY NOT SAY IT IS. The graph is written and printed either way —
	// a plan with one leaf too big for the worker that will run it is still the
	// best account of the goal anyone has, and a caller that wants to look at it
	// or hand it to `run` must be able to. What changes is the answer to `$?`,
	// the one thing a harness reads: a node the ruler put past one worker's
	// reach and the passes then left whole is an unfinished piece of planning,
	// and exit 0 over it reads as "planned" to every script and every bench.
	// Measured: the door exited 0 on 103 of 273 draws holding exactly this.
	//
	// It is exitIncomplete rather than a failure because that is what it is —
	// it ran, something usable is above, and part of what was asked for does
	// not stand — and the reason is printed on stderr so stdout stays the plan.
	// #424 wrote this as `exitPartial`, which was rung 2 of `do`'s own table;
	// it is rung 2 of the ONE ladder now and the constant moved with it
	// (envelope.go).
	if refused := unsettledSizing(graph); len(refused) > 0 {
		fmt.Fprintln(os.Stderr)
		for _, node := range refused {
			fmt.Fprintf(os.Stderr, "not settled: %s — %s\n", node.Title, node.Undivided)
		}
		return exitIncomplete
	}
	return nil
}

// unsettledSizing lists the work nodes a finished graph still carries an
// unresolved sizing refusal on: measured past what one worker holds, and then
// left whole for a reason journaled on the node — nobody could name two pieces
// for it, the division gave back the node again, the depth ceiling arrived
// first. Every one of those is a piece of planning that did not finish.
//
// A node with no reason on it is not one of these. An oversized leaf can be
// left whole deliberately — the split gate collapsing a graph writes its own
// sentence and takes the responsibility — and what this reads is the refusal,
// not the size.
func unsettledSizing(graph *plan.Graph) []plan.Node {
	var refused []plan.Node
	for _, node := range graph.Nodes {
		if node.Kind == plan.KindWork && node.Size == plan.SizeOversized && node.Undivided != "" {
			refused = append(refused, node)
		}
	}
	return refused
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
		return fmt.Errorf("usage: codeaf plan revise <plan.json> \"<what happened>\"")
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
	useAutoSeats(settings)
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
	// `codeaf show --help` used to answer `open --help: no such file or
	// directory` — a filesystem error about a flag — because this door parses
	// no flags at all and read the argument as a path (usage.go).
	if askedForHelp(args) {
		return commandHelp(name)
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: codeaf plan show <plan.json>")
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
// person who typed `codeaf do` with nothing after it used to get a process
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
		return fmt.Errorf("no goal given\n\nrun `codeaf --help` for every command.")
	}
	return fmt.Errorf("no goal given\n\n%s\n\nrun `codeaf --help` for every command.", shape)
}

// reorder moves flags ahead of positional arguments. Go's flag package stops
// parsing at the first non-flag token, so `codeaf plan "goal" -o out.json`
// would otherwise fold the flag into the goal text — silently, which is the
// worst way for it to fail.
//
// ── A BRIEF THAT BEGINS WITH "-" IS TEXT, NOT A FLAG ────────────────────────
//
// This used to decide by shape alone: a leading dash meant a flag. So
// `codeaf do "- Update the display style property…"` — a brief written as a
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
// The wording they replaced was `(default CODEAF_MODEL)`, which named one rung
// of four and hid the two that decide most runs: a profile's crew, and this
// build's own default when nobody has said anything at all. A help string that
// names the whole ladder is the shortest place a person can learn that their
// crew reaches this command (config.ResolveSeats).
const (
	workLadderHelp    = "flag › CODEAF_MODEL › crew › default"
	planLadderHelp    = "flag › CODEAF_PLAN_MODEL › crew mastermind › the work model"
	checkLadderHelp   = "flag › CODEAF_CHECK_MODEL › plan pinned by flag or environment › crew careful"
	modelFlagHelp     = "work model for this run (" + workLadderHelp + ")"
	planModelFlagHelp = "model that plans, when different from the work model (" + planLadderHelp + ")"
	// The check seat's ladder names its environment rung and the resolved
	// plan seat fallback before the crew's careful row.
	checkModelFlagHelp = "model that checks finished work (" + checkLadderHelp + ")"
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
// looking for where their data lives searches for a store, and `codeaf doctor`
// now labels the same file that way.
const storeFlagHelp = "the store to work in"

// debugFlagHelp is what `--debug` keeps AND WHERE IT PUTS IT, said once on the
// three doors that carry it.
//
// It read `in a folder of its own under the state root`, which names no folder
// at all — and the state root has two of them. The developer who turned the
// switch on went to `~/.codeaf/runs/codeaf-do-<n>/`, which is where the run's
// own line on stderr had just pointed them for a DIFFERENT thing (the graph
// scratch `--keep` holds), found nothing but a `graph.db`, and concluded the
// record was never written. It had been: the record is a sibling of the
// model-call log, under `logs/trace/<run>/`, and the run announces the exact
// path on stderr when it has written one (internal/trace's Announce).
//
// SO THE SENTENCE NAMES THE PLACE, and names it from the constants that own it
// rather than from a path typed here — a folder that moves and a help page that
// does not is exactly how this sentence went wrong the first time.
//
// It is a function and not a constant because the root MOVES: internal/home
// reads CODEAF_HOME, so the answer is only right once the environment the door
// was started with has been read. A package-level string would be computed at
// init and would name somebody else's path for the rest of the process.
func debugFlagHelp() string {
	return "keep the full record of this run — call bodies, tool calls and the choices " +
		"made — in a folder of its own under " + debugRecordRoot() + " (env CODEAF_DEBUG)"
}

// debugRecordRoot is where the debug records go, as a person would type it:
// tilde-shortened when it really is under their home, and absolute otherwise.
func debugRecordRoot() string {
	root := home.Join(trace.DirName, trace.TraceDirName)
	if house, err := os.UserHomeDir(); err == nil && house != "" && strings.HasPrefix(root, house+string(filepath.Separator)) {
		return "~" + root[len(house):]
	}
	return root
}

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
