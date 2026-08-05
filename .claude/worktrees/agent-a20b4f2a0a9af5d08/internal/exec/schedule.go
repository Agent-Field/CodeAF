package exec

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// Scheduler drives a graph to completion.
//
// It is dependency-driven rather than wave-locked. Waves are a way to describe
// a schedule, not a way to run one: waiting for the slowest node of a wave
// before starting anything in the next holds back work whose inputs are already
// finished. A node starts the moment the specific nodes it named are done, which
// is what the whole binding pass was for.
type Scheduler struct {
	registry    *Registry
	workspace   *Workspace
	concurrency int
	usage       Usage

	// OnEvent reports state changes as they happen. A run is long and mostly
	// invisible; without this the only feedback is silence followed by a graph.
	OnEvent func(Event)
}

// Event is one thing happening to one node.
type Event struct {
	NodeID  int
	Title   string
	State   plan.State
	Detail  string
	Elapsed time.Duration
}

func NewScheduler(registry *Registry, workspace *Workspace, concurrency int) *Scheduler {
	if concurrency <= 0 {
		concurrency = 8
	}
	return &Scheduler{registry: registry, workspace: workspace, concurrency: concurrency}
}

// Usage is the total cost of the run, summed as outcomes are applied. It is
// accumulated on the scheduler's own goroutine along with everything else that
// touches the graph, so no worker ever writes it.
func (s *Scheduler) Usage() Usage { return s.usage }

// Run executes every runnable node in the graph and records results onto it.
//
// The graph is mutated in place and never concurrently: workers return outcomes
// through a channel and the scheduler applies them on its own goroutine. That
// keeps the one shared structure single-writer, which matters more here than
// anywhere else — appending to g.Nodes while a worker held a *Node has already
// cost us a duplicated subtree once.
func (s *Scheduler) Run(ctx context.Context, graph *plan.Graph) error {
	started := time.Now()
	type completion struct {
		nodeID  int
		outcome *Outcome
		err     error
	}
	done := make(chan completion)
	slots := make(chan struct{}, s.concurrency)
	inFlight := 0

	for {
		// Anything whose inputs all failed can never run; retiring it before
		// looking for work stops the loop spinning on nodes that will never
		// become ready.
		s.propagateBlocked(graph)

		ready := s.ready(graph)
		for _, id := range ready {
			node := graph.Node(id)
			node.State = plan.StateRunning
			s.emit(Event{NodeID: id, Title: node.Title, State: plan.StateRunning, Elapsed: time.Since(started)})
			task := s.taskFor(graph, node)
			inFlight++
			go func(id int, task Task) {
				slots <- struct{}{}
				defer func() { <-slots }()
				outcome, err := s.registry.For("linear").Run(ctx, task)
				done <- completion{nodeID: id, outcome: outcome, err: err}
			}(id, task)
		}

		// Nothing running and nothing newly runnable means everything that can
		// be done has been.
		if inFlight == 0 {
			return nil
		}

		select {
		case finished := <-done:
			inFlight--
			s.apply(graph, finished.nodeID, finished.outcome, finished.err, started)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ready lists pending nodes whose inputs are all done.
func (s *Scheduler) ready(graph *plan.Graph) []int {
	var ids []int
	for _, node := range graph.Nodes {
		if node.State != plan.StatePending {
			continue
		}
		runnable := true
		for _, need := range node.Needs {
			if source := graph.Node(need); source == nil || source.State != plan.StateDone {
				runnable = false
				break
			}
		}
		if runnable {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

// propagateBlocked marks the descendants of a failure. One failed node should
// cost its own subtree and nothing else: everything independent still runs, and
// the report can say exactly what was lost rather than that the run died.
func (s *Scheduler) propagateBlocked(graph *plan.Graph) {
	for changed := true; changed; {
		changed = false
		for index := range graph.Nodes {
			node := &graph.Nodes[index]
			if node.State != plan.StatePending {
				continue
			}
			for _, need := range node.Needs {
				source := graph.Node(need)
				if source != nil && (source.State == plan.StateFailed || source.State == plan.StateBlocked) {
					node.State = plan.StateBlocked
					node.Failure = fmt.Sprintf("input %d (%s) did not complete", source.ID, source.Title)
					changed = true
					break
				}
			}
		}
	}
}

// taskFor assembles one node's assignment. The inputs are exactly its declared
// dependencies — this is the moment the edge list stops being a schedule and
// becomes a context router.
func (s *Scheduler) taskFor(graph *plan.Graph, node *plan.Node) Task {
	task := Task{
		NodeID:     node.ID,
		Title:      node.Title,
		Goal:       graph.Goal,
		Brief:      node.Brief,
		Contract:   node.Contract,
		OutputHint: SuggestPath(node.ID, node.Title),
	}
	if strings.TrimSpace(task.Brief) == "" {
		task.Brief = fallbackBrief(node)
	}
	for _, need := range node.Needs {
		source := graph.Node(need)
		if source == nil {
			continue
		}
		task.Inputs = append(task.Inputs, Input{
			Title:     source.Title,
			Summary:   source.Summary,
			Result:    boundInput(source.Result, source.Artifacts),
			Artifacts: source.Artifacts,
		})
	}
	return task
}

// maxInputBytes bounds one upstream result as it is routed downstream.
//
// This is the roll-up half of the context problem and it multiplies worse than
// the loop's own: an input sits in the opening prompt, so it is resent on every
// turn the consumer takes. A node with five long inputs pays for all five, every
// turn, before it has done anything. The full text is never lost — it is in the
// artifact the producer wrote, one `sh` call away.
const maxInputBytes = 6 << 10

func boundInput(result string, artifacts []string) string {
	if len(result) <= maxInputBytes {
		return result
	}
	pointer := "the file it wrote"
	if len(artifacts) > 0 {
		pointer = strings.Join(artifacts, ", ")
	}
	return result[:maxInputBytes] + fmt.Sprintf(
		"\n\n... [truncated at %d of %d bytes — the complete version is in %s]",
		maxInputBytes, len(result), pointer)
}

// fallbackBrief covers nodes the planner never wrote an instruction for.
// Synthesis nodes are the normal case: the harness owns them, so it owns their
// instruction too rather than asking a model to invent one.
func fallbackBrief(node *plan.Node) string {
	if node.Kind == plan.KindSynthesis {
		return "Several separate pieces of work have been completed and their results are above. " +
			"Bring them together into the one finished outcome the goal asked for. " +
			"Where they disagree, resolve it explicitly rather than averaging it away. " +
			"Where they have been done separately and now need to work as a whole, make that so. " +
			"Do not redo work that is already finished — everything you need is above or in the " +
			"files it names. If the outcome is a document, write it out; if it is something that " +
			"has to work, check that it does."
	}
	return node.Summary
}

func (s *Scheduler) apply(graph *plan.Graph, nodeID int, outcome *Outcome, err error, started time.Time) {
	node := graph.Node(nodeID)
	if node == nil {
		return
	}
	if outcome != nil {
		s.usage.merge(outcome.Usage)
		node.Turns = outcome.Turns
		node.Tokens = outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
		node.Cost = outcome.Usage.Cost
		node.Stop = string(outcome.Stop)
		node.Artifacts = outcome.Artifacts
		node.Result = outcome.Text
	}
	if err != nil || outcome == nil || strings.TrimSpace(node.Result) == "" {
		node.State = plan.StateFailed
		node.Failure = "produced no result"
		if err != nil {
			node.Failure = err.Error()
		}
		s.emit(Event{NodeID: nodeID, Title: node.Title, State: plan.StateFailed, Detail: node.Failure, Elapsed: time.Since(started)})
		return
	}
	node.State = plan.StateDone
	detail := fmt.Sprintf("%d turns, %dk tok", outcome.Turns, node.Tokens/1000)
	switch outcome.Stop {
	case StopTurnCap:
		detail += ", hit the iteration backstop — the leaf could not converge"
	case StopBudget:
		// Worth surfacing rather than burying: a leaf that exhausted its token
		// budget is evidence the sizing anchors let too much into one node.
		detail += ", exhausted its token budget — the leaf was too large"
	}
	if outcome.Decayed > 0 {
		detail += fmt.Sprintf(", %d observations faded", outcome.Decayed)
	}
	if len(outcome.Artifacts) > 0 {
		detail += ", wrote " + strings.Join(outcome.Artifacts, ", ")
	}
	s.emit(Event{NodeID: nodeID, Title: node.Title, State: plan.StateDone, Detail: detail, Elapsed: time.Since(started)})
}

func (s *Scheduler) emit(event Event) {
	if s.OnEvent != nil {
		s.OnEvent(event)
	}
}
