package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// contractPrompt writes the working method one agent will follow.
//
// This is the dynamic half of the executor. The brief says what the job is;
// the contract says how work of this kind is done well — the discipline a
// specialised harness would have baked into its system prompt, generated per
// task instead of hand-written per domain. pi is a coding harness because its
// prompt carries a coding method; a generic loop handed a generated method for
// the leaf in front of it gets the same advantage on any kind of work, and
// the harness itself never changes.
//
// It is a structuring call: it runs with the planner's economy (reasoning
// off), one call per leaf, all leaves in parallel, so the layer costs one
// call's latency however wide the graph is.
const contractPrompt = `You write the working method for one agent about to do one job, alone, with
tools: a shell, file writing, file editing, and web search.

Do not restate the job — the agent already has its instruction. Write the
method: what someone experienced in exactly this kind of work does differently
from someone merely competent.

Concretely, for this kind of work:
- What to understand before touching anything, and what order the work is best
  produced in.
- What "done" means here — including how the result must connect to or be
  reachable from what already exists, if anything does. Work that functions
  only in isolation is unfinished.
- How to verify: the checks this kind of work admits, exercised the way the
  result's eventual user would reach it.
- The two or three mistakes most often made in this kind of work, stated as
  things to watch for.

Every line must be specific to this kind of work — advice that would fit any
job ("plan first", "be thorough") is filler and wastes the agent's attention.

Write as direct instruction to the agent. 120-200 words, plain prose or short
dashes, no headings, no preamble, no mention of "the plan" or "this task".`

var contractSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "contract": { "type": "string" }
  },
  "required": ["contract"],
  "additionalProperties": false
}`)

// Contracts writes a working method for every leaf that lacks one, all leaves
// at once. Like Briefs, it is the batch form used at run time; the wall-clock
// cost is one call however many leaves there are.
func Contracts(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	shared := graph.context()

	type result struct {
		id       int
		contract string
		usage    *ai.Usage
		err      error
	}
	var group sync.WaitGroup
	var mutex sync.Mutex
	var results []result

	for _, id := range graph.Leaves() {
		node := graph.Node(id)
		if node == nil || node.Kind != KindWork || strings.TrimSpace(node.Contract) != "" {
			continue
		}
		group.Add(1)
		go func(node Node) {
			defer group.Done()
			contract, usage, err := writeContract(ctx, client, shared, node)
			mutex.Lock()
			defer mutex.Unlock()
			results = append(results, result{id: node.ID, contract: contract, usage: usage, err: err})
		}(*node)
	}
	group.Wait()

	var usage Usage
	var failures []error
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			// A missing contract degrades to the generic loop rather than
			// failing the run: the contract is an edge, not a load-bearing wall.
			failures = append(failures, item.err)
			continue
		}
		if node := graph.Node(item.id); node != nil {
			node.Contract = item.contract
		}
	}
	return usage, joinErrors(failures)
}

func writeContract(ctx context.Context, client Completer, shared string, node Node) (string, *ai.Usage, error) {
	var target strings.Builder
	fmt.Fprintf(&target, "The job: %s — %s\n", node.Title, node.Summary)
	if len(node.Sources) > 0 {
		fmt.Fprintf(&target, "It is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
	if brief := strings.TrimSpace(node.Brief); brief != "" {
		fmt.Fprintf(&target, "The instruction the agent will receive:\n%s\n", brief)
	}
	target.WriteString("\nWrite the working method for this kind of job.")

	messages := []ai.Message{
		systemMessage(contractPrompt),
		userMessage(shared),
		userMessage(target.String()),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanContract)
	var decoded struct {
		Contract string `json:"contract"`
	}
	response, err := structured(ctx, client, messages, contractSchema, &decoded)
	if err != nil {
		return "", usageOf(response), fmt.Errorf("contract %q: %w", node.Title, err)
	}
	contract := trim(decoded.Contract)
	if contract == "" {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return "", usageOf(response), annotate(fmt.Errorf("contract %q: empty response", node.Title), response)
	}
	// The schema held and the field is not empty; whether the method it
	// describes is a good one is not checkable without running the leaf.
	provider.Report(ctx, provider.VerdictUnverifiedSuccess)
	return contract, usageOf(response), nil
}
