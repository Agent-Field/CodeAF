package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── a handed-over task, as the app reads and moves it ───────────────────────
//
// A TASK THE CONVERSATION HANDED OVER IS A RUN, and a run's rows are not nodes
// in the graph (orchestrate.go says why), so the app's task list read past
// them: a running hand-over drew on the rail and answered "no such task" to a
// pause, a page and a log. They are read here off the rows the run published,
// which the graph keeps, with the run's own store behind them for what the row
// does not carry: the person's notes and when the worker had each one, the
// steps it took, and whether a person's hold is on it.
//
// A HOLD ON A RUN IS A PERSON'S, ON THE STORE, AND IT REACHES THE STEP IN HAND.
// The run's own task takes the whole-run hold ([plandb.Store.HoldRun]); one of
// its parts takes the ordinary one. Nothing new starts under a hold, and a
// worker already running waits at its next step until it is released.

// appRunRowState is a run row's state in the app's words, the same words a
// node's state is said in ([appNodeState]).
func appRunRowState(row TaskNotice) string {
	if row.Stopped {
		return "stopped"
	}
	if row.Paused && !row.State.settled() {
		return "paused"
	}
	switch row.State {
	case TaskQueued:
		return "waiting"
	case TaskUnverified:
		return "your_call"
	case TaskInterrupted:
		return "paused"
	default:
		return string(row.State)
	}
}

// appRunRowKey is the store's id for a run row's task: the plan task it was
// published for, or its own number, which is the key a run's rows are filed
// under ([Agent.sayToRunRow]).
func appRunRowKey(row TaskNotice) string {
	if key := strings.TrimSpace(row.PlanTask); key != "" {
		return planTaskID(key)
	}
	return strconv.FormatUint(row.ID, 10)
}

// appRunRows is every row the conversation's runs published, as they last
// stood.
func (a *Agent) appRunRows() []TaskNotice {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	return graph.runRowsLocked()
}

// appRunRow is one run row by its number, found where it is kept: nothing
// else is copied to find it.
func (a *Agent) appRunRow(id uint64) (TaskNotice, bool) {
	graph := a.tasker()
	if graph == nil {
		return TaskNotice{}, false
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	return graph.runRowLocked(id)
}

// appRunView is one run row as the app's task.
func (a *Agent) appRunView(row TaskNotice, cap float64) AppTaskView {
	if _, held := a.appHeldRuns.Load(row.ID); held && !row.State.settled() {
		row.Paused = true
	}
	view := AppTaskView{
		ID: strconv.FormatUint(row.ID, 10), Title: row.Title, Brief: row.Brief, State: appRunRowState(row),
		Model: row.Model, Cap: cap, Spent: row.CostUSD, Result: row.Report, Files: append([]string{}, row.Changed...),
		Elapsed: int64(row.Elapsed.Seconds()), Deadline: row.Deadline, Held: row.Paused, Version: 1, Origin: "asked",
	}
	if row.Parent != 0 {
		view.Parent = strconv.FormatUint(row.Parent, 10)
	}
	for _, id := range row.DependsOn {
		view.Waits = append(view.Waits, strconv.FormatUint(id, 10))
	}
	a.appRunDetail(&view, appRunRowKey(row))
	switch {
	case row.park != nil || a.taskRouting.isParked(row.ID):
		view.RoutingState = TaskRoutingBlocked
	case row.Crew != nil:
		view.RoutingState, view.Crew = TaskRoutingRouted, row.Crew
		if row.CrewState != nil {
			view.TaskPins = row.CrewState.TaskPins
		}
	default:
		view.RoutingState = TaskRoutingSingle
	}
	return view
}

// appRunDetail fills what the row does not carry from the run's store: the
// person's notes with the step each was read at, the worker's steps, and the
// step count.
func (a *Agent) appRunDetail(view *AppTaskView, key string) {
	stores, _, closeStores := a.openPlanReadHandles()
	defer closeStores()
	for _, store := range stores {
		task := store.Task(key)
		if task == nil {
			continue
		}
		dir := filepath.Dir(store.Path())
		steps, reads := appTrajectoryRead(filepath.Join(plandb.TaskDir(dir, key), planTrajectoryFile))
		view.WorkLog = steps
		if view.Step == 0 {
			view.Step = len(steps)
		}
		if view.Brief == "" {
			view.Brief = task.Description
		}
		for _, note := range store.Notes(key, 200) {
			if note.From != plandb.NoteFromPerson {
				continue
			}
			seen := AppTaskNote{Text: note.Body}
			if step, ok := reads[note.ID]; ok {
				seen.ReadAtStep = &step
			}
			view.Notes = append(view.Notes, seen)
		}
		return
	}
}

// appTrajectoryRead reads a task's trajectory: the command of every step, in
// order, and for every note the worker was handed, the step it had reached.
// A missing or unreadable file is no steps and no reads.
func appTrajectoryRead(path string) ([]string, map[string]int) {
	reads := map[string]int{}
	file, err := os.Open(path)
	if err != nil {
		return nil, reads
	}
	defer file.Close()
	var steps []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var line struct {
			Kind    string   `json:"kind"`
			Step    int      `json:"step"`
			Command string   `json:"command"`
			Notes   []string `json:"notes"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch line.Kind {
		case "step":
			steps = append(steps, line.Command)
		case "notes":
			for _, id := range line.Notes {
				if _, known := reads[id]; !known {
					reads[id] = len(steps)
				}
			}
		}
	}
	return steps, reads
}

// appRunAction is a person's verb on a run row.
func (a *Agent) appRunAction(row TaskNotice, intent AppTaskIntent) error {
	switch intent.Action {
	case "note", "brief":
		if strings.TrimSpace(intent.Text) == "" {
			return errors.New("words required")
		}
		_, err := a.SteerTask(row.ID, intent.Text)
		return err
	case "stop":
		if a.DropParkedTask(row.ID) {
			return nil
		}
		_, err := a.cancelTask(row.ID, "")
		return err
	case "pause", "resume":
		return a.appHoldRun(row, intent.Action == "pause")
	case "continue":
		// A run the app's quit left with nothing driving it is picked up where
		// its store stands ([Agent.ContinueRun]); one already going is fine.
		if row.State != TaskInterrupted {
			return nil
		}
		_, err := a.ContinueRun(context.Background(), row.ID)
		return err
	}
	return errors.New("that action is unavailable for a handed-over task")
}

// appHoldRun sets or releases a person's hold on a run row's task in the live
// run's store, and says the row again under its new state.
func (a *Agent) appHoldRun(row TaskNotice, pause bool) error {
	if row.State.settled() {
		return errors.New("that task has finished")
	}
	key := appRunRowKey(row)
	a.beltMu.Lock()
	run := a.beltRun
	owned := run != nil && (run.row == row.ID || containsRow(run.joined, row.ID))
	a.beltMu.Unlock()
	if !owned || run.store == nil {
		return errors.New("that task is not running here any more")
	}
	var err error
	switch {
	case key == run.store.RootID() || run.row == row.ID:
		_, err = run.store.HoldRun(pause)
	case pause:
		_, err = run.store.Pause(key)
	default:
		_, err = run.store.Resume(key)
	}
	if err != nil {
		return err
	}
	if pause {
		a.appHeldRuns.Store(row.ID, true)
	} else {
		a.appHeldRuns.Delete(row.ID)
	}
	row.Paused = pause
	if g := a.graph(); g != nil {
		a.publishRunRow(g, row)
	}
	return nil
}

func containsRow(rows []uint64, id uint64) bool {
	for _, row := range rows {
		if row == id {
			return true
		}
	}
	return false
}
