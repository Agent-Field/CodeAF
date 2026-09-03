package session

// A PERSON STOPS A JOB THROUGH THE SAME DOOR THEY STOP ANYTHING ELSE.
//
// THE LAW THIS FILE EXISTS FOR: a background job has always had a way to end —
// `jobs kill`, which is the model's tool and takes the registry's own numbers —
// and until this file that number was unreachable from [Agent.Cancel]. The
// reason the surface drew no ✕ on a job row was exact: "the id on a job's row
// is the roster's own and names nothing session.Agent.Cancel can find". Jobs
// now publish a [JobNotice] carrying their OWN registry id, so `job:3` is a
// Cancel id the same way `task:7` is.
//
// THIS IS NOT A SECOND KILL. The registry already ends a job
// ([jobRegistry.kill]); this file is the grammar that lets a person reach it.
// A stop the model asked for and a stop the person asked for travel the same
// path through the process, and they differ only in who is told afterwards.
//
// THE INTERRUPT RULE IS UNTOUCHED. An interrupt kills fork hands and
// deliberately leaves background commands running (agent.go's [Agent.Interrupt],
// jobs.go's [jobRegistry.stopHands]). Shutdown does not call explicitStop,
// because process exit pauses task work for resume. A person-initiated stop
// is neither of those: it is Cancel, and only Cancel.

import (
	"context"
	"fmt"
	"strings"
)

// cancelJob stops one background job on a person's word, and answers with the
// line to show.
//
// THE AGENT'S LOCK IS NOT HELD ACROSS THE KILL. [jobRegistry.kill] waits on
// the job's done, and the reaper that closes done then publishes through
// [jobRegistry.announceRow], which takes this agent's lock and the graph's
// (jobrow.go: no lock of the registry's is held across an announce, and the
// graph's lock is never taken while holding a.mu). Holding a.mu here would be
// the lock Interrupt has to be able to take, held across a wait of seconds —
// and [Agent.enqueueJobNote] takes the same lock, so a hold across this
// function would deadlock the note this stop owes the model. Copy the
// registry, release, then kill.
func (a *Agent) cancelJob(id uint64) (string, error) {
	a.mu.Lock()
	jobs := a.jobs
	a.mu.Unlock()
	if jobs == nil {
		return "", fmt.Errorf("there is no job %d in this session", id)
	}
	number := int(id)
	target := jobs.find(number)
	if target == nil {
		return "", fmt.Errorf("there is no job %d in this session", id)
	}
	name := jobStopName(number, target)
	if !target.running() {
		return name + " has already finished; there is nothing to stop", nil
	}
	// A PERSON'S STOP IS NOT ON A TURN'S CLOCK, so this kill is given a plain
	// background context rather than a cancellable one. [jobRegistry.kill]
	// takes a context because the bounded stop needs its SIGTERM and SIGKILL
	// graces to end the moment a turn is abandoned (issue #265); those graces
	// are two seconds each and end on their own, so passing a context that is
	// never cancelled leaves this path behaving exactly as it did. What it must
	// not do is inherit a turn's context: a person who asks for a job to stop
	// is owed the kill even if the turn they asked from is already over.
	//
	// A PERSON'S STOP OWNS THE ENDING IT ASKED FOR, and the mark goes on BEFORE
	// the kill. [job.requestKill] is what makes the death requested, so a settle
	// that sees the requested death also sees this — and the reaper, which
	// releases every other requested death the instant it lands, steps over this
	// one ([jobRegistry.settleExit]). Without the mark the release happened from
	// inside the kill below, while the line this stop owes the model was still
	// unwritten, and a worker parked on the command woke to an empty queue.
	target.markPersonStopped()
	_, failed := jobs.kill(context.Background(), number)
	if failed {
		// The job ended between the running check and the kill: same news as
		// a second press on work that has already landed.
		//
		// AND THIS STOP PAYS ONLY FOR AN ENDING NOBODY ELSE WILL SPEAK FOR. The
		// job settled on its own account in that gap, and which account it was
		// decides who owes the parked worker its release. An ordinary EXIT is the
		// reaper's road: it has a note and the release rides with it, so paying
		// here would be a second, earlier release with nothing behind it. A death
		// somebody ASKED for is the other half of the mark above — the reaper was
		// just told to step over it, so nobody is coming, and the debt is settled
		// here or not at all. It is read off the job rather than inferred from the
		// failed kill, which fails for both reasons and cannot tell them apart.
		if target.settledKilled() && target.payOwed() {
			a.releaseParkedOnJob()
		}
		return name + " has already finished; there is nothing to stop", nil
	}
	// AND THE DEBT IS CLEARED BEFORE THE NOTE, for [jobRegistry.settleExit]'s
	// reason exactly: the release rides with the append ([userMessage.ending]),
	// and a note that released while the debt still stood would wake the park,
	// which would read itself owed and park again on a generation nothing will
	// ever close.
	target.payOwed()
	// A KILL THIS SESSION ASKED FOR DOES NOT REPORT ITS OWN DEATH — the
	// registry's flag is set, the reaper stays quiet, and that rule still
	// holds, because a note from the watcher would be the agent telling
	// itself what it just did (jobs.go's killRequested). A PERSON stopping a
	// job is a different caller: the model did not ask, and without a note it
	// would keep reasoning about work that is no longer running. The owed
	// lane is how every other job ending reaches it.
	//
	// It travels as a headline like every other job note: the sentence IS the
	// whole of the news, and there is no ending to read out of the log that the
	// person did not just ask for the end of.
	a.enqueueJobNote(fmt.Sprintf("job %d was stopped", number), false)
	return "stopped " + name + " — its log is kept", nil
}

// jobStopName is how a stop line names one job: what it is called where it has
// a name, and its id where it does not — the floor [JobNotice.Label] keeps on
// the surface, kept here for the same reason. A sentence about "job 3" is one
// a person can say out loud.
func jobStopName(id int, one *job) string {
	if one == nil {
		return fmt.Sprintf("job %d", id)
	}
	label := strings.TrimSpace(noticeOf(one.info()).Label())
	if label == "" {
		return fmt.Sprintf("job %d", id)
	}
	return fmt.Sprintf("job %d (%s)", id, clip(label, 60))
}
