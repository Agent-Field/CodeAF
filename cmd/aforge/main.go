// Command aforge builds and revises task graphs. It executes nothing: the
// graph is the product.
//
//	aforge plan "<goal>" [-o graph.json]
//	aforge revise graph.json "<what happened>" [--done 1,2] [-o graph.json]
//	aforge show graph.json
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		// No arguments opens the resident surface: the chat thread over the
		// durable graph. The one-shot commands below are unchanged.
		return runChat(nil)
	}
	switch os.Args[1] {
	case "chat":
		return runChat(os.Args[2:])
	case "plan":
		return runPlan(os.Args[2:])
	case "revise":
		return runRevise(os.Args[2:])
	case "run":
		return runExecute(os.Args[2:])
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
	case "doctor":
		return runDoctor(os.Args[2:])
	case "why":
		return runWhy(os.Args[2:])
	case "-h", "--help", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usageText)
	}
}

const usageText = `aforge — build and revise task graphs

  aforge                 open the chat surface over the durable graph
  aforge chat [--db path] [--session id]
  aforge plan "<goal>" [-o graph.json] [--json] [--brief] [--ensemble N]
  aforge revise <graph.json> "<what happened>" [--done 1,2,3] [-o graph.json]
  aforge run  <graph.json> [-w dir] [-j 8] [-o done.json] [--yes-spend]
  aforge show <graph.json>
  aforge models
  aforge notebook [--db path]
  aforge notebook retract|restore <seq> [--db path]
  aforge competence [--db path] [--model slug]
  aforge services [--db path]
  aforge services stop <name> [--db path]
  aforge wake [--db path] [--max-seconds N]  run one full resident pass and exit
  aforge doctor [--db path]     show the brain, resident, watch, spend, and open counts
  aforge why self [--db path]   show today's self-spend receipts

Environment:
  OPENROUTER_API_KEY   required
  AFORGE_MODEL         default ` + config.DefaultModel + `
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
  AFORGE_PROFILE_DIR   where measured behaviour is kept (default ~/.aforge)`

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
	if err := flags.Parse(reorder(args, map[string]bool{"o": true, "ensemble": true})); err != nil {
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
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)
	ctx := settings.Context(context.Background(), goal)

	// The ruler in force comes from measured work when there is any; the
	// built-in prior is only the starting point.
	store, _ := profile.Load(settings.ProfileDir, settings.Model, "linear")
	plan.UseAnchors(store.Anchors)

	if !*asJSON {
		fmt.Printf("goal:   %s\nmodel:  %s (reasoning: %s)\n", goal, settings.Model, settings.Reasoning)
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
		Recall:       recallHits(history, goal, groundRecallLimit),
		SpineSamples: settings.SpineSamples,
		MaxDepth:     settings.MaxDepth,
		NodeBudget:   settings.NodeBudget,
		Briefs:       *briefs,
		Ensemble:     *ensemble,
		Report:       report,
		Progress:     headlessPlanProgress(os.Stderr),
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
	return emit(graph, *output, *asJSON)
}

func runRevise(args []string) error {
	flags := flag.NewFlagSet("revise", flag.ContinueOnError)
	output := flags.String("o", "", "write the revised graph as JSON to this file")
	asJSON := flags.Bool("json", false, "print the graph as JSON instead of a table")
	done := flags.String("done", "", "mark these node ids finished before revising")
	if err := flags.Parse(reorder(args, map[string]bool{"o": true, "done": true})); err != nil {
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
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)
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
