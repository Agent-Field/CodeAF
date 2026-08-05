package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// briefPrompt writes the instruction a leaf is actually handed.
//
// This is the boundary of the whole system: everything above it is structure,
// and this is where structure turns into something an agent can run. The agent
// on the other side sees nothing but these words — not the goal, not the graph,
// not its neighbours — so anything the instruction leaves out is simply absent.
//
// Two failure modes are worth writing against. An instruction that assumes
// context produces an agent that invents it; and an instruction without
// explicit boundaries produces agents that all drift toward the same
// interesting middle of the problem, which is how a wide graph collapses back
// into duplicated work.
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
	ctx     context.Context
	client  Completer
	enabled bool

	group   sync.WaitGroup
	mutex   sync.Mutex
	results map[int]string
	usage   Usage
	errs    []error
}

func newBriefWriter(ctx context.Context, client Completer, enabled bool) *briefWriter {
	return &briefWriter{ctx: ctx, client: client, enabled: enabled, results: map[int]string{}}
}

// launch starts one node's brief. Everything it needs is passed by value, so
// the goroutine never reads the graph while the graph is being modified.
func (w *briefWriter) launch(shared string, node Node, inputs []string) {
	if !w.enabled || node.Kind != KindWork {
		return
	}
	w.group.Add(1)
	go func() {
		defer w.group.Done()
		brief, usage, err := writeBrief(w.ctx, w.client, shared, node, inputs)
		w.mutex.Lock()
		defer w.mutex.Unlock()
		w.usage.Add(usage)
		if err != nil {
			w.errs = append(w.errs, err)
			return
		}
		w.results[node.ID] = brief
	}()
}

// apply waits for every brief and writes them in.
func (w *briefWriter) apply(graph *Graph) (Usage, error) {
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
func Briefs(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	writer := newBriefWriter(ctx, client, true)
	shared := graph.context() + "\nThe full plan:\n" + graph.briefCatalog()
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
		writer.launch(shared, *node, inputs)
	}
	return writer.apply(graph)
}

func writeBrief(ctx context.Context, client Completer, shared string, node Node, inputs []string) (string, *ai.Usage, error) {
	var target strings.Builder
	fmt.Fprintf(&target, "Write the instruction for node %d, %q: %s\n", node.ID, node.Title, node.Summary)
	if len(node.Sources) > 0 {
		fmt.Fprintf(&target, "It is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
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
	response, err := client.CompleteWithMessages(ctx, messages)
	if err != nil {
		return "", nil, fmt.Errorf("brief %q: %w", node.Title, err)
	}
	brief := trim(response.Text())
	if brief == "" {
		return "", usageOf(response), annotate(fmt.Errorf("brief %q: empty response", node.Title), response)
	}
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
