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
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
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
	// Depth is the row's level below the page task; direct children are zero.
	Depth int
	// Waits is the tasks this row is held behind that are not its parent: the ids
	// of its hard dependencies (feeds_into/blocks), in store order, and empty
	// when it waits on nothing but its own parent. A row still `pending` because
	// of one of these hangs under it and names it ([planWaits]).
	Waits []string
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
	// Live is the step the task is running right now — its number, the command
	// its worker asked the belt to run, and the moment the command started — read
	// off the store's live row. Its zero value is the honest answer for a task
	// that is running nothing, which the emptiness law turns into no line drawn
	// at all; the worker clears the row the moment the command ends, and every
	// ending of its loop, so a task that is not running never claims a present.
	Live plandb.LiveStep
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
// PlanTaskQuestion is one held task’s question with the row it belongs to.
// Options are the question’s own accepted keys and labels, unchanged.
type PlanTaskQuestion struct {
	TaskID  string
	Head    string
	Options []AnswerOption
}

type PlanTaskPage struct {
	Row         PlanTaskRow
	WorkModel   string
	PlanModel   string
	Questions   []PlanTaskQuestion
	Description string
	Notes       []PlanTaskNote
	Steps       []PlanStep
	// Live is the step the task is running right now — the same reading
	// [PlanTaskRow.Live] carries, lifted onto the page so a surface can draw the
	// step ONE STEP EARLY, before its end line reaches the trajectory. The zero
	// value is the honest answer for a task that is running nothing, and
	// [plandb.LiveStep.Empty] is the one question a surface asks before it draws
	// the line (the emptiness law, as the live step's own file states it).
	Live plandb.LiveStep
	// Children is the task's own children — the rows whose Parent is this task —
	// in store order, so a page can draw the tree under the task the way the
	// plan list draws it. Empty for a leaf, which is the ordinary case.
	Children []PlanTaskRow
	// WaitRows feed the page's two-way waits reading: own dependencies first,
	// then open tasks directly waiting on this task. Empty omits the section.
	WaitRows []PlanTaskRow
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
	store, plan, closeStore := a.openPlanHandle()
	if store == nil {
		return nil
	}
	defer closeStore()
	dir := filepath.Dir(store.Path())
	spend := planSpendByTask(store.Path())
	live := store.LiveSteps()
	tasks := store.Tasks(plandb.Filter{Chat: plan.chat})
	rows := make([]PlanTaskRow, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, planTaskRow(store, dir, task, spend, live))
	}
	return rows
}

// PlanTaskPage answers one task's page — its row, its description, its notes
// and its steps — for the id a surface was handed. False is the answer for a
// task this chat did not spawn, whether it is another conversation's or no
// task at all: the page is the chat's own reading of its own plan.
func (a *Agent) PlanTaskPage(id string) (PlanTaskPage, bool) {
	store, plan, closeStore := a.openPlanHandle()
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
	live := store.LiveSteps()
	// Walk admission order once; membership follows parent edges only.
	all := store.Tasks(plandb.Filter{Chat: plan.chat})
	rows := make(map[string]PlanTaskRow, len(all))
	var children []PlanTaskRow
	depths := map[string]int{task.ID: -1}
	for _, child := range all {
		row := planTaskRow(store, dir, child, spend, live)
		rows[child.ID] = row
		depth, under := depths[child.ParentID]
		if !under || child.ID == task.ID {
			continue
		}
		row.Depth = depth + 1
		children = append(children, row)
		depths[child.ID] = row.Depth
	}
	open := func(status plandb.Status) bool {
		return status != plandb.StatusDone && status != plandb.StatusFailed && status != plandb.StatusCancelled
	}
	var waitRows []PlanTaskRow
	pageRow := rows[task.ID]
	for _, id := range pageRow.Waits {
		if row, ok := rows[id]; ok && open(plandb.Status(row.Status)) {
			waitRows = append(waitRows, row)
		}
	}
	for _, candidate := range all {
		row := rows[candidate.ID]
		if candidate.ID == task.ID || !open(candidate.Status) {
			continue
		}
		for _, id := range row.Waits {
			if id == task.ID {
				waitRows = append(waitRows, row)
				break
			}
		}
	}
	workModel, _ := roles.TierModel(roles.Source(a.config.RolesSource), roles.TierWorker)
	planModel, _ := roles.TierModel(roles.Source(a.config.RolesSource), roles.TierMastermind)
	return PlanTaskPage{
		Row:         planTaskRow(store, dir, task, spend, live),
		WorkModel:   workModel,
		PlanModel:   planModel,
		Questions:   a.planTaskQuestions(),
		Description: task.Description,
		Notes:       planTaskNotes(store, task.ID),
		Steps:       planTrajectory(dir, task.ID),
		Children:    children,
		WaitRows:    waitRows,
	}, true
}

// openPlanHandle opens the run's store for one pass — a reading verb or one of
// the person's steering writes (plandb_steer.go) — under the plan gate the way
// every pulse takes it, and answers the store, its plan and the close to run. A
// nil store is a conversation with no plan, and the caller answers from that
// emptiness rather than opening one.
//
// The handle is fresh every call, for the reason every pass opens one: the
// store's memory is only as fresh as its last transaction and the worker's CLI
// is a separate process that has been writing since.
func (a *Agent) openPlanHandle() (*plandb.Store, *planState, func()) {
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

// PlanSpendLine is one seat's share of a run's spending: the role the store
// gave the work, the model that seat spent most of its money through, and what
// the seat came to over the window it was asked about.
//
// IT CARRIES THE MODEL BESIDE THE SEAT because the spend page draws the two on
// one line: a seat is a role, and the model is the thing a person can go and
// change when they read that a seat has grown dear.
type PlanSpendLine struct {
	Seat  string
	Model string
	USD   float64
	Calls int
}

// PlanSpend answers THIS conversation's plan spending rolled up by seat: one
// line per role the store gave work, carrying the model that seat spent most
// through over the dollars and calls it wrote down since a moment.
//
// NIL IS THE HONEST ANSWER for a conversation with no plan store and for a
// store with nothing priced in the window, exactly as [Agent.PlanTasks] answers
// nil for a conversation with no plan: the spend page draws its block's heading
// and whisper from that emptiness rather than a zero line, which is the
// emptiness law applied to money.
//
// THE ROLLUP IS SUMMED HERE AND NOT BY THE STORE. [plandb.Store.SpendBy]
// groups by seat but names no model beside it, and the page draws the model on
// the seat's own line, so the ledger is read the way [planSpendByTask] reads it
// — a read-only connection beside the writer, so a store that will not open as
// a reader answers nothing rather than failing the read.
func (a *Agent) PlanSpend(since time.Time) []PlanSpendLine {
	store, plan, closeStore := a.openPlanHandle()
	if store == nil {
		return nil
	}
	defer closeStore()
	return planSpendBySeat(store.Path(), plan.chat, since)
}

// planSpendBySeat sums the run's spend ledger per seat, and per model under
// each seat so the seat can name the one most of its money went through. Rows
// are narrowed to this chat's tasks by the same join [plandb.Store.spendTotals]
// makes, and the window is cut in Go, not in the query, for [plandb.Store]
// .SpendBy's reason: the ledger stores `at` as RFC3339Nano, whose fractional
// digits vary, so a text comparison against a bound would misorder a whole
// second against its own fraction.
func planSpendBySeat(path, chat string, since time.Time) []PlanSpendLine {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT s.role, s.model, s.usd, s.at
		FROM spend s JOIN tasks t ON t.id = s.task_id WHERE t.chat = ?`, chat)
	if err != nil {
		return nil
	}
	defer rows.Close()
	// A SEAT KEEPS ITS OWN RUNNING TOTAL and a tally per model, so the line's
	// model is chosen from what the seat actually spent rather than from the
	// last row read.
	type modelTally struct {
		usd   float64
		calls int
	}
	seats := map[string]*PlanSpendLine{}
	models := map[string]map[string]*modelTally{}
	var order []string
	for rows.Next() {
		var seat, model, at string
		var usd float64
		if rows.Scan(&seat, &model, &usd, &at) != nil {
			return nil
		}
		// A ZERO-PRICED ROW IS AN UNPRICED ONE AND NOT A FREE CALL. The
		// emptiness law does not let an unknown price become a measured zero, so
		// the row is left out of the rollup whole — which is also what keeps
		// `$0.00` off the page.
		if usd <= 0 {
			continue
		}
		if !since.IsZero() && at != "" {
			moment, err := time.Parse(time.RFC3339Nano, at)
			if err != nil || moment.Before(since) {
				continue
			}
		}
		line := seats[seat]
		if line == nil {
			line = &PlanSpendLine{Seat: seat}
			seats[seat] = line
			models[seat] = map[string]*modelTally{}
			order = append(order, seat)
		}
		line.USD += usd
		line.Calls++
		tally := models[seat][model]
		if tally == nil {
			tally = &modelTally{}
			models[seat][model] = tally
		}
		tally.usd += usd
		tally.calls++
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]PlanSpendLine, 0, len(order))
	for _, seat := range order {
		line := seats[seat]
		// THE MODEL IS THE ONE THE SEAT SPENT MOST THROUGH, with the call
		// count and then the name breaking a tie, so the same ledger always
		// names the same model and the row is stable across reads.
		best, bestUSD, bestCalls := "", 0.0, 0
		for model, tally := range models[seat] {
			switch {
			case tally.usd > bestUSD,
				tally.usd == bestUSD && tally.calls > bestCalls,
				tally.usd == bestUSD && tally.calls == bestCalls && model < best:
				best, bestUSD, bestCalls = model, tally.usd, tally.calls
			}
		}
		line.Model = best
		out = append(out, *line)
	}
	// DEAREST SEAT FIRST, the ordering the store's own rollup and this page's
	// tables both keep, so two seats never swap places between reads.
	sort.Slice(out, func(i, j int) bool {
		if out[i].USD != out[j].USD {
			return out[i].USD > out[j].USD
		}
		return out[i].Seat < out[j].Seat
	})
	return out
}

// planTaskRow builds one row from the store read and the two figures that are
// not on the task: the dollars its spend rows carry, already summed, and its
// steps, already read. The live step is the third: the store's live rows, read
// whole in one pass by the caller, keyed by the task's own bare id.
func planTaskRow(store *plandb.Store, dir string, task *plandb.Task, spend map[string]float64, live map[string]plandb.LiveStep) PlanTaskRow {
	seat, _ := store.RoleOf(task.ID)
	// A HELD TASK WEARS THE HOLD'S OWN WORD. Pause is status-independent in the
	// store — a held task keeps the rung it reached — while the row says what a
	// person does next, and a hold is the person's call. The surface maps the
	// word "paused" onto that state, so the row carries it rather than the rung
	// underneath it (tui3's planStateWord owns the one mapping).
	status := string(task.Status)
	if task.Paused {
		status = "paused"
	}
	row := PlanTaskRow{
		ID:             planStoreID(task.ID),
		Title:          task.Title,
		Status:         status,
		Seat:           seat,
		Steps:          len(planTrajectory(dir, task.ID)),
		USD:            spend[task.ID],
		Started:        task.CreatedAt,
		Ended:          task.CompletedAt,
		Note:           planLastNote(store, task.ID),
		TrajectoryPath: planTrajectoryPath(dir, task.ID),
		Live:           live[task.ID],
	}
	if task.ParentID != "" {
		row.Parent = planStoreID(task.ParentID)
	}
	// WAITS IS THE HARD EDGES, THE ONES THAT REALLY HOLD IT. `suggests` is advice
	// the store does not gate on, so a row kept `pending` never is because of one;
	// carrying it would name work that is not holding the task. Store order is
	// kept, so the same dependency is named on every read.
	for _, dep := range task.Dependencies {
		if dep.Kind == plandb.DepSuggests {
			continue
		}
		row.Waits = append(row.Waits, planStoreID(dep.TaskID))
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

// planTaskQuestions associates the session’s existing open questions with the
// plan rows whose live task nodes raised them. The question remains the source
// of truth for its words and accepted answer keys.
func (a *Agent) planTaskQuestions() []PlanTaskQuestion {
	g := a.graph()
	if g == nil {
		return nil
	}
	byNode := make(map[uint64]string)
	g.mu.Lock()
	for _, node := range g.nodes {
		if node.spec.planID != "" {
			byNode[node.id] = planStoreID(node.spec.planID)
		}
	}
	g.mu.Unlock()
	var out []PlanTaskQuestion
	for _, question := range a.OpenQuestions() {
		taskID := byNode[question.Subject.ID]
		if question.Subject.Kind != SubjectNode || taskID == "" {
			continue
		}
		out = append(out, PlanTaskQuestion{
			TaskID: taskID, Head: question.Head,
			Options: append([]AnswerOption(nil), question.Options...),
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
