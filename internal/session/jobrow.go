package session

// A BACKGROUND JOB IS A ROW ON THE ROSTER WHILE IT LIVES.
//
// THE LAW THIS FILE EXISTS FOR: work this conversation started shows on the
// right, whatever door started it. Until this file, one door broke it. A person
// asked for a multi-part sweep, the model started it with `bash background:true`
// and narrated it as a research run, and the column beside the conversation
// stayed completely empty for the whole nine minutes — because a job lives in
// the registry (jobs.go) and the registry had no way to say so to anybody but
// the `jobs` tool. From the person's side a session that was working and a
// session that had quietly given up looked identical.
//
// SO A JOB PUBLISHES A ROW, THROUGH THE ONE ROSTER DOOR EVERYTHING ELSE USES.
// There is no second row-space and no second lane: a live job's row is a
// [TaskNotice] of [TaskKindJob] on [Agent.emitTaskUpdate], which is what a task
// node and an adaptive run's family both publish. A surface that draws the
// roster draws jobs the day it is rebuilt against this package and needs to know
// nothing new to do it.
//
// IT REGISTERS AND IT DOES NOT ADMIT, which is orchestrate.go's phrase for
// exactly this shape. Nothing here calls [TaskGraph.admit] — admission is what
// STARTS work, and a job is already running in a registry of its own. What is
// reused is the RECORD-ROW SEAM an adaptive run's family opened
// ([TaskGraph.keepRunRows], named for its first user and now the one row-space
// for every row that is not a node): the graph's id sequence, so no job's row
// can collide with a task's or a run's; the replay in
// [Agent.replayTaskRoster], so a surface that attaches while the job is still
// going is handed the row rather than an empty column; and the checkpoint, so a
// conversation reopened tomorrow redraws what it ran rather than an empty column
// beside a transcript full of jobs.
//
// A JOB IS A FAMILY OF ONE. A run hands over a whole family per publish because
// its rows move together; a job has no workers, no parts and no children, so the
// slice it hands over is the one row it is.
//
// WHAT IS NOT REUSED IS A NODE'S AFTERLIFE. No project index row, no landing
// note, no card in the conversation, no room. A job already tells the model it
// ended, once, on the steering lane ([jobRegistry.settleExit]), and a second
// account of the same ending in a different vocabulary would be the harness
// narrating itself. The row is a row.
//
// A TASK NODE IS NOT GIVEN ONE. Task nodes are in the job registry too — one id
// space, one log, one kill (jobs.go's [jobKindTask]) — and they already have the
// roster row this file is about, published by the graph. A second row would be
// the same work counted twice, on the column and in the head count both.
//
// EXECUTION DOES NOT SURVIVE THE PROCESS AND THE ROW SAYS SO. Jobs are children
// of this process and die with it, so a row that came back saying "running"
// would claim a present that ended — and the seam settles exactly such a row on
// the way in, in the run rows' own words with the one noun changed: a job's log
// is kept where a run's journal is (task_store.go's [jobEndedReport]). Nothing
// is restarted, nothing re-enters a frontier, and a restored row counts as
// nobody working.

import (
	"fmt"
	"strconv"
)

// jobRowLead is what a job row's under-line says: which job the row is, and
// where the whole of its output is.
//
// BOTH HALVES ARE HANDLES AND NEITHER IS DECORATION. The id is what the model is
// addressed by — `jobs output 3`, `jobs kill 3` — so a person who wants to see
// more than the row has only to say the number out loud. The path is the log
// itself, which for a background job is the entire record of what happened: it
// has no transcript, no branch and no report, and when the row settles this is
// the only thing left to go and look at.
func jobRowLead(id int, logPath string) string {
	if logPath == "" {
		return "job " + strconv.Itoa(id)
	}
	return fmt.Sprintf("job %d%s%s", id, jobRowLogSep, logPath)
}

// jobRowLogSep joins the handle to the path in the sentence above. It is a
// constant because the sentence is now a STORAGE FORMAT as much as a row —
// [jobNoticeFromRow] reads it back out of a checkpoint written by an older
// aforge — and a separator spelled in two places is a separator that drifts
// until one of them stops being able to read the other.
const jobRowLogSep = " · log "

// jobRowTitle is the short name a job's row is drawn under.
//
// IT IS THE REGISTRY'S OWN NAME WHEREVER THERE IS ONE. A watch, a video render
// and a task node are each given a label when they start (jobs.go), and those
// labels are what `jobs list` already puts in front of the model — so a row that
// invented a second name for the same work would leave a person unable to match
// what they can see against what they can ask about. Only a plain background
// command has no label, and then the command's own first line is the truest
// short name there is for it: "npm run dev" is what a person would call that
// job, because it is what they would have typed.
func jobRowTitle(info jobInfo) string {
	if info.kind == jobKindWatch && info.label != "" {
		// A watch leads with its KIND, exactly as its `jobs list` row does: "app"
		// alone says nothing about why this row is sitting there for an hour.
		return clip("watch "+info.label, titleLimit)
	}
	if info.label != "" {
		return clip(firstLine(info.label), titleLimit)
	}
	return clip(firstLine(info.command), titleLimit)
}

// jobRowState maps a job's life onto the states a roster row is drawn from.
//
// THE THREE ENDINGS ARE THE THREE A PERSON MEANS. A command that exited zero is
// DONE. A command that exited anything else did not come off, which is the one
// thing [TaskFailed] says on a row with no branch behind it. And a job somebody
// ENDED — `jobs kill`, or the shutdown at Close — settles as failed too, with
// [TaskNotice.Stopped] beside it, because "it failed" and "you stopped it" are
// different news about the same state and the row says which (task_contract.go).
//
// There is deliberately no queued state and no unverified one: a job is running
// from the instant it forks, and nobody audits a log file.
func jobRowState(info jobInfo) (TaskState, bool) {
	switch info.state {
	case jobKilled:
		return TaskFailed, true
	case jobExited:
		if info.code == 0 {
			return TaskDone, false
		}
		return TaskFailed, false
	default:
		return TaskRunning, false
	}
}

// announceJobRow publishes one job's row — the first time as the work starting,
// and again each time its life moves.
//
// THE ROW ID IS THE GRAPH'S AND IT IS MINTED ONCE. A job's own id restarts at
// one in every window and shares nothing with the task graph's counter, so a
// roster keyed on it would draw job 3 and task 3 as one row. Reserving from the
// graph is what orchestrate.go's family seam does for the same reason, and it is
// the only thing this file takes from the graph.
//
// A NODE'S OWN JOBS ARE NOT ANNOUNCED. A task node has a registry of its own —
// its `make` can be promoted exactly as the conversation's can (promote.go) —
// and no roster to publish onto: its lane is its room, and the row a person sees
// for that work is the node's. Publishing here would be rows on a column nobody
// is reading, minted from a graph that is not the conversation's.
func (a *Agent) announceJobRow(info jobInfo) {
	if a.config.InTask {
		return
	}
	row, minted := a.jobRowID(info.id)
	state, stopped := jobRowState(info)
	if !minted && state == TaskRunning {
		// The row already stands and nothing has moved: a start is announced once.
		return
	}
	notice := TaskNotice{
		ID:      row,
		Title:   jobRowTitle(info),
		Kind:    TaskKindJob,
		State:   state,
		Elapsed: info.elapsed,
		Stopped: stopped,
		// THE LOG IS THE REPORT, and it is carried from the row's first breath
		// rather than only at the end. A background job writes no report — there is
		// nobody in it to write one — so the honest answer to "what did this do" is
		// where its output is, and a person watching it run wants that as much as a
		// person reading it afterwards.
		Report: jobRowLead(info.id, info.logPath),
	}
	// THE ORDER IS THE FAMILY SEAM'S ORDER, and for its reasons
	// ([orchestrateFamily.publish]): the news goes to whoever is watching before
	// it goes to a file. No lock of the registry's is held across either call —
	// [jobRegistry.add] has released its own before it announces — so nothing in
	// the graph or the store can ever reach back into this registry, which is the
	// half of the law that keeps the two lock orders from meeting.
	//
	// NOTHING SERIALIZES THE TWO ANNOUNCEMENTS BECAUSE PROGRAM ORDER ALREADY
	// DOES. Every start registers the job and announces it BEFORE it launches the
	// goroutine that can settle it (see [jobRegistry.start] and
	// [jobRegistry.adopt]), so a "done" can never overtake the "running" that has
	// to precede it, and two different jobs are two different families with
	// nothing between them to order.
	// THE ROW IS KEPT AND IT IS NOT PUBLISHED. Those were one act while a job
	// reached a surface as a task row, and they are two things: keeping is what
	// makes a conversation reopened tomorrow able to draw what it ran, and
	// publishing is what a surface draws NOW. A job is published as a job
	// (jobnotice.go), so the roster's lane is left to the work that belongs on
	// it — anything else would be one piece of work counted twice, once in a
	// section and once among the task families.
	//
	// WHAT THE STORE KEEPS IS STILL A TASK ROW because the store is a FILE, and
	// files already written are read by the aforge that opens them next. The
	// checkpoint's own dialect is projected back at the edge on the way out
	// ([jobNoticeFromRow], replayed by [Agent.replayTaskRoster]).
	a.graph().keepRunRows(row, []TaskNotice{notice})
	a.emitJobUpdate(noticeOf(info))
	if minted {
		// THE JOB NEVER WAITS TO BE NAMED. The process is already running —
		// start announced after the fork — and this is an errand on its own
		// goroutine, reached through the same announce seam the registry
		// already uses to find the agent (agent.go's jobs.announce).
		a.nameJob(info)
	}
}

// emitJobUpdate fans one job's notice out to whoever is listening: the turn's
// hub, if a turn is in flight, and every standing watcher. It is
// [Agent.emitTaskUpdate] for a job, on the same two lanes, because a job
// outlives the turn that started it for the same reason a node does and the
// subscribers who already hold that lane are who will draw it.
func (a *Agent) emitJobUpdate(notice JobNotice) {
	event := Event{Kind: EventJobUpdate, Job: &notice}
	a.mu.Lock()
	hub := a.hub
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()

	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// jobRowID is the roster id for one job, minted from the task graph's sequence
// on first sight and remembered for the job's whole life. It reports whether
// this call is the one that minted it.
func (a *Agent) jobRowID(job int) (uint64, bool) {
	a.mu.Lock()
	if id, held := a.jobRows[job]; held {
		a.mu.Unlock()
		return id, false
	}
	a.mu.Unlock()
	// The reserve is taken OUTSIDE the agent's lock: it takes the graph's, and
	// this package keeps one lock order (task_run.go's [TaskNode.notice] states
	// it). Two announcements racing on one job would mint two ids, which is why
	// the map is checked again under the lock below and the loser's id is
	// dropped — an id nobody used costs a number in a sequence and nothing else.
	minted := a.graph().reserve()
	a.mu.Lock()
	defer a.mu.Unlock()
	if id, held := a.jobRows[job]; held {
		return id, false
	}
	if a.jobRows == nil {
		a.jobRows = map[int]uint64{}
	}
	a.jobRows[job] = minted
	return minted, true
}

// jobsWorkingNow is the background half of the live work tree: one row per job
// that is running right now.
//
// A JOB COUNTS AS WORKING, which is the head count half of this file's law. The
// number over the column is what a person reads to know whether anything is
// happening at all ([CountWorking]), and a session with a nine-minute sweep in
// flight that answered "nothing" was the same lie the empty column was telling.
//
// SETTLED JOBS ARE NOT IN THE TREE AT ALL, and that is the difference between
// this and the task graph's half. A task's finished workers are kept because
// their family is still on screen and a division with two of three parts home is
// a picture that needs all three rows ([Agent.WorkingNow] states it). A job has
// no family: it is one row, it either is running or it is not, and a settled one
// in a tree named "working now" would be exactly the claim the emptiness law
// forbids. The roster keeps the history; this counts the present.
//
// THE ID IS ADDRESSED THE WAY THE TREE'S OTHERS ARE, `job:3` — the registry's
// own number, which is what `jobs kill` takes. It is not a [Agent.Cancel] id:
// this session has no stop verb for a job, and a surface must not read one into
// the spelling (cancel.go owns that vocabulary and does not use this word).
func (a *Agent) jobsWorkingNow() []WorkNode {
	if a.jobs == nil {
		return nil
	}
	var out []WorkNode
	for _, one := range a.jobs.all() {
		info := one.info()
		// A task node is the graph's row and the graph's count. See the header.
		if info.kind == jobKindTask || info.state != jobRunning {
			continue
		}
		out = append(out, WorkNode{
			ID:    "job:" + strconv.Itoa(info.id),
			Title: jobRowTitle(info),
			State: WorkRunning,
			Born:  one.started,
		})
	}
	return out
}
