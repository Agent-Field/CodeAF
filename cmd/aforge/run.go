package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/term"
)

// runExecute is `aforge run`, and `aforge run` MEANS ONE THING NOW: run one
// saved program.
//
// It used to mean two unrelated commands wearing one word — `aforge run
// <graph.json>` executed a static plan and `aforge run subharness <name>` ran a
// saved program — and the code admitted it out loud, in a `longerCommands`
// table whose entire job was to stop `aforge run --help` printing the wrong
// synopsis (usage.go). The pipeline is `aforge plan run <plan.json>` now, and
// this door reads its argument to keep both old spellings working for one
// release:
//
//   - a leading `subharness` is the old spelling of this very command;
//   - a first positional SPELLED AS A PATH is the old spelling of `aforge plan
//     run`, because a plan is a file a person points at and a program is a
//     registry name. The positional is found through the union of both doors'
//     flag sets ([namesAPlanPath]), so `--input in.json` cannot be mistaken
//     for it;
//   - anything else is a program name, which is what `run` means from here on.
func runExecute(args []string) error {
	if len(args) > 0 && args[0] == "subharness" {
		return renamedTo("run subharness <name>", "run <name>", args[1:], runSubharnessCommand)
	}
	if namesAPlanPath(args) {
		return renamedTo("run <plan.json>", "plan run <plan.json>", args,
			func(args []string) error { return runGraph("plan run", args) })
	}
	return runSubharnessCommand(args)
}

// namesAPlanPath reports whether this invocation's first positional argument is
// SPELLED AS A PATH — which is what tells the old `aforge run <plan.json>`
// apart from the new `aforge run <program>`.
//
// IT IS A QUESTION ABOUT THE WORD AND NEVER ABOUT THE DISK. It used to be
// os.Stat: a first positional that existed as a file took the old road. So a
// saved program called `formatter` executed `./formatter` as a static plan
// whenever a file of that name happened to be sitting in the working
// directory, and WHICH WORKFLOW RAN DEPENDED ON WHERE THE CALLER WAS STANDING
// — the same command, in two directories, meaning two different things. THE
// CALLER'S DIRECTORY NEVER CHANGES WHAT A COMMAND MEANS, so the reading is the
// shape of the token and nothing else ([looksLikeAPath]): a bare word is a
// registry name, and only something a person wrote as a path is a file.
//
// AND THE PATH FORM WINS A TIE. `aforge run ./formatter` takes the old road
// even where `formatter` is also a saved program, because the caller spelled a
// path on purpose; `aforge run formatter` is the saved program whatever is on
// disk beside it.
//
// The positional is found the way every other door finds one: by asking A FLAG
// SET which tokens are flags and which of those consume the token after them
// ([reorder]). The set here is the UNION of both doors' flags, so
// `aforge run myprogram --input in.json` finds `myprogram` rather than the
// input file named after it.
func namesAPlanPath(args []string) bool {
	union := commandFlags("run")
	union.String("dir", "", "")
	union.String("w", "", "")
	union.String("out", "", "")
	union.String("o", "", "")
	union.Int("parallel", 0, "")
	union.Int("j", 0, "")
	union.Int("max-turns", 0, "")
	union.Int("turns", 0, "")
	union.Int("token-budget", 0, "")
	union.Int("budget", 0, "")
	union.Int("total-token-budget", 0, "")
	union.Int("run-budget", 0, "")
	union.Bool("no-method", false, "")
	union.Bool("contracts", false, "")
	union.Bool("yes-spend", false, "")
	union.String("model", "", "")
	union.String("plan-model", "", "")
	union.String("input", "", "")
	union.String("journal", "", "")
	union.Bool("json", false, "")
	ordered := reorder(union, args)
	for index, token := range ordered {
		if token != "--" {
			continue
		}
		if index+1 >= len(ordered) {
			return false
		}
		return looksLikeAPath(ordered[index+1])
	}
	return false
}

// looksLikeAPath reports whether a token is SPELLED as a path rather than as a
// name: a separator anywhere in it, a `./`, `../` or `~` in front of it, or a
// file extension on the end.
//
// Every one of those is something a person types on purpose to mean "this
// file", and none of them can be answered differently in two directories,
// which is the whole reason the test is written here and not against the disk.
// A saved program's name is a bare word, so `formatter` is a program and
// `formatter.json`, `./formatter` and `plans/formatter` are files.
func looksLikeAPath(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.ContainsRune(token, '/') || strings.ContainsRune(token, os.PathSeparator) {
		return true
	}
	if strings.HasPrefix(token, "~") {
		return true
	}
	// A dot in the last element is an extension, and `plan.json` is a file
	// however it is reached. `filepath.Ext` is asked rather than a hand-rolled
	// LastIndex, so this and the rest of the binary agree about what an
	// extension is.
	return filepath.Ext(token) != ""
}

// runGraph executes a plan file exactly as it is written: `aforge plan run`.
func runGraph(name string, args []string) error {
	flags := commandFlags(name)
	workspace := flags.String("dir", "", "the directory to work in (default ./aforge-<goal hash>)")
	shorthandFlag(flags, "w", "dir")
	output := flags.String("out", "", "write the completed plan as JSON to this file")
	shorthandFlag(flags, "o", "out")
	concurrency := flags.Int("parallel", 32, "how many steps may run at once")
	shorthandFlag(flags, "j", "parallel")
	maxTurns := flags.Int("max-turns", 200, "runaway backstop on iterations per step (clamped to the executor's own backstop)")
	renamedFlag(flags, "turns", "max-turns")
	maxTokens := flags.Int("token-budget", 150000, "token budget per step — the limit that actually binds")
	renamedFlag(flags, "budget", "token-budget")
	runBudget := flags.Int("total-token-budget", 0, "token budget for the whole run; once passed, nothing new starts and steps in flight land (0 = per-step budgets only)")
	renamedFlag(flags, "run-budget", "total-token-budget")
	// A BOOLEAN THAT DEFAULTS ON GETS A NEGATIVE SPELLING. This was
	// `--contracts`, defaulting true, so the only way to turn it off was
	// `--contracts=false` — a form nothing else in this binary needs — and the
	// thing it turned off was named after the machinery rather than after what
	// it is: a working method for each step.
	noMethod := flags.Bool("no-method", false, "do not write a working method for each step before running")
	contractsOff := invertedFlag{off: noMethod}
	flags.Var(&contractsOff, "contracts", hiddenRenamed+"no-method")
	yesSpend := flags.Bool("yes-spend", false, yesSpendFlagHelp)
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", "model for instructions, working methods, and recalibration, when different from the work model ("+planLadderHelp+")")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	contracts := new(bool)
	*contracts = !*noMethod
	rest := flags.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: aforge plan run <plan.json> [--dir dir] [--parallel 8]")
	}
	data, err := os.ReadFile(rest[0])
	if err != nil {
		return err
	}
	graph, err := plan.Load(data)
	if err != nil {
		return err
	}
	settings, err := config.Load()
	if err != nil {
		return err
	}
	seats := config.ResolveSeats(settings.ProfileDir, *model, *planModel)
	applySeats(&settings, seats)
	// A graph may be loaded from disk and expanded again after an overrun, so
	// run installs the measured ruler before any planning-capable work starts.
	// The ruler stays keyed to the work model even when a different model
	// plans: the anchors measure the executor.
	installMeasuredRulers(settings, settings.Model)
	ctx := settings.Context(context.Background(), graph.Goal)
	// Discovery started before the clients are built, because an adapter reads
	// the catalog to decide which knobs a model will accept. It is started, not
	// waited for: every question it answers here is asked later than the first
	// frame of work, and the adapter treats a catalog that has not landed as one
	// more way of not knowing.
	//
	// It is the same catalog the ruler installation already seated for model
	// identity rather than a second one over the same cache file: one process,
	// one discovery.
	modelCatalog := sharedCatalog(settings)
	settings.Models = modelCatalog
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)
	// Planning-class calls — briefs, contracts, recalibration — run on the
	// plan slot; unsplit, this is exactly the work client.
	planner, closePlanner, err := planningClient(settings, client)
	if err != nil {
		return err
	}
	defer closePlanner()
	mediaClient, err := settings.MediaClient()
	if err != nil {
		return err
	}
	visionClient, err := settings.VisionClient()
	if err != nil {
		return err
	}
	documentClient, err := settings.DocumentClient()
	if err != nil {
		return err
	}

	root := *workspace
	if root == "" {
		root = "aforge-" + strings.TrimPrefix(plan.RunID(graph.Goal), "aforge-")[:10]
	}
	space, err := exec.NewWorkspace(root)
	if err != nil {
		return err
	}
	// The harness's own files leave the leaf's working directory here too, and
	// they go to aforge's state root rather than to a sibling of the workspace:
	// `-w` may name a person's repository, and a run that answered "outside your
	// cwd" by putting a directory next to their project would have moved the mess
	// rather than removed it. One home per workspace name, so a resumed run
	// appends to the recorders it already wrote.
	scratchRoot := home.Join("scratch", filepath.Base(space.Root()))
	if err := os.MkdirAll(scratchRoot, 0o700); err != nil {
		return err
	}
	space = space.WithScratch(scratchRoot)
	history := openDefaultHistory()
	if history != nil {
		defer history.Close()
	}

	railStore, err := openDailyRailStore()
	if err != nil {
		return err
	}
	defer railStore.Close()

	// The scratch home is printed because it is now the only place the flight
	// recorders are, and a debugger who cannot find them has no run to read.
	//
	// ALL OF IT IS AN ASIDE. This is what a person reads about the run and not
	// the run's answer, so it goes where `do` has always put the same lines
	// (streams.go); `aforge plan run p.json > result.txt` keeps the result and
	// nothing else.
	fmt.Fprintf(aside, "goal:      %s\nworkspace: %s\nrecorders: %s\n", graph.Goal, space.Root(), scratchRoot)
	// Both seats, on every run rather than only on a split one, and each with
	// the rung that chose it, AND THE SENTENCE COMES FROM THE ONE PLACE THAT
	// OWNS IT. This door used to spell the models line itself — its own label,
	// its own padding, its own placement for the inheritance notice — which is
	// the failure Seats.Report exists to prevent: the next field added to the
	// report would have been missing here and nowhere else.
	fmt.Fprintln(aside, seats.Report())
	if len(settings.Panel.Models) > 0 {
		fmt.Fprintf(aside, "panel:     %s\n", strings.Join(panelSlugs(settings.Panel), ", "))
	}

	// A graph planned without --brief has nothing for an agent to read, so the
	// instructions are written now rather than failing at dispatch, and then
	// the per-leaf working contracts are written from those instructions.
	// Within each pass every leaf is one independent call and all leaves run at
	// once, so a graph that already carries briefs — the normal case — pays one
	// call's latency for the whole preamble however wide it is.
	var preparedUsage plan.Usage
	if missingBriefs(graph) > 0 || *contracts {
		start := time.Now()
		progress := headlessPlanProgress(os.Stderr)
		if missingBriefs(graph) > 0 {
			usage, err := plan.Briefs(ctx, planner, graph, progress)
			graph.Usage.Calls += usage.Calls
			graph.Usage.Cost += usage.Cost
			preparedUsage.Calls += usage.Calls
			preparedUsage.PromptTokens += usage.PromptTokens
			preparedUsage.CompletionTokens += usage.CompletionTokens
			preparedUsage.CachedTokens += usage.CachedTokens
			preparedUsage.Cost += usage.Cost
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
		}
		if *contracts {
			usage, err := plan.Contracts(ctx, planner, graph, resident.ContractPlaybook(history), progress)
			graph.Usage.Calls += usage.Calls
			graph.Usage.Cost += usage.Cost
			preparedUsage.Calls += usage.Calls
			preparedUsage.PromptTokens += usage.PromptTokens
			preparedUsage.CompletionTokens += usage.CompletionTokens
			preparedUsage.CachedTokens += usage.CachedTokens
			preparedUsage.Cost += usage.Cost
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
		}
		fmt.Fprintf(aside, "prepared:  %s in %s\n", plural(len(graph.Leaves()), "step"), time.Since(start).Round(10*time.Millisecond))
	}
	if preparedUsage.Calls > 0 {
		if err := railStore.RecordUsage(store.NodeUsage{
			NodeID: store.RootID, PromptTokens: preparedUsage.PromptTokens,
			CompletionTokens: preparedUsage.CompletionTokens,
			CachedTokens:     preparedUsage.CachedTokens, Cost: preparedUsage.Cost,
		}); err != nil {
			return fmt.Errorf("journal headless preparation usage: %w", err)
		}
	}

	web := exec.NewWeb()
	if web == nil {
		fmt.Fprintln(os.Stderr, "note: EXA_API_KEY unset — the web tool will be unavailable")
	}
	// The deadline is a hang backstop, not a work limit, so it scales with the
	// budget the operator granted: a 2M-token leaf doing honest work with
	// reasoning on runs well past the quarter hour that fits the default. The
	// shape is asked for rather than worked out here — see
	// exec.SubharnessInfo.Deadline, which is the one place in the process that
	// knows it.
	deadline := exec.SubharnessFor(exec.LinearSubharness).Deadline(*maxTokens)
	mediaTools := &exec.MediaTools{
		Provider: mediaClient, Catalog: modelCatalog, VisionClient: visionClient, WorkingModel: settings.Model,
		DocumentClient: documentClient, DocumentEngine: settings.DocumentEngine,
		ImageModel: settings.ResolveImageModel(modelCatalog), SpeechModel: settings.ResolveSpeechModel(modelCatalog),
		MusicModel: settings.ResolveMusicModel(modelCatalog), VideoModel: settings.ResolveVideoModel(modelCatalog),
		VisionModel: settings.ResolveVisionModel(modelCatalog, settings.Model, settings.Model),
		ResolveModel: func(modality, word string) (string, error) {
			return config.ResolveMediaModel(modelCatalog, modality, word)
		},
	}
	if video, ok := modelCatalog.Model(mediaTools.VideoModel); ok {
		mediaTools.VideoPrice = video.RequestPrice
	}
	// The window a leaf remembers in is sized from what the model can hold, and
	// the catalog is the only thing on this side that knows. An unknown model,
	// or no catalog at all, passes zero and the loop takes its own default —
	// this is economics, never a capability check, and a run must not depend on
	// a metadata endpoint having answered.
	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline).
		WithStore(history).WithMedia(mediaTools).WithAttribution(settings.Attribution).
		WithContextLength(modelCatalog.ContextLength(settings.Model))
	// The worker this build constructs, offered to the scheduler. The headless
	// surface resolves a node's leaf through this registry while the resident
	// surface builds one per leaf; the covenant is that both reach the same
	// constructor, so a leaf cannot behave one way on one surface and another
	// way on the other.
	registry := exec.NewRegistry(linear)
	registerLeafExecutors(registry, leafBuild{
		settings: settings, client: client, workspace: space, web: web,
		graph: history, media: mediaTools, model: settings.Model, models: modelCatalog,
		maxTurns: *maxTurns, maxTokens: *maxTokens, deadline: deadline,
	})
	// A graph written by an older build may name a worker that no longer exists.
	// The registry will hand those leaves to linear and say nothing, which is the
	// right behavior and the wrong silence: said once per node, here, before
	// anything is spent, it is the difference between a degraded run and a run
	// that lied about which worker it measured.
	noted := make(map[int]bool)
	for _, node := range graph.Nodes {
		if node.Kind != plan.KindWork || noted[node.ID] || !degradedWorker(node.Subharness) {
			continue
		}
		noted[node.ID] = true
		noteUnavailableWorker(os.Stderr, node.Subharness)
	}
	scheduler := exec.NewScheduler(registry, space, *concurrency)
	scheduler.Budget = *runBudget
	preauthorized := spendPreauthorized(*yesSpend, os.Getenv)
	interactive := stdinIsTerminal(os.Stdin)
	var spendGate sync.Mutex
	beforeSpend := func(additional float64) error {
		spendGate.Lock()
		defer spendGate.Unlock()
		rail, err := railStore.DailyRailToday(settings.DailyBudgetUSD)
		if err != nil {
			return err
		}
		rail = rail.WithAdditionalSpend(scheduler.Usage().Cost + additional)
		if !rail.Reached {
			return nil
		}
		allowed, err := authorizeHeadlessRail(os.Stdin, os.Stderr, interactive, preauthorized, rail)
		if err != nil {
			return err
		}
		if !allowed {
			return errDailyRailNotAuthorized
		}
		origin := "headless:stdin"
		if *yesSpend {
			origin = "headless:--yes-spend"
		} else if preauthorized {
			origin = "headless:AFORGE_PREAUTHORIZE_SPEND"
		}
		return railStore.RaiseDailyRail(rail.RaiseAmount(), origin)
	}
	scheduler.BeforeLaunch = func(context.Context) error { return beforeSpend(0) }
	mediaTools.BeforeSpend = func(_ context.Context, additional float64) error { return beforeSpend(additional) }
	// A failed leaf is only worth re-running when there is somewhere stronger to
	// run it, so the panel decides rather than the scheduler assuming. One
	// escalation, not a ladder: the router lab's cascade averaged 1.35 calls a
	// task, and a leaf is the most expensive thing in the system to repeat.
	if panel, routed := client.(*router.Router); routed && panel.Rungs() > 1 {
		scheduler.Escalations = 1
	}
	// The watchdog sits above every deadline a leaf was given: it only fires
	// when an executor is wedged past all of them, and it turns that from a
	// silent forever-hang into a node that goes back on the queue with whatever
	// it had reached. The pad above the deadline is the subharness table's, for
	// the same reason the deadline is.
	scheduler.NodeTimeout = exec.SubharnessFor(exec.LinearSubharness).Watchdog(*maxTokens)
	clock := newClockWatch()
	scheduler.OnEvent = func(event exec.Event) {
		clock.sample()
		marker := map[plan.State]string{
			plan.StateRunning: "▶", plan.StateDone: "✓",
			plan.StateFailed: "✗", plan.StateBlocked: "·",
		}[event.State]
		line := fmt.Sprintf("  %s %2d %-24s %6s", marker, event.NodeID, clip(event.Title, 24), event.Elapsed.Round(time.Second))
		if event.Detail != "" {
			line += "  " + event.Detail
		}
		fmt.Fprintln(aside, line)
	}

	fmt.Fprintf(aside, "\n── executing ───────────────────────────────────────────────────────\n")
	start := time.Now()
	// An interrupt must land the run, not vanish it: a Go process dies on
	// Ctrl+C with nothing written, which is indistinguishable from a crash.
	// Routed through the context instead, the scheduler stops launching,
	// drains what is in flight, and the summary below still prints.
	runCtx, stopSignals := signal.NotifyContext(settings.ExecContext(ctx), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	// The executor gets its own reasoning level. The run-wide context carries
	// the planning economy (reasoning off), which is right for briefs and wrong
	// for the loop: an agent that cannot think between tool calls writes
	// nothing down and never converges.
	runErr := scheduler.Run(runCtx, graph)
	stopSignals()

	runUsage := scheduler.Usage()
	if runUsage.Calls > 0 {
		if err := railStore.RecordUsage(store.NodeUsage{
			NodeID: store.RootID, PromptTokens: runUsage.PromptTokens,
			CompletionTokens: runUsage.CompletionTokens,
			CachedTokens:     runUsage.CachedTokens, Cost: runUsage.Cost,
		}); err != nil && runErr == nil {
			runErr = fmt.Errorf("journal headless usage: %w", err)
		}
	}
	railDeclined := errors.Is(runErr, errDailyRailNotAuthorized)
	summaryErr := runErr
	if railDeclined {
		summaryErr = nil
	}

	graph.Usage.Calls += runUsage.Calls
	graph.Usage.PromptTokens += runUsage.PromptTokens
	graph.Usage.CompletionTokens += runUsage.CompletionTokens
	graph.Usage.CachedTokens += runUsage.CachedTokens
	graph.Usage.Cost += runUsage.Cost
	clock.sample()
	if report := recordAndCalibrate(ctx, planner, settings, settings.Model, graph); report != "" {
		fmt.Fprintf(aside, "\n%s\n", report)
	}
	renderRunSummary(graph, space, runUsage, time.Since(start), clock, summaryErr)
	if *output != "" {
		encoded, err := graph.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(aside, "\nwritten to %s\n", *output)
	}
	if railDeclined {
		return nil
	}
	return runErr
}

var errDailyRailNotAuthorized = errors.New("daily budget reached; approval not granted")

func openDailyRailStore() (*store.Store, error) {
	path := defaultChatDB()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daily rail store: %w", err)
	}
	graph, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open daily rail store: %w", err)
	}
	return graph, nil
}

func spendPreauthorized(flagged bool, getenv func(string) string) bool {
	return flagged || (getenv != nil && getenv("AFORGE_PREAUTHORIZE_SPEND") == "1")
}

func authorizeHeadlessRail(input io.Reader, output io.Writer, interactive, preauthorized bool, rail store.DailyRail) (bool, error) {
	fmt.Fprintln(output, rail.Question())
	if preauthorized {
		fmt.Fprintln(output, "spend preauthorized; raising today's rail and continuing")
		return true, nil
	}
	if !interactive {
		fmt.Fprintln(output, "stdin is not a TTY; rerun with --yes-spend or AFORGE_PREAUTHORIZE_SPEND=1 to continue without a prompt")
		return false, nil
	}
	fmt.Fprint(output, "Continue? [y/N] ")
	var answer string
	if _, err := fmt.Fscan(input, &answer); err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(output)
			return false, nil
		}
		return false, err
	}
	fmt.Fprintln(output)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func stdinIsTerminal(input *os.File) bool {
	if input == nil {
		return false
	}
	return term.IsTerminal(input.Fd())
}

// recordAndCalibrate turns the run into evidence, and lets that evidence rewrite
// the ruler the planner sizes tasks with.
//
// This is the loop closing. The sizing anchors were a prior invented before
// anything had ever executed; now every leaf that runs says how much a task of
// its shape really costs this model, and once that contradicts the ruler, the
// ruler is rewritten from tasks that actually happened.
func recordAndCalibrate(ctx context.Context, client plan.Completer, settings config.Config, model string, graph *plan.Graph) string {
	report, _ := recordAndCalibrateDetailed(ctx, client, settings, model, graph)
	return report
}

type landedProfileRecord struct {
	planID int
	record profile.Record
}

// recordAndCalibrateDetailed turns the run's landed leaves into profile records
// and recalibrates the ruler against them.
//
// One measurement, one file, one ruler. The profile is what the planner's ruler
// is rewritten from, so a record that describes something other than an
// ordinary leaf would teach the planner the wrong size for every leaf after it.
// It runs with or without evidence, because its report line is the one this
// function has always returned.
func recordAndCalibrateDetailed(ctx context.Context, client plan.Completer, settings config.Config, model string, graph *plan.Graph) (string, []landedProfileRecord) {
	pending := make([]landedProfileRecord, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Kind != plan.KindWork || node.Turns == 0 || strings.TrimSpace(node.Title) == "" {
			continue
		}
		pending = append(pending, landedProfileRecord{planID: node.ID, record: profile.Record{
			Title:   node.Title,
			Summary: node.Summary,
			// The touch-list and the fan-in, which are two different facts and
			// were one field for as long as the join price was wrong. Sources
			// says how much this leaf had to visit; FanIn says how much landed
			// in it, and only the second prices reassembly.
			Sources:      len(node.Sources),
			SourcesKnown: true,
			FanIn:        profile.FanInOf(recordedFanIn(node)),
			Size:         recordedSize(node),
			Turns:        node.Turns,
			Tokens:       node.Tokens,
			Cost:         node.Cost,
			Stop:         node.Stop,
			Verdict:      node.Verdict,
			// What the worker said about its own fit. It is empty on every node
			// that was taken and finished, which is the additive law arriving at
			// the profile file: an existing profile gains no new keys.
			Calibration: append([]string(nil), node.Calibration...),
		}})
	}
	return recordAndCalibrateWorker(ctx, client, settings, model, pending)
}

// recordedSize is the shape this leaf is journaled under, and it never writes
// the empty string.
//
// A node reaches here unsized when nobody ever asked how big it was: the
// undivided shortcut, a single-leaf remainder, a node spliced in after planning.
// Those are not a missing measurement, they are a shape — one worker over the
// whole job — and writing them as "" filed the commonest thing the product does
// into a bucket no reader looks up, where it could never gather the eight
// samples an expectation needs. It is the same reasoning that gave direct
// dispatch its own bucket rather than calling it atomic.
func recordedSize(node plan.Node) string {
	if node.Size == plan.SizeUnknown {
		return profile.BucketWhole
	}
	return string(node.Size)
}

// recordedFanIn is how many earlier results landed in this leaf: what the
// claiming surface measured against the live store, or the plan's own
// dependency count when nobody measured.
//
// It is deliberately not len(node.Sources). Sources is the touch-list — the
// pages and datasets a part must visit — and reading it as a fan-in is what made
// every ordinary leaf look like a join and priced reassembly at the price of an
// atomic.
func recordedFanIn(node plan.Node) int {
	if node.FanIn != nil {
		return *node.FanIn
	}
	return len(node.Needs)
}

// recordAndCalibrateWorker writes the records into the profile file and
// recalibrates the ruler against its own three examples.
func recordAndCalibrateWorker(ctx context.Context, client plan.Completer, settings config.Config,
	model string, pending []landedProfileRecord) (string, []landedProfileRecord) {
	measured, err := profile.Load(settings.ProfileDir, model, exec.LinearSubharness)
	if err != nil {
		return fmt.Sprintf("ruler: could not load profile: %v", err), nil
	}
	records := make([]profile.Record, len(pending))
	for index := range pending {
		records[index] = pending[index].record
	}
	added := measured.Add(records...)
	for index := range added {
		pending[index].record = added[index]
	}

	anchors, reason, _, recalibrateErr := plan.Recalibrate(ctx, client, measured)
	var report string
	switch {
	case recalibrateErr != nil:
		report = fmt.Sprintf("ruler: could not recalibrate: %v", recalibrateErr)
	case anchors != "":
		measured.Anchors = anchors
		plan.UseAnchors(anchors)
		var rendered strings.Builder
		fmt.Fprintf(&rendered, "ruler recalibrated: %s", reason)
		for _, line := range strings.Split(anchors, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(&rendered, "\n│ %s", clip(strings.TrimSpace(line), 70))
			}
		}
		report = rendered.String()
	default:
		report = "ruler: " + reason
	}
	if saveErr := measured.Save(); saveErr != nil {
		report += fmt.Sprintf("\nprofile: could not save: %v", saveErr)
		pending = nil
	}
	return report, pending
}

func missingBriefs(graph *plan.Graph) int {
	count := 0
	for _, id := range graph.Leaves() {
		if node := graph.Node(id); node != nil && strings.TrimSpace(node.Brief) == "" {
			count++
		}
	}
	return count
}

// clockJumpThreshold is how far the two clocks may drift before the run is
// treated as having been suspended. Ordinary NTP correction moves the wall
// clock by milliseconds; a closed laptop moves it by minutes.
const clockJumpThreshold = 2 * time.Minute

// clockWatch notices the machine sleeping mid-run.
//
// A suspended host stalls every leaf, and the monotonic clock stops with it, so
// every elapsed time reported afterwards is short by however long the machine
// was out — silently, which is the problem. The gap is measured as the
// divergence between wall and monotonic time rather than as the delay between
// two scheduler events, because a long leaf legitimately emits nothing for a
// quarter of an hour and would otherwise look identical to a suspend.
type clockWatch struct {
	wall time.Time // monotonic reading stripped, so subtraction is real time
	mono time.Time
	gap  time.Duration
}

func newClockWatch() *clockWatch {
	now := time.Now()
	return &clockWatch{wall: now.Round(0), mono: now}
}

// sample is called from the scheduler's own goroutine, the only one that emits
// events, so the largest-gap update needs no lock.
func (c *clockWatch) sample() {
	if gap := time.Now().Round(0).Sub(c.wall) - time.Since(c.mono); gap > c.gap {
		c.gap = gap
	}
}

// jumped reports the suspend in whole minutes: the point is the order of
// magnitude, not the precision.
func (c *clockWatch) jumped() (int, bool) {
	return int(c.gap.Minutes()), c.gap >= clockJumpThreshold
}

func renderRunSummary(graph *plan.Graph, space *exec.Workspace, usage exec.Usage, elapsed time.Duration, clock *clockWatch, runErr error) {
	var done, failed, blocked, inFlight, neverStarted, turns int
	for _, node := range graph.Nodes {
		switch node.State {
		case plan.StateDone:
			done++
		case plan.StateFailed:
			failed++
		case plan.StateBlocked:
			blocked++
		case plan.StateRunning:
			inFlight++
		case plan.StatePending:
			neverStarted++
		}
		turns += node.Turns
	}

	fmt.Printf("\n── result ──────────────────────────────────────────────────────────\n")
	for _, node := range graph.Nodes {
		if node.Kind == plan.KindSynthesis && node.State == plan.StateDone && node.Result != "" {
			fmt.Printf("\n%s\n\n", node.Title)
			for _, line := range wrap(node.Result, 74) {
				fmt.Println("  " + line)
			}
		}
	}
	// Only what the run itself wrote counts as an artifact. Listing the
	// workspace root reported a cloned repository's own README as output —
	// which reads as "covered everything" when nothing was produced at all.
	written := map[string]bool{}
	for _, node := range graph.Nodes {
		for _, artifact := range node.Artifacts {
			written[artifact] = true
		}
	}
	if len(written) > 0 {
		fmt.Printf("\n  files written in %s:\n", space.Root())
		paths := make([]string, 0, len(written))
		for path := range written {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			display := path
			if relative, err := filepath.Rel(space.Root(), path); err == nil && !strings.HasPrefix(relative, "..") {
				display = relative
			}
			// Sizes come from the workspace rather than os.Stat on the
			// recorded string: that string is workspace-relative and the root
			// may be spelled through a symlink, so statting it directly
			// reported every artifact as 0 bytes — a run that produced a full
			// deliverable read as one that produced nothing.
			if size, ok := space.Size(path); ok {
				fmt.Printf("    %-40s %6d bytes\n", display, size)
			} else {
				fmt.Printf("    %-40s %6s\n", display, "missing")
			}
		}
	}

	fmt.Printf("\n  %d done", done)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	if blocked > 0 {
		fmt.Printf(", %d blocked", blocked)
	}
	if inFlight > 0 {
		fmt.Printf(", %d in flight when the run stopped", inFlight)
	}
	if neverStarted > 0 {
		fmt.Printf(", %d never started", neverStarted)
	}
	fmt.Printf("  |  %d agent turns  |  %d calls  |  %d in (%d cached) / %d out  |  $%.4f  |  %s\n",
		turns, usage.Calls, usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens, usage.Cost,
		elapsed.Round(time.Second))
	// The stop reason is part of the summary, not something to reconstruct
	// from a bare exit code: a run that stopped early must say so here, next
	// to the accounting of what it managed before stopping.
	if runErr != nil {
		fmt.Printf("  stopped early: %v\n", runErr)
	}
	if minutes, ok := clock.jumped(); ok {
		fmt.Printf("  clock jumped %dm — machine likely slept; timings unreliable\n", minutes)
	}
	for _, node := range graph.Nodes {
		switch node.State {
		case plan.StateFailed, plan.StateBlocked:
			fmt.Printf("    %2d %-24s %s: %s\n", node.ID, clip(node.Title, 24), node.State, node.Failure)
		case plan.StateRunning:
			fmt.Printf("    %2d %-24s in flight when the run stopped\n", node.ID, clip(node.Title, 24))
		case plan.StatePending:
			fmt.Printf("    %2d %-24s never started\n", node.ID, clip(node.Title, 24))
		}
	}
}
