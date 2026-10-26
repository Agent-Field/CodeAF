package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// AppTaskView is an engine read, independent of the app's JSON contract.
// Numeric graph ids and plan ids keep their existing bytes behind the HTTP codec.
type AppTaskView struct {
	// Retryable is a producer capability, not permission to bypass retry admission.
	Retryable bool

	ID, Parent, Title, Brief, State, Model, Origin, Result string
	Waits                                                  []string
	Deadline                                               time.Time
	Held                                                   bool
	Cap, Spent                                             float64
	Step                                                   int
	Files                                                  []string
	Notes                                                  []AppTaskNote
	WorkLog                                                []string
	// Steps is the task's work AS FACTS, oldest first and bounded
	// (task_app_steps.go). WorkLog stays beside it as the worded tail a model
	// reads; a surface that groups, folds and times steps reads this, because
	// every fact it needs was spent composing those sentences.
	Steps []AppTaskStep
	// StepsKnown says whether Step is a COUNT or an absence. A task whose record
	// never counted its steps and a task that took none put the same zero in
	// Step, and a page that read that zero as a count said "0 steps" about work
	// it simply had no record of. UNKNOWN IS NOT ZERO, so the two are separated
	// here rather than guessed at by whoever draws them.
	StepsKnown bool
	Elapsed    int64
	Version    uint64
	// Source is the message or directive that started the task, captured when it
	// was admitted; nil is unknown (task_source.go). Dismissed says the person hid
	// it from the default view (task_dismiss.go); it changes nothing about the work.
	Source    *TaskSource
	Dismissed bool
	// FinishedAt is when the work ended, as the record kept it; the zero time is
	// unknown.
	FinishedAt time.Time
	// RoutingState and Crew are the task's models in brief (taskrouting.go):
	// the published decision, never a settings read.
	RoutingState string
	Crew         *crewroute.Decision
	TaskPins     map[crewroute.Seat]bool
	// Program is the program the task was handed to, off the page read AppTask
	// already makes (task_app_program.go); nil for every other task.
	Program *AppProgramView `json:",omitempty"`
	// RunProgram, Ending and Branch are what a handed-over task's ROW persisted,
	// copied and never derived: the program's name, the typed word for why it
	// ended ("stopped" when the row's stopped flag is set, which does not say who
	// stopped it), and the branch its work was kept
	// on. "" is unknown for each. They need no log read, so a list carries them
	// as cheaply as a page does (task_app_runs.go, [appRunFacts]).
	RunProgram, Ending, Branch string
}
type AppTaskNote struct {
	Direction uint64 `json:"direction,omitempty"`
	Text      string
	// From is WHO SAID IT, carried from the record's own label and never read
	// back out of the words ([directionFrom], plandb's note origins).
	//
	// IT IS THE DIFFERENCE BETWEEN A PERSON AND A MACHINE. A page draws the
	// person's own notes as their messages; before this field existed every note
	// reached the app with no origin at all, so a surface defaulting to "yours"
	// put a running task's own lines — and the words a checker handed back — in
	// the person's mouth. The engine knows which is which at the moment each
	// arrives, so it says so.
	//
	// "" is a note from a record that never labelled one, which a reader takes as
	// unknown and must not take as the person's.
	From       AppNoteFrom
	ReadAtStep *int
	// ClientID is the sender's own opaque key for this note, kept so a surface can
	// tell its sent note from the one it reads back. It is OPAQUE: the engine never
	// reads meaning into it and never matches by text. Absent is unknown: a record
	// from before this field, a plan-store note, a task's own line and a note that
	// arrived by another road carry none, and nothing stamps one by position.
	ClientID string `json:"client_id,omitempty"`
}

// AppNoteClientIDMax bounds a note's client id in bytes; it is the one limit
// message client ids already keep.
const AppNoteClientIDMax = MaxClientIDBytes

// AppNoteClientID is clientID if it can be kept as a note's key, else "". An
// oversized or malformed key is dropped whole, never truncated, because a
// truncated key could collide with another.
func AppNoteClientID(clientID string) string {
	if len(clientID) > AppNoteClientIDMax || !utf8.ValidString(clientID) {
		return ""
	}
	for _, r := range clientID {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return clientID
}

// AppNoteFrom is a note's origin, in the engine's own words.
type AppNoteFrom string

const (
	// AppNoteFromPerson is the person at a keyboard: a steer typed into a task,
	// the words sent with a continue. Only these may revise an assignment
	// (assignment.go's authority note).
	AppNoteFromPerson AppNoteFrom = "person"
	// AppNoteFromTask is the task's own line, or another agent's coordination
	// with it — anything delivered to or by the work rather than typed by the
	// person.
	AppNoteFromTask AppNoteFrom = "task"
)

// appNoteFrom is the one mapping from the assignment's record of who spoke to
// the word a page is given. Anything this build does not recognise reads as the
// task's, which is the reading that claims nothing about a person.
func appNoteFrom(from directionFrom) AppNoteFrom {
	if from == directionFromPerson {
		return AppNoteFromPerson
	}
	return AppNoteFromTask
}

type AppQuickTaskView struct {
	// Grew is why the quick work asked to become a full task, while it waits
	// for that answer; State is then "grew".
	ID, Title, State, Grew string
	Files                  []AppQuickFile
	Added, Removed         int
	Version                uint64
}
type AppQuickFile struct {
	Path, BeforeHash, AfterHash string
	// Added and Removed are this file's own changed lines.
	Added, Removed int
}

// AppActionProposalModel is the proposal card's Change model: it moves one
// proposed (or held) task's model before it starts and is refused for anything
// else, so it can never retarget work already running or touch the agent.
const AppActionProposalModel = "proposal_model"

type AppTaskIntent struct {
	ID, Action, Text, Model, Verb string
	State                         *string
	Version                       *uint64
	// ClientID is a note's opaque correlation key ([AppTaskNote.ClientID]); it is
	// read only by the "note" and "brief" actions on a graph task and by "note"
	// on a running quick task, and is written with the direction itself.
	ClientID string `json:",omitempty"`
}

// The metadata records a person's holds and note receipts beside the work itself.
// Wake channels belong to this process; a restored hold creates one on demand.
type appTaskRecord struct {
	Paused   bool          `json:"paused,omitempty"`
	Declined bool          `json:"declined,omitempty"`
	Version  uint64        `json:"version,omitempty"`
	Notes    []AppTaskNote `json:"notes,omitempty"`
	Brief    string        `json:"brief,omitempty"`
}

func cloneAppTaskRecord(in appTaskRecord) appTaskRecord {
	in.Notes = append([]AppTaskNote(nil), in.Notes...)
	for i := range in.Notes {
		if in.Notes[i].ReadAtStep != nil {
			step := *in.Notes[i].ReadAtStep
			in.Notes[i].ReadAtStep = &step
		}
	}
	return in
}

// writeAppControl uses the checkpoint's established store-before-graph lock order.
// A failed disk write restores the in-memory change before any reader can see it.
func (g *TaskGraph) writeAppControl(change func() (func(), error)) error {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	rollback, err := change()
	if err != nil {
		return err
	}
	if store != nil {
		if err := store.writeLocked(g.documentLocked()); err != nil {
			if rollback != nil {
				rollback()
			}
			return err
		}
	}
	return nil
}

// appBoundary holds the next action without changing whether a note was read.
func (n *TaskNode) appBoundary(ctx context.Context) error {
	for {
		n.graph.mu.Lock()
		paused := n.app.Paused
		if paused && n.appWake == nil {
			n.appWake = make(chan struct{})
		}
		wake := n.appWake
		n.graph.mu.Unlock()
		if !paused {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
	return ctx.Err()
}

func appNodeState(n *TaskNode) string {
	if n.app.Declined {
		return "declined"
	}
	if n.stopped {
		return "stopped"
	}
	if n.app.Paused && !n.state.settled() {
		return "paused"
	}
	switch n.state {
	case TaskQueued:
		return "waiting"
	case TaskUnverified:
		return "your_call"
	case TaskInterrupted:
		return "paused"
	default:
		return string(n.state)
	}
}

// AppTasks reads both engine task roads and pending proposals; no UI copy owns state.
func (a *Agent) AppTasks() ([]AppTaskView, error) {
	out := []AppTaskView{}
	cap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	graphIDs := map[string]bool{}
	out = a.appGraphTasks(out, cap, graphIDs)
	// Both projections share the plan gate and each trajectory for this listing.
	rows := a.appRunRows()
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	read := &planTaskRead{stores: stores, plan: plan}
	for _, row := range rows {
		id := strconv.FormatUint(row.ID, 10)
		if graphIDs[id] {
			continue
		}
		graphIDs[id] = true
		out = append(out, a.appRunViewIn(row, cap, read))
	}
	for _, row := range a.planTasksIn(read) {
		if graphIDs[row.ID] {
			continue
		}
		state := row.StateWord()
		if state == "queued" {
			state = "waiting"
		}
		if state == "your call" {
			state = "your_call"
		}
		if state == "incomplete" {
			state = "failed"
		}
		out = append(out, AppTaskView{Source: cloneSource(row.Source), Dismissed: state == "failed" && a.tasker().isDismissed(row.ID), ID: row.ID, Parent: row.Parent, Title: row.Title, State: state, Model: row.Model, Cap: cap, Spent: row.USD, Step: row.Steps, Waits: append([]string{}, row.Waits...), Origin: "asked"})
	}
	out = append(out, a.appProposalViews(cap)...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// appGraphTasks is the task graph's own nodes as the app lists them, each
// marked in ids so the rows after it are not listed twice.
func (a *Agent) appGraphTasks(out []AppTaskView, cap float64, ids map[string]bool) []AppTaskView {
	graph := a.tasker()
	if graph == nil {
		return out
	}
	graph.mu.Lock()
	nodes := make([]*TaskNode, 0, len(graph.order))
	for _, id := range graph.order {
		nodes = append(nodes, graph.nodes[id])
	}
	graph.mu.Unlock()
	for _, node := range nodes {
		if node == nil || node.spec.quick != nil {
			continue
		}
		notice := node.notice()
		_, live, _ := a.taskLiveRead(node.id, taskLiveMaxTail)
		graph.mu.Lock()
		view := AppTaskView{Retryable: true, RoutingState: TaskRoutingSingle, ID: strconv.FormatUint(node.id, 10), Title: notice.Title, Brief: notice.Brief, State: appNodeState(node), Model: notice.Model, Cap: cap, Spent: notice.CostUSD, Step: live.Steps, Result: notice.Report, Files: append([]string{}, notice.Changed...), Notes: node.appNotesLocked(), WorkLog: live.Lines, Elapsed: int64(notice.Elapsed.Seconds()), Version: node.app.Version + 1, Origin: "proposed"}
		if node.app.Brief != "" {
			view.Brief = node.app.Brief
		}
		view.Source, view.Dismissed, view.FinishedAt = cloneSource(node.spec.source), graph.dismissed[view.ID] && view.State == "failed" && node.endingLocked() != TaskEndingStopped, node.ended
		if node.parent != 0 {
			view.Parent = strconv.FormatUint(node.parent, 10)
		}
		for _, id := range node.dependsOn {
			view.Waits = append(view.Waits, strconv.FormatUint(id, 10))
		}
		if len(view.Files) == 0 {
			view.Files = append([]string{}, node.wrote...)
		}
		journal := node.journal
		graph.mu.Unlock()
		view.WorkLog = appTaskWorkLog(view.WorkLog, journal)
		appTaskSteps(&view, live, journal)
		ids[view.ID] = true
		out = append(out, view)
	}
	return out
}

func (a *Agent) AppTask(id string) (AppTaskView, bool, error) {
	rows, err := a.AppTasks()
	if err != nil {
		return AppTaskView{}, false, err
	}
	for _, row := range rows {
		if row.ID == id {
			// THE PROGRAM IS READ WITHIN ITS OWN BUDGET, never through the page's
			// whole-log read (task_app_program.go).
			if page, source, ok := a.planTaskPageFor(id, false); ok {
				ctx, cancel := context.WithTimeout(context.Background(), appProgramReadTime)
				row.Program = a.appProgramRead(ctx, source, -1, "")
				cancel()
				if row.Brief == "" {
					row.Brief = page.Description
				}
				if row.Result == "" {
					row.Result = page.Result
				}
				if len(row.Notes) == 0 {
					// WHO LEFT EACH NOTE IS ON THE RECORD AND WAS BEING THROWN
					// AWAY HERE. A plan task's page carries its worker's own
					// handoffs beside the person's questions, each already marked
					// ([PlanTaskNote.Person]); dropping the mark left a surface
					// with nothing to attribute by, so every default put the
					// worker's lines — the handoff, the account of the work —
					// into the person's own bubble as words they never typed.
					for _, note := range page.Notes {
						from := AppNoteFromTask
						if note.Person {
							from = AppNoteFromPerson
						}
						// The key is the one the store committed with the note;
						// nothing here stamps one ([plandb.Note.ClientID]).
						row.Notes = append(row.Notes, AppTaskNote{Text: note.Body, From: from, ClientID: note.ClientID})
					}
				}
				if len(row.Steps) == 0 {
					appPlanTaskSteps(&row, page)
				}
				if len(row.WorkLog) == 0 {
					row.WorkLog = appTrajectoryLines(row.Steps)
					if len(row.WorkLog) == 0 {
						for _, step := range page.Steps {
							row.WorkLog = append(row.WorkLog, step.Command)
						}
					}
				}
			}
			return row, true, nil
		}
	}
	return AppTaskView{}, false, nil
}

func appExpect(intent AppTaskIntent, state string, version uint64) error {
	if (intent.State != nil && *intent.State != state) || (intent.Version != nil && *intent.Version != version) {
		return errors.New("stale: that task changed; open it again")
	}
	return nil
}

// AppTaskAction keeps admission and landing separate: a proposal answers a wait;
// a finished task uses the existing continuation, check and acceptance doors.
//
// Its errors are exactly what they always were. A caller that has to tell a
// note that was HELD, REFUSED or of UNKNOWN fate apart asks
// [Agent.AppTaskActionReceipt], which takes the same dispatch.
func (a *Agent) AppTaskAction(intent AppTaskIntent) error {
	_, err := a.appTaskAct(intent)
	return err
}

// appTaskAct is the one dispatch both doors take. The receipt is the node's or
// the store's own answer for a note or a brief, and empty for every other verb.
func (a *Agent) appTaskAct(intent AppTaskIntent) (SteerReceipt, error) {
	id, numeric := strconv.ParseUint(intent.ID, 10, 64)
	// Starting an already offered proposal answers its pending question.
	if numeric == nil && intent.Action == "start" {
		if handled, err := a.appProposalAction(id, intent); handled {
			return SteerReceipt{}, err
		}
	}
	// Answers, stopping, hiding and holding work remain available on a viewer.
	switch intent.Action {
	case "cancel", "stop", "hold", "typing", "decline", AppActionDismiss, AppActionRestore:
	case "decide":
		if intent.Verb == "not_right" || intent.Verb == "tell" {
			if err := a.CheckWork(); err != nil {
				return SteerReceipt{}, err
			}
		}
	default:
		if err := a.CheckWork(); err != nil {
			return SteerReceipt{}, err
		}
	}
	// DISMISS AND RESTORE ARE ONE GENERIC FACT FOR EVERY ROAD, so they are settled
	// before the road is chosen (task_dismiss.go).
	if intent.Action == AppActionDismiss || intent.Action == AppActionRestore {
		return SteerReceipt{}, a.appDismissAction(intent)
	}
	if handled, said, err := a.appBeforeTheList(id, numeric, intent); handled {
		return said, err
	}
	if intent.Action == AppActionProposalModel {
		// Past its proposal a task's model is RetargetTask's promise, not this card's.
		return SteerReceipt{}, errors.New("that task must be a proposed task to change its model")
	}
	row, found, err := a.AppTask(intent.ID)
	if err != nil {
		return SteerReceipt{}, err
	}
	if !found {
		return SteerReceipt{}, errors.New("no such task")
	}
	if err := appExpect(intent, row.State, row.Version); err != nil {
		return SteerReceipt{}, err
	}
	node := a.taskNode(id)
	if numeric == nil && node == nil {
		if run, ok := a.appRunRow(id); ok {
			return a.appRunAction(run, intent)
		}
	}
	if numeric != nil || node == nil {
		return SteerReceipt{}, a.appPlanAction(intent)
	}
	if appWordAction(intent.Action) {
		return a.appTaskNote(node, intent)
	}
	return SteerReceipt{}, a.appGraphAction(node, intent, row)
}

// appWordAction is whether an action carries the person's words to the task:
// the two verbs that have a receipt ([Agent.AppTaskActionReceipt]).
func appWordAction(action string) bool { return action == "note" || action == "brief" }

// appBeforeTheList answers the numeric roads that are not on the page list: a
// proposal waiting for its answer, and a note to a quick task.
//
// A QUICK TASK IS NOT ON THE PAGE LIST BUT IS A NODE THAT CAN BE TOLD
// SOMETHING, so a note to one takes the node's door before the list is asked.
// Every other action on a quick id falls through to the list's own refusal.
func (a *Agent) appBeforeTheList(id uint64, numeric error, intent AppTaskIntent) (bool, SteerReceipt, error) {
	if numeric != nil {
		return false, SteerReceipt{}, nil
	}
	if handled, err := a.appProposalAction(id, intent); handled {
		return true, SteerReceipt{}, err
	}
	if intent.Action != "note" {
		return false, SteerReceipt{}, nil
	}
	if node := a.taskNode(id); node != nil && node.spec.quick != nil {
		said, err := a.appQuickNote(node, intent)
		return true, said, err
	}
	return false, SteerReceipt{}, nil
}

// A proposal's clock and model are edited under the same lock as its answer.
func (a *Agent) appProposalAction(id uint64, intent AppTaskIntent) (bool, error) {
	var choice taskModelChoice
	if intent.Action == "model" || intent.Action == AppActionProposalModel {
		choice = a.resolveTaskModel(intent.Model)
	}
	a.mu.Lock()
	question := a.taskAnswers[id]
	if question == nil {
		a.mu.Unlock()
		return false, nil
	}
	state := "proposed"
	if question.held {
		state = "held"
	}
	if err := appExpect(intent, state, question.appVersion+1); err != nil {
		a.mu.Unlock()
		return true, err
	}
	switch intent.Action {
	case "hold", "typing":
		a.mu.Unlock()
		a.HoldTask(id)
		return true, nil
	case "model", AppActionProposalModel:
		if intent.Action == AppActionProposalModel && strings.TrimSpace(intent.Model) == "" {
			a.mu.Unlock()
			return true, errors.New("choose one known model")
		}
		if choice.problem != "" || choice.model == "" {
			a.mu.Unlock()
			return true, errors.New("choose one known model")
		}
		question.notice.Model = choice.model
		question.appVersion++
		notice, hub := question.notice, a.hub
		a.mu.Unlock()
		a.broadcastTaskProposal(hub, notice)
		return true, nil
	case "start", "decline":
		delete(a.taskAnswers, id)
		notice := question.notice
		a.mu.Unlock()
		if intent.Action == "decline" {
			a.keepAppDeclined(id, notice)
		}
		question.answer <- TaskAnswer{Approved: intent.Action == "start", Model: notice.Model}
		return true, nil
	default:
		a.mu.Unlock()
		return true, errors.New("that proposal must be started, held or declined")
	}
}

func (a *Agent) appPlanAction(intent AppTaskIntent) error {
	switch intent.Action {
	case "note":
		// The key is committed in the note's own transaction ([Agent.PlanNoteKeyed]).
		return a.PlanNoteKeyed(intent.ID, intent.Text, AppNoteClientID(intent.ClientID))
	case "brief":
		return a.PlanAmend(intent.ID, intent.Text)
	case "pause":
		return a.PlanPause(intent.ID)
	case "resume":
		return a.PlanResume(intent.ID)
	case "stop":
		return a.PlanCancel(intent.ID)
	}
	return errors.New("that action is unavailable for this plan task")
}
func (a *Agent) appGraphAction(node *TaskNode, intent AppTaskIntent, row AppTaskView) error {
	switch intent.Action {
	case "pause", "resume":
		return node.appPause(intent)
	case "stop":
		_, err := a.Cancel("task:" + intent.ID)
		return err
	case "model":
		_, err := a.RetargetTask(node.id, intent.Model)
		return err
	case "decide":
		return a.appTaskDecide(node.id, intent, row.State)
	case "continue":
		// A task the app's quit left with nothing driving it goes back on the
		// frontier as itself ([Agent.ContinueTask]); any other is left alone.
		node.graph.mu.Lock()
		interrupted := node.state == TaskInterrupted
		node.graph.mu.Unlock()
		if !interrupted {
			return nil
		}
		return a.ContinueTask(node.id, "")
	}
	return errors.New("unknown task action")
}

// A queued hold keeps the frontier still; a running hold releases at a boundary.
func (node *TaskNode) appPause(intent AppTaskIntent) error {
	var wake chan struct{}
	err := node.graph.writeAppControl(func() (func(), error) {
		if err := appExpect(intent, appNodeState(node), node.app.Version+1); err != nil {
			return nil, err
		}
		if node.state.settled() {
			return nil, errors.New("that task has finished")
		}
		old := node.app
		node.app.Paused = intent.Action == "pause"
		node.app.Version++
		if intent.Action == "resume" {
			wake = node.appWake
		}
		return func() { node.app = old }, nil
	})
	if err != nil {
		return err
	}
	if wake != nil {
		node.graph.mu.Lock()
		if node.appWake == wake {
			close(wake)
			node.appWake = nil
		}
		node.graph.mu.Unlock()
	}
	node.graph.announce(node)
	if intent.Action == "resume" {
		node.graph.runFrontier()
	}
	return nil
}

func (a *Agent) appTaskNote(node *TaskNode, intent AppTaskIntent) (SteerReceipt, error) {
	if strings.TrimSpace(intent.Text) == "" {
		return SteerReceipt{}, errors.New("words required")
	}
	// THE KEY IS WRITTEN WITH THE WORDS, IN THE ONE ADMISSION THAT PUTS THEM ON
	// THE DISK ([admitDirection]), and nowhere after. A second write here used to
	// carry it, which meant a delivered note could come back as an error and a
	// restart between the two could keep the words without their key.
	said, err := a.sayToTask(node.id, intent.Text, fromPerson, spokenSource{clientID: AppNoteClientID(intent.ClientID)})
	if err != nil {
		return SteerReceipt{}, err
	}
	if intent.Action != "brief" {
		node.graph.announce(node)
		return said, nil
	}
	// A brief is a note AND the page's own brief. The note is delivered already,
	// so a failure to keep the brief is not a refusal of the words, and the
	// receipt the words got is still theirs.
	err = node.graph.writeAppControl(func() (func(), error) {
		old := cloneAppTaskRecord(node.app)
		node.app.Brief = intent.Text
		node.app.Version++
		return func() { node.app = old }, nil
	})
	node.graph.announce(node)
	if err != nil {
		return said, fmt.Errorf("%w: %v", ErrBriefNotKept, err)
	}
	return said, nil
}

// ErrBriefNotKept says a brief's words WERE delivered to the task as a note and
// the page's brief could not be written down. IT IS NOT A REFUSAL: sending the
// words again would deliver them twice, and nothing about the send is unknown.
var ErrBriefNotKept = errors.New("the words reached the task, but its brief could not be kept")

// appQuickNote is a note to a running quick task, through the node's own door
// with its key ([Agent.appTaskNote]). A quick task that has settled is refused
// there in the engine's own words, and nothing here can start one.
func (a *Agent) appQuickNote(node *TaskNode, intent AppTaskIntent) (SteerReceipt, error) {
	rows, _ := a.AppQuickTasks()
	for _, row := range rows {
		if row.ID == intent.ID {
			if err := appExpect(intent, row.State, row.Version); err != nil {
				return SteerReceipt{}, err
			}
		}
	}
	return a.appTaskNote(node, intent)
}

func (a *Agent) appTaskDecide(id uint64, intent AppTaskIntent, state string) error {
	if state != "your_call" {
		return errors.New("that task is not waiting for your call")
	}
	switch intent.Verb {
	case "accept":
		return a.ResolveUnverified(id, TaskAccept, intent.Text)
	case "recheck":
		return a.ResolveUnverified(id, TaskReaudit, intent.Text)
	case "not_right", "tell":
		return a.ContinueTask(id, intent.Text)
	case "let_it_decide":
		return a.HandUnverifiedToModel(id)
	}
	return errors.New("unknown your call verb")
}

// A declined proposal has a record but never enters the running frontier.
func (a *Agent) keepAppDeclined(id uint64, notice TaskNotice) {
	graph := a.graph()
	node := &TaskNode{graph: graph, id: id, kind: "", state: TaskFailed, done: make(chan struct{}), spec: taskSpec{title: notice.Title, named: true, brief: notice.Brief, acceptance: notice.Acceptance, model: notice.Model}, app: appTaskRecord{Declined: true}, report: "Declined by you"}
	close(node.done)
	graph.mu.Lock()
	if graph.nodes[id] == nil {
		graph.nodes[id] = node
		graph.order = append(graph.order, id)
	}
	graph.mu.Unlock()
	graph.checkpoint()
}

func (a *Agent) AppQuickTasks() ([]AppQuickTaskView, error) {
	out := []AppQuickTaskView{}
	graph := a.tasker()
	if graph == nil {
		return out, nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.spec.quick == nil {
			continue
		}
		quick := node.spec.quick
		state := "running"
		switch {
		case quick.undone:
			state = "undone"
		case quick.promoted:
			state = "promoted"
		case node.state == TaskDone:
			state = "done"
		case node.state.settled():
			state = "stopped"
		case quick.promotion != 0 && quick.grew != "":
			state = "grew"
		}
		view := AppQuickTaskView{ID: strconv.FormatUint(id, 10), Title: node.spec.title, State: state, Grew: quick.grew, Version: node.app.Version + 1, Files: []AppQuickFile{}}
		for _, c := range quick.captures {
			added, removed := quickLineChanges(c.Before, c.After)
			view.Files = append(view.Files, AppQuickFile{Path: c.Path, BeforeHash: quickHash(c.Before, c.BeforeExists), AfterHash: quickHash(c.After, c.AfterExists), Added: added, Removed: removed})
			view.Added += added
			view.Removed += removed
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *Agent) AppQuickTaskAction(intent AppTaskIntent) ([]string, []string, error) {
	if intent.Action != "cancel" {
		if err := a.CheckWork(); err != nil {
			return nil, nil, err
		}
	}
	id, err := strconv.ParseUint(intent.ID, 10, 64)
	if err != nil {
		return nil, nil, errors.New("no such quick task")
	}
	node := a.taskNode(id)
	if node == nil || node.spec.quick == nil {
		return nil, nil, errors.New("no such quick task")
	}
	rows, _ := a.AppQuickTasks()
	for _, row := range rows {
		if row.ID == intent.ID {
			if err := appExpect(intent, row.State, row.Version); err != nil {
				return nil, nil, err
			}
		}
	}
	switch intent.Action {
	case "undo":
		return a.UndoQuickTask(id)
	case "cancel":
		_, err := a.Cancel("task:" + intent.ID)
		return nil, nil, err
	case "promote":
		return nil, nil, a.appPromoteQuick(node)
	}
	return nil, nil, errors.New("unknown quick task action")
}

// Note receipts come from the steering record that was carried into a request.
//
// A NOTE'S KEY IS THE ONE ITS DIRECTION WAS WRITTEN WITH. The app's own sidecar
// is read only for a direction that carries none, which is a note written by a
// build that kept the key there; nothing is ever stamped by position, text or
// time, so a note with no key on either record is unknown.
func (n *TaskNode) appNotesLocked() []AppTaskNote {
	notes := cloneAppTaskRecord(n.app).Notes
	known := map[uint64]bool{}
	for i := range notes {
		known[notes[i].Direction] = true
		// EVERY NOTE THE APP'S OWN DOOR WROTE IS THE PERSON'S, because that door
		// is the person's ([Agent.appTaskNote] is reached from a note they typed).
		// It is stated here rather than left to whoever draws it, and the
		// assignment's own label wins wherever the record kept one.
		notes[i].From = AppNoteFromPerson
		if said, ok := n.assignment.direction(notes[i].Direction); ok {
			notes[i].ReadAtStep = said.readAtStep
			notes[i].From = appNoteFrom(said.from)
			if said.clientID != "" {
				notes[i].ClientID = said.clientID
			}
		}
	}
	// Only the person's own directions are added from the assignment, as they
	// always were: another agent's coordination with running work is a delivery
	// to the worker and not a line in the person's own thread with the task.
	for _, said := range n.assignment.directions {
		if said.from == directionFromPerson && !known[said.id] {
			notes = append(notes, AppTaskNote{Direction: said.id, Text: said.words,
				From: appNoteFrom(said.from), ReadAtStep: said.readAtStep, ClientID: said.clientID})
		}
	}
	return notes
}

// An explicit Promote press is permission to start the ordinary task road.
func (a *Agent) promoteQuickDirect(node *TaskNode) error {
	if !node.quickIO.TryLock() {
		return errors.New("stale: that quick task is changing; read it again")
	}
	defer node.quickIO.Unlock()
	node.graph.mu.Lock()
	quick := node.spec.quick
	if quick.promoted || quick.undone {
		node.graph.mu.Unlock()
		return errors.New("that quick task has ended")
	}
	brief := quick.line + "\n\nContinue from these in-place edits; do not discard them."
	for _, capture := range quick.captures {
		brief += "\n" + capture.Path
	}
	title, model, parent, owner := node.spec.title, node.spec.model, node.parent, node.owner
	ground := node.Ground
	source := cloneSource(node.spec.source)
	depends := []uint64{}
	if !node.state.settled() {
		depends = append(depends, node.id)
	}
	cancel := node.cancel
	node.graph.mu.Unlock()
	id := node.graph.reserve()
	err := node.graph.writeAppControl(func() (func(), error) {
		quick.promoted = true
		old := node.app.Version
		node.app.Version++
		return func() { quick.promoted = false; node.app.Version = old }, nil
	})
	if err != nil {
		return err
	}
	spec := taskSpec{admission: a.admissionContext(), source: source, sourceCaptured: true, title: title, named: true, brief: brief, acceptance: taskPersonAcceptance, model: model, ground: ground, where: "in place", mode: TaskModeInPlace, parent: parent, owner: owner, dependsOn: depends}
	node.graph.admit(id, spec)
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *Agent) appProposalViews(cap float64) []AppTaskView {
	out := []AppTaskView{}
	a.mu.Lock()
	for id, question := range a.taskAnswers {
		notice := question.notice
		state := "proposed"
		if question.held {
			state = "held"
		}
		view := AppTaskView{Source: cloneSource(notice.Source), RoutingState: TaskRoutingPicking, ID: strconv.FormatUint(id, 10), Title: notice.Title, Brief: notice.Brief, State: state, Model: notice.Model, Deadline: notice.Deadline, Held: question.held, Cap: cap, Origin: "proposed", Version: question.appVersion + 1}
		for _, depends := range notice.DependsOn {
			view.Waits = append(view.Waits, strconv.FormatUint(depends, 10))
		}
		out = append(out, view)
	}
	a.mu.Unlock()
	return out
}

// appTaskSteps fills a graph node's work as facts, and says honestly how many
// steps it took.
//
// THE LIVE RING IS PREFERRED AND THE JOURNAL IS THE FLOOR, for the same reason
// [appTaskWorkLog] prefers it: a running node's present is in the recorder and
// nowhere else, and a restored node has no recorder at all. They are not merged.
// A node that has both is a node whose ring already holds this life's calls; a
// merge would draw a restarted task's steps twice, once from each record, with
// no field able to say which copy is which.
//
// THE COUNT COMES FROM WHATEVER RECORD ANSWERED. [taskLive.steps] counts ends
// and is exact while a node runs, but a restored node's recorder has counted
// none, so its zero is an absence and the journal's own step count is the fact.
// A node with neither leaves StepsKnown false, and a page says nothing at all
// rather than a reassuring zero.
func appTaskSteps(view *AppTaskView, live taskLiveState, journal string) {
	if len(live.Calls) > 0 || live.Steps > 0 {
		view.Steps, view.StepsKnown = live.Calls, true
		if live.Steps > 0 {
			view.Step = live.Steps
		} else {
			view.Step = appStepsFinished(live.Calls)
		}
		return
	}
	steps := appJournalSteps(journal, appTaskStepsMax)
	if len(steps) == 0 {
		// A node with no record of its work says nothing about its work. Step is
		// left exactly as its notice set it, and StepsKnown stays false unless
		// that notice genuinely counted something.
		view.StepsKnown = view.Step > 0
		return
	}
	view.Steps, view.StepsKnown = steps, true
	view.Step = len(steps)
}

// appPlanTaskSteps fills a plan task's work as facts.
//
// IT READS THE TRAJECTORY ITSELF WHERE THE ROW NAMES ONE. [PlanStep] mirrors
// internal/run's step line but was written before the tool name and the recorded
// exit were fields of it, so a page built from the mirror alone cannot say which
// tool ran or how a command came out. The row carries the path of the record,
// and the record has both.
//
// THE MIRROR IS THE FLOOR AND STAYS TRUTHFUL. Where there is no readable
// trajectory the page's own steps are used, and each is marked legacy: it has a
// command, the engine's facts about whether anything was attempted, and no claim
// at all about which hands were used, which is exactly what that record holds.
func appPlanTaskSteps(row *AppTaskView, page PlanTaskPage) {
	if path := strings.TrimSpace(page.Row.TrajectoryPath); path != "" {
		if steps, _ := appTrajectorySteps(path, appTaskStepsMax); len(steps) > 0 {
			row.Steps, row.StepsKnown = steps, true
			if row.Step == 0 {
				row.Step = len(steps)
			}
			return
		}
	}
	if len(page.Steps) == 0 {
		return
	}
	for _, step := range page.Steps {
		row.Steps = append(row.Steps, AppTaskStep{
			ID:          AppStepTrajectory.prefix() + strconv.Itoa(step.Step),
			Command:     appStepText(step.Command),
			Detail:      appStepText(step.Observation),
			Step:        step.Step,
			Source:      AppStepTrajectory,
			HarnessMade: step.NotRun,
			Refused:     step.Refused,
			Legacy:      true,
			Text:        appStepText(step.Command),
		})
	}
	row.StepsKnown = true
	if row.Step == 0 {
		row.Step = len(page.Steps)
	}
}

// appStepsFinished is how many of a projection's steps the record has settled.
// It is the figure a page may call a step count; a step still in flight has not
// been taken yet.
func appStepsFinished(steps []AppTaskStep) int {
	n := 0
	for _, step := range steps {
		if !step.Running {
			n++
		}
	}
	return n
}

// A restored node reads its existing journal when no live work-log tail remains.
func appTaskWorkLog(live []string, journal string) []string {
	if len(live) > 0 || journal == "" {
		return live
	}
	out := []string{}
	for _, entry := range ReadTranscript(journal).Entries {
		if entry.Role == "tool" || entry.Role == "assistant" {
			out = append(out, entry.Hint+" "+entry.Text)
		}
	}
	return out
}

// A stale checkpoint may name a proposal from the previous engine lifetime.
func (a *Agent) appPromoteQuick(node *TaskNode) error {
	node.graph.mu.Lock()
	promotion, promoted, active := node.spec.quick.promotion, node.spec.quick.promoted, !node.state.settled()
	node.graph.mu.Unlock()
	if promoted {
		return errors.New("that quick task was already promoted")
	}
	if promotion != 0 {
		if owner := node.graph.quickProposalOwner(node); owner != nil {
			if handled, err := owner.appProposalAction(promotion, AppTaskIntent{ID: strconv.FormatUint(promotion, 10), Action: "start"}); handled {
				return err
			}
		}
		if active {
			return errors.New("stale: that promotion is still opening; read it again")
		}
	}
	return a.promoteQuickDirect(node)
}

// A quick worker's promotion belongs to the person, even inside a nested task.
func (g *TaskGraph) quickProposalOwner(node *TaskNode) *Agent {
	if g.home != nil {
		return g.home
	}
	return g.runner(node)
}
func (a *Agent) taskProposalOwner(spec taskSpec) *Agent {
	if !spec.irreversible || !a.config.InTask {
		return a
	}
	if graph := a.tasker(); graph != nil && graph.home != nil {
		return graph.home
	}
	return a
}
