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

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

func main() {
	// Before anything else — before the GC is tuned, before a flag is read,
	// before a single line of aforge exists in this process — the binary asks
	// whether it was started to be something else. See swepro.go.
	if sweproSentinel(os.Getenv) {
		os.Exit(dispatchSwepro(os.Args[1:]))
	}
	// The default heap target collects several times before the surface is even
	// drawn, and none of those collections free anything worth the pause: the
	// launch path allocates a graph snapshot, a catalog, and a thread, and then
	// keeps them. Trading a few megabytes of resident memory for those cycles
	// is the right side of that bargain for an interactive tool. An explicit
	// GOGC still decides — this is a default, not a policy.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(400)
	}
	os.Exit(execute())
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
	// Who this build can hand a leaf to, declared once, before any command has
	// read a flag. Every surface below reads the same menu and the same rulers
	// because none of them registers anything of its own.
	installSubharnesses()
	if len(os.Args) < 2 {
		// No arguments opens the resident surface. On branch chat-v3 that
		// surface IS v3 (docs/CHAT-V3.md, "Entry and cutover"); v2 stays
		// reachable behind its flag until the V3-3 deletion.
		if v2, rest := wantChatV2(nil, os.Getenv); v2 {
			return runChatV2(rest)
		}
		return runChatV3(nil)
	}
	switch os.Args[1] {
	case "chat":
		// The v2 surface is chosen before the old one reads a flag, so the old
		// path runs the same bytes it ran yesterday (11.1: disconnect, don't
		// delete). Without --v2 or AFORGE_CHAT_V2 nothing here changes.
		if v2, rest := wantChatV2(os.Args[2:], os.Getenv); v2 {
			return runChatV2(rest)
		}
		// On branch chat-v3, `aforge chat` IS v3 (docs/CHAT-V3.md, "Entry and
		// cutover"). runChat stays reachable through the no-argument path
		// until v3 reaches parity in substance.
		return runChatV3(os.Args[2:])
	case "resume":
		// The chat surface, opened on the list of conversations this directory
		// has already had (internal/tui3's resume.go). It is a v3 door only:
		// the older surfaces have no session files to pick from.
		return runResumeV3(os.Args[2:])
	case "engine":
		// The far half of `aforge chat --host <host>`: the process ssh starts
		// on the other machine, speaking the wire protocol on its own pipes
		// (engine.go). It is DELIBERATELY ABSENT from the usage text below —
		// it is machinery a surface dials, not a thing a person runs, and a
		// command that draws nothing and reads no keys would only be a puzzle
		// in a list of commands that do.
		return runRemoteEngine(os.Args[2:])
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

const usageText = `aforge — build and revise task graphs

  aforge                 open the chat surface, resuming your last conversation
  aforge chat [--db path] [--session id|new]
  aforge resume          pick an earlier conversation by name and open it
                         the same list is /resume inside the chat
  aforge do   "<task>" [--db path] [--keep] [-w dir] [--timeout 900] [--json] [--yes-spend] [--model slug] [--plan-model slug]
                       [--subharness name] [--context-fill 60] [--completion-reserve 65536]
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
  aforge rebuild [--db path] [--yes]  discard every derived table and replay the journal
  aforge why self [--db path]   show today's self-spend receipts
  aforge version                print the build this binary was cut from
                                (--version and -v say the same thing)

Workers:
  chat, do and run each take --subharness <name>, which forces every leaf onto
  one worker instead of letting the compiler choose per node. Leave it unset
  unless you are measuring one worker against another. This build has:
    swe   a whole software-engineering pipeline. It takes a coding issue in a
          git repository whole — plans it, edits in parallel worktrees, judges
          each change before merging, and audits the result against that
          repository's own build and tests before calling itself done.
  An unknown name is a note on stderr and the default worker, never a refusal.

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
  AFORGE_NODE_BUDGET   60  hard ceiling on total nodes
  AFORGE_DAILY_BUDGET  20.0  daily dollar rail (0 = unlimited)
  AFORGE_IMAGE_MODEL          image-generation model (catalog-resolved by default)
  AFORGE_SPEECH_MODEL         speech-synthesis model (catalog-resolved by default)
  AFORGE_MUSIC_MODEL          music-generation model (catalog-resolved by default)
  AFORGE_VIDEO_MODEL          video-generation model (catalog-resolved by default)
  AFORGE_VISION_MODEL         image-inspection proxy model (talk/work/catalog-resolved by default)
  AFORGE_DOC_ENGINE           auto (default), local, free, or ocr document-reading rung
  AFORGE_PRACTICE_BUDGET  2.0  daily self-practice carve-out (0 = disabled)
  AFORGE_PRACTICE_IDLE  20m  quiet period before self-practice
  AFORGE_BRIEF_AFTER   4h  minimum absence before an arrival brief (0 = always)
  AFORGE_PREAUTHORIZE_SPEND  1 raises the rail without a headless stdin prompt
  AFORGE_SWE_MAX_COST  10.0  dollar ceiling on one swe leaf's run inside the
                       coding pipeline. A backstop, not a budget — the daily
                       rail is the budget.
  AFORGE_HOME          the whole state root — journal, workspace, CAS, craft,
                       profiles, catalog, skills (default ~/.aforge). Move it to
                       run a disposable brain that touches nothing of yours.
  AFORGE_PROFILE_DIR   where measured behaviour is kept (default AFORGE_HOME)

  The user-facing knobs above — budgets, rhythm, the document rung, the vision
  and media slots — are also the ` + "`/settings`" + ` sheet in the chat, which persists
  them to the profile's config.json. A variable set here always wins, and that
  row reads read-only in the sheet rather than fighting your shell.`

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
	model := flags.String("model", "", "work model for this run (default AFORGE_MODEL)")
	planModel := flags.String("plan-model", "", "model that plans, when different from the work model (default AFORGE_PLAN_MODEL)")
	// The same -w that run takes, and it means the same directory. Plan runs
	// before run in the headless pipeline, so there is no workspace yet unless
	// the person naming the goal also names the material it is about — which is
	// exactly when the material is worth looking at.
	workspace := flags.String("w", "", "directory holding the material this goal is about, read once to ground the plan")
	if err := flags.Parse(reorder(args, map[string]bool{"o": true, "ensemble": true, "model": true, "plan-model": true, "w": true})); err != nil {
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
	applyModelFlags(&settings, *model, *planModel)
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
	model := flags.String("model", "", "work model for this run (default AFORGE_MODEL)")
	planModel := flags.String("plan-model", "", "model that revises the plan, when different from the work model (default AFORGE_PLAN_MODEL)")
	if err := flags.Parse(reorder(args, map[string]bool{"o": true, "done": true, "model": true, "plan-model": true})); err != nil {
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
	applyModelFlags(&settings, *model, *planModel)
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
		fmt.Printf("goal:   %s\nevent:  %s\n\n", graph.Goal, event)
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

func readText(args []string) (string, error) {
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " ")), nil
	}
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
// worst way for it to fail. Flags that take a value are named explicitly
// because only the caller knows which ones consume the token after them.
func reorder(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			positional = append(positional, argument)
			continue
		}
		flags = append(flags, argument)
		name := strings.TrimLeft(argument, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if valueFlags[name] && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return append(flags, positional...)
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
