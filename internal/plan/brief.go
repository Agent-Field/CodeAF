package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// briefPrompt writes the instruction a leaf is actually handed.
//
// This is the boundary of the whole system: everything above it is structure,
// and this is where structure turns into something an agent can run. The agent
// on the other side sees nothing but these words — not the goal, not the graph,
// not its neighbours — so anything the instruction leaves out is simply absent.
//
// Three failure modes are worth writing against. An instruction that assumes
// context produces an agent that invents it; and an instruction without
// explicit boundaries produces agents that all drift toward the same
// interesting middle of the problem, which is how a wide graph collapses back
// into duplicated work.
//
// Its second paragraph is workerPremise, shared verbatim with the working
// method: both passes are writing for the same machine, and the one place that
// fact was restated rather than shared is the one place it was wrong.
//
// The third is narrower and was the most expensive. The goal reaches every
// agent, so a goal naming a deliverable reads to all of them as an instruction
// to produce it — five nodes once wrote the same REVIEW.md over the top of each
// other. The graph already knows who owns it, so the brief is told, and every
// non-owner is told in words that its result is handed over instead.
const briefPrompt = `You write the instruction that one agent receives.

` + workerPremise + `

It sees only what you write — not the wider goal, not the plan, not the other
agents' work. Whatever you leave out is simply missing.

Write directly to it:
- Give it the context it needs to make sense of the job on its own.
- State exactly what it is responsible for delivering.
- Say what finished looks like in the terms of whoever will use the result —
  what they do with it, what they must see — and where to stop.
- Where other agents are delivering something adjacent, say which results are
  theirs, so this one does not redo them.

Exactly one node produces the goal's final deliverable, and you are told which.
When it is not this one, say plainly that the deliverable is that other agent's
to produce and that this agent hands its own result over instead of writing any
version of it — every agent sees the goal, and without this line they each write
the same file over the top of the others. When it is this one, say that it is
this agent's alone and that the other results arrive as inputs to it. This is a
statement about what the agent delivers, not a fence around what it may touch.

The boundary is always about what an agent is responsible for delivering, never
about what it is allowed to touch. Say nothing about which files, sections,
components or parts of the work it may or may not change. It must be free to do
whatever its own piece requires — including changing things that already exist,
and connecting its work into what is already there so that it actually takes
effect. An instruction that fences an agent off from the rest of the work
produces something that is correct in isolation and connected to nothing, which
is worth nothing.

Its work must be finished, not merely written. If the result has to be reachable,
callable, registered, wired in or otherwise made live to count as done, say so.

If it will receive results from earlier work, refer to those as inputs it will
already have. Do not tell it to go and find them.

No preamble, no headings, no meta-commentary, no mention of "the plan", "your
task", or "this node". 90-160 words of plain instruction.`

// criterionBlock is the second half of the same call.
//
// A stopping condition is the one fact nothing in the system carried. Rounds
// were bounded by counting them, results were judged against prose that had to
// be re-read to be understood, and a retry had nothing to inherit — so what a
// re-aimed node was judged against was whatever its replacement's author
// happened to write down. A criterion is the object that survives that.
//
// It rides this call rather than buying one. The instruction writer has already
// read the goal, the node and its inputs; asking it for the criterion in the
// same breath costs completion tokens and no prompt tokens at all, and asking
// anything else would mean paying twice to read the same thing.
//
// The last paragraph is the one that earns its place. A model told to state a
// criterion will state a generous one, and every condition it invents becomes a
// requirement the person never made — the same failure the working method's
// prompt already writes against, in the same words, because it is the same
// failure.
const criterionBlock = `
Alongside the instruction, state the criterion by which this work will be judged
finished. It is a positive statement of what must be true once the work has
landed — not a list of steps, and not the instruction said again.

Give it as a small set of independent conditions. Each one must be settleable by
someone who has the result in front of them and did not do the work. A condition
that can be settled by running something says the exact thing to run and what
its outcome must be. A condition that can only be settled by reading says what
must be present and what would make it absent.

Name the things the result must produce, by the names they will carry, so that a
reader holding only this criterion could tell whether they exist.

Write no condition the request did not ask for. A criterion that demands more
than the person asked is a criterion that cannot be met, and every condition you
invent becomes a requirement nobody made.

Answer with one bare JSON object and nothing else — no code fence around it and
no sentence before or after it — carrying the instruction and the criterion:
{"instruction": "<the instruction>", "done": {"produces": ["<name>"],
"conditions": [{"kind": "run"|"read", "check": "<what to run or look for>",
"expect": "<what its outcome must be>"}]}}`

// briefWithCriterion is the prompt as sent when the criterion is on. The
// instruction half is byte-identical to what it has always been, so the shared
// prefix every brief call in a build hits is untouched and the criterion costs
// nothing but its own completion.
const briefWithCriterion = briefPrompt + "\n" + criterionBlock

// briefSchema is closed on purpose. An open schema is what lets a criterion
// grow fields nobody reads, and the condition list is capped again in code
// (NormalizeDone) because a schema cannot say "few".
var briefSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "instruction": { "type": "string" },
    "done": {
      "type": "object",
      "properties": {
        "produces": { "type": "array", "items": { "type": "string" } },
        "conditions": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "kind":   { "type": "string", "enum": ["run", "read"] },
              "check":  { "type": "string" },
              "expect": { "type": "string" }
            },
            "required": ["kind", "check", "expect"],
            "additionalProperties": false
          }
        }
      },
      "required": ["produces", "conditions"],
      "additionalProperties": false
    }
  },
  "required": ["instruction", "done"],
  "additionalProperties": false
}`)

// briefReply is the decoded answer. done is optional in every direction that
// matters: absent, empty, or malformed all yield today's brief unchanged.
type briefReply struct {
	Instruction string `json:"instruction"`
	Done        Done   `json:"done"`
}

// decodeBrief reads what came back, and tolerates a model that ignored the
// schema entirely.
//
// A router that falls back to a provider without structured output answers in
// prose, and prose is a perfectly good instruction — it is exactly what this
// call returned before the criterion existed. So a decode failure is not a
// failure: it is the old answer, with no criterion, which is legal everywhere.
func decodeBrief(text string) (string, Done) {
	body := trim(text)
	if body == "" {
		return "", Done{}
	}
	var reply briefReply
	if err := decodeJSON(body, &reply); err != nil {
		return body, Done{}
	}
	instruction := trim(reply.Instruction)
	if instruction == "" {
		// Valid JSON with nothing in the field it required. The text itself is
		// not an instruction either — handing a worker a JSON object as its
		// brief is worse than handing it nothing — so this is the empty answer
		// the caller already knows how to report.
		return "", Done{}
	}
	return instruction, NormalizeDone(reply.Done)
}

// synthesisBrief is the gathering node's instruction, and the harness writes it
// rather than buying one. A synthesis has no subject of its own — it is always
// the same act performed on whatever arrived — so there is nothing for a model
// to learn about it that these sentences do not already say.
const synthesisBrief = "Several separate pieces of work have been completed and their results are above. " +
	"Bring them together into the one finished outcome the goal asked for. " +
	"Where they disagree, resolve it explicitly rather than averaging it away. " +
	"Where they have been done separately and now need to work as a whole, make that so. " +
	"Do not redo work that is already finished — everything you need is above or in the " +
	"files it names. If the outcome is a document, write it out; if it is something that " +
	"has to work, check that it does."

// ComposedBrief is a node's instruction written from what the plan already knows
// about it, for the nodes no model wrote one for.
//
// Two roads reach this and they have to arrive at the same words. A graph
// planned with briefs turned off is dispatched carrying none, and the scheduler
// composes one at the moment it hands the job over; a brief call that would not
// answer twice is composed for here, while the plan is still being built. A
// second composition written for the second road would be a second answer to
// "what does a node say when nobody wrote its instruction", which is precisely
// the drift the one-source-of-truth law is about.
//
// The goal is deliberately absent from it. Every leaf is already handed the goal
// beside its brief — "This work is part of a larger goal" — and a composition
// that repeated it would put the same paragraph into one prompt twice.
func ComposedBrief(node Node) string {
	if node.Kind == KindSynthesis {
		return synthesisBrief
	}
	var composed strings.Builder
	if title := trim(node.Title); title != "" {
		composed.WriteString(title + "\n\n")
	}
	if summary := trim(node.Summary); summary != "" {
		composed.WriteString(summary + "\n")
	}
	// The sources are the one fact the summary reliably leaves out, and they are
	// the difference between an agent that opens the right file and one that
	// goes looking for it.
	if len(node.Sources) > 0 {
		fmt.Fprintf(&composed, "\nThis work is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
	return trim(composed.String())
}

// BriefJournal writes one node's rendered brief as a first-class, queryable
// event, when the build has a durable home to journal to. The plan package
// knows the plan node id and the rendered brief; it does not know the store id
// the node was minted under (that spelling is the caller's — see
// resident.PlanStoreIDs), so the callback receives the graph and the plan node
// id and the caller forms the store id. It is best-effort for the same reason
// RecordPlanGraph is: losing it costs an audit and never the plan.
type BriefJournal func(graph *Graph, nodeID int, brief store.NodeBrief)

// briefResult is one node's finished brief and the account of how it was
// arrived at. fault is empty on the ordinary node, whose instruction a model
// wrote; on a node whose call would not answer it carries the reason, the
// instruction beside it is composed, and the reason is journaled rather than
// returned. The two travel together because a brief written and a brief
// composed are the same object to every reader downstream and different objects
// to anyone reading the run back afterwards.
type briefResult struct {
	briefReply
	fault string
}

// briefWriter writes leaf instructions in the background.
//
// Briefs used to run as a final pass over the finished graph, which put one
// call's latency on the end of every plan for work that was decided long
// before. A node's brief depends only on that node and its inputs, so the call
// can start the moment the node stops changing — while other nodes are still
// being expanded — and a settled node then arrives with its instruction already
// written.
//
// Results are collected here and written into the graph only at the end, on the
// caller's goroutine. Writing them as they arrive would mean touching g.Nodes
// while expansion is still appending to it, which is the exact aliasing hazard
// that already cost us a duplicated subtree.
type briefWriter struct {
	ctx      context.Context
	client   Completer
	enabled  bool
	progress Progress

	// journal, when set, writes one node_briefed event per briefed node as
	// apply lands its brief. Nil is the build that has no store to journal to
	// — the one-shot `aforge plan` command, the batch `Briefs` form — and
	// leaves the brief exactly as durable as it was before this hook existed.
	journal BriefJournal

	// sink is the deliverable owner, which is written for even though it is not
	// KindWork. It is a single id rather than a predicate because every other
	// non-work node in a graph is an expanded container — structure nobody runs —
	// and launching a brief for those would buy a call per container.
	sink int

	group     sync.WaitGroup
	mutex     sync.Mutex
	results   map[int]briefResult
	usage     Usage
	errs      []error
	launched  int
	completed int
	// completions holds titles in actual completion order until the final leaf
	// total is known. apply replays them with honest counts instead of dropping
	// fast background work from the materializing plan.
	completions []string
	base        int
	total       int
	reporting   bool
}

func newBriefWriter(ctx context.Context, client Completer, enabled bool, progress Progress, journal BriefJournal) *briefWriter {
	return &briefWriter{ctx: ctx, client: client, enabled: enabled, progress: progress, journal: journal, results: map[int]briefResult{}}
}

// launch starts one node's brief. Everything it needs is passed by value —
// including the ownership line, which is read off the graph by the caller — so
// the goroutine never reads the graph while the graph is being modified.
func (w *briefWriter) launch(shared string, node Node, inputs []string, deliverable string) {
	if !w.enabled || (node.Kind != KindWork && node.ID != w.sink) {
		return
	}
	w.mutex.Lock()
	w.launched++
	w.mutex.Unlock()
	w.group.Add(1)
	go func() {
		defer w.group.Done()
		// apply waits on this group and reads what it left behind, so a fault
		// has to record itself the way a failed call does: an error in errs and
		// no brief for the node, which leaves the leaf to the generic loop.
		defer func() {
			if recovered := recover(); recovered != nil {
				fault := guard.Note(fmt.Sprintf("plan/brief node %d", node.ID), recovered)
				w.mutex.Lock()
				defer w.mutex.Unlock()
				w.errs = append(w.errs, fault)
				w.completed++
			}
		}()
		brief, done, usage, err := writeBrief(w.ctx, w.client, shared, node, inputs, deliverable)
		// One entry per call actually made, because Usage.Add counts a call
		// whether or not the provider returned any numbers with it.
		spent := []*ai.Usage{usage}
		if err != nil {
			// One free retry, on the same bytes. This is the discipline the
			// ground and fan-out passes already keep and not a new one: the ask
			// was right and the reply was not an answer, so what is worth
			// asking again is the same question rather than a different one.
			var retry *ai.Usage
			brief, done, retry, err = writeBrief(w.ctx, w.client, shared, node, inputs, deliverable)
			spent = append(spent, retry)
		}
		written := briefResult{briefReply: briefReply{Instruction: brief, Done: done}}
		if err != nil {
			// THE LAW: STRUCTURE THE PLANNER HAS ALREADY FOUND IS NEVER
			// DISCARDED FOR A DOWNSTREAM FAULT. A brief is one leaf's
			// instruction; it is not the graph's right to exist. A six-node
			// plan was thrown away for one node's reply that stopped being
			// language, and the whole job then ran as a single oversized
			// worker. So the node keeps an instruction composed from what the
			// plan already knows about it, and the reason the call would not
			// answer is journaled against that node instead of returned.
			written = briefResult{briefReply: briefReply{Instruction: ComposedBrief(node)}, fault: err.Error()}
		}
		w.mutex.Lock()
		defer w.mutex.Unlock()
		for _, usage := range spent {
			w.usage.Add(usage)
		}
		w.results[node.ID] = written
		w.completed++
		// The node is briefed either way, so the row names it either way. A gap
		// here used to be the only sign a call had failed, which read on the
		// stream as a node that had simply not finished yet.
		latest := nodeProgressTitle(node)
		if w.reporting && w.progress != nil {
			emitProgress(w.progress, "briefs", fmt.Sprintf("%d/%d", w.base+w.completed, w.total), latest)
		} else {
			w.completions = append(w.completions, latest)
		}
	}()
}

// apply waits for every brief and writes them in.
func (w *briefWriter) apply(graph *Graph) (Usage, error) {
	if w.enabled {
		w.mutex.Lock()
		w.reporting = true
		w.total = len(graph.writtenLeaves())
		w.base = w.total - w.launched
		if w.progress != nil {
			if len(w.completions) == 0 {
				emitProgress(w.progress, "briefs", fmt.Sprintf("%d/%d", w.base, w.total), "")
			}
			for index, latest := range w.completions {
				emitProgress(w.progress, "briefs", fmt.Sprintf("%d/%d", w.base+index+1, w.total), latest)
			}
		}
		w.completions = nil
		w.mutex.Unlock()
	}
	w.group.Wait()
	w.mutex.Lock()
	defer w.mutex.Unlock()
	for id, written := range w.results {
		if node := graph.Node(id); node != nil {
			// Dual-write. Brief is what every reader still reads; the spec is
			// written beside it so the object exists to be carried forward, and
			// so the swap that makes it the read is one line per reader.
			node.Brief = written.Instruction
			node.Spec.Instruction = written.Instruction
			node.Spec.Done = written.Done
			node.Spec.Sources = node.Sources
			// Journal the rendered brief as a first-class event per node, so a
			// run's sufficiency sentence is queryable from its own artifacts
			// rather than only as a field inside the plan blob. The caller forms
			// the store id; this is a no-op when no journal was wired in.
			if w.journal != nil {
				w.journal(graph, id, store.NodeBrief{
					Node:      id,
					Brief:     written.Instruction,
					Criterion: written.Done.Sentence(),
					// Why no model wrote this one, on the nodes where none did.
					// It is the only place an autopsy can tell a composed brief
					// from a written one after the fact.
					Fault:      written.fault,
					Subharness: node.Subharness,
				})
			}
		}
	}
	return w.usage, joinErrors(w.errs)
}

// Briefs writes an instruction for every leaf that lacks one, all at once. The
// planner writes briefs in the background as nodes settle; this is the batch
// form, for a graph that was planned without them and is about to be executed.
func Briefs(ctx context.Context, client Completer, graph *Graph, callbacks ...Progress) (Usage, error) {
	var callback Progress
	if len(callbacks) > 0 {
		callback = serialProgress(callbacks[0])
	}
	writer := newBriefWriter(ctx, client, true, callback, nil)
	writer.sink = graph.deliverableSink()
	shared := graph.context() + "\nThe full plan:\n" + graph.briefCatalog()
	owner, label := graph.deliverableOwner()
	for _, id := range graph.writtenLeaves() {
		node := graph.Node(id)
		if node == nil || strings.TrimSpace(node.Brief) != "" {
			continue
		}
		var inputs []string
		for _, need := range node.Needs {
			if source := graph.Node(need); source != nil {
				inputs = append(inputs, fmt.Sprintf("%q (%s)", source.Title, source.Summary))
			}
		}
		writer.launch(shared, *node, inputs, deliverableLineFor(owner, label, node.ID, graph.FileShaped))
	}
	return writer.apply(graph)
}

// deliverableLine tells one node whether the goal's final deliverable is its to
// produce. Every node is told, and only one is told yes: an agent that is not
// the owner has to be told so explicitly, because the goal it also receives
// names the deliverable and reads as an instruction to build it.
func (g *Graph) deliverableLine(nodeID int) string {
	owner, label := g.deliverableOwner()
	return deliverableLineFor(owner, label, nodeID, g.FileShaped)
}

// deliverableLineFor is that line written from an ownership answer that has
// already been worked out. Working it out means finding the sinks, which walks
// every node's needs, and the answer is one fact about the whole graph rather
// than a fact about the node — so a caller writing a line for every node in a
// round resolves it once and spends the walk once instead of per node.
//
// fileShaped is the delivery law's carve-out (see delivery.go). False is what a
// caller that has not made the judgment passes, and it renders the line this
// pass has always rendered, byte for byte.
func deliverableLineFor(owner int, label string, nodeID int, fileShaped bool) string {
	if owner == nodeID {
		// Owning it and handing it over are two different facts, and only the
		// first used to be stated. An agent told it owns the deliverable and
		// nothing more can own it into a file and reply with the path, which
		// reads as ownership and delivers nothing — so the instruction is told
		// to say where the finished thing has to appear.
		//
		// Unless the ask itself named the file, in which case saying where it is
		// IS the delivery, and the law that applies is the other half of
		// DeliveryLaw rather than a weaker version of this one.
		if fileShaped {
			return "This node owns the final deliverable the goal asks for: it is the only " +
				"one that produces it, and the other results arrive here as inputs. Hold the instruction " +
				"you write to this:\n\n" + DeliverToNamedFile + "\n"
		}
		return "This node owns the final deliverable the goal asks for: it is the only " +
			"one that produces it, and the other results arrive here as inputs. Tell it to put that " +
			"finished deliverable in its own reply, written out in full, rather than describing it or " +
			"saying where it can be found.\n"
	}
	return fmt.Sprintf("The final deliverable the goal asks for — whatever single file, report or "+
		"document it names — is produced by %s, not here. This node produces its own result "+
		"and hands it over.\n", label)
}

func writeBrief(ctx context.Context, client Completer, shared string, node Node, inputs []string, deliverable string) (string, Done, *ai.Usage, error) {
	var target strings.Builder
	fmt.Fprintf(&target, "Write the instruction for node %d, %q: %s\n", node.ID, node.Title, node.Summary)
	if len(node.Sources) > 0 {
		fmt.Fprintf(&target, "It is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
	target.WriteString(deliverable)
	if len(inputs) > 0 {
		fmt.Fprintf(&target, "It will already have the results of: %s\n", strings.Join(inputs, "; "))
	} else {
		target.WriteString("It receives no input from other work — it starts from nothing but your instruction.\n")
	}

	system := briefPrompt
	// NO CEILING TRAVELS. A brief used to go out under the same derived room as
	// every other planning call; that room is no longer a request this harness
	// makes of anybody's model (internal/shaped), and a brief is prose the
	// prompt already bounds.
	var options []ai.Option
	if Criterion {
		system = briefWithCriterion
		options = append(options, ai.WithSchema(briefSchema))
	}
	messages := []ai.Message{
		systemMessage(system),
		userMessage(shared),
		userMessage(target.String()),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanBrief)
	// WHICH NODE THIS CALL BELONGS TO, for the reason the contract pass names
	// its own: the briefs of a stage are written concurrently and are otherwise
	// indistinguishable in the log.
	ctx = provider.WithCallNode(ctx, callNodeKey(node.ID))
	response, err := client.CompleteWithMessages(ctx, messages, options...)
	if err != nil {
		provider.Report(ctx, provider.VerdictProviderFailure)
		return "", Done{}, nil, fmt.Errorf("brief %q: %w", node.Title, err)
	}
	brief, done := decodeBrief(response.Text())
	if !Criterion {
		brief, done = trim(response.Text()), Done{}
	}
	if brief == "" {
		// Nothing at all came back. On a reasoning model the usual cause is the
		// whole budget going to private deliberation, which is a different fact
		// about the model than a badly written instruction and is worth naming.
		provider.Report(ctx, provider.VerdictEmptyResponse)
		return "", Done{}, usageOf(response), annotate(fmt.Errorf("brief %q: empty response", node.Title), response)
	}
	// A brief is prose. There is no schema to check it against and nothing cheap
	// that can say whether it is a good instruction, so this is exactly the case
	// the unverified verdict exists for: output that worked, evidence that does
	// not move a rating.
	provider.Report(ctx, provider.VerdictUnverifiedSuccess)
	return brief, done, usageOf(response), nil
}

// briefCatalog lists every node by title only. Titles are enough to hold a
// boundary — that is what stops two agents writing the same thing — and
// summaries would multiply the shared prefix without changing any instruction.
func (g *Graph) briefCatalog() string {
	var block strings.Builder
	for _, node := range g.Nodes {
		if node.Kind == KindSynthesis {
			continue
		}
		fmt.Fprintf(&block, "  %d. %s\n", node.ID, node.Title)
	}
	return block.String()
}
