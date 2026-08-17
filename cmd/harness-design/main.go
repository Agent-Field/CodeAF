// Command harness-design is a development rig, not a shipped surface.
//
// THE QUESTION IT ASKS: handed a plain goal and nothing else, can a MODEL
// architect a good sub-harness — and does the harness it wrote produce a good
// answer when it is actually run?
//
// That question is why nothing here is hand-written. A fixture somebody typed
// proves the runner works, which internal/subharness already proves in its own
// tests; what it cannot prove is that the DESIGN half is reachable by a model
// from words a person would say. So the rig has three stages and no shortcuts
// between them:
//
//	STAGE 1  DESIGN  a designer brief (designer.go) + the goal → a harness page,
//	                 its cues and its justification. Decoded and validated by
//	                 internal/subharness, retried with the error fed back.
//	STAGE 2  REVIEW  the page, the justification, and the CARD — the rendering a
//	                 person actually approves — printed for a human.
//	STAGE 3  RUN     the page executed against a model-backed Env (execmodel.go),
//	                 with the full trace and every step's whole output printed,
//	                 and the trace saved under harnesses/<name>/run/<ts>.json.
//
// Usage:
//
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal all
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal "your own sentence"
//	                     -model deepseek/deepseek-v4-flash -design-only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// The goals, simple → complex. They are held here rather than passed in so that
// two runs of this rig are comparable, and they are the person's OWN words: the
// point is a plain sentence, not a specification.
var goals = []struct {
	key  string
	note string
	text string
}{
	{
		key:  "g1",
		note: "simple",
		text: "Give me three punchy taglines for a CLI tool that watches files.",
	},
	{
		key:  "g2",
		note: "medium",
		text: "Research the current state of small open-weight LLMs for coding (2026) and give me a cited summary.",
	},
	{
		key:  "g3",
		note: "complex",
		text: "Decide whether our Go TUI should adopt a component model like Elm or stay with ad-hoc views — investigate both sides, argue against your own conclusion, then produce a recommendation with the strongest counterargument addressed.",
	},
}

func main() {
	var (
		which      = flag.String("goal", "all", "g1|g2|g3|all, or a goal of your own in quotes")
		model      = flag.String("model", "deepseek/deepseek-v4-flash", "the OpenRouter model that designs and runs")
		storeDir   = flag.String("store", "harnesses", "where pages and run traces land")
		retries    = flag.Int("retries", 2, "how many times a failed design is fed its own error back")
		designOnly = flag.Bool("design-only", false, "stop after stage 2")
		gate       = flag.String("gate", "approve", "what the absent person says at a human.gate: approve|decline|intervene")
		maxTurns   = flag.Int("max-turns", 4, "the clamp on one agent.loop's rounds, whatever the page asked for")
		maxTokens  = flag.Int("design-tokens", 16000, "the design turn's completion budget; a reasoning model spends most of it thinking")
		temp       = flag.Float64("temp", 0.3, "the design turn's temperature")
	)
	flag.Parse()

	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		die("OPENROUTER_API_KEY is not set")
	}

	chosen := pick(*which)
	if len(chosen) == 0 {
		die("no goal to run")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	chat := newChatClient(key, *model)
	system := designerSystem(availableTools)
	store := subharness.At(*storeDir)

	fmt.Printf("harness-design · model %s · store %s\n", *model, store.Dir())
	fmt.Printf("designer brief: %d bytes, %d lines\n", len(system), strings.Count(system, "\n")+1)

	failed := 0
	for _, goal := range chosen {
		if err := oneGoal(ctx, chat, store, system, goal.key, goal.note, goal.text, *retries, *designOnly, *gate, *maxTurns, *maxTokens, *temp); err != nil {
			failed++
			fmt.Printf("\n!! %s ended in an error: %v\n", goal.key, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	fmt.Printf("\n%s\ntotal  %s\n", rule(), chat.bill())
	if failed > 0 {
		os.Exit(1)
	}
}

// oneGoal is the three stages for one goal.
func oneGoal(
	ctx context.Context,
	chat *chatClient,
	store *subharness.Store,
	system, key, note, goal string,
	retries int,
	designOnly bool,
	gate string,
	maxTurns, maxTokens int,
	temperature float64,
) error {
	head(fmt.Sprintf("%s · %s", key, note), goal)

	// ── STAGE 1 · DESIGN ────────────────────────────────────────────────────
	//
	// The history is kept across attempts on purpose. A designer that is shown
	// its own broken page AND the validator's sentence is being asked to repair
	// what it wrote; one that is only shown the error is being asked to guess
	// again, and the number this rig reports would be measuring the wrong thing.
	history := []message{
		{Role: "system", Content: system},
		{Role: "user", Content: "THE GOAL:\n\n" + goal + "\n\nDesign the sub-harness for it."},
	}
	var (
		envelope design
		harness  subharness.Harness
		attempts int
		err      error
	)
	for attempt := 0; attempt <= retries; attempt++ {
		attempts = attempt + 1
		began := time.Now()
		var raw string
		envelope, harness, raw, err = designOnce(ctx, chat, history, maxTokens, temperature)
		if err == nil {
			fmt.Printf("stage 1  design accepted on attempt %d/%d · %s\n", attempts, retries+1, time.Since(began).Round(time.Millisecond))
			break
		}
		fmt.Printf("stage 1  attempt %d/%d refused · %s\n         %v\n", attempts, retries+1, time.Since(began).Round(time.Millisecond), err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == retries {
			if raw != "" {
				fmt.Printf("\n         the last reply, verbatim:\n%s\n", indent(clip(raw, 4000), "         "))
			}
			return fmt.Errorf("no valid design in %d attempts: %w", attempts, err)
		}
		history = append(history,
			message{Role: "assistant", Content: raw},
			message{Role: "user", Content: "That harness was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."})
	}

	// ── STAGE 2 · REVIEW PRINT ──────────────────────────────────────────────
	//
	// Three renderings of one design, because they answer three different
	// questions: the JSON is what was saved, the justification is what the
	// designer thought it was doing, and the CARD is the only one a person can
	// actually approve.
	page, err := subharness.Encode(harness)
	if err != nil {
		return err
	}
	section("stage 2 · the page")
	fmt.Print(string(page))

	section("stage 2 · the justification")
	fmt.Println(wrap(envelope.Justification, 78))

	section("stage 2 · the card")
	fmt.Println(subharness.Card(harness))

	section("stage 2 · how it would be reached")
	entry := entryOf(harness, envelope.Cues)
	score := subharness.Score(subharness.Turn{Text: goal}, entry)
	fmt.Printf("cues   %s\n", strings.Join(envelope.Cues, " · "))
	fmt.Printf("desc   %s\n", entry.Description)
	fmt.Printf("score  %.2f against the goal itself (threshold %.2f, exceeded) — %s\n",
		score, subharness.Threshold, offered(score))
	fmt.Println(conditionNotes(harness))

	if designOnly {
		return nil
	}

	// ── STAGE 3 · RUN ───────────────────────────────────────────────────────
	//
	// The page is SAVED first and then loaded back through the store, so the run
	// is against a page that survived a round trip through the format — and the
	// version in every trace is a real pointer rather than a draft.
	saved, err := store.Save(harness)
	if err != nil {
		return fmt.Errorf("save: %w", err)
	}
	section("stage 3 · run")
	fmt.Printf("saved  %s/%s/v%d.json\n\n", store.Dir(), saved.Id.Name, saved.Id.Version)

	env := &execModel{
		chat:     chat,
		goal:     goal,
		gate:     gate,
		maxTurns: maxTurns,
		log:      func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
	}
	runner := &subharness.Runner{Env: env, Loader: store, Saver: store}

	before := chat.calls
	began := time.Now()
	trace, runErr := runner.Run(ctx, saved, goal)
	elapsed := time.Since(began)

	path, saveErr := store.SaveRun(trace)
	if saveErr != nil {
		fmt.Printf("!! the trace could not be saved: %v\n", saveErr)
	}

	fmt.Println(subharness.RunCard(trace))
	fmt.Printf("\n%s · %d model calls · %s\n", elapsed.Round(time.Millisecond), chat.calls-before, chat.bill())
	if path != "" {
		fmt.Printf("trace  %s\n", path)
	}

	section("stage 3 · every step, whole")
	for _, entry := range trace.Trail {
		fmt.Printf("── %d %s (%s) · %s\n", entry.Step, entry.Id, entry.Kind, entry.Elapsed.Round(time.Millisecond))
		if entry.Err != "" {
			fmt.Printf("   ERROR %s\n", entry.Err)
		}
		if entry.Out != "" {
			fmt.Println(indent(entry.Out, "   "))
		}
		fmt.Println()
	}

	section("stage 3 · the run's answer")
	fmt.Printf("status %s", trace.Status)
	if trace.Spent > 0 {
		fmt.Printf(" · spent %d of a cap of %d", trace.Spent, saved.Dyn.Cap)
	}
	fmt.Println()
	if trace.Err != "" {
		fmt.Printf("err    %s\n", trace.Err)
	}
	fmt.Println()
	fmt.Println(firstOr(trace.Out, "(the run left nothing behind)"))
	return runErr
}

// conditionNotes says which of a program's conditions the small language decides
// and which are sentences somebody has to judge. It is printed in the review
// because the two cost different things at run time — one is free and identical
// on every machine, the other is a model call and a judgement.
func conditionNotes(h subharness.Harness) string {
	var lines []string
	for _, node := range h.Program.Nodes {
		var condition string
		switch node.Kind {
		case subharness.KindBranch:
			condition = node.Fields.Get("when")
		case subharness.KindLoopUntil:
			condition = node.Fields.Get("until")
		default:
			continue
		}
		how := "judged by the environment (a model call)"
		if subharness.ValidCondition(condition) == nil {
			how = "decided by the condition language (free, deterministic)"
		}
		lines = append(lines, fmt.Sprintf("cond   %s: %q — %s", node.Id, condition, how))
	}
	if len(lines) == 0 {
		return "cond   none — nothing in this program decides a path"
	}
	return strings.Join(lines, "\n")
}

func offered(score float64) string {
	if score > subharness.Threshold {
		return "the goal itself would raise the offer card"
	}
	return "the goal itself would NOT raise the offer card"
}

func pick(which string) []struct{ key, note, text string } {
	var out []struct{ key, note, text string }
	add := func(key, note, text string) {
		out = append(out, struct{ key, note, text string }{key, note, text})
	}
	lower := strings.ToLower(strings.TrimSpace(which))
	for _, goal := range goals {
		if lower == "all" || lower == goal.key {
			add(goal.key, goal.note, goal.text)
		}
	}
	if len(out) == 0 && strings.TrimSpace(which) != "" {
		add("own", "yours", which)
	}
	return out
}

// ── the printing ────────────────────────────────────────────────────────────

func rule() string { return strings.Repeat("─", 78) }

func head(title, goal string) {
	fmt.Printf("\n\n%s\n%s\n%s\n", strings.Repeat("═", 78), title, strings.Repeat("═", 78))
	fmt.Printf("%s\n\n", wrap(goal, 78))
}

func section(title string) {
	fmt.Printf("\n%s\n%s\n\n", title, strings.Repeat("─", len(title)))
}

func indent(text, with string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for at, line := range lines {
		lines[at] = with + line
	}
	return strings.Join(lines, "\n")
}

// wrap folds prose to a width on word boundaries, leaving the line breaks the
// writer put in where they are.
func wrap(text string, width int) string {
	var out []string
	for _, paragraph := range strings.Split(strings.TrimSpace(text), "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func die(reason string) {
	fmt.Fprintf(os.Stderr, "harness-design: %s\n", reason)
	os.Exit(2)
}
