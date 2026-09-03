package session

// A FOREGROUND CALL THAT BECAME A JOB IS STILL THE CALL THIS WORK IS WAITING
// FOR. A task worker does not ask for its next step while a command it started
// in the foreground is still running: it waits for that command's ending and
// reads the ending as the answer. Waiting is not a step, and a wait is never
// repetition.
//
// ── WHAT IT COST NOT TO HAVE THIS ──
//
// A worker's `bash` crossed the background-after clock, the call was adopted
// into a job (promote.go), and the tool result said `still running as job 16;
// log at …`. That is a true sentence and a terrible question: the loop asked the
// model what to do next while the eight-minute suite the whole node was waiting
// on had another seven minutes to run. There is nothing to do next, so the model
// manufactured something — `sleep 28; tail <log>` — the loop detector read the
// repetition correctly, and the node killed a healthy test run and ended saying
// the turn was going in circles.
//
// The reader was right and the question was wrong. So the question is not asked:
// the work parks until the ending is in front of it, and the polling it used to
// invent has nothing left to be for.
//
// ── IT IS A WORKER'S LAW AND NEVER A CONVERSATION'S ──
//
// A person's chat answers a long command by YIELDING — the promotion exists so
// that the keyboard stays theirs and a message can reach the model while the
// build runs (#55). A worker has nobody to yield to. It is alone in a worktree
// with one job: to run the work and report what happened, and the ending of the
// command it just started is the largest single fact it is going to learn. So
// only [runTaskChild] arms this, and an unarmed park costs one lock and returns.
//
// ── THE CLOCK IS NOT PUSHED, AND THAT IS THE DIFFERENCE FROM A PART'S PARK ──
//
// A node waiting on its parts is waiting on somebody ELSE'S work, so
// [childRun.park] pushes the deadline by exactly the parked time. A node waiting
// on a command IT started is waiting on its OWN, and the wall is a bound on how
// long this node may take: the minutes its build spends are minutes it took. So
// this park touches no counter, no clock and no lane. It books nothing BY
// CONSTRUCTION rather than by exemption — a parked node makes no tool call, so
// there is no step to count, no effect to ledger and no meter to move.
//
// AND THE GRAPH LANE IS DELIBERATELY NOT HANDED BACK. [TaskNode.park] exists so
// that a node waiting on work another lane has to do cannot hold the lane that
// would do it; a node waiting on its own command is genuinely occupying a worker
// and is waiting on nothing that needs a lane, so there is no deadlock to break
// and nothing to give up.

import (
	"context"
	"time"
)

// armJobPark makes this worker one that waits for the commands it started. The
// bound is the whole allowance the run was given, and [Agent.parkOnOwedJob] says
// why it is that number and not a smaller one of its own.
//
// It is called by [runTaskChild], which is what makes this a task worker's law
// rather than every session's — and once more, with a zero, by
// [childRun.landIfStopped]: A LANDING TURN NEVER WAITS, because a node that has
// already been stopped is being asked only to write down what it is holding.
func (a *Agent) armJobPark(bound time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.jobParkBound = bound
}

// parkOnOwedJob holds this worker at a step boundary while a command it started
// in the foreground is still running, and returns the moment that command's
// ending is on the queue in front of it.
//
// ── THE ORDER OF THE THREE READS IS THE WHOLE CORRECTNESS ──
//
// The generation comes FIRST, exactly as [childRun.foldParts] takes it: an
// ending that lands between the question and the wait closes the channel this
// select is about to wait on, so the wait returns at once instead of missing the
// news it was waiting for.
//
// THE PERSON IS THE EXCEPTION AND THE ONLY ONE. Somebody who typed into this
// node's room is owed an answer now, and holding their line for as long as a
// build runs would be the room going silent on them — the rule [childRun.park]
// already states for a node holding parts.
//
// NO SINGLE WAIT MAY OUTLAST THE WHOLE ALLOWANCE THE RUN WAS GIVEN. The bound is
// one timer for the whole park rather than one per piece of news, so unrelated
// news cannot renew it; when it expires the node is asked again and the ordinary
// deadline checkpoint collects it ([childRun.trip]). A command that has wedged
// can therefore cost this node its allowance and never more than it, which is
// what a bound is for.
func (a *Agent) parkOnOwedJob(ctx context.Context) {
	a.mu.Lock()
	bound, jobs := a.jobParkBound, a.jobs
	a.mu.Unlock()
	// THE CHEAP QUESTION IS ASKED BEFORE ANYTHING IS BUILT. This runs at every
	// step boundary of every worker, and the overwhelmingly common answer is that
	// nothing is owed — so a timer allocated here would be a timer stopped
	// microseconds later, once per step, forever. It is NOT the duplicate of the
	// read inside the loop and must not be deleted as one: that read is taken
	// after the generation and is what carries the correctness, while this one is
	// only the early out.
	if bound <= 0 || jobs == nil || !jobs.owedRunning() {
		return
	}
	timer := time.NewTimer(bound)
	defer timer.Stop()
	for {
		news := a.taskNewsWait()
		if !jobs.owedRunning() {
			return
		}
		if a.steeringHeld() {
			return
		}
		select {
		case <-news:
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		}
	}
}

// releaseParkedOnJob is the release for AN ENDING WITH NO NOTE BEHIND IT: a
// `jobs kill`, a shutdown, a stop that raced the process's own death. It is the
// release a sub-task's report already makes ([Agent.postTaskNews]) with the
// report taken out, and it is the whole of what such an ending owes — nothing is
// coming, and a worker held until its bound over news that will never be spoken
// is the wait costing exactly what it was written to save.
//
// AN ENDING THAT DOES HAVE A NOTE NEVER COMES THROUGH HERE. It releases in the
// same locked step as its own append ([userMessage.ending]), because a release
// made anywhere NEAR that append rather than with it has an interleaving in
// which the worker wakes to a queue the note has not reached yet.
//
// It counts nothing. `taskNotes` is what this agent's own sub-tasks have handed
// over and a job's ending is not one of those — it rides the steering queue like
// any other session note, and the drain on the very next line of the loop is
// what puts it in front of the model.
func (a *Agent) releaseParkedOnJob() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.releaseTaskWaitLocked()
}
