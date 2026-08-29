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

// bindPrompt decides how fast the plan can possibly run, and it is written
// against the model's strongest bias. Asked what depends on what, a model
// trained on project plans returns a chain, because prose is sequential and the
// plans in its training data are narratives. Every invented link is wall clock
// nobody gets back.
//
// Three levers push the other way. The test is made operational — name the
// artifact you read, or there is no edge — so a vague sense of relatedness
// cannot survive the question. The cost is stated in both currencies at once,
// delay and context pollution, because one list controls both. And the empty
// answer is named as the normal one, so returning nothing reads as success
// rather than as a failure to find something.
//
// Reading is not the only thing that can order two nodes, and pretending it was
// cost a whole run: four agents were handed one workspace, one of them patching
// it while the others read it, and every result was drawn from a tree that was
// changing underneath them. So state mutation is a second reason for an edge
// with the same standing as information. It is the one reason that can order two
// nodes inside a single stage — the parts of a stage are simultaneous by
// construction, and the only thing that may break that is one of them changing
// what the others work on — which is why this pass now covers stage 1 too.
//
// It also collects duplicates, which is not a detour: this is the first call
// that sees every stage at once, and the fan-out's blindness means the same
// work can appear twice. The later node is the one flagged, since edges point
// backwards and the earlier node is the one everything else can already reach.
const bindPrompt = `You decide what each node must wait for.

You are given a goal and every node in the plan. Each node is executed by a
separate AI agent that receives the original goal and the outputs of the nodes
you list — and nothing else. So the list does two jobs at once: it decides when
the node may start, and it decides what its agent is allowed to see.

There are two reasons for an edge, and they count equally.

INFORMATION — this node cannot produce a correct, complete result without
reading that node's actual output. Name to yourself the specific fact, number,
decision, or artifact that crosses over. If you cannot name one, there is no
edge of this kind.

STATE — that node changes the shared material this node works from: patching a
tree, installing or upgrading what others run against, fetching or generating
the corpus everyone reads, restructuring a document others quote. Whoever
changes shared material runs before everyone who reads it, even when no fact
crosses over and nothing is quoted. Two nodes that only read the same unchanged
material need no edge between them; a node that changes it is upstream of every
node that touches it afterwards. They run at the same time otherwise, and one
agent rewriting what three others are reading corrupts all four results.

These are not dependencies:
- sharing a topic, a subject, or a theme
- being "informed by", "building on", or "consistent with" another node
- simply coming later in the plan
- matching tone, format, or structure

Every dependency costs twice. It stops this node from starting until the other
finishes, and it pours another node's output into a context that was otherwise
clean. Most nodes need nothing: an empty list is the normal answer.

The exception is a node that gathers — one whose job is to assemble, judge,
review, or deliver what other nodes produce. For that node an empty list is
almost always wrong: it would start alongside the very work it exists to
consume. Name the nodes whose outputs it assembles.

You may list nodes from earlier stages for either reason. You may also list a
node from this same stage, but only for STATE — the parts of one stage were
written to run at the same time, so the only thing that may order them is one of
them changing what the others work on.

Separately, report duplicates. The stages were written independently, so the
same work sometimes appears twice under different names. Report a node as a
duplicate only when the two would produce substantially the same output, and
always name the earlier node as the one to keep. Overlapping topics are not
duplicates.`

var bindSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "bindings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":  { "type": "integer" },
          "needs": { "type": "array", "items": { "type": "integer" } }
        },
        "required": ["node", "needs"],
        "additionalProperties": false
      }
    },
    "duplicates": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":    { "type": "integer" },
          "same_as": { "type": "integer" }
        },
        "required": ["node", "same_as"],
        "additionalProperties": false
      }
    }
  },
  "required": ["bindings", "duplicates"],
  "additionalProperties": false
}`)

type bindReply struct {
	Bindings []struct {
		Node  int   `json:"node"`
		Needs []int `json:"needs"`
	} `json:"bindings"`
	Duplicates []struct {
		Node   int `json:"node"`
		SameAs int `json:"same_as"`
	} `json:"duplicates"`
}

// bindResult is one stage's answer. asked separates a stage that answered
// nothing from a stage that was never called: both leave an empty reply behind,
// and counting the second as a call would put calls in the accounting that
// nobody made.
type bindResult struct {
	asked bool
	reply bindReply
	usage *ai.Usage
	err   error
}

// Bind resolves dependencies for every stage, all at once. Stage 1 is asked
// only when it holds more than one node: the only edge it can produce is the
// mutation ordering between two siblings, and a stage of one has no siblings.
//
// It is split into a gather phase and an apply phase so a concurrent pass can
// share the graph. Gathering only reads and calls; every write waits for
// bindApply, which the builder runs serially — the sizing pass copies nodes
// while binding is in flight, and interleaved writes were a data race.
func Bind(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	return bindApply(graph, bindGather(ctx, client, graph, graph.planBlock()))
}

// bindGather runs every stage's call against a catalog block the caller has
// already rendered. It never writes to the graph.
func bindGather(ctx context.Context, client Completer, graph *Graph, shared string) []bindResult {
	if len(graph.Stages) == 0 || len(graph.Nodes) < 2 {
		return nil
	}

	results := make([]bindResult, len(graph.Stages))
	var group sync.WaitGroup
	for stage := 1; stage <= len(graph.Stages); stage++ {
		if !worthBinding(graph, stage) {
			continue
		}
		group.Add(1)
		go func(stage int) {
			defer group.Done()
			// asked stays true: the stage was called, and a fault is how that
			// call ended. It reaches bindApply as a failure, which leaves the
			// stage unbound rather than silently claiming it had no edges.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[stage-1] = bindResult{asked: true, err: guard.Note(fmt.Sprintf("plan/bind stage %d", stage), recovered)}
				}
			}()
			reply, usage, err := bindStage(ctx, client, shared, graph, stage)
			results[stage-1] = bindResult{asked: true, reply: reply, usage: usage, err: err}
		}(stage)
	}
	group.Wait()
	return results
}

// bindApply writes the gathered answers into the graph.
func bindApply(graph *Graph, results []bindResult) (Usage, error) {
	var usage Usage
	var failures []error
	for _, item := range results {
		if !item.asked {
			continue
		}
		usage.Add(item.usage)
		if item.err != nil {
			failures = append(failures, item.err)
			continue
		}
		for _, entry := range item.reply.Bindings {
			graph.setNeeds(entry.Node, entry.Needs)
		}
	}
	// Folding happens after every binding is applied, so a duplicate's inbound
	// edges are already known and travel with it to the node that survives.
	for _, item := range results {
		for _, duplicate := range item.reply.Duplicates {
			keep, drop := duplicate.SameAs, duplicate.Node
			if keep == drop {
				continue
			}
			if source, target := graph.Node(drop), graph.Node(keep); source == nil || target == nil || source.Stage < target.Stage {
				continue
			}
			_ = graph.Retarget(drop, keep)
		}
	}
	return usage, joinErrors(failures)
}

// worthBinding says whether a stage can produce an edge at all. Every stage
// after the first has earlier nodes to point at; the first has only its own
// siblings, so a single-node stage 1 would spend a call to be told nothing.
func worthBinding(graph *Graph, stage int) bool {
	count := 0
	for _, node := range graph.Nodes {
		if node.Stage == stage {
			count++
		}
	}
	if stage == 1 {
		return count > 1
	}
	return count > 0
}

func bindStage(ctx context.Context, client Completer, shared string, graph *Graph, stage int) (bindReply, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanBind)
	var targets strings.Builder
	for _, node := range graph.Nodes {
		if node.Stage == stage {
			fmt.Fprintf(&targets, "%d. %s — %s\n", node.ID, node.Title, node.Summary)
		}
	}
	messages := []ai.Message{
		systemMessage(bindPrompt),
		userMessage(shared),
		userMessage(fmt.Sprintf("For each of these stage %d nodes, list what it must wait for:\n%s", stage, targets.String())),
	}
	var reply bindReply
	// One row per node being bound: the ask's own cardinality, read off the same
	// loop that wrote the list.
	response, err := structuredParts(ctx, client, messages, bindSchema, targetCount(graph, stage), &reply)
	if err != nil {
		return bindReply{}, usageOf(response), fmt.Errorf("bind stage %d: %w", stage, err)
	}
	// An id nobody has heard of is the bind pass's characteristic failure: the
	// catalog is long, the answer is numbers, and a model that has lost track
	// invents them. setNeeds drops those edges silently, which is right for the
	// graph and wrong for the record — so the reply is checked here, where the
	// difference between "this plan needs no edges" and "this model could not
	// read the catalog" is still visible.
	if bindNamesUnknownNodes(graph, reply) {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return reply, usageOf(response), nil
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return reply, usageOf(response), nil
}

func bindNamesUnknownNodes(graph *Graph, reply bindReply) bool {
	for _, entry := range reply.Bindings {
		if graph.Node(entry.Node) == nil {
			return true
		}
		for _, need := range entry.Needs {
			if graph.Node(need) == nil {
				return true
			}
		}
	}
	for _, duplicate := range reply.Duplicates {
		if graph.Node(duplicate.Node) == nil || graph.Node(duplicate.SameAs) == nil {
			return true
		}
	}
	return false
}
