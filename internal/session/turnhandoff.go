package session

import "github.com/Agent-Field/aforge-v2/internal/provider"

// turnhandoff.go answers TWO questions for the end of a turn, both facts about
// the graph rather than readings of anything the model said:
//
//	 1. did this turn put the work THE CURRENT REQUEST asked for into a task that
//	    is still live? ([turnHandedItsAskOff])
//	 2. is this turn answering a request whose work has already come home with
//	    its check green? ([turnSettledItsAsk])
//
// They are the two halves of one moment. The first stops the reader polling a
// task that is about to report. The second stops it re-opening a report that
// already landed done.

// ── THE LIVE HALF ──────────────────────────────────────────────────────────

// THE MEASURED FAILURE.
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
// The live gate opens only when ALL of these hold:
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
		// already reported. A failed or unfinished landing is still read. A done
		// landing whose own check passed is the other gate ([turnSettledItsAsk]).
		if node.state == TaskQueued || node.state == TaskRunning {
			return true
		}
	}
	return false
}

// ── and work that is still out from an EARLIER turn ─────────────────────────
//
// THE LAW: A TURN THAT ENDED WITH WORK OF ITS OWN STILL RUNNING IS WAITING, NOT
// STOPPED SHORT.
//
// [Agent.turnHandedItsAskOff] above is qualified to the turn that made the
// handoff, and it has to be: a person who types a new ask while something runs
// is owed a reading of THAT ask. But a turn nobody typed is a different animal.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// 2026-09-10, 16:19, deepseek-v4.1-flash. A chat started two quick tasks in one
// batch and ended its turn saying they would land on their own. The first landed
// at 19:22, its note woke a turn, and the chat answered it. That woken turn
// stopped — correctly, with nothing to do until the second task returned — and
// the carry-on road read it, was told the second task had not come back, and
// re-opened it. Three times, at 19:51, 20:05 and 20:11, each one a reader call
// and a `tasks` poll of the very node that was about to report, each one
// answered "still running, no gap to fix", until [checkpointCarryOnCap] stopped
// it. The same fired in a second drive over a task that was landing in the same
// second.
//
// So the gate is the one above with the epoch taken out and the PERSON put in
// its place: work this conversation started, still queued or running, and
// nothing of the person's own in the turn that is ending.
//
// ── THE PERSON VETOES AND NOTHING ELSE DOES ─────────────────────────────────
//
// It is [Agent.deliveringOwnedResult]'s rule, for its reason. A landing, a job
// exiting, a watch firing are the session talking to itself; the ask they are
// answering is the one the work was started under, and its ending is already on
// its way. A sentence the PERSON typed or steered in is a new request, is owed a
// reading of its own, and takes this gate away — which is what keeps the three
// cases below (an old task, a handoff before their next sentence) answering
// exactly as they did.

// turnIsWaitingOnItsOwnTasks reports that this conversation has a task of its
// own — quick or ordinary, from this turn or any earlier one — that is still
// queued or running, in a turn the person has said nothing in. It names the
// first such node so the journal can say which ending the turn is waiting for.
func (a *Agent) turnIsWaitingOnItsOwnTasks() (uint64, bool) {
	// A NODE'S OWN TURN IS NEVER GATED, for [Agent.turnHandedItsAskOff]'s
	// reason: a worker owes its own deliverable whatever its pieces are doing.
	// The carry-on road does not run inside one anyway ([Agent.checkpoints]);
	// saying so keeps the reading true on its own.
	if a.config.InTask {
		return 0, false
	}
	a.mu.Lock()
	for _, owed := range a.owedAsks {
		if owed.from == owedByPerson {
			a.mu.Unlock()
			return 0, false
		}
	}
	a.mu.Unlock()
	return a.tasker().liveWorkOf(a)
}

// liveWorkOf names one node this agent's work is out with, if any is.
//
// OWN IS THE ADMITTER OR THE RUNNER, which is [TaskGraph.resultsThisAgentOwns]'s
// reading of the same fact and is stated there: admitBy is whose request handed
// the work out, owner is who runs it, and for a root this conversation proposed
// they are the same agent. A node rehydrated from a checkpoint has neither and
// is claimed by nobody.
//
// THE WALK IS IN ADMISSION ORDER so the id a journal line carries is the oldest
// piece still out rather than whichever one a map handed back first.
func (g *TaskGraph) liveWorkOf(owner *Agent) (uint64, bool) {
	if g == nil || owner == nil {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || (node.admitBy != owner && node.owner != owner) {
			continue
		}
		// Queued or running is what the graph knows and is not a liveness
		// proof; the header above says what bounds both.
		if node.state == TaskQueued || node.state == TaskRunning {
			return node.id, true
		}
	}
	return 0, false
}

// ── THE SETTLED HALF ────────────────────────────────────────────────────────

// turnSettledItsAsk says this turn is answering a request whose work has already
// come home with its check green, so the end-of-turn reader must not re-open it
// as unfinished.
//
// THE MEASURED FAILURE. A task landed done, its own checks passed, the card
// showed the settled mark, and the end-of-turn reader — looking at a digest of
// the transcript, not the tree — said the ask was unfinished. The turn was
// carried on three times and then told the person the work was not finished
// (#468). The live-task gate ([turnHandedItsAskOff]) covers the other half of
// the same moment: work still out. This is the landing's half.
//
// THE GATE OPENS ONLY WHEN ALL OF THESE HOLD, and each is a runtime fact:
//
//  1. this agent is a conversation, not a task worker;
//  2. this turn arrived with at least one result ([Agent.turnResults]);
//  3. every ask it owes came from those results — not the person's own later
//     words, which would be a new request on top of the landing
//     ([wakecause.go]);
//  4. every arrived node is done, and its own check passed.
//
// A PIECE OF A LARGER ASK IS NOT THIS. A landing whose tag carries no request
// leaves the turn owing the person's broader sentence, and that turn is still
// read — TestAWokenTurnThatStopsShortIsReopenedEvenWhenCheap is that shape. A
// failed landing is news to carry on from, and a done landing nobody checked is
// still read: the green mark on the card is the check that ran, and without it
// the reader is the second opinion the work has.
func (a *Agent) turnSettledItsAsk() bool {
	if a.config.InTask {
		return false
	}
	a.mu.Lock()
	arrived := append([]uint64(nil), a.turnResults...)
	owed := append([]owedAsk(nil), a.owedAsks...)
	a.mu.Unlock()
	if len(arrived) == 0 || len(owed) == 0 {
		return false
	}
	for _, ask := range owed {
		if ask.from != owedByResult || ask.task == 0 {
			return false
		}
	}
	graph := a.tasker()
	if graph == nil {
		return false
	}
	settled := make(map[uint64]bool, len(arrived))
	for _, id := range arrived {
		node := graph.node(id)
		if node == nil || node.stateNow() != TaskDone {
			return false
		}
		if node.checkAnswer() != provider.VerdictVerifiedSuccess {
			return false
		}
		settled[id] = true
	}
	for _, ask := range owed {
		if !settled[ask.task] {
			return false
		}
	}
	return true
}
