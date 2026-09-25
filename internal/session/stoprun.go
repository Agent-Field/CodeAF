package session

// A PERSON'S STOP REACHES A RUN.
//
// A run under the bash belt is published as rows that wear a task's number: its
// own row, and one more for every hand-off that joined it. A surface stops a row
// by the number it wears, as `task:N` ([Agent.Cancel]), and the model's own stop
// does the same. Until this file that id went to the task graph and nowhere
// else, and the graph has never heard of a run's rows: measured on the real
// binary on 2026-09-19, the stop card's "stop it" answered "there is no task 1
// in this session" four times of four, the run's own page answered with the
// store's sentence about who owns what, and the run carried on to its own
// landing. The run was started on a context nothing could cut, and no cancel
// was kept anywhere.
//
// THE ID A SURFACE ALREADY SENDS IS RESOLVED TO WHOEVER OWNS THE ROW. Nothing
// on the wire changes, so a window built before this file stops a run through
// an engine built after it. [Agent.cancelTask] asks the live run first and the
// graph second, and the page's own stop on the run's task takes the same road
// ([Agent.PlanCancel]).
//
// WHAT A STOP OF A RUN DOES, IN ORDER, AND WHY THE ORDER:
//
//	the store first   the run's task and everything open under it are ended in
//	                  one write ([plandb.Store.StopRoot]). A worker that comes
//	                  home after that finds its task already ended and writes
//	                  nothing over it, so no part of a stopped run reads as a
//	                  failure with a cut call's error for its reason. And a store
//	                  whose run is over reads as over on its page.
//	the context next  every worker and every call a worker has out was handed
//	                  this context, so the spend ends here and not at the next
//	                  pass of anybody's loop.
//	the work last     once the engine has answered that its workers are home,
//	                  what they had made is committed on the run's own branch and
//	                  the copy is given back ([keptWork], the road every stopped
//	                  task takes). NOTHING GOES INTO THE PERSON'S FOLDER: work
//	                  that was stopped half-way is work nobody checked. A
//	                  program's run has no copy: its folder is finished the way
//	                  every ending of it finishes it ([ProgramFolder.Finish]), on
//	                  its own branch, which the person's branch never becomes.
//
// IT IS IDEMPOTENT, like every other stop here (cancel.go): a second press on a
// run that is stopping says so, and a press on a run that is over says that.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// beltStoppedWhere is how a stopped run says where its work is and what a
// person can do with it, said once on the run's own page and once in the
// conversation. It is the fact the stop's own sentence promised ("its branch is
// kept"), with the two things a person who stopped work half-way wants next:
// how it comes in, and how it goes away. A run that had changed nothing says
// that instead, because a branch named over no work sends somebody looking.
func beltStoppedWhere(branch, ground string, changed []string) string {
	if strings.TrimSpace(branch) == "" || len(changed) == 0 {
		return "it had changed nothing"
	}
	return "its work so far is kept on " + branch + " and did not go into " + ground +
		" · merge that branch to bring it in, or delete it to drop it"
}

// stopBeltRow is a stop on a row a run owns. It reports whether the number is a
// run's at all: false sends the caller on to the task graph, which is where
// every other task number lives.
func (a *Agent) stopBeltRow(id uint64, why string) (string, bool, error) {
	why = strings.TrimSpace(why)
	a.beltMu.Lock()
	run := a.beltRun
	if run == nil {
		a.beltMu.Unlock()
		return a.endedBeltRow(id, why)
	}
	if run.row == id {
		name := taskStopName(id, run.title)
		if run.stopped {
			a.beltMu.Unlock()
			return name + " is already stopping", true, nil
		}
		run.stopped, run.stopReason = true, why
		cut := run.cut
		a.beltMu.Unlock()
		// THE STORE FIRST, THE CONTEXT NEXT (the header says why). A store that
		// would not take the write is said on the run's page by whoever settles
		// it; the context is cut either way, because the person asked for the
		// spend to end and that is the half nothing else can do for them.
		if err := run.store.StopRoot(stopBecause(taskStoppedWord, why)); err != nil {
			if g := a.graph(); g != nil {
				g.planNote("the run's stop could not be written: " + err.Error())
			}
		}
		if cut != nil {
			cut()
		}
		promise := "its branch is kept"
		if run.folder != nil {
			promise = run.folder.StopPromise()
		}
		return "stopping " + stopBecause(name, why) + " — " + promise, true, nil
	}
	joined := false
	for _, row := range run.joined {
		joined = joined || row == id
	}
	a.beltMu.Unlock()
	if !joined {
		return a.endedBeltRow(id, why)
	}
	return a.stopJoinedRow(run, id, why)
}

// liveBeltTaskByToken resolves the model's task spelling against the run that
// is alive now. A run's rows are deliberately not graph nodes, and the index
// rows they write are left out of this conversation's own reading of the index
// (task_run_index.go), so that index cannot be the door onto stopping one. The
// run's own kept rows carry the same ids and
// titles the rail shows, which makes a number and a title-derived name mean the
// same thing here that they mean for an ordinary task.
func (a *Agent) liveBeltTaskByToken(token string) (TaskIndexEntry, uint64, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return TaskIndexEntry{}, 0, false
	}
	a.beltMu.Lock()
	run := a.beltRun
	if run == nil {
		a.beltMu.Unlock()
		return TaskIndexEntry{}, 0, false
	}
	ids := append([]uint64{run.row}, run.joined...)
	root, rootTitle := run.row, run.title
	a.beltMu.Unlock()

	// The newest name wins, matching LookupTask. Numbers remain unambiguous
	// because every row in one conversation comes from the graph's one counter.
	for index := len(ids) - 1; index >= 0; index-- {
		id := ids[index]
		title := ""
		if id == root {
			title = rootTitle
		} else if task := run.store.Task(strconv.FormatUint(id, 10)); task != nil {
			title = task.Title
		}
		written := strconv.FormatUint(id, 10)
		if token != written && token != TaskSlug(title) {
			continue
		}
		return TaskIndexEntry{
			ID: written, Name: TaskSlug(title), Label: taskLabel(title), Title: title,
			Status: string(TaskRunning),
		}, id, true
	}
	return TaskIndexEntry{}, 0, false
}

// stopJoinedRow ends one hand-off that joined a run and leaves the run going.
// The store's own cancel is the whole stop: it ends the task and what hangs on
// it, and the run's next pass ends the worker that held it. The row is settled
// here and not at the run's end, because a row a person stopped that went on
// spinning until everything beside it finished is a stop that looks refused.
func (a *Agent) stopJoinedRow(run *beltRun, id uint64, why string) (string, bool, error) {
	key := strconv.FormatUint(id, 10)
	task := run.store.Task(key)
	title := ""
	if task != nil {
		title = task.Title
	}
	name := taskStopName(id, title)
	if task == nil {
		// A JOINED ROW WHOSE STORE TASK IS GONE is a row nothing can move, and
		// answering "already finished" over a row the rail draws running is the
		// same row saying two things. It is settled as stopped instead.
		return a.stopUndrivenRow(id, run.row, why)
	}
	if terminalStoreStatus(task.Status) {
		return name + " has already finished; there is nothing to stop", true, nil
	}
	if _, err := run.store.Cancel(key, stopBecause(taskStoppedWord, why)); err != nil {
		return "", true, err
	}
	if g := a.graph(); g != nil {
		notice := TaskNotice{
			ID: id, Title: title, State: TaskFailed, Stopped: true, Parent: run.row,
			Report: stopBecause(taskStoppedWord, why), EndedAt: a.taskClockNow(),
		}
		for _, kept := range g.runRows(id) {
			if kept.ID == id {
				notice.Title, notice.StartedAt = kept.Title, kept.StartedAt
			}
		}
		a.publishRunRow(g, notice)
	}
	return "stopped " + stopBecause(name, why), true, nil
}

// endedBeltRow answers a stop on a run's row whose run is already over. The
// row is still on the person's screen, so the press is a real one and is owed
// the sentence every settled task answers, not the graph's "there is no task".
func (a *Agent) endedBeltRow(id uint64, why string) (string, bool, error) {
	g := a.graph()
	if g == nil {
		return "", false, nil
	}
	for _, kept := range g.runRows(id) {
		if kept.ID == id && kept.Run == "" {
			// A ROW STILL SAYING RUNNING WITH NO RUN BEHIND IT is not finished,
			// and the stop is what clears it ([Agent.stopUndrivenRow]).
			if kept.State == TaskRunning {
				return a.stopUndrivenRow(id, kept.Parent, why)
			}
			return taskStopName(id, kept.Title) + " has already finished; there is nothing to stop", true, nil
		}
	}
	return "", false, nil
}

// runRowUndrivenWord is the one sentence a run row with nothing behind it
// answers a message with: the run that published it is not the one this
// conversation is driving, or the plan it names holds no such task, so no
// worker can read the words. It says the one thing a person can do about it.
const runRowUndrivenWord = "nothing is driving this task any more, so no worker can read a message; stop it to clear the row"

// stopUndrivenRow settles a run row nothing drives: a row the rail draws
// running whose run is gone, or whose task the run's plan does not hold. THERE
// IS NO WORK TO CUT, so the stop is the row's ending and nothing else, written
// where every run row's ending is written ([Agent.publishRunRow]) so the rail
// and the conversation read back tomorrow agree that it stopped.
func (a *Agent) stopUndrivenRow(id, parent uint64, why string) (string, bool, error) {
	g := a.graph()
	notice := TaskNotice{ID: id, Parent: parent}
	for _, kept := range g.runRows(id) {
		if kept.ID == id {
			notice = kept
		}
	}
	notice.State, notice.Stopped = TaskFailed, true
	notice.Report = stopBecause(taskStoppedWord, why) + " · nothing was driving it any more"
	notice.EndedAt = a.taskClockNow()
	if g != nil {
		a.publishRunRow(g, notice)
	} else {
		a.emitTaskUpdate(notice)
	}
	return "stopped " + stopBecause(taskStopName(id, notice.Title), why) + " — nothing was driving it any more", true, nil
}

// sayToRunRow is a message to a row a run owns, and it reports whether the
// number is a run's at all: false sends the caller on to the answer that there
// is no such task.
//
// A RUN'S ROWS ARE NOT NODES, so the node door ([Agent.sayToTask]) had nothing
// to deliver to and answered every one of them `no task N in this session`,
// over a row the rail was drawing running. The live run's own rows take the
// words as a note on their task's page, which is where a run's worker reads
// what it is told between its steps (the page's own box writes the same note,
// [Agent.PlanNote]). A row nothing drives says so, and says it can be stopped.
func (a *Agent) sayToRunRow(id uint64, text string, origin messageOrigin) (SteerReceipt, bool, error) {
	a.beltMu.Lock()
	run := a.beltRun
	owned := run != nil && (run.row == id || slices.Contains(run.joined, id))
	a.beltMu.Unlock()
	if owned {
		key := strconv.FormatUint(id, 10)
		task := run.store.Task(key)
		if task == nil {
			return SteerReceipt{}, true, errors.New(taskStopName(id, "") + ": " + runRowUndrivenWord)
		}
		if terminalStoreStatus(task.Status) {
			return SteerReceipt{}, true, fmt.Errorf("%s has finished, not running", taskStopName(id, task.Title))
		}
		var err error
		if origin == fromPerson {
			_, err = run.store.AddPersonNote(key, text)
		} else {
			_, err = run.store.AddNote(key, plandb.NoteAgentChat, text)
		}
		if err != nil {
			return SteerReceipt{}, true, err
		}
		return SteerReceipt{Landing: steerRunNoteWord}, true, nil
	}
	g := a.graph()
	if g == nil {
		return SteerReceipt{}, false, nil
	}
	for _, kept := range g.runRows(id) {
		if kept.ID != id || kept.Run != "" {
			continue
		}
		if kept.State == TaskRunning {
			return SteerReceipt{}, true, errors.New(taskStopName(id, kept.Title) + ": " + runRowUndrivenWord)
		}
		return SteerReceipt{}, true, fmt.Errorf("%s is %s, not running", taskStopName(id, kept.Title), kept.State)
	}
	return SteerReceipt{}, false, nil
}

// beltRunStopped reports whether a person stopped this run, and the words they
// gave for it.
func (a *Agent) beltRunStopped(run *beltRun) (bool, string) {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	return run.stopped, run.stopReason
}

// beltRunRootRow answers the row a run's own task is published under, when id
// names the live run's own task. It is how the page's stop finds the run: the
// page speaks the store's ids and the stop speaks the row's number.
func (a *Agent) beltRunRootRow(id string) (uint64, bool) {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if a.beltRun == nil || planTaskID(id) != a.beltRun.root {
		return 0, false
	}
	return a.beltRun.row, true
}

// settleStoppedBeltRun ends a run a person stopped: what its workers had made
// is committed on the run's own branch and the copy given back, the run's page
// and the conversation are told once where that work is, and the rows settle as
// stopped by a person. NOTHING IS LANDED AND NO TURN IS BOUGHT: the person
// ended the spend, and a model call to narrate the ending would be more of it.
//
// A PROGRAM'S STOPPED WORK GOES WHERE AN ENDED ONE'S DOES: its folder finished
// the one way every ending of it is ([ProgramFolder.Finish]), with the stop's
// words as the body of the commit that holds what it left.
func (a *Agent) settleStoppedBeltRun(run *beltRun, why string, cut []string) {
	report := stopBecause(taskStoppedWord, why)
	var merge, branch string
	var changed []string
	if run.folder != nil {
		end := run.folder.Finish(report)
		report += " · " + end.Sentence()
		merge, changed = mergeInPlace, end.Changed
		if end.Kept {
			merge, branch = mergeKept, run.folder.Branch
		}
	} else {
		merge, changed = keptWork(run.tree, run.title, nil, a.signsGitWork())
		if merge != mergeInPlace {
			report += " · " + beltStoppedWhere(run.tree.branch, run.ground, changed)
		}
		// THE ROW NAMES A BRANCH ONLY WHEN THERE IS WORK ON IT, for the reason
		// the sentence does ([beltStoppedWhere]): measured on the real binary, a
		// run stopped in its first seconds drew `branch kept` beside "it had
		// changed nothing".
		if merge != mergeInPlace && len(changed) > 0 {
			branch = run.tree.branch
		}
	}
	if _, err := run.store.AddNote(run.root, run.root, report); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's outcome note failed: " + err.Error())
		}
	}
	note := userText(taskStopName(run.row, run.title) + " " + report)
	note.authored = true
	a.mu.Lock()
	a.recordUserLocked(note)
	a.mu.Unlock()

	// THE ROW ENDS WHERE THE RUN'S WORK DID — the instant the program was gone,
	// or the engine answered — and not after the kept work was committed
	// ([Agent.beltRunEndedAt]).
	notice := TaskNotice{
		ID: run.row, Title: run.title, State: TaskFailed, Stopped: true,
		Report: report, Changed: changed, Merge: merge, Branch: branch, EndedAt: a.beltRunEndedAt(run),
	}
	g := a.graph()
	if g == nil {
		a.emitTaskUpdate(notice)
		return
	}
	for _, kept := range g.runRows(run.row) {
		if kept.ID == run.row {
			notice.StartedAt, notice.Parent = kept.StartedAt, kept.Parent
		}
	}
	a.publishRunRow(g, notice)
	// A JOINED ROW THE STOP TOOK DOWN ENDS WITH THE STOP, NOT AS A FAULT: cut
	// is the run engine's own typed record of which tasks its ending cut
	// mid-flight, and the stop is a person, so the law draws those rows with
	// the stop ending ([Agent.settleJoinedRows], [TaskReasonOf]).
	a.settleJoinedRows(g, run, notice.EndedAt, TaskEndingStopped, cut)
}
