package session

// A BACKGROUND JOB PUBLISHES ITSELF, IN ITS OWN WORDS.
//
// THE LAW THIS FILE EXISTS FOR: a job is described by the facts a job actually
// has. It has an id, a command, a log file, a clock and — when it is over — an
// exit code. It does not have a brief, an acceptance, a ground, a branch, a
// worktree, a report anybody wrote, a price, or an agent inside it to be steered.
//
// WHAT WAS HERE BEFORE, AND WHY IT WENT. Jobs reached a surface as [TaskNotice]
// of kind `job`, because when jobs first needed to be visible at all the roster
// was the only door there was (jobrow.go's header still tells that story). The
// borrowing worked, and it cost three things that were paid every day
// afterwards:
//
//   - THE FACTS WERE PACKED INTO A SENTENCE. A job's id and log path travelled
//     as `job 3 · log /…/3.log` in the one string field a task row has for prose,
//     and two separate files in the surface cut it back apart on ` · log ` to get
//     at either half. A separator spelled in three places is a separator that
//     drifts, and a fact that has to be parsed out of a sentence is a fact the
//     sender knew and threw away.
//   - A JOB NEEDED A SECOND ID. Roster rows are keyed on the task graph's
//     sequence, so every job had one number minted from the graph to be drawn
//     under and kept its own registry number to be ADDRESSED by — `jobs kill 3`
//     takes the second and no surface could show it without the first. Two ids
//     for one thing, and a map between them to be kept in step.
//   - EVERY SURFACE DOWNSTREAM CARRIED A CLAUSE. `TaskKindJob` had to be excluded
//     from the project index, from the landing card, from the stop key, from the
//     roster's under-line and from the room's record rows — five files whose
//     entire content on the subject was that a job is not really one of these.
//
// SO A JOB IS PUBLISHED AS A JOB. The notice below is [jobInfo] — the snapshot
// the registry already copies out from under its own lock — made public, plus
// the name. There is NO NEW STATE ANYWHERE: [jobRegistry] remains the single
// source of truth about what is running, and this is a projection of it taken at
// the moment something moved.

import "time"

// JobKind is what sort of background work a job is, in the one vocabulary a
// surface reads. It is the registry's own [jobKind] made public.
//
// IT EXISTS SO A SURFACE NEED NOT READ THE NAME TO KNOW THE KIND. A watch and a
// render behave differently on a page — a watch has ticks and no ending, a
// render has an artifact at the end of it — and a surface that had to infer that
// from a label reading "watch app" would be parsing prose for a fact the engine
// already holds.
type JobKind string

const (
	// JobKindCommand is a shell command running in the background: a server, a
	// build, a sweep. It is the ordinary case and the only kind whose name is
	// worth asking a model for, because it is the only one with no label of its
	// own (jobname.go).
	JobKindCommand JobKind = "command"
	// JobKindWatch is a command re-run on a timer, which ends when it is stopped
	// and not when it is finished.
	JobKindWatch JobKind = "watch"
	// JobKindRender is a provider call that makes a file — a video, a piece of
	// music — and is a job for as long as it renders.
	JobKindRender JobKind = "render"
	// JobKindHand is one hand of a fork: a side errand this conversation sent off
	// and will hear back from on its own lane.
	JobKindHand JobKind = "hand"
	// JobKindTask is a task node's own worker. It is in the registry for the id
	// space, the log and the kill, and it is NEVER published as a job: the work
	// already has a roster row of its own, and a second row would be the same
	// piece of work counted twice.
	JobKindTask JobKind = "task"
)

// JobState is where a job is in its one and only life.
//
// THERE ARE THREE AND THERE IS NO FOURTH. A job is running from the instant it
// forks — there is no queue in front of it and nothing admits it — and it ends
// either by finishing or by being stopped. Nothing audits a log file, so there
// is no unverified state here of the sort a task node has.
type JobState string

const (
	// JobRunning is a live process, or a watch whose timer is still going.
	JobRunning JobState = "running"
	// JobDone is a command that exited zero.
	JobDone JobState = "done"
	// JobFailed is a command that exited anything else.
	JobFailed JobState = "failed"
	// JobStopped is a job somebody ENDED — `jobs kill`, the stop verb on its own
	// page, or the shutdown when the conversation closes.
	//
	// IT IS ITS OWN STATE RATHER THAN A FLAG ON FAILURE. "It failed" and "you
	// stopped it" are different news about a process that is equally not running,
	// and a surface that had to read a boolean beside the state to tell them apart
	// would be reading two fields to answer one question.
	JobStopped JobState = "stopped"
)

// Over reports whether this state is an ending. It is one function because the
// question is asked on every surface that draws a job — the section's counts,
// the page's foot, the log reader's last reading — and a rule spelled four times
// is a rule that drifts.
func (s JobState) Over() bool { return s != JobRunning }

// JobNotice is one background job, as everything outside this package sees it.
//
// EVERY FIELD IS A FACT THE REGISTRY ALREADY HELD. Nothing here is derived,
// formatted or joined: a surface that wants `job 3 · 4m12s` builds that sentence
// itself, out of ID and Elapsed, in its own words and at its own width. That is
// the difference between this and what it replaces — the packed report was the
// engine choosing a surface's wording, and it fitted no width in particular.
type JobNotice struct {
	// ID is the job's own number, and it is the ONLY id it has. It is what
	// `jobs output 3` and `jobs kill 3` take, what [Agent.Cancel] takes as
	// `job:3`, and what a person reads on the row. One thing, one number.
	ID int
	// Name is the short name the job is CALLED — three or four words, given by
	// the cheap namer for a plain command (jobname.go) or taken from the label
	// the registry minted for a watch, a render or a hand.
	//
	// IT IS EMPTY UNTIL THERE IS ONE, and that is the emptiness law rather than a
	// gap: naming is an errand on its own goroutine and the work never waits for
	// it, so the first notice a job sends usually has no name on it at all. A
	// surface draws Command until Name arrives and then redraws. It is never the
	// id: `job 3` is the handle and is drawn beside the name, never as it.
	Name string
	// Command is what is actually running, whole and unshortened. A surface cuts
	// it to whatever room it has; this is the truth it cuts from.
	Command string
	// Detail is a watch's terms — "every 10s on change" — and is empty for
	// everything else.
	Detail string
	// Kind is what sort of background work this is.
	Kind JobKind
	// State is where the job is now.
	State JobState
	// LogPath is the file everything this job wrote is spooled to, absolute.
	//
	// IT IS THE WHOLE RECORD. A job has no transcript, no journal and no report,
	// so when it is over this file is the only thing left to go and look at — and
	// it outlives the process, so it is worth showing after the job has gone.
	// A hosted session's path belongs to the ENGINE's machine and is not openable
	// on the machine drawing it; that is the surface's fact to say, not this
	// field's to hide.
	LogPath string
	// Started is when the process forked, and Elapsed is how long it ran — still
	// counting for a live job, final for a settled one.
	//
	// BOTH ARE HERE BECAUSE A CLOCK THAT TICKS AND A DURATION THAT IS OVER ARE
	// DIFFERENT DRAWINGS. A surface with only Elapsed would have to re-ask the
	// engine four times a second to animate a running job's clock; with Started
	// it counts up on its own beat and asks nothing.
	Started time.Time
	Elapsed time.Duration
	// ExitCode is meaningful only when State is [JobDone] or [JobFailed]. A
	// stopped job never reached one and a running job has not yet.
	ExitCode int
	// Ticks counts a watch's completed runs of its command, and is zero for
	// everything else.
	Ticks int
}

// Over reports whether this job has ended.
func (n JobNotice) Over() bool { return n.State.Over() }

// Label is the job's name if it has one and its command if it does not.
//
// IT IS ONE FUNCTION BECAUSE IT IS THE SAME CHOICE EVERYWHERE. A section row, a
// page header and a sentence dropped into somebody's message all want "what is
// this job called", all have to answer it before the namer has replied, and all
// have to answer it the same way — otherwise the row a person clicked and the
// page it opened are about two differently-named things. The width is the
// caller's business; this only picks WHICH string.
func (n JobNotice) Label() string {
	if n.Name != "" {
		return n.Name
	}
	return firstLine(n.Command)
}

// noticeOf projects one registry snapshot into the notice a surface reads. It is
// the one place the two vocabularies meet.
func noticeOf(info jobInfo) JobNotice {
	return JobNotice{
		ID:       info.id,
		Name:     jobNameOf(info),
		Command:  info.command,
		Detail:   info.detail,
		Kind:     jobKindWord(info.kind),
		State:    jobStateWord(info),
		LogPath:  info.logPath,
		Started:  info.started,
		Elapsed:  info.elapsed,
		ExitCode: info.code,
		Ticks:    info.ticks,
	}
}

// jobNameOf is what a job is CALLED, and it is one function because there is one
// answer and several sources for it.
//
// THE REGISTRY'S OWN LABEL WINS WHEREVER THERE IS ONE. A watch, a render, a hand
// and a task node are each given a label the moment they start, and those labels
// are what `jobs list` already puts in front of the model — so a name invented
// separately for the same work would leave a person unable to match what they can
// see against what they can ask about. Only a plain command arrives with no label
// at all, and that is exactly the one the namer is asked about (jobname.go).
//
// A WATCH LEADS WITH ITS KIND, as its `jobs list` row does: "app" alone says
// nothing about why that line has been sitting there for an hour.
func jobNameOf(info jobInfo) string {
	if info.label != "" {
		if info.kind == jobKindWatch {
			return "watch " + info.label
		}
		return firstLine(info.label)
	}
	return info.name
}

// jobKindWord translates the registry's private kind into the published one.
func jobKindWord(kind jobKind) JobKind {
	switch kind {
	case jobKindWatch:
		return JobKindWatch
	case jobKindRender:
		return JobKindRender
	case jobKindHand:
		return JobKindHand
	case jobKindTask:
		return JobKindTask
	}
	return JobKindCommand
}

// jobStateWord translates the registry's private state into the published one,
// splitting the one case the registry keeps as a flag: a killed job settles
// under the same private state as a clean exit, and it is [JobStopped] here
// because that is different news.
func jobStateWord(info jobInfo) JobState {
	switch info.state {
	case jobKilled:
		return JobStopped
	case jobExited:
		if info.code == 0 {
			return JobDone
		}
		return JobFailed
	}
	return JobRunning
}
