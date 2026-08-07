package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

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
// anchors is the ruler in force. It is mutable because a measured profile can
// replace the built-in prior, and atomic because chat can recalibrate while a
// model change or another plan reads the ruler.
var anchorValue atomic.Value

func init() {
	anchorValue.Store(sizeAnchors)
}

// Anchors returns one stable snapshot of the ruler in force.
func Anchors() string {
	return anchorValue.Load().(string)
}

// UseAnchors installs a calibrated ruler. An empty string restores the prior,
// which is the right fallback whenever a profile is missing or unreadable.
func UseAnchors(anchors string) {
	if strings.TrimSpace(anchors) == "" {
		anchors = sizeAnchors
	}
	anchorValue.Store(anchors)
}

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

Weigh both costs honestly. Splitting a node costs a round of planning and an
extra result to reassemble, and buys nothing if the parts would just run one
after another anyway. Leaving a node too big costs an agent grinding serially
through work that had no reason to be sequential.

Judge the nodes relative to each other as well as to the anchors — you are
seeing all of them, and the largest few are what matter.

Most nodes in a well-built plan are atomic. Say so when they are.

Also return split_into for every node:

- For an atomic node, return an empty list. Always.
- Otherwise, name the pieces the node would break into that could genuinely run
  at the same time. Each is a short label of two or three words — "Berlin
  permits", "competitor pricing" — a name, not a sentence, and never a copy of
  the node's own summary or of the things it must touch.
- Name a piece only if it is real: something an agent could work on knowing
  nothing about the others, producing a result of its own. If the inside of the
  node is a sequence, or if you would only be restating it in smaller words,
  there are no pieces.
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
		if graph.Nodes[index].Kind == KindWork && graph.Nodes[index].Size == SizeUnknown {
			graph.Nodes[index].Size = SizeAtomic
		}
	}
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
	messages := []ai.Message{
		systemMessage(sizePromptWith(Anchors())),
		userMessage(shared),
		userMessage(fmt.Sprintf("Judge the size of each of these stage %d nodes:\n%s", stage, targets.String())),
	}
	var decoded struct {
		Sizes []sizeVerdict `json:"sizes"`
	}
	response, err := structured(ctx, client, messages, sizeSchema, &decoded)
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
