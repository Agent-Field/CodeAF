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
	"strconv"
	"time"
)

const (
	// jobParkBoundReason is what a park hands the model when its OWN bound trips:
	// the same family the settle turn's bound speaks in ([taskAskSettleReason]),
	// said about the wait rather than about the landing. The wait did not settle
	// within the stretch it was given, so the turn comes back with that said out
	// loud — never as a silent stop.
	jobParkBoundReason = "the park was not settled within its bound"

	// jobParkBoundRule is the one clause a park's handback carries about ITSELF.
	// A bound that ended the wait is not a bound that ended the command, and a
	// model told only that its command has not finished would reach for the log a
	// second time — the exact polling the park exists to remove.
	jobParkBoundRule = "— the wait ended, not the command: it is still running and will report its own ending. Do not poll for it and do not kill it."

	// jobParkBoundShare divides the run's allowance into the stretch ONE wait for a
	// promoted command may take before the turn is handed back to the model with
	// the record above.
	//
	// ── WHY A SHARE, AND WHY THIS ONE ──
	//
	// A share is what scales: whatever wall the node was given, the park takes a
	// third of it and no more, so the handback is always strictly inside the
	// allowance and never the allowance itself — which is the wedge this answers,
	// where the only stop was the whole wall. Two thirds of the run are still ahead
	// of the model when it hears that the wait ran long, which is what "well
	// before" has to mean for it to be able to act on.
	//
	// AND A THIRD IS SAFE FOR A COMMAND THAT IS HONESTLY WORKING, because this bound
	// ends the WAIT and never the WORK. The promoted command is left exactly as it
	// was — the registry is not touched, its process is not signalled, its log goes
	// on filling — so a job that is still producing output keeps producing it, and
	// its own ending still lands as a note in front of the model. A job that never
	// ends no longer holds the turn for the whole allowance; a job that is still
	// working is never cut.
	jobParkBoundShare = 3
)

// armJobPark makes this worker one that waits for the commands it started. The
// bound handed here is the whole allowance the run was given;
// [Agent.parkOnOwedJob] takes the smaller share of it it waits under and says
// why.
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

// owedJob is the command this park is waiting for, or nil when nothing is owed
// any more. It is read only when the park's own bound trips, to name the job in
// the record the model is handed; it is the same predicate [jobRegistry.owedRunning]
// answers with, asked for the job itself rather than for a yes or no.
func owedJob(jobs *jobRegistry) *job {
	if jobs == nil {
		return nil
	}
	for _, candidate := range jobs.all() {
		if candidate.stillOwed() {
			return candidate
		}
	}
	return nil
}

// parkBoundNote is the record a park hands back when its own bound trips. It
// rides the ending lane ([jobNote]) because that is the one lane a parked worker
// is reading, and the parked turn is the one turn that is waiting for it. The
// sentence names the job so the model knows which command is still running, and
// the clause beside it says the command was not cut.
func parkBoundNote(one *job) userMessage {
	text := jobParkBoundReason + ": job " + strconv.Itoa(one.id) + " is still running\n" + jobParkBoundRule
	return jobNote(text)
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
// A SINGLE WAIT HAS A BOUND OF ITS OWN, AND IT IS A SHARE OF THE ALLOWANCE
// RATHER THAN THE ALLOWANCE. The runner hands this park the run's whole
// allowance, and the park waits under [jobParkBoundShare] of it: a wait that is
// never settled within that stretch is handed back to the model with a record
// ([parkBoundNote]) instead of sitting silent until the whole wall is spent. It
// is one timer for the whole park rather than one per piece of news, so unrelated
// news cannot renew it. The runner independently watches its current deadline in
// childRun.drain, and renewal and cancellation remain the runner's decisions;
// this local timer is the backstop that keeps a never-ending command from being
// the stop itself.
func (a *Agent) parkOnOwedJob(ctx context.Context) {
	a.mu.Lock()
	allowance, jobs := a.jobParkBound, a.jobs
	a.mu.Unlock()
	// THE CHEAP QUESTION IS ASKED BEFORE ANYTHING IS BUILT. This runs at every
	// step boundary of every worker, and the overwhelmingly common answer is that
	// nothing is owed — so a timer allocated here would be a timer stopped
	// microseconds later, once per step, forever. It is NOT the duplicate of the
	// read inside the loop and must not be deleted as one: that read is taken
	// after the generation and is what carries the correctness, while this one is
	// only the early out.
	if allowance <= 0 || jobs == nil || !jobs.owedRunning() {
		return
	}
	// THE PARK'S OWN BOUND, taken from the allowance rather than being it. A run
	// given a sliver of wall still gets a bound shorter than the sliver; a bound
	// that rounds to nothing falls back to the allowance so a wait is never made
	// instantaneous by an arithmetic accident.
	bound := allowance / jobParkBoundShare
	if bound <= 0 {
		bound = allowance
	}
	// IT IS THE RUN'S CLOCK ([Agent.taskClockTimer]), which is what lets a test
	// drive the bound without a sleep standing in for causality.
	timer, stop := a.taskClockTimer(bound)
	defer stop()
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
		case <-timer:
			// THE BOUND ENDS THE WAIT AND NOT THE COMMAND. The ending has not come,
			// so the turn goes back to the model with the record of the wait rather
			// than sitting silent to the allowance. The command is left running: the
			// registry is not touched and its own ending will still arrive as a note
			// ([jobRegistry.settleExit]). Only if the debt was cleared in the instant
			// the timer fired is there nothing to name — and then that ending's note
			// is already on its way, so the wait is answered all the same.
			if one := owedJob(jobs); one != nil {
				a.enqueueNote(parkBoundNote(one))
			}
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
