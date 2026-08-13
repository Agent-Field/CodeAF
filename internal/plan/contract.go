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
//
// The toolbox sentence is load-bearing and used to be wrong. It named four
// tools — shell, write, edit, web — for a worker that also holds background
// jobs, recall of everything folded away, a line to its siblings, and, on ask,
// tools that read documents and generate images, music, video and speech. The
// one prompt whose entire job is to say how this kind of work is done well
// could therefore not route a method through any of them, so a job that needed
// a PDF read or a chart drawn got a method that worked around the capability
// sitting unused. It is a sentence, not a manual: the method writer needs to
// know the routes exist, and the tool descriptions themselves say how to drive
// them.
//
// The verify bullet binds to the brief for the same reason. Done-means has two
// authors — the instruction states the acceptance bar, the method states how to
// check — and the second was never told the first was binding. One task's brief
// asked for a flicker to stop and its contract verified the structure of the
// page instead; a sibling task with the same shape got it right, which is what a
// coin flip looks like. The clause costs a line and removes the flip: the bar is
// stated once, in the instruction, and the method exercises that one.
const contractPrompt = `You write the working method for one agent about to do one job.

` + workerPremise + `

Its toolbox is a shell that also runs work in the background, file writing and
editing, web search and page fetching, recall of work already folded away, and
one line it can pass to the other agents on this job when there are any; where
the machine is configured for them it can ask for tools that read documents and
generate images, music, video and speech.

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
  to declare unverified and the one short check that would settle it. Where the
  agent's instruction already states the bar for done, the check you write
  exercises that bar itself rather than a stand-in for it.
- The two or three mistakes most often made in this kind of work, stated as
  things to watch for.
- Where this kind of work most often gets stuck — the source that is down, the
  tool that refuses, the case that resists — and the route an experienced hand
  takes around it: the substitute source, method, or construction that reaches
  the same end. The agent cannot ask anyone when it hits that wall; this line
  is what carries it through.

Every line must be specific to this kind of work — advice that would fit any
job ("plan first", "be thorough") is filler and wastes the agent's attention.

The method may demand evidence only in the shape the request asked for it. A
request to confirm, check, or verify something is satisfied by the fact of the
result — what was run and what came back, in the agent's own words — and the
method never escalates that into reporting the raw output, the full transcript,
or the verbatim text of anything the request did not ask to see. The method is
held against the deliverable by a reviewer later: every demand you write
becomes a requirement the person never made, so write none they did not.

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
	// Read off the graph here rather than inside the goroutines: it is one fact
	// about the goal, and the discipline in this loop is that nothing concurrent
	// touches the graph at all. The record roster is read at the same moment and
	// for the same reason.
	fileShaped := graph.FileShaped
	continues, records := graph.Continues, append([]string(nil), graph.Records...)
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
			contract, usage, err := writeContract(ctx, client, shared, node, notes, node.ID == sink, fileShaped, continues, records)
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
			// Dual-write, as with the brief: Contract stays the read, and the
			// spec's Method is the same string in the object that survives
			// re-targeting. The prompt above is unchanged — the method is not a
			// new thing, it is the same thing with somewhere durable to live.
			node.Contract = item.contract
			node.Spec.Method = item.contract
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
//
// The second sentence is the same lesson one level further in. A method that
// knows this job is the deliverable can still describe a way of *assembling*
// one — read the inputs, reconcile them, record the result — and a merge run to
// that method ends with an account of the merging. So the line says what the
// method must ask for in the end: the thing written out, here, in the agent's
// own last message.
const contractDeliverableLine = "This job IS the deliverable: every other result arrives here as material, and what " +
	"this agent produces is the whole of what the person who asked will read. The method must therefore end by " +
	"telling the agent to write that whole thing out in its own final message — the merged result, the figures, " +
	"the verdict, in full — and never to file it somewhere and name the place, describe how the material was " +
	"combined, or report that the assembly is finished.\n"

// contractDeliverableFileLine is the same job under the delivery law's carve-out
// (delivery.go): the ask named the file, so the method that ends by forbidding
// the file is the one that fails the run. The framing sentence is identical
// because the job is identical — what changes is only where the finished thing
// has to land, which is why that half comes from the shared constant rather than
// being said again here in slightly different words.
const contractDeliverableFileLine = "This job IS the deliverable: every other result arrives here as material, and what " +
	"this agent produces is the whole of what the person who asked will read. The method must therefore end by " +
	"holding the agent to this:\n\n" + DeliverToNamedFile + "\n"

// contractDeliverableLineFor picks the half of the law the ask calls for. False
// is what a caller that has not made the judgment passes, and it renders exactly
// the bytes this pass has always rendered.
func contractDeliverableLineFor(fileShaped bool) string {
	if fileShaped {
		return contractDeliverableFileLine
	}
	return contractDeliverableLine
}

// recordBlock tells the method writer what the agent will actually be handed of
// the work already done, and it is a statement of fact rather than an
// instruction about what to do with it.
//
// It exists because of a false statement that was shipped to a person. A leaf
// was asked to explain what had been wrong with some code; the pass that wrote
// its method had been handed nothing about the finished work but a list of file
// PATHS; with no way to say where the answer could come from, the method it
// wrote told the agent to infer the cause from the fact that the tests now
// passed, and offered an example of what such a cause might be. The agent
// shipped the example verbatim as the real root cause. Every layer behaved
// exactly as designed: the method writer had nothing, so it improvised, and the
// gate that should have caught it held file names and no content.
//
// So the mechanism is what is HANDED IN, not a rule about what to write. With a
// record, the writer knows there is somewhere for the facts to come from and can
// route the method through it. Without one, the writer knows there is not — and
// the sentence a method needs in that case is the one that permits an agent to
// say the record does not name something, which a writer who believes the agent
// can find out anyway will never think to write.
func recordBlock(continues bool, records []string) string {
	// A plan that continues nothing renders nothing at all, and the prompt is
	// what it has always been: there is no earlier work for a record to be of,
	// so both halves below would be answering a question this job never raised.
	if !continues {
		return ""
	}
	if len(records) == 0 {
		return "\nNo record of the earlier work is handed to this agent: it will have its " +
			"instruction, its inputs, and whatever it can run or read for itself, and nothing else. " +
			"Where done means stating a fact about work that has already happened, the method must " +
			"say where that fact is to come from, and must have the agent say plainly that the record " +
			"does not name it when it is not there to be found.\n"
	}
	return "\nThe agent will be handed these records of the work already done, as readable files it " +
		"can open with its shell:\n" + strings.Join(records, "\n") +
		"\nAny fact the job needs about what was done, changed, or found is in them, and the method " +
		"should say to read them for it rather than to reason it out.\n"
}

func writeContract(ctx context.Context, client Completer, shared string, node Node, playbook string, deliverable, fileShaped, continues bool, records []string) (string, *ai.Usage, error) {
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
		target.WriteString(contractDeliverableLineFor(fileShaped))
	}
	// What this agent will be able to KNOW, stated before the method is written
	// rather than discovered by an agent that has already promised an answer.
	// It goes in the per-node message and not in the shared preamble so the
	// system doctrine and the shared context stay byte-identical across the
	// fan-out and N-1 of the calls still land on a warm prefix.
	target.WriteString(recordBlock(continues, records))
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
