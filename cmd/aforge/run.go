package main

import (
	"context"
	"errors"
	"flag"
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

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/term"
)

func runExecute(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	workspace := flags.String("w", "", "workspace directory (default ./aforge-run-<goal hash>)")
	output := flags.String("o", "", "write the completed graph as JSON to this file")
	concurrency := flags.Int("j", 8, "how many leaves may run at once")
	maxTurns := flags.Int("turns", 200, "runaway backstop on iterations per leaf")
	maxTokens := flags.Int("budget", 150000, "token budget per leaf — the limit that actually binds")
	runBudget := flags.Int("run-budget", 0, "global token budget for the whole run; once passed, nothing new launches and in-flight leaves land (0 = per-leaf budgets only)")
	contracts := flags.Bool("contracts", true, "write a per-leaf working method before executing")
	yesSpend := flags.Bool("yes-spend", false, "preauthorize raising today's dollar rail when reached")
	if err := flags.Parse(reorder(args, map[string]bool{"w": true, "o": true, "j": true, "turns": true, "budget": true, "run-budget": true})); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: aforge run <graph.json> [-w dir] [-j 8]")
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
	// A graph may be loaded from disk and expanded again after an overrun, so
	// run installs the measured ruler before any planning-capable work starts.
	measured, _ := profile.Load(settings.ProfileDir, settings.Model, "linear")
	plan.UseAnchors(measured.Anchors)
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)
	ctx := settings.Context(context.Background(), graph.Goal)
	modelCatalog := catalog.Load(ctx, catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})
	mediaClient, err := settings.MediaClient()
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
	history := openDefaultHistory()
	if history != nil {
		defer history.Close()
	}

	railStore, err := openDailyRailStore()
	if err != nil {
		return err
	}
	defer railStore.Close()

	fmt.Printf("goal:      %s\nworkspace: %s\n", graph.Goal, space.Root())
	if len(settings.Panel.Models) > 0 {
		fmt.Printf("panel:     %s\n", strings.Join(panelSlugs(settings.Panel), ", "))
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
			usage, err := plan.Briefs(ctx, client, graph, progress)
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
			usage, err := plan.Contracts(ctx, client, graph, resident.ContractPlaybook(history), progress)
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
		fmt.Printf("prepared:  %s in %s\n", plural(len(graph.Leaves()), "leaf"), time.Since(start).Round(10*time.Millisecond))
	}
	if preparedUsage.Calls > 0 {
		if err := railStore.RecordUsage(store.NodeUsage{
			NodeID: store.RootID, PromptTokens: preparedUsage.PromptTokens,
			CompletionTokens: preparedUsage.CompletionTokens, Cost: preparedUsage.Cost,
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
	// reasoning on runs well past the quarter hour that fits the default.
	deadline := 15 * time.Minute
	if scaled := time.Duration(*maxTokens/50_000) * time.Minute; scaled > deadline {
		deadline = scaled
	}
	mediaTools := &exec.MediaTools{
		Provider: mediaClient, Catalog: modelCatalog, WorkingModel: settings.Model,
		ImageModel: settings.ResolveImageModel(modelCatalog), SpeechModel: settings.ResolveSpeechModel(modelCatalog),
	}
	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline).
		WithStore(history).WithMedia(mediaTools)
	scheduler := exec.NewScheduler(exec.NewRegistry(linear), space, *concurrency)
	scheduler.Budget = *runBudget
	preauthorized := spendPreauthorized(*yesSpend, os.Getenv)
	interactive := stdinIsTerminal(os.Stdin)
	var spendGate sync.Mutex
	scheduler.BeforeLaunch = func(context.Context) error {
		spendGate.Lock()
		defer spendGate.Unlock()
		rail, err := railStore.DailyRailToday(settings.DailyBudgetUSD)
		if err != nil {
			return err
		}
		rail = rail.WithAdditionalSpend(scheduler.Usage().Cost)
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
	mediaTools.BeforeSpend = scheduler.BeforeLaunch
	// A failed leaf is only worth re-running when there is somewhere stronger to
	// run it, so the panel decides rather than the scheduler assuming. One
	// escalation, not a ladder: the router lab's cascade averaged 1.35 calls a
	// task, and a leaf is the most expensive thing in the system to repeat.
	if panel, routed := client.(*router.Router); routed && panel.Rungs() > 1 {
		scheduler.Escalations = 1
	}
	// The watchdog sits above every deadline a leaf was given: it only fires
	// when an executor is wedged past all of them, and it turns that from a
	// silent forever-hang into a recorded failure the run survives.
	scheduler.NodeTimeout = deadline + 2*time.Minute
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
		fmt.Println(line)
	}

	fmt.Printf("\n── executing ───────────────────────────────────────────────────────\n")
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
			CompletionTokens: runUsage.CompletionTokens, Cost: runUsage.Cost,
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
	if report := recordAndCalibrate(ctx, client, settings, settings.Model, graph); report != "" {
		fmt.Printf("\n%s\n", report)
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
		fmt.Printf("\nwritten to %s\n", *output)
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

func recordAndCalibrateDetailed(ctx context.Context, client plan.Completer, settings config.Config, model string, graph *plan.Graph) (string, []landedProfileRecord) {
	measured, err := profile.Load(settings.ProfileDir, model, "linear")
	if err != nil {
		return fmt.Sprintf("ruler: could not load profile: %v", err), nil
	}
	var pending []landedProfileRecord
	for _, node := range graph.Nodes {
		if node.Kind != plan.KindWork || node.Turns == 0 || strings.TrimSpace(node.Title) == "" {
			continue
		}
		pending = append(pending, landedProfileRecord{planID: node.ID, record: profile.Record{
			Title:        node.Title,
			Summary:      node.Summary,
			Sources:      len(node.Sources),
			SourcesKnown: true,
			Size:         string(node.Size),
			Turns:        node.Turns,
			Tokens:       node.Tokens,
			Stop:         node.Stop,
			Verdict:      node.Verdict,
		}})
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
