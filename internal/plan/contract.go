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
- What "done" means here, said as what whoever ends up using the result does
  with it and sees — including how it must connect to or be reachable from what
  already exists, if anything does. Work that functions only in isolation is
  unfinished.
- How to verify: the whole path exercised the way that user reaches it, run
  before anything may be called verified, since parts checked separately never
  add up to a working result — and, for whatever cannot be run from here, what
  to declare unverified and the one short check that would settle it.
- The two or three mistakes most often made in this kind of work, stated as
  things to watch for.
- Where this kind of work most often gets stuck — the source that is down, the
  tool that refuses, the case that resists — and the route an experienced hand
  takes around it: the substitute source, method, or construction that reaches
  the same end. The agent cannot ask anyone when it hits that wall; this line
  is what carries it through.

Every line must be specific to this kind of work — advice that would fit any
job ("plan first", "be thorough") is filler and wastes the agent's attention.

Write as direct instruction to the agent. 120-200 words, plain prose or short
dashes, no headings, no preamble, no mention of "the plan" or "this task".

Answer with one bare JSON object and nothing else — {"contract": "<the method>"}
— with no code fence around it and no sentence before or after it.`

var contractSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "contract": { "type": "string" }
  },
  "required": ["contract"],
  "additionalProperties": false
}`)

// ContractPlaybook returns the earned method bullets relevant to one leaf.
// Nil and empty results preserve the fixed contract prompt byte for byte.
type ContractPlaybook func(Node) string

// Contracts writes a working method for every leaf that lacks one, all leaves
// at once. Like Briefs, it is the batch form used at run time; the wall-clock
// cost is one call however many leaves there are.
func Contracts(ctx context.Context, client Completer, graph *Graph, playbook ContractPlaybook, callbacks ...Progress) (Usage, error) {
	shared := graph.context()
	var progress Progress
	if len(callbacks) > 0 {
		progress = serialProgress(callbacks[0])
	}

	type result struct {
		id       int
		contract string
		usage    *ai.Usage
		err      error
	}
	var group sync.WaitGroup
	var mutex sync.Mutex
	var results []result
	var targets []Node

	// The deliverable owner is written for like any other leaf. It used to be
	// skipped for not being KindWork, which left the node the delivery gate
	// judges holding a two-line harness stub where every other node held a
	// method — and the gate's whole question is whether the finished whole is
	// what was asked for, judged against exactly that stub.
	sink := graph.deliverableSink()
	for _, id := range graph.writtenLeaves() {
		node := graph.Node(id)
		if node == nil || strings.TrimSpace(node.Contract) != "" {
			continue
		}
		targets = append(targets, *node)
	}
	total := len(graph.writtenLeaves())
	base := total - len(targets)
	if progress != nil {
		emitProgress(progress, "contracts", fmt.Sprintf("%d/%d", base, total), "")
	}
	completed := 0
	for _, node := range targets {
		group.Add(1)
		go func(node Node) {
			defer group.Done()
			// The results slice is appended to, so a faulting node has to add
			// its own failed entry or it would simply vanish from the pass.
			// A contract that fails degrades to the generic loop; so does this.
			defer func() {
				if recovered := recover(); recovered != nil {
					fault := guard.Note(fmt.Sprintf("plan/contract node %d", node.ID), recovered)
					mutex.Lock()
					defer mutex.Unlock()
					results = append(results, result{id: node.ID, err: fault})
				}
			}()
			notes := ""
			if playbook != nil {
				notes = playbook(node)
			}
			contract, usage, err := writeContract(ctx, client, shared, node, notes, node.ID == sink)
			mutex.Lock()
			defer mutex.Unlock()
			results = append(results, result{id: node.ID, contract: contract, usage: usage, err: err})
			completed++
			if progress != nil {
				latest := ""
				if err == nil {
					latest = nodeProgressTitle(node)
				}
				emitProgress(progress, "contracts", fmt.Sprintf("%d/%d", base+completed, total), latest)
			}
		}(node)
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

// contractDeliverableLine tells the method writer which of the two jobs it is
// writing for. Every other leaf produces material; this one produces the thing
// itself, and its method has to be about the finished whole from the seat of the
// person who asked — what they open, what they read, what would make them say it
// is not done. Without the line the sink reads as a filing step and gets a
// filing method.
const contractDeliverableLine = "This job IS the deliverable: every other result arrives here as material, and what " +
	"this agent produces is the whole of what the person who asked will read.\n"

func writeContract(ctx context.Context, client Completer, shared string, node Node, playbook string, deliverable bool) (string, *ai.Usage, error) {
	var target strings.Builder
	// A node that came out of a plan always has a title; the one-leaf job does
	// not, because there was nothing to distinguish it from. Naming the job
	// twice, or naming it as an empty handle, both spend the model's attention
	// on nothing.
	if title := strings.TrimSpace(node.Title); title != "" {
		fmt.Fprintf(&target, "The job: %s — %s\n", title, node.Summary)
	} else {
		fmt.Fprintf(&target, "The job: %s\n", node.Summary)
	}
	if len(node.Sources) > 0 {
		fmt.Fprintf(&target, "It is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
	if brief := strings.TrimSpace(node.Brief); brief != "" {
		fmt.Fprintf(&target, "The instruction the agent will receive:\n%s\n", brief)
	}
	if deliverable {
		target.WriteString(contractDeliverableLine)
	}
	// The earned notes are per-leaf, retrieved for this node's territory, so
	// they belong here and nowhere earlier. Every leaf in a project is written
	// concurrently against the same doctrine and the same shared context; if the
	// notes rode in the system message, the first byte of the very first message
	// would differ per leaf and each of the N calls would write its own prefix
	// cold. Kept at the tail of the only per-node message, the whole 3-message
	// head — system doctrine plus shared context — is byte-identical across the
	// fan-out, and N-1 of the calls land on a warm prefix.
	if playbook = strings.TrimSpace(playbook); playbook != "" {
		target.WriteString("\nEarned method notes for this territory:\n" + playbook + "\n")
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
