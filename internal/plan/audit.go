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

// auditPrompt exists because bind is deliberately biased and a biased judge
// needs an opposing one. Bind is written to resist chaining, and it overshoots:
// early runs produced a migration plan that never read which tool was chosen,
// and event operations that never read which venue was booked. Both are worse
// failures than a spurious edge — a spurious edge costs latency, a missing one
// means the agent cannot see data it needs and confidently answers anyway.
//
// So this pass asks the opposite question, and the phrasing is the whole point.
// It never asks whether a dependency exists, which would just reproduce bind's
// judgment from the other side. It asks whether the node can be *finished* with
// what it has — a question about a concrete deliverable, where a missing input
// shows up as an answer the agent cannot write.
const auditPrompt = `You check whether each node can actually be completed with what it has been given.

` + agentPremise + `

Each node's agent receives the original goal and the outputs of the nodes listed
as its inputs — nothing else. It cannot see the rest of the plan or any other
node's work.

For each node below, ask: with only the goal and those inputs in hand, can a
competent agent produce a correct and complete result?

Answer ok unless something concrete is missing. If something is missing, name
the nodes whose outputs supply it — a decision that has not been made yet, a
number that has not been computed, a choice that determines what this node is
even about.

Answer ok when the node would merely be better informed with more context.
Being richer is not the test. Being finishable is.

You may only name nodes from earlier stages.`

var auditSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "checks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":    { "type": "integer" },
          "ok":      { "type": "boolean" },
          "missing": { "type": "array", "items": { "type": "integer" } }
        },
        "required": ["node", "ok", "missing"],
        "additionalProperties": false
      }
    }
  },
  "required": ["checks"],
  "additionalProperties": false
}`)

type auditCheck struct {
	Node    int   `json:"node"`
	OK      bool  `json:"ok"`
	Missing []int `json:"missing"`
}

// Audit runs the completability check over every stage after the first and adds
// back the edges whose absence would strand a node. It returns how many it
// recovered, which is the honest measure of how far bind undershot.
func Audit(ctx context.Context, client Completer, graph *Graph) (int, Usage, error) {
	return auditWith(ctx, client, graph, graph.planBlock())
}

// auditWith is Audit against a catalog block the caller has already rendered.
// The caller owns the guarantee that the block still describes this graph.
func auditWith(ctx context.Context, client Completer, graph *Graph, shared string) (int, Usage, error) {
	if len(graph.Stages) < 2 {
		return 0, Usage{}, nil
	}

	type result struct {
		checks []auditCheck
		usage  *ai.Usage
		err    error
	}
	results := make([]result, len(graph.Stages))
	var group sync.WaitGroup
	for stage := 2; stage <= len(graph.Stages); stage++ {
		group.Add(1)
		go func(stage int) {
			defer group.Done()
			// The fault fills the stage's slot on the way out: an audit that
			// faults recovers no edges, which is what a failed one already does.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[stage-1] = result{err: guard.Note(fmt.Sprintf("plan/audit stage %d", stage), recovered)}
				}
			}()
			checks, usage, err := auditStage(ctx, client, shared, graph, stage)
			results[stage-1] = result{checks: checks, usage: usage, err: err}
		}(stage)
	}
	group.Wait()

	var usage Usage
	var failures []error
	added := 0
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			failures = append(failures, item.err)
			continue
		}
		for _, check := range item.checks {
			if check.OK {
				continue
			}
			for _, missing := range check.Missing {
				target, source := graph.Node(check.Node), graph.Node(missing)
				if target == nil || source == nil || source.Stage >= target.Stage {
					continue
				}
				if contains(target.Needs, missing) {
					continue
				}
				if err := graph.AddNeed(check.Node, missing); err == nil {
					added++
				}
			}
		}
	}
	return added, usage, joinErrors(failures)
}

func auditStage(ctx context.Context, client Completer, shared string, graph *Graph, stage int) ([]auditCheck, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanAudit)
	var targets strings.Builder
	for _, node := range graph.Nodes {
		if node.Stage != stage {
			continue
		}
		inputs := "nothing — only the goal"
		if len(node.Needs) > 0 {
			inputs = "the outputs of " + joinInts(node.Needs)
		}
		fmt.Fprintf(&targets, "%d. %s — %s\n   currently receives: %s\n", node.ID, node.Title, node.Summary, inputs)
	}
	messages := []ai.Message{
		systemMessage(auditPrompt),
		userMessage(shared),
		userMessage(fmt.Sprintf("Check each of these stage %d nodes:\n%s", stage, targets.String())),
	}
	var decoded struct {
		Checks []auditCheck `json:"checks"`
	}
	// One check per node: the ask's own cardinality, read off the same loop that
	// wrote the list.
	response, err := structuredParts(ctx, client, messages, auditSchema, targetCount(graph, stage), &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("audit stage %d: %w", stage, err)
	}
	// Audit answers about specific nodes, so naming one that does not exist is
	// the same tell as in bind: the model stopped reading the catalog. An audit
	// that legitimately finds nothing missing returns an empty list and is right.
	for _, check := range decoded.Checks {
		if graph.Node(check.Node) == nil {
			provider.Report(ctx, provider.VerdictSemanticFailure)
			return decoded.Checks, usageOf(response), nil
		}
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return decoded.Checks, usageOf(response), nil
}
