package session

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// THE WORK TREE IS ONE SNAPSHOT BOTH SURFACES READ. The rail's folded window
// and a room's full board are two depths of the same picture, and the moment
// they render from two accountings they will disagree about what is running —
// so every surface that says "what is working right now" reads this door and
// nothing else. (docs/HOME-BRIDGE.md states the one-source-of-truth law; this
// file is that law applied to live work.)
//
// A node is a WORKER: a task's own thread, a planned node of an orchestrated
// run, or a child born mid-run when the division gate fired. The tree does not
// say which road created a child — to the person a hand at work is a hand at
// work, and the room is where provenance lives.

// WorkState is the little a row needs to draw a worker: it is moving, it is
// waiting for something (a free hand, an answer), or it has landed.
type WorkState string

const (
	WorkRunning WorkState = "running"
	WorkWaiting WorkState = "waiting"
	WorkDone    WorkState = "done"
)

// WorkNode is one worker and the workers under it.
type WorkNode struct {
	ID       string
	Title    string
	State    WorkState
	Born     time.Time
	Children []WorkNode
}

// WorkingNow is the session's live work as one tree: the session's tasks at
// the top, their workers beneath, born children included the moment they
// exist. An idle session answers nil, and nil renders as nothing — the
// emptiness law.
//
// ── WHAT IS IN THE TREE AND WHAT IS NOT ──
//
// A ROOT IS DRAWN ONLY WHILE SOMETHING UNDER IT IS ALIVE, and once it is drawn
// EVERY worker under it is drawn, settled ones included. The two halves of that
// rule are one decision. A session accumulates finished tasks for as long as it
// runs, and a tree that kept all of them would grow without bound and would say
// "working right now" about work that finished an hour ago — the roster is
// where history is read. But a task that split into three parts and has two of
// them home is a board with three rows on it, and dropping the two that landed
// would leave a person looking at the one part still going with no way to see
// it was ever a division. So liveness decides the ROOT and the root decides the
// family.
//
// AN ID IS SOMETHING A SURFACE CAN ACT ON. The spellings are cancel.go's, the
// one place in this package where both kinds of work already share a
// vocabulary: `task:4` for a node of the task graph, `run:2` for an adaptive
// run, `run:2/node-a` for one planned node inside one, and `job:3` for a
// background job. A reader can hand any of them except the planned node
// straight to [Agent.Cancel]; nothing else in this package mints
// an id that means two different things depending on which kind of work you
// thought you were looking at.
//
// BORN IS WHEN THE WORKER STARTED, and it is deliberately zero for a worker
// that has not started yet — one still waiting for a free hand has no age to
// draw, and zero renders as nothing by the emptiness law. An adaptive run's
// planned nodes carry no start time at all in [orchestrate.Snapshot], so they
// answer zero for the same reason: the honest answer to a question the engine
// cannot answer is nothing.
//
// NEVER FAKE LIVENESS. Every row here is a worker this process is holding — a
// node in the graph or a node on a run's own snapshot. Nothing is invented for
// a shape that has not been admitted yet.
// A BACKGROUND JOB IS A HAND AT WORK. It is not an agent and it has no worker
// under it, but the question this door answers is "is anything happening", and a
// nine-minute command running under `bash background:true` is the plainest
// possible yes. It joins the tree for the same reason it joins the roster
// (jobrow.go): the door that started the work does not change where the work
// shows.
func (a *Agent) WorkingNow() []WorkNode {
	nodes := append(a.tasker().workingNow(), a.runsWorkingNow()...)
	return append(nodes, a.jobsWorkingNow()...)
}

// workingNow is the task graph's half of the tree. It is nil-safe: a session
// that never groomed a task has no graph, and "no tasks" is the honest answer
// rather than a graph built to answer one question ([Agent.tasker]).
func (g *TaskGraph) workingNow() []WorkNode {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// The children of each parent, in ADMISSION order — the same order the
	// frontier starts them in, so two surfaces drawing this tree draw the parts
	// of a division in the same order the person watched them appear.
	kids := make(map[uint64][]uint64, len(g.order))
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.parent != 0 {
			kids[node.parent] = append(kids[node.parent], id)
		}
	}
	var out []WorkNode
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.parent != 0 {
			continue
		}
		if row, live := g.workRowLocked(node, kids); live {
			out = append(out, row)
		}
	}
	return out
}

// workRowLocked draws one node and everything under it, and says whether
// anything in that subtree is still alive. The liveness answer is what the
// caller uses to decide whether a ROOT belongs in the tree at all; inside a
// root it is ignored, which is the family half of the rule stated on
// [Agent.WorkingNow].
//
// The recursion cannot loop: a node's parent is always a node that was reserved
// before it ([TaskGraph.reserve] is monotonic and a parent exists before it can
// propose), so the parent edges are a forest and not a graph.
func (g *TaskGraph) workRowLocked(node *TaskNode, kids map[uint64][]uint64) (WorkNode, bool) {
	row := WorkNode{
		ID:    CancelTask + ":" + strconv.FormatUint(node.id, 10),
		Title: node.spec.title,
		State: workStateLocked(node),
		Born:  node.started,
	}
	live := !node.state.settled()
	for _, id := range kids[node.id] {
		child := g.nodes[id]
		if child == nil {
			continue
		}
		childRow, childLive := g.workRowLocked(child, kids)
		row.Children = append(row.Children, childRow)
		live = live || childLive
	}
	return row, live
}

// workStateLocked is the whole mapping from a node's life to the three words a
// row needs.
//
// WAITING IS TWO DIFFERENT WAITS AND ONE WORD, which is [WorkWaiting]'s own
// statement of itself. A QUEUED node is waiting for a free hand — the person's
// own cap, or this machine's load, holding it on the frontier
// ([TaskGraph.runFrontier]). A PARKED node is waiting on an answer: it has
// handed its lane back and is sitting on the reports of the parts it divided
// into ([TaskGraph.park]). A PACED node is waiting on the provider. All three
// are a worker that exists and is not moving, and a person reading a rail wants
// to know that and not which of the three it is.
func workStateLocked(node *TaskNode) WorkState {
	switch {
	case node.state.settled():
		return WorkDone
	case node.state == TaskQueued, node.parked, node.paced > 0:
		return WorkWaiting
	default:
		return WorkRunning
	}
}

// runsWorkingNow is the adaptive runs' half of the tree: the run at the top and
// its planned nodes beneath, which is the same two depths a task and its parts
// are drawn at.
//
// THE SNAPSHOT IS TAKEN OUTSIDE a.mu. Each run has a lock of its own, and
// asking one for its shape while holding the session's would put the session's
// lock underneath every orchestrator's — so the registry is copied out first
// and the questions are asked afterwards.
func (a *Agent) runsWorkingNow() []WorkNode {
	a.mu.Lock()
	ids := make([]string, 0, len(a.orchestrations))
	live := make([]*orchestration, 0, len(a.orchestrations))
	for id := range a.orchestrations {
		ids = append(ids, id)
	}
	// A map has no order and a rail must not shuffle. Run ids are the run's
	// number written out ([Agent.RunOrchestrate]), so they sort as numbers and
	// fall back to their text for anything a test scripted by hand.
	sort.Slice(ids, func(i, j int) bool {
		first, firstErr := strconv.ParseUint(ids[i], 10, 64)
		second, secondErr := strconv.ParseUint(ids[j], 10, 64)
		if firstErr == nil && secondErr == nil {
			return first < second
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		live = append(live, a.orchestrations[id])
	}
	a.mu.Unlock()

	var out []WorkNode
	for i, entry := range live {
		if entry == nil || entry.run == nil {
			continue
		}
		snap := entry.run.Snapshot()
		// A finished run is left in the registry on purpose — its snapshot is
		// what somebody opens the room to read afterwards ([Agent.settleOrchestrate])
		// — and it is not live work.
		if snap.Done {
			continue
		}
		row := WorkNode{
			ID:    CancelRun + ":" + ids[i],
			Title: clip(firstLine(snap.Goal), titleLimit),
			Born:  entry.born,
			State: WorkRunning,
		}
		// A PAUSED RUN IS WAITING ON AN ANSWER: it is out of fuel and standing at
		// the gate for the person to top it up or finish on what is done
		// ([Agent.ResolveOrchestrate]).
		if snap.Paused {
			row.State = WorkWaiting
		}
		for _, node := range snap.Nodes {
			row.Children = append(row.Children, WorkNode{
				ID: CancelRun + ":" + ids[i] + "/" + node.ID,
				// THE NODE'S OWN NAME, never its brief and never its id. A planned
				// node's goal is written to a worker in the second person and runs to a
				// paragraph (internal/orchestrate's law 4), so a row cut from its head
				// named every worker of a wide run "You are a". The frontier settles
				// this field once and the run's namer writes into the same one, so this
				// tree reads it rather than deriving a second answer that would never
				// hear about the first.
				Title: node.Title,
				State: runWorkState(node.State),
			})
		}
		out = append(out, row)
	}
	return out
}

// runWorkState maps a planned node's life onto the same three words a task
// node's does. A node whose needs are unmet and one whose needs are met but
// which has no slot are both waiting for a free hand, which is the one thing
// [WorkWaiting] says.
func runWorkState(state orchestrate.State) WorkState {
	switch state {
	case orchestrate.Running:
		return WorkRunning
	case orchestrate.Queued, orchestrate.Ready:
		return WorkWaiting
	default:
		return WorkDone
	}
}

// WorkPath is the little a surface needs to walk from an id back to the work:
// the kind word and the rest. It is the inverse of the spellings
// [Agent.WorkingNow] mints, written here so a reader never has to take the
// format apart with its own strings.Cut.
func WorkPath(id string) (kind, rest string) {
	kind, rest, found := strings.Cut(strings.TrimSpace(id), ":")
	if !found {
		return "", strings.TrimSpace(id)
	}
	return kind, rest
}

// CountWorking is the one number the head of a rail or a pulse line quotes:
// every node in the tree that is running right now, at every depth.
func CountWorking(nodes []WorkNode) int {
	count := 0
	for _, node := range nodes {
		if node.State == WorkRunning {
			count++
		}
		count += CountWorking(node.Children)
	}
	return count
}
