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
	_, failed := jobs.kill(number)
	if failed {
		// The job ended between the running check and the kill: same news as
		// a second press on work that has already landed.
		return name + " has already finished; there is nothing to stop", nil
	}
	// A KILL THIS SESSION ASKED FOR DOES NOT REPORT ITS OWN DEATH — the
	// registry's flag is set, the reaper stays quiet, and that rule still
	// holds, because a note from the watcher would be the agent telling
	// itself what it just did (jobs.go's killRequested). A PERSON stopping a
	// job is a different caller: the model did not ask, and without a note it
	// would keep reasoning about work that is no longer running. The owed
	// lane is how every other job ending reaches it.
	a.enqueueJobNote(fmt.Sprintf("job %d was stopped", number))
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
