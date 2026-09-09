package session

// turnhandoff.go answers ONE question for the end of a turn: did this turn put
// the work THE CURRENT REQUEST asked for into a task that is still live?
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// "Please hand this work to a task: run ./slow-build.sh, wait for it to finish,
// and tell me the marker it wrote. Start it now; keep the main conversation
// available while it runs." The turn proposed the task, said it was started and
// that the conversation stayed free, and stopped — which is the whole of what
// was asked for. The end-of-turn reader was then asked whether the ASK was
// finished, answered no (the marker is not known yet, because the build is
// running), and the turn was re-opened with [checkpointCarryOnNote]. The model,
// with nothing left to do, polled `tasks` and started a watch over its own
// running task.
//
// THE TURN WAS FINISHED AND THE OUTCOME WAS NOT. The reader only ever answers
// the second, and a live node is what makes the difference safe: its landing
// starts a turn here by itself ([Agent.reportTaskNode], [Agent.deliverTaskNote])
// and THAT turn is read for what remains, at wake prices, with the report in
// front of it.
//
// ── THE GUARANTEE, EXACTLY ──────────────────────────────────────────────────
//
// The gate opens only when ALL of these hold, and each is a runtime fact rather
// than a reading of anything the model said:
//
//	 1. this agent is a conversation, not a task worker ([Config.InTask]);
//	 2. a turn is running, and the node was admitted BY THIS AGENT DURING IT
//	    ([TaskGraph.admit] stamps every node it takes, whichever door opened it);
//	 3. the person has said nothing since that admission — same turn number AND
//	    same steer number ([requestEpoch]);
//	 4. that node is queued or running.
//
// ── AND WHAT IT DOES NOT CLAIM ──────────────────────────────────────────────
//
// IT IS AN OWNERSHIP SIGNAL, NOT A COMPLETION PROOF. It says the request's work
// has an owner that will report; it does not say every clause of the request was
// delegated. A turn that hands one part off and quietly drops another ends here
// too, and what catches that is the woken turn's own reading when the part
// lands, not this gate. Nothing is marked done, no acceptance is answered, and
// the goal reading is deferred rather than skipped.
//
// QUEUED OR RUNNING IS WHAT THE GRAPH KNOWS, AND IT IS NOT A LIVENESS PROOF. A
// node held behind a slot, the machine governor or a dependency stays queued for
// as long as that lasts, and a running node that has stopped making progress
// still reads as running; this file invents no further state to pretend
// otherwise. What bounds both is the graph's own machinery — the frontier, the
// step and no-progress caps, the deadline — every one of which ends the node and
// wakes this session with the news.

// requestEpoch is WHICH REQUEST a turn is working on, as two numbers the runtime
// already keeps: the turn ([Agent.turnSeq]) and the sentences the person has
// spliced into it ([Agent.steerSeq], minted by [Agent.Steer]).
//
// THE STEER HALF IS WHAT MAKES IT A REQUEST AND NOT A TURN. A steer does not
// start a new turn — it lands at the next boundary of the running one — so a
// turn number alone would let work handed out before a correction stand as the
// answer to the correction. A person who typed anything at all since the
// handoff moves this number, and the turn is read as it would have been.
type requestEpoch struct {
	turn  uint64
	steer uint64
}

// live says an epoch names a request at all. The zero epoch is "no turn was
// running", which is what work opened between turns carries.
func (e requestEpoch) live() bool { return e.turn != 0 }

// requestEpochAt is the request one agent is working on right now, read under
// the lock [Agent.Steer] mints its id inside, so the pair cannot be torn.
func requestEpochAt(admitter *Agent) requestEpoch {
	if admitter == nil {
		return requestEpoch{}
	}
	admitter.mu.Lock()
	defer admitter.mu.Unlock()
	if !admitter.running {
		return requestEpoch{}
	}
	return requestEpoch{turn: admitter.turnSeq, steer: admitter.steerSeq.Load()}
}

// admittingAgent is whose turn is handing this spec out.
//
// It is the PROPOSER where there is one — a conversation proposing a root, a
// node proposing a sub-task — and otherwise the conversation, but only for work
// that stands on its own. A part admitted under a parent (a division) belongs to
// that parent's turn and never to whatever the conversation happens to be doing
// at the same moment.
func (g *TaskGraph) admittingAgent(spec taskSpec) *Agent {
	switch {
	case spec.owner != nil:
		return spec.owner
	case spec.parent == 0:
		return g.home
	default:
		return nil
	}
}

// turnHandedItsAskOff says the turn now ending handed the current request's work
// to a task that is still live, so the outcome it owes arrives as that task's
// news.
func (a *Agent) turnHandedItsAskOff() bool {
	// A NODE'S OWN TURN IS NEVER GATED: the work is what it was given, and a
	// worker that hands a piece out still owes its own deliverable and report.
	if a.config.InTask {
		return false
	}
	now := requestEpochAt(a)
	if !now.live() {
		return false
	}
	return a.tasker().liveWorkFromEpoch(a, now)
}

// liveWorkFromEpoch says whether one request's admissions are still live.
//
// It is nil-safe for the reason every other reader of this graph is: a session
// that never groomed a task has no graph, and "nothing was handed out" is the
// honest answer rather than a graph built to answer one question.
func (g *TaskGraph) liveWorkFromEpoch(admitter *Agent, now requestEpoch) bool {
	if g == nil || admitter == nil || !now.live() {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, node := range g.nodes {
		if node == nil || node.admitBy != admitter || node.admitAt != now {
			continue
		}
		// A settled node is news to answer rather than work to wait for: it has
		// already reported, and the turn holding that report is read like any
		// other. See the header for what queued and running do and do not mean.
		if node.state == TaskQueued || node.state == TaskRunning {
			return true
		}
	}
	return false
}
