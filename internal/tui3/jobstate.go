package tui3

// ── WHAT THIS WINDOW KNOWS ABOUT ITS BACKGROUND JOBS ────────────────────────
//
// A background job — a server, a build, a sweep, a watch, a render — is not a
// task and never was. It has no agent inside it, no room, no worktree, no
// branch, no report anybody wrote and no price. What it has is a number, a
// command, a log file, a clock and, when it is over, an exit code.
//
// IT USED TO BE KEPT ON THE TASK SIDE, and that is the defect this file is half
// of. Jobs arrived as [session.TaskNotice] of a `job` kind, were filed among the
// nodes, drawn among the task families, and then excluded again — one clause at
// a time — from the project index, the landing card, the stop key, the roster's
// under-line and the room's record rows. Five exclusions is what it costs to
// keep a thing in a list it does not belong in.
//
// SO THIS WINDOW KEEPS ITS JOBS ITSELF, off [session.EventJobUpdate], and the
// two surfaces that draw them — the column's section (jobsection.go) and a
// job's own page (jobpage.go) — read the slice below and nothing else.
//
// THIS FILE HOLDS STATE AND NOTHING ELSE. It files what arrives and answers the
// counting questions the section's label asks. It draws nothing, reads no disk
// and formats no string a person sees; those belong to the two files above, and
// the pure half of the drawing (jobsview.go) is written to take this slice as an
// argument so it can be tested with no window at all.

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// jobUpdate files one job's news, and reports whether anything actually moved.
//
// THE WINDOW REDRAWS ON NEWS AND NOT ON ARRIVAL. A job publishes when it starts,
// when its name lands and when it settles, and the engine may republish a state
// this window already holds — so the caller is told whether the frame is stale
// rather than being made to guess. That is the same rule the task side's own
// upsert is written under.
//
// A TASK'S OWN WORKER IS NOT A JOB HERE. Task nodes live in the job registry for
// the id space, the log and the kill, and the engine already declines to publish
// them ([session.JobKindTask] says why: the work has a roster row of its own and
// a second one would be the same piece of work counted twice). The guard is kept
// on this side as well, because a surface that trusted a sender to filter is a
// surface that breaks the day a second sender appears.
func (a *app) jobUpdate(notice session.JobNotice) bool {
	if notice.ID == 0 || notice.Kind == session.JobKindTask {
		return false
	}
	for at, held := range a.jobs {
		if held.ID != notice.ID {
			continue
		}
		if held == notice {
			return false
		}
		a.jobs[at] = notice
		return true
	}
	// OLDEST FIRST, AND THE ORDER NEVER CHANGES AGAIN. A job's place in this
	// slice is the order it started in, so a row does not move under a person's
	// cursor when an older job finishes. What the section does with that order —
	// which end it reads from, what it folds — is the section's own business.
	a.jobs = append(a.jobs, notice)
	return true
}

// jobAt is the job with this id, or nil. It is the one lookup, so a page opened
// on a job that has since been forgotten gets an honest nothing rather than a
// stale copy somebody kept.
func (a *app) jobAt(id int) *session.JobNotice {
	for at := range a.jobs {
		if a.jobs[at].ID == id {
			return &a.jobs[at]
		}
	}
	return nil
}

// jobsRunning is how many of this conversation's jobs are alive right now, and
// jobsOver is how many have ended. The section's label is built from the pair.
//
// THEY ARE COUNTED AND NOT CACHED. The slice is tens of entries at the very most
// and the label is asked for twice a frame; a cache here would be a second copy
// of a fact that is already cheap, kept in step for no gain.
func (a *app) jobsRunning() int {
	count := 0
	for _, job := range a.jobs {
		if !job.Over() {
			count++
		}
	}
	return count
}

func (a *app) jobsOver() int { return len(a.jobs) - a.jobsRunning() }

// dropJobs forgets this conversation's background jobs, which is what /new and a
// switch to another conversation owe the column.
//
// THE ROWS GO BECAUSE THE JOBS WERE NEVER THIS WINDOW'S TO KEEP. A job belongs
// to the conversation that started it — its log lives under that session's own
// folder, its number is that session's registry's — so a section carried into
// the next conversation would be a list of work that conversation never ran,
// counted in its label and openable onto a page reading somebody else's log.
// That is the same reason the roster drops its nodes here (task.go's
// [app.dropTasks]), said about the other kind of work.
//
// THE PAGE GOES WITH THEM for the roster's own reason: a page is one job's
// record, and a job that left with its conversation is a page that can be
// neither read nor stopped.
//
// THE FOLD GOES TOO. Whether the section was open is a fact about a list that
// no longer exists, and the next conversation opens its own — collapsed, which
// is where every conversation's section starts.
func (a *app) dropJobs() {
	a.jobs = nil
	a.jobsOpen = false
	a.closeJobPage()
}

// ── THE SEAM BETWEEN THE COLUMN AND THE PAGE ────────────────────────────────
//
// The column's section (jobsection.go) is where a person presses enter, and a
// job's page (jobpage.go) is what opens. Neither reaches into the other: the
// section asks to go there and the page draws whatever this says is open, which
// is why the one fact they share lives here with the rest of the state.

// showJobPage opens one job's page, or does nothing at all for a job this window
// has never been told about.
//
// A PAGE IS NEVER OPENED ONTO NOTHING. An id with no job behind it is a row that
// was drawn from a reading this window no longer holds, and a page opened on it
// would draw a header with no name, a clock with no start and a log with no
// path — three absences the emptiness law would have to explain one at a time.
// Refusing here means the page can be written as though its job exists, because
// it does.
func (a *app) showJobPage(id int) bool {
	if a.jobAt(id) == nil {
		return false
	}
	a.jobPage = id
	return true
}

// closeJobPage comes back out to whatever was underneath, and drops the reading
// with it: a log tail and a scroll position kept past the page they belong to
// are what the NEXT page would open showing (jobpage.go's [jobDraw]).
func (a *app) closeJobPage() {
	a.jobPage = 0
	a.jobDraw = nil
}

// jobPageOpen reports whether a job's page is on the frame, and jobPageJob is
// the job it is about — nil once that job has gone, which is the reading every
// draw on that page hangs off.
func (a *app) jobPageOpen() bool { return a.jobPage != 0 }

func (a *app) jobPageJob() *session.JobNotice {
	if a.jobPage == 0 {
		return nil
	}
	return a.jobAt(a.jobPage)
}

// jobsLive is this conversation's running jobs, oldest first, and jobsSettled is
// the ones that have ended, NEWEST first.
//
// THE TWO ORDERS ARE DIFFERENT ON PURPOSE, and it is the whole of the
// don't-pollute rule stated as two functions. Running work reads in the order it
// started, because that is the order a person set it going in and a row that
// jumped would be unreadable. History reads newest first, because the useful end
// of a list of finished things is the recent end — and because the section shows
// history only in whatever room is left over, so the ones that fall off the
// bottom should be the ones nobody is looking for.
func (a *app) jobsLive() []session.JobNotice {
	out := make([]session.JobNotice, 0, len(a.jobs))
	for _, job := range a.jobs {
		if !job.Over() {
			out = append(out, job)
		}
	}
	return out
}

func (a *app) jobsSettled() []session.JobNotice {
	out := make([]session.JobNotice, 0, len(a.jobs))
	for at := len(a.jobs) - 1; at >= 0; at-- {
		if a.jobs[at].Over() {
			out = append(out, a.jobs[at])
		}
	}
	return out
}
