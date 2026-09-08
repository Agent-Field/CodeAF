package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sizeAnchors is the calibration point of the whole system, and the only place
// a number about harness capacity is written down.
//
// A fixed threshold — "split above six sources" — mis-sizes everything, because
// a research node is source-heavy while a writing node touches almost nothing
// and still takes an hour. But free judgment is worse: asked whether something
// can be decomposed, a model always says yes, which is exactly the runaway we
// spent the spine prompt fighting.
//
// So the model is given a reference frame instead of a rule. Placing a task
// against three concrete examples is a comparison, which models are good at;
// estimating it in the abstract is not. And it makes recalibration tractable —
// a different model or a different harness means rewriting these three examples
// and nothing else. They are a prior today. Once an executor exists and we can
// measure how many steps a leaf really costs, they get replaced by measured
// ones, and every judgment downstream moves with them.
const sizeAnchors = `Use these three reference tasks to judge scale. They are the ruler; place the
node against them rather than estimating it on its own.

TOO SMALL — one action and one answer. Look up a single fact; rename one thing;
  correct one sentence. An agent finishes before it has finished reading the
  instruction. Work this small should have stayed part of something larger,
  because handing it over costs more than doing it.

RIGHT — one self-contained subject taken from start to finished, in one sitting.
  Profile a single named vendor from its own material and write it up; add one
  capability along with the tests that prove it works; draft one section of a
  document. Perhaps ten steps, all on one subject, one deliverable, nothing
  waiting on a decision being made elsewhere. This is the target, and most work
  should land here.

TOO BIG — several unrelated threads tangled together, or more than one
  deliverable hiding inside one instruction. Survey a whole market and recommend
  a product; rebuild a component and migrate everything that uses it and update
  the documentation. Open-ended, no natural stopping point, and an agent doing it
  alone spends most of its time on work that could have been happening at once.

Judge by the breadth of the subject, not the volume of material. A great deal of
material about one subject is RIGHT. A little material about five unrelated
subjects, or one job that quietly contains three different deliverables, is TOO
BIG. A long list of things to touch is not by itself a reason to call something
oversized.`

// sizePrompt asks where a node sits against the anchors, and states the trade-off
// in the currency that actually applies. The economics matter as much as the
// ruler: told only "is this the right size", a model optimises for tidiness,
// but told what splitting costs and what not splitting costs, it optimises for
// the thing we care about. This is the same lever that made the spine stop
// inventing stages.
//
// LinearSubharness names the worker. It is written here rather than imported
// because the ruler is a fact about the worker, and the package that describes
// the worker is the one that already imports this one; a constant repeated in
// two packages is cheaper than a cycle.
//
// It is a name, and that is load-bearing. The generalist used to be spelled as
// the empty string, which made "this node was judged and the answer is the
// generalist" indistinguishable from "nobody judged this node" — and every
// reader that fills an unanswered question in from somewhere else, admission's
// inheritance above all, read the first as the second. A verdict that was made
// says so out loud; empty is reserved for the verdict nobody made.
const LinearSubharness = "linear"

// The ruler in force. It is mutable because a measured profile can replace the
// built-in prior, and guarded because chat can recalibrate it while a model
// change or another plan reads it.
var (
	anchorMutex   sync.RWMutex
	anchorInForce = sizeAnchors
)

// Anchors returns one stable snapshot of the ruler in force.
func Anchors() string {
	anchorMutex.RLock()
	defer anchorMutex.RUnlock()
	return anchorInForce
}

// UseAnchors installs a calibrated ruler. An empty string restores the built-in
// prior, which is the right fallback whenever a profile is missing or
// unreadable.
func UseAnchors(anchors string) {
	anchorMutex.Lock()
	defer anchorMutex.Unlock()
	if strings.TrimSpace(anchors) == "" {
		anchors = sizeAnchors
	}
	anchorInForce = anchors
}

// GeneralistSubharness reports whether a name is the worker, named. It is not
// the same question as "is this column filled in": a node whose worker column
// says "linear" was judged and answered, and an empty one was never asked. Both
// run the same worker, and only the second may be filled in from elsewhere.
func GeneralistSubharness(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), LinearSubharness)
}

// SubharnessChosen reports whether the column was written at all. Only the
// empty string means nothing was, and only then may a reader fill it in.
func SubharnessChosen(name string) bool { return strings.TrimSpace(name) != "" }

// sizePromptWith is the sizing prompt. Its bytes are pinned by a golden that
// reads them one for one: this pass is the one place capacity is judged, and a
// sentence added to it silently moves every plan the harness will ever make.
//
// Its second paragraph restates the worker premise in its own words, and keeps
// them deliberately — workerPremise is the source of truth for the shared fact,
// but this pass is the one that measures capacity, and capacity is precisely
// what the shared fact leaves out. One tool at a time, one deliverable, and a
// context that fills as it goes are the terms every judgment below is made in;
// a prompt that said only "works alone, in order, with tools" would be asking
// for a size against nothing.
//
// The burden section is the other half of the grain section, and the two are
// written to be read together. Grain says follow the seams the material really
// has; burden says a seam nobody can name is not a reason to cut. Left alone,
// each fails in the direction the other guards: a pass told only to follow the
// grain will invent one, and a pass told only to distrust splitting will refuse
// enumerations that were already there. So the burden is stated as a proof
// obligation on the joint objective — the wait, the cost, and the answer,
// together — with exactly two things that discharge it, concurrency or a node
// that cannot converge in one worker's budget. Neither is a fallback for the
// other, and neither is thoroughness, which is what a model reaches for when it
// wants to divide and has no reason to.
func sizePromptWith(anchors string) string {
	return `You judge whether each node is the right size to hand to a single agent.

That agent works alone and in order: it thinks, uses one tool, sees the result,
thinks again. It cannot do two things at once. It produces one deliverable and
stops. Everything it reads stays in front of it, so it slows down as it goes.

` + anchors + `

Judge each node against those anchors:

- atomic      — close to RIGHT, or smaller. One agent should just do it.
- borderline  — larger than RIGHT, but it is not obvious that breaking it up
                would produce parts that could genuinely run at the same time.
- oversized   — clearly toward TOO BIG. Splitting it would let real work happen
                simultaneously that is currently stuck behind other work.

Weigh both costs honestly. Splitting a node costs a round of planning, an extra
result to reassemble, and whatever context each new piece must be given before
it can start — a split whose pieces must each be told the whole subject is paid
for twice and buys nothing if the pieces would just run one after another
anyway. Leaving a node too big costs an agent grinding serially through work
that had no reason to be sequential, and the person waits for the longest chain,
never the total, so a piece that carries only its own share is cheap in context
and repaid in waiting.

Judge the nodes relative to each other as well as to the anchors — you are
seeing all of them, and the largest few are what matter.

Most nodes in a well-built plan are atomic. Say so when they are.

The node stands whole until a split proves what it buys. That is the starting
position at every level and it is the answer whenever the case for dividing is
not actually made — not a last resort for when nothing better comes to mind.
Whatever is bought has to be bought on all three of the things being spent at
once: the time the person waits, what the work costs to run, and how good the
answer comes back. A division that improves none of the three is a division that
was made for its own sake.

Two things and only two things discharge that burden. Either the pieces would
genuinely run at the same time — none of them waiting on another, so the wait
becomes the longest piece instead of the sum of all of them — or the node cannot
be brought to an end inside what one worker can hold, so it would be stopped and
resumed however anyone decides. A sequence discharges neither: pieces that run
one after another land no sooner than the undivided node, and they add a
briefing for each piece and a reassembly at the end that the undivided node
never paid for.

Each piece must also be sayable more precisely than the node itself. A piece
carries a deliverable of its own and its own condition for being finished, and
both can be written down without reference to the other pieces. If the only
difference between a piece and the node is narrower wording, it is not a piece,
it is the node said again; and two pieces that would finish on the same
condition are one piece, with the second being that answer bought twice.

Say the price out loud before accepting it: every piece re-pays whatever it must
be told before it can start, every piece is one more result to reassemble, and
every piece is one more thing to schedule and wait on. What clears that price is
width that can be named — the units that stand apart, and which piece owns which
— or a named reason the node cannot converge as one job. Thoroughness never
clears it. Dividing work does not make it better, and a split made for the look
of the thing is paid for in full by the person waiting.

Also return split_into for every node:

- For an atomic node, return an empty list. Always.
- Otherwise, name the pieces the node would break into that could genuinely run
  at the same time. Each is a short label of two or three words — "Berlin
  permits", "competitor pricing" — a name, not a sentence, and never a copy of
  the node's own summary or of the things it must touch.
- Name a piece only if it is real: something an agent could work on knowing
  nothing about the others, producing a result of its own. If you would only be
  restating the node in smaller words, there are no pieces.
- Where the inside of an oversized node is a sequence, name the ordered stages it
  passes through instead — one short label each, in the order they happen. Say
  them here because this is the only place the node has to say what it is made
  of, and a node too large to be carried to an end in one sitting is divided
  whether or not anything in it runs at the same time. Nothing reads this list as
  a claim that they are simultaneous. Where you cannot name two stages either,
  the list is empty.
- Where the node's own words, or the things it says it must touch, already
  enumerate units that stand apart, the pieces are that enumeration: one unit
  each, or an even batch of them each when the units are many. Do not halve an
  enumerated set into two coarse pieces, and never name two pieces that would
  each cover the whole set — each would do all of it, and one answer would be
  paid for twice. Where the material enumerates nothing, there is no split to
  read off it.
- Name a piece only when it could be written down with a deliverable of its own
  and its own way of telling that it is finished, distinguishable from the
  node's and from every other piece's. If you would be handing the same finish
  line to two of them, there are no pieces.
- If you cannot name at least two, return an empty list. That is the answer that
  says to leave the node whole, and it is a common and correct one.`
}

var sizeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "sizes": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":  { "type": "integer" },
          "size":  { "type": "string", "enum": ["atomic", "borderline", "oversized"] },
          "split_into": { "type": "array", "items": { "type": "string" } }
        },
        "required": ["node", "size", "split_into"],
        "additionalProperties": false
      }
    }
  },
  "required": ["sizes"],
  "additionalProperties": false
}`)

type sizeVerdict struct {
	Node  int      `json:"node"`
	Size  string   `json:"size"`
	Parts []string `json:"split_into"`
}

// sizeResult is one stage's verdicts.
type sizeResult struct {
	verdicts []sizeVerdict
	usage    *ai.Usage
	err      error
}

// SizeNodes judges every node in the graph, one call per stage, all at once.
// It reads nothing that bind writes, so it is run concurrently with binding
// rather than as a pass of its own — the judgment is free in wall clock.
//
// Like Bind, it is split into gather and apply: the calls run while another
// pass reads the same graph, so no write may happen until the builder runs
// sizeApply serially.
func SizeNodes(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	return sizeApply(graph, sizeGather(ctx, client, graph, graph.planBlock()))
}

// sizeGather runs every stage's call against a catalog block the caller has
// already rendered. It never writes to the graph.
func sizeGather(ctx context.Context, client Completer, graph *Graph, shared string) []sizeResult {
	stages := len(graph.Stages)
	if stages == 0 {
		stages = 1
	}

	results := make([]sizeResult, stages)
	var group sync.WaitGroup
	for stage := 1; stage <= stages; stage++ {
		group.Add(1)
		go func(stage int) {
			defer group.Done()
			// A faulted stage lands as a failed one, and sizeApply's default —
			// unjudged work nodes are atomic — carries its nodes the rest of
			// the way, exactly as it does for a stage the model skipped.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[stage-1] = sizeResult{err: guard.Note(fmt.Sprintf("plan/size stage %d", stage), recovered)}
				}
			}()
			verdicts, usage, err := sizeStage(ctx, client, shared, graph, stage)
			results[stage-1] = sizeResult{verdicts: verdicts, usage: usage, err: err}
		}(stage)
	}
	group.Wait()
	return results
}

// sizeApply writes the gathered verdicts into the graph.
func sizeApply(graph *Graph, results []sizeResult) (Usage, error) {
	var usage Usage
	var failures []error
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			failures = append(failures, item.err)
			continue
		}
		for _, verdict := range item.verdicts {
			node := graph.Node(verdict.Node)
			if node == nil || node.Kind == KindSynthesis {
				continue
			}
			switch Size(verdict.Size) {
			case SizeAtomic, SizeBorderline, SizeOversized:
				node.Size = Size(verdict.Size)
			}
			node.Parts = shortLabels(verdict.Parts)
		}
	}
	// A node the model skipped is treated as atomic. Defaulting the unknown
	// case toward not expanding is the safe direction: an unnecessary split
	// wastes calls and invites the runaway, while an unsplit node still gets
	// done, only more slowly.
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork || node.Size != SizeUnknown {
			continue
		}
		node.Size = SizeAtomic
	}
	// And last, the one verdict on this pass that is not the model's to give.
	// The prompt above asks whether a node can be brought to an end inside what
	// one worker holds; where the node names its own material and that material
	// has been weighed, the answer is already known and a judgment against it is
	// simply wrong. See correctBeyondReach in reach.go — which is also where the
	// other half of that law lives, the half that leaves a correctly divided
	// lane alone.
	correctBeyondReach(graph)
	return usage, joinErrors(failures)
}

// shortLabels keeps only things that look like names. Asked for parts, a model
// will sometimes hand back the lines it was given — the node's own summary, its
// list of sources — which would make the pre-check pass for every node and stop
// checking anything. A part is a label; anything sentence-length is an echo.
func shortLabels(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		value = trim(value)
		if value == "" || len(strings.Fields(value)) > 5 || strings.ContainsAny(value, ";—") {
			continue
		}
		kept = append(kept, value)
	}
	return kept
}

func sizeStage(ctx context.Context, client Completer, shared string, graph *Graph, stage int) ([]sizeVerdict, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanSize)
	var targets strings.Builder
	found := false
	for _, node := range graph.Nodes {
		if node.Stage != stage || node.Kind == KindSynthesis {
			continue
		}
		found = true
		fmt.Fprintf(&targets, "%d. %s — %s\n", node.ID, node.Title, node.Summary)
		if len(node.Sources) > 0 {
			fmt.Fprintf(&targets, "   must touch: %s\n", strings.Join(node.Sources, "; "))
		}
	}
	if !found {
		return nil, nil, nil
	}
	// One verdict comes back per node being judged, so that count is what the
	// reply has to have room for. Reading it off the same loop that built the
	// list is the whole of the derivation: the ask states its own cardinality
	// and nobody has to guess it.
	judged := targetCount(graph, stage)
	// The prices go on the very last message and nowhere else. The system prompt
	// above is the same bytes for every stage of every job on this machine and
	// the catalog block is the same bytes for every stage of this one; an
	// invoice spliced into either would rewrite a shared prefix every time a
	// leaf finished, and cost every cache hit behind it for a table of six
	// numbers. See invoice.go on cache shape.
	messages := []ai.Message{
		systemMessage(sizePromptWith(Anchors())),
		userMessage(shared),
		userMessage(withInvoice(
			fmt.Sprintf("Judge the size of each of these stage %d nodes:\n%s", stage, targets.String()),
			graph.Invoice)),
	}
	var decoded struct {
		Sizes []sizeVerdict `json:"sizes"`
	}
	response, err := structuredParts(ctx, client, messages, sizeSchema, judged, &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("size stage %d: %w", stage, err)
	}
	// The pass was asked about a named set of nodes; an answer that judges none
	// of them, or judges one that does not exist, did not do the job. Sizes
	// themselves are opinions and are not checkable here.
	if len(decoded.Sizes) == 0 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return decoded.Sizes, usageOf(response), nil
	}
	for _, verdict := range decoded.Sizes {
		if graph.Node(verdict.Node) == nil {
			provider.Report(ctx, provider.VerdictSemanticFailure)
			return decoded.Sizes, usageOf(response), nil
		}
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return decoded.Sizes, usageOf(response), nil
}
