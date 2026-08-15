package session

// The executor: a graph of work, a frontier, and one node's life inside a
// worktree.
//
// ── THE SHAPE IS A GRAPH, TODAY IT HOLDS ONE NODE ──
//
// [TaskGraph] is nodes and edges; [TaskGraph.runFrontier] is the whole
// scheduler. A node whose dependencies are all done is RUNNABLE; runnable nodes
// start until the concurrency cap is full and the rest wait on the frontier; a
// node that finishes unlocks its dependents and its report becomes part of
// their briefs. What v1 exercises is a graph of one node with no edges, and
// nothing here is written for that case: when decomposition lands it adds EDGES
// INTO THIS EXECUTOR, not a second machine, and the only thing that changes is
// that depends_on stops being empty.
//
// THE BRIEF IS ASSEMBLED WHEN THE NODE STARTS, not when it was proposed. That
// is the point of doing it here: a dependent's prerequisites have run by then,
// so what it is handed is its own brief plus what the work before it learned —
// facts that did not exist at proposal time.
//
// ── WHY A WORKTREE ──
//
// A node edits files while the person is editing files. Sharing a checkout
// would mean the node's half-finished sweep is what the person's `go build`
// compiles, and a node killed mid-edit would leave its wreckage in their tree.
// So each node gets `git worktree add` on a branch off the person's current
// HEAD: it works in a directory of its own, and the work comes home as a MERGE
// — clean, or a conflict that keeps the branch and says so. A workspace that is
// not a repository has no such isolation to offer, and the node runs in place
// rather than pretending (Merge is "inplace", and the person is told).
//
// ── WHY A NODE NEVER ASKS ──
//
// There is nobody to ask. Its approval posture is composed from the same
// [approval.Policy] the conversation uses — allow everything, with the critical
// table (rm -rf /, mkfs, a redirect onto a raw disk, shutdown) still a floor
// under it — and where a conversation would raise a question, the node reads a
// refusal it can act on. That is exactly consent.go's law for a headless run,
// said in the node's own words.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// taskMaxRunning is how many nodes work at once. Two, because a node is a
	// whole agent — its own model calls, its own build, its own checkout — and
	// the person is running one conversation beside them on the same machine. A
	// third RUNNABLE node is not an error and never refuses the tool: it sits on
	// the frontier and starts when a slot frees, which is what a queue in a
	// graph is for.
	taskMaxRunning = 2

	// taskDeadline bounds one node's whole life. Thirty minutes is a long build
	// and a fix loop after it; past that the node is not working, it is stuck,
	// and a stuck node that never ends holds a slot and a worktree forever.
	taskDeadline = 30 * time.Minute

	// tasksDirName is where the worktrees live, beside the job logs and under
	// the repository so `git worktree list` and a person's file browser both
	// find them where they were left.
	tasksDirName = ".aforge-v3/tasks"

	// taskReportLines and taskReportLineLimit bound the report. Two or three
	// lines is what a person reads off a finished card and what a dependent's
	// brief can afford to carry; the whole story is in the node's journal.
	taskReportLines     = 3
	taskReportLineLimit = 300

	// taskSlugLimit keeps a branch name readable in `git branch`.
	taskSlugLimit = 32
)

// The three merge outcomes, and the fourth that says a branch never came home.
// They are the strings [TaskNotice.Merge] carries, spelled once.
const (
	mergeMerged     = "merged"
	mergeConflicted = "conflicted"
	mergeInPlace    = "inplace"
	mergeAborted    = "aborted"
)

// ── the graph ───────────────────────────────────────────────────────────────

// TaskNode is one piece of work: what it was admitted with, where it is in its
// life, and what it leaves behind.
//
// Every field below the mutex line is guarded by the GRAPH's lock, not one of
// its own. A node is never touched alone — starting one reads its dependencies'
// reports, finishing one unlocks its dependents — so a second lock would be a
// lock ordering to get wrong for no gain.
type TaskNode struct {
	graph     *TaskGraph
	id        uint64
	dependsOn []uint64
	// done is closed when the node reaches a final state. It is how a waiter —
	// a test, a future join — waits without polling.
	done chan struct{}

	spec  taskSpec
	brief string
	state TaskState
	// report, changed, branch and merge are the node's leavings, written by the
	// goroutine that ran it and read by everybody else.
	report  string
	changed []string
	branch  string
	merge   string
	started time.Time
	// queuedSaid marks the one "queued" update this node ever sends. A node
	// waiting behind the cap or behind an edge must show up on the surface —
	// otherwise admitted work is invisible until it starts — but a frontier pass
	// runs on every completion, and a node that announced itself on each of them
	// would be a card redrawing itself for news that has not changed.
	queuedSaid bool
	// cancel ends this node's run: the deadline's context, cancelled early by
	// jobs kill or by Close.
	cancel context.CancelFunc
}

// TaskGraph is the session's work as a directed acyclic graph, plus the
// frontier executor that runs it.
type TaskGraph struct {
	mu    sync.Mutex
	nodes map[uint64]*TaskNode
	// order is admission order, and it is what makes the frontier
	// DETERMINISTIC: with a cap in play, which of two ready nodes starts first
	// must not be Go's map iteration.
	order   []uint64
	seq     uint64
	running int

	// run executes one node to completion and calls [TaskGraph.complete] when
	// it lands. It is a field rather than a method so the graph can be exercised
	// with a scripted runner — the scheduling law (readiness, the cap, brief
	// assembly) is the thing worth testing on its own, and it should not need a
	// provider and a git repository to be looked at.
	run func(*TaskNode)
	// report is called once per node reaching a final state, outside the lock.
	// It is how the world hears: the update event, and the note that reaches the
	// model through the steering lane.
	report func(*TaskNode)
}

func newTaskGraph() *TaskGraph {
	return &TaskGraph{nodes: make(map[uint64]*TaskNode, 1)}
}

// graph is the session's graph, built on first use. Most conversations never
// groom a task, and one that does builds it exactly once.
func (a *Agent) graph() *TaskGraph {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tasks == nil {
		graph := newTaskGraph()
		graph.run = a.runTaskNode
		graph.report = a.reportTaskNode
		a.tasks = graph
	}
	return a.tasks
}

// reserve takes the next id. It is separate from admission because a PROPOSAL
// has an id — the surface answers by it — while a node only exists once the
// proposal is approved.
func (g *TaskGraph) reserve() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	return g.seq
}

// admit puts an approved proposal into the graph and turns the frontier. The
// state it reports is what the node is doing by the time the tool answers:
// running, or queued behind its dependencies or the cap.
func (g *TaskGraph) admit(id uint64, spec taskSpec) TaskState {
	node := &TaskNode{
		graph:     g,
		id:        id,
		dependsOn: spec.dependsOn,
		done:      make(chan struct{}),
		spec:      spec,
		state:     TaskQueued,
	}
	g.mu.Lock()
	if g.nodes == nil {
		g.nodes = make(map[uint64]*TaskNode, 1)
	}
	g.nodes[id] = node
	g.order = append(g.order, id)
	g.mu.Unlock()

	g.runFrontier()
	return node.stateNow()
}

// runFrontier is the scheduler, and it is the whole of it.
//
// One pass: fail every queued node whose dependencies cannot be met, start
// every queued node whose dependencies are all done until the cap is full. A
// node that fails here unlocks nothing, so its own dependents are failed by the
// pass this one tail-calls — a cascade walks the graph one layer per pass
// rather than needing a recursive walk holding the lock.
//
// It is called after admission and after every completion, and it is safe to
// call when nothing can move: the cost of a pass with nothing to do is one lock
// and a walk of the order slice.
func (g *TaskGraph) runFrontier() {
	g.mu.Lock()
	var starting, failing, waiting []*TaskNode
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.state != TaskQueued {
			continue
		}
		ready, blocked := g.readinessLocked(node)
		if blocked != "" {
			node.state = TaskFailed
			node.report = blocked
			failing = append(failing, node)
			continue
		}
		if !ready || g.running >= taskMaxRunning {
			if !node.queuedSaid {
				node.queuedSaid = true
				waiting = append(waiting, node)
			}
			continue
		}
		// JIT: the brief is assembled here, with the prerequisites' reports in
		// hand, and never at proposal time when they did not exist yet.
		node.brief = g.briefLocked(node)
		node.state = TaskRunning
		node.started = time.Now()
		g.running++
		starting = append(starting, node)
	}
	g.mu.Unlock()

	for _, node := range waiting {
		g.announce(node)
	}
	for _, node := range failing {
		close(node.done)
		g.announce(node)
	}
	for _, node := range starting {
		g.announce(node)
		go g.run(node)
	}
	// A cascade needs one more pass: the nodes just failed may block others,
	// and a failure frees no slot, so nothing else can have moved.
	if len(failing) > 0 {
		g.runFrontier()
	}
}

// readinessLocked answers two questions at once: may this node start, and is it
// waiting on something that will never come. The second is why a dependency on
// a FAILED node is not simply "not ready yet" — a node whose prerequisite did
// not finish is a node whose brief can never be assembled, and leaving it
// queued forever would be a task the person watches wait on nothing.
func (g *TaskGraph) readinessLocked(node *TaskNode) (bool, string) {
	ready := true
	for _, id := range node.dependsOn {
		prerequisite := g.nodes[id]
		if prerequisite == nil {
			return false, fmt.Sprintf("it waits on task %d, which is not in this session's work", id)
		}
		switch prerequisite.state {
		case TaskDone:
		case TaskFailed:
			return false, fmt.Sprintf("it waits on task %d, which did not finish", id)
		default:
			ready = false
		}
	}
	return ready, ""
}

// briefLocked assembles what one node is handed: its own brief, and then what
// the work before it learned. The heading is plain words rather than a marker
// because the node reads it as prose — it is a colleague being told what the
// last shift found, not a data structure.
func (g *TaskGraph) briefLocked(node *TaskNode) string {
	var learned strings.Builder
	for _, id := range node.dependsOn {
		prerequisite := g.nodes[id]
		if prerequisite == nil || strings.TrimSpace(prerequisite.report) == "" {
			continue
		}
		fmt.Fprintf(&learned, "\n\n%s (task %d):\n%s",
			prerequisite.spec.title, id, prerequisite.report)
	}
	if learned.Len() == 0 {
		return node.spec.brief
	}
	return node.spec.brief + "\n\nWhat the work before you learned:" + learned.String()
}

// complete settles one node and turns the frontier again. The runner has
// already written the node's leavings through [TaskNode.finish]; this is the
// state transition and the slot being handed back.
func (g *TaskGraph) complete(node *TaskNode, state TaskState) {
	g.mu.Lock()
	node.state = state
	if g.running > 0 {
		g.running--
	}
	g.mu.Unlock()
	close(node.done)

	g.announce(node)
	g.runFrontier()
}

// announce tells the world about one node's current state, outside the lock: a
// report hook that emits an event and writes to a journal must never run with
// the graph held.
func (g *TaskGraph) announce(node *TaskNode) {
	if g.report != nil {
		g.report(node)
	}
}

// node looks one up by id.
func (g *TaskGraph) node(id uint64) *TaskNode {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nodes[id]
}

// ── one node, from the outside ──────────────────────────────────────────────

func (n *TaskNode) stateNow() TaskState {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.state
}

// assembledBrief is the brief the node is actually working from: its own, plus
// its prerequisites' reports.
func (n *TaskNode) assembledBrief() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.brief
}

func (n *TaskNode) title() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.title
}

// instruction is what the child agent is asked: the assembled brief, and the
// done-condition it is finished against, named as such.
func (n *TaskNode) instruction() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.brief + "\n\nAcceptance: " + n.spec.acceptance
}

// setCancel hands the node the handle that ends its run.
func (n *TaskNode) setCancel(cancel context.CancelFunc) {
	n.graph.mu.Lock()
	n.cancel = cancel
	n.graph.mu.Unlock()
}

// finish writes the node's leavings before its state changes, so the update
// that announces "done" carries them.
func (n *TaskNode) finish(report string, changed []string, branch, merge string) {
	n.graph.mu.Lock()
	n.report = strings.TrimSpace(report)
	n.changed = changed
	n.branch = branch
	n.merge = merge
	n.graph.mu.Unlock()
}

// notice copies the node out from under the lock, shaped for an
// EventTaskUpdate. Rendering never holds the graph.
func (n *TaskNode) notice() TaskNotice {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	elapsed := time.Duration(0)
	if !n.started.IsZero() {
		elapsed = time.Since(n.started)
	}
	changed := make([]string, len(n.changed))
	copy(changed, n.changed)
	return TaskNotice{
		ID:        n.id,
		Title:     n.spec.title,
		DependsOn: n.dependsOn,
		State:     n.state,
		Elapsed:   elapsed,
		Report:    n.report,
		Changed:   changed,
		Branch:    n.branch,
		Merge:     n.merge,
	}
}

// ── the world hearing about a node ──────────────────────────────────────────

// reportTaskNode is the graph's report hook: one event for a surface, and — on
// a final state — one note for the model.
//
// The note rides the STEERING LANE, exactly as a background job's exit does
// (jobs.go). A node's completion is news that arrives while the model is busy,
// it must land at a step boundary rather than inside a tool batch, and from the
// model's side it is a line somebody said. Giving it a second lane would be a
// second ordering rule for the same kind of message.
func (a *Agent) reportTaskNode(node *TaskNode) {
	notice := node.notice()
	a.emitTaskUpdate(notice)
	if notice.State == TaskRunning || notice.State == TaskQueued {
		return
	}
	a.enqueueSteering(taskNote(notice))
}

// taskNote is what the model reads when a node lands: the outcome, the report,
// and the two facts it cannot infer — what changed, and whether the work came
// home.
func taskNote(notice TaskNotice) string {
	var note strings.Builder
	verb := "finished"
	if notice.State == TaskFailed {
		verb = "failed"
	}
	fmt.Fprintf(&note, "task %d %s: %s", notice.ID, verb, notice.Title)
	if notice.Report != "" {
		note.WriteString("\n" + notice.Report)
	}
	if len(notice.Changed) > 0 {
		note.WriteString("\nchanged: " + strings.Join(notice.Changed, ", "))
	}
	switch notice.Merge {
	case mergeMerged:
		note.WriteString("\nits branch " + notice.Branch + " merged into yours")
	case mergeConflicted:
		note.WriteString("\nits branch " + notice.Branch + " did not merge cleanly and was kept — merge it yourself when you are ready")
	case mergeAborted:
		note.WriteString("\nit was stopped; its branch " + notice.Branch + " was kept")
	case mergeInPlace:
		note.WriteString("\nit worked directly in the workspace: there was no repository to branch")
	}
	return note.String()
}

// emitTaskUpdate puts one update in front of whoever is watching.
//
// TWO LANES, and they carry the same event because they answer to two different
// lifetimes. The turn's hub is what a Submit caller is reading, and it is the
// right place for an update that happens while the model works. But a node
// outlives the turn that proposed it: its "done" lands minutes later, when
// there is no turn and no hub, so [Agent.TaskUpdates] is the standing
// subscription a surface holds for the whole session.
//
// A surface reading both sees an in-turn update on both, exactly as a surface
// holding two Submit channels sees each event on each (see [Agent.Submit]).
func (a *Agent) emitTaskUpdate(notice TaskNotice) {
	event := Event{Kind: EventTaskUpdate, Tool: "propose_task", Task: &notice}
	a.mu.Lock()
	hub := a.hub
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()

	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// TaskUpdates is a standing subscription to every task update this session
// emits, for the whole life of the session rather than one turn.
//
// It exists because a node's most important event — it finished, here is the
// report, here is what merged — happens when no turn is running and no Submit
// channel is open. A surface that draws task cards subscribes once at startup;
// a surface that does not draw them never calls this and pays nothing.
//
// The channel is never closed by a turn ending — a turn's end is not the end of
// the work it handed off. A surface holds it for the life of the session and
// stops reading when it stops drawing.
func (a *Agent) TaskUpdates() <-chan Event {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out
	}
	a.taskWatchers = append(a.taskWatchers, stream)
	a.mu.Unlock()
	return stream.out
}

// ── running one node ────────────────────────────────────────────────────────

// runTaskNode is one node's whole life: a working copy, a child agent, the run,
// and the branch coming home.
//
// It is the graph's run hook, so everything it does is bracketed by the graph:
// the node was marked running before this started, and [TaskGraph.complete] at
// the end frees the slot and turns the frontier.
func (a *Agent) runTaskNode(node *TaskNode) {
	ctx, cancel := context.WithTimeout(context.Background(), taskDeadline)
	defer cancel()
	node.setCancel(cancel)

	// The node is a JOB, from the registry every other piece of background work
	// comes from: one id space, one row in `jobs list`, one `jobs kill`, one
	// death at Close. What differs is the middle, which is this function.
	listed, err := a.jobs.startTask(node.id, node.title(), cancel)
	if err == nil {
		defer listed.settle(0)
	}

	state := a.workTaskNode(ctx, node, listed)
	node.graph.complete(node, state)
}

// workTaskNode does the work and reports the state the node ended in. Every
// failure is a state and a report rather than an error: a node that could not
// get a working copy has to be able to say so to the person who asked for it.
func (a *Agent) workTaskNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	log := taskLog(listed)
	tree, err := prepareTaskTree(a.config.Workspace, node.id, node.title())
	if err != nil {
		node.finish("could not prepare a working copy: "+err.Error(), nil, "", "")
		return TaskFailed
	}
	fmt.Fprintf(log, "task %d · %s\nworking in %s\n", node.id, node.title(), tree.dir)

	child, err := a.newTaskAgent(tree.dir, node)
	if err != nil {
		node.finish("could not start the task: "+err.Error(), nil, tree.branch, tree.merge)
		return TaskFailed
	}
	defer func() {
		_ = child.Close()
		// The node's spend is the person's, so it is folded into the session's
		// auxiliary usage — the pocket the title and the compaction summary come
		// out of — rather than charged to whichever turn happened to propose it.
		a.foldTaskUsage(child)
	}()

	changed, runErr := runTaskChild(ctx, child, node.instruction(), tree.dir, log)
	report := taskReport(child)

	switch {
	case ctx.Err() == context.DeadlineExceeded:
		node.finish(withReport("ran out of time", report), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
	case ctx.Err() != nil:
		node.finish(withReport("stopped before it finished", report), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
	case runErr != nil:
		node.finish(withReport("it ended with an error: "+runErr.Error(), report), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
	}

	merge, detail := tree.comeHome(node.title())
	fmt.Fprintf(log, "merge: %s %s\n", merge, detail)
	node.finish(withReport(report, detail), changed, tree.branch, merge)
	return TaskDone
}

// withReport joins the runner's own sentence and the child's words, dropping
// whichever is empty. An empty report is a real outcome — a node killed before
// it said anything — and "\n\n" around nothing would be a blank line the person
// reads as missing text.
func withReport(lead, tail string) string {
	lead, tail = strings.TrimSpace(lead), strings.TrimSpace(tail)
	switch {
	case lead == "":
		return tail
	case tail == "":
		return lead
	default:
		return lead + "\n" + tail
	}
}

// abortedMerge is what a branch that never came home is marked with. A node
// that ran in place has nothing to abort: its edits are already in the person's
// tree, and calling that "aborted" would say work was thrown away that is
// sitting in front of them.
func abortedMerge(tree taskTree) string {
	if tree.merge == mergeInPlace {
		return mergeInPlace
	}
	return mergeAborted
}

// runTaskChild submits the brief and consumes the node's own events internally.
//
// The events are NOT forwarded. A node's tool rows belong to its journal, not
// to the conversation: a person watching a chat must not have forty of somebody
// else's greps scroll past. What is kept is the one thing the person needs to
// see afterwards — which files the node wrote — read off the edit and write
// calls as they end.
func runTaskChild(ctx context.Context, child *Agent, instruction, dir string, log io.Writer) ([]string, error) {
	events, err := child.Submit(ctx, instruction)
	if err != nil {
		return nil, err
	}
	var (
		changed []string
		seen    = map[string]bool{}
		failure error
	)
	for event := range events {
		switch event.Kind {
		case EventToolBegin:
			fmt.Fprintf(log, "· %s\n", event.Hint)
		case EventToolEnd:
			path, ok := changedPath(event, dir)
			if ok && !seen[path] {
				seen[path] = true
				changed = append(changed, path)
			}
		case EventError:
			failure = event.Err
		}
	}
	return changed, failure
}

// changedPath reads the file one edit or write touched, workspace-relative.
//
// It reads the CALL's arguments rather than the result because that is where
// the path is: the tools answer with a sentence about what they did, and the
// arguments are the record of what was asked (see [Event.Args]).
func changedPath(event Event, dir string) (string, bool) {
	if event.Tool != "edit" && event.Tool != "write" {
		return "", false
	}
	var fields struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(event.Args), &fields); err != nil {
		return "", false
	}
	path := strings.TrimSpace(fields.Path)
	if path == "" {
		return "", false
	}
	if relative, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative), true
	}
	return filepath.ToSlash(path), true
}

// taskReport is the node's last word: the final assistant message, cut to three
// lines. It is read off the child's transcript rather than accumulated from its
// deltas because both paths — a streaming provider and a non-streaming one —
// end with the same recorded message, and only one of them emits deltas.
func taskReport(child *Agent) string {
	entries := child.Transcript()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Role != "assistant" || strings.TrimSpace(entry.Text) == "" {
			continue
		}
		return firstLines(entry.Text, taskReportLines)
	}
	return ""
}

// firstLines is the first n non-empty lines, each clipped.
func firstLines(text string, n int) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, clip(line, taskReportLineLimit))
		if len(kept) == n {
			break
		}
	}
	return strings.Join(kept, "\n")
}

// foldTaskUsage charges the node's spend to the session.
func (a *Agent) foldTaskUsage(child *Agent) {
	used := child.Usage()
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	cost := used.CostUSD
	a.addAuxiliaryUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens:             used.Input,
		CompletionTokens:         used.Output,
		CacheReadInputTokens:     used.CacheRead,
		CacheCreationInputTokens: used.CacheWrite,
		Cost:                     &cost,
	}})
}

// ── the child agent ─────────────────────────────────────────────────────────

// newTaskAgent builds the agent that IS the node: the same package, the same
// loop, the same hands — a different workspace, a different journal, and a belt
// with two tools left off (tools.go).
//
// It inherits the conversation's model, client, window and capabilities because
// a node is the same worker doing the same job somewhere quieter. It inherits
// neither the transcript nor the memory file: the brief is the node's whole
// world by construction, and two agents appending to one memory file would be
// two writers on a document neither can see the other editing.
func (a *Agent) newTaskAgent(dir string, node *TaskNode) (*Agent, error) {
	a.mu.Lock()
	parent := a.config
	model := a.model
	client := unwrapCompleter(a.client)
	journal := taskJournalPath(a.sessionID(), node.id)
	a.mu.Unlock()

	return newAgent(Config{
		Workspace:      dir,
		Model:          model,
		APIKey:         parent.APIKey,
		BaseURL:        parent.BaseURL,
		ContextWindow:  parent.ContextWindow,
		CompactEnabled: parent.CompactEnabled,
		SessionFile:    journal,
		// ALLOW EVERYTHING EXCEPT THE FLOOR. approval's critical table still
		// turns an allow into a "prompt" for the handful of shapes that destroy
		// a disk or drop the machine, and a prompt in a node is a refusal it can
		// read (consent.go, InTask) — never a question and never a hang.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		InTask:         true,
		SupportsImages: parent.SupportsImages,
		RolesSource:    parent.RolesSource,
		SearchProvider: parent.SearchProvider,
		SearchFetcher:  parent.SearchFetcher,
		ImageGenModel:  parent.ImageGenModel,
		ImageGenClient: parent.ImageGenClient,
	}, client)
}

// sessionID names the conversation a node's journal belongs under. A session
// with no file on disk still has a lineage worth grouping by, so the fallback is
// a stable word rather than an empty path segment. Called with a.mu held.
func (a *Agent) sessionID() string {
	if a.file != nil {
		if id := a.file.ID(); id != "" {
			return id
		}
	}
	return "unfiled"
}

// taskJournalPath is where a node's own transcript lives:
// ~/.aforge/v3/tasks/<session>/<when>_<id>.jsonl.
//
// It is a REAL SESSION FILE — the node is an agent, and everything it did is
// resumable and readable with the same tools — kept under the conversation that
// asked for it, so "what did that task actually do" is one directory listing
// away. A machine with no home directory gets an in-memory node instead of a
// failed one: the journal is evidence, not a prerequisite.
func taskJournalPath(session string, id uint64) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := fmt.Sprintf("%s_%d.jsonl", time.Now().Format("20060102-150405"), id)
	return filepath.Join(home, ".aforge", "v3", "tasks", session, name)
}

// unwrapCompleter reaches past the session's own request wrapper.
//
// [sessionCompleter] stamps THIS conversation's prompt-cache key on every
// request it carries. Handing it to a node would put the node's transcript on
// the conversation's cache lineage — two different prefixes asking the same
// replica for a warm head, each cold-starting the other — so the node is built
// on the client underneath and newAgent gives it a lineage of its own.
func unwrapCompleter(client Completer) Completer {
	if wrapper, ok := client.(sessionCompleter); ok {
		return wrapper.inner
	}
	return client
}

// taskLog is the node's job log as a writer, and io.Discard when the registry
// could not open one. A node whose log file failed still runs.
func taskLog(listed *job) io.Writer {
	if listed == nil || listed.sink == nil {
		return io.Discard
	}
	return listed.sink
}

// ── the working copy ────────────────────────────────────────────────────────

// taskTree is where one node works and how its work comes home.
type taskTree struct {
	// dir is the node's workspace: a worktree, or the person's own directory
	// when there is no repository.
	dir string
	// root is the repository the branch merges back into, empty in place.
	root string
	// branch is the node's branch, empty in place.
	branch string
	// merge is the outcome so far: "inplace" for a non-repository, and empty
	// while a branch is still out.
	merge string
}

// gitRoot serializes the operations that touch the ROOT repository's shared
// state — adding a worktree, merging, removing a worktree, deleting a branch.
//
// Two nodes finishing at once would otherwise race on the index lock and one
// would fail with git's "another git process seems to be running", which is a
// merge lost to a coincidence rather than to a conflict. Node-local work (the
// commit inside its own worktree) is not serialized: a worktree has an index of
// its own, and that is the whole point of it.
var gitRoot sync.Mutex

// prepareTaskTree gives one node a place to work.
//
// A repository gets `git worktree add` on a fresh branch off the person's
// CURRENT HEAD — that is what makes the node's work a merge later, and what
// keeps a killed node's half-finished edits out of the person's checkout.
// Anything else — not a repository, or a repository with no commit to branch
// from — runs IN PLACE and says so, because pretending to isolate is worse than
// not isolating.
func prepareTaskTree(workspace string, id uint64, title string) (taskTree, error) {
	root, ok := repositoryRoot(workspace)
	if !ok {
		return taskTree{dir: workspace, merge: mergeInPlace}, nil
	}
	if _, err := git(root, "rev-parse", "--verify", "HEAD"); err != nil {
		// A repository with no commits has no HEAD to branch from. The honest
		// answer is the non-repository one.
		return taskTree{dir: workspace, merge: mergeInPlace}, nil
	}

	dir := filepath.Join(root, filepath.FromSlash(tasksDirName), strconv.FormatUint(id, 10))
	branch := "task/" + slugify(title) + "-" + shortID()

	gitRoot.Lock()
	defer gitRoot.Unlock()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return taskTree{}, err
	}
	// A directory left by a run that died with the process would make the add
	// fail; prune first so the stale registration goes with it.
	if _, err := os.Stat(dir); err == nil {
		_, _ = git(root, "worktree", "remove", "--force", dir)
		_, _ = git(root, "worktree", "prune")
		_ = os.RemoveAll(dir)
	}
	if out, err := git(root, "worktree", "add", "-b", branch, dir, "HEAD"); err != nil {
		return taskTree{}, fmt.Errorf("git worktree add: %s", firstLine(out))
	}
	return taskTree{dir: dir, root: root, branch: branch}, nil
}

// comeHome commits whatever the node wrote and merges its branch into the
// person's. It reports the outcome and, ONLY when the outcome needs explaining,
// one line for the node's report: an ordinary merge is already said by the
// Merge and Branch fields and by the completion note, and saying it a third
// time inside the report is the same sentence three times on one card.
//
// THE MERGE IS ATTEMPTED WHATEVER THE PERSON'S TREE LOOKS LIKE. A dirty
// checkout is the normal state of somebody who has been working, and refusing
// to try would make "I had a file open" the reason a finished task did not
// land. If git cannot do it — a real conflict, or local changes it would have
// to overwrite — the branch is KEPT and named, and nothing of the node's work
// is lost.
func (t taskTree) comeHome(title string) (string, string) {
	if t.merge == mergeInPlace || t.root == "" {
		return mergeInPlace, ""
	}
	commitTaskWork(t.dir, title)

	gitRoot.Lock()
	defer gitRoot.Unlock()
	if out, err := git(t.root, "merge", "--no-edit", t.branch); err != nil {
		// --abort is best-effort: a merge that never started (git refused
		// before touching the index) has nothing to abort, and it says so.
		_, _ = git(t.root, "merge", "--abort")
		return mergeConflicted, fmt.Sprintf("its branch %s did not merge cleanly and was kept: %s",
			t.branch, firstLine(out))
	}
	// The branch is gone only once its work is in: removing the worktree first
	// keeps `git branch -d` from refusing on a checked-out branch.
	if _, err := git(t.root, "worktree", "remove", t.dir); err != nil {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
	}
	_, _ = git(t.root, "branch", "-d", t.branch)
	return mergeMerged, ""
}

// commitTaskWork puts everything the node wrote into one commit on its own
// branch. Without it there would be nothing to merge: a node's work is files on
// disk, and git only moves what has been committed.
//
// The identity is passed per-command rather than configured, so a machine with
// no git identity still commits and the person's own config is not touched.
// "nothing to commit" is not a failure — a node that only read is a node with
// an empty branch, and an empty branch merges cleanly.
func commitTaskWork(dir, title string) {
	if _, err := git(dir, "add", "-A"); err != nil {
		return
	}
	// The harness's own droppings are not the node's work: a background job the
	// node started wrote its log under the workspace (jobs.go), and a build log
	// merged into the person's branch is noise they did not ask for.
	_, _ = git(dir, "reset", "--quiet", "--", ".aforge-v3")
	_, _ = git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--no-verify", "-m", "task: "+clip(firstLine(title), 72))
}

// repositoryRoot is the top of the repository a directory sits in.
func repositoryRoot(dir string) (string, bool) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", false
	}
	return root, true
}

// git runs one command in a directory and returns its combined output. There is
// no context: every call here is a local plumbing command that either answers
// immediately or is a broken repository, and a node's own deadline already
// bounds the run it belongs to.
func git(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	// A pager or an editor in the middle of a merge would hang a node forever on
	// a terminal it does not have.
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	return string(out), err
}

// slugify turns a title into the branch-name half a person can read:
// lowercase, one dash between words, nothing git has to be escaped from.
func slugify(title string) string {
	var slug strings.Builder
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			slug.WriteRune(char)
			dash = false
		default:
			if !dash && slug.Len() > 0 {
				slug.WriteByte('-')
				dash = true
			}
		}
		if slug.Len() >= taskSlugLimit {
			break
		}
	}
	trimmed := strings.Trim(slug.String(), "-")
	if trimmed == "" {
		return "task"
	}
	return trimmed
}

// shortID keeps two tasks with the same title on two different branches — the
// same work proposed twice in one session is the ordinary case, not a mistake.
func shortID() string {
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()%0xffffff, 16)
	}
	return hex.EncodeToString(raw[:])
}
