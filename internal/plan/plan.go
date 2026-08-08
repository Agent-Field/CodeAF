// Package plan builds and revises the task graph for a goal. It executes
// nothing; the graph is the product.
//
// The graph is generated through a spine of ordered stages and then immediately
// freed from it. Stages exist for two reasons, neither of which is scheduling.
// They keep generation cheap, because a stage can be fanned out knowing only
// the spine, so every stage expands at the same time. And they keep the result
// acyclic almost for free, because a generated dependency may only point at an
// earlier stage — or, in the single case where one part changes the material its
// siblings work on, at another node of the same stage, which is the one edge
// that is cycle-checked. Once the real edges are known, stage membership gates
// nothing: a node that needs no input starts immediately, whichever stage
// produced it.
//
// Four passes, each a single round no matter how large the graph gets:
//
//	spine    1 call     ordered stages — the only serial call in the system
//	fan-out  S calls    every stage split into simultaneous parts, at once
//	bind     ≤S calls   what each node reads or waits behind, and what duplicates what
//	audit    S-1 calls  what each node is missing — the counterweight to bind
//
// Bind and audit are deliberately opposed. Bind is written to resist the
// model's habit of turning any plan into a chain, so it under-connects; audit
// asks the opposite question and puts back only the edges whose absence would
// leave a node unable to finish. Neither framing is trustworthy alone.
package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// agentPremise is the one paragraph every planning prompt shares. It is stated
// once and reused verbatim because it is the single assumption the whole design
// rests on: the executors are agents, so none of the human-shaped structure a
// model reaches for by default — owners, phases, effort, sign-off — describes
// anything real here.
const agentPremise = `The work is done by AI agents. They are instant, free, and unlimited in number.
They start together, never talk to each other, and never see each other's work.
So there are no owners, roles, hand-offs, schedules, or budgets, and no
coordination, review, or status work. That is human overhead, not structure.`

// titleRule keeps the handle short. Titles are how a person and the model both
// address a node; the summary carries the meaning.
const titleRule = `Give a title of 1-3 words, like a short file name, distinct from the others.
Put the meaning in a one-line summary under 15 words, not in the title.`

// proportionRule is the size of the plan measured against the size of the ask.
//
// Every other guard here argues about where a boundary belongs once the work is
// already being divided. This one asks the question before that: how much of a
// plan does this deserve at all. It exists because the observed failure was not
// mis-drawn boundaries — it was ceremony, small asks arriving with a structure
// built for large ones, each part then earning its own verification round, and
// the user waiting on a graph to produce something one agent says in a
// paragraph. The structural caps downstream bound how far that can run; they
// cannot make the judgment, because by the time a cap fires the plan has already
// decided the work is big.
//
// It is stated as a burden of proof rather than a number. A threshold would be
// wrong at both ends — the same count is ceremony for one ask and thin for
// another — and asked plainly whether something could be decomposed, a model
// always says yes. Asked what each part buys, it can answer honestly.
//
// The last line is the one the verification spirals needed. Checking is part of
// doing the work, and a plan that hands it to a separate part has both invented
// a piece of work that produces nothing and taught the part that does the work
// that finishing is someone else's problem.
const proportionRule = `Make the plan exactly as large as the goal, and no larger. A large plan for a
small goal is not thoroughness; it is delay and expense the person asking pays
for, and it is the more common mistake by far.

Decomposition has to earn itself. Keep a division only when you can say what it
buys: parts that genuinely run at the same time, or a gate that genuinely blocks
what follows. When the honest answer is that the pieces would run one after
another anyway, or that one agent would simply do the whole thing, that is the
plan — a goal that asks for one finished thing is one piece of work by default,
and returning it whole is a correct answer rather than a failure to decompose.

Checking the work is part of doing it, never a piece of work of its own. Do not
add anything whose purpose is to look at, confirm, review, or verify what
another part produced; whoever produces a thing is who checks it.`

// Completer is the slice of the provider adapter this package needs. Depending
// on the method rather than the concrete client keeps the prompts testable
// without a network.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Usage is the running cost of a plan across every pass.
type Usage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	Cost             float64 `json:"cost"`
}

// Add folds one response's accounting in. A nil usage still counts the call:
// pretending a call did not happen because the provider was quiet about it
// would understate the plan.
func (u *Usage) Add(usage *ai.Usage) {
	u.Calls++
	if usage == nil {
		return
	}
	u.PromptTokens += usage.PromptTokens
	u.CompletionTokens += usage.CompletionTokens
	if details := usage.PromptTokensDetails; details != nil {
		u.CachedTokens += details.CachedTokens
	}
	if usage.Cost != nil {
		u.Cost += *usage.Cost
	}
}

func (u *Usage) merge(other Usage) {
	u.Calls += other.Calls
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.CachedTokens += other.CachedTokens
	u.Cost += other.Cost
}

// Report exposes the planner's diagnostic pass timings. It is deliberately
// separate from Progress: reports are operator telemetry, while progress is
// phrased for the person waiting on the work.
type Report func(pass string, elapsed time.Duration, detail string)

// Options configures a build.
type Options struct {
	// Recall is folded history relevant to this goal. Empty preserves the
	// pre-memory prompt byte for byte; populated memory is consumed only by the
	// ground pass, before parallel planning can reinterpret the goal.
	Recall []store.RecallHit

	// SpineSamples is how many spines to draw before choosing one. The spine is
	// the only call whose framing every later pass inherits, so it is the only
	// one worth sampling; the samples run concurrently and cost no wall clock.
	SpineSamples int

	// MaxDepth bounds recursion. It is the guard that matters most, because
	// depth is the only cost of decomposition that is genuinely serial — a
	// level costs four call-rounds however many nodes expand within it.
	MaxDepth int

	// NodeBudget is the hard ceiling the model cannot argue with. Every other
	// stop condition is pressure applied through a prompt; this one is
	// arithmetic, and it is what guarantees termination.
	NodeBudget int

	// Briefs turns on per-leaf instruction writing. It is opt-in because it
	// costs one call per leaf and only matters once something is going to
	// execute them.
	Briefs bool

	// Ensemble chooses between the two ways of spending parallelism: splitting
	// work by subject, or doing one judgment several times over independently
	// and merging. 0 lets the planner decide from the goal, -1 never asks, and
	// N >= 2 forces a panel of N. See ensemble.go.
	Ensemble int

	Report Report

	// Progress is called at pass boundaries. Nil keeps planning behavior and
	// output unchanged.
	Progress Progress

	// OnReady fires the instant a node is final and has nothing to wait for.
	// Those nodes are dispatchable at once — a linear harness could be running
	// them while the rest of the graph is still being planned — so the moment
	// we know is the moment worth telling someone. The node is passed by value:
	// the graph is still being appended to, and a pointer into it can go stale.
	OnReady func(node Node, elapsed time.Duration)
}

// Build produces the graph. Four call-rounds, whatever the size of the result.
func Build(ctx context.Context, client Completer, goal string, options Options) (*Graph, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, errors.New("goal is required")
	}
	report := options.Report
	if report == nil {
		report = func(string, time.Duration, string) {}
	}
	progress := serialProgress(options.Progress)
	start := time.Now()
	graph := &Graph{Goal: goal, NextID: 1}
	emitProgress(progress, "grounding", "settling what to look at", "")

	// Grounding and the spine both need only the goal, so they run together and
	// the grounding is free. It has to finish before the fan-out, though, and
	// that ordering is the point: the fan-out is where one decision would
	// otherwise get made independently several times over.
	var choice *SpineChoice
	var spineUsage, groundUsage Usage
	var spineErr, groundErr error
	var opening sync.WaitGroup
	opening.Add(2)
	go func() {
		defer opening.Done()
		// Both openers carry their fault out in the error the caller below
		// already reads: a faulted spine fails the build, as a failed one does,
		// and a faulted grounding is joined into the returned error while the
		// plan carries on without it.
		defer func() {
			if recovered := recover(); recovered != nil {
				choice, spineErr = nil, guard.Note("plan/build spine", recovered)
			}
		}()
		choice, spineUsage, spineErr = spineWithProgress(ctx, client, goal, options.SpineSamples, progress)
	}()
	go func() {
		defer opening.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				groundErr = guard.Note("plan/build ground", recovered)
			}
		}()
		grounding, usage, err := GroundWith(ctx, client, goal, options.Recall)
		groundUsage.Add(usage)
		graph.Settled, graph.Open, graph.Evidence, groundErr = grounding.Settled, grounding.Open, grounding.Evidence, err
	}()
	opening.Wait()
	if spineErr != nil {
		return nil, spineErr
	}
	graph.Stages = choice.Stages
	graph.Usage.merge(spineUsage)
	graph.Usage.merge(groundUsage)
	emitProgress(progress, "grounded", groundedSummary(graph.Settled), "")
	emitProgress(progress, "spine", plural(len(choice.Stages), "stage"), "")
	report("ground", time.Since(start), fmt.Sprintf("%s settled, %s open",
		plural(len(graph.Settled), "point"), plural(len(graph.Open), "question")))
	report("spine", time.Since(start), fmt.Sprintf("%s %s", plural(len(choice.Stages), "stage"), spreadLabel(choice)))

	// --- ensemble hook (ensemble.go) ---------------------------------------
	// Redundancy is the other way to spend parallelism, and it replaces
	// decomposition rather than refining it: if this goal is one judgment over
	// one body of material, everything below this line is the wrong shape for
	// it. It is decided here because grounding and the spine are both already
	// paid for and both feed the judgment.
	if ensemble, chosen, ensembleErr := ensembleHook(ctx, client, graph, options, report, start); chosen {
		return ensemble, errors.Join(groundErr, ensembleErr)
	}
	// --- end ensemble hook --------------------------------------------------

	nodes, fanUsage, fanErr := FanOut(ctx, client, graph.context(), choice.Stages)
	graph.Usage.merge(fanUsage)
	if len(nodes) == 0 {
		return nil, errors.Join(fanErr, errors.New("fan-out produced no nodes"))
	}
	for _, node := range nodes {
		graph.Add(node)
		emitProgress(progress, "fan-out", plural(len(graph.Nodes), "node"), nodeProgressTitle(node))
	}
	report("fan-out", time.Since(start), plural(len(graph.Nodes), "node"))

	// Binding and sizing read the same thing — the node catalog — and neither
	// reads what the other writes, so the size judgment is free in wall clock.
	// Only their calls overlap: each pass gathers concurrently and is applied
	// serially afterwards, because one pass writing node fields while the other
	// copies nodes to render its prompts is a data race.
	//
	// They are also given one render of the catalog block rather than one each.
	// It is the same ~8 KB of goal, premise and node list for both, it is what
	// the prefix cache keys on, and rendering it twice concurrently produced two
	// identical strings.
	shared := graph.planBlock()
	var bindResults []bindResult
	var sizeResults []sizeResult
	var passes sync.WaitGroup
	passes.Add(2)
	go func() {
		defer passes.Done()
		// A pass that faults before it returns leaves nothing behind, and
		// nothing is indistinguishable from "no stage was worth asking". So the
		// fault is appended as one failed result: the apply step below reads it
		// as a failure and the error reaches the caller.
		defer func() {
			if recovered := recover(); recovered != nil {
				bindResults = append(bindResults, bindResult{asked: true, err: guard.Note("plan/build bind", recovered)})
			}
		}()
		bindResults = bindGather(ctx, client, graph, shared)
	}()
	go func() {
		defer passes.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				sizeResults = append(sizeResults, sizeResult{err: guard.Note("plan/build size", recovered)})
			}
		}()
		sizeResults = sizeGather(ctx, client, graph, shared)
	}()
	passes.Wait()
	rendered := len(graph.Nodes)
	bindUsage, bindErr := bindApply(graph, bindResults)
	sizeUsage, sizeErr := sizeApply(graph, sizeResults)
	graph.Usage.merge(bindUsage)
	graph.Usage.merge(sizeUsage)
	emitProgress(progress, "sizing", fmt.Sprintf("%s — %s to split",
		plural(len(graph.Nodes), "node"), countLabel(len(selectForExpansion(graph, options)))), "")
	report("bind+size", time.Since(start), fmt.Sprintf("%s, %s", plural(graph.Edges(), "edge"), sizeSummary(graph)))

	// Stage 1 is settled already. Binding has run over it — the only edge it can
	// take is a sibling that changes what it works on — and audit skips it, since
	// it has no earlier stage to point at. So once sizing has run and we know
	// which nodes will be expanded, every stage-1 node that is not a candidate is
	// final. Announcing them here rather than at the end is most of the win:
	// they are also the nodes most likely to have no dependencies, which makes
	// them exactly the ones something could start on immediately.
	briefs := newBriefWriter(ctx, client, options.Briefs, progress)
	settled := map[int]bool{}
	pending := map[int]bool{}
	for _, id := range selectForExpansion(graph, options) {
		pending[id] = true
	}
	announce(graph, options, settled, briefs, start, func(node *Node) bool {
		return node.Stage == 1 && !pending[node.ID]
	})

	// Audit gets the same render again, but only when it still describes the
	// graph. Nothing since it was taken writes anything the catalog shows —
	// setNeeds and sizeApply touch needs, sizes and parts, none of which are
	// rendered — except folding a duplicate away, which removes a node. So the
	// node count is the whole test, and a fold means audit pays for its own.
	auditShared := shared
	if len(graph.Nodes) != rendered {
		auditShared = graph.planBlock()
	}
	added, auditUsage, auditErr := auditWith(ctx, client, graph, auditShared)
	graph.Usage.merge(auditUsage)
	emitProgress(progress, "audit", fmt.Sprintf("%s restored", plural(added, "link")), "")
	report("audit", time.Since(start), fmt.Sprintf("%s recovered", plural(added, "edge")))

	// Bind under-connects by design and audit only asks whether a node is
	// finishable — which a gathering node technically is, by redoing everything
	// itself. Neither judgment can be trusted to wire a late node that both
	// left empty, so the graph enforces it structurally before anything is
	// announced ready: nothing the spine placed after stage 1 may start at t=0.
	if forced := graph.anchorLateStarts(); forced > 0 {
		report("anchor", time.Since(start), fmt.Sprintf("%s forced", plural(forced, "edge")))
	}

	// With audit and the anchor done, no more edges will be added to anything
	// that already exists. Every node not queued for expansion has stopped
	// changing.
	announce(graph, options, settled, briefs, start, func(node *Node) bool {
		return !pending[node.ID]
	})

	// Recursion runs level by level. Each level decomposes everything worth
	// decomposing at once, so the loop turns as many times as the graph is
	// deep, not as many times as it is wide.
	for level := 0; level < options.MaxDepth; level++ {
		spliced, expandUsage, expandErr := ExpandLevel(ctx, client, graph, options)
		graph.Usage.merge(expandUsage)
		if expandErr != nil {
			auditErr = errors.Join(auditErr, expandErr)
		}
		if spliced == 0 {
			emitProgress(progress, "expand", "nothing else needs splitting", "")
			break
		}
		emitProgress(progress, "expand", fmt.Sprintf("%s split — %s total",
			plural(spliced, "node"), plural(len(graph.Nodes), "node")), "")
		report("expand", time.Since(start), fmt.Sprintf("%s split, %s total",
			plural(spliced, "node"), plural(len(graph.Nodes), "node")))

		// Children arrive already bound — they inherit their parent's inputs and
		// nothing later adds edges to them — so the only thing that could still
		// change one is being expanded again. Whatever is not queued for the next
		// level is finished, and waiting for the rest of the tree to settle
		// before saying so would hold back work that could already be running.
		next := map[int]bool{}
		for _, id := range selectForExpansion(graph, options) {
			next[id] = true
		}
		announce(graph, options, settled, briefs, start, func(node *Node) bool {
			return !next[node.ID]
		})
	}

	graph.Prune()
	emitProgress(progress, "steps", fmt.Sprintf("%d", len(graph.Nodes)), "")
	graph.addSynthesis()

	// Everything that was still moving has now stopped.
	announce(graph, options, settled, briefs, start, func(*Node) bool { return true })

	briefUsage, briefErr := briefs.apply(graph)
	graph.Usage.merge(briefUsage)
	if options.Briefs {
		report("brief", time.Since(start), plural(len(graph.Leaves()), "leaf"))
	}

	return graph, errors.Join(groundErr, fanErr, bindErr, sizeErr, auditErr, briefErr)
}

// announce settles every node matching the predicate: it starts that node's
// brief, and tells the caller about it if there is nothing left for it to wait
// for. It is idempotent through the settled set, so it can be called at several
// points as different parts of the graph stop changing.
func announce(graph *Graph, options Options, settled map[int]bool, briefs *briefWriter, start time.Time, ready func(*Node) bool) {
	// The catalog is snapshotted per call rather than per node. It is the frozen
	// shared prefix for every brief launched in this round, and re-rendering it
	// for each one would cost the cache hit that makes the round cheap. Who owns
	// the deliverable is snapshotted with it and for the same reason: it is one
	// question about the graph, the graph does not move inside this loop, and
	// asking it per node walked every node's needs once per node.
	var shared, label string
	var owner int
	if briefs.enabled {
		shared = graph.context() + "\nThe full plan:\n" + graph.briefCatalog()
		owner, label = graph.deliverableOwner()
	}
	elapsed := time.Since(start)
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if settled[node.ID] || node.Kind != KindWork || !ready(node) {
			continue
		}
		settled[node.ID] = true

		var inputs []string
		for _, need := range node.Needs {
			if source := graph.Node(need); source != nil {
				inputs = append(inputs, fmt.Sprintf("%q (%s)", source.Title, source.Summary))
			}
		}
		briefs.launch(shared, *node, inputs, deliverableLineFor(owner, label, node.ID))
		if len(node.Needs) == 0 && options.OnReady != nil {
			options.OnReady(*node, elapsed)
		}
	}
}

// sizeSummary reports the size distribution, which is the number to watch: a
// plan where everything is oversized means the sizing prompt has lost its
// anchor, and one where nothing ever is means it has lost its nerve.
func sizeSummary(graph *Graph) string {
	counts := map[Size]int{}
	for _, node := range graph.Nodes {
		if node.Kind == KindWork {
			counts[node.Size]++
		}
	}
	parts := make([]string, 0, 3)
	for _, size := range []Size{SizeOversized, SizeBorderline, SizeAtomic} {
		if counts[size] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[size], size))
		}
	}
	if len(parts) == 0 {
		return "no work nodes"
	}
	return strings.Join(parts, "/")
}

// serialProgress makes concurrent pass completions safe for callbacks that
// append to a journal or write to a stream. It also keeps count updates in the
// order their completion numbers were assigned.
func serialProgress(callback Progress) Progress {
	if callback == nil {
		return func(ProgressUpdate) {}
	}
	var mutex sync.Mutex
	return func(update ProgressUpdate) {
		mutex.Lock()
		defer mutex.Unlock()
		callback(update)
	}
}

// groundedSummary names the scope variable the grounding pass actually bound.
// Grounding commonly returns one sentence containing several concrete members
// ("The three cities are ..."), so counting result rows alone would hide the
// load-bearing number the user is waiting to learn.
func groundedSummary(settled []string) string {
	if len(settled) == 0 {
		return "scope already clear"
	}
	for _, point := range settled {
		fields := strings.Fields(point)
		for index, field := range fields {
			count, ok := cardinal(strings.Trim(field, ".,:;()[]{}\"'"))
			if !ok || index+1 >= len(fields) {
				continue
			}
			noun := strings.ToLower(strings.Trim(fields[index+1], ".,:;()[]{}\"'"))
			if noun != "" {
				return fmt.Sprintf("%d %s settled", count, noun)
			}
		}
	}
	return plural(len(settled), "scope decision") + " settled"
}

func cardinal(word string) (int, bool) {
	words := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	}
	if count, ok := words[strings.ToLower(word)]; ok {
		return count, true
	}
	var count int
	if _, err := fmt.Sscanf(word, "%d", &count); err == nil && count >= 0 {
		return count, true
	}
	return 0, false
}

func countLabel(count int) string {
	if count == 1 {
		return "1 node"
	}
	return fmt.Sprintf("%d nodes", count)
}

// structured performs one planning call and turns its reply into a Go value.
//
// Every pass in this package does the same three things — send, decode, and say
// whether what came back was usable — and the last of those is what anything
// downstream learns from. Doing it once here is what keeps the verdict honest: a
// pass that hand-rolled the sequence would sooner or later report a parse
// failure as a success, and nothing reading the verdict could tell.
//
// It reports only the two verdicts it can determine by itself. Whether a reply
// that parsed is actually *right* is a question only the caller can answer, so
// the slot is left open for it; a caller that never answers leaves the call
// unverified, which is the truth.
func structured(ctx context.Context, client Completer, messages []ai.Message, schema json.RawMessage, into any) (*ai.Response, error) {
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithSchema(schema))
	if err != nil {
		provider.Report(ctx, provider.VerdictProviderFailure)
		return nil, err
	}
	// A reasoning model can burn its entire completion budget thinking and
	// return no text at all: finish_reason=length with an empty body (seen in
	// the wild at completion_tokens=32768). One retry with a doubled budget is
	// the difference between a contract and a dead node; a second empty answer
	// is the model's problem, not the budget's.
	if strings.TrimSpace(response.Text()) == "" && finishedForLength(response) {
		retry, retryErr := client.CompleteWithMessages(ctx, messages,
			ai.WithSchema(schema), ai.WithMaxTokens(retryTokenBudget(response)))
		if retryErr == nil {
			response = retry
		}
	}
	if err := decodeJSON(response.Text(), into); err != nil {
		provider.Report(ctx, provider.VerdictFormatFailure)
		return response, annotate(err, response)
	}
	return response, nil
}

func finishedForLength(response *ai.Response) bool {
	return response != nil && len(response.Choices) > 0 &&
		response.Choices[0].FinishReason == "length"
}

// retryTokenBudget doubles what the truncated attempt actually spent, bounded
// so one pathological node cannot demand an absurd completion.
func retryTokenBudget(response *ai.Response) int {
	const ceiling = 96_000
	spent := 0
	if response != nil && response.Usage != nil {
		spent = response.Usage.CompletionTokens
	}
	budget := spent * 2
	if budget < 16_000 {
		budget = 16_000
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

// decodeJSON reads a structured reply. The fence stripping is defensive: strict
// schema should make it unnecessary, but a router that silently falls back to a
// provider without structured-output support would otherwise turn a good answer
// into a parse error.
func decodeJSON(text string, destination any) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return errors.New("empty response")
	}
	if strings.HasPrefix(trimmed, "```") {
		if start := strings.Index(trimmed, "\n"); start >= 0 {
			trimmed = trimmed[start+1:]
		}
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "```"))
	}
	if err := json.Unmarshal([]byte(trimmed), destination); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

// annotate turns an unusable reply into a diagnosable one. A truncated answer
// and a refused one both arrive as empty text, and only the finish reason tells
// them apart — on a reasoning model the usual cause is the whole token budget
// being spent thinking, which the completion count makes obvious.
func annotate(err error, response *ai.Response) error {
	if response == nil || len(response.Choices) == 0 {
		return err
	}
	detail := fmt.Sprintf("finish_reason=%s", response.Choices[0].FinishReason)
	if usage := response.Usage; usage != nil {
		detail += fmt.Sprintf(" completion_tokens=%d", usage.CompletionTokens)
	}
	return fmt.Errorf("%w (%s)", err, detail)
}

func usageOf(response *ai.Response) *ai.Usage {
	if response == nil {
		return nil
	}
	return response.Usage
}

func joinErrors(errs []error) error {
	var kept []error
	for _, err := range errs {
		if err != nil {
			kept = append(kept, err)
		}
	}
	return errors.Join(kept...)
}

// spreadLabel makes the sampling visible. When the samples disagreed on stage
// count, that disagreement is the most useful thing we learned about the goal:
// it says the shape of the plan was genuinely ambiguous, and a single-sample
// run would have committed to one reading without ever knowing.
func spreadLabel(choice *SpineChoice) string {
	if choice.Samples < 2 {
		return ""
	}
	if choice.Agreed {
		return fmt.Sprintf("(%d samples agreed)", choice.Samples)
	}
	return fmt.Sprintf("(%d samples: %s)", choice.Samples, joinInts(choice.Spread))
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	if noun == "leaf" {
		return fmt.Sprintf("%d leaves", count)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// RunID is a stable identifier for a goal, used to name a run's workspace so
// that re-running the same goal lands in the same directory.
func RunID(goal string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(goal)))
	return "aforge-" + hex.EncodeToString(sum[:8])
}

func trim(value string) string { return strings.TrimSpace(value) }

func userMessage(text string) ai.Message {
	return ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

func systemMessage(text string) ai.Message {
	return ai.Message{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: text}}}
}
