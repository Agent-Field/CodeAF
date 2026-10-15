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
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// appTurnLong is how long a reply runs before the app is told it is running
// long and may be moved to a task. `codeaf app --turn-long` changes it.
const appTurnLong = 180 * time.Second

// appMoveWait bounds how long agents.move_to_task waits for the reply's task:
// the move happens at the reply's next step and writes a brief first.
const appMoveWait = 2 * time.Minute

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

// appProcessID is a process's id on the wire.
func appProcessID(agent string, job int) string { return agent + ":p" + strconv.Itoa(job) }

// appSplitID is an id's agent and the agent's own number for the thing.
func appSplitID(id string) (agent, local string, ok bool) {
	i := strings.LastIndex(id, ":")
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

// appProcessJob is a process id's agent and job number.
func appProcessJob(id string) (string, int, bool) {
	agent, local, ok := appSplitID(id)
	if !ok || !strings.HasPrefix(local, "p") {
		return "", 0, false
	}
	job, err := strconv.Atoi(local[1:])
	return agent, job, err == nil && job > 0
}

// held answers the held conversation for an agent id, opening it if none is.
func (b *appBackend) heldLive(agent string) (*appAgent, *appLive, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, nil, err
	}
	view, opened, err := b.ensure(a)
	if err != nil {
		return nil, nil, err
	}
	if opened {
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
	raw := string(notice.State)
	if strings.TrimSpace(raw) == "" {
		raw = string(session.TaskQueued)
	}
	state := appTaskState(raw)
	if state == "" {
		return appwire.Task{}, false
	}
	task := appwire.Task{
		ID: appTaskID(agent, notice.ID), Agent: agent, Title: notice.Title, State: state, Group: notice.Run, UpdatedAt: now,
		Irreversible: notice.Irreversible, YourCall: state == appwire.TaskUnverified,
		Result: redact.Secrets(strings.TrimSpace(notice.Result)), Files: appFiles(notice.Changed), SpentUSD: appFigure(notice.CostUSD),
	}
	if notice.Parent != 0 && notice.Parent != notice.ID {
		task.Parent = appTaskID(agent, notice.Parent)
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
	return task, true
}

// appTaskOfView is one task as the engine's app read describes it.
func appTaskOfView(agent string, view session.AppTaskView) appwire.Task {
	n, _ := strconv.ParseUint(view.ID, 10, 64)
	id := view.ID
	if n != 0 {
		id = appTaskID(agent, n)
	}
	task := appwire.Task{ID: id, Agent: agent, Title: view.Title, Result: redact.Secrets(view.Result), Files: appFiles(view.Files), BudgetUSD: appFigure(view.Cap), SpentUSD: appFigure(view.Spent)}
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
		if task.Parent == "" {
			task.Parent = before.Parent
		}
		if task.Group == "" {
			task.Group = before.Group
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
// A conversation joined in a window's host has none, and its tasks' lines are
// what the engine says about them.
type appTaskRoom interface {
	WatchTaskRoom(id uint64) (<-chan session.Event, func(), error)
}

// flyPilot watches one running task's calls, once, so its line can say the
// call it is making: `write essay3.md`, `bash go test ./...`.
func (b *appBackend) flyPilot(live *appLive, key string, n uint64) {
	room, ok := live.conn.Conv.(appTaskRoom)
	if !ok {
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
	lane, stop, err := room.WatchTaskRoom(n)
	if err != nil {
		b.locked(func() { delete(live.pilots, key) })
		return
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
	page := appwire.TaskPage{Task: task, Result: task.Result, Files: task.Files, Brief: view.Brief}
	at := b.now()
	for i, line := range view.WorkLog {
		if line = strings.TrimSpace(line); line != "" {
			page.Log = append(page.Log, appwire.Entry{ID: task.ID + ".l" + strconv.Itoa(i), Agent: agent, Kind: appwire.EntryNotice, At: at, Text: redact.Secrets(line), Quiet: true})
		}
	}
	for i, note := range view.Notes {
		page.Notes = append(page.Notes, appwire.TaskNote{ID: task.ID + ".n" + strconv.Itoa(i), Text: note.Text, At: at, ReadAtStep: note.ReadAtStep})
	}
	return page, nil
}

// ActOnTask is every verb on a task, through the engine's one task door.
func (b *appBackend) ActOnTask(id, action, verdict, text string) error {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	intent := session.AppTaskIntent{ID: local, Action: action, Text: strings.TrimSpace(text), Verb: verdict}
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
	}
	if err := live.conn.Conv.AppTaskAction(intent); err != nil {
		return appTaskError(err)
	}
	return nil
}

// appTaskError sorts an engine refusal about a task into the app's codes.
func appTaskError(err error) error {
	words := err.Error()
	switch {
	case strings.Contains(words, "no such task"), strings.Contains(words, "no such quick task"):
		return appwire.Errorf(appwire.CodeNotFound, "%s", plainWords(words))
	case strings.Contains(words, "stale"), strings.Contains(words, "not waiting"), strings.Contains(words, "finished"), strings.Contains(words, "unknown"), strings.Contains(words, "must be"):
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
	case "grew":
		quick.State = appwire.QuickGrew
		quick.Grew = &appwire.QuickGrowth{Files: len(view.Files), Reason: view.Grew}
	default:
		quick.State = appwire.QuickDone
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
	_, live, err := b.heldLive(agent)
	if err != nil {
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
	process := appwire.Process{
		ID: appProcessID(agent, job.ID), Agent: agent, Title: redact.Secrets(job.Label()), Command: redact.Secrets(job.Command),
		StartedAt: job.Started, LastLine: redact.Secrets(appLastLine(job.LogPath)),
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
		if !job.Started.IsZero() {
			ended := job.Started.Add(job.Elapsed)
			process.EndedAt = &ended
		}
		if process.State != appwire.ProcessStopped {
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
	process := appProcessOf(agent, *job)
	same := false
	b.locked(func() {
		before, known := live.processes[process.ID]
		live.processes[process.ID] = process
		same = known && reflect.DeepEqual(before, process)
		live.logs[process.ID] = job.LogPath
	})
	if !same {
		live.box.Event(appwire.EventProcessChanged, appwire.ProcessChanged{Process: process})
	}
}

// processRows is an agent's processes: the live registry's while its
// conversation is held, its record on disk otherwise.
func (b *appBackend) processRows(agent string) ([]session.JobNotice, *appLive, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, nil, err
	}
	var transcript string
	live := b.liveOf(a)
	b.locked(func() { transcript = a.transcript })
	if live != nil {
		rows, err := live.conn.Conv.JobNotices()
		if err != nil {
			return nil, nil, appEngineError(err)
		}
		return rows, live, nil
	}
	rows, err := session.ReadProcessNotices(transcript)
	if err != nil {
		return nil, nil, appEngineError(err)
	}
	return rows, nil, nil
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
	return out, nil
}

// processLog is where one process's output is spooled.
func (b *appBackend) processLog(id string) (string, error) {
	agent, job, ok := appProcessJob(id)
	if !ok {
		return "", appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	rows, _, err := b.processRows(agent)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.ID == job {
			return row.LogPath, nil
		}
	}
	return "", appwire.Errorf(appwire.CodeNotFound, "there is no process %s", id)
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
	path, err := b.processLog(id)
	if err != nil {
		return nil, 0, err
	}
	lines, next, err := appReadLines(path, from, appOutputLines)
	if err != nil {
		return nil, 0, appEngineError(err)
	}
	return lines, next, nil
}

// FollowProcess starts or stops the live tail of one process's output, which
// arrives as process.output only while it is followed.
func (b *appBackend) FollowProcess(id string, follow bool) error {
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
	agent, job, ok := appProcessJob(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	if _, err := live.conn.Conv.Cancel("job:" + strconv.Itoa(job)); err != nil {
		return appEngineError(err)
	}
	return nil
}

// RerunProcess runs an ended process's command again, as a new process.
func (b *appBackend) RerunProcess(id string) (appwire.Process, error) {
	agent, job, ok := appProcessJob(id)
	if !ok {
		return appwire.Process{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.Process{}, err
	}
	row, err := live.conn.Conv.RerunProcess(job)
	if err != nil {
		return appwire.Process{}, appEngineError(err)
	}
	return appProcessOf(agent, row), nil
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

// MoveToTask moves the running reply to a task at its next step and answers
// that task.
func (b *appBackend) MoveToTask(agent string, turn uint64) (appwire.Task, error) {
	a, err := b.find(agent)
	if err != nil {
		return appwire.Task{}, err
	}
	live := b.liveOf(a)
	known := map[string]bool{}
	running := false
	b.locked(func() {
		if live == nil {
			return
		}
		running = live.running && (turn == 0 || live.turn == turn)
		for id := range live.tasks {
			known[id] = true
		}
	})
	if !running {
		return appwire.Task{}, appwire.Errorf(appwire.CodeNotFound, "that reply is not running")
	}
	if err := live.conn.Conv.MoveTurnToTask(); err != nil {
		return appwire.Task{}, appEngineError(err)
	}
	deadline := time.Now().Add(appMoveWait)
	for {
		var found *appwire.Task
		still := false
		b.locked(func() {
			still = live.running && (turn == 0 || live.turn == turn)
			for id, task := range live.tasks {
				if !known[id] && task.Parent == "" {
					found = &task
					return
				}
			}
		})
		if found != nil {
			return *found, nil
		}
		if !still {
			return appwire.Task{}, appwire.Errorf(appwire.CodeNotFound, "the reply finished before it could move to a task")
		}
		if time.Now().After(deadline) {
			return appwire.Task{}, appwire.Errorf(appwire.CodeEngine, "the reply has not moved to a task yet; it moves at its next step")
		}
		time.Sleep(100 * time.Millisecond)
	}
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
		rows, err := live.conn.Conv.JobNotices()
		if err == nil {
			for _, row := range rows {
				if row.State != session.JobRunning || row.Kind == session.JobKindTask {
					continue
				}
				if _, err := live.conn.Conv.Cancel("job:" + strconv.Itoa(row.ID)); err == nil {
					slept := appSlept{id: appProcessID(id, row.ID), label: row.Label()}
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
	return nil
}

// Wake brings every agent back, and says once per process the sleep stopped
// that it stopped, so the person can run it again.
func (b *appBackend) Wake() error {
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
	return nil
}
