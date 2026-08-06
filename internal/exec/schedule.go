package exec

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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

	// Budget bounds the whole run's token spend, in prompt+completion tokens.
	// Zero means unbounded — each leaf still has its own budget. When the
	// cumulative spend passes it, no new node is launched; whatever is in
	// flight lands (mirroring the per-leaf landing reserve), never-started
	// nodes are marked, and Run reports the stop reason.
	Budget int

	// Escalations is how many times a failed leaf may be re-run on a stronger
	// model. Zero — the default — is exactly today's behaviour: a leaf that
	// fails, fails. It is only worth setting when there is somewhere stronger to
	// go, so the caller sets it from the panel rather than the scheduler
	// assuming one exists.
	//
	// Only verdicts that a better model could plausibly fix count: running out
	// of budget, running out of turns, returning nothing at all. A provider
	// failure is weather and a rate limit is not cured by spending more.
	Escalations int

	// NodeTimeout is the watchdog on a single node. The executor has its own
	// deadline, so this only fires when an executor is wedged past every
	// deadline it was given — a hung pipe, a stuck transport. The node is
	// recorded as failed and abandoned rather than letting one stuck goroutine
	// freeze the run silently and forever. Zero disables it.
	NodeTimeout time.Duration

	// OnEvent reports state changes as they happen. A run is long and mostly
	// invisible; without this the only feedback is silence followed by a graph.
	OnEvent func(Event)
}

// stallAfter is how long the run may go without a completion before the
// scheduler says which nodes it is still waiting on. A wedged provider call
// once froze the log for thirteen minutes mid-run; the only thing worse than
// the stall was that it was indistinguishable from the process having died.
const stallAfter = 3 * time.Minute

// drainGrace bounds how long a stopping run waits for in-flight nodes to
// land. Their contexts are already cancelled, so an honest executor returns
// in seconds; anything still out after this is recorded and abandoned.
const drainGrace = 30 * time.Second

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
	// Buffered to the whole graph so a worker can always deliver its result
	// and exit, even after the scheduler has stopped listening for it — an
	// abandoned worker blocked on an unbuffered send would leak forever.
	done := make(chan completion, len(graph.Nodes))
	// When a node started, keyed by id. Doubles as the in-flight set.
	inFlight := map[int]time.Time{}
	// How many times each node has already been given up on. It is also the
	// attempt number the leaf runs under, which is how a router is told to climb
	// without the scheduler knowing what it is climbing.
	retries := map[int]int{}
	// Why launching stopped. Once set, nothing new starts, in-flight work
	// lands, and Run reports it — a run may stop early, but it must never
	// stop silently.
	var stop string
	lastProgress := time.Now()

	ticker := time.NewTicker(s.tickEvery())
	defer ticker.Stop()

	for {
		// Anything whose inputs all failed can never run; retiring it before
		// looking for work stops the loop spinning on nodes that will never
		// become ready.
		s.propagateBlocked(graph)

		if stop == "" && s.Budget > 0 && s.usage.PromptTokens+s.usage.CompletionTokens >= s.Budget {
			stop = fmt.Sprintf("global budget exhausted: %d of %d tokens spent",
				s.usage.PromptTokens+s.usage.CompletionTokens, s.Budget)
		}
		if stop == "" {
			for _, id := range s.ready(graph) {
				if len(inFlight) >= s.concurrency {
					break
				}
				node := graph.Node(id)
				node.State = plan.StateRunning
				s.emit(Event{NodeID: id, Title: node.Title, State: plan.StateRunning, Elapsed: time.Since(started)})
				task := s.taskFor(graph, node)
				inFlight[id] = time.Now()
				go s.work(ctx, id, task, retries[id], leafShape(node), done)
			}
		}

		// Nothing running and nothing newly runnable means everything that can
		// be done has been — or that the run was told to stop and the last
		// in-flight node has landed. A cancellation that emptied the in-flight
		// set through the nodes' own child contexts still counts as a stop:
		// the race between a node landing cancelled and the scheduler seeing
		// ctx.Done must not decide whether the reason gets reported.
		if len(inFlight) == 0 {
			if stop == "" && ctx.Err() != nil {
				stop = fmt.Sprintf("run context cancelled (%v)", ctx.Err())
			}
			return s.finish(graph, stop)
		}

		select {
		case finished := <-done:
			delete(inFlight, finished.nodeID)
			lastProgress = time.Now()
			s.apply(graph, finished.nodeID, finished.outcome, finished.err, started, retries)
		case <-ctx.Done():
			// The run context being cancelled — an interrupt, an operator
			// deadline — is a stop, not a vanishing act. In-flight nodes hold
			// child contexts that are already cancelled, so they are drained
			// with a bounded grace and their outcomes recorded before the
			// stop is reported.
			if stop == "" {
				stop = fmt.Sprintf("run context cancelled (%v)", ctx.Err())
			}
			s.drain(graph, done, inFlight, started)
			return s.finish(graph, stop)
		case <-ticker.C:
			now := time.Now()
			for id, since := range inFlight {
				if s.NodeTimeout > 0 && now.Sub(since) > s.NodeTimeout {
					node := graph.Node(id)
					node.State = plan.StateFailed
					node.Failure = fmt.Sprintf("executor did not return within %s; abandoned", s.NodeTimeout.Round(time.Second))
					s.emit(Event{NodeID: id, Title: node.Title, State: plan.StateFailed, Detail: node.Failure, Elapsed: time.Since(started)})
					delete(inFlight, id)
				}
			}
			// A silent run is indistinguishable from a dead one. When nothing
			// has completed for a while, say what is still out and for how
			// long, so a frozen log reads as waiting rather than as death.
			if len(inFlight) > 0 && now.Sub(lastProgress) >= stallAfter {
				lastProgress = now
				for id, since := range inFlight {
					node := graph.Node(id)
					s.emit(Event{NodeID: id, Title: node.Title, State: plan.StateRunning,
						Detail:  fmt.Sprintf("still in flight after %s — the run is waiting, not dead", now.Sub(since).Round(time.Second)),
						Elapsed: time.Since(started)})
				}
			}
		}
	}
}

// completion is one worker's report back to the scheduler's goroutine.
type completion struct {
	nodeID  int
	outcome *Outcome
	err     error
}

// work runs one node and always reports back, even when the executor panics —
// a panic that unwinds a worker silently would strand the scheduler waiting on
// a completion that can never come.
func (s *Scheduler) work(ctx context.Context, id int, task Task, attempt int, shape string, done chan<- completion) {
	defer func() {
		if recovered := recover(); recovered != nil {
			done <- completion{nodeID: id, err: fmt.Errorf("executor panicked: %v", recovered)}
		}
	}()
	// One leaf is one routable unit, opened here rather than inside the loop.
	// Every turn the executor takes belongs to this slot, so a router picks a
	// model once and the whole transcript stays on it — a leaf that changed
	// model mid-loop would rewrite its prefix cache every turn and splice two
	// lineages into one conversation.
	ctx = provider.WithCallShape(ctx, provider.ClassExecLeaf, attempt, shape)
	outcome, err := s.registry.For("linear").Run(ctx, task)
	done <- completion{nodeID: id, outcome: outcome, err: err}
}

// leafShape says which population of leaves this node belongs to, so that what a
// router learns about one kind of leaf is not applied to every other kind.
//
// Three buckets, and the count is the design. Every leaf used to be one class,
// and arm B measured what that costs: five leaves that exhausted their budget on
// t1 — the one task whose leaves carry 2.2M prompt tokens — moved the single
// global rating far enough to reroute t2's document reading and t3's small
// repairs, where the demoted model had never once failed, and both collapsed
// from working to zero. But the opposite mistake is just as easy: a key so fine
// that no bucket ever accumulates enough graded outcomes to pass MinGraded is a
// ledger that has learned nothing at all, expensively.
//
// So it splits on the two things the harness already knows about a node before
// it runs, and nothing else. Kind separates the synthesis nodes the harness owns
// — many long inputs, a roll-up rather than a job — from the work the plan asked
// for. Size separates the rest along the axis the failures actually fell on:
// oversized and borderline are the leaves one agent may not fit, which is what a
// budget stop usually means, and atomic is the rest. Borderline sits with
// oversized rather than with atomic because the risk it names is the same risk,
// and because erring that way keeps a lesson learned on a doubtful leaf away
// from the leaves nobody doubted.
func leafShape(node *plan.Node) string {
	if node.Kind == plan.KindSynthesis {
		return "synthesis"
	}
	switch node.Size {
	case plan.SizeOversized, plan.SizeBorderline:
		return "oversized"
	default:
		return "atomic"
	}
}

// drain lets in-flight nodes land after the run has been told to stop. Their
// contexts are already cancelled, so each executor's own landing procedure is
// what runs here; the grace period only bounds a worker that is wedged past
// even that.
func (s *Scheduler) drain(graph *plan.Graph, done <-chan completion, inFlight map[int]time.Time, started time.Time) {
	grace := time.NewTimer(drainGrace)
	defer grace.Stop()
	for len(inFlight) > 0 {
		select {
		case finished := <-done:
			delete(inFlight, finished.nodeID)
			// No retries while draining: the run has already been told to stop,
			// and putting a node back to pending here would leave it pending
			// forever with nothing left to launch it.
			s.apply(graph, finished.nodeID, finished.outcome, finished.err, started, nil)
		case <-grace.C:
			for id := range inFlight {
				node := graph.Node(id)
				node.State = plan.StateFailed
				node.Failure = fmt.Sprintf("in flight when the run stopped and did not land within %s", drainGrace)
				s.emit(Event{NodeID: id, Title: node.Title, State: plan.StateFailed, Detail: node.Failure, Elapsed: time.Since(started)})
				delete(inFlight, id)
			}
		}
	}
}

// finish annotates whatever never ran and turns the stop reason into the run's
// error. Every early return in Run funnels through here, so no path can end
// the run without the graph saying what happened to each node.
func (s *Scheduler) finish(graph *plan.Graph, stop string) error {
	if stop == "" {
		return nil
	}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.State == plan.StatePending {
			node.State = plan.StateBlocked
			node.Failure = "never started: " + stop
		}
	}
	return fmt.Errorf("run stopped: %s", stop)
}

// tickEvery sizes the housekeeping tick to the watchdog it drives; without a
// timeout the tick only feeds the stall heartbeat.
func (s *Scheduler) tickEvery() time.Duration {
	tick := 15 * time.Second
	if s.NodeTimeout > 0 && s.NodeTimeout/4 < tick {
		tick = s.NodeTimeout / 4
	}
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	return tick
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

func (s *Scheduler) apply(graph *plan.Graph, nodeID int, outcome *Outcome, err error, started time.Time, retries map[int]int) {
	node := graph.Node(nodeID)
	if node == nil {
		return
	}
	// A node the watchdog already retired may still report in late. Its state
	// has been decided; only the spend is real and must not be lost.
	if node.State != plan.StateRunning {
		if outcome != nil {
			s.usage.merge(outcome.Usage)
		}
		return
	}
	if outcome != nil {
		s.usage.merge(outcome.Usage)
		node.Turns = outcome.Turns
		node.Tokens = outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
		node.Cost = outcome.Usage.Cost
		node.Stop = string(outcome.Stop)
		node.Verdict = outcome.Verdict
		node.Artifacts = outcome.Artifacts
		node.Result = outcome.Text
	}
	// A leaf that failed in a way a stronger model might fix is worth one more
	// run. It is expressed by putting the node back to pending rather than by
	// launching from here: the scheduler's own ready-and-launch path is the only
	// place a node may start, and going through it keeps concurrency, budget and
	// blocking checks applying to a retry exactly as they do to a first attempt.
	//
	// The spend already made is kept. It was really spent, and a retry that
	// hid it would understate the run.
	if s.Escalations > 0 && retries != nil && outcome != nil &&
		retries[nodeID] < s.Escalations && outcome.Verdict.Escalates() {
		retries[nodeID]++
		node.State = plan.StatePending
		s.emit(Event{NodeID: nodeID, Title: node.Title, State: plan.StatePending,
			Detail:  fmt.Sprintf("%s — retrying on a stronger model", outcome.Verdict),
			Elapsed: time.Since(started)})
		return
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
