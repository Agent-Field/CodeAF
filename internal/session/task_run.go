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
// ── A NODE STOPS BY A NAMED THRESHOLD, NEVER BY WANDERING ──
//
// Three things end a node's work, and every one of them has a name the person
// can read: its deadline, its step budget, and its no-progress count
// (harness-research-notes.md §1 — Argus terminates on named thresholds rather
// than on a reviewer's judgement, arXiv:2608.05144). A node that has taken
// forty steps or has taken six in a row without changing a file is not working,
// it is circling, and the difference between a harness that says "stopped: 6
// steps without progress" and one that lets the thirty-minute deadline collect
// it is half an hour of the person's money and a report that explains nothing.
// Both thresholds are per-node overridable on the wire (task.go's max_steps and
// no_progress) because the right budget for a one-file rename and for a sweep
// across forty files is not the same number.
//
// ── AND IT IS NEVER THE NODE THAT SAYS IT IS DONE ──
//
// A finished run is a CLAIM. What turns it into TaskDone is an independent
// read-only auditor with hard evidence (task_audit.go): VERIFIED merges,
// REFUTED is a TaskFailed carrying the auditor's evidence, and the cascade
// below fails its dependents with it. Nothing in this file writes TaskDone off
// the child's own last words.
//
// And when the auditor gives NO verdict — twice, having been asked again — the
// node lands TaskUnverified: not done, not failed, branch kept, dependents
// waiting rather than cascading, until a person resolves it
// ([Agent.ResolveUnverified]). A false failure is still a failure, and this
// file must not manufacture one out of an audit that never happened.
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
	"crypto/sha256"
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

	// taskDeadline bounds one node's whole life. An hour is a long build, the
	// reading around it, and the audit after it; past that the node is not
	// working, it is stuck, and a stuck node that never ends holds a slot and
	// a worktree forever. The deadline is the LEASH, not the budget: the
	// auditor is what says a node is done.
	taskDeadline = 60 * time.Minute

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

	// taskMaxSteps and taskNoProgress are the two named thresholds a node stops
	// by, counted in the only unit this side of the wall can see: the child's
	// finished tool calls. They are the LEASH, not the budget — the auditor is
	// what says a node is done, and a healthy node never meets either number.
	//
	// Two hundred steps is liberal on purpose: a real task reads, builds, fixes
	// and verifies, and a step cap that bites during honest work is how you get
	// "aborted" on a node that was about to finish. Six consecutive steps with
	// no NEW information and no new dirt is the shape of a spin — the detector
	// reads novelty now, so it only fires when a node is genuinely re-treading
	// the same call, and it fires in a minute rather than in thirty.
	taskMaxSteps   = 200
	taskNoProgress = 6
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
//
// ── THE GOAL CONTRACT: spec IS FROZEN AT ADMISSION ──
//
// NOTHING IN THIS FILE WRITES spec.brief OR spec.acceptance AFTER admit. Not
// the frontier, not the runner, not the auditor, not a redirect that arrives
// late. The node's goal is settled the moment [TaskGraph.admit] takes it, and
// every reader downstream — the instruction the child is given, the acceptance
// the auditor judges against, the report a dependent inherits — reads THAT text
// and no other.
//
// This is Argus's two-tier goal contract (harness-research-notes.md §1,
// arXiv:2608.05144), and the two tiers are drawn exactly here: the semantic
// tier moves freely BEFORE admission — the model grooms the brief, the person
// redirects it and their words are appended (task.go) — and the precise
// objective moves only with authority, which in this surface means a NEW
// admission by the person. A redirect is not an edit to a running node; there
// is no path to one, and there must not be. The reason is the auditor: a
// frontier that verifies work against an acceptance which can move while the
// work runs verifies nothing, because whoever holds the pen can always make the
// work pass. What the person gets instead is honest — the running node lands
// against what it was given, and the correction is a task of its own.
//
// The only fields below that change after admission are the node's LIFE (state,
// started, cancel) and its LEAVINGS (brief, report, changed, branch, merge).
// brief is assembled once at start (runFrontier's JIT assembly) from spec.brief
// plus prerequisites' reports and is not the spec.
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
	// report, changed, branch, worktree and merge are the node's leavings,
	// written by the goroutine that ran it and read by everybody else. worktree
	// is where it worked, and it is kept for one reader only: a recovery that has
	// to tell the person where an interrupted node's half-finished work is
	// (task_store.go).
	report   string
	changed  []string
	branch   string
	worktree string
	merge    string
	started  time.Time
	// elapsed is the node's age FROZEN at the moment it landed. While it runs it
	// is zero and the age is measured from started; a node rehydrated from a
	// checkpoint has no started to measure from and this is the age it had.
	elapsed time.Duration
	// cost is what this node's agents have spent, in dollars, ACCUMULATED where
	// each of them is closed ([Agent.foldTaskUsage]). While the node runs it is
	// zero and the live figure is asked of the child directly (see
	// [TaskNode.spend]); after it lands this is the frozen answer, because the
	// node outlives its child and a surface asking a landed node what it cost
	// has nobody else to ask.
	//
	// It is NOT in the checkpoint: a resumed graph is a graph whose child agents
	// are gone, and a figure rehydrated from a file would be the only number on
	// the task index nobody could point at a request for. It reaches the project
	// index (task_index.go) and the node's own notice on the way past.
	cost float64
	// noted says this node's completion note has been handed to the steering
	// lane, and it is what stops a resumed session announcing finished work
	// twice (task_store.go).
	noted bool
	// interrupted marks a node that was RUNNING when a session ended and whose
	// interrupt a recovery has consumed. It is history, and it is written down so
	// that exactly one recovery ever consumes it.
	interrupted bool
	// queuedSaid marks the one "queued" update this node ever sends. A node
	// waiting behind the cap or behind an edge must show up on the surface —
	// otherwise admitted work is invisible until it starts — but a frontier pass
	// runs on every completion, and a node that announced itself on each of them
	// would be a card redrawing itself for news that has not changed.
	queuedSaid bool
	// cancel ends this node's run: the deadline's context, cancelled early by
	// jobs kill or by Close.
	cancel context.CancelFunc
	// room is the node as a PLACE: the child agent somebody can talk to and the
	// live subscribers watching it work (task_room.go). It is nil until the
	// first person enters or the runner attaches its child, and it is emptied
	// when the node lands.
	room *taskRoom
	// journal is where this node's transcript was written, recorded when its
	// child agent was built. It is the node's history, and it outlives the room.
	journal string
	// mend is the gap a repair round is closing right now, in plain words, and ""
	// at every other moment (task_audit.go's repairNode). It is the ONLY thing
	// the repair loop puts on the wire while it runs: the node is still running,
	// nothing has landed, and what a surface draws is the work still going with
	// one line saying what is being finished.
	mend string
	// auditing marks a re-audit in flight over a landed node (task_audit.go's
	// reauditTask). It is the one thing that can still change an unverified
	// node's state without a person, so it is also what stops a person's own
	// resolution racing a verdict that is already on its way.
	auditing bool
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

	// store is where the graph is written down after every transition, and nil
	// for a graph with no journal behind it — a session with no file, and every
	// scripted graph in the tests (task_store.go).
	store *taskStore
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
		// The checkpoint is per-journal, so a session with no file gets a graph
		// with no disk behind it rather than a session that refuses to run tasks
		// (task_store.go).
		graph.store = newTaskStore(taskCheckpointPath(a.config.SessionFile))
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

	// ADMISSION IS A TRANSITION, and it is checkpointed before the frontier turns
	// rather than after: a process killed between these two lines still resumes
	// with the node the person approved, queued.
	g.checkpoint()
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

	// The pass's transitions reach the disk BEFORE the runs they authorize start:
	// a node that is running in this process must never be a node the checkpoint
	// still calls queued, or a kill in that window would resume work that is
	// already in a worktree.
	if len(starting) > 0 || len(failing) > 0 {
		g.checkpoint()
	}

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
		case TaskUnverified:
			// AN UNVERIFIED PREREQUISITE IS NOT A FAILED ONE. Nobody said its
			// work is wrong — only that nobody could say it is right — so this
			// node WAITS rather than dying in the cascade. What will move it is
			// a person resolving that node ([Agent.ResolveUnverified]), and until
			// then a brief assembled from an unaudited report would be work
			// built on a claim nothing stands behind.
			ready = false
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
	if !node.started.IsZero() {
		// The age stops here. A finished node's elapsed is a fact about how long
		// the work took, and a checkpoint that recomputed it from `started` would
		// have finished work ageing on disk.
		node.elapsed = time.Since(node.started)
	}
	if g.running > 0 {
		g.running--
	}
	g.mu.Unlock()
	close(node.done)

	g.checkpoint()
	g.announce(node)
	g.runFrontier()
}

// resettle moves a node that has ALREADY landed to a new final state: an
// unverified node a person accepted, refuted, or had audited again
// ([Agent.ResolveUnverified]).
//
// IT IS NOT [TaskGraph.complete], and the two things it deliberately does not do
// are the reason it exists. It does not close `done` — that channel was closed
// when the node first landed, and closing it twice would panic. And it does not
// hand back a slot: the slot went back with the run that ended, so a second
// decrement here would be this node quietly raising the concurrency cap for
// everybody else. What it shares with complete is everything that matters —
// the checkpoint, the announcement, and the frontier pass that lets the
// dependents move on the new state.
func (g *TaskGraph) resettle(node *TaskNode, state TaskState) {
	g.mu.Lock()
	node.state = state
	g.mu.Unlock()

	g.checkpoint()
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

// model is the id this node runs on, and "" for a node admitted before anybody
// chose one — a checkpoint written by an older build, a scripted graph in a
// test. Its caller reads that emptiness as "the conversation's own".
func (n *TaskNode) model() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.model
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

// acceptance is the frozen contract, read by the auditor. It is deliberately
// the SAME field [TaskNode.instruction] hands the child: two readers of one
// text, so there is no version of this where the work was finished against one
// acceptance and judged against another.
func (n *TaskNode) acceptance() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.acceptance
}

// limits are the node's named thresholds, its own if it named them and the
// defaults if it did not.
func (n *TaskNode) limits() taskLimits {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return taskLimits{
		maxSteps:   thresholdOr(n.spec.maxSteps, taskMaxSteps),
		noProgress: thresholdOr(n.spec.noProgress, taskNoProgress),
	}
}

// taskLimits is what stops a node short of its deadline. Both are counted in
// the child's finished tool calls.
type taskLimits struct {
	// maxSteps is the whole budget: the node stops when it has taken this many.
	maxSteps int
	// noProgress is how many CONSECUTIVE steps may pass with no progress —
	// and progress is broader than an edit: a successful edit or write, a
	// bash that leaves the worktree dirtier, or a read/search/fetch of a
	// target the node has not looked at before all reset it to zero. What it
	// catches is the spin: the same query, the same file, the same nothing.
	noProgress int
}

// thresholdOr reads a wire value that may be absent. Zero and negative both
// mean "the model did not choose", because a schema field the model omitted and
// a field it filled with nonsense should not be two different behaviours.
func thresholdOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
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
	// THE VERDICT IS A TRANSITION TOO. The state change follows immediately, but
	// what a person needs after a kill is the merge outcome and the branch name,
	// and those are written here.
	n.graph.checkpoint()
}

// leavings is what a landed node left behind: what it said, what it wrote, and
// where its branch stands. A resolution that changes one of them has to carry
// the other three forward, because [TaskNode.finish] writes all four and a
// caller that passed nil for the ones it was not changing would erase them.
func (n *TaskNode) leavings() (report string, changed []string, branch, merge string) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	changed = append([]string(nil), n.changed...)
	return n.report, changed, n.branch, n.merge
}

// workingCopy rebuilds the node's [taskTree] from what it wrote down when it
// started (setTree), so a node that has already landed can be merged or audited
// again.
//
// THE ROOT IS THE PERSON'S REPOSITORY, not the worktree's own: `git rev-parse
// --show-toplevel` inside a linked worktree answers the worktree, and a merge
// aimed there would merge the branch into itself. It is derived from the
// workspace exactly as prepareTaskTree derived it.
//
// A working copy that is GONE is an error rather than a silent in-place tree:
// the node's changes live in that directory, and pretending otherwise would
// merge an empty branch and call it done.
func (n *TaskNode) workingCopy(workspace string) (taskTree, error) {
	n.graph.mu.Lock()
	dir, branch, merge := n.worktree, n.branch, n.merge
	n.graph.mu.Unlock()

	if merge == mergeInPlace || strings.TrimSpace(branch) == "" {
		// It ran in the person's own tree: there is nothing to merge and the
		// place to look is where they are standing.
		if strings.TrimSpace(dir) == "" {
			dir = workspace
		}
		return taskTree{dir: dir, merge: mergeInPlace}, nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return taskTree{}, fmt.Errorf("its working copy is gone from %s, so there is nothing left to look at — its branch %s is still there", dir, branch)
	}
	root, ok := repositoryRoot(workspace)
	if !ok {
		return taskTree{}, fmt.Errorf("%s is no longer a repository, so its branch %s cannot come home", workspace, branch)
	}
	return taskTree{dir: dir, root: root, branch: branch}, nil
}

// mending sets — or clears — the gap this node is closing, and TELLS THE WORLD
// on the way past.
//
// The announcement is the whole reason it is a method rather than a field
// assignment. A repair round takes minutes; a surface that learned about it only
// when the node landed would show a task sitting still through the one stretch
// where the most interesting thing about it is what it is finishing. The state
// does not move — a repairing node is a RUNNING node, and reportTaskNode returns
// after the event for a running one — so this is an update and never a landing.
func (n *TaskNode) mending(line string) {
	n.graph.mu.Lock()
	changed := n.mend != line
	n.mend = line
	n.graph.mu.Unlock()
	if changed {
		n.graph.announce(n)
	}
}

// beginAudit claims the node for one re-audit, and reports false when somebody
// already holds it. endAudit hands it back; beingAudited asks.
func (n *TaskNode) beginAudit() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.auditing {
		return false
	}
	n.auditing = true
	return true
}

func (n *TaskNode) endAudit() {
	n.graph.mu.Lock()
	n.auditing = false
	n.graph.mu.Unlock()
}

func (n *TaskNode) beingAudited() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.auditing
}

// addSpend charges one agent's cost to the node.
//
// It ADDS rather than sets, because a node is more than one agent: the worker,
// and the auditor that judges it (task_audit.go). The person asked for a task,
// not for a task and separately for a judge, so the node's figure is what the
// whole node cost — which is the same pocket [Agent.foldTaskUsage] charges the
// session from.
//
// Zero is left alone rather than written: a provider that reported no price is
// not a node that was free, and a row saying "$0.00" would be this build stating
// a figure nobody gave it.
func (n *TaskNode) addSpend(cost float64) {
	if n == nil || cost <= 0 {
		return
	}
	n.graph.mu.Lock()
	n.cost += cost
	n.graph.mu.Unlock()
}

// setTree records where the node is working, the moment it has somewhere to
// work. It is the one write that makes an interrupt recoverable: a node killed
// mid-run has a branch and a directory on disk, and this is where the checkpoint
// learns their names (task_store.go).
func (n *TaskNode) setTree(tree taskTree) {
	n.graph.mu.Lock()
	n.worktree = tree.dir
	n.branch = tree.branch
	n.merge = tree.merge
	n.graph.mu.Unlock()
	n.graph.checkpoint()
}

// openRoom returns the node's room, opening it on first use — the runner
// attaching its child, or a person entering a node that has not started yet.
// A node whose life is over has no room: nothing more will happen in it, and
// its history is its journal (task_room.go).
//
// THE ROOM CLOSES WITH THE NODE, and it is hung on the node's own done channel
// rather than on a call at the end of the run, because there is more than one
// way to a final state: the runner's return, the frontier's cascade over a
// dependent whose prerequisite failed, and a recovery consuming an interrupt.
// All three close done, so all three empty the room and end every watcher's
// channel — including the one Close takes, which kills the node's job and lands
// it exactly as `jobs kill` would.
func (n *TaskNode) openRoom() *taskRoom {
	n.graph.mu.Lock()
	if n.state.settled() {
		n.graph.mu.Unlock()
		return nil
	}
	room, opening := n.room, false
	if room == nil {
		room = newTaskRoom()
		n.room = room
		opening = true
	}
	done := n.done
	n.graph.mu.Unlock()

	if opening {
		go func() {
			<-done
			room.close()
		}()
	}
	return room
}

// setJournal records where this node's transcript is being written. The path is
// minted with a timestamp in it (taskJournalPath), so it is written down the
// moment it exists rather than recomputed later into the name of a file nobody
// wrote.
func (n *TaskNode) setJournal(path string) {
	n.graph.mu.Lock()
	n.journal = path
	n.graph.mu.Unlock()
}

func (n *TaskNode) journalPath() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.journal
}

// markNoted records that this node's completion note has been handed to the
// steering lane, so no later life of this session says it again.
func (n *TaskNode) markNoted() {
	n.graph.mu.Lock()
	already := n.noted
	n.noted = true
	n.graph.mu.Unlock()
	if !already {
		n.graph.checkpoint()
	}
}

// notice copies the node out from under the lock, shaped for an
// EventTaskUpdate. Rendering never holds the graph.
func (n *TaskNode) notice() TaskNotice {
	// The spend is read BEFORE the graph lock is taken, and it has to be: it
	// asks the room for the child agent and the child agent for its own usage,
	// each of which is a lock of its own. Taking them under the graph's would be
	// a second lock order in a package that has one.
	cost := n.spend()
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	// A landed node's age is frozen, and a rehydrated one has only the age its
	// checkpoint recorded; only a node that is still running is measured.
	elapsed := n.elapsed
	if elapsed == 0 && !n.started.IsZero() {
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
		Mending:   n.mend,
		Model:     n.spec.model,
		CostUSD:   cost,
	}
}

// spend is what this node has cost so far, in dollars.
//
// A RUNNING NODE IS ASKED ITS CHILD; a landed one reports the figure accumulated
// as each of its agents was folded into the session's ([Agent.foldTaskUsage]).
// The two are the same number at two moments, and the freeze is what keeps it
// after the child agent has been closed and the room emptied.
//
// Zero means "nobody has published a price", which is what an unpriced model and
// a node that has not started both look like from here — and a surface that
// draws this draws nothing rather than a $0.00 it made up.
func (n *TaskNode) spend() float64 {
	n.graph.mu.Lock()
	room, frozen := n.room, n.cost
	n.graph.mu.Unlock()
	if frozen > 0 {
		return frozen
	}
	if child := room.speaker(); child != nil {
		return child.Usage().CostUSD
	}
	return frozen
}

// ── the world hearing about a node ──────────────────────────────────────────

// reportTaskNode is the graph's report hook: one event for a surface, a row in
// the project's index, and — on a final state — one note for the model.
//
// The note rides the STEERING LANE, exactly as a background job's exit does
// (jobs.go). A node's completion is news that arrives while the model is busy,
// it must land at a step boundary rather than inside a tool batch, and from the
// model's side it is a line somebody said. Giving it a second lane would be a
// second ordering rule for the same kind of message.
//
// THE INDEX ROW IS WRITTEN HERE FOR THE SAME REASON THE NOTE IS: this is the
// one place in the build where "this node has finished" is a fact rather than a
// guess, and a row written anywhere else would be a second definition of landed
// (task_index.go).
func (a *Agent) reportTaskNode(node *TaskNode) {
	notice := node.notice()
	a.emitTaskUpdate(notice)
	if notice.State == TaskRunning || notice.State == TaskQueued {
		return
	}
	a.recordTaskIndex(node)
	a.enqueueSteering(taskNote(notice, taskURI(node.journalPath())))
	// SAID ONCE, ACROSS LIVES. The checkpoint records that this node's completion
	// has been announced, so a session resumed from it restores the node as
	// history instead of telling the model that finished work has just landed
	// (task_store.go).
	node.markNoted()
}

// taskNote is what the model reads when a node lands: the outcome, the report,
// and the three facts it cannot infer — what changed, whether the work came
// home, and where the whole story is.
//
// THE VERB IS THE STATE'S OWN WORD, and each one is chosen against the thing it
// must not be mistaken for. The model is about to tell a person what happened,
// in its own sentence, and every wrong word here is a wrong word there:
//
//	finished          the gate let it through, and the report is the evidence
//	                  it went through on
//	failed            somebody looked and made a finding — and when the finding
//	                  was that work is missing, the report says "incomplete — "
//	                  and what is missing
//	needs your look   nobody could look, or nobody would say — which is not the
//	                  same news and does not cascade
//
// NOT ONE OF THEM IS THE HARNESS'S OWN VOCABULARY. The model reads this line and
// says it back to a person in its own words, so "auditor", "verdict", VERIFIED
// and REFUTED must not be in it — the person asked for work, not for a trial,
// and a model handed a courtroom will hold one (task_audit.go's vocabulary law).
// The one word here that comes from the machinery is `reaudit`, and it is there
// because the model has to TYPE it back: a handle is an address, not a finding.
//
// THE TRANSCRIPT URI RIDES THE FIRST LINE, when the node has a journal to point
// at. It is the same handle the `tasks` tool hands out for a node somebody wants
// to read for themselves (task_index.go's TranscriptURI), and it is here so the
// model can hand it over — or read it — without first going looking for the
// row. Empty for a node whose journal this session no longer knows, and then
// the line simply ends after the title.
func taskNote(notice TaskNotice, transcript string) string {
	var note strings.Builder
	verb := "finished"
	switch notice.State {
	case TaskFailed:
		verb = "failed"
	case TaskUnverified:
		// NOT "failed", and the wording is the whole point of the state: the
		// model is about to tell the person what happened, and "failed" would
		// be it reporting a finding nobody made (task_contract.go). It is also
		// not "could not be verified", which was the same sentence in the
		// harness's vocabulary — this says whose problem it now is.
		verb = "needs your look"
	}
	fmt.Fprintf(&note, "task %d %s: %s", notice.ID, verb, notice.Title)
	if transcript != "" {
		note.WriteString(" · transcript " + transcript)
	}
	if notice.Report != "" {
		note.WriteString("\n" + notice.Report)
	}
	if notice.State == TaskUnverified {
		note.WriteString("\nit is neither done nor failed, its branch is kept, and anything waiting on it waits until somebody decides: tasks id " +
			strconv.FormatUint(notice.ID, 10) + " resolve accept|reaudit|refute")
	}
	// AN INCOMPLETE LANDING IS AN INVITATION, NOT A DEAD END. The work was sent
	// back as many times as it was allowed and what is still missing is written
	// above, in the person's terms, with the branch it is sitting on. The one
	// thing the model must not do with that is quietly spend another task on it:
	// the harness has already tried that, at the person's expense, and the next
	// move is theirs to choose. The lead is what says which failure this is —
	// a killed node and a node that ran out of time reach TaskFailed too, and
	// neither of them has a gap anybody could offer to close.
	if notice.State == TaskFailed && strings.HasPrefix(notice.Report, incompleteLead) {
		note.WriteString("\nwhat is missing is above and the branch is kept: offer them a follow-up in their own words before anything else is spent on it")
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
	// Written down before a single tool call runs in it: from here on, a process
	// that dies leaves a checkpoint that knows where this node's work is.
	node.setTree(tree)
	fmt.Fprintf(log, "task %d · %s\nworking in %s\n", node.id, node.title(), tree.dir)

	child, err := a.newTaskAgent(tree.dir, node, "")
	if err != nil {
		node.finish("could not start the task: "+err.Error(), nil, tree.branch, tree.merge)
		return TaskFailed
	}
	defer func() {
		_ = child.Close()
		// The node's spend is the person's, so it is folded into the session's
		// auxiliary usage — the pocket the title and the compaction summary come
		// out of — rather than charged to whichever turn happened to propose it.
		// It is kept ON THE NODE as well, in the same call, because the node
		// outlives its child: a surface asking a landed node what it cost has
		// nobody else to ask (see [TaskNode.spend]).
		a.foldTaskUsage(node, child)
	}()

	// THE ROOM OPENS HERE, because this is the first moment there is anybody in
	// it: from now until the node lands, its events reach whoever is watching
	// and the person's words reach this child's steering lane (task_room.go).
	room := node.openRoom()
	room.speaking(child)

	changed, stopped, runErr := runTaskChild(ctx, child, node.instruction(), tree.dir, node.limits(), room, log)
	report := taskReport(child)

	switch {
	// The threshold comes FIRST because it is the most specific answer: it
	// cancelled the child itself, so every check below would also be true, and
	// each of them would say something less useful than the name of the
	// threshold that fired.
	case stopped != "":
		fmt.Fprintf(log, "%s\n", stopped)
		node.finish(withReport(stopped, report), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
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

	// THE GATE. Everything above is the node's own account of itself; what
	// follows is somebody else's (task_audit.go). Only a VERIFIED verdict
	// reaches comeHome, so only verified work is ever merged onto the person's
	// branch — and a refuted node keeps its branch exactly as a killed one does,
	// because "not proven" is not "throw it away". With the audit row off the
	// gate stands open and the node's own account merges — marked unaudited,
	// because 'done' should never wear 'verified's clothes.
	if !a.config.TaskAudit {
		merge, detail := tree.comeHome(node.title())
		fmt.Fprintf(log, "merge: %s %s (unaudited)\n", merge, detail)
		// THE SETTING KEY IS THE ONE PIECE OF MACHINERY VOCABULARY A PERSON IS
		// ALLOWED TO SEE, and only because it is an ADDRESS: they turned this row
		// off, this is the row's name, and a sentence that translated it would
		// leave them holding a word their settings sheet does not answer to
		// (task_audit.go's vocabulary law). Everything either side of it is plain.
		node.finish(withReport("nothing checked this work: the task.audit setting is off",
			withReport(report, detail)), changed, tree.branch, merge)
		return TaskDone
	}
	// THE GATE MAY SEND THE WORK BACK BEFORE IT ANSWERS. What returns from here
	// is the end of the whole loop — the last verdict, the gaps of every round,
	// and the files and the claim as the LAST worker left them (task_audit.go).
	outcome := a.auditWithRepair(ctx, node, tree, changed, report, log)
	changed, report = outcome.changed, outcome.claim
	verdict := outcome.verdict
	switch {
	case ctx.Err() != nil:
		node.finish(withReport("stopped while its work was being checked", report), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
	case !verdict.answered:
		// NOBODY COULD SAY. Not done — nothing merges on an answer nobody gave —
		// and not failed either, because no finding was made about this work.
		// The node's own claim is kept UNDER the non-answer: whoever is asked to
		// resolve this needs both halves, what the work says it did and what the
		// checker said instead of an answer (task_contract.go's TaskUnverified).
		node.finish(withReport(verdict.lookOutcome(), report), changed, tree.branch, abortedMerge(tree))
		return TaskUnverified
	case !verdict.verified:
		// INCOMPLETE, WITH EVERY ROUND'S GAPS. The node's own claim is dropped
		// exactly as it was before: somebody looked at the work and said what is
		// missing, and that answers the claim.
		node.finish(gapsOutcome(outcome.gaps), changed, tree.branch, abortedMerge(tree))
		return TaskFailed
	}

	merge, detail := tree.comeHome(node.title())
	fmt.Fprintf(log, "merge: %s %s\n", merge, detail)
	// The evidence leads the report: the first thing a person reads off a
	// finished card is what was checked and what was seen, and the node's own
	// words follow as the account they now are. The state says "done" — nothing
	// here says it a second time in the harness's own vocabulary.
	node.finish(withReport(verdict.doneOutcome(), withReport(report, detail)), changed, tree.branch, merge)
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

// runTaskChild submits the brief, consumes the node's own events internally,
// and enforces the two thresholds from the same stream.
//
// The events are NOT forwarded INTO THE CONVERSATION. A node's tool rows belong
// to its journal, not to the chat: a person watching a chat must not have forty
// of somebody else's greps scroll past. What is kept for the chat is the one
// thing the person needs to see afterwards — which files the node wrote — read
// off the edit and write calls as they end.
//
// They ARE forwarded into the node's own ROOM, which is the other half of that
// same rule: the events a chat must not carry are exactly the events somebody
// who walked into this node's page came to see (task_room.go). The publish is
// the first thing this loop does with an event, so a watcher sees the child's
// narrative in the order it happened rather than in the order this side of the
// wall got round to counting it — and it runs for a nil room too, which is the
// ordinary case of a node nobody is watching.
//
// THE STEP IS ONE FINISHED TOOL CALL, and it is the only unit available from
// out here: the child's model round-trips are inside its own loop, and this
// side of the wall sees the calls they produce. PROGRESS is either half of the
// job — a SUCCESSFUL edit or write (EventToolEnd and not EventToolFailed,
// because an edit whose oldText did not match changed nothing and a node
// repeating it is the exact spin the counter exists to catch), or a step that
// TAUGHT the node something it did not know ([taughtSomething]). What the
// counter kills is the third thing: the same call again, changing nothing,
// learning nothing.
//
// The cancel is this function's own, hung off the node's context, so tripping a
// threshold ends the child the way `jobs kill` does — a cancelled turn, its
// partial work kept, its branch intact — and the caller is told WHICH threshold
// fired rather than being left to infer it from a context error that has three
// possible causes.
func runTaskChild(ctx context.Context, child *Agent, instruction, dir string, limits taskLimits, room *taskRoom, log io.Writer) ([]string, string, error) {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	events, err := child.Submit(runCtx, instruction)
	if err != nil {
		return nil, "", err
	}
	var (
		changed  []string
		seen     = map[string]bool{}
		seenInfo = map[string]bool{}
		lastDirt string
		failure  error
		stopped  string
		steps    int
		idle     int
	)
	for event := range events {
		room.publish(event)
		switch event.Kind {
		case EventToolBegin:
			fmt.Fprintf(log, "· %s\n", event.Hint)
		case EventToolEnd, EventToolFailed:
			steps++
			path, wrote := changedPath(event, dir)
			switch {
			case wrote && event.Kind == EventToolEnd:
				idle = 0
				if !seen[path] {
					seen[path] = true
					changed = append(changed, path)
				}
			case taughtSomething(event, seenInfo, dir, &lastDirt):
				// EXPLORATION IS PROGRESS. A research node may never write
				// until its final words; a build node may spend its first
				// dozen steps reading. What stops a node is SPINNING — the
				// same target again, no new dirt — not the absence of an
				// edit (PMCoder's "reads saturated", not "reads happened").
				idle = 0
			default:
				idle++
			}
			// Named once. The loop keeps draining after the cancel — the child
			// is still finishing its batch and closing its stream — and a second
			// threshold tripping on the way out must not rewrite the reason the
			// node was stopped.
			if stopped != "" {
				continue
			}
			switch {
			case steps >= limits.maxSteps:
				stopped = fmt.Sprintf("stopped: %d steps and no finish", limits.maxSteps)
				stop()
			case idle >= limits.noProgress:
				stopped = fmt.Sprintf("stopped: %d steps without progress", limits.noProgress)
				stop()
			}
		case EventError:
			failure = event.Err
		}
	}
	return changed, stopped, failure
}

// knowledgeTools are the hands on a node's belt (tools.go's [Agent.belt]) that
// return what the world is rather than change it. A call to one of them aimed
// at a target the node has not aimed at before is PROGRESS, because the node
// came back knowing something it did not know a step ago.
//
// It is the whole read-only half of the belt and not a shortlist, which is the
// point: the counter used to name six of them, and every other look at the
// world counted as a stall — so a node that read a scanned page, asked after
// the build it started, or recalled its own working state was punished for
// exploring. When a hand is added to belt(), it belongs here or it belongs to
// the paragraph below, and one of the two is always true.
//
// What is deliberately ABSENT: edit and write (they are counted as the diff
// they are, one branch up), note, forget, track and commit (a node writing its
// own memory again is not learning anything), and generate_image (it produces,
// it does not inform). bash is absent because it is BOTH, and is handled on its
// own below.
var knowledgeTools = map[string]bool{
	"read":          true,
	"read_document": true,
	"ls":            true,
	"grep":          true,
	"find":          true,
	"web_search":    true,
	"web_fetch":     true,
	"jobs":          true,
	"recall":        true,
}

// taughtSomething reports whether one call advanced the node's KNOWLEDGE rather
// than its diff: a knowledge tool aimed at a target it has not aimed at before
// (the same search retried is not new information, and SUCCESS is not required
// — a new target that failed still taught the node that it failed), or a bash
// that left new dirt in the worktree or ran a command the node had not run. The
// seen map keys tool+target so re-reading one file while reading another new one
// still counts exactly once.
//
// The judgement is made on the CALL and never on the result: [Event.Output] is
// a display copy, capped, and a counter that read it would be deciding a node's
// life from bytes that were truncated for a person's screen.
func taughtSomething(event Event, seen map[string]bool, dir string, lastDirt *string) bool {
	if event.Tool == "bash" {
		// BASH IS BOTH HANDS. `go build` writes, `go test ./...`, `git log` and
		// `rg` do not, and the tool name says nothing about which one this was.
		// So both are asked: new dirt in the worktree is the diff moving, and a
		// command the node has never run is the world answering a question it
		// has never asked. Only the same command again, changing nothing, is
		// the spin — which is the same law every other tool here is read by,
		// and it is what a workspace that is not a repository (worktreeDirt is
		// "" forever) is now judged by instead of by nothing at all.
		//
		// The novelty is recorded first, and unconditionally: a command that
		// dirtied the tree must not also be spendable as a fresh target the
		// next time it is run.
		fresh := freshTarget(event, seen)
		dirt := worktreeDirt(dir)
		if dirt != *lastDirt {
			*lastDirt = dirt
			return true
		}
		return fresh
	}
	if !knowledgeTools[event.Tool] {
		return false
	}
	return freshTarget(event, seen)
}

// freshTarget reports whether this call aimed somewhere the node has not aimed
// before, and records it either way.
//
// The WHOLE CALL is the target, not one field of it: paging one long file by
// offset is exploration, fetching one page twice is a spin, and only the args in
// full tell them apart. Display-capped args compare fine — two calls capped at
// the same mark are the same call as far as anyone can see.
func freshTarget(event Event, seen map[string]bool) bool {
	key := event.Tool + " " + strings.TrimSpace(event.Args)
	if seen[key] {
		return false
	}
	seen[key] = true
	return true
}

// argField reads one string field out of a tool call's display args. The args
// arrive compacted for show; a field that did not survive the cap simply
// counts as no-new-target, which is the safe direction.
func argField(args, field string) string {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	var value string
	if raw, ok := parsed[field]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

// worktreeDirt is the worktree's dirty fingerprint: the sorted porcelain
// listing hashed, so a bash that creates, modifies or deletes a file reads
// as progress while one that only inspects does not. A non-git directory
// answers "" — stable, so it never moves the counter either way.
func worktreeDirt(dir string) string {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(out)
	return string(sum[:8])
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
	return firstLines(lastSaid(child), taskReportLines)
}

// lastSaid is an agent's final assistant message, whole. It is what taskReport
// clips and what the auditor's verdict is parsed out of (task_audit.go) — a
// verdict is four lines and a report is three, and cutting before the parse
// would be the harness deciding a verdict was too long to read.
func lastSaid(child *Agent) string {
	entries := child.Transcript()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Role != "assistant" || strings.TrimSpace(entry.Text) == "" {
			continue
		}
		return entry.Text
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

// foldTaskUsage charges the node's spend to the session, and records it on the
// node on the way past — this is the only moment anybody can ask a child agent
// what it cost, because the line after this one closes it.
func (a *Agent) foldTaskUsage(node *TaskNode, child *Agent) {
	used := child.Usage()
	node.addSpend(used.CostUSD)
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
// It inherits the conversation's client, window and capabilities because a node
// is the same worker doing the same job somewhere quieter. It inherits neither
// the transcript nor the memory file: the brief is the node's whole world by
// construction, and two agents appending to one memory file would be two writers
// on a document neither can see the other editing.
//
// THE MODEL IS THE NODE'S OWN, and the conversation's only when the node has
// none (taskmodel.go). It is read BEFORE this takes a.mu, because the spec lives
// under the graph's lock and this package takes one lock at a time.
func (a *Agent) newTaskAgent(dir string, node *TaskNode, suffix string) (*Agent, error) {
	model := node.model()
	a.mu.Lock()
	parent := a.config
	if strings.TrimSpace(model) == "" {
		model = a.model
	}
	window := parent.ContextWindow
	if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(a.model)) {
		// A WINDOW MEASURED FOR ANOTHER MODEL IS NOT A FACT ABOUT THIS ONE. The
		// figure the surface handed down is the conversation model's, and a node
		// running elsewhere gets zero — this package's own conservative default —
		// rather than a number that could be four times the window it actually
		// has. Compacting early costs a summary; overflowing costs the turn.
		window = 0
	}
	client := unwrapCompleter(a.client)
	journal := taskJournalPath(a.sessionID(), node.id, suffix)
	a.mu.Unlock()

	// Written on the node the moment it is minted: the name carries a timestamp,
	// so this is the only moment anybody can learn it, and [Agent.TaskJournal] is
	// how a person opens the node's whole transcript afterwards (task_room.go).
	//
	// A REPAIR ROUND DOES NOT TAKE THE NODE'S JOURNAL OVER. It is a worker with a
	// suffix, its transcript sits beside the node's in the same directory under
	// its own name, and the node keeps pointing at the run that IS the node —
	// the one the person's "read the transcript" means. Repointing it each round
	// would leave the index and the completion note aimed at the last ten percent
	// of a job, with the ninety unreachable.
	if suffix == "" {
		node.setJournal(journal)
	}

	return newAgent(Config{
		Workspace:      dir,
		Model:          model,
		APIKey:         parent.APIKey,
		BaseURL:        parent.BaseURL,
		ContextWindow:  window,
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
		// A node reads documents on the rung the person chose, like the
		// conversation does (tools_doc.go): the same worker, working somewhere
		// quieter, must not silently drop to a different engine — or to a paid
		// one — because it is running in a worktree.
		DocumentEngine: parent.DocumentEngine,
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
// ~/.aforge/v3/tasks/<session>/<when>_<id>.jsonl, and everything else the node
// spent an agent on beside it under a suffix: <when>_<id>-audit-<nonce>.jsonl
// for one check, <when>_<id>-repair1.jsonl for one repair round.
//
// It is a REAL SESSION FILE — the node is an agent, and everything it did is
// resumable and readable with the same tools — kept under the conversation that
// asked for it, so "what did that task actually do" is one directory listing
// away. A machine with no home directory gets an in-memory node instead of a
// failed one: the journal is evidence, not a prerequisite.
//
// The audit gets a file of its own rather than a section of the node's, because
// it is a different agent with a different context: two transcripts written into
// one journal would be the exact context mixing the audit exists to avoid, and a
// person asking "what did the auditor actually run" wants a file to open. The
// SUFFIX IS THE CALLER'S TO MAKE UNIQUE — the stamp here is only good to the
// second, and [newAgent] resumes a file that is already there, so two agents
// handed one path would be one agent with two names (task_audit.go).
func taskJournalPath(session string, id uint64, suffix string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := fmt.Sprintf("%s_%d%s.jsonl", time.Now().Format("20060102-150405"), id, suffix)
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
	if !stageTaskWork(dir) {
		return
	}
	_, _ = git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--no-verify", "-m", "task: "+clip(firstLine(title), 72))
}

// stageTaskWork puts everything the node wrote into the worktree's index, and
// it is the step BOTH the audit and the commit need.
//
// The audit needs it because `git diff` shows tracked files only: a node whose
// whole change was three new files has an empty diff and a full index, and an
// auditor reading the empty one refutes good work for a reason that has nothing
// to do with the work. `git diff --cached` in a staged tree shows all of it.
// The commit needs it because git only moves what has been staged. Running it
// twice is free — the second add finds nothing new — and running it once from
// two places would be the audit and the merge disagreeing about what the node
// wrote.
//
// The harness's own droppings are not the node's work: a background job the
// node started wrote its log under the workspace (jobs.go), and a build log in
// the diff — or merged into the person's branch — is noise they did not ask for.
func stageTaskWork(dir string) bool {
	if _, err := git(dir, "add", "-A"); err != nil {
		return false
	}
	_, _ = git(dir, "reset", "--quiet", "--", ".aforge-v3")
	return true
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
