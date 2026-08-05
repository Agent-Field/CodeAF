package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
)

func runExecute(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	workspace := flags.String("w", "", "workspace directory (default ./aforge-run-<goal hash>)")
	output := flags.String("o", "", "write the completed graph as JSON to this file")
	concurrency := flags.Int("j", 8, "how many leaves may run at once")
	maxTurns := flags.Int("turns", 200, "runaway backstop on iterations per leaf")
	maxTokens := flags.Int("budget", 150000, "token budget per leaf — the limit that actually binds")
	contracts := flags.Bool("contracts", true, "write a per-leaf working method before executing")
	if err := flags.Parse(reorder(args, map[string]bool{"w": true, "o": true, "j": true, "turns": true, "budget": true})); err != nil {
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
	client, err := settings.Client()
	if err != nil {
		return err
	}
	ctx := settings.Context(context.Background(), graph.Goal)

	root := *workspace
	if root == "" {
		root = "aforge-" + strings.TrimPrefix(plan.RunID(graph.Goal), "aforge-")[:10]
	}
	space, err := exec.NewWorkspace(root)
	if err != nil {
		return err
	}

	fmt.Printf("goal:      %s\nworkspace: %s\n", graph.Goal, space.Root())

	// A graph planned without --brief has nothing for an agent to read, so the
	// instructions are written now rather than failing at dispatch, and then
	// the per-leaf working contracts are written from those instructions.
	// Within each pass every leaf is one independent call and all leaves run at
	// once, so a graph that already carries briefs — the normal case — pays one
	// call's latency for the whole preamble however wide it is.
	if missingBriefs(graph) > 0 || *contracts {
		start := time.Now()
		if missingBriefs(graph) > 0 {
			usage, err := plan.Briefs(ctx, client, graph)
			graph.Usage.Calls += usage.Calls
			graph.Usage.Cost += usage.Cost
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
		}
		if *contracts {
			usage, err := plan.Contracts(ctx, client, graph)
			graph.Usage.Calls += usage.Calls
			graph.Usage.Cost += usage.Cost
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
		}
		fmt.Printf("prepared:  %s in %s\n", plural(len(graph.Leaves()), "leaf"), time.Since(start).Round(10*time.Millisecond))
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
	linear := exec.NewLinear(client, space, web, *maxTurns, *maxTokens, deadline)
	scheduler := exec.NewScheduler(exec.NewRegistry(linear), space, *concurrency)
	scheduler.OnEvent = func(event exec.Event) {
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
	// The executor gets its own reasoning level. The run-wide context carries
	// the planning economy (reasoning off), which is right for briefs and wrong
	// for the loop: an agent that cannot think between tool calls writes
	// nothing down and never converges.
	runErr := scheduler.Run(settings.ExecContext(ctx), graph)

	graph.Usage.Calls += scheduler.Usage().Calls
	graph.Usage.PromptTokens += scheduler.Usage().PromptTokens
	graph.Usage.CompletionTokens += scheduler.Usage().CompletionTokens
	graph.Usage.CachedTokens += scheduler.Usage().CachedTokens
	graph.Usage.Cost += scheduler.Usage().Cost
	recordAndCalibrate(ctx, client, settings, graph)
	renderRunSummary(graph, space, scheduler.Usage(), time.Since(start))
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
	return runErr
}

// recordAndCalibrate turns the run into evidence, and lets that evidence rewrite
// the ruler the planner sizes tasks with.
//
// This is the loop closing. The sizing anchors were a prior invented before
// anything had ever executed; now every leaf that runs says how much a task of
// its shape really costs this model, and once that contradicts the ruler, the
// ruler is rewritten from tasks that actually happened.
func recordAndCalibrate(ctx context.Context, client plan.Completer, settings config.Config, graph *plan.Graph) {
	store, err := profile.Load(settings.ProfileDir, settings.Model, "linear")
	if err != nil {
		return
	}
	for _, node := range graph.Nodes {
		if node.Kind != plan.KindWork || node.Turns == 0 {
			continue
		}
		store.Add(profile.Record{
			Title:   node.Title,
			Summary: node.Summary,
			Sources: len(node.Sources),
			Size:    string(node.Size),
			Turns:   node.Turns,
			Tokens:  node.Tokens,
			Stop:    node.Stop,
			Done:    node.State == plan.StateDone,
		})
	}

	anchors, reason, _, err := plan.Recalibrate(ctx, client, store)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "note: could not recalibrate: %v\n", err)
	case anchors != "":
		store.Anchors = anchors
		fmt.Printf("\n── ruler ───────────────────────────────────────────────────────────\n")
		fmt.Printf("  recalibrated: %s\n", reason)
		for _, line := range strings.Split(anchors, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("  │ %s\n", clip(strings.TrimSpace(line), 70))
			}
		}
	default:
		fmt.Printf("\n  ruler: %s\n", reason)
	}
	if err := store.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not save profile: %v\n", err)
	}
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

func renderRunSummary(graph *plan.Graph, space *exec.Workspace, usage exec.Usage, elapsed time.Duration) {
	var done, failed, blocked, turns int
	for _, node := range graph.Nodes {
		switch node.State {
		case plan.StateDone:
			done++
		case plan.StateFailed:
			failed++
		case plan.StateBlocked:
			blocked++
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
	fmt.Printf("  |  %d agent turns  |  %d calls  |  %d in (%d cached) / %d out  |  $%.4f  |  %s\n",
		turns, usage.Calls, usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens, usage.Cost,
		elapsed.Round(time.Second))
	for _, node := range graph.Nodes {
		if node.State == plan.StateFailed || node.State == plan.StateBlocked {
			fmt.Printf("    %2d %-24s %s: %s\n", node.ID, clip(node.Title, 24), node.State, node.Failure)
		}
	}
}
