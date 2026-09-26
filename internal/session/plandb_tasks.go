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

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// PlanTaskRow is one row of the chat's plan. It carries what a surface draws
// without touching the store again: the task's identity and standing, the seat
// its shape gives it (RoleOf), its parent, the money its own spend rows carry,
// the span it covered, the last thing anybody said on it, and where its record
// lives.
type PlanTaskRow struct {
	// Done, Running, Queued, Failed and Total summarize every task below a run root.
	// They stay zero on ordinary task rows. Claimed work is running; pending and
	// ready work is queued. Total includes every descendant store row.
	Done    int
	Running int
	Queued  int
	Failed  int
	Total   int
	ID      string
	Title   string
	Status  string
	// Hold is why a ready part or an unstarted root has no worker yet. It
	// crosses the remote wire only while admission refused this row's start.
	Hold string `json:",omitempty"`
	// Stopped is true only when this task's own ending records a person's stop.
	// It is established from store data here and crosses remote reads as row data.
	Stopped bool
	// Interrupted is true only when this task was ended because nothing was
	// driving its run when the next request arrived ([planTaskInterrupted]). It
	// crosses remote reads as row data, the way Stopped does.
	Interrupted bool
	Seat        string
	Parent      string
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
	// Model is the model this task's own spend rows spent most through, and
	// Tokens the tokens those rows carried, in and out together. Both are read
	// off the same ledger as USD and both are EMPTY WHEN THE LEDGER NAMES NONE:
	// a task that has written no spend row has no known model and no known
	// token count, which is not the same fact as a model called "" or a count
	// of zero, and a surface draws nothing for either.
	Model  string `json:",omitempty"`
	Tokens int    `json:",omitempty"`
	// Started is when the task was created and Ended when it completed; a task
	// still open carries the zero Ended. A PROGRAM's task carries its run's one
	// pair instead — the hand-off and the instant the program was gone
	// (task_run_clock.go's [planRunClocks.apply]) — because the store's pair
	// brackets the copy being cut at one end and whenever each kind of ending
	// wrote the store at the other.
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
	Live      plandb.LiveStep
	LiveParts []PlanCommandPart
	// Program is the name of the program this task was handed to — senior-dev —
	// read off the program record in the task's own record folder
	// ([planProgramRecord]), and empty for every task a worker of this
	// conversation's own drives. Stage is the word for where that program says
	// it is right now — the step of its own process, `explore`, or before it
	// has named one its stage's word — its live step read without its name in
	// front ([planProgramStage]), and empty whenever nothing is live. The rail draws
	// both under the run's own row, where a program's run used to wear only its
	// clock.
	Program string
	Stage   string
	// TrajectoryPath is the file the task's steps are recorded in, for a reader
	// that wants the record itself and not only its length.
	TrajectoryPath string
	// Folder is the run copy this row works in. A surface says it once in the
	// page head and may omit only a leading change into this exact directory.
	Folder string
	// Archived is true for a row read from an ENDED run's store, one of the
	// runs this conversation finished before the one it holds now. The rows
	// arrive oldest run first, so a reader that wants the live run first — the
	// digest in front of the person's sentence — has to be able to tell them
	// apart without reopening a store ([planDigestOrder]).
	Archived bool `json:",omitempty"`
}

// planWordRunning is the rail's word for a row whose work is under way. It is
// not [taskWordWorking], and that is the rail's own ruling rather than a slip:
// the plan list has always said `running` for a claimed row, and the word the
// conversation reads about a row is the word the person reads beside it.
const planWordRunning = "running"

// StateWord is the row's state IN THE WORDS THE RAIL DRAWS: queued, running,
// done, stopped, incomplete, your call. It is the ONE mapping from the store's
// own words (pending, ready, claimed, failed, cancelled, paused) to the ones a
// person reads, and it lives beside the row so every reader of a row — the
// side list, the `tasks` listing, the digest in front of the person's sentence
// — says the same word about the same row. Two readings of one row were two
// states to the model: the digest said `cancelled` and `claimed` of rows the
// person saw as `stopped` and `running`.
//
// A status this build has never heard of reads as NOTHING rather than a word
// invented for it — the emptiness law, applied to a vocabulary that may grow.
func (row PlanTaskRow) StateWord() string {
	if row.Stopped {
		return taskWordStopped
	}
	if row.Hold != "" && (row.Status == string(plandb.StatusReady) || row.Status == string(plandb.StatusRunning)) {
		return taskWordQueued
	}
	switch strings.TrimSpace(row.Status) {
	case string(plandb.StatusPending):
		return taskWordQueued
	case string(plandb.StatusReady), string(plandb.StatusClaimed), string(plandb.StatusRunning):
		return planWordRunning
	case string(plandb.StatusDone):
		return taskWordDone
	case string(plandb.StatusFailed), string(plandb.StatusCancelled):
		return taskWordIncomplete
	case "paused":
		return taskWordYourCall
	}
	return ""
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
	Result      string
	Checks      []string
	Folder      string
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
	// Program is the program this task was handed to and the conversation it
	// has had with codeaf so far (plandb_program.go): nil for every task a worker
	// of this conversation's own drives, which is every page but a program's.
	// A page that carries one is drawn as that conversation rather than as a
	// list of steps.
	Program *PlanProgram
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
	// NotRun is the engine's fact that the harness answered this call itself and
	// nothing ran. Its absence is false, so a record written before the field
	// draws as it did.
	NotRun bool `json:"not_run,omitempty"`
	// Refused is the engine's fact that the call was an action the worker
	// attempted and a door refused. A NotRun step without it is a correction
	// about the form of a reply, which is no step a person reads.
	Refused bool `json:"refused,omitempty"`
	// Parts are display facts derived from Command. Command remains the byte-for-byte
	// record; a surface filters parts instead of rewriting that record.
	Parts []PlanCommandPart `json:"parts,omitempty"`
	// ObservationHeadWithheld says the head of what came back may not be drawn
	// under this step's row, because the row leaves out a part that could have
	// written it ([planStepDisplayFacts] states the law). IT IS THE NEGATIVE ON
	// PURPOSE: its absence is false, so a step from an engine built before the
	// field, and a step no display facts were made for, draws its head as it did.
	ObservationHeadWithheld bool `json:"observation_head_withheld,omitempty"`
}

// PlanCommandPart is one quote-aware command part and the facts only the
// session can establish about it. Separator is the text that followed it.
//
// Command[Start:End] of the step's recorded command is this part as it was
// typed and [End:SepEnd] is the boundary after it, so a surface that leaves a
// part out CUTS ITS SPAN FROM THE RECORDED LINE and never retypes what it
// keeps. A joined-up copy of trimmed parts is a different line from the one
// that ran: it lost the spaces around every boundary and the bracket that
// closed the last one.
//
// A FACT IS SET ONLY WHERE CUTTING IS SAFE. Both facts mean "this part is not
// the work", and both are set only on a part that stands in sequence with its
// neighbours: after the start of the line or a boundary that ends a command,
// and before the end of the line or another such boundary. A part inside a
// substitution or a group is never marked, because the line around a hole in
// one of those is not a command anybody ran. A pipeline is one command: it is
// marked whole when its first part is addressed to the record, and not at all
// otherwise.
type PlanCommandPart struct {
	Command         string
	Separator       string
	Start           int
	End             int
	SepEnd          int
	RecordAddressed bool
	RunCopyPrefix   bool
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
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	if len(stores) == 0 {
		return nil
	}
	var rows []PlanTaskRow
	copies := a.planDisplayRunCopy()
	carried := a.planCarriedPrograms()
	clocks := a.planRunClocks()
	for _, store := range stores {
		dir := filepath.Dir(store.Path())
		// AN ENDED RUN'S STORE IS NAMED FOR ITS PLACE IN THE LINE (`plan.db.1`,
		// [planArchivePaths]); the live run's is the plan's own path.
		archived := filepath.Clean(store.Path()) != filepath.Clean(plan.path)
		spend := planSpendByTask(store.Path())
		live := store.LiveSteps()
		tasks := store.Tasks(plandb.Filter{Chat: plan.chat})
		// THE RUN'S ROOT IS WHAT THE STORE SAYS IT IS, never a name. A run the
		// conversation opens is rooted at the task's own number
		// ([Agent.startKnownTaskRun]), so a comparison against the word `root`
		// counted nothing on any real run and its row wore no progress.
		root := store.RootID()
		for _, task := range tasks {
			row := planTaskRow(store, dir, task, spend, live)
			planCarriedRow(&row, carried[task.ID])
			clocks.apply(&row, dir, task, root)
			a.markPlanMachineHold(&row, store.Path(), task.ID == root)
			row.Folder = a.planTaskRunCopy(task.ID)
			row.LiveParts = planStepDisplayFacts(PlanStep{Command: row.Live.Command}, copies.or(row.Folder), planShimFilename).Parts
			row.Archived = archived
			rows = append(rows, row)
			if task.ID == root {
				applyPlanRootProgress(&rows[len(rows)-1], tasks, root)
			}
		}
	}
	if rows == nil {
		return []PlanTaskRow{}
	}
	return rows
}

// PlanTaskPage answers one task's page — its row, its description, its notes
// and its steps — for the id a surface was handed. False is the answer for a
// task this chat did not spawn, whether it is another conversation's or no
// task at all: the page is the chat's own reading of its own plan.
func (a *Agent) PlanTaskPage(id string) (PlanTaskPage, bool) {
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	if len(stores) == 0 {
		return PlanTaskPage{}, false
	}
	var store *plandb.Store
	var task *plandb.Task
	for _, candidate := range stores {
		if found := candidate.Task(planTaskID(id)); found != nil && found.Chat == plan.chat {
			store, task = candidate, found
			break
		}
	}
	if task == nil {
		return PlanTaskPage{}, false
	}
	dir := filepath.Dir(store.Path())
	spend := planSpendByTask(store.Path())
	live := store.LiveSteps()
	copies := a.planDisplayRunCopy()
	carried := a.planCarriedPrograms()
	clocks := a.planRunClocks()
	// Walk admission order once; membership follows parent edges only.
	all := store.Tasks(plandb.Filter{Chat: plan.chat})
	rows := make(map[string]PlanTaskRow, len(all))
	var children []PlanTaskRow
	depths := map[string]int{task.ID: -1}
	for _, child := range all {
		row := planTaskRow(store, dir, child, spend, live)
		planCarriedRow(&row, carried[child.ID])
		clocks.apply(&row, dir, child, store.RootID())
		a.markPlanMachineHold(&row, store.Path(), child.ID == store.RootID())
		row.Folder = a.planTaskRunCopy(child.ID)
		row.LiveParts = planStepDisplayFacts(PlanStep{Command: row.Live.Command}, copies.or(row.Folder), planShimFilename).Parts
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
	if root := store.RootID(); task.ID == root {
		applyPlanRootProgress(&pageRow, all, root)
	}
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
	// THE PAGE'S OWN ROW CARRIES WHO SPENT AND HOW MUCH THEY READ AND WROTE, off
	// the same ledger its price comes from. A listing does not: the figures are
	// drawn on a task's page and nowhere else.
	if usage, ok := planUsageByTask(store.Path())[task.ID]; ok {
		pageRow.Model, pageRow.Tokens = usage.model, usage.tokens
	}
	return PlanTaskPage{
		Row:         pageRow,
		Description: task.Description,
		Result:      task.Result,
		Checks:      append([]string(nil), task.Checks...),
		Folder:      pageRow.Folder,
		Notes:       planTaskNotes(store, task.ID),
		Steps:       planStepDisplayFactsForPage(planTrajectory(dir, task.ID), copies.or(pageRow.Folder)),
		// THE PAGE CARRIES ITS OWN LIVE STEP, lifted off its row. The field was
		// declared for a surface to draw the step one step early and was never
		// set, so the step in flight — and a program's stage, which is published
		// as that same step — was drawn nowhere on the page.
		Live:     pageRow.Live,
		Children: children,
		WaitRows: waitRows,
		Program:  planProgramPage(dir, task.ID, carried[task.ID], copies.or(pageRow.Folder), a.config.Delegates),
	}, true
}

// markPlanMachineHold copies only this row's refused start. An archived store
// and a worker already running have no admission to wait for.
func (a *Agent) markPlanMachineHold(row *PlanTaskRow, path string, root bool) {
	a.beltMu.Lock()
	run := a.beltRun
	held := run != nil && run.machineHeld[row.ID] && run.store != nil && filepath.Clean(run.store.Path()) == filepath.Clean(path)
	a.beltMu.Unlock()
	if held && (row.Status == string(plandb.StatusReady) || root && row.Status == string(plandb.StatusRunning)) {
		row.Hold = waitingMachineBusy
	}
}

// openPlanReadHandles opens every ended run oldest-first and then the live run.
// Ended handles stay cached because an ended store never changes; only the live
// handle is reopened for each read so writes from another process are visible.
func (a *Agent) openPlanReadHandles() ([]*plandb.Store, *planState, func()) {
	g := a.graph()
	if g == nil {
		return nil, nil, func() {}
	}
	plan := g.planForPages()
	if plan == nil {
		return nil, nil, func() {}
	}
	plan.mu.Lock()
	if plan.archives == nil {
		plan.archives = make(map[string]*plandb.Store)
	}
	stores := make([]*plandb.Store, 0, len(planArchivePaths(plan.path))+1)
	for _, path := range planArchivePaths(plan.path) {
		store := plan.archives[path]
		if store == nil {
			opened, err := plandb.Open(path, "", "", "", "")
			if err != nil {
				continue
			}
			plan.archives[path] = opened
			store = opened
		}
		stores = append(stores, store)
	}
	live := plan.open()
	if live != nil {
		stores = append(stores, live)
	}
	if len(stores) == 0 {
		// NOTHING TO READ IS NEVER RETURNED LOCKED. A plan is armed before its
		// store exists, and both readers answer "no stores" by returning at once;
		// a lock handed back on that path is a lock nobody releases, and every
		// later read, the run's own driver and the conversation's close then wait
		// on it for good ([Agent.openPlanHandle] gives it back the same way).
		plan.mu.Unlock()
		return nil, nil, func() {}
	}
	return stores, plan, func() {
		if live != nil {
			_ = live.Close()
		}
		plan.mu.Unlock()
	}
}

// closePlanArchives closes every read handle this conversation kept on an ended
// run's store. It takes the graph's plan gate to find the plan and the plan's
// own gate to empty it, the same two every reader takes, and it never arms a
// plan that was not armed: a conversation that read nothing holds nothing.
func (a *Agent) closePlanArchives() {
	g := a.graph()
	if g == nil {
		return
	}
	g.planMu.Lock()
	plan := g.plan
	g.planMu.Unlock()
	if plan == nil {
		return
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	for path, store := range plan.archives {
		_ = store.Close()
		delete(plan.archives, path)
	}
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
	plan := g.planForPages()
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

// applyPlanRootProgress puts the run-wide subtree figures on its root row. The
// caller supplies the store read it already made, so progress costs no second read.
func applyPlanRootProgress(row *PlanTaskRow, tasks []*plandb.Task, root string) {
	for _, task := range tasks {
		if task.ID == root {
			continue
		}
		row.Total++
		switch task.Status {
		case plandb.StatusDone:
			row.Done++
		case plandb.StatusClaimed, plandb.StatusRunning:
			row.Running++
		case plandb.StatusPending, plandb.StatusReady:
			row.Queued++
		case plandb.StatusFailed:
			row.Failed++
		}
	}
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
		Stopped:        planTaskStopped(store, task),
		Interrupted:    planTaskInterrupted(task),
		Seat:           seat,
		Steps:          len(planTrajectory(dir, task.ID)),
		USD:            spend[task.ID],
		Started:        task.CreatedAt,
		Ended:          task.CompletedAt,
		Note:           planLastNote(store, task.ID),
		TrajectoryPath: planTrajectoryPath(dir, task.ID),
		Live:           live[task.ID],
	}
	// A PROGRAM'S ROW NAMES ITS PROGRAM AND THE STAGE IT IS IN, both off what is
	// on disk beside the trajectory or already read: the record the worker wrote
	// at the program's hello, and the live step the worker publishes the stage
	// as. Every other row costs one look for a record that is not there.
	if record, ok := planProgramRecord(dir, task.ID, ""); ok {
		planProgramRow(&row, record.Name)
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

// planTaskStopped is the store property that distinguishes a person's stop
// from every other cancellation. The stop road writes either the bare word or
// that word followed by the person's reason, and this package writes it
// ([stopBecause]), so reading it back is reading its own word.
//
// WHAT A STOP TOOK DOWN WITH IT WAS STOPPED TOO. The store ends everything
// under a cancelled task in the same write and at the same instant, under its
// own reason, so a part reads as stopped when it was cancelled in the very
// instant an ancestor of it was stopped by a person. A part that failed or was
// cancelled at any other moment, for any other reason, is not: it keeps the
// word every other ending without a judgement wears.
func planTaskStopped(store *plandb.Store, task *plandb.Task) bool {
	if task == nil || task.Status != plandb.StatusCancelled {
		return false
	}
	if planStopReason(task.Error) {
		return true
	}
	seen := map[string]bool{task.ID: true}
	for up := task.ParentID; up != "" && !seen[up] && store != nil; {
		seen[up] = true
		parent := store.Task(up)
		if parent == nil {
			return false
		}
		if parent.Status == plandb.StatusCancelled && planStopReason(parent.Error) {
			return parent.CompletedAt.Equal(task.CompletedAt)
		}
		up = parent.ParentID
	}
	return false
}

// planTaskInterrupted is the store property a run set aside as interrupted
// leaves on its rows: the run's own task and every part still open when the next
// request arrived were ended under the word `interrupted` ([setAsideRunStore]).
// Such a row is not a failure and not a stop, and reading it as either would say
// something happened to the work when nothing did: nobody was driving it.
func planTaskInterrupted(task *plandb.Task) bool {
	if task == nil || (task.Status != plandb.StatusFailed && task.Status != plandb.StatusCancelled) {
		return false
	}
	return strings.TrimSpace(task.Error) == taskWordInterrupted
}

// planStopReason reports whether an ending's reason is the one a person's stop
// writes: the word alone, or the word and what they said.
func planStopReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason == taskStoppedWord || strings.HasPrefix(reason, taskStoppedWord+": ")
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

// planNoteAuthorWord names the hand behind one note in the words a reader
// knows, and it is the one spelling this package uses for that: the person
// steering the run, the sibling task whose worker wrote it when the store kept
// a name, and otherwise another worker on the run. It exists so the listing and
// the task page cannot come to say the same author two different ways.
func planNoteAuthorWord(note PlanTaskNote) string {
	if note.Person {
		return "the person"
	}
	switch name := strings.TrimSpace(note.Author); {
	case name == plandb.NoteAgentChat:
		return "you"
	case name != "" && name != "default":
		return "task " + name
	}
	return "a worker on this run"
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

// planTaskUsage is one task's model and token figures, read off its spend rows.
type planTaskUsage struct {
	model  string
	tokens int
}

// planUsageByTask reads the run's spend ledger per task for the two figures a
// task's page draws beside its price: the model the task's rows spent most
// through, and the tokens they carried. It is [planSpendByTask]'s reading, on
// its own read-only connection for the same reason. A row that names no model
// names none, and a task whose rows carry no tokens has none known.
func planUsageByTask(path string) map[string]planTaskUsage {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT task_id, model, SUM(usd), COUNT(*), SUM(in_tokens + out_tokens) FROM spend GROUP BY task_id, model`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type tally struct {
		usd   float64
		calls int
	}
	best := map[string]tally{}
	out := map[string]planTaskUsage{}
	for rows.Next() {
		var id, model string
		var usd float64
		var calls, tokens int
		if rows.Scan(&id, &model, &usd, &calls, &tokens) != nil {
			return out
		}
		usage := out[id]
		if tokens > 0 {
			usage.tokens += tokens
		}
		// THE MODEL IS THE ONE THE TASK SPENT MOST THROUGH, with the call count
		// and then the name breaking a tie, so the same ledger always names the
		// same model ([planSpendBySeat] chooses a seat's model the same way).
		if model = strings.TrimSpace(model); model != "" {
			held, seen := best[id]
			if !seen || usd > held.usd || (usd == held.usd && calls > held.calls) ||
				(usd == held.usd && calls == held.calls && model < usage.model) {
				best[id] = tally{usd: usd, calls: calls}
				usage.model = model
			}
		}
		out[id] = usage
	}
	return out
}

// planRunCopies says which folders are a run's own copy. live is the copy of
// the run in flight, or the folder the row names when no run is; root is the
// conversation's own folder of copies. AN ENDED RUN'S COPY HAS BEEN GIVEN BACK
// and its steps still record the change into it, so a folder directly under
// root is a run's copy whether or not it still exists. A folder further down is
// somewhere the work went inside its copy, and that is the work.
type planRunCopies struct {
	live string
	root string
}

func (c planRunCopies) holds(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	dir = filepath.Clean(dir)
	if c.live != "" && dir == filepath.Clean(c.live) {
		return true
	}
	return c.root != "" && filepath.Dir(dir) == filepath.Clean(c.root)
}

// planSequenced reports whether a boundary ends one command and starts the
// next, which is the only kind a part may be cut at.
func planSequenced(separator string) bool {
	switch separator {
	case "", ";", "\n", "&&", "||", "&":
		return true
	}
	return false
}

// planStepDisplayFacts annotates a copy of a recorded step. It never changes
// the trajectory or Command: these facts are a read-side view only.
func planStepDisplayFacts(step PlanStep, copies planRunCopies, recordCommand string) PlanStep {
	approvalParts := approval.SplitBashCommand(step.Command)
	var parts []PlanCommandPart
	if len(approvalParts) > 0 {
		parts = make([]PlanCommandPart, len(approvalParts))
	}
	for i, part := range approvalParts {
		parts[i] = PlanCommandPart{Command: part.Command, Separator: part.Separator, Start: part.Start, End: part.End, SepEnd: part.SepEnd}
	}
	// A PIPELINE IS READ AS ONE COMMAND: from a part that stands in sequence to
	// the last part a bar joins to it.
	for i := 0; i < len(parts); {
		last := i
		for last+1 < len(parts) && parts[last].Separator == "|" {
			last++
		}
		before := ""
		if i > 0 {
			before = parts[i-1].Separator
		}
		// Two boundaries in a row leave a stretch that made no part, and the
		// boundary that was lost with it may have been one that groups: a part
		// that does not begin where the last boundary ended, or the first part
		// of a line that does not begin the line, is not in sequence.
		adjoins := parts[i].Start == 0
		if i > 0 {
			adjoins = parts[i-1].SepEnd == parts[i].Start
		}
		if adjoins && planSequenced(before) && planSequenced(parts[last].Separator) {
			words := strings.Fields(parts[i].Command)
			switch {
			case len(words) == 0:
			case strings.Trim(words[0], "'\"") == recordCommand:
				for at := i; at <= last; at++ {
					parts[at].RecordAddressed = true
				}
			case i == 0 && last == 0 && len(words) == 2 && words[0] == "cd" && copies.holds(strings.Trim(words[1], "'\"")):
				parts[i].RunCopyPrefix = true
			}
		}
		i = last + 1
	}
	step.Parts = parts
	// THE HEAD OF WHAT CAME BACK IS DRAWN UNDER A ROW ONLY WHEN EVERY PART LEFT
	// OUT OF THE ROW CANNOT HAVE WRITTEN TO IT AND STOPS EVERYTHING AFTER IT IF IT
	// FAILS. The record holds one observation for the whole line, and nothing
	// anywhere knows where one part's output ends and the next begins, so a head
	// under a row that left a part out is a guess about whose words they are
	// unless the left-out part is silent when it works and final when it does
	// not: then the head is the work's, or it is the failure of a step in which
	// nothing else ran. A row that leaves nothing out has no such part and keeps
	// its head. The test is on the two facts a part carries, never on the words a
	// command prints:
	//
	//   - a part addressed to the run's record writes what it likes, so it always
	//     withholds the head;
	//   - a leading change into the run's copy has the property only when it is
	//     joined so that its failure ends the line. Joined any other way, what
	//     follows runs after a failed change and the head may be that failure's.
	for _, part := range parts {
		if part.RecordAddressed || (part.RunCopyPrefix && strings.TrimSpace(part.Separator) != "&&") {
			step.ObservationHeadWithheld = true
			break
		}
	}
	return step
}

// planTaskRunCopy answers the folder the row has always exposed. A belt run's
// copy is deliberately not this property: Folder keeps the conversation
// workspace while display suppression follows the live belt run separately.
func (a *Agent) planTaskRunCopy(planID string) string {
	g := a.graph()
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, node := range g.nodes {
		if node != nil && node.spec.planID == planID {
			return node.worktree
		}
	}
	return a.config.Workspace
}

// planDisplayRunCopy is the folders a step's leading change may name and still
// be no part of the work: the live belt run's copy and any copy under the
// conversation's own folder of them. IT IS READ ONCE FOR A WHOLE LISTING, never
// once per row: it takes the belt's lock and resolves a path, and a listing has
// as many rows as the run has parts.
func (a *Agent) planDisplayRunCopy() planRunCopies {
	copies := planRunCopies{root: a.treesDir()}
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if a.beltRun != nil {
		copies.live = a.beltRun.workspace
	}
	return copies
}

// or names the row's own folder as the copy when no run is in flight, which is
// the folder a row has always exposed.
func (c planRunCopies) or(folder string) planRunCopies {
	if c.live == "" {
		c.live = folder
	}
	return c
}

func planStepDisplayFactsForPage(steps []PlanStep, copies planRunCopies) []PlanStep {
	for i := range steps {
		steps[i] = planStepDisplayFacts(steps[i], copies, planShimFilename)
	}
	return steps
}
