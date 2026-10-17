package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// workBackend is a fake backend with one open agent whose task lane the test
// can speak on.
func workBackend(t *testing.T, conv *fakeConversation) (*appBackend, *appRecorder, appwire.Agent) {
	t.Helper()
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return backend, rec, agent
}

// lane puts one event on the fake's task lane.
func (f *fakeConversation) lane(event session.Event) {
	f.mu.Lock()
	lane := f.tasks
	f.mu.Unlock()
	lane <- event
}

func taskChanged(rec *appRecorder, t *testing.T, match func(appwire.Task) bool) appwire.Task {
	t.Helper()
	i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.TaskChanged)
		return ok && match(changed.Task)
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.data[i].(appwire.TaskChanged).Task
}

// A TASK SAYS WHAT ITS CARD AND ITS ROW DRAW. A proposal is proposed with its
// clock, an irreversible one has none, a running task carries the terminal's
// live line and its phase, and a finished one waiting on the person is your
// call; every id names its agent.
func TestAppTasksCarryTheCardAndTheLiveLine(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	deadline := time.Now().Add(12 * time.Second).Round(time.Second)

	conv.lane(session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 4, Title: "add a cache", Deadline: deadline}})
	proposed := taskChanged(rec, t, func(task appwire.Task) bool { return task.Title == "add a cache" })
	if proposed.ID != agent.ID+":4" || !proposed.Proposed || proposed.ClockUntil == nil || !proposed.ClockUntil.Equal(deadline) || proposed.State != appwire.TaskQueued {
		t.Fatalf("the proposal = %+v", proposed)
	}
	conv.lane(session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 5, Title: "drop the table", Irreversible: true}})
	final := taskChanged(rec, t, func(task appwire.Task) bool { return task.Title == "drop the table" })
	if !final.Irreversible || final.ClockUntil != nil || !final.Proposed {
		t.Fatalf("an irreversible proposal = %+v", final)
	}

	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 4, Title: "add a cache", State: session.TaskRunning, Waiting: "waiting for a slot", CostUSD: 0.25}})
	running := taskChanged(rec, t, func(task appwire.Task) bool { return task.ID == agent.ID+":4" && task.State == appwire.TaskRunning })
	if running.Proposed || running.Line != "waiting for a slot" || running.SpentUSD == nil || *running.SpentUSD != 0.25 {
		t.Fatalf("the running task = %+v", running)
	}
	conv.lane(session.Event{Kind: session.EventTaskPhase, TaskPhase: &session.TaskPhaseNotice{ID: 4, Phase: session.TaskPhaseRepairing, Round: 1, Rounds: 2}})
	checked := taskChanged(rec, t, func(task appwire.Task) bool { return strings.HasPrefix(task.Line, "closing gaps") })
	if checked.Line != "closing gaps · round 1 of 2" {
		t.Fatalf("the phase line = %q", checked.Line)
	}
	// Plain working says the call the task is making, as the terminal's row
	// does, and nothing once the call ends.
	conv.lane(session.Event{Kind: session.EventTaskPhase, TaskPhase: &session.TaskPhaseNotice{ID: 4, Phase: session.TaskPhaseWorking}})
	room := func() chan session.Event {
		for i := 0; i < 200; i++ {
			conv.mu.Lock()
			room := conv.room
			conv.mu.Unlock()
			if room != nil {
				return room
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("the running task's calls were never watched")
		return nil
	}()
	room <- session.Event{Kind: session.EventToolBegin, Tool: "write", Args: `{"path":"cache.go"}`}
	calling := taskChanged(rec, t, func(task appwire.Task) bool { return strings.Contains(task.Line, "cache.go") })
	if calling.State != appwire.TaskRunning {
		t.Fatalf("the task making a call = %+v", calling)
	}
	room <- session.Event{Kind: session.EventToolEnd, Tool: "write"}
	taskChanged(rec, t, func(task appwire.Task) bool {
		return task.ID == agent.ID+":4" && task.State == appwire.TaskRunning && task.Line == ""
	})
	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 4, State: session.TaskUnverified, Result: "cached the reads", Changed: []string{"cache.go"}}})
	call := taskChanged(rec, t, func(task appwire.Task) bool { return task.State == appwire.TaskUnverified })
	if !call.YourCall || call.Line != "" || call.Result != "cached the reads" || len(call.Files) != 1 || call.Files[0].Path != "cache.go" || call.Title != "add a cache" {
		t.Fatalf("the task waiting on you = %+v", call)
	}
	if err := backend.ActOnTask("nonsense", "start", "", ""); err == nil {
		t.Fatal("an id that names no agent was taken")
	}
}

// EVERY CARD VERB GOES THROUGH THE ENGINE'S ONE TASK DOOR with the agent's own
// number: typing holds the clock as hold does, and not_right carries its words.
func TestAppTaskVerbsReachTheTaskDoor(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{ID: "4", Title: "add a cache", State: "your_call", Brief: "cache the reads", Result: "done it", Files: []string{"cache.go"}, WorkLog: []string{"read cache.go", "edit cache.go"}, Notes: []session.AppTaskNote{{Text: "keep it small"}}, Cap: 5, Spent: 1.5}}}
	backend, _, agent := workBackend(t, conv)
	id := agent.ID + ":4"
	for _, act := range [][4]string{
		{"start", "", ""}, {"hold", "", ""}, {"typing", "", ""}, {"decline", "", ""}, {"stop", "", ""},
		{"note", "", "use the LRU"}, {"decide", appwire.VerdictNotRight, "it misses the writes"}, {"decide", appwire.VerdictAccept, ""},
	} {
		if err := backend.ActOnTask(id, act[0], act[1], act[2]); err != nil {
			t.Fatalf("%v: %v", act, err)
		}
	}
	conv.mu.Lock()
	intents := append([]session.AppTaskIntent(nil), conv.intents...)
	conv.mu.Unlock()
	want := []string{"start", "hold", "hold", "decline", "stop", "note", "decide", "decide"}
	if len(intents) != len(want) {
		t.Fatalf("intents = %+v", intents)
	}
	for i, intent := range intents {
		if intent.ID != "4" || intent.Action != want[i] {
			t.Fatalf("intent %d = %+v, want %s on 4", i, intent, want[i])
		}
	}
	if intents[5].Text != "use the LRU" || intents[6].Verb != "not_right" || intents[6].Text != "it misses the writes" || intents[7].Verb != "accept" {
		t.Fatalf("the words did not travel: %+v", intents[5:])
	}

	page, err := backend.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if page.Task.ID != id || !page.Task.YourCall || page.Brief != "cache the reads" || page.Result != "done it" || len(page.Log) != 2 || len(page.Notes) != 1 || page.Notes[0].Text != "keep it small" ||
		page.Task.BudgetUSD == nil || *page.Task.BudgetUSD != 5 || len(page.Files) != 1 {
		t.Fatalf("the page = %+v", page)
	}
	if _, err := backend.GetTask(agent.ID + ":9"); err == nil {
		t.Fatal("a task that is not there was answered")
	}
}

// A QUICK TASK IS SAID AS A QUICK TASK, with each file's lines. Its undo
// refuses a file changed since, naming it, and the quick task reads conflict;
// once the file is put back the undo goes through and it reads undone.
func TestAppQuickTasksAndUndo(t *testing.T) {
	conv := &fakeConversation{quickTasks: []session.AppQuickTaskView{{ID: "7", Title: "rename the flag", State: "done", Files: []session.AppQuickFile{{Path: "main.go", Added: 3, Removed: 1}}}}, undoChanged: []string{"main.go"}}
	backend, rec, agent := workBackend(t, conv)
	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Kind: session.TaskKindQuick, Title: "rename the flag", State: session.TaskDone}})
	i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool { _, ok := data.(appwire.QuickChangedEvent); return ok })
	rec.mu.Lock()
	quick := rec.data[i].(appwire.QuickChangedEvent).Quick
	rec.mu.Unlock()
	if quick.ID != agent.ID+":7" || quick.State != appwire.QuickDone || len(quick.Files) != 1 || quick.Files[0].Add != 3 || quick.Files[0].Del != 1 {
		t.Fatalf("the quick task = %+v", quick)
	}
	rec.mu.Lock()
	for _, event := range rec.events {
		if event == appwire.EventTaskChanged {
			t.Fatal("a quick task was also said as a task")
		}
	}
	rec.mu.Unlock()

	err := backend.UndoQuick(quick.ID)
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict || len(wire.Changed) != 1 || wire.Changed[0] != "main.go" {
		t.Fatalf("a conflicted undo = %v", err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.QuickChangedEvent)
		return ok && changed.Quick.State == appwire.QuickConflict && changed.Quick.Conflict != nil && changed.Quick.Conflict.Changed[0] == "main.go"
	})
	conv.mu.Lock()
	conv.undoChanged = nil
	conv.mu.Unlock()
	if err := backend.UndoQuick(quick.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.QuickChangedEvent)
		return ok && changed.Quick.State == appwire.QuickUndone && changed.Quick.Conflict == nil
	})
}

// A PROCESS IS SAID AS IT MOVES, its output is read from its spool by line and
// followed live only while followed, stop and rerun go through the engine,
// and a sleep stops what is running and says so once on wake.
func TestAppProcessesOutputFollowStopAndSleep(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "job-3.log")
	if err := os.WriteFile(spool, []byte("checking 10 links\nok /docs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := session.JobNotice{ID: 3, Name: "link checker", Command: "npx linkinator .", State: session.JobRunning, LogPath: spool, Started: time.Now()}
	conv := &fakeConversation{jobs: []session.JobNotice{job}}
	backend, rec, agent := workBackend(t, conv)
	conv.lane(session.Event{Kind: session.EventJobUpdate, Job: &job})
	i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool { _, ok := data.(appwire.ProcessChanged); return ok })
	rec.mu.Lock()
	process := rec.data[i].(appwire.ProcessChanged).Process
	rec.mu.Unlock()
	id := appProcessID(agent.ID, job)
	if process.ID != id || process.Title != "link checker" || process.State != appwire.ProcessRunning || process.LastLine != "ok /docs" || process.ExitCode != nil {
		t.Fatalf("the process = %+v", process)
	}
	list, err := backend.ListProcesses(agent.ID)
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("processes.list = %+v, %v", list, err)
	}
	lines, next, err := backend.ProcessOutput(id, 1)
	if err != nil || len(lines) != 1 || lines[0] != "ok /docs" || next != 2 {
		t.Fatalf("output from line 1 = %q, %d, %v", lines, next, err)
	}

	if err := backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	appendLine := func(line string) {
		f, err := os.OpenFile(spool, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	appendLine("broken /old")
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		out, ok := data.(appwire.ProcessOutput)
		return ok && out.ID == id && len(out.Lines) == 1 && out.Lines[0] == "broken /old"
	})
	if err := backend.FollowProcess(id, false); err != nil {
		t.Fatal(err)
	}

	if err := backend.StopProcess(id); err != nil {
		t.Fatal(err)
	}
	// AN ENGINE WITHOUT THE STAMPED DOOR IS NEVER ASKED TO RUN ANYTHING AGAIN
	// (appprocessactions.go): it cannot check the run's ending or folder.
	if _, err := backend.RerunProcess(id); !isCode(err, appwire.CodeUnknownMethod) {
		t.Fatalf("rerun on an engine without the stamped door = %v", err)
	}

	if err := backend.Sleep(); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.State == appwire.StateAsleep
	})
	conv.mu.Lock()
	cancels := append([]string(nil), conv.cancels...)
	conv.mu.Unlock()
	if len(cancels) != 2 || cancels[0] != "job:3" || cancels[1] != "job:3" {
		t.Fatalf("stops = %q", cancels)
	}
	if err := backend.Wake(); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryNotice && added.Entry.Text == "link checker stopped while this Mac slept · Rerun" && added.Entry.Ref == id
	})
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.State == appwire.StateIdle
	})
}

// A REPLY THAT RUNS LONG SAYS SO ONCE, with its steps, and stays a reply until
// the person moves it; the move answers the task it became.
func TestAppTurnLongAndMoveToTask(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "read"},
		{Kind: session.EventToolEnd, CallID: "c1", Tool: "read"},
	}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	backend.turnLong = 50 * time.Millisecond
	turn, err := backend.Send(agent.ID, "refactor everything", "c-9")
	if err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool { _, ok := data.(appwire.TurnLong); return ok })
	rec.mu.Lock()
	long := rec.data[i].(appwire.TurnLong)
	rec.mu.Unlock()
	if long.Agent != agent.ID || long.Turn != turn || long.Steps != 1 {
		t.Fatalf("turn.long = %+v", long)
	}

	// THE MOVE ANSWERS AT ONCE, while the step in flight is still running, and
	// the task arrives on its own when the reply moves.
	answered, err := backend.MoveToTask(agent.ID, turn)
	if err != nil || !answered.Moving || answered.Task != nil {
		t.Fatalf("the move answered %+v, %v", answered, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		conv.mu.Lock()
		asked := conv.moves
		conv.mu.Unlock()
		if asked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the move never reached the engine")
		}
		time.Sleep(5 * time.Millisecond)
	}
	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 11, Title: "refactor everything", State: session.TaskRunning}})
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.TaskChanged)
		return ok && changed.Task.ID == agent.ID+":11"
	})
	close(hold)
	rec.waitFor(t, 2*time.Second, appTurnDone)
	if _, err := backend.MoveToTask(agent.ID, turn); err == nil {
		t.Fatal("a finished reply was moved")
	}

	entries, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	_ = entries
	var marks map[int]appTurnMark
	a, _ := backend.find(agent.ID)
	backend.locked(func() { marks = a.marks })
	if mark := marks[0]; mark.turn != turn || mark.clientID != "c-9" {
		t.Fatalf("the message's mark = %+v", mark)
	}
	listed := appEntriesOf(agent.ID, []session.DisplayEntry{{Role: "user", Text: "refactor everything"}, {Role: "assistant", Text: "on it"}}, time.Now(), marks)
	if listed[0].Turn != turn || listed[0].ClientID != "c-9" || listed[1].Turn != turn || listed[1].ClientID != "" {
		t.Fatalf("listed entries = %+v", listed)
	}
}

// A CONVERSATION THE APP IS NOT HOLDING STILL LISTS ITS TASKS, finished ones
// included, read off its own folder's tasks.json: a terminal conversation the
// app has never opened has every task it ever ran.
func TestAppTasksListReadsAConversationsOwnTaskFile(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, filepath.Join(root, "profile"))
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tasks := `{"type":"tasks","version":1,"seq":4,"nodes":[
{"id":1,"title":"fix the parser","brief":"b","acceptance":"a","state":"done","report":"fixed it","changed":["parser.go"],"costUsd":0.4},
{"id":2,"title":"add tests","brief":"b","acceptance":"a","state":"running","interrupted":true,"parent":1},
{"id":3,"title":"lint it","brief":"b","acceptance":"a","state":"unverified"},
{"id":4,"title":"rename","brief":"b","acceptance":"a","state":"done","kind":"quick"}]}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(transcript), "tasks.json"), []byte(tasks), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	id := appAgentID(transcript)
	list, err := backend.ListTasks(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("tasks = %+v", list)
	}
	byTitle := map[string]appwire.Task{}
	for _, task := range list {
		byTitle[task.Title] = task
	}
	if done := byTitle["fix the parser"]; done.ID != id+":1" || done.State != appwire.TaskDone || done.Result != "fixed it" || len(done.Files) != 1 || done.SpentUSD == nil {
		t.Fatalf("the finished task = %+v", done)
	}
	if left := byTitle["add tests"]; left.State != appwire.TaskFailed || left.Result != "Interrupted" || left.Parent != id+":1" {
		t.Fatalf("the interrupted task = %+v", left)
	}
	if call := byTitle["lint it"]; !call.YourCall {
		t.Fatalf("the task waiting on you = %+v", call)
	}
	if backend.liveOf(backend.agent(id)) != nil {
		t.Fatal("listing tasks opened the conversation")
	}
}

// MOVING A REPLY WHOSE TASK IS ALREADY PROPOSED STARTS THAT TASK. The reply is
// parked on the card, so it reaches no step to move at; the move answers the
// card with yes and answers with the task.
func TestAppMoveToTaskStartsAPendingProposal(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	conv.lane(session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 4, Title: "add a cache", Deadline: time.Now().Add(time.Minute)}})
	taskChanged(rec, t, func(task appwire.Task) bool { return task.Title == "add a cache" && task.Proposed })
	moved, err := backend.MoveToTask(agent.ID, 0)
	if err != nil || moved.Task == nil || moved.Task.ID != agent.ID+":4" || moved.Moving {
		t.Fatalf("the move answered %+v, %v", moved, err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if len(conv.intents) != 1 || conv.intents[0].Action != "start" || conv.intents[0].ID != "4" {
		t.Fatalf("the card was answered with %+v", conv.intents)
	}
}
