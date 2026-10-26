package main

// ── codeaf app: TASKS, QUICK TASKS, PROCESSES, RUNNING LONG, SLEEP ───────────
//
// Everything here is read from and done through the engine's own doors — the
// task graph's app reads and verbs (internal/session's task_app.go), the job
// registry's notices, the checkpoint's one move — so the app and the terminal
// never disagree about what a task is doing or what a process said.
//
// IDS ARE THE APP'S, AND THEY NAME THE AGENT. A task's number is its own
// conversation's, and two agents both have a task 3, so every id on this wire
// is "<agent>:<n>" for a task or a quick task and "<agent>:p<n>" for a process.
// The methods that take only an id find the agent from it.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// appTurnLong is how long a reply runs before the app is told it is running
// long and may be moved to a task. `codeaf app --turn-long` changes it.
const appTurnLong = 180 * time.Second

// appMoveWait bounds how long agents.move_to_task waits for the reply's task:
// the move happens at the reply's next step and writes a brief first.
const appMoveWait = 10 * time.Minute

// appMoveQuick is how long agents.move_to_task waits for a reply that moves at
// once before it answers that the move is taken.
const appMoveQuick = 300 * time.Millisecond

// appFollowEvery is how often a followed process's output is looked at.
const appFollowEvery = 500 * time.Millisecond

// appOutputLines bounds one processes.output answer.
const appOutputLines = 2000

// appTailBytes is how much of a process's output is read for its last line.
const appTailBytes = 4096

// appPromoteWait bounds how long quick.promote waits for its task to appear.
const appPromoteWait = 5 * time.Second

// appTaskID is a task's or a quick task's id on the wire.
func appTaskID(agent string, n uint64) string { return agent + ":" + strconv.FormatUint(n, 10) }

// appProcessID is a process's id on the wire: its agent, the job's number,
// and when it started.
//
// THE START IS PART OF THE ID because a job's number is only its
// conversation's count, and that count starts again at 1 when the app is
// opened again: the "p1" a quit stopped and the "p1" started after it are two
// processes, and a window holding the first must never stop or rerun the
// second.
func appProcessID(agent string, job session.JobNotice) string {
	id := agent + ":p" + strconv.Itoa(job.ID)
	if stamp := appProcessStamp(job); stamp != "" {
		id += "." + stamp
	}
	return id
}

// appProcessStamp is a job's start, as the last part of its id.
func appProcessStamp(job session.JobNotice) string {
	if job.Started.IsZero() {
		return ""
	}
	return strconv.FormatInt(job.Started.UnixMilli(), 36)
}

// appSplitID is an id's agent and the agent's own number for the thing.
func appSplitID(id string) (agent, local string, ok bool) {
	i := strings.LastIndex(id, ":")
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

// appProcessJob is a process id's agent, job number and start stamp ("" for
// an id written without one).
func appProcessJob(id string) (string, int, string, bool) {
	agent, local, ok := appSplitID(id)
	if !ok || !strings.HasPrefix(local, "p") {
		return "", 0, "", false
	}
	number, stamp, _ := strings.Cut(local[1:], ".")
	job, err := strconv.Atoi(number)
	return agent, job, stamp, err == nil && job > 0
}

// processRow is the job a process id names, and none when the conversation's
// job of that number is a different process, started at another time. It
// reads that one record (appprocesspage.go), never the whole list.
func (b *appBackend) processRow(id string) (string, session.JobNotice, error) {
	agent, _, _, _ := appProcessJob(id)
	ctx, cancel := context.WithTimeout(context.Background(), appwire.ProcessReadTimeout)
	defer cancel()
	record, _, err := b.processRecord(ctx, id, true)
	if err != nil {
		return "", session.JobNotice{}, err
	}
	return agent, record, nil
}

// processRowFromList finds a process in its conversation's whole list, for an
// engine too old to answer one record.
func (b *appBackend) processRowFromList(id string) (string, session.JobNotice, error) {
	agent, job, stamp, ok := appProcessJob(id)
	if !ok {
		return "", session.JobNotice{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	rows, _, err := b.processRows(agent)
	if err != nil {
		return "", session.JobNotice{}, err
	}
	for _, row := range rows {
		if row.ID == job && (stamp == "" || appProcessStamp(row) == stamp) {
			return agent, row, nil
		}
	}
	return "", session.JobNotice{}, appwire.Errorf(appwire.CodeNotFound, "there is no process %s", id)
}

// held answers the held conversation for an agent id, opening it if none is.
func (b *appBackend) heldLive(agent string) (*appAgent, *appLive, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	return b.heldLiveLocked(a)
}

// heldLiveLocked opens the conversation while its admission and release gate
// remains held by the caller, including through a work-producing action.
func (b *appBackend) heldLiveLocked(a *appAgent) (*appAgent, *appLive, error) {
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		return nil, nil, err
	}
	// A blank agent opened here is not announced (appblank.go): a crew's
	// start names it, or its first words say it, a moment later.
	said := false
	b.locked(func() { said = a.saidLocked() })
	if opened && said {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	live := b.liveOf(a)
	if live == nil {
		return nil, nil, appwire.Errorf(appwire.CodeEngine, "the conversation was let go; try again")
	}
	b.locked(func() { live.lastUse = b.now() })
	return a, live, nil
}

// ── tasks ───────────────────────────────────────────────────────────────────

// appTaskLine is a task's live status line as one notice says it, by the
// terminal's own order: the gap it is closing, what it is waiting on, its phase
// in its own words, and the check's phase the engine said last. A running
// task's line on the wire also has the call it is making ([appBackend.lineOf]).
func appTaskLine(notice *session.TaskNotice, phase string) string {
	return appFirstLine(appTaskSaid(notice), phase)
}

// appTaskSaid is what one notice says about its task's present, before any
// phase: the gap, the wait, or the task's own phase word.
func appTaskSaid(notice *session.TaskNotice) string {
	return appFirstLine(notice.Mending, notice.Waiting, notice.Doing)
}

// appFirstLine is the first of lines that says something.
func appFirstLine(lines ...string) string {
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" && line != string(session.PresenceIdle) {
			return redact.Secrets(line)
		}
	}
	return ""
}

// lineOf is one running task's line on the wire, the order the terminal's
// task page uses: what it said about its present, then the check's phase,
// then the call it is making. Callers hold b.mu.
func lineOf(live *appLive, key string) string {
	return appFirstLine(live.said[key], live.phases[key], live.calls[key])
}

// appFiles is a list of changed paths as file changes whose line counts are
// not known.
func appFiles(paths []string) []appwire.FileChange {
	var files []appwire.FileChange
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			files = append(files, appwire.FileChange{Path: path})
		}
	}
	return files
}

// appTaskOf is one task notice on the wire. proposal says it arrived as a
// proposal: a task that has not started and is waiting for the person.
func appTaskOf(agent string, notice *session.TaskNotice, proposal bool, phase string, now time.Time) (appwire.Task, bool) {
	if notice.Kind == session.TaskKindQuick {
		return appwire.Task{}, false
	}
	raw := string(notice.State)
	if strings.TrimSpace(raw) == "" {
		raw = string(session.TaskQueued)
	}
	state := appTaskState(raw)
	if state == "" {
		return appwire.Task{}, false
	}
	task := appwire.Task{
		Retryable: notice.Retryable,
		ID:        appTaskID(agent, notice.ID), Agent: agent, Title: notice.Title, State: state, Group: notice.Run, UpdatedAt: now,
		Irreversible: notice.Irreversible, YourCall: state == appwire.TaskUnverified,
		Result: redact.Secrets(strings.TrimSpace(notice.Result)), Files: appFiles(notice.Changed), SpentUSD: appFigure(notice.CostUSD),
	}
	// A FAILED TASK SAYS WHAT STOPPED IT. A run row that ended without working
	// carries its reason as the report and no result — a hand-off whose run never
	// started is the plainest case — and the engine's own read answers the same
	// row with the report as its result ([session.Agent.AppTasks]).
	if state == appwire.TaskFailed && task.Result == "" {
		task.Result = redact.Secrets(strings.TrimSpace(notice.Report))
	}
	if notice.Parent != 0 && notice.Parent != notice.ID {
		task.Parent = appTaskID(agent, notice.Parent)
	}
	// The task as its own conversation: its model, its clock, its brief, what
	// it waits on, and whether the person paused it.
	task.Model, task.Paused = strings.TrimSpace(notice.Model), notice.Paused
	task.Instructions = redact.Secrets(strings.TrimSpace(notice.Brief))
	task.CostUSD = task.SpentUSD
	if !notice.StartedAt.IsZero() {
		started := notice.StartedAt
		task.StartedAt = &started
	}
	if !notice.EndedAt.IsZero() && (state == appwire.TaskDone || state == appwire.TaskFailed || state == appwire.TaskUnverified) {
		ended := notice.EndedAt
		task.FinishedAt = &ended
	}
	for _, waited := range notice.DependsOn {
		task.WaitsOn = append(task.WaitsOn, appTaskID(agent, waited))
	}
	switch {
	case notice.Withdrawn != "":
		task.State, task.Result = appwire.TaskFailed, notice.Withdrawn
	case notice.Decided != nil && !notice.Decided.Approved:
		task.State, task.Result = appwire.TaskFailed, "Declined"
	case proposal && notice.Decided == nil:
		task.Proposed = true
		if !notice.Deadline.IsZero() && !notice.Irreversible {
			until := notice.Deadline
			task.ClockUntil = &until
		}
	}
	if state == appwire.TaskRunning {
		task.Line = appTaskLine(notice, phase)
	}
	task.Models = appNoticeModels(notice, task.Proposed)
	task.Source, task.Dismissed = appSourceOf(notice.Source), notice.Dismissed
	// THE ROW'S OWN FACTS, as session.appRunFacts reads them for a cold list:
	// a row with its stopped flag set is "stopped" (the flag does not say who
	// stopped it), otherwise the typed ending, and nothing is derived.
	task.Program, task.Ending, task.Branch = strings.TrimSpace(notice.Program), string(notice.Ending), strings.TrimSpace(notice.Branch)
	if notice.Stopped {
		task.Ending = string(session.TaskEndingStopped)
	}
	return task, true
}

// appSourceOf is a recorded source on the wire: bounded again and scrubbed, and
// nil when the engine captured none. A source is never made up here.
func appSourceOf(source *session.TaskSource) *appwire.TaskSource {
	if source == nil || source.ID == "" || (source.Kind != appwire.SourceMessage && source.Kind != appwire.SourceDirective) {
		return nil
	}
	// SCRUBBED BEFORE IT IS CUT, so a secret the cut would split is still a secret.
	text, cut := workgraph.CutBytes(redact.Secrets(source.Text), session.MaxTaskSourceBytes)
	out := &appwire.TaskSource{ID: source.ID, Kind: source.Kind, Text: text, Cut: cut || source.Cut}
	if !source.At.IsZero() {
		at := source.At
		out.At = &at
	}
	return out
}

// appTaskOfView is one task as the engine's app read describes it.
func appTaskOfView(agent string, view session.AppTaskView) appwire.Task {
	n, _ := strconv.ParseUint(view.ID, 10, 64)
	id := view.ID
	if n != 0 {
		id = appTaskID(agent, n)
	}
	task := appwire.Task{Retryable: view.Retryable, ID: id, Agent: agent, Title: view.Title, Result: redact.Secrets(view.Result), Files: appFiles(view.Files), BudgetUSD: appFigure(view.Cap), SpentUSD: appFigure(view.Spent)}
	if p, err := strconv.ParseUint(view.Parent, 10, 64); err == nil && p != 0 {
		task.Parent = appTaskID(agent, p)
	}
	switch view.State {
	case "waiting", "paused":
		task.State = appwire.TaskQueued
	case "proposed", "held":
		task.State, task.Proposed = appwire.TaskQueued, true
		if view.State == "proposed" && !view.Deadline.IsZero() {
			until := view.Deadline
			task.ClockUntil = &until
		}
	case "your_call":
		task.State, task.YourCall = appwire.TaskUnverified, true
	case "done":
		task.State = appwire.TaskDone
	case "running":
		task.State = appwire.TaskRunning
	default:
		task.State = appwire.TaskFailed
	}
	task.Paused = view.State == "paused" || view.Held
	task.Models = appModelsChip(view.RoutingState, view.Crew, view.TaskPins, view.Model)
	task.Model, task.StartedBy = view.Model, view.Origin
	task.Source, task.Dismissed = appSourceOf(view.Source), view.Dismissed
	task.Program, task.Ending, task.Branch = view.RunProgram, view.Ending, view.Branch
	if !view.FinishedAt.IsZero() && (task.State == appwire.TaskDone || task.State == appwire.TaskFailed) {
		ended := view.FinishedAt
		task.FinishedAt = &ended
	}
	task.Instructions = redact.Secrets(strings.TrimSpace(view.Brief))
	task.CostUSD = task.SpentUSD
	// THE STEP COUNT IS SENT WHEN IT IS KNOWN AND OMITTED WHEN IT IS NOT, which
	// includes a known zero: a record that counted no steps is a fact, and a task
	// with no record of its work is an absence, and they were both arriving as
	// the same missing field. A page that cannot tell them apart either says
	// "0 steps" about unknown history or says nothing about a task that really
	// has taken none.
	if view.StepsKnown || view.Step > 0 {
		steps := view.Step
		task.Steps = &steps
	}
	for _, waited := range view.Waits {
		if w, err := strconv.ParseUint(waited, 10, 64); err == nil {
			task.WaitsOn = append(task.WaitsOn, appTaskID(agent, w))
		}
	}
	return task
}

// taskNotice turns one task update into task.changed, once per change: the
// same update can arrive on the turn's stream and on the task lane. A quick
// task's update is a quick.changed instead.
func (b *appBackend) taskNotice(id string, live *appLive, notice *session.TaskNotice, proposal bool) {
	if notice.Kind == session.TaskKindQuick {
		b.quickMoved(id, live, notice.ID)
		return
	}
	key := appTaskID(id, notice.ID)
	var phase string
	b.locked(func() { phase = live.phases[key] })
	task, ok := appTaskOf(id, notice, proposal, phase, b.now())
	if !ok {
		return
	}
	same := false
	b.locked(func() {
		before, known := live.tasks[key]
		// What an update leaves out it has not changed: its title, its place
		// in the tree and its run are kept from what was said before.
		if task.Title == "" {
			task.Title = before.Title
		}
		if task.Models == nil {
			task.Models = before.Models
		}
		if task.Parent == "" {
			task.Parent = before.Parent
		}
		// A SOURCE IS A FACT OF ADMISSION AND NEVER MOVES: an update that leaves it out
		// has not changed it.
		if task.Source == nil {
			task.Source = before.Source
		}
		if task.Group == "" {
			task.Group = before.Group
		}
		// A PROGRAM IS A FACT OF THE ROW'S WHOLE LIFE: an update that leaves it out has not changed it.
		if task.Program == "" {
			task.Program = before.Program
		}
		// THE GROUP'S LABEL IS ITS RUN'S OWN ROW'S TITLE: the row with the run's
		// id and no parent is the run itself.
		if task.Group != "" {
			if task.Parent == "" {
				task.GroupTitle = task.Title
			} else {
				for _, other := range live.tasks {
					if other.Group == task.Group && other.Parent == "" {
						task.GroupTitle = other.Title
						break
					}
				}
			}
			if task.GroupTitle == "" {
				task.GroupTitle = before.GroupTitle
			}
		}
		if task.Result == "" && task.State == before.State {
			task.Result = before.Result
		}
		if task.Files == nil && task.State == before.State {
			task.Files = before.Files
		}
		if task.State == appwire.TaskRunning {
			live.said[key] = appTaskSaid(notice)
			task.Line = lineOf(live, key)
		}
		if known {
			at := before.UpdatedAt
			before.UpdatedAt = task.UpdatedAt
			same = reflect.DeepEqual(before, task)
			if same {
				task.UpdatedAt = at
			}
		}
		if task.State == appwire.TaskDone || task.State == appwire.TaskFailed {
			delete(live.phases, key)
			delete(live.said, key)
			delete(live.calls, key)
		}
		live.tasks[key] = task
	})
	if task.State == appwire.TaskRunning {
		b.flyPilot(live, key, notice.ID)
	} else {
		b.landPilot(live, key)
	}
	if same {
		return
	}
	live.box.Event(appwire.EventTaskChanged, appwire.TaskChanged{Task: task})
}

// taskPhase folds one phase move into its task's live line.
func (b *appBackend) taskPhase(id string, live *appLive, notice *session.TaskPhaseNotice) {
	key := appTaskID(id, notice.ID)
	words := tui3.TaskPhaseWords(notice.Phase, notice.Round, notice.Rounds)
	var task appwire.Task
	moved := false
	b.locked(func() {
		// A phase move is newer than the wait the task last said it was in.
		live.phases[key] = words
		delete(live.said, key)
		task, moved = b.relineLocked(live, key)
	})
	if moved {
		live.box.Event(appwire.EventTaskChanged, appwire.TaskChanged{Task: task})
	}
}

// relineLocked redraws one running task's line and answers the task when the
// line moved. Callers hold b.mu.
func (b *appBackend) relineLocked(live *appLive, key string) (appwire.Task, bool) {
	before, known := live.tasks[key]
	line := lineOf(live, key)
	if !known || before.State != appwire.TaskRunning || before.Line == line {
		return appwire.Task{}, false
	}
	before.Line, before.UpdatedAt = line, b.now()
	live.tasks[key] = before
	return before, true
}

// appTaskRoom is a conversation that can be watched one task at a time, the
// door the terminal's task rail reads a running task's current call through.
type appTaskRoom interface {
	WatchTaskRoom(id uint64) (<-chan session.Event, func(), error)
}

// appTaskJournal is a conversation in another process, whose tasks are read
// off the tail of their own journals, as the terminal's hosted task page does.
type appTaskJournal interface {
	TaskRoom(id uint64, tail int) (session.TaskRecord, error)
}

// appPilotTail is how much of a task's journal one look reads, in bytes: the
// call in flight is the last thing written.
const appPilotTail = 128 << 10

// appPilotBeat is how often a task's journal is looked at for its call.
const appPilotBeat = time.Second

// flyPilot watches one running task's calls, once, so its line can say the
// call it is making: `write essay3.md`, `bash go test ./...`.
func (b *appBackend) flyPilot(live *appLive, key string, n uint64) {
	room, watches := live.conn.Conv.(appTaskRoom)
	journal, reads := live.conn.Conv.(appTaskJournal)
	if !watches && !reads {
		return
	}
	flying := false
	b.locked(func() {
		_, flying = live.pilots[key]
		if !flying {
			live.pilots[key] = func() {}
		}
	})
	if flying {
		return
	}
	var lane <-chan session.Event
	var stop func()
	if watches {
		var err error
		if lane, stop, err = room.WatchTaskRoom(n); err != nil {
			b.locked(func() { delete(live.pilots, key) })
			return
		}
	} else {
		done := make(chan struct{})
		var once sync.Once
		stop = func() { once.Do(func() { close(done) }) }
		lane = appJournalLane(journal, n, done)
	}
	if stop == nil {
		stop = func() {}
	}
	landed := false
	b.locked(func() {
		// The task may have landed while the lane was opening.
		_, kept := live.pilots[key]
		if landed = !kept; !landed {
			live.pilots[key] = stop
		}
	})
	if landed {
		stop()
		return
	}
	guard.Go("app/pilot", func() { b.pilot(live, key, lane) })
}

// appJournalLane turns looks at a task's journal into the call events a
// watched lane would carry: a begin when a call is in flight, an end when
// none is. It closes when done does.
func appJournalLane(journal appTaskJournal, n uint64, done <-chan struct{}) <-chan session.Event {
	lane := make(chan session.Event, 1)
	guard.Go("app/pilot-journal", func() {
		defer close(lane)
		tick := time.NewTicker(appPilotBeat)
		defer tick.Stop()
		said := ""
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			record, err := journal.TaskRoom(n, appPilotTail)
			if err != nil {
				continue
			}
			event := session.Event{Kind: session.EventToolEnd}
			read := session.ReadTranscriptBytes(record.Journal)
			if last := appInFlight(read.Entries); last != nil {
				event = session.Event{Kind: session.EventToolBegin, Tool: last.Tool, Args: last.Args, Hint: last.Hint}
			}
			now := event.Tool + "\x00" + event.Args
			if now == said {
				continue
			}
			said = now
			select {
			case lane <- event:
			case <-done:
				return
			}
		}
	})
	return lane
}

// appInFlight is the call a journal's tail has asked for and not yet heard
// back from, or nil.
func appInFlight(entries []session.DisplayEntry) *session.DisplayEntry {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role != "tool" {
			continue
		}
		if entries[i].Answered {
			return nil
		}
		return &entries[i]
	}
	return nil
}

// pilot folds one task's call events into its line until the lane closes.
func (b *appBackend) pilot(live *appLive, key string, lane <-chan session.Event) {
	for event := range lane {
		var call string
		switch event.Kind {
		case session.EventToolAnnounced, session.EventToolBegin:
			call = redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, event.Hint))
		case session.EventToolEnd, session.EventToolFailed:
		default:
			continue
		}
		var task appwire.Task
		moved := false
		b.locked(func() {
			if _, watched := live.pilots[key]; !watched {
				return
			}
			live.calls[key] = call
			task, moved = b.relineLocked(live, key)
		})
		if moved {
			live.box.Event(appwire.EventTaskChanged, appwire.TaskChanged{Task: task})
		}
	}
}

// landPilot stops watching one task, which has landed.
func (b *appBackend) landPilot(live *appLive, key string) {
	var stop func()
	b.locked(func() {
		stop = live.pilots[key]
		delete(live.pilots, key)
		delete(live.calls, key)
	})
	if stop != nil {
		stop()
	}
}

// landPilots stops watching every task of a conversation being given back.
func (b *appBackend) landPilots(live *appLive) {
	var stops []func()
	b.locked(func() {
		for key, stop := range live.pilots {
			stops = append(stops, stop)
			delete(live.pilots, key)
		}
	})
	for _, stop := range stops {
		stop()
	}
}

// GetTask answers one task's page.
func (b *appBackend) GetTask(id string) (appwire.TaskPage, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.TaskPage{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.TaskPage{}, err
	}
	view, found, err := live.conn.Conv.AppTask(local)
	if err != nil {
		return appwire.TaskPage{}, appEngineError(err)
	}
	if !found {
		return appwire.TaskPage{}, appwire.Errorf(appwire.CodeNotFound, "there is no task %s", id)
	}
	task := appTaskOfView(agent, view)
	b.locked(func() {
		// What the lane said is newer than a read, for the live line above all.
		if said, known := live.tasks[task.ID]; known {
			task.Line, task.Group, task.Irreversible = said.Line, said.Group, said.Irreversible
			if task.Proposed {
				task.ClockUntil = said.ClockUntil
			}
		}
	})
	page := appwire.TaskPage{Task: task, Result: task.Result, Files: task.Files, Brief: view.Brief, Program: appTaskProgram(view.Program)}
	at := b.now()
	page.Log = appTaskLog(agent, task.ID, view, at)
	for i, note := range view.Notes {
		page.Notes = append(page.Notes, appwire.TaskNote{ID: task.ID + ".n" + strconv.Itoa(i),
			Text: note.Text, At: at, From: appNoteFrom(note.From), ReadAtStep: note.ReadAtStep, ClientID: note.ClientID})
	}
	return page, nil
}

// appNoteFrom is a note's origin on the wire. The engine's own word is carried
// through; anything it does not label stays unlabelled, because "unknown" and
// "the person said it" are not the same claim (appwire's [appwire.TaskNote]).
func appNoteFrom(from session.AppNoteFrom) string {
	switch from {
	case session.AppNoteFromPerson:
		return appwire.NoteFromYou
	case session.AppNoteFromTask:
		return appwire.NoteFromTask
	}
	return ""
}

// appTaskLog is a task's work as the page's log: ONE TOOL ENTRY PER STEP,
// carrying the facts of its call.
//
// THIS IS WHERE THE PAGE USED TO LOSE EVERY STEP IT HAD. The engine answered
// with worded lines and this loop wrapped each of them in a quiet NOTICE, so an
// app that filters its work log to `kind == "tool"` — which it must, because
// that is the only kind carrying a kind, a clock and an exit code — found none,
// and a task that really ran sixty-two tests over seven steps drew a blank log
// under a footer that said nothing. The engine now answers with facts
// ([session.AppTaskStep]) and they are dressed as the tool entries they are.
//
// A LEGACY STEP IS STILL A NOTICE, and deliberately. A record that kept a
// command and no tool cannot support a claim about which hands were used, so
// such a step keeps its own words on a dim line rather than being classified
// into a kind it never declared. A page shows it; nothing pretends it is proof.
func appTaskLog(agent, id string, view session.AppTaskView, at time.Time) []appwire.Entry {
	var log []appwire.Entry
	for i, step := range view.Steps {
		if step.Legacy {
			if line := strings.TrimSpace(step.Text); line != "" {
				log = append(log, appwire.Entry{ID: appStepEntryID(id, step, i), Agent: agent,
					Kind: appwire.EntryNotice, At: at, Text: redact.Secrets(line), Quiet: true})
			}
			continue
		}
		log = append(log, appTaskStepEntry(agent, id, step, i, at))
	}
	if len(log) > 0 {
		return log
	}
	// NO TYPED STEPS IS NOT NO WORK. A task from an engine lane that has not
	// filled Steps, or one whose only record is the worded tail, still has lines
	// a person can read, and they are said as the lines they are.
	for i, line := range view.WorkLog {
		if line = strings.TrimSpace(line); line != "" {
			log = append(log, appwire.Entry{ID: id + ".l" + strconv.Itoa(i), Agent: agent,
				Kind: appwire.EntryNotice, At: at, Text: redact.Secrets(line), Quiet: true})
		}
	}
	return log
}

// appStepEntryID is one step's id on the page, stable across reads of the same
// record: the engine's own stable id for the step, scoped to the task so two
// tasks of one agent cannot collide.
func appStepEntryID(task string, step session.AppTaskStep, at int) string {
	if id := strings.TrimSpace(step.ID); id != "" {
		return task + ".s" + id
	}
	return task + ".s" + strconv.Itoa(at)
}

// appTaskStepEntry is one typed step as its tool entry.
//
// EVERY FIELD IS CARRIED OR LEFT OUT; none is defaulted. A duration appears only
// where a clock was recorded, an exit code only where a command reported one, an
// outcome only where the record settled it — so a reader can tell "it passed"
// from "nobody wrote down how it went", which is the whole point of the change.
func appTaskStepEntry(agent, task string, step session.AppTaskStep, at int, now time.Time) appwire.Entry {
	entry := appwire.Entry{
		ID:     appStepEntryID(task, step, at),
		Agent:  agent,
		Kind:   appwire.EntryTool,
		At:     now,
		Call:   step.Call,
		Name:   step.Tool,
		Title:  redact.Secrets(appStepTitle(step)),
		Detail: appDetail(redact.Secrets(step.Detail)),
		OK:     step.OK,
	}
	if !step.StartedAt.IsZero() {
		entry.At = step.StartedAt
	}
	entry.ToolFacts = appStepFacts(step)
	entry.ToolFacts.Task = task
	return entry
}

// appStepTitle is the one line the row shows for a step: what it was asked to
// do where the record kept that, and the call's own gloss otherwise.
func appStepTitle(step session.AppTaskStep) string {
	for _, said := range []string{step.Command, step.Target, step.Text} {
		if said = strings.TrimSpace(said); said != "" {
			return said
		}
	}
	return step.Tool
}

// appStepFacts is a step's facts in the work log's own shape. The KIND comes
// from the engine's family for the tool through the one door every other call
// uses ([appToolKind]), so a task's steps and a conversation's steps are sorted
// by exactly the same rule and cannot drift apart.
func appStepFacts(step session.AppTaskStep) appwire.ToolFacts {
	facts := appwire.ToolFacts{
		Kind:      appToolKind(step.Tool, step.Service),
		Batch:     step.Batch,
		Connector: step.ServiceName,
	}
	if facts.Kind == appwire.ToolConnector && facts.Connector == "" {
		facts.Connector = step.Service
	}
	if !step.StartedAt.IsZero() {
		started := step.StartedAt
		facts.StartedAt = &started
	}
	// AN END IS SAID ONLY WHERE ONE WAS RECORDED. A step whose record kept a
	// duration but no instants has a real duration and no real end, and
	// manufacturing one from "now" would let a page draw a wall-clock bar for
	// work that finished days ago.
	if !step.EndedAt.IsZero() {
		ended := step.EndedAt
		facts.EndedAt = &ended
	}
	if step.ExitCode != nil {
		code := *step.ExitCode
		facts.ExitCode = &code
	}
	if len(step.Tail) > 0 {
		facts.Tail = append([]string(nil), step.Tail...)
		for i := range facts.Tail {
			facts.Tail[i] = redact.Secrets(facts.Tail[i])
		}
	}
	if step.Lines != nil {
		lines := *step.Lines
		facts.Lines = &lines
	}
	if input := appToolInput(facts.Kind, step.Tool, step.Args, step.Target, ""); input != nil {
		facts.Input = input
	}
	if path, adds, dels, ok := tui3.ToolFileChange(step.Tool, step.Args); ok {
		facts.Files = []appwire.FileChange{{Path: path, Add: adds, Del: dels}}
	}
	return facts
}

// ActOnTask is every verb on a task, through the engine's one task door.
func (b *appBackend) ActOnTask(id, action, verdict, text string) error {
	return b.actOnTask(id, action, verdict, text, "")
}

// actOnTask is ActOnTask with a note's opaque client id, which only the graph
// task's note road keeps (session.AppTaskIntent.ClientID).
func (b *appBackend) actOnTask(id, action, verdict, text, clientID string) error {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return err
	}
	answer := false
	if action == "start" {
		b.locked(func() {
			if task, ok := live.tasks[id]; ok && task.Proposed {
				answer = true
			}
			for _, q := range live.questions {
				if q.Kind == session.QuestionTask && strconv.FormatUint(q.ID, 10) == local {
					answer = true
				}
			}
		})
	}
	switch action {
	case "cancel", "stop", "hold", "typing", "decline", "dismiss", "restore":
	case "decide":
		if verdict == appwire.VerdictNotRight || verdict == "not_right" || verdict == "tell" {
			if err := b.workAllowed(a); err != nil {
				return err
			}
		}
	default:
		if !answer {
			if err := b.workAllowed(a); err != nil {
				return err
			}
		}
	}
	intent := session.AppTaskIntent{ID: local, Action: action, Text: strings.TrimSpace(text), Verb: verdict, ClientID: clientID}
	switch action {
	case "decide":
		// TELL IT… WITH WORDS AND NO VERDICT IS A NOTE; not_right carries them.
		if verdict == appwire.VerdictNotRight {
			intent.Verb = "not_right"
		}
	case "hold", "typing":
		// A held card is a held clock: the same engine hold, whether the person
		// pressed hold or started typing in the card.
		intent.Action = "hold"
	case "brief":
		return b.briefOnTask(live, id, intent)
	}
	// A TASK THE APP HAS ALREADY SEEN START can be a moment ahead of the
	// engine's own task list; the action is asked again for a few seconds
	// rather than refused for a task that is there.
	known := false
	b.locked(func() { _, known = live.tasks[id] })
	deadline := b.now().Add(appTaskSettleWait)
	for {
		err := live.conn.Conv.AppTaskAction(intent)
		if err == nil {
			if action == session.AppActionDismiss || action == session.AppActionRestore {
				b.sayDismissal(id, local, live)
			}
			return nil
		}
		if !known || !strings.Contains(err.Error(), "no such task") || b.now().After(deadline) {
			return appTaskError(err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// briefOnTask sends a brief through the engine's TYPED door and answers from
// its receipt, never from an error's words.
//
// EVERY APP CONVERSATION IS A REMOTE ONE, and the plain door's error crosses the
// socket as bare text: a brief whose words were delivered but whose page brief
// was not kept, a plan task's amendment whose commit may be on the disk, and a
// call whose reply never came all used to read as an ordinary engine error,
// which invited the person to send the brief again and have it land twice. The
// receipt crosses typed. Unknown and a brief that was not kept are `uncertain`;
// a refusal keeps its own code, and only a refusal that the task is not listed
// yet is asked again ([appBackend.receiptOnTask]), because nothing was done.
// Nothing else is sent again, and the plain door is never the fallback: an
// engine that cannot type the outcome is answered `uncertain` too.
func (b *appBackend) briefOnTask(live *appLive, id string, intent session.AppTaskIntent) error {
	receipt := b.receiptOnTask(live, id, intent)
	switch {
	case receipt.Outcome == session.AppTaskRefused:
		return appTaskError(errors.New(receipt.Reason))
	case receipt.Outcome == session.AppTaskUnknown, receipt.BriefNotKept:
		reason := receipt.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "it is not known whether that brief reached the task"
		}
		return appwire.Errorf(appwire.CodeUncertain, "%s", redact.Secrets(plainWords(reason)))
	}
	return nil
}

// sayDismissal says task.changed for a task the person just hid or brought
// back, read from the engine AFTER its action returned, so the event carries the
// state the checkpoint holds and not the one the app hoped for. A plan row has no
// notice road of its own, which is why the app's door says this itself for every
// road rather than only for the ones that do.
func (b *appBackend) sayDismissal(id, local string, live *appLive) {
	view, found, err := live.conn.Conv.AppTask(local)
	if err != nil || !found {
		return
	}
	agent, _, _ := appSplitID(id)
	task := appTaskOfView(agent, view)
	task.ID = id
	b.locked(func() {
		if before, known := live.tasks[id]; known {
			// The cached task keeps everything the notice road said; the engine's read
			// moves only what dismissing moves.
			before.Dismissed, before.UpdatedAt = task.Dismissed, b.now()
			if before.Source == nil {
				before.Source = task.Source
			}
			task = before
		} else {
			task.UpdatedAt = b.now()
		}
		live.tasks[id] = task
	})
	live.box.Event(appwire.EventTaskChanged, appwire.TaskChanged{Task: task})
}

// SetTaskModel is the proposal card's Change model: one proposed task's model,
// never the agent's, with the card's own state and version honoured.
func (b *appBackend) SetTaskModel(id, model, state string, version *uint64) error {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	intent := session.AppTaskIntent{ID: local, Action: session.AppActionProposalModel, Model: strings.TrimSpace(model), Version: version}
	if state = strings.TrimSpace(state); state != "" {
		intent.State = &state
	}
	if err := live.conn.Conv.AppTaskAction(intent); err != nil {
		return appTaskError(err)
	}
	return nil
}

// appTaskSettleWait bounds how long an action on a task the app has seen
// waits for the engine's list to name it.
const appTaskSettleWait = 3 * time.Second

// appTaskError sorts an engine refusal about a task into the app's codes.
func appTaskError(err error) error {
	// AN OUTCOME NOBODY CAN NAME IS NOT A REFUSAL. The engine's own sentinels
	// say the words may have arrived, or did arrive with the page's brief left
	// unwritten; either way sending them again would deliver them twice. A plan
	// store's failed commit is the same: the change may be on the disk. THESE
	// ARE READ ONLY IN PROCESS: across the remote socket every app conversation
	// uses, the plain door's error is bare text, which is why a brief is answered
	// from its typed receipt instead ([appBackend.briefOnTask]).
	if errors.Is(err, session.ErrSendUnanswered) || errors.Is(err, session.ErrBriefNotKept) || errors.Is(err, plandb.ErrCommitUncertain) {
		return appwire.Errorf(appwire.CodeUncertain, "%s", plainWords(err.Error()))
	}
	words := err.Error()
	switch {
	case strings.Contains(words, "no such task"), strings.Contains(words, "no such quick task"):
		return appwire.Errorf(appwire.CodeNotFound, "%s", plainWords(words))
	case strings.Contains(words, "stale"), strings.Contains(words, "not waiting"), strings.Contains(words, "finished"), strings.Contains(words, "unknown"), strings.Contains(words, "must be"), strings.Contains(words, "choose one"),
		strings.Contains(words, "not a settled failure"), strings.Contains(words, "not settled work to hide"), strings.Contains(words, "still has"):
		return appwire.Errorf(appwire.CodeBadParams, "%s", plainWords(words))
	}
	return appEngineError(err)
}

// ── quick tasks ─────────────────────────────────────────────────────────────

// appQuickOf is one quick task on the wire.
func appQuickOf(agent string, view session.AppQuickTaskView, conflict []string) appwire.Quick {
	n, _ := strconv.ParseUint(view.ID, 10, 64)
	quick := appwire.Quick{ID: appTaskID(agent, n), Agent: agent, Title: view.Title, Files: []appwire.FileChange{}}
	for _, file := range view.Files {
		quick.Files = append(quick.Files, appwire.FileChange{Path: file.Path, Add: file.Added, Del: file.Removed})
	}
	switch view.State {
	case "running":
		quick.State = appwire.QuickRunning
	case "undone":
		quick.State = appwire.QuickUndone
	case "done":
		quick.State = appwire.QuickDone
	case "promoted":
		// A quick task that became a full task has always gone out as done: the
		// full task is where its work now shows, and the app has no other word
		// for the row it left behind. Kept so this change moves no other row.
		quick.State = appwire.QuickDone
	case "stopped":
		quick.State = appwire.QuickStopped
	case "grew":
		quick.State = appwire.QuickGrew
		quick.Grew = &appwire.QuickGrowth{Files: len(view.Files), Reason: view.Grew}
	default:
		quick.State = view.State
	}
	if len(conflict) > 0 && quick.State == appwire.QuickDone {
		quick.State = appwire.QuickConflict
		quick.Conflict = &appwire.QuickChanged{Changed: append([]string(nil), conflict...)}
	}
	return quick
}

// quickMoved reads one quick task again and says quick.changed if it moved.
// The read is a call to the engine, so it is made off the lane that heard it.
func (b *appBackend) quickMoved(agent string, live *appLive, n uint64) {
	guard.Go("app/quick", func() { b.sayQuick(agent, live, appTaskID(agent, n)) })
}

// sayQuick reads one quick task and says quick.changed if it moved.
func (b *appBackend) sayQuick(agent string, live *appLive, id string) (appwire.Quick, bool) {
	_, local, _ := appSplitID(id)
	rows, err := live.conn.Conv.AppQuickTasks()
	if err != nil {
		return appwire.Quick{}, false
	}
	for _, row := range rows {
		if row.ID != local {
			continue
		}
		var conflict []string
		b.locked(func() { conflict = live.conflicts[id] })
		quick := appQuickOf(agent, row, conflict)
		same := false
		b.locked(func() {
			before, known := live.quicks[id]
			same = known && reflect.DeepEqual(before, quick)
			live.quicks[id] = quick
		})
		if !same {
			live.box.Event(appwire.EventQuickChanged, appwire.QuickChangedEvent{Quick: quick})
		}
		return quick, true
	}
	return appwire.Quick{}, false
}

// UndoQuick puts back every file a quick task wrote, or none: a file changed
// since is a conflict, named, and nothing is touched.
func (b *appBackend) UndoQuick(id string) error {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a quick task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	_, changed, err := live.conn.Conv.AppQuickTaskAction(session.AppTaskIntent{ID: local, Action: "undo"})
	if err != nil {
		return appTaskError(err)
	}
	b.locked(func() {
		if len(changed) > 0 {
			live.conflicts[id] = changed
		} else {
			delete(live.conflicts, id)
		}
	})
	b.sayQuick(agent, live, id)
	if len(changed) > 0 {
		return &appwire.Error{Code: appwire.CodeConflict, Message: "changed since the quick task wrote them: " + strings.Join(changed, ", "), Changed: changed}
	}
	return nil
}

// PromoteQuick makes a quick task a full task and answers that task.
func (b *appBackend) PromoteQuick(id string) (appwire.Task, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Task{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a quick task id", id)
	}
	a, err := b.find(agent)
	if err != nil {
		return appwire.Task{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return appwire.Task{}, err
	}
	if err := b.workAllowed(a); err != nil {
		return appwire.Task{}, err
	}
	before := map[string]bool{}
	if views, err := live.conn.Conv.AppTasks(); err == nil {
		for _, view := range views {
			before[view.ID] = true
		}
	}
	if _, _, err := live.conn.Conv.AppQuickTaskAction(session.AppTaskIntent{ID: local, Action: "promote"}); err != nil {
		return appwire.Task{}, appTaskError(err)
	}
	b.sayQuick(agent, live, id)
	deadline := time.Now().Add(appPromoteWait)
	for {
		if views, err := live.conn.Conv.AppTasks(); err == nil {
			for _, view := range views {
				if !before[view.ID] && view.ID != local {
					return appTaskOfView(agent, view), nil
				}
			}
		}
		if time.Now().After(deadline) {
			return appwire.Task{}, appwire.Errorf(appwire.CodeEngine, "the quick task was promoted, but its task has not appeared yet")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ── processes ───────────────────────────────────────────────────────────────

// appFollow is one process the app is following, and how to stop following.
type appFollow struct {
	stop chan struct{}
	done chan struct{}
}

// appProcessOf is one job notice on the wire.
func appProcessOf(agent string, job session.JobNotice) appwire.Process {
	process := appProcessBase(agent, job)
	process.LastLine = redact.Secrets(appLastLine(job.LogPath))
	return process
}

// appProcessBase is a process in the wire's words without its last line,
// which each door reads its own way (appprocesspage.go reads it bounded).
func appProcessBase(agent string, job session.JobNotice) appwire.Process {
	process := appwire.Process{
		ID: appProcessID(agent, job), Agent: agent, Title: redact.Secrets(job.Label()), Command: redact.Secrets(job.Command),
		StartedAt: job.Started,
	}
	// Only a loopback origin leaves the engine as a process URL; a record from
	// before the strict reader may hold a looser spelling, which is normalized
	// or dropped here.
	if origin, ok := session.LoopbackOrigin(job.URL); ok {
		process.URL = origin
	}
	switch job.State {
	case session.JobRunning:
		process.State = appwire.ProcessRunning
	case session.JobDone:
		process.State = appwire.ProcessEnded
	case session.JobFailed:
		process.State = appwire.ProcessFailed
	default:
		process.State = appwire.ProcessStopped
	}
	if process.State != appwire.ProcessRunning {
		if !job.Ended.IsZero() {
			process.EndedAt = &job.Ended
		} else if !job.Started.IsZero() && job.Elapsed > 0 {
			ended := job.Started.Add(job.Elapsed)
			process.EndedAt = &ended
		}
		if process.State != appwire.ProcessStopped && !job.ExitCodeUnknown {
			code := job.ExitCode
			process.ExitCode = &code
		}
	}
	return process
}

// appLastLine is the last line a process wrote, read off its spool's tail.
func appLastLine(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	start := max(info.Size()-appTailBytes, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return appDetail(line)
		}
	}
	return ""
}

// processMoved says process.changed for one job update, once per change.
func (b *appBackend) processMoved(agent string, live *appLive, job *session.JobNotice) {
	if job.Kind == session.JobKindTask {
		return
	}
	recovered := appRecoverJob(*job, live.replayJobs)
	process := appProcessOf(agent, recovered)
	same, finished := false, false
	b.locked(func() {
		before, known := live.processes[process.ID]
		live.processes[process.ID] = process
		same = known && reflect.DeepEqual(before, process)
		finished = known && before.State == appwire.ProcessRunning && (process.State == appwire.ProcessEnded || process.State == appwire.ProcessFailed)
		live.logs[process.ID] = job.LogPath
	})
	if !same {
		live.box.Event(appwire.EventProcessChanged, appwire.ProcessChanged{Process: process})
	}
	// A PROCESS THAT ENDS ON ITS OWN leaves one quiet line in the
	// conversation, pointing at its output.
	if finished {
		footer := appFooter("pf"+process.ID, agent, 0, appwire.FooterProcessFinished, "Process finished · "+process.Title, b.now())
		footer.Ref = process.ID
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: footer})
	}
}

// processRows is an agent's processes: the live registry's while its
// conversation is held, its record on disk otherwise.
func (b *appBackend) processRows(agent string) ([]session.JobNotice, *appLive, error) {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return nil, nil, err
	}
	var transcript string
	live := b.liveOf(a)
	b.locked(func() { transcript = a.transcript })
	if live != nil {
		rows, err := live.conn.Conv.JobNotices()
		if err != nil {
			return nil, nil, appListError(err)
		}
		for i := range rows {
			rows[i] = appRecoverJob(rows[i], live.replayJobs)
		}
		return rows, live, nil
	}
	rows, err := session.ReadProcessNotices(transcript)
	if err != nil {
		return nil, nil, appListError(err)
	}
	return rows, nil, nil
}

// appListError says a history the whole list cannot carry as too_large,
// pointing at the method that can, and anything else as the engine's error.
// The list never answers part of a history as though it were all of it.
func appListError(err error) error {
	if errors.Is(err, session.ErrProcessListPartial) || strings.Contains(err.Error(), session.ErrProcessListPartial.Error()) {
		return appwire.Errorf(appwire.CodeTooLarge, "this conversation's process history does not fit processes.list; read it with processes.page")
	}
	return appEngineError(err)
}

// ListProcesses answers an agent's processes, running and ended.
func (b *appBackend) ListProcesses(agent string) ([]appwire.Process, error) {
	rows, live, err := b.processRows(agent)
	if err != nil {
		return nil, err
	}
	var out []appwire.Process
	for _, row := range rows {
		if row.Kind == session.JobKindTask {
			continue
		}
		process := appProcessOf(agent, row)
		if live != nil {
			b.locked(func() { live.logs[process.ID] = row.LogPath })
		}
		out = append(out, process)
	}
	// One list: the agent's kept-running processes are its processes too.
	return append(b.quitStopped(agent, out), b.serviceProcesses(agent)...), nil
}

// processLog is where one process's output is spooled.
func (b *appBackend) processLog(id string) (string, error) {
	_, row, err := b.processRow(id)
	if err != nil {
		return "", err
	}
	return row.LogPath, nil
}

// appReadLines reads a spool's complete lines from line from, at most limit,
// and answers the next line to read.
func appReadLines(path string, from, limit int) ([]string, int, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, from, nil
	}
	if err != nil {
		return nil, from, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	lines, n := []string{}, 0
	for len(lines) < limit {
		line, err := reader.ReadString('\n')
		if err != nil {
			// A line still being written is not a line yet.
			break
		}
		if n >= from {
			lines = append(lines, redact.Secrets(strings.TrimRight(line, "\r\n")))
		}
		n++
	}
	return lines, from + len(lines), nil
}

// ProcessOutput answers a process's output from a line on.
func (b *appBackend) ProcessOutput(id string, from int) ([]string, int, error) {
	if _, _, ok := appServiceID(id); ok {
		return b.serviceProcessOutput(id, from)
	}
	path, err := b.processLog(id)
	if err != nil {
		return nil, 0, err
	}
	// A long output is read from its last 512 KB, with a line saying so.
	trimmed := false
	if from == 0 {
		if start := appTrimmedStart(path); start > 0 {
			from, trimmed = start, true
		}
	}
	lines, next, err := appReadLines(path, from, appOutputLines)
	if err != nil {
		return nil, 0, appEngineError(err)
	}
	if trimmed {
		lines = append([]string{appTrimmedLine}, lines...)
	}
	return lines, next, nil
}

// FollowProcess starts or stops the live tail of one process's output, which
// arrives as process.output only while it is followed.
func (b *appBackend) FollowProcess(id string, follow bool) error {
	if _, _, ok := appServiceID(id); ok {
		return b.serviceFollow(id, follow)
	}
	path, err := b.processLog(id)
	if err != nil {
		return err
	}
	var stop *appFollow
	start := false
	b.locked(func() {
		current := b.follows[id]
		switch {
		case !follow && current != nil:
			stop = current
			delete(b.follows, id)
		case follow && current == nil && !b.closing:
			b.follows[id] = &appFollow{stop: make(chan struct{}), done: make(chan struct{})}
			start = true
		}
	})
	if stop != nil {
		close(stop.stop)
		<-stop.done
	}
	if start {
		var f *appFollow
		b.locked(func() { f = b.follows[id] })
		from, _ := appCountLines(path)
		guard.Go("app/follow", func() { b.follow(id, path, from, f) })
	}
	return nil
}

// appCountLines is how many complete lines a spool holds now.
func appCountLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	n := 0
	for {
		if _, err := reader.ReadString('\n'); err != nil {
			return n, nil
		}
		n++
	}
}

// follow tails one spool until it is unfollowed or the backend closes.
func (b *appBackend) follow(id, path string, from int, f *appFollow) {
	defer close(f.done)
	ticker := time.NewTicker(appFollowEvery)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
		}
		lines, next, err := appReadLines(path, from, appOutputLines)
		if err != nil || len(lines) == 0 {
			continue
		}
		from = next
		b.emit.Emit(appwire.EventProcessOutput, appwire.ProcessOutput{ID: id, Lines: lines})
	}
}

// StopProcess stops one running process.
func (b *appBackend) StopProcess(id string) error {
	if _, _, ok := appServiceID(id); ok {
		return b.serviceProcessStop(id)
	}
	agent, row, err := b.processRow(id)
	if err != nil {
		return err
	}
	job := row.ID
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	// An older engine cannot prove the run's identity or whether a committed
	// render refused its stop. Never turn that unknown into a successful Cancel.
	if served, err := b.stampedStop(live, id, job); served {
		return err
	}
	return appProcessActError(remote.ErrProcessActionsUnserved)
}

// RerunProcess runs an ended process's command again, as a new process.
func (b *appBackend) RerunProcess(id string) (appwire.Process, error) {
	if _, _, ok := appServiceID(id); ok {
		return b.StartService(id)
	}
	agent, row, err := b.processRow(id)
	if err != nil {
		return appwire.Process{}, err
	}
	job := row.ID
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.Process{}, err
	}
	// ONLY THE STAMPED DOOR RUNS ANYTHING AGAIN. An engine too old to check
	// the run's ending, folder and starter is refused, never asked by number.
	return b.stampedRerun(agent, live, id, job)
}

// ── running long ────────────────────────────────────────────────────────────

// watchLong says turn.long once if the turn is still running after the
// backend's long figure, with how many steps it has taken.
func (b *appBackend) watchLong(id string, live *appLive, turn uint64, started time.Time) *time.Timer {
	return time.AfterFunc(b.turnLong, func() {
		defer guard.Recover("app/long")
		running := false
		steps := 0
		b.locked(func() {
			running = live.running && live.turn == turn
			steps = live.steps
		})
		if !running {
			return
		}
		elapsed := int(b.now().Sub(started).Round(time.Second) / time.Second)
		live.box.Event(appwire.EventTurnLong, appwire.TurnLong{Agent: id, Turn: turn, ElapsedS: elapsed, Steps: steps})
	})
}

// MoveToTask asks the running reply to move to a task and answers AT ONCE.
// The engine moves a reply at its next step boundary, which can be a long way
// off while a call is in flight; the person's ask is taken now, the answer says
// so, and the task arrives on its own as task.changed with the move's notice.
// A reply that ends before it could move leaves a notice saying so.
func (b *appBackend) MoveToTask(agent string, turn uint64) (appwire.MoveResult, error) {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return appwire.MoveResult{}, err
	}
	a.gate.Lock()
	if err := b.workAllowed(a); err != nil {
		a.gate.Unlock()
		return appwire.MoveResult{}, err
	}
	live := b.liveOf(a)
	// A TASK THE REPLY ALREADY PROPOSED IS THE TASK IT MOVES TO. While its card
	// waits, the reply is parked on the person's answer and reaches no step at
	// which it could move, so moving it is saying yes to the card.
	var proposed *appwire.Task
	b.locked(func() {
		if live == nil {
			return
		}
		for _, task := range live.tasks {
			if task.Proposed && task.Parent == "" && task.State != appwire.TaskFailed && task.State != appwire.TaskDone && (proposed == nil || task.ID > proposed.ID) {
				found := task
				proposed = &found
			}
		}
	})
	if proposed != nil {
		// ActOnTask takes this same gate for its own admission.
		a.gate.Unlock()
		if err := b.ActOnTask(proposed.ID, "start", "", ""); err != nil {
			return appwire.MoveResult{}, err
		}
		proposed.Proposed = false
		return appwire.MoveResult{Task: proposed}, nil
	}
	defer a.gate.Unlock()
	known := map[string]bool{}
	running := false
	b.locked(func() {
		if live == nil {
			return
		}
		running = live.running && (turn == 0 || live.turn == turn)
		if turn == 0 {
			turn = live.turn
		}
		for id := range live.tasks {
			known[id] = true
		}
	})
	if !running {
		return appwire.MoveResult{}, appwire.Errorf(appwire.CodeNotFound, "that reply is not running")
	}
	if err := live.conn.Conv.MoveTurnToTask(); err != nil {
		return appwire.MoveResult{}, appEngineError(err)
	}
	moved := func() (*appwire.Task, bool) {
		var found *appwire.Task
		still := false
		b.locked(func() {
			still = live.running && live.turn == turn
			for id, task := range live.tasks {
				if !known[id] && task.Parent == "" {
					found = &task
					return
				}
			}
		})
		return found, still
	}
	// A reply between steps moves at once; the answer carries its task then.
	quick := time.Now().Add(appMoveQuick)
	for time.Now().Before(quick) {
		if task, still := moved(); task != nil {
			return appwire.MoveResult{Task: task}, nil
		} else if !still {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	guard.Go("app/move-to-task", func() {
		deadline := time.Now().Add(appMoveWait)
		for time.Now().Before(deadline) {
			task, still := moved()
			if task != nil {
				return
			}
			if !still {
				b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
					ID: "move-" + strconv.FormatUint(turn, 10), Agent: agent, Turn: turn, Kind: appwire.EntryNotice,
					At: b.now(), Text: "The reply finished before it could move to a task", Quiet: true,
				}})
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return appwire.MoveResult{Moving: true}, nil
}

// ── sleep ───────────────────────────────────────────────────────────────────

// appWakeNotices numbers the notices a wake leaves.
var appWakeNotices atomic.Uint64

// appSlept is one process stopped because the Mac went to sleep.
type appSlept struct {
	id, label string
}

// Sleep stops every running process before the Mac sleeps, and every held
// agent reads asleep until it wakes.
func (b *appBackend) Sleep() error {
	powerErr := b.factoryPower(true)
	var agents []*appAgent
	b.locked(func() {
		b.asleep = true
		for _, a := range b.agents {
			if a.live != nil {
				agents = append(agents, a)
			}
		}
	})
	for _, a := range agents {
		live := b.liveOf(a)
		if live == nil {
			continue
		}
		var id string
		b.locked(func() { id = a.view.ID })
		rows, err := b.runningJobs(live)
		if err == nil {
			for _, row := range rows {
				if _, err := live.conn.Conv.Cancel("job:" + strconv.Itoa(row.ID)); err == nil {
					slept := appSlept{id: appProcessID(id, row), label: row.Label()}
					b.locked(func() { live.slept = append(live.slept, slept) })
				}
			}
		}
		var view appwire.Agent
		b.locked(func() {
			a.view.State = appwire.StateAsleep
			view = a.view
		})
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return powerErr
}

// Wake brings every agent back, and says once per process the sleep stopped
// that it stopped, so the person can run it again.
func (b *appBackend) Wake() error {
	powerErr := b.factoryPower(false)
	var agents []*appAgent
	b.locked(func() {
		b.asleep = false
		for _, a := range b.agents {
			if a.live != nil {
				agents = append(agents, a)
			}
		}
	})
	at := b.now()
	for _, a := range agents {
		live := b.liveOf(a)
		if live == nil {
			continue
		}
		var slept []appSlept
		var view appwire.Agent
		b.locked(func() {
			slept, live.slept = live.slept, nil
			a.view.State = live.restingState()
			if live.running && a.view.State == appwire.StateIdle {
				a.view.State = appwire.StateWorking
			}
			view = a.view
		})
		for _, s := range slept {
			n := appWakeNotices.Add(1)
			live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
				ID: "w" + strconv.FormatUint(n, 10), Agent: view.ID, Kind: appwire.EntryNotice, At: at,
				Text: s.label + " stopped while this Mac slept · Rerun", Ref: s.id,
			}})
		}
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return powerErr
}

// Factory presence follows the native power door, not whether a step's chat is
// open. Never hold the local-host lock while contacting its elected service.
func (b *appBackend) factoryPower(asleep bool) error {
	b.local.mu.Lock()
	host := b.local.host
	b.local.mu.Unlock()
	if host == nil {
		return nil
	}
	return host.power(context.Background(), asleep)
}
