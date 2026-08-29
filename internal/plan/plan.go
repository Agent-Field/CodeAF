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
	"sort"
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
//
// Its second paragraph is the correction W6 was written for. The premise used
// to say the workers were "instant, free, and unlimited in number", which is
// true of exactly one of the three axes and false on the other two: an agent is
// not instant, and it is certainly not free, because every branch re-pays
// whatever context it must be given before it can start. A planner told the
// workers are free has no reason to prefer any plan over a larger one, and the
// measured bills say the prompt side is where the money goes. So the denial is
// replaced by the honest trade — width is bought with context and paid back in
// waiting — and it is stated here, once, because every pass that can add work
// reads this paragraph.
//
// Its last sentence is the other half of the same correction, and it is the one
// that makes the trade decidable rather than merely stated. Three quantities are
// being spent by every division — the wait, the money, the quality of what comes
// back — and a prompt that names them separately invites a planner to improve
// one and call the decision made; every audited over-decomposition did exactly
// that, buying wall time nobody was waiting for at a cost nobody counted. So
// they are named once, as a single objective, and the sentence points at the
// prices rather than restating a rule: on this machine those three quantities
// are measured, the measurements ride the tail of this very call when they
// exist (see invoice.go), and a division that cannot be paid for out of them is
// a division nobody has a reason for. It says "when they are there" and not
// "always", because on a fresh machine they are not there, and a premise that
// demanded arithmetic against numbers nobody has would be asking the planner to
// invent them.
const agentPremise = `The work is done by AI agents. They can be started in any number and they start
together; they never talk to each other and never see each other's work. So
there are no owners, roles, hand-offs, schedules, or budgets, and no
coordination, review, or status work. That is human overhead, not structure.

What they cost is not time. It is the context each one must be given, and a
piece of work split in two costs whatever the second piece must be told that the
first was already told. A split that shares almost everything is nearly free; a
split that has to re-explain the whole subject twice is not. What the person
waits for is the longest chain, never the total — so width is worth buying, and
worth buying only where the pieces genuinely do not need the same thing said
twice.

The wait, the money and the quality of the answer are one thing being spent
together and never three things to trade against each other, so where measured
figures for this machine are given to you, a division has to pay for itself on
those figures rather than on how well it reads.`

// workerPremise is what the thing on the other side of a prompt actually is.
//
// Six prompts described it in prose and no two agreed, which matters because
// every one of them is writing *for* that worker: an instruction, a working
// method, a ruler, a repair. One of the six had it plainly wrong — it believed
// the worker held four tools — and nothing could catch that, because there was
// nothing for it to disagree with.
//
// It is deliberately the shared fact and only the shared fact. Sites whose
// wording is doing work of their own keep theirs and point here: sizing
// (size.go) needs the worker's serial capacity because capacity is what it
// measures, the ruler (recalibrate.go) needs the same in order to say whose
// envelope is being redrawn, the sentinel (revise.go) states it as a bound on
// what a new node may be, and the leaf's own system prompt (exec/linear.go)
// says it in the second person to the worker itself. Flattening those would
// cost the specialisation, not buy the sharing.
const workerPremise = `That agent works alone, in order, with tools. It cannot ask anyone anything and
nobody will follow up with it, so whatever it is handed is all it gets.`

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
// checkingRule is the paragraph the verification spirals needed, lifted out of
// proportionRule so that the passes which build a plan are not the only ones
// that state it.
//
// It was written for decomposition and it was true of every pass that can add
// work: a plan that hands checking to a separate part has both invented a piece
// of work that produces nothing and taught the part that does the work that
// finishing is someone else's problem. The sentinel could add exactly that node
// mid-run, one round at a time, and did — which is the doctrine gap under the
// 27-round spiral the governors could only bound.
//
// The extraction is byte-preserving by construction: proportionRule is the same
// literal with this paragraph spliced back in at the position it always held,
// and a test reads both against the original bytes.
const checkingRule = `Checking the work is part of doing it, never a piece of work of its own. Do not
add anything whose purpose is to look at, confirm, review, or verify what
another part produced; whoever produces a thing is who checks it.`

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

` + checkingRule + `

Judging one artifact is one part, whatever its length. The sections of a thing
under judgment are not independent items, because what the judgment exists to
catch lives in the cross-references between them — a figure in one section
contradicting a claim in another is invisible to a reader who was handed only
one of them. Divide repeated operations over items that stand alone; never
divide one act of comprehension.`

// verdictRule is the meta-judgment the qualitative guards above do not make.
//
// proportionRule asks whether the plan deserves to exist at all, checkingRule
// forbids a node that exists to look at another, and the subject test forbids
// phases passed off as parts. None of them states the ledger test a division
// has to pass once it is past all of those: that each part buys back, in
// parallel time saved, more than the fixed cost a worker pays before it
// produces, that no deliverable is sacrificed to buy width, that two parts
// covering the same ground are one, and that the verdict is read off the
// accounting rather than off how the split reads. It rides the same two prompts
// proportionRule does, at their end, because those are where a division is
// actually decided.
const verdictRule = `A split is valid only when each part's expected parallel saving exceeds the
fixed cost a worker pays before it produces — the orientation, setup, and
delivery it owes before any work leaves it. A part that does not pay back that
cost is the same wait made more expensive, not a saving.

Do not drop a deliverable to buy parallelism. If a thing cannot be divided
without losing the whole, the whole is the part: its pieces would not stand
alone, and one of them would then owe the whole anyway.

Duplicate work is not parallelism. Two parts that each re-read or re-explain the
same subject are one part charged twice; a split counts only when each part
works on material the others do not touch.

Judge a part against the ledger, not against how reasonable the split sounds.
What decides it is what the part costs to run and what it produces — a division
that reads well but shortens no wait and lightens no one worker's load is a
division nobody owed.`

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
	// Through the accessor rather than the nesting: cache reads come back as
	// prompt_tokens_details.cached_tokens from OpenAI-shaped endpoints and as
	// cache_read_input_tokens from Anthropic-family ones, and reading only the
	// first recorded a flat zero for every plan call made against the second.
	u.CachedTokens += usage.CacheReadTokens()
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

	// Terrain is what the run's workspace holds, rendered by the caller with
	// RenderTerrain before the build starts. Empty is the whole of the
	// compatibility story: a caller with no workspace sends the prompt bytes it
	// has always sent.
	//
	// The caller renders it, not this package, and renders it exactly once. This
	// block joins the frozen preamble that every fan-out, bind, size, audit and
	// brief call shares, so re-reading the directory mid-build — where a worker
	// may already be writing into it — would change the prefix under passes that
	// are still running, cost every cache hit behind it, and leave two calls
	// planning from two different pictures of the same workspace. It is a
	// snapshot taken at build start and frozen for the build.
	Terrain string

	// Asked are the separable requests the caller's reading of the ask found in
	// it, in the person's own words. Fewer than two is the ordinary ask and
	// changes no prompt byte anywhere.
	//
	// It is handed to the build rather than acted on by the caller, and that is
	// the whole of this field. A caller that acts on it is a second planner
	// with one shape in it: it can lay the requests side by side, and it cannot
	// answer the one question a flat layout destroys — whether one of them is
	// written over what the others produce. Here the reading reaches the passes
	// whose job that question already is. See Graph.Asked.
	Asked []string

	// SpineSamples is how many spines to draw before choosing one. The spine is
	// the only call whose framing every later pass inherits, so it is the only
	// one worth sampling; the samples run concurrently and cost no wall clock.
	SpineSamples int

	// MaxDepth bounds recursion. It is the guard that matters most, because
	// depth is the only cost of decomposition that is genuinely serial — a
	// level costs four call-rounds however many nodes expand within it.
	MaxDepth int

	// BuildDepth is how many of those levels the *build* runs for itself, for
	// a caller that intends to carry the rest later.
	//
	// Zero means MaxDepth, which is every caller that has ever existed and is
	// the whole of the rollback: a build asked nothing new answers exactly as
	// it did. A scheduler that expands at claim time sets it low and keeps
	// MaxDepth where it was — the difference between the two is not a smaller
	// graph, it is the same depth decided later, against dependencies that have
	// landed instead of against their titles. Every level the build skips is
	// one it would have decided at t=0 with the least information it will ever
	// have, and one whose four serial call-rounds it would have made the person
	// wait through before the first leaf started.
	BuildDepth int

	// NodeBudget is the hard ceiling the model cannot argue with. Every other
	// stop condition is pressure applied through a prompt; this one is
	// arithmetic, and it is what guarantees termination.
	NodeBudget int

	// Briefs turns on per-leaf instruction writing. It is opt-in because it
	// costs one call per leaf and only matters once something is going to
	// execute them.
	Briefs bool

	// FileShaped carries the delivery-law bit onto the graph, for the case where
	// briefs are written inside the build and the caller never sees the graph
	// before they are. See Graph.FileShaped and DeliveryLaw.
	FileShaped bool

	// Continues says this plan is the remainder of work that already happened.
	// See Graph.Continues.
	Continues bool

	// Records are the files the work this plan continues left behind, which the
	// agents it plans can open and read. See Graph.Records.
	Records []string

	// Undivided stops the build at the spine when the spine says there is
	// nothing to divide. It exists for the remainder path and it is opt-in
	// because it is the wrong answer for a fresh project: a one-stage project
	// still has parallel parts inside that stage, and finding them is the
	// point.
	//
	// A remainder is different in kind. It is what is left of one leaf's
	// assignment after a reviewer named a gap, so it is by construction
	// smaller than work one agent was already given. Measured: planning one
	// such remainder — "run pytest and show the output" — cost 13,828 prompt
	// tokens across seven planner passes, and another cost 23,964. That is
	// five to eight times the entire structuring cost of the original job, to
	// produce a graph of one node.
	//
	// The judgement stays the model's and the structure stays the code's: the
	// spine call already reads the goal and already answers "one stage" when
	// the goal has no real gate — its prompt says so in as many words, and
	// calls that a correct and common answer. When it says one, this stops
	// there and hands back one worker. When it says more, the full pipeline
	// runs exactly as before. Nothing matches a phrase against anything.
	Undivided bool

	// Ensemble chooses between the two ways of spending parallelism: splitting
	// work by subject, or doing one judgment several times over independently
	// and merging. 0 lets the planner decide from the goal, -1 never asks, and
	// N >= 2 forces a panel of N. See ensemble.go.
	Ensemble int

	// ContextTokens is the window of the model that reads this package's
	// prompts — the planner, and afterwards the reviser and the completion gate
	// that read the same document. Zero means nobody could say, and every
	// budget sized from it then falls back to the literal it always used.
	//
	// It rides onto the graph at build so that the passes which happen later,
	// through signatures that carry a document and not an options struct, size
	// themselves from the same number the build did. See Graph.ContextTokens.
	ContextTokens int

	// Invoice is the measured price list for this model's workers, rendered by
	// the caller with RenderInvoice before the build starts. Empty is a machine
	// with nothing measured yet and leaves every prompt byte for byte as it was.
	//
	// The caller renders it for the same reason it renders the terrain: this
	// package holds no profile directory and no model name, and the block must
	// be one snapshot frozen for the build rather than a figure that moves under
	// passes still running. See Graph.Invoice and invoice.go.
	Invoice string

	// CapacitySamples and CapacityOverrunRate are the journal's measured answer
	// to how often one-worker leaves exceeded their envelope. Zero is no
	// measurement and preserves the old decision exactly; callers only populate
	// them behind the swarm gate. The sample count remains separate because a
	// dramatic rate from one run is an anecdote, not a reason to buy more work.
	CapacitySamples     int
	CapacityOverrunRate float64

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

	// Journal, when set, writes one node_briefed event per briefed node as the
	// brief pass lands — the rendered instruction, the sufficiency sentence
	// (Spec.Done) and the subharness — so a run's stopping condition is
	// queryable from its own artifacts rather than only as a field inside the
	// plan blob. The caller forms the store id; see BriefJournal. Nil leaves
	// briefs exactly as durable as they were before this existed, which is the
	// one-shot `aforge plan` path and every caller with no store to journal to.
	Journal BriefJournal
}

// buildLevels is how many expansion levels this build runs. It never exceeds
// MaxDepth, because MaxDepth is the ceiling on the shape and BuildDepth is only
// a statement about who decides it and when.
func (o Options) buildLevels() int {
	if o.BuildDepth > 0 && o.BuildDepth < o.MaxDepth {
		return o.BuildDepth
	}
	return o.MaxDepth
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
	// The terrain is placed on the graph before either opener launches. Both
	// goroutines below, and every pass after them, read the graph's preamble;
	// setting it afterwards would give the openers a different prefix from
	// everything that follows, which is the one thing the shared block exists to
	// prevent.
	graph := &Graph{Goal: goal, NextID: 1, Terrain: options.Terrain, FileShaped: options.FileShaped,
		// The requests the ask was read as containing, cleaned once and frozen
		// for the build like the terrain beside them, and for the same reason:
		// they join the shared prefix every pass reads.
		Asked:     cleanStrings(options.Asked),
		Continues: options.Continues, Records: append([]string(nil), options.Records...),
		// The window rides onto the document at the same moment the terrain
		// does, and for the same reason: every later pass over this graph has to
		// size itself from the same fact the build was sized from.
		ContextTokens: options.ContextTokens,
		// The prices, frozen for the build for the third time and the same
		// reason: sizing, expansion and the panel decision must all weigh a
		// division against one set of numbers rather than three snapshots taken
		// as leaves landed underneath them.
		Invoice: options.Invoice}
	emitProgress(progress, "grounding", "settling what to look at", "")

	// Grounding and the spine both need only the goal and the workspace it
	// stands on, so they run together and the grounding is free. It has to
	// finish before the fan-out, though, and that ordering is the point: the
	// fan-out is where one decision would otherwise get made independently
	// several times over.
	//
	// Both are handed the terrain from the options rather than reading it off
	// the graph. The value is the same one — it was copied onto the graph a few
	// lines up and nothing writes it again — but these two run before the graph
	// has a preamble worth rendering, and a pass that read a half-built graph
	// while the other goroutine was writing to it would be a data race for the
	// sake of nothing.
	terrain := options.Terrain
	// The requests travel beside the terrain and are read off the options for
	// the same reason: both openers run before there is a graph worth rendering
	// a preamble from, and one of them is writing to that graph.
	asked := graph.Asked
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
		choice, spineUsage, spineErr = spineWithProgress(ctx, client, goal, terrain, asked, options.SpineSamples, progress)
	}()
	go func() {
		defer opening.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				groundErr = guard.Note("plan/build ground", recovered)
			}
		}()
		grounding, usage, err := GroundWith(ctx, client, goal, terrain, asked, options.Recall)
		groundUsage.merge(usage)
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

	// --- the undivided shortcut ---------------------------------------------
	// The spine has spoken and there is nothing gated in this goal. For a
	// remainder that is the whole answer: one fresh worker, one contract, and
	// none of fan-out, bind, size, audit, expansion or per-leaf briefs.
	if options.Undivided && len(choice.Stages) == 1 {
		stage := choice.Stages[0]
		graph.Add(Node{
			Kind: KindWork, Stage: 1,
			Title:   strings.TrimSpace(stage.Title),
			Summary: strings.TrimSpace(stage.Summary),
			Brief:   goal,
		})
		emitProgress(progress, "steps", "1", "")
		report("undivided", time.Since(start), "one worker — the spine found nothing gated")
		return graph, groundErr
	}

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

	nodes, fanUsage, fanErr := FanOutWith(ctx, client, graph.context(), choice.Stages, graph.Settled)
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
	briefs := newBriefWriter(ctx, client, options.Briefs, progress, options.Journal)
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
	for level := 0; level < options.buildLevels(); level++ {
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

	// --- the one-sitting collapse ---------------------------------------------
	// The spine answered a question of gates, not of size: it can draw a chain
	// whose every link the sizing pass then judges atomic — one agent's sitting,
	// priced as a graph. A chain of sittings-that-are-each-atomic is one
	// sitting: the same agent reads its own earlier files, which is sequence
	// inside a worker, not a gate between workers. When the whole graph is that
	// chain, with nobody's specialist machinery involved, the graph is the
	// undivided answer the spine's one-stage sample would have given, reached
	// by evidence instead of by sampling luck.
	if collapsed := collapseAtomicChain(graph); collapsed != 0 {
		report("collapse", time.Since(start), fmt.Sprintf("chain of %d atomic nodes is one sitting", collapsed))
		emitProgress(progress, "steps", "1", "")
		return graph, errors.Join(groundErr, fanErr, bindErr, sizeErr, auditErr)
	}

	graph.Prune()
	emitProgress(progress, "steps", fmt.Sprintf("%d", len(graph.Nodes)), "")
	graph.addSynthesis()
	// The gathering node exists only now, and it is written for like any other
	// leaf: it is the one the finished job is judged against, so it is the last
	// place that can afford a harness stub for an instruction.
	briefs.sink = graph.deliverableSink()

	// Everything that was still moving has now stopped.
	announce(graph, options, settled, briefs, start, func(*Node) bool { return true })

	briefUsage, briefErr := briefs.apply(graph)
	graph.Usage.merge(briefUsage)
	if options.Briefs {
		report("brief", time.Since(start), plural(len(graph.writtenLeaves()), "leaf"))
	}

	return graph, errors.Join(groundErr, fanErr, bindErr, sizeErr, auditErr, briefErr)
}

// collapseAtomicChain folds a graph that is one short chain of atomic work
// nodes into a single undivided leaf. The shape it looks for is exact: every
// work node sized atomic by the sizing pass, no specialist anywhere, one node
// per stage, and each node's only need the node before it — work the spine
// drew as stages and the ruler then measured as sittings one agent could each
// just do. Four links is the most the rule trusts: past that the sittings sum
// past what one context ought to hold, and the graph stands as drawn.
//
// The collapse is the undivided shortcut reached by evidence rather than by
// the spine's coin: one work node, the goal as its brief, the first link's
// title. It returns how many nodes were folded, zero when the shape is
// anything else.
func collapseAtomicChain(graph *Graph) int {
	var chain []*Node
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork {
			return 0
		}
		if node.Size != SizeAtomic || node.Depth != 0 {
			return 0
		}
		if node.Subharness != "" && !GeneralistSubharness(node.Subharness) {
			return 0
		}
		chain = append(chain, node)
	}
	if len(chain) < 2 || len(chain) > 4 {
		return 0
	}
	sort.Slice(chain, func(i, j int) bool { return chain[i].Stage < chain[j].Stage })
	for index, node := range chain {
		if node.Stage != index+1 {
			return 0
		}
		if index == 0 {
			if len(node.Needs) != 0 {
				return 0
			}
			continue
		}
		if len(node.Needs) != 1 || node.Needs[0] != chain[index-1].ID {
			return 0
		}
	}
	folded := len(chain)
	first := chain[0]
	graph.Nodes = graph.Nodes[:0]
	graph.Stages = graph.Stages[:1]
	graph.Add(Node{
		Kind: KindWork, Stage: 1,
		Title:   strings.TrimSpace(first.Title),
		Summary: strings.TrimSpace(first.Summary),
		Brief:   graph.Goal,
		Undivided: fmt.Sprintf("chain of %d atomic nodes — one sitting",
			folded),
	})
	return folded
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
		if settled[node.ID] || (node.Kind != KindWork && node.ID != briefs.sink) || !ready(node) {
			continue
		}
		settled[node.ID] = true

		var inputs []string
		for _, need := range node.Needs {
			if source := graph.Node(need); source != nil {
				inputs = append(inputs, fmt.Sprintf("%q (%s)", source.Title, source.Summary))
			}
		}
		briefs.launch(shared, *node, inputs, deliverableLineFor(owner, label, node.ID, graph.FileShaped))
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
// Grounding binds several concrete members under one variable ("the three
// cities" → Berlin, Lisbon, Warsaw), so counting result rows alone would hide
// the load-bearing number the user is waiting to learn.
//
// The typed settlement makes the first branch exact rather than parsed: the
// number is the length of the values slice and the noun is the variable's own
// last word. The sentence scan below it remains for graphs whose settlements
// came off disk as prose.
func groundedSummary(settled []Settlement) string {
	if len(settled) == 0 {
		return "scope already clear"
	}
	for _, settlement := range settled {
		if len(settlement.Values) < 2 {
			continue
		}
		fields := strings.Fields(settlement.Variable)
		if len(fields) == 0 {
			continue
		}
		noun := strings.ToLower(strings.Trim(fields[len(fields)-1], ".,:;()[]{}\"'"))
		if noun != "" {
			return fmt.Sprintf("%d %s settled", len(settlement.Values), noun)
		}
	}
	for _, point := range SettledLines(settled) {
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
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithSchema(schema), ai.WithMaxTokens(structuredReplyTokens))
	if err != nil {
		provider.Report(ctx, provider.VerdictProviderFailure)
		return nil, err
	}
	// THE VERDICT ON A CUT REPLY IS WHETHER IT DECODES, NEVER THE FINISH
	// REASON ALONE. Two failures look the same on the wire — a reasoning model
	// that burned the budget thinking and returned nothing (seen at
	// completion_tokens=32768), and a model that looped inside the schema and
	// returned fifty-seven characters of nothing — and for both, one retry with
	// a doubled budget is the difference between a contract and a dead node; a
	// second cut answer is the model's problem, not the budget's. But a third
	// shape exists that is not a failure at all: nvidia/nemotron-3.5-lightning
	// answers the panel question with a complete object and finish_reason
	// "length", the counter having reached the ceiling on tokens that never
	// became text. A retry taken on the finish reason alone spent forty-five
	// seconds fetching a second copy of an answer that was already in hand.
	if finishedForLength(response) && !decodesAsObject(response.Text()) {
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

// structuredReplyTokens is the ceiling on a planning call's first attempt.
//
// Every call in this package asks for one JSON object of a known schema — a
// verdict, a brief, a contract, a panel decision — and the largest of them is a
// few thousand tokens. Left unset, the ceiling was the client's default, which
// the headless door raises to the leaf completion reserve (65536): a ceiling
// for a worker writing code, applied to a call answering a form. The
// difference is not academic. A model that loops inside a schema runs to
// whatever ceiling it is given; at 65536 that was four minutes and $0.013 per
// call on nvidia/nemotron-3.5-lightning, twice in one plan, with the run
// showing nothing but "still waiting" for the duration. Sized for the answer,
// the same failure costs thirty seconds, and the retry below still has room
// to double for a reply that genuinely needed more.
const structuredReplyTokens = 8192

// decodesAsObject is the probe structured uses before it spends a retry: it
// asks only whether the text holds one complete JSON object, into a target
// that accepts any, so the caller's destination is written exactly once.
func decodesAsObject(text string) bool {
	return decodeJSON(text, &struct{}{}) == nil
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

// decodeJSON reads a structured reply. The tolerance is not a nicety: a router
// that silently falls back to a provider without structured-output support
// answers in prose or behind a code fence, and this pass used to demand a bare
// value. Every contract call on such a model failed on "invalid character 'B'"
// — the call was paid for and the leaf ran without its working method — so the
// decode goes through the one extractor the whole system shares.
func decodeJSON(text string, destination any) error {
	return provider.DecodeJSONObject(text, destination)
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
