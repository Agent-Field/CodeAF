// Command aforge builds and revises task graphs. It executes nothing: the
// graph is the product.
//
//	aforge plan "<goal>" [-o graph.json]
//	aforge revise graph.json "<what happened>" [--done 1,2] [-o graph.json]
//	aforge show graph.json
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
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
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
		return runPlan(os.Args[2:])
	case "revise":
		return runRevise(os.Args[2:])
	case "run":
		return runExecute(os.Args[2:])
	case "exec":
		return runExec(os.Args[2:])
	case "show":
		return runShow(os.Args[2:])
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
	// Three spellings for one question, because three different callers ask it
	// and none of them should have to know which one this build prefers: the
	// agentfield Python doctor runs `aforge version`, the Go doctor runs
	// `aforge --version`, and a person types `-v`.
	case "version", "--version", "-v":
		return runVersion()
	case "-h", "--help", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usageText)
	}
}

// usageText is a var and not a const for ONE reason: the dollar figures in the
// environment table are the real defaults, interpolated from the constants that
// own them ([config.DefaultDailyBudgetUSD] and the rest). They were typed out by
// hand here once, and every one of them was stale by the time somebody read it —
// which is the one-source-of-truth law's own worked example.
var usageText = `aforge — build and revise task graphs

  aforge                 open the chat surface, resuming your last conversation
  aforge chat [--model slug] [--reasoning level] [--session path] [--host host[:path]]
              [--at name[:path]] [--once "text"] [--no-compact] [--yolo] [--one-model]
                         --session names a transcript FILE to resume, not an id and not
                         the word "new": a path that does not exist yet is a new
                         conversation written there, and no --session at all resumes
                         this directory's most recent
  aforge resume          pick an earlier conversation by name and open it
                         the same list is /resume inside the chat
  aforge serve [--workspace path] [--relay url]
                         be reachable from your other devices without ssh: this machine dials out,
                         prints the name it answers to, and shows a pairing code for a new device
  aforge devices [revoke [--all] <name>]
                         list the devices paired with this machine, and stop one
  aforge do   "<task>" [--db path] [--keep] [-w dir] [--timeout 900] [--json] [--yes-spend] [--model slug] [--plan-model slug]
                       [--context-fill 60] [--completion-reserve 65536]
                         do one task and exit — the same living brain the chat runs, with nobody watching
                         the task is run verbatim: what you type is the goal, and what it has to assume it declares
                         exit 0 the whole of it stands · 1 nothing usable · 2 partial: the wall came first,
                         the delivery gate rejected it, or parts of it did not land
  aforge plan "<goal>" [-o graph.json] [-w dir] [--json] [--brief] [--ensemble N] [--model slug] [--plan-model slug]
  aforge revise <graph.json> "<what happened>" [--done 1,2,3] [-o graph.json] [--model slug] [--plan-model slug]
  aforge run  <graph.json> [-w dir] [-j 8] [-o done.json] [--yes-spend] [--model slug] [--plan-model slug]
                         plan and run are the static pipeline: a graph written to a file, then executed
                         exactly as written. Kept for reading, editing, and inspecting a plan by hand.
  aforge exec ["<prompt>"] [-w dir] [--system text] [--turns N] [--budget N] [--timeout seconds]
                         [--model slug] [--plan-model slug] [--context-fill N] [--completion-reserve N]
                         [--json] [-o file]
                         run one linear worker with no resident planning graph
  aforge run subharness <name> --input <file.json|-> [-w dir] [--model slug] [--journal path]
                         run one subharness as a program, with nobody watching: typed input in,
                         its account and its typed output on stdout, everything else on stderr
                         a question it was not told how to answer stops it rather than being guessed
                         exit 0 it finished · 1 it could not be run at all · 2 it ran and did not finish
  aforge show <graph.json>
  aforge models
  aforge notebook [--db path]
  aforge notebook retract|restore <seq> [--db path]
  aforge competence [--db path] [--model slug]
  aforge services [--db path]
  aforge services stop <name> [--db path]
  aforge wake [--db path] [--max-seconds N]  run one full resident pass and exit
  aforge doctor [--db path]     show the brain, resident, watch, spend, and open counts
  aforge logs [--tail 40] [--follow] [--path]
                                every model call aforge made — what was asked, what came
                                back, and which ones are still in flight. Prompts are not
                                in it. AFORGE_CALL_LOG=off turns it off, or names a file.
  aforge cache                  what the shared build cache holds, and how big it is
  aforge cache clean [--yes]    delete ~/.aforge/cache to free disk. It prints the size and
                                path, then asks you to type "clean" — --yes skips the
                                question for scripts. Conversations are never touched.
  aforge rebuild [--db path] [--yes]  discard every derived table and replay the journal
  aforge why self [--db path]   show today's self-spend receipts
  aforge why <node-id> [--db path]  show what one leaf actually did: its turns, the
                                tools it called with what arguments, what came back,
                                and how it ended
  aforge version                print the build this binary was cut from
                                (--version and -v say the same thing)

Environment:
  OPENROUTER_API_KEY   required
  AFORGE_MODEL         default ` + config.DefaultModel + `
  AFORGE_PLAN_MODEL    unset: the work model plans too. Set it to run planning,
                       replans, contracts, and the delivery gate on a stronger
                       model while a smaller one executes the leaves; --model
                       and --plan-model do the same per run.
  AFORGE_MODELS        unset: one model, exactly as above. Set it to a panel and
                       calls cascade — cheapest model first, escalating when a
                       verifier catches a failure. Either a comma-separated list
                       of slugs, or a path to a JSON file:
                         AFORGE_MODELS=google/gemma-3-12b-it,~deepseek/deepseek-v4-flash-latest,moonshotai/kimi-k2.6
                         AFORGE_MODELS=~/.aforge/models.json
                       Ratings accumulate in ~/.aforge/router-ledger.json across
                       runs; see them with ` + "`aforge models`" + `.
  AFORGE_REASONING     planning calls: off (default), low, medium, high
  AFORGE_EXEC_REASONING  executor calls: model default (unset), off, low, medium, high
  AFORGE_EXEC_TIMEOUT  ` + "`aforge exec`" + ` only: hard wall in seconds when --timeout is
                       not passed. AFORGE_EXEC_BUDGET and AFORGE_EXEC_TURNS do
                       the same for --budget and --turns. A flag that was typed
                       always wins; these exist so a harness can set the walls
                       once for a campaign instead of on every call.
  AFORGE_MAX_DEPTH     2   how many levels of decomposition
  AFORGE_NODE_BUDGET   ` + strconv.Itoa(config.DefaultNodeBudget) + `  hard ceiling on total nodes
  AFORGE_DAILY_BUDGET  ` + usageDollars(config.DefaultDailyBudgetUSD) + `  daily dollar rail (0 = unlimited)
  AFORGE_PLAN_CONSENT  ` + usageDollars(config.DefaultPlanConsentUSD) + `  a plan estimated above this quotes its price
                       and waits for your word (0 = never asks)
  AFORGE_IMAGE_MODEL          image-generation model (catalog-resolved by default)
  AFORGE_SPEECH_MODEL         speech-synthesis model (catalog-resolved by default)
  AFORGE_MUSIC_MODEL          music-generation model (catalog-resolved by default)
  AFORGE_VIDEO_MODEL          video-generation model (catalog-resolved by default)
  AFORGE_VISION_MODEL         image-inspection proxy model (talk/work/catalog-resolved by default)
  AFORGE_DOC_ENGINE           auto (default), local, free, or ocr document-reading rung
  AFORGE_PRACTICE_BUDGET  ` + usageDollars(config.DefaultPracticeBudgetUSD) + `  daily self-practice carve-out (0 = disabled)
  AFORGE_PRACTICE_IDLE  20m  quiet period before self-practice
  AFORGE_BRIEF_AFTER   4h  minimum absence before an arrival brief (0 = always)
  AFORGE_MAX_HOURS     how many hours an unattended chat --yolo session may
                       carry its own work on (default none: it stops when the
                       model stops). --max-hours wins.
  AFORGE_MAX_COST      the same ceiling in dollars. --max-cost wins. Either one
                       alone is a budget; without one, --yolo is only the
                       approval posture it has always been.
  AFORGE_PREAUTHORIZE_SPEND  1 raises the rail without a headless stdin prompt
  AFORGE_HOME          the whole state root — journal, workspace, CAS, craft,
                       profiles, catalog, skills (default ~/.aforge). Move it to
                       run a disposable brain that touches nothing of yours.
  AFORGE_PROFILE_DIR   where measured behaviour is kept (default AFORGE_HOME)
  AFORGE_CALL_LOG      the model-call log (default <profile>/logs/calls.jsonl).
                       "off" writes nothing; any other value is the file to write.
  AFORGE_CALL_LOG_BODIES=1
                       also record each call's whole request and response — your
                       prompts included. Off by default, and for one run at a time.

  The user-facing knobs above — budgets, rhythm, the document rung, the vision
  and media slots — are also the ` + "`/settings`" + ` sheet in the chat, which persists
  them to the profile's config.json. A variable set here always wins, and that
  row reads read-only in the sheet rather than fighting your shell.`

// usageDollars writes a default the way the table has always written it: the
// shortest form that is still the same number, so 500 stays 500 and 2.5 stays
// 2.5 rather than growing a trailing zero nobody typed.
func usageDollars(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func usage() error {
	fmt.Println(usageText)
	return nil
}

func runPlan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	output := flags.String("o", "", "write the graph as JSON to this file")
	asJSON := flags.Bool("json", false, "print the graph as JSON instead of a table")
	briefs := flags.Bool("brief", false, "write a self-contained instruction for every leaf")
	ensemble := flags.Int("ensemble", plan.EnsembleAuto, "0 decide from the goal, -1 never, N>=2 force N independent passes and merge them")
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", planModelFlagHelp)
	// The same -w that run takes, and it means the same directory. Plan runs
	// before run in the headless pipeline, so there is no workspace yet unless
	// the person naming the goal also names the material it is about — which is
	// exactly when the material is worth looking at.
	workspace := flags.String("w", "", "directory holding the material this goal is about, read once to ground the plan")
	if err := flags.Parse(reorder(flags, args)); err != nil {
		return err
	}
	goal, err := readText(flags.Args())
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
		fmt.Printf("goal:   %s\nmodel:  %s (reasoning: %s)\n", goal, settings.PlanModelResolved(), settings.Reasoning)
		fmt.Println(seats.Line())
		if settings.PlanSplit() {
			fmt.Printf("sized for: %s (the work model this ruler measures)\n", settings.Model)
		}
		if spread := store.Measure(); spread.Samples > 0 {
			calibrated := "built-in"
			if strings.TrimSpace(store.Anchors) != "" {
				calibrated = "calibrated"
			}
			fmt.Printf("ruler:  %s, from %d measured tasks (%d-%d turns, median %d)\n",
				calibrated, spread.Samples, spread.MinTurns, spread.MaxTurns, spread.Median)
		}
		fmt.Println()
	}
	report := func(pass string, elapsed time.Duration, detail string) {
		if !*asJSON {
			fmt.Printf("  %-8s %-22s %s\n", pass, detail, elapsed.Round(10*time.Millisecond))
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
				fmt.Printf("    ready   %-22s %s\n", clip(node.Title, 22), elapsed.Round(10*time.Millisecond))
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
func runRevise(args []string) error {
	flags := flag.NewFlagSet("revise", flag.ContinueOnError)
	output := flags.String("o", "", "write the revised graph as JSON to this file")
	asJSON := flags.Bool("json", false, "print the graph as JSON instead of a table")
	done := flags.String("done", "", "mark these node ids finished before revising")
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", "model that revises the plan, when different from the work model ("+planLadderHelp+")")
	if err := flags.Parse(reorder(flags, args)); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: aforge revise <graph.json> \"<what happened>\"")
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
		fmt.Printf("goal:   %s\nevent:  %s\n", graph.Goal, event)
		fmt.Printf("%s\n\n", seats.Line())
	}
	start := time.Now()
	operations, usage, err := plan.Revise(ctx, client, graph, event)
	if err != nil {
		return err
	}
	graph.Usage.Calls += usage.Calls
	graph.Usage.Cost += usage.Cost

	if !*asJSON {
		fmt.Printf("  revise   %-22s %s\n\n", plural(len(operations), "operation"), time.Since(start).Round(10*time.Millisecond))
		renderOperations(operations)
	}
	return emit(graph, *output, *asJSON)
}

func runShow(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: aforge show <graph.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	graph, err := plan.Load(data)
	if err != nil {
		return err
	}
	fmt.Printf("goal:   %s\n", graph.Goal)
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
		fmt.Printf("\nwritten to %s\n", output)
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
func readText(args []string) (string, error) {
	if len(args) == 1 && args[0] == "-" {
		return readPipedText()
	}
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " ")), nil
	}
	if stdinIsTerminal(os.Stdin) {
		return "", fmt.Errorf("no goal given\n\n%s", usageText)
	}
	return readPipedText()
}

func readPipedText() (string, error) {
	piped, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(piped))
	if text == "" {
		return "", fmt.Errorf("no goal given\n\n%s", usageText)
	}
	return text, nil
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
