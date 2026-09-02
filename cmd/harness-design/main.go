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
//	                   cues and its justification. Salvaged, decoded and validated
//	                   by internal/subharness, repaired once, then retried with
//	                   the error fed back.
//	STAGE 1.5  REVIEW  the same guide wearing PART FOUR reads the draft as a
//	                   critic — SPEED, COST, QUALITY — and hands back a PATCH.
//	                   The ops are applied to the draft this rig already parsed,
//	                   so the critic never retypes what it is not changing, and
//	                   the delta printed is the patch itself.
//	STAGE 2    PRINT   the page, the justification, and the CARD — the rendering
//	                   a person actually approves.
//	STAGE 3    RUN     the page executed against a model-backed Env
//	                   (execmodel.go), with the full trace and every step's whole
//	                   output printed, and the trace saved under
//	                   harnesses/<name>/run/<ts>.json.
//
// AND A SECOND RIG BEHIND THE SAME BINARY. `-orchestrate` runs none of the
// above: it asks the opposite question — what if nobody designs a graph at all,
// and a planner amends a live frontier on every completion? That is
// internal/orchestrate, and orchestrate.go is the whole of it. The two share
// this file's transport, salvage ladder and printing, and nothing else.
//
// Usage:
//
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal all
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal hard
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -goal "your own sentence"
//	                     -model deepseek/deepseek-v4-flash -design-only -review=false
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -orchestrate -goal all -fuel 1.50
//	OPENROUTER_API_KEY=… go run ./cmd/harness-design -repro 5 -belt chat
//	                     -model deepseek/deepseek-v4-pro -goal "…"
//
// AND A THIRD, WHICH IS AN INSTRUMENT RATHER THAN A QUESTION. `-repro N` runs
// stage 1 N times and tallies what refused it — see repro.go, and the reason it
// had to exist.
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

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
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
}

func main() {
	var (
		which      = flag.String("goal", "all", "g1|g2|g3|c1|c2|c3|all|easy|hard, or a goal of your own in quotes")
		model      = flag.String("model", "deepseek/deepseek-v4-flash", "the OpenRouter model that designs, reviews and runs")
		storeDir   = flag.String("store", "harnesses", "where pages and run traces land")
		retries    = flag.Int("retries", 2, "how many times a failed design or review is fed its own error back")
		review     = flag.Bool("review", true, "run stage 1.5, the design review")
		designOnly = flag.Bool("design-only", false, "stop after stage 2")
		gate       = flag.String("gate", "approve", "what the absent person says at a human.gate: approve|decline|intervene")
		maxTurns   = flag.Int("max-turns", 4, "the clamp on one agent.loop's rounds, whatever the page asked for")
		// 8000 was measured against models that answer straight, and it is the
		// number that made this pipeline look broken. Rendered against the CHAT's
		// belt, deepseek-v4-flash spent the WHOLE of it thinking and answered
		// with nothing — twice in three attempts — which is the failure
		// openrouter.go names and which reads to a person as "the design keeps
		// failing". It matches internal/session's harnessDesignTokens on purpose:
		// a rig whose budget is half the shipped one measures a different program.
		designTokens = flag.Int("design-tokens", 16000, "the design turn's completion budget; a reasoning model draws its thinking from this too")
		// 6000 was the budget when the critic had three checklists to run. It has
		// two duties as well now, and on a reasoning model the thinking is drawn
		// from this same budget: a hard draft came back three times at
		// finish=length, having spent 6001 tokens and emitted no reply at all. A
		// review that cannot afford its own answer is the most expensive kind of
		// nothing — the design turn is already paid for by then.
		reviewTokens = flag.Int("review-tokens", 10000, "the review turn's budget: the critic's thinking, its findings and the ops patch — never the whole page")

		// The ADAPTIVE RUN is a different rig behind the same binary: no design,
		// no page, no store — a planner amending a live frontier. Its flags are
		// grouped here rather than in a second command because it shares the
		// transport, the salvage ladder and every printing helper above.
		adaptive    = flag.Bool("orchestrate", false, "run the ADAPTIVE RUN instead of the design stages: internal/orchestrate's planner against a live frontier")
		fuel        = flag.Float64("fuel", 2.00, "the adaptive run's whole tank per goal, in dollars — every model call meters against it, the planner's included")
		planTokens  = flag.Int("plan-tokens", 6000, "the planner turn's completion budget")
		nodeTokens  = flag.Int("node-tokens", 4000, "one node's completion budget — a node that hits it loses the DIGEST it writes last")
		maxParallel = flag.Int("max-parallel", 8, "the clamp on how many nodes this rig will have in flight at once")
		fuelGate    = flag.String("fuel-gate", "finish", "what the absent person answers when the tank empties: finish|stop")

		// THE REPRODUCTION is a third mode, and it is the one that exists because
		// this binary went blind (repro.go). It runs stage 1 and nothing else, N
		// times, against the belt a CHAT hands a harness — so a refusal a person
		// hits in conversation can be counted here rather than argued about.
		repro = flag.Int("repro", 0, "run the design stage this many times and tally what refused it, instead of the stages")
		belt  = flag.String("belt", "rig", "whose tools the guide is rendered against: rig (three that cannot fail) or chat (what a conversation hands a harness)")
	)
	flag.Parse()

	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		die("OPENROUTER_API_KEY is not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The adaptive run shares nothing above stage 0 with the design stages — no
	// designer guide, no store, no page — so it branches before either is built.
	if *adaptive {
		os.Exit(orchestrateMode(ctx, newChatClient(key, *model), *which, *model, driver{
			fuel:        orchestrate.Fuel{Cap: *fuel},
			planTokens:  *planTokens,
			nodeTokens:  *nodeTokens,
			maxParallel: *maxParallel,
			gate:        *fuelGate,
		}))
	}

	chosen := pick(*which)
	if len(chosen) == 0 {
		die("no goal to run")
	}

	tools, err := beltNamed(*belt, ".")
	if err != nil {
		die(err.Error())
	}
	// THE CHAT BELT IS RENDERED, NEVER RUN. Its tools are pi's real read, write
	// and bash over this machine, and stage 3 executing them would be the rig
	// editing the repository it is measuring. So the belt that reproduces a chat
	// is allowed only where nothing runs.
	if *repro == 0 && !*designOnly && !sameBelt(tools, availableTools) {
		die("-belt chat is design-only: pass -repro N or -design-only, because stage 3 would run those tools for real")
	}

	// The briefs are rendered BEFORE the first request, because a guide whose
	// placeholders have drifted from this binary's machinery should cost nothing
	// and stop everything (prompts.Render) rather than reach a model.
	designer, err := designerSystem(tools)
	if err != nil {
		die("the designer guide will not render: " + err.Error())
	}
	reviewer, err := reviewSystem(tools)
	if err != nil {
		die("the reviewer guide will not render: " + err.Error())
	}

	// The repro branches here: it has the guide it needs and wants none of the
	// store, the review or the run.
	if *repro > 0 {
		fmt.Printf("harness-design · repro · model %s · belt %s · %d trials\n", *model, *belt, *repro)
		os.Exit(reproMode(ctx, newChatClient(key, *model), designer, chosen, *repro, *designTokens, *retries))
	}

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
	for tries := 0; tries <= r.retries; tries++ {
		began := time.Now()
		var at attempt
		envelope, harness, at, err = designOnce(ctx, r.chat, history, r.designTokens)
		if err == nil {
			fmt.Printf("stage 1  design accepted on attempt %d/%d · %s%s · %d nodes · %s/%s · at most %d model calls\n",
				tries+1, r.retries+1, time.Since(began).Round(time.Millisecond), at.cost(),
				len(harness.Program.Nodes), harness.Verify.Ladder, harness.Dyn.Ladder, estimateCalls(harness))
			break
		}
		fmt.Printf("stage 1  attempt %d/%d refused · %s%s\n         %v\n", tries+1, r.retries+1, time.Since(began).Round(time.Millisecond), at.cost(), err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if tries == r.retries {
			if at.raw != "" {
				fmt.Printf("\n         the last reply, verbatim:\n%s\n", indent(clip(at.raw, 4000), "         "))
			}
			return fmt.Errorf("no valid design in %d attempts: %w", tries+1, err)
		}
		history = append(history,
			message{Role: "assistant", Content: at.raw},
			message{Role: "user", Content: "That harness was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."})
	}

	// ── STAGE 1.5 · DESIGN REVIEW ───────────────────────────────────────────
	//
	// A review whose patch will not validate is NOT a failed goal. Stage 1.5 is an
	// improvement pass, not a gate, so a critic that cannot produce a legal page
	// loses its turn and the draft goes forward — loudly, because a rig that
	// silently skipped half of what it was measuring would report a number for
	// the wrong thing.
	if r.review {
		draft := envelope
		revised, revisedHarness, applied, err := r.reviewStage(ctx, goal, draft, harness)
		if err != nil {
			fmt.Printf("stage 1.5 the review produced nothing usable, so the DRAFT goes forward · %v\n", err)
		} else {
			envelope, harness = revised.design(draft), revisedHarness
			r.printReview(draft, revised, applied, revisedHarness)
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

	before, _, _ := r.chat.spent()
	began := time.Now()
	trace, runErr := runner.Run(ctx, saved, goal)
	elapsed := time.Since(began)

	path, saveErr := r.store.SaveRun(trace)
	if saveErr != nil {
		fmt.Printf("!! the trace could not be saved: %v\n", saveErr)
	}

	after, _, _ := r.chat.spent()
	fmt.Println(subharness.RunCard(trace))
	fmt.Printf("\n%s · %d model calls (the page's estimate was at most %d) · %s\n",
		elapsed.Round(time.Millisecond), after-before, estimateCalls(saved), r.chat.bill())
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

// reviewStage is one critique-and-patch, retried on its own errors exactly as the
// design is.
//
// The critic is shown the draft AS JSON rather than as the card, because it is
// patching a page and the node ids its ops name are on that page; and it is shown
// the draft's justification, because half of what is worth criticising is the
// reasoning rather than the shape.
func (r *rig) reviewStage(ctx context.Context, goal string, draft design, draftHarness subharness.Harness) (revision, subharness.Harness, []subharness.OpResult, error) {
	page, err := subharness.Encode(draftHarness)
	if err != nil {
		return revision{}, subharness.Harness{}, nil, err
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
			"Review it and reply with your findings and the ops that answer them.",
		}, "\n\n")},
	}

	var (
		envelope revision
		harness  subharness.Harness
		applied  []subharness.OpResult
	)
	for tries := 0; tries <= r.retries; tries++ {
		began := time.Now()
		var at attempt
		envelope, harness, applied, at, err = reviewOnce(ctx, r.chat, history, r.reviewTokens, draft, draftHarness)
		if err == nil {
			fmt.Printf("stage 1.5 review accepted on attempt %d/%d · %s%s · %d findings · %d ops (%d applied) · %d nodes · %s/%s\n",
				tries+1, r.retries+1, time.Since(began).Round(time.Millisecond), at.cost(),
				envelope.findings(), len(applied), landed(applied),
				len(harness.Program.Nodes), harness.Verify.Ladder, harness.Dyn.Ladder)
			return envelope, harness, applied, nil
		}
		fmt.Printf("stage 1.5 attempt %d/%d refused · %s%s\n          %v\n", tries+1, r.retries+1, time.Since(began).Round(time.Millisecond), at.cost(), err)
		if ctx.Err() != nil {
			return revision{}, subharness.Harness{}, nil, ctx.Err()
		}
		if tries == r.retries {
			return revision{}, subharness.Harness{}, nil, fmt.Errorf("no valid revision in %d attempts: %w", tries+1, err)
		}
		history = append(history,
			message{Role: "assistant", Content: at.raw},
			message{Role: "user", Content: "That patch was REFUSED:\n\n" + err.Error() +
				"\n\nFix exactly that and reply with the whole review envelope again — one JSON object, no prose. " +
				"The ops are applied to the ORIGINAL draft every time, so send the whole patch, not the difference from your last one."})
	}
	return revision{}, subharness.Harness{}, nil, err
}

// printReview puts the critic's findings and the patch that answers them next to
// each other.
//
// THE PATCH IS THE DELTA. There is nothing to compute and nothing to distrust: an
// op is not a model's account of a change, it is the change. What this print does
// add is the fate of each one — an op that named a node nobody declared is skipped
// rather than fatal, and a review that landed six of nine edits should say so.
func (r *rig) printReview(draft design, revised revision, applied []subharness.OpResult, revisedHarness subharness.Harness) {
	section("stage 1.5 · the critique")
	for _, pass := range revised.byPass() {
		if len(pass.Found) == 0 {
			fmt.Printf("%-8s nothing found\n", pass.Name)
			continue
		}
		for at, found := range pass.Found {
			label := pass.Name
			if at > 0 {
				label = ""
			}
			fmt.Printf("%-8s · %s\n", label, indentRest(wrap(found, 68), "           "))
		}
	}

	section("stage 1.5 · the patch, which IS the delta")
	if len(applied) == 0 {
		fmt.Println("no ops — the critic kept the draft as it stands")
	}
	for _, result := range applied {
		if result.Applied() {
			fmt.Printf("✓ %s\n", indentRest(wrap(result.Op.String(), 74), "  "))
			continue
		}
		fmt.Printf("✗ %s\n  SKIPPED %s\n", indentRest(wrap(result.Op.String(), 74), "  "),
			indentRest(wrap(result.Err.Error(), 66), "          "))
	}
	if skipped := len(applied) - landed(applied); skipped > 0 {
		fmt.Printf("\n%d of %d ops were skipped — the rest of the patch still landed\n", skipped, len(applied))
	}
	if revised.Calls.Draft != 0 || revised.Calls.Revised != 0 {
		fmt.Printf("\nthe critic counted %d model calls in the draft and %d after its patch\n",
			revised.Calls.Draft, revised.Calls.Revised)
	}
	switch {
	case strings.TrimSpace(revised.Justification) == "" && landed(applied) > 0:
		fmt.Printf("\nthe critic patched the page and did NOT restate the justification, so the draft's stands\n")
	case strings.TrimSpace(revised.Justification) != "":
		fmt.Printf("\njust.    %d → %d bytes\n", len(draft.Justification), len(revised.Justification))
	}
	if len(revised.Cues) > 0 {
		fmt.Printf("cues     %s → %s\n", strings.Join(draft.Cues, " · "), strings.Join(revised.Cues, " · "))
	}

	section("stage 1.5 · where a failed check goes")
	fmt.Println(verifyPaths(revisedHarness))
}

// landed counts the ops that did what they said.
func landed(results []subharness.OpResult) int {
	total := 0
	for _, result := range results {
		if result.Applied() {
			total++
		}
	}
	return total
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

// verifyPaths says, for every verify node in the page the review is handing on,
// what runs when that check says FAIL. It is MECHANICAL — successors and kinds,
// nothing read and nothing judged — because the defect it names is mechanical:
// a check whose failure has nowhere to go cannot change what happens next, so
// the run ends on the verdict with all the work before it done and paid for.
//
// It is printed after the patch rather than instead of it. The critic owes a
// finding on every verify (PART FOUR's second duty), and this line is how a
// reader sees whether the finding matched the page — a review that argued
// death-is-the-answer and a page that strands its verify agree; a review that
// said nothing and a page that strands its verify is the failure this exists to
// surface.
func verifyPaths(h subharness.Harness) string {
	var lines []string
	for _, node := range h.Program.Nodes {
		if node.Kind != subharness.KindVerify {
			continue
		}
		successors := h.Program.Successors(node.Id)
		fork := ""
		for _, id := range successors {
			next, ok := h.Program.Node(id)
			if ok && next.Kind == subharness.KindBranch {
				fork = fmt.Sprintf("forked at %q on %q", next.Id, next.Fields.Get("when"))
				break
			}
		}
		switch {
		case fork != "":
			lines = append(lines, fmt.Sprintf("verify %s: %s", node.Id, fork))
		case len(successors) == 0:
			lines = append(lines, fmt.Sprintf("verify %s: STRANDED — it is a last node, so a FAIL ends the run with the work done", node.Id))
		default:
			lines = append(lines, fmt.Sprintf("verify %s: STRANDED — %s runs whether it passed or failed", node.Id, strings.Join(quoted(successors), " and ")))
		}
	}
	if len(lines) == 0 {
		return "verify none — this program checks nothing of its own"
	}
	return strings.Join(lines, "\n")
}

func quoted(ids []string) []string {
	out := make([]string, len(ids))
	for at, id := range ids {
		out[at] = fmt.Sprintf("%q", id)
	}
	return out
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
