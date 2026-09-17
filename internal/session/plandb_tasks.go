package session

// The chat's read-only view of its plan (plandb_plan.go owns the store's
// wiring; this file is the reading side of it): the rows a surface draws for
// the plan this conversation seeded, and one task's page when a person opens
// it. Nothing here writes the store — the pulse, the CLI and the worker own
// every write — and nothing here runs a model, so a pane refresh costs a
// couple of reads and no calls.
//
// A ROW IS THE STORE'S READ, narrowed to the conversation. The plan store is
// read whole and filtered by the run's chat tag, so a store that holds another
// conversation's work answers only this one's; the surfaces that want the
// machine-wide picture read the store whole through the CLI, and this door is
// deliberately the narrower one.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// PlanTaskRow is one row of the chat's plan. It carries what a surface draws
// without touching the store again: the task's identity and standing, the seat
// its shape gives it (RoleOf), its parent, the money its own spend rows carry,
// the span it covered, the last thing anybody said on it, and where its record
// lives.
type PlanTaskRow struct {
	ID     string
	Title  string
	Status string
	Seat   string
	Parent string
	// Steps is the count of the task's own trajectory lines — the steps its
	// worker recorded, which is what the row's "14 steps" counts.
	Steps int
	// USD is the sum of the task's spend rows: what this piece of the plan has
	// cost so far.
	USD float64
	// Started is when the task was created and Ended when it completed; a task
	// still open carries the zero Ended.
	Started time.Time
	Ended   time.Time
	// Note is the text of the task's last note, empty when nobody has left one.
	Note string
	// TrajectoryPath is the file the task's steps are recorded in, for a reader
	// that wants the record itself and not only its length.
	TrajectoryPath string
}

// PlanTaskNote is one note on a task's page: what was said, who said it, and
// when. The author is the worker's agent name, empty for a person's note,
// which [PlanTaskNote.Person] marks so a surface can draw the two voices
// apart.
type PlanTaskNote struct {
	Author string
	Person bool
	Body   string
	At     time.Time
}

// PlanTaskPage is everything a person reads when they open one task: its row,
// the description that is its work order, every note left on it, and the steps
// its worker recorded.
type PlanTaskPage struct {
	Row         PlanTaskRow
	Description string
	Notes       []PlanTaskNote
	Steps       []PlanStep
}

// PlanStep is one line of a task's trajectory — one command the worker ran and
// the head of what came back. IT MIRRORS internal/run's Step, which is where
// the record is written: this package cannot import that one, because it
// imports this one, so the step line is decoded here from the same JSON shape.
// The two must move together, and the run engine's own file is the definition.
type PlanStep struct {
	Kind        string   `json:"kind"`
	Step        int      `json:"step"`
	Command     string   `json:"command"`
	Observation string   `json:"observation,omitempty"`
	FullOutput  string   `json:"full_output,omitempty"`
	Writes      []string `json:"writes,omitempty"`
	Children    []string `json:"children,omitempty"`
}

// planTrajectoryFile is the file a task's steps are appended to, under the
// task's own record folder (plandb.TaskDir). It is the same name the run engine
// writes ([internal/run]'s trajectoryName) and is spelled here because that
// package cannot be imported back; the two must stay one name.
const planTrajectoryFile = "trajectory.jsonl"

// PlanTasks answers the plan this conversation seeded, as rows ready to draw,
// in the store's own admission order. Nil is the honest answer for a
// conversation with no plan — the experiment's switch is off, or no store was
// ever seeded — and an empty slice (not nil) is a plan that holds only other
// chats' work: the store is there and this chat's part of it is not.
func (a *Agent) PlanTasks() []PlanTaskRow {
	store, plan, closeStore := a.openPlanForRead()
	if store == nil {
		return nil
	}
	defer closeStore()
	dir := filepath.Dir(store.Path())
	spend := planSpendByTask(store.Path())
	tasks := store.Tasks(plandb.Filter{Chat: plan.chat})
	rows := make([]PlanTaskRow, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, planTaskRow(store, dir, task, spend))
	}
	return rows
}

// PlanTaskPage answers one task's page — its row, its description, its notes
// and its steps — for the id a surface was handed. False is the answer for a
// task this chat did not spawn, whether it is another conversation's or no
// task at all: the page is the chat's own reading of its own plan.
func (a *Agent) PlanTaskPage(id string) (PlanTaskPage, bool) {
	store, plan, closeStore := a.openPlanForRead()
	if store == nil {
		return PlanTaskPage{}, false
	}
	defer closeStore()
	task := store.Task(planTaskID(id))
	if task == nil || task.Chat != plan.chat {
		return PlanTaskPage{}, false
	}
	dir := filepath.Dir(store.Path())
	spend := planSpendByTask(store.Path())
	return PlanTaskPage{
		Row:         planTaskRow(store, dir, task, spend),
		Description: task.Description,
		Notes:       planTaskNotes(store, task.ID),
		Steps:       planTrajectory(dir, task.ID),
	}, true
}

// openPlanForRead opens the run's store for a reading verb, under the plan
// gate the way every pulse takes it, and answers the store, its plan and the
// close to run. A nil store is a conversation with no plan to read, and the
// caller answers from that emptiness rather than opening one.
//
// The handle is fresh every call, for the reason every pass opens one: the
// store's memory is only as fresh as its last transaction and the worker's CLI
// is a separate process that has been writing since.
func (a *Agent) openPlanForRead() (*plandb.Store, *planState, func()) {
	g := a.graph()
	if g == nil {
		return nil, nil, func() {}
	}
	plan := g.planIfArmed()
	if plan == nil {
		return nil, nil, func() {}
	}
	plan.mu.Lock()
	store := plan.open()
	if store == nil {
		plan.mu.Unlock()
		return nil, nil, func() {}
	}
	return store, plan, func() {
		_ = store.Close()
		plan.mu.Unlock()
	}
}

// planTaskRow builds one row from the store read and the two figures that are
// not on the task: the dollars its spend rows carry, already summed, and its
// steps, already read.
func planTaskRow(store *plandb.Store, dir string, task *plandb.Task, spend map[string]float64) PlanTaskRow {
	seat, _ := store.RoleOf(task.ID)
	row := PlanTaskRow{
		ID:             planStoreID(task.ID),
		Title:          task.Title,
		Status:         string(task.Status),
		Seat:           seat,
		Steps:          len(planTrajectory(dir, task.ID)),
		USD:            spend[task.ID],
		Started:        task.CreatedAt,
		Ended:          task.CompletedAt,
		Note:           planLastNote(store, task.ID),
		TrajectoryPath: planTrajectoryPath(dir, task.ID),
	}
	if task.ParentID != "" {
		row.Parent = planStoreID(task.ParentID)
	}
	return row
}

// planLastNote answers the text of the newest note on a task, empty when there
// is none. Notes come back oldest first, so the last one is the newest.
func planLastNote(store *plandb.Store, taskID string) string {
	notes := store.Notes(taskID, 0)
	if len(notes) == 0 {
		return ""
	}
	return notes[len(notes)-1].Body
}

// planTaskNotes answers every note on a task, oldest first, each with its
// author and moment. The store bounds the count; a page that outgrows the
// bound shows the notes it keeps.
func planTaskNotes(store *plandb.Store, taskID string) []PlanTaskNote {
	notes := store.Notes(taskID, 200)
	out := make([]PlanTaskNote, 0, len(notes))
	for _, note := range notes {
		out = append(out, PlanTaskNote{
			Author: note.Agent,
			Person: note.From == plandb.NoteFromPerson,
			Body:   note.Body,
			At:     note.At,
		})
	}
	return out
}

// planTaskID names a task the way the store does, from either spelling: a
// surface hands back the `t-` id it was given in a row, and the store reads
// its bare ids.
func planTaskID(id string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "t-"))
}

// planTrajectoryPath is where one task's steps are recorded: the trajectory
// file under the task's own record folder beside the store.
func planTrajectoryPath(dir, id string) string {
	return filepath.Join(plandb.TaskDir(dir, id), planTrajectoryFile)
}

// planTrajectory reads one task's recorded steps back, in the order they were
// appended. A task that has never run has no file and answers no steps; a line
// that will not parse, or that is the run's ending line rather than a step, is
// skipped — the record is about the steps, and an interrupted append leaves a
// half-written last line behind.
func planTrajectory(dir, id string) []PlanStep {
	data, err := os.ReadFile(planTrajectoryPath(dir, id))
	if err != nil {
		return nil
	}
	var steps []PlanStep
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var step PlanStep
		if json.Unmarshal([]byte(line), &step) != nil || step.Kind != "step" {
			continue
		}
		steps = append(steps, step)
	}
	return steps
}

// planSpendByTask sums the run's spend ledger per task — the dollars each
// task's own rows carry, which is the figure a row shows. The store's own
// rollups are per project and per chat; a task's total is not among them, so
// the ledger is summed here. THE READ IS ITS OWN READ-ONLY CONNECTION, so it
// never races the handle the pulse writes through — WAL lets a reader run
// beside a writer, and a store that will not open as a reader answers no
// figures rather than failing the read.
func planSpendByTask(path string) map[string]float64 {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT task_id, SUM(usd) FROM spend GROUP BY task_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	totals := map[string]float64{}
	for rows.Next() {
		var id string
		var usd float64
		if rows.Scan(&id, &usd) != nil {
			return totals
		}
		totals[id] = usd
	}
	return totals
}
