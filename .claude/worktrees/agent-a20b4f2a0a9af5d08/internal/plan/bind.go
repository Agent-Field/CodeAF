package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
// It also collects duplicates, which is not a detour: this is the first call
// that sees every stage at once, and the fan-out's blindness means the same
// work can appear twice. The later node is the one flagged, since edges point
// backwards and the earlier node is the one everything else can already reach.
const bindPrompt = `You decide what each node must wait for.

You are given a goal and every node in the plan. Each node is executed by a
separate AI agent that receives the original goal and the outputs of the nodes
you list — and nothing else. So the list does two jobs at once: it decides when
the node may start, and it decides what its agent is allowed to see.

A node depends on an upstream node only when it is impossible to produce a
correct, complete result without reading that node's actual output. Name to
yourself the specific fact, number, decision, or artifact that crosses over. If
you cannot name one, there is no dependency.

These are not dependencies:
- sharing a topic, a subject, or a theme
- being "informed by", "building on", or "consistent with" another node
- simply coming later in the plan
- matching tone, format, or structure

Every dependency costs twice. It stops this node from starting until the other
finishes, and it pours another node's output into a context that was otherwise
clean. Most nodes need nothing: an empty list is the normal answer.

You may only list nodes from earlier stages.

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

// Bind resolves dependencies for every stage after the first, all at once.
// Stage 1 is skipped rather than asked: it has nothing earlier to point at, so
// its answer is known without spending a call.
func Bind(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	if len(graph.Stages) < 2 {
		return Usage{}, nil
	}
	shared := graph.context() + "\nEvery node in the plan:\n" + graph.catalog()

	type result struct {
		reply bindReply
		usage *ai.Usage
		err   error
	}
	results := make([]result, len(graph.Stages))
	var group sync.WaitGroup
	for stage := 2; stage <= len(graph.Stages); stage++ {
		group.Add(1)
		go func(stage int) {
			defer group.Done()
			reply, usage, err := bindStage(ctx, client, shared, graph, stage)
			results[stage-1] = result{reply: reply, usage: usage, err: err}
		}(stage)
	}
	group.Wait()

	var usage Usage
	var failures []error
	for _, item := range results {
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

func bindStage(ctx context.Context, client Completer, shared string, graph *Graph, stage int) (bindReply, *ai.Usage, error) {
	var targets string
	for _, node := range graph.Nodes {
		if node.Stage == stage {
			targets += fmt.Sprintf("%d. %s — %s\n", node.ID, node.Title, node.Summary)
		}
	}
	messages := []ai.Message{
		systemMessage(bindPrompt),
		userMessage(shared),
		userMessage(fmt.Sprintf("For each of these stage %d nodes, list what it must wait for:\n%s", stage, targets)),
	}
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithSchema(bindSchema))
	if err != nil {
		return bindReply{}, nil, fmt.Errorf("bind stage %d: %w", stage, err)
	}
	var reply bindReply
	if err := decodeJSON(response.Text(), &reply); err != nil {
		return bindReply{}, usageOf(response), annotate(fmt.Errorf("bind stage %d: %w", stage, err), response)
	}
	return reply, usageOf(response), nil
}
