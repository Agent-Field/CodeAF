package session

// STOPPING WORK, WHATEVER KIND IT IS.
//
// This session drives four kinds of thing that outlive the sentence that asked
// for them: a task node in the graph, an adaptive run, a sub-harness run, and a
// harness being designed. Every one of them already had SOME way to end — a
// context, a goroutine, a channel — and not one of them had a way a person
// could reach. `jobs kill` is the model's tool and takes the registry's own
// numbers; the fuel gate's "stop" only exists once a run has spent its tank;
// and a design in flight could be ended by closing the session and by nothing
// else. So the answer to "how do I stop this" was, everywhere on this surface,
// "say so in words and hope the model does it".
//
// [Agent.Cancel] is the one door. It takes an id, works out which kind of work
// that id names, and ends it — and the four endings differ only in what they
// have to cut.
//
// WHAT A STOP MEANS, EXACTLY. It means STOP SPENDING NOW, and every part of
// this file bends around that one sentence:
//
//	in flight    its context is cut where it stands, and its partial output is
//	             discarded — a half-answer handed on as a fact is worse than no
//	             answer at all
//	queued       dropped instantly; there is nothing running to wait for
//	settled      it keeps what it produced, and the trace stays whole
//
// WHAT IS NEVER THROWN AWAY IS THE TRACE. A stopped task keeps its branch and
// its transcript, a stopped run keeps the nodes that finished, their digests
// and its notes, and both keep what they spent — because a person who stops
// work is deciding not to spend MORE on it, not asking for the last twenty
// minutes to be deleted.
//
// IT IS IDEMPOTENT. Two presses, or a press on work that has already landed,
// is a sentence saying so and nothing else. A confirmation a surface draws and
// a key a person leans on must never be two different amounts of stopping.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// The four kinds of work an id can name, spelled as [Agent.Cancel] takes them:
// `task:7`, `run:2`, `harness:4`, `design:1`.
//
// THE PREFIX IS NOT DECORATION. The four counters that mint these ids are four
// counters — the graph's, the run register's, the harness lane's — so "7" is a
// task AND a run AND a design, and a surface handing over a bare number would
// be asking this file to guess which piece of somebody's work to end. A bare
// number is read as a TASK and only as a task, because that is the id space
// every surface on this program already had before any of the others existed.
const (
	CancelTask    = "task"
	CancelRun     = "run"
	CancelHarness = "harness"
	CancelDesign  = "design"
)

// Cancel stops one piece of work and answers with the line to show for it.
//
// The line is the point of the return value: a surface that asked for something
// to be stopped is owed a sentence saying what that did, in the same way a
// surface that answered the fuel gate is ([Agent.ResolveOrchestrate]). An error
// is only ever an id this session cannot place — an unknown kind, an id that is
// not a number, or work this session has never heard of.
func (a *Agent) Cancel(id string) (string, error) {
	kind, rest, prefixed := strings.Cut(strings.TrimSpace(id), ":")
	if !prefixed {
		kind, rest = CancelTask, kind
	}
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case CancelTask:
		number, err := cancelNumber(CancelTask, rest)
		if err != nil {
			return "", err
		}
		return a.cancelTask(number)
	case CancelRun:
		return a.cancelRun(rest)
	case CancelHarness:
		number, err := cancelNumber(CancelHarness, rest)
		if err != nil {
			return "", err
		}
		return a.cancelHarnessRun(number)
	case CancelDesign:
		number, err := cancelNumber(CancelDesign, rest)
		if err != nil {
			return "", err
		}
		return a.cancelDesign(number)
	}
	return "", fmt.Errorf("%q names no kind of work this session can stop", id)
}

func cancelNumber(kind, rest string) (uint64, error) {
	number, err := strconv.ParseUint(rest, 10, 64)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("%q is not a %s this session can stop", rest, kind)
	}
	return number, nil
}

// ── a task node ─────────────────────────────────────────────────────────────

// cancelTask stops one node of the work graph.
func (a *Agent) cancelTask(id uint64) (string, error) {
	a.mu.Lock()
	graph := a.tasks
	a.mu.Unlock()
	if graph == nil {
		return "", fmt.Errorf("there is no task %d in this session", id)
	}
	return graph.stop(id)
}

// stop ends one node on a person's word, and answers with the line to show.
//
// THE TWO STATES END DIFFERENTLY AND SAY SO. A running node is CUT: its context
// dies, its child agent's turn ends where it stands, and the runner settles it
// the way it settles every other early ending — failed, its branch kept, its
// report saying what happened (task_run.go's workTaskNode). Nothing here waits
// for that; the landing arrives on the lane a moment later like any other.
//
// A queued node is dropped IN THIS FUNCTION, because there is nothing to wait
// for: it never started, so it has no context to cut and no child to end. It is
// dropped exactly the way the frontier's own cascade drops one — the state
// moves, `done` closes, the checkpoint is written, and NO SLOT IS HANDED BACK,
// because a node that never ran never took one and a decrement here would be
// this node quietly raising the concurrency cap for everybody else.
func (g *TaskGraph) stop(id uint64) (string, error) {
	g.mu.Lock()
	node := g.nodes[id]
	if node == nil {
		g.mu.Unlock()
		return "", fmt.Errorf("there is no task %d in this session", id)
	}
	name := taskStopName(id, node.spec.title)
	var (
		cut     context.CancelFunc
		dropped bool
		line    string
	)
	switch {
	case node.stopped && node.state == TaskRunning:
		// Already stopping. A second press is a person leaning on a key, not a
		// second decision, and the honest answer is what is already happening.
		g.mu.Unlock()
		return name + " is already stopping", nil
	case node.state == TaskRunning:
		node.stopped = true
		cut = node.cancel
		// A RUNNING NODE ALWAYS HAS A HANDLE — [Agent.runTaskNode] sets it before
		// the first line of work, and a node restored from a checkpoint is turned
		// into a failed one before the graph ever holds it (task_store.go's
		// interrupt). If one somehow has none, nothing is ever going to settle it,
		// so it settles HERE: a card promising "stopping" over work that nothing
		// is doing is the one answer this must not give.
		dropped = cut == nil
		if dropped {
			node.state, node.report, node.held = TaskFailed, taskStoppedWord, ""
			line = "stopped " + name
			break
		}
		// THE BRANCH IS NAMED IN THE PROMISE AND NOT IN THE PAST TENSE. What the
		// node wrote is on its worktree branch and stays there whatever ends it
		// (task_run.go's abortedMerge); the branch's actual name arrives on the
		// landing card, a moment after this line.
		line = "stopping " + name + " — its branch is kept"
	case node.state == TaskQueued:
		node.stopped = true
		node.state, node.report, node.held = TaskFailed, taskStoppedQueuedWord, ""
		dropped = true
		line = "stopped " + name + " before it started"
	default:
		g.mu.Unlock()
		return name + " has already finished; there is nothing to stop", nil
	}
	g.mu.Unlock()

	if dropped {
		close(node.done)
		g.checkpoint()
		g.announce(node)
		// The cascade: whatever was waiting on this node can never be briefed
		// from it, and the frontier is the one place that judgement is made.
		g.runFrontier()
		return line, nil
	}
	cut()
	return line, nil
}

// The two reports a node settled by this file carries. Both say the one thing
// somebody reading the row afterwards needs — whether there is anything to go
// and look at.
const (
	taskStoppedWord       = "stopped"
	taskStoppedQueuedWord = "stopped before it started"
)

// taskStopName is how a stop line names one node: its own title where it has
// one, and its id where it does not — the floor [taskTitleOf] keeps on the
// surface, kept here for the same reason. A sentence about "task 7" is one a
// person can say out loud.
func taskStopName(id uint64, title string) string {
	if title = strings.TrimSpace(title); title == "" {
		return fmt.Sprintf("task %d", id)
	}
	return fmt.Sprintf("task %d (%s)", id, clip(title, 60))
}

// ── an adaptive run ─────────────────────────────────────────────────────────

// cancelRun stops one adaptive run: the in-flight nodes are cut, the pending
// ones dropped, and the run settles with its partial trace kept
// ([orchestrate.Orchestrator.Cancel] states the whole law).
//
// WHAT IT WAS AND WHAT IT COST IS NOT SAID HERE. The figures a person wants —
// what was spent, how many nodes landed — are only final once the cut nodes
// have come home, which is a moment after this returns, so the sentence
// carrying them is written where the run actually lands
// ([Agent.landOrchestrate]). Quoting them here would be quoting a meter that is
// still moving.
func (a *Agent) cancelRun(id string) (string, error) {
	live, known := a.orchestration(id)
	if !known {
		return "", fmt.Errorf("there is no run %q in this session", id)
	}
	if live.run.Snapshot().Done {
		return "that run has already finished; there is nothing to stop", nil
	}
	live.run.Cancel()
	return orchestrate.StoppedWord, nil
}

// stoppedRunNote is what a stopped run says to the conversation once it has
// actually stopped: what it spent, and how much of the graph it got through.
//
// It is a function of the snapshot alone so that the sentence a person reads
// and the figures the run settled on cannot be two different readings of the
// same run.
func stoppedRunNote(snap orchestrate.Snapshot) string {
	return fmt.Sprintf("stopped — %s spent, %d of %d nodes done",
		orchestrate.Dollars(snap.Fuel.Spent), doneNodes(snap), len(snap.Nodes))
}

// ── a sub-harness run ───────────────────────────────────────────────────────

// beginHarnessRun puts one sub-harness run on the register: the context it
// runs on, the id a surface names it by, and the way to take it off again.
//
// A RUN HAS NO OTHER HANDLE. It happens inside one tool-less turn, on the
// turn's own context (harness.go), and the only thing that could have ended one
// was interrupting the whole turn. The register is what lets [Agent.Cancel]
// name it — and it is emptied by the same defer that ends it, so a session that
// never runs a harness pays for nothing but a nil map.
func (a *Agent) beginHarnessRun(ctx context.Context) (context.Context, uint64, func()) {
	runCtx, cut := context.WithCancel(ctx)
	a.mu.Lock()
	a.harnessSeq++
	id := a.harnessSeq
	if a.harnessRuns == nil {
		a.harnessRuns = make(map[uint64]context.CancelFunc, 1)
	}
	a.harnessRuns[id] = cut
	a.mu.Unlock()
	return runCtx, id, func() {
		a.mu.Lock()
		delete(a.harnessRuns, id)
		a.mu.Unlock()
		cut()
	}
}

// cancelHarnessRun ends one sub-harness run where it stands. The turn it is
// inside reports what the run had reached when it was cut, which is the harness
// runner's own bargain: a failed run still reports its trail, because the trail
// is the one thing worth having when a run went wrong (chatv3_harness.go).
func (a *Agent) cancelHarnessRun(id uint64) (string, error) {
	a.mu.Lock()
	cut := a.harnessRuns[id]
	a.mu.Unlock()
	if cut == nil {
		return "", fmt.Errorf("there is no harness run %d in this session", id)
	}
	cut()
	return "stopping the harness run; its trail is kept", nil
}

// ── a harness being designed ────────────────────────────────────────────────

// cancelDesign ends one harness design in flight.
//
// IT SAYS SO IN THE TRANSCRIPT ITSELF, which none of the other three has to do.
// A design that loses its context returns without a word — deliberately, because
// the ordinary way it loses one is the session closing and there is nobody left
// to tell (harness_build.go's designHarness). Stopped on purpose there IS
// somebody, and silence would leave them watching for a card that is never
// coming.
func (a *Agent) cancelDesign(id uint64) (string, error) {
	a.mu.Lock()
	design := a.harnessDesigns[id]
	a.mu.Unlock()
	if design == nil {
		return "", fmt.Errorf("there is no harness design %d in this session", id)
	}
	design.cancel()
	a.noteHarnessDesign(designStoppedWord)
	return designStoppedWord, nil
}

const designStoppedWord = "harness design stopped; nothing was saved"
