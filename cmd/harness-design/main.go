// Command harness-design is a development rig, not a shipped surface.
//
// THE QUESTION IT ASKS: handed a plain goal and nothing else, can a MODEL
// architect a good sub-harness — and does the harness it wrote produce a good
// answer when it is actually run?
//
// That question is why nothing here is hand-written. A fixture somebody typed
// proves the runner works, which internal/subharness already proves in its own
// tests; what it cannot prove is that the DESIGN half is reachable by a model
// from words a person would say. So the rig has four stages and no shortcuts
// between them:
//
//	STAGE 1    DESIGN  the meta-guide (internal/subharness/prompts/designer.md,
//	                   rendered by designer.go) + the goal → a harness page, its
//	                   cues and its justification. Decoded and validated by
//	                   internal/subharness, retried with the error fed back.
//	STAGE 1.5  REVIEW  the same guide wearing PART FOUR reads the draft as a
//	                   critic — SPEED, COST, QUALITY — and hands back a revision.
//	                   The delta printed is COMPUTED from the two pages, not
//	                   taken from the critic's account of itself.
//	STAGE 2    PRINT   the page, the justification, and the CARD — the rendering
//	                   a person actually approves.
//	STAGE 3    RUN     the page executed against a model-backed Env
//	                   (execmodel.go), with the full trace and every step's whole
//	                   output printed, and the trace saved under
//	                   harnesses/<name>/run/<ts>.json.
//
// Usage:
//
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal all
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal hard
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal "your own sentence"
//	                     -model deepseek/deepseek-v4-flash -design-only -review=false
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
//
// The g-goals ask whether a design can be reached at all. The c-goals ask the
// harder question the meta-guide was written for: whether a REAL piece of
// engineering judgement — an architecture decision, a three-way comparison, a
// debugging plan — decomposes into a shape somebody would defend. g1 stays where
// it is as the regression that matters most: the guide teaches restraint, and a
// guide that turns three taglines into a mesh has failed at exactly the thing it
// spends the most words on.
var goals = []struct {
	key  string
	note string
	text string
}{
	{
		key:  "g1",
		note: "simple · the over-engineering regression",
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
	{
		key:  "c1",
		note: "hard · investigate, enumerate, model cost, refute, decide",
		text: "Assess whether we should rewrite our session journal as an append-only event log with snapshots — investigate the current design, enumerate failure modes, model migration cost, adversarially check the proposal, deliver a decision memo.",
	},
	{
		key:  "c2",
		note: "hard · three options, four dimensions, independent re-derivation",
		text: "Compare three approaches to adding voice input to a Go TUI (local whisper.cpp, cloud STT API, push-to-talk via an external app) across latency, cost, privacy, and implementation risk — with an independent re-derivation of the winner's score before deciding.",
	},
	{
		key:  "c3",
		note: "hard · hypothesis tree, an experiment each, ranked with falsification",
		text: "Given a flaky test report (intermittent nil pointer in a TUI render path, only under resize), build a hypothesis tree of causes, design a minimal experiment for each, and produce a ranked debugging plan with falsification criteria.",
	},
}

// rig is one configured run of this command. It exists because the three stages
// share the same client, the same store, the same two briefs and the same eight
// flags, and threading those through as parameters was already at the edge of
// readable before stage 1.5 added to it.
type rig struct {
	chat  *chatClient
	store *subharness.Store

	// The two briefs, built once so the whole of what each model turn was told
	// can be printed beside what it produced.
	designer string
	reviewer string

	retries      int
	review       bool
	designOnly   bool
	gate         string
	maxTurns     int
	designTokens int
	reviewTokens int
	temperature  float64
}

func main() {
	var (
		which        = flag.String("goal", "all", "g1|g2|g3|c1|c2|c3|all|easy|hard, or a goal of your own in quotes")
		model        = flag.String("model", "deepseek/deepseek-v4-flash", "the OpenRouter model that designs, reviews and runs")
		storeDir     = flag.String("store", "harnesses", "where pages and run traces land")
		retries      = flag.Int("retries", 2, "how many times a failed design or review is fed its own error back")
		review       = flag.Bool("review", true, "run stage 1.5, the design review")
		designOnly   = flag.Bool("design-only", false, "stop after stage 2")
		gate         = flag.String("gate", "approve", "what the absent person says at a human.gate: approve|decline|intervene")
		maxTurns     = flag.Int("max-turns", 4, "the clamp on one agent.loop's rounds, whatever the page asked for")
		designTokens = flag.Int("design-tokens", 16000, "the design turn's completion budget; a reasoning model spends most of it thinking")
		reviewTokens = flag.Int("review-tokens", 20000, "the review turn's budget: it writes findings AND the whole revised page")
		temp         = flag.Float64("temp", 0.3, "the design turn's temperature")
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

	// The briefs are rendered BEFORE the first request, because a guide whose
	// placeholders have drifted from this binary's machinery should cost nothing
	// and stop everything (prompts.Render) rather than reach a model.
	designer, err := designerSystem(availableTools)
	if err != nil {
		die("the designer guide will not render: " + err.Error())
	}
	reviewer, err := reviewSystem(availableTools)
	if err != nil {
		die("the reviewer guide will not render: " + err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := &rig{
		chat:         newChatClient(key, *model),
		store:        subharness.At(*storeDir),
		designer:     designer,
		reviewer:     reviewer,
		retries:      *retries,
		review:       *review,
		designOnly:   *designOnly,
		gate:         *gate,
		maxTurns:     *maxTurns,
		designTokens: *designTokens,
		reviewTokens: *reviewTokens,
		temperature:  *temp,
	}

	fmt.Printf("harness-design · model %s · store %s\n", *model, r.store.Dir())
	fmt.Printf("designer brief: %d bytes, %d lines\n", len(designer), strings.Count(designer, "\n")+1)
	if r.review {
		fmt.Printf("reviewer brief: %d bytes, %d lines\n", len(reviewer), strings.Count(reviewer, "\n")+1)
	} else {
		fmt.Printf("reviewer brief: not built — stage 1.5 is off\n")
	}

	failed := 0
	for _, goal := range chosen {
		if err := r.oneGoal(ctx, goal.key, goal.note, goal.text); err != nil {
			failed++
			fmt.Printf("\n!! %s ended in an error: %v\n", goal.key, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	fmt.Printf("\n%s\ntotal  %s\n", rule(), r.chat.bill())
	if failed > 0 {
		os.Exit(1)
	}
}

// oneGoal is the stages for one goal.
func (r *rig) oneGoal(ctx context.Context, key, note, goal string) error {
	head(fmt.Sprintf("%s · %s", key, note), goal)

	// ── STAGE 1 · DESIGN ────────────────────────────────────────────────────
	//
	// The history is kept across attempts on purpose. A designer that is shown
	// its own broken page AND the validator's sentence is being asked to repair
	// what it wrote; one that is only shown the error is being asked to guess
	// again, and the number this rig reports would be measuring the wrong thing.
	history := []message{
		{Role: "system", Content: r.designer},
		{Role: "user", Content: "THE GOAL:\n\n" + goal + "\n\nDesign the sub-harness for it."},
	}
	var (
		envelope design
		harness  subharness.Harness
		err      error
	)
	for attempt := 0; attempt <= r.retries; attempt++ {
		began := time.Now()
		var raw string
		envelope, harness, raw, err = designOnce(ctx, r.chat, history, r.designTokens, r.temperature)
		if err == nil {
			fmt.Printf("stage 1  design accepted on attempt %d/%d · %s · %d nodes · %s/%s · at most %d model calls\n",
				attempt+1, r.retries+1, time.Since(began).Round(time.Millisecond),
				len(harness.Program.Nodes), harness.Verify.Ladder, harness.Dyn.Ladder, estimateCalls(harness))
			break
		}
		fmt.Printf("stage 1  attempt %d/%d refused · %s\n         %v\n", attempt+1, r.retries+1, time.Since(began).Round(time.Millisecond), err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == r.retries {
			if raw != "" {
				fmt.Printf("\n         the last reply, verbatim:\n%s\n", indent(clip(raw, 4000), "         "))
			}
			return fmt.Errorf("no valid design in %d attempts: %w", attempt+1, err)
		}
		history = append(history,
			message{Role: "assistant", Content: raw},
			message{Role: "user", Content: "That harness was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."})
	}

	// ── STAGE 1.5 · DESIGN REVIEW ───────────────────────────────────────────
	//
	// A review that will not validate is NOT a failed goal. Stage 1.5 is an
	// improvement pass, not a gate, so a critic that cannot produce a legal page
	// loses its turn and the draft goes forward — loudly, because a rig that
	// silently skipped half of what it was measuring would report a number for
	// the wrong thing.
	if r.review {
		draft, draftHarness := envelope, harness
		revised, revisedHarness, err := r.reviewStage(ctx, goal, draft, draftHarness)
		if err != nil {
			fmt.Printf("stage 1.5 the review produced nothing usable, so the DRAFT goes forward · %v\n", err)
		} else {
			envelope, harness = revised.design(), revisedHarness
			r.printReview(draft, draftHarness, revised, revisedHarness)
		}
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
	fmt.Printf("calls  at most %d model calls, before any tool turn a worker takes\n", estimateCalls(harness))
	fmt.Println(conditionNotes(harness))

	if r.designOnly {
		return nil
	}

	// ── STAGE 3 · RUN ───────────────────────────────────────────────────────
	//
	// The page is SAVED first and then loaded back through the store, so the run
	// is against a page that survived a round trip through the format — and the
	// version in every trace is a real pointer rather than a draft.
	saved, err := r.store.Save(harness)
	if err != nil {
		return fmt.Errorf("save: %w", err)
	}
	section("stage 3 · run")
	fmt.Printf("saved  %s/%s/v%d.json\n\n", r.store.Dir(), saved.Id.Name, saved.Id.Version)

	env := &execModel{
		chat:     r.chat,
		goal:     goal,
		gate:     r.gate,
		maxTurns: r.maxTurns,
		log:      func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
	}
	runner := &subharness.Runner{Env: env, Loader: r.store, Saver: r.store}

	before := r.chat.calls
	began := time.Now()
	trace, runErr := runner.Run(ctx, saved, goal)
	elapsed := time.Since(began)

	path, saveErr := r.store.SaveRun(trace)
	if saveErr != nil {
		fmt.Printf("!! the trace could not be saved: %v\n", saveErr)
	}

	fmt.Println(subharness.RunCard(trace))
	fmt.Printf("\n%s · %d model calls (the page's estimate was at most %d) · %s\n",
		elapsed.Round(time.Millisecond), r.chat.calls-before, estimateCalls(saved), r.chat.bill())
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

// reviewStage is one critique-and-revision, retried on its own errors exactly as
// the design is.
//
// The critic is shown the draft AS JSON rather than as the card, because it is
// revising a page and a page is what it must hand back; and it is shown the
// draft's justification, because half of what is worth criticising is the
// reasoning rather than the shape.
func (r *rig) reviewStage(ctx context.Context, goal string, draft design, draftHarness subharness.Harness) (revision, subharness.Harness, error) {
	page, err := subharness.Encode(draftHarness)
	if err != nil {
		return revision{}, subharness.Harness{}, err
	}
	section("stage 1.5 · the draft, as it stands")
	fmt.Print(string(page))
	fmt.Println()

	history := []message{
		{Role: "system", Content: r.reviewer},
		{Role: "user", Content: strings.Join([]string{
			"THE GOAL:\n\n" + goal,
			"THE DRAFT'S CUES:\n\n" + strings.Join(draft.Cues, " · "),
			"THE DRAFT'S JUSTIFICATION:\n\n" + draft.Justification,
			"THE DRAFT HARNESS:\n\n" + string(page),
			"Review it and hand back the version that survives.",
		}, "\n\n")},
	}

	var (
		envelope revision
		harness  subharness.Harness
	)
	for attempt := 0; attempt <= r.retries; attempt++ {
		began := time.Now()
		var raw string
		envelope, harness, raw, err = reviewOnce(ctx, r.chat, history, r.reviewTokens, r.temperature)
		if err == nil {
			fmt.Printf("stage 1.5 review accepted on attempt %d/%d · %s · %d findings · %d nodes · %s/%s\n",
				attempt+1, r.retries+1, time.Since(began).Round(time.Millisecond),
				envelope.findings(), len(harness.Program.Nodes), harness.Verify.Ladder, harness.Dyn.Ladder)
			return envelope, harness, nil
		}
		fmt.Printf("stage 1.5 attempt %d/%d refused · %s\n          %v\n", attempt+1, r.retries+1, time.Since(began).Round(time.Millisecond), err)
		if ctx.Err() != nil {
			return revision{}, subharness.Harness{}, ctx.Err()
		}
		if attempt == r.retries {
			return revision{}, subharness.Harness{}, fmt.Errorf("no valid revision in %d attempts: %w", attempt+1, err)
		}
		history = append(history,
			message{Role: "assistant", Content: raw},
			message{Role: "user", Content: "That revision was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole review envelope again — one JSON object, no prose."})
	}
	return revision{}, subharness.Harness{}, err
}

// printReview puts the critic's three passes, its account of what it changed, and
// the delta this rig computed from the two pages next to each other.
//
// THE TWO ARE PRINTED SEPARATELY ON PURPOSE. `changed` is a model's report of its
// own work, which is the class of claim this whole system exists to distrust; the
// delta is read off the pages. Where they disagree, the disagreement is the
// finding, and it is named rather than reconciled.
func (r *rig) printReview(draft design, draftHarness subharness.Harness, revised revision, revisedHarness subharness.Harness) {
	section("stage 1.5 · the critique")
	for _, pass := range []struct {
		name     string
		findings []string
	}{
		{"speed", revised.Speed},
		{"cost", revised.Cost},
		{"quality", revised.Quality},
	} {
		if len(pass.findings) == 0 {
			fmt.Printf("%-8s nothing found\n", pass.name)
			continue
		}
		for at, finding := range pass.findings {
			label := pass.name
			if at > 0 {
				label = ""
			}
			fmt.Printf("%-8s · %s\n", label, indentRest(wrap(finding, 68), "           "))
		}
	}

	section("stage 1.5 · what the critic says it changed, and why")
	if len(revised.Changed) == 0 {
		fmt.Println("nothing — the critic says it kept the draft as it stands")
	}
	for _, change := range revised.Changed {
		fmt.Printf("· %s\n", indentRest(wrap(change, 74), "  "))
	}
	if revised.Calls.Draft != 0 || revised.Calls.Revised != 0 {
		fmt.Printf("\nthe critic counted %d model calls in the draft and %d in its revision\n",
			revised.Calls.Draft, revised.Calls.Revised)
	}

	section("stage 1.5 · the delta, computed from the two pages")
	d := diff(draftHarness, revisedHarness)
	switch {
	case d.empty() && len(revised.Changed) > 0:
		fmt.Println("the two pages are IDENTICAL — the critic listed changes it did not make")
	case d.empty():
		fmt.Println("the two pages are identical, which is what the critic said")
	default:
		line := func(label string, values ...string) {
			for at, value := range values {
				if at > 0 {
					label = ""
				}
				fmt.Printf("%-9s %s\n", label, value)
			}
		}
		line("nodes+", d.AddedNodes...)
		line("nodes-", d.RemovedNodes...)
		line("rekind", d.Rekinded...)
		line("fields", d.Rebriefed...)
		line("edges+", d.AddedEdges...)
		line("edges-", d.RemovedEdges...)
		if d.Verify != "" {
			line("verify", d.Verify)
		}
		if d.Dyn != "" {
			line("dyn", d.Dyn)
		}
		if d.Whitelist != "" {
			line("tools", d.Whitelist)
		}
		if d.Calls != "" {
			line("calls", d.Calls)
		}
		if d.Identity != "" {
			line("identity", d.Identity)
		}
		if len(revised.Changed) == 0 {
			fmt.Println("\nthe critic said it changed NOTHING and the pages differ — its account of its own work is wrong")
		}
	}
	if a, b := len(draft.Justification), len(revised.Justification); a != b {
		fmt.Printf("just.    %d → %d bytes\n", a, b)
	}
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

// pick resolves what to run. `easy` and `hard` are the two halves the goals table
// already describes, named so a whole tier can be run without typing three keys.
func pick(which string) []struct{ key, note, text string } {
	var out []struct{ key, note, text string }
	add := func(key, note, text string) {
		out = append(out, struct{ key, note, text string }{key, note, text})
	}
	lower := strings.ToLower(strings.TrimSpace(which))
	for _, goal := range goals {
		tier := "easy"
		if strings.HasPrefix(goal.key, "c") {
			tier = "hard"
		}
		if lower == "all" || lower == goal.key || lower == tier {
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

// indentRest indents every line but the first, which is what a hanging label
// wants: the label sits on line one and the wrap lines up under it.
func indentRest(text, with string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for at := 1; at < len(lines); at++ {
		lines[at] = with + lines[at]
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
