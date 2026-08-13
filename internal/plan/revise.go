package plan

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// revisePrompt is the sentinel. It runs when something has been learned — a
// node finished and its result contradicts an assumption, or a person changed
// the goal — and it may rewrite only what has not started.
//
// The danger here is not that it edits badly, it is that it edits at all. Give
// a model the power to replan and it will replan, every time, forever: that is
// the agent that plans beautifully and ships nothing. So the default is stated
// as no change, and any edit has to name three specific things — which result,
// which assumption, which node. A revision that cannot point at all three is
// almost always the model restating a preference rather than reacting to
// information.
//
// The frozen rule is not advisory and is enforced in code as well as stated
// here. A started node's output may already be another node's input, so the
// past cannot be edited; a correction to finished work is new work appended
// after it, never a rewrite of it.
//
// It carries checkingRule for a reason found the expensive way. The rule was
// written into the two prompts that build a plan and into neither of the two
// that grow one, and this is the pass that grows one mid-run: told a result was
// disappointing, it would add a node to look at what another node produced, that
// node would report a gap, and the gap would come back here. That is the shape
// of the 27-round spiral the governors could only bound by counting. A cap stops
// a loop; the doctrine stops it being started.
//
// The paragraph about what a new node may be restates the worker premise in its
// own words on purpose — see workerPremise for the shared fact. Here it is a
// bound on the size of an edit rather than a description of the executor, and
// the sentence that follows it is the whole point of the sentence before it.
const revisePrompt = `You maintain a task graph while it is being executed.

` + agentPremise + `

Something has happened, and you decide whether the remaining plan should change.

The plan is a hierarchy. Indented nodes are parts of the node above them, and a
node marked "gathers" is not work — it is where its parts' results come back
together. Aim an edit at the node that actually does the work: changing what a
gathering node collects, when the problem is in one of its parts, fixes nothing
and disconnects the rest.

Nodes marked [locked] have started or finished. You cannot touch them: their
output may already be feeding another node. If finished work turned out wrong,
the answer is a new node that corrects it, never an edit to the old one.

Anything you add is going to one agent working alone, in order, with tools. Keep
a new node to something one agent finishes in a single pass and hand off as one
result. If what is needed is genuinely larger than that, add it as two or three
nodes that can run at the same time rather than one that cannot.

` + checkingRule + `

Your default is no change. Return an empty operation list unless a specific
result contradicts a specific assumption in a specific unstarted node. If you
cannot name all three, there is nothing to do here, and saying so is the right
answer.

Do not restructure work that is merely imperfect. Do not add nodes because more
detail would be nice. Do not reorder for tidiness. Every edit you make risks
invalidating work already in flight.

When a node was CANCELLED by the user, that is a decision and not a problem. Do
not add a node that redoes it, finishes it, resumes it, or checks what it left
behind — the user is spending nothing more on that work, and adding it back is
overruling them. The only legitimate edit is to the steps that were relying on
it: rewire, retitle or remove them as the loss of that input requires.

When you do act, use the fewest operations possible:
- add     a new node, with its inputs, when new work is genuinely required
- remove  an unstarted node whose purpose no longer exists
- rewire  an unstarted node's inputs when it now needs different information
- retitle an unstarted node whose scope has shifted

For every operation, give the reason as the concrete contradiction you found.`

var reviseSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "op":      { "type": "string", "enum": ["add", "remove", "rewire", "retitle"] },
          "node":    { "type": "integer" },
          "title":   { "type": "string" },
          "summary": { "type": "string" },
          "needs":   { "type": "array", "items": { "type": "integer" } },
          "reason":  { "type": "string" }
        },
        "required": ["op", "node", "title", "summary", "needs", "reason"],
        "additionalProperties": false
      }
    }
  },
  "required": ["operations"],
  "additionalProperties": false
}`)

// Operation is one edit the sentinel asked for, together with what actually
// happened to it. A rejected operation is kept rather than dropped so the
// caller can see what the model wanted and why the graph refused.
type Operation struct {
	Op      string `json:"op"`
	Node    int    `json:"node"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Needs   []int  `json:"needs"`
	Reason  string `json:"reason"`
	Applied bool   `json:"applied"`
	Refused string `json:"refused,omitempty"`
}

// Revise asks the sentinel whether the unstarted part of the graph should
// change in light of an event, and applies whatever it returns that the graph
// will legally accept.
func Revise(ctx context.Context, client Completer, graph *Graph, event string) ([]Operation, Usage, error) {
	var usage Usage
	// The sentinel is asked to find where a result contradicts a specific
	// assumption in a specific unstarted node, and it was the only plan pass in
	// the system that never saw the assumptions. Every other pass gets
	// graph.context() — what is settled for this goal, what the work itself has
	// to decide, what evidence the goal warrants — and it is exactly the list a
	// contradiction has to be found against. It leads with the goal already, so
	// nothing is lost by replacing the bare goal line with it.
	messages := []ai.Message{
		systemMessage(revisePrompt),
		userMessage(graph.context() + "\nThe plan as it stands:\n" + graph.stateBlock()),
		userMessage("What has happened:\n" + event),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanRevise)
	var decoded struct {
		Operations []Operation `json:"operations"`
	}
	response, err := structured(ctx, client, messages, reviseSchema, &decoded)
	usage.Add(usageOf(response))
	if err != nil {
		return nil, usage, fmt.Errorf("revise: %w", err)
	}

	applied := make([]Operation, 0, len(decoded.Operations))
	refused := 0
	for _, operation := range decoded.Operations {
		result := apply(graph, operation)
		if result.Refused != "" {
			refused++
		}
		applied = append(applied, result)
	}
	graph.Prune()
	// Refusals are the graph's own rules rejecting an edit — editing frozen
	// work, naming a node that is not there, closing a cycle. They are recorded
	// rather than swallowed for exactly this reason: a pass whose every
	// operation was refused produced a legal document describing an illegal
	// plan, which is a semantic failure and nothing else can see it.
	if refused > 0 && refused == len(applied) {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return applied, usage, nil
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return applied, usage, nil
}

// apply executes one operation against the graph's rules. Every refusal is
// recorded rather than silently swallowed: a sentinel that keeps trying to edit
// locked work is telling us something about the prompt, and we only find out if
// the refusals are visible.
// A refusal names the REASON and never the subject. The one reader of these
// strings puts them in front of a person (internal/resident/revise.go composes
// the receipt note), and it is the reader that knows which step is meant and
// what the person calls it. A refusal that named its own subject produced
// "retitle task-8-n4: node 4 is running" — the id and the machine's word for a
// step, both in the room, in a line the person did not ask for.
func apply(graph *Graph, operation Operation) Operation {
	refuse := func(reason string) Operation {
		operation.Refused = reason
		return operation
	}
	switch operation.Op {
	case "add":
		id := graph.Add(Node{
			Stage:   stageFor(graph, operation.Needs),
			Title:   trim(operation.Title),
			Summary: trim(operation.Summary),
		})
		for _, need := range operation.Needs {
			if err := graph.AddNeed(id, need); err != nil {
				return refuse(err.Error())
			}
		}
		operation.Node = id
		operation.Applied = true

	case "remove":
		if err := graph.Remove(operation.Node); err != nil {
			return refuse(err.Error())
		}
		operation.Applied = true

	case "rewire":
		node := graph.Node(operation.Node)
		if node == nil {
			return refuse("it is no longer in the plan")
		}
		if node.State.Frozen() {
			return refuse(fmt.Sprintf("it is already %s", node.State))
		}
		previous := node.Needs
		node.Needs = nil
		for _, need := range operation.Needs {
			if err := graph.AddNeed(operation.Node, need); err != nil {
				node.Needs = previous
				return refuse(err.Error())
			}
		}
		operation.Applied = true

	case "retitle":
		node := graph.Node(operation.Node)
		if node == nil {
			return refuse("it is no longer in the plan")
		}
		if node.State.Frozen() {
			return refuse(fmt.Sprintf("it is already %s", node.State))
		}
		if title := trim(operation.Title); title != "" {
			node.Title = title
		}
		if summary := trim(operation.Summary); summary != "" {
			node.Summary = summary
		}
		operation.Applied = true

	default:
		return refuse("unknown operation " + operation.Op)
	}
	return operation
}

// stageFor places a new node just after the latest stage it reads from, so the
// stage field stays a meaningful summary of where the node sits even after the
// graph has been edited. Scheduling does not consult it — waves come from the
// edges — but the display and the backward-edge rule both do.
func stageFor(graph *Graph, needs []int) int {
	stage := 1
	for _, need := range needs {
		if node := graph.Node(need); node != nil && node.Stage >= stage {
			stage = node.Stage + 1
		}
	}
	return stage
}
