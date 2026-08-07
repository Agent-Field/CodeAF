package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/provider"
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
// The third is narrower and was the most expensive. The goal reaches every
// agent, so a goal naming a deliverable reads to all of them as an instruction
// to produce it — five nodes once wrote the same REVIEW.md over the top of each
// other. The graph already knows who owns it, so the brief is told, and every
// non-owner is told in words that its result is handed over instead.
const briefPrompt = `You write the instruction that one agent receives.

That agent works alone, in order, with tools. It sees only what you write — not
the wider goal, not the plan, not the other agents' work — and it cannot ask
anyone anything. Whatever you leave out is simply missing.

Write directly to it:
- Give it the context it needs to make sense of the job on its own.
- State exactly what it is responsible for delivering.
- Say what finished looks like, and where to stop.
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

	group     sync.WaitGroup
	mutex     sync.Mutex
	results   map[int]string
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

func newBriefWriter(ctx context.Context, client Completer, enabled bool, progress Progress) *briefWriter {
	return &briefWriter{ctx: ctx, client: client, enabled: enabled, progress: progress, results: map[int]string{}}
}

// launch starts one node's brief. Everything it needs is passed by value —
// including the ownership line, which is read off the graph by the caller — so
// the goroutine never reads the graph while the graph is being modified.
func (w *briefWriter) launch(shared string, node Node, inputs []string, deliverable string) {
	if !w.enabled || node.Kind != KindWork {
		return
	}
	w.mutex.Lock()
	w.launched++
	w.mutex.Unlock()
	w.group.Add(1)
	go func() {
		defer w.group.Done()
		brief, usage, err := writeBrief(w.ctx, w.client, shared, node, inputs, deliverable)
		w.mutex.Lock()
		defer w.mutex.Unlock()
		w.usage.Add(usage)
		if err != nil {
			w.errs = append(w.errs, err)
		} else {
			w.results[node.ID] = brief
		}
		w.completed++
		if w.reporting && w.progress != nil {
			latest := ""
			if err == nil {
				latest = nodeProgressTitle(node)
			}
			emitProgress(w.progress, "briefs", fmt.Sprintf("%d/%d", w.base+w.completed, w.total), latest)
		} else {
			latest := ""
			if err == nil {
				latest = nodeProgressTitle(node)
			}
			w.completions = append(w.completions, latest)
		}
	}()
}

// apply waits for every brief and writes them in.
func (w *briefWriter) apply(graph *Graph) (Usage, error) {
	if w.enabled {
		w.mutex.Lock()
		w.reporting = true
		w.total = len(graph.Leaves())
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
	for id, brief := range w.results {
		if node := graph.Node(id); node != nil {
			node.Brief = brief
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
	writer := newBriefWriter(ctx, client, true, callback)
	shared := graph.context() + "\nThe full plan:\n" + graph.briefCatalog()
	owner, label := graph.deliverableOwner()
	for _, id := range graph.Leaves() {
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
		writer.launch(shared, *node, inputs, deliverableLineFor(owner, label, node.ID))
	}
	return writer.apply(graph)
}

// deliverableLine tells one node whether the goal's final deliverable is its to
// produce. Every node is told, and only one is told yes: an agent that is not
// the owner has to be told so explicitly, because the goal it also receives
// names the deliverable and reads as an instruction to build it.
func (g *Graph) deliverableLine(nodeID int) string {
	owner, label := g.deliverableOwner()
	return deliverableLineFor(owner, label, nodeID)
}

// deliverableLineFor is that line written from an ownership answer that has
// already been worked out. Working it out means finding the sinks, which walks
// every node's needs, and the answer is one fact about the whole graph rather
// than a fact about the node — so a caller writing a line for every node in a
// round resolves it once and spends the walk once instead of per node.
func deliverableLineFor(owner int, label string, nodeID int) string {
	if owner == nodeID {
		return "This node owns the final deliverable the goal asks for: it is the only " +
			"one that produces it, and the other results arrive here as inputs.\n"
	}
	return fmt.Sprintf("The final deliverable the goal asks for — whatever single file, report or "+
		"document it names — is produced by %s, not here. This node produces its own result "+
		"and hands it over.\n", label)
}

func writeBrief(ctx context.Context, client Completer, shared string, node Node, inputs []string, deliverable string) (string, *ai.Usage, error) {
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

	messages := []ai.Message{
		systemMessage(briefPrompt),
		userMessage(shared),
		userMessage(target.String()),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanBrief)
	response, err := client.CompleteWithMessages(ctx, messages)
	if err != nil {
		provider.Report(ctx, provider.VerdictProviderFailure)
		return "", nil, fmt.Errorf("brief %q: %w", node.Title, err)
	}
	brief := trim(response.Text())
	if brief == "" {
		// Nothing at all came back. On a reasoning model the usual cause is the
		// whole budget going to private deliberation, which is a different fact
		// about the model than a badly written instruction and is worth naming.
		provider.Report(ctx, provider.VerdictEmptyResponse)
		return "", usageOf(response), annotate(fmt.Errorf("brief %q: empty response", node.Title), response)
	}
	// A brief is prose. There is no schema to check it against and nothing cheap
	// that can say whether it is a good instruction, so this is exactly the case
	// the unverified verdict exists for: output that worked, evidence that does
	// not move a rating.
	provider.Report(ctx, provider.VerdictUnverifiedSuccess)
	return brief, usageOf(response), nil
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
