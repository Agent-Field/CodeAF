package factory

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ── A WORKSTREAM'S TASKS (Workstreams v5, SPEC §2.2) ──────────────────────
//
// A task is one piece of work with ONE OWNER. Who does it varies (Afi, a chat,
// the Factory, a person); who answers for it does not. The desk keeps the same
// shape in its own SQLite for a Personal workstream (codeaf-desk
// crates/workstreams), and both stores enforce the rules in this file:
//
//   - ONLY THE OWNER ANSWERS AN ASK. With owner Everyone, anyone who can edit
//     the workstream may, the first answer decides, and AnsweredBy records who.
//     An Ask unanswered for a day is also the workstream's fallback person's.
//   - ONLY THE OWNER TICKS A PERSON TASK. Ticking it releases the tasks that
//     wait on it.
//   - WAITING ON IS THE ONLY ORDERING TOOL: tasks of the same workstream, never
//     itself, never a circle.
//   - A REPLY ON A DONE TASK REOPENS IT, so the history stays one thread.
//   - A FACTORY JOB IN THE WORKSTREAM IS A TASK. It is derived from the job on
//     every read (JobTask) and never copied; what a person sets on it (its
//     owner, its heading, what it waits on, its due date) is kept as a small
//     overlay under the job's stable id, "job:<n>".
//   - AFI NEVER ACTS ON A COMMENT. It may hang an offer under one; anyone may
//     take it, and the owner is told and may undo it for five seconds.
//
// The words a person reads for a state are the app's own: Failed, Interrupted,
// Stopped by you, Didn't report back.

// The task states.
const (
	TaskUpNext      = "up_next"
	TaskRunning     = "running"
	TaskNeeds       = "needs"
	TaskDone        = "done"
	TaskFailed      = "failed"
	TaskInterrupted = "interrupted"
	TaskStopped     = "stopped"
	TaskNoReport    = "no_report"
)

// The kinds of owner, doer and origin.
const (
	TaskOwnerPerson   = "person"
	TaskOwnerEveryone = "everyone"

	TaskByAfi     = "afi"
	TaskByChat    = "chat"
	TaskByFactory = "factory"
	TaskByPerson  = "person"
	TaskByOrder   = "order"
)

// The kinds of event in a task's thread.
const (
	TaskEventComment     = "comment"
	TaskEventWorked      = "worked"
	TaskEventRequestNote = "request_note"
	TaskEventOffer       = "offer"
	TaskEventAnswered    = "answered"
	TaskEventPosted      = "posted"
	TaskEventState       = "state"
	TaskEventHanded      = "handed"
	TaskEventMoved       = "moved"
	TaskEventChecked     = "checked"
)

// The states of an offer.
const (
	OfferOpen      = "open"
	OfferTaken     = "taken"
	OfferDismissed = "dismissed"
)

// The numbers the rules are made of. ONE SOURCE: the desk's wsTasksModel.ts
// spells the same three.
const (
	// TaskFallbackAfter is how long an Ask waits on its owner before the
	// workstream's fallback person may answer it too.
	TaskFallbackAfter = 24 * time.Hour
	// TaskOfferUndo is how long the owner may undo a taken offer.
	TaskOfferUndo = 5 * time.Second
	// TaskJobPrefix is the id a Factory job is shown under: "job:<n>".
	TaskJobPrefix = "job:"
)

// The most one task or one thread may hold.
const (
	taskTitleMost   = 300
	taskRequestMost = 8 << 10
	taskTextMost    = 16 << 10
	taskWaitsMost   = 50
	taskResultsMost = 50
	taskOptionsMost = 8
	// TasksMost is how many tasks one workstream keeps.
	TasksMost = 5000
)

// PersonRef names a person: their handle on an organization, and the words
// to show (the handle until a client knows their name).
type PersonRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TaskOwner is who answers for a task: a person, or everyone.
type TaskOwner struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// TaskDoneBy is who does the work.
type TaskDoneBy struct {
	Kind   string `json:"kind"`
	ChatID string `json:"chat_id,omitempty"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Job    int    `json:"job,omitempty"`
}

// TaskCameFrom is where a task came from.
type TaskCameFrom struct {
	Kind    string `json:"kind"`
	ChatID  string `json:"chat_id,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Issue   string `json:"issue,omitempty"`
	OrderID string `json:"order_id,omitempty"`
}

// TaskGroup is a heading that groups tasks. It is not a task.
type TaskGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// TaskResult is one thing a task made: a file in Files, or an address.
type TaskResult struct {
	Kind   string `json:"kind"`
	FileID string `json:"file_id,omitempty"`
	URL    string `json:"url,omitempty"`
	Label  string `json:"label,omitempty"`
}

// TaskAskOption is one answer an Ask offers.
type TaskAskOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
}

// TaskAsk is the one question a task waits on.
type TaskAsk struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Detail     string          `json:"detail"`
	Options    []TaskAskOption `json:"options"`
	AskedAt    string          `json:"asked_at"`
	LaterUntil string          `json:"later_until,omitempty"`
}

// WorkstreamTask is one task.
type WorkstreamTask struct {
	ID          string       `json:"id"`
	WS          string       `json:"ws"`
	Rev         int          `json:"rev"`
	Title       string       `json:"title"`
	Request     string       `json:"request"`
	Owner       TaskOwner    `json:"owner"`
	DoneBy      TaskDoneBy   `json:"done_by"`
	CameFrom    TaskCameFrom `json:"came_from"`
	State       string       `json:"state"`
	StateReason string       `json:"state_reason,omitempty"`
	// Step is where a Factory task's job is, "Step 4 of 4, Review"; absent on
	// every other task and on a job that is not running or waiting.
	Step       string       `json:"step,omitempty"`
	WaitingOn  []string     `json:"waiting_on"`
	Due        string       `json:"due,omitempty"`
	Group      *TaskGroup   `json:"group,omitempty"`
	Results    []TaskResult `json:"results"`
	Check      string       `json:"check,omitempty"`
	Ask        *TaskAsk     `json:"ask,omitempty"`
	CostUSD    *float64     `json:"cost_usd,omitempty"`
	CreatedBy  PersonRef    `json:"created_by"`
	CreatedAt  string       `json:"created_at"`
	UpdatedAt  string       `json:"updated_at"`
	DoneAt     string       `json:"done_at,omitempty"`
	AnsweredBy *PersonRef   `json:"answered_by,omitempty"`
}

// TaskEventBy is who an event in the thread is from.
type TaskEventBy struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// TaskOffer is Afi's offer under a comment.
type TaskOffer struct {
	ForEvent string     `json:"for_event"`
	State    string     `json:"state"`
	TakenBy  *PersonRef `json:"taken_by,omitempty"`
}

// WorkstreamTaskEvent is one line of a task's thread.
type WorkstreamTaskEvent struct {
	ID        string      `json:"id"`
	Task      string      `json:"task"`
	At        string      `json:"at"`
	By        TaskEventBy `json:"by"`
	Kind      string      `json:"kind"`
	Text      string      `json:"text"`
	Steps     int         `json:"steps,omitempty"`
	DurationS int         `json:"duration_s,omitempty"`
	Offer     *TaskOffer  `json:"offer,omitempty"`
	UndoUntil string      `json:"undo_until,omitempty"`
}

// TaskInvalid is a task the store will not keep, answered 422.
type TaskInvalid string

func (e TaskInvalid) Error() string { return string(e) }

func taskInvalidf(format string, args ...any) error { return TaskInvalid(fmt.Sprintf(format, args...)) }

// TaskRefused is a rule refusing who the caller is, answered 403.
type TaskRefused string

func (e TaskRefused) Error() string { return string(e) }

// The refusals a person reads.
var (
	ErrTaskNotOwner     error = TaskRefused("only the task's owner can do this")
	ErrTaskNotAnswerer  error = TaskRefused("only the task's owner answers its question")
	ErrTaskNoAsk              = errors.New("this task is not waiting on a question")
	ErrTaskOfferGone          = errors.New("that offer is not open")
	ErrTaskUndoOver           = errors.New("the five seconds to undo this have passed")
	ErrTaskFactoryState       = errors.New("a Factory task's state is its job's: stop, start or answer the job instead")
)

// ── who may do what ──

// TaskActor is the caller as the task rules see them.
type TaskActor struct {
	Handle string
	// Edit is whether they may edit the workstream; view alone reads tasks.
	Edit bool
}

func (a TaskActor) owns(t WorkstreamTask) bool {
	if t.Owner.Kind == TaskOwnerEveryone {
		return a.Edit
	}
	return a.Edit && a.Handle != "" && NormalizeHandle(t.Owner.ID) == a.Handle
}

// AskPastFallback is whether the task's Ask has waited long enough for the
// fallback person to answer it too.
func AskPastFallback(t WorkstreamTask, now time.Time) bool {
	if t.Ask == nil {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, t.Ask.AskedAt)
	return err == nil && now.Sub(at) >= TaskFallbackAfter
}

// MayAnswer is whether a may answer t's Ask: its owner (anyone who edits, for
// Everyone), or the fallback person once the Ask is a day old.
func MayAnswer(t WorkstreamTask, a TaskActor, fallback string, now time.Time) bool {
	if t.Ask == nil || t.State != TaskNeeds || !a.Edit {
		return false
	}
	if a.owns(t) {
		return true
	}
	return fallback != "" && NormalizeHandle(fallback) == a.Handle && AskPastFallback(t, now)
}

// MayTick is whether a may tick (or untick) t: a person task, by its owner.
func MayTick(t WorkstreamTask, a TaskActor) bool {
	return t.DoneBy.Kind == TaskByPerson && a.owns(t)
}

// IsJobTask is whether id names a Factory job's task, and which job.
func IsJobTask(id string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, TaskJobPrefix))
	return n, strings.HasPrefix(id, TaskJobPrefix) && err == nil && n > 0
}

// JobTaskID is the stable id job n is shown under.
func JobTaskID(n int) string { return TaskJobPrefix + strconv.Itoa(n) }

// ── checks ──

// TaskCreate is what a new task is made from.
type TaskCreate struct {
	Title     string        `json:"title"`
	Request   string        `json:"request,omitempty"`
	Owner     *TaskOwner    `json:"owner,omitempty"`
	DoneBy    TaskDoneBy    `json:"done_by"`
	CameFrom  *TaskCameFrom `json:"came_from,omitempty"`
	WaitingOn []string      `json:"waiting_on,omitempty"`
	Due       string        `json:"due,omitempty"`
	Group     *TaskGroup    `json:"group,omitempty"`
	State     string        `json:"state,omitempty"`
}

// NewTask makes a task from in, checked. id is the store's; by is the caller;
// defaultOwner is the owner of work nobody asked for directly (the workstream's
// rule, else its steward). Whoever asked owns it by default: the caller, when
// it came from a person or a chat; the default owner when it came from the
// Factory or a standing order.
func NewTask(ws, id string, in TaskCreate, by PersonRef, defaultOwner TaskOwner, all []WorkstreamTask, now time.Time) (WorkstreamTask, error) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	t := WorkstreamTask{
		ID: id, WS: ws, Rev: 1, Title: strings.TrimSpace(in.Title), Request: strings.TrimSpace(in.Request),
		DoneBy: in.DoneBy, State: TaskUpNext, WaitingOn: cleanIDs(in.WaitingOn), Due: strings.TrimSpace(in.Due),
		Results: []TaskResult{}, CreatedBy: by, CreatedAt: stamp, UpdatedAt: stamp,
	}
	if in.CameFrom != nil {
		t.CameFrom = *in.CameFrom
	} else {
		t.CameFrom = TaskCameFrom{Kind: TaskByPerson, ID: by.ID, Name: by.Name}
	}
	switch {
	case in.Owner != nil:
		t.Owner = *in.Owner
	case t.CameFrom.Kind == TaskByFactory || t.CameFrom.Kind == TaskByOrder:
		t.Owner = defaultOwner
	default:
		t.Owner = TaskOwner{Kind: TaskOwnerPerson, ID: by.ID, Name: by.Name}
	}
	switch in.State {
	case "", TaskUpNext:
	case TaskRunning:
		if t.DoneBy.Kind == TaskByPerson {
			return t, TaskInvalid("a person task is ticked when done; it does not run")
		}
		t.State = TaskRunning
	default:
		return t, TaskInvalid(`a new task is "up_next" or "running"`)
	}
	if in.Group != nil {
		g := groupFor(*in.Group, all)
		t.Group = &g
	}
	if err := CheckTask(t, all); err != nil {
		return t, err
	}
	return t, nil
}

// groupFor is the group a new task joins. A group named with no id joins the
// workstream's group of that name and kind, or starts one: Afi's batch of five
// tasks names "Release 2.4" five times and gets one group.
func groupFor(g TaskGroup, all []WorkstreamTask) TaskGroup {
	g.Name, g.Kind = strings.TrimSpace(g.Name), strings.TrimSpace(g.Kind)
	if g.Kind == "" {
		g.Kind = "batch"
	}
	if g.ID != "" {
		return g
	}
	for _, t := range all {
		if t.Group != nil && t.Group.Kind == g.Kind && strings.EqualFold(t.Group.Name, g.Name) {
			return *t.Group
		}
	}
	g.ID = "grp_" + strings.ToLower(strings.Join(strings.Fields(g.Kind+" "+g.Name), "-"))
	return g
}

// CheckTask checks one task against the workstream's others (all may hold t
// itself, by id, which is ignored).
func CheckTask(t WorkstreamTask, all []WorkstreamTask) error {
	switch {
	case t.Title == "":
		return TaskInvalid("a task needs a title")
	case len(t.Title) > taskTitleMost:
		return taskInvalidf("a task's title is at most %d characters", taskTitleMost)
	case strings.ContainsAny(t.Title, "\n\r"):
		return TaskInvalid("a task's title is one line")
	case len(t.Request) > taskRequestMost:
		return taskInvalidf("a task's request is at most %d characters", taskRequestMost)
	case len(t.Results) > taskResultsMost:
		return taskInvalidf("a task keeps at most %d results", taskResultsMost)
	}
	if err := checkOwner(t.Owner); err != nil {
		return err
	}
	if err := checkDoneBy(t.DoneBy); err != nil {
		return err
	}
	if t.Due != "" {
		if _, err := time.Parse(time.DateOnly, t.Due); err != nil {
			return TaskInvalid("a due date is YYYY-MM-DD")
		}
	}
	if g := t.Group; g != nil {
		if g.Name == "" || (g.Kind != "batch" && g.Kind != "factory" && g.Kind != "order") {
			return TaskInvalid(`a group has a name and a kind: "batch", "factory" or "order"`)
		}
	}
	for _, r := range t.Results {
		if (r.Kind == "file" && r.FileID == "") || (r.Kind == "url" && r.URL == "") || (r.Kind != "file" && r.Kind != "url") {
			return TaskInvalid(`a result is {"kind":"file","file_id"} or {"kind":"url","url","label"}`)
		}
	}
	return CheckWaits(t.ID, t.WaitingOn, all)
}

func checkOwner(o TaskOwner) error {
	switch o.Kind {
	case TaskOwnerEveryone:
		return nil
	case TaskOwnerPerson:
		if strings.TrimSpace(o.ID) == "" {
			return TaskInvalid("a person owner needs their id")
		}
		return nil
	}
	return TaskInvalid(`an owner is {"kind":"person","id","name"} or {"kind":"everyone"}`)
}

func checkDoneBy(d TaskDoneBy) error {
	switch d.Kind {
	case TaskByAfi:
	case TaskByChat:
		if d.ChatID == "" {
			return TaskInvalid("a chat's task names the chat")
		}
	case TaskByFactory:
		if d.Job <= 0 {
			return TaskInvalid("a Factory task names its job")
		}
	case TaskByPerson:
		if d.ID == "" {
			return TaskInvalid("a person's task names the person")
		}
	default:
		return TaskInvalid(`done_by is afi, chat, factory or person`)
	}
	return nil
}

// CheckWaits checks what task id waits on: tasks of the same workstream (all),
// never itself, never a circle.
func CheckWaits(id string, waits []string, all []WorkstreamTask) error {
	if len(waits) > taskWaitsMost {
		return taskInvalidf("a task waits on at most %d others", taskWaitsMost)
	}
	byID := make(map[string]WorkstreamTask, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}
	for _, w := range waits {
		if w == id {
			return TaskInvalid("a task cannot wait on itself")
		}
		if _, ok := byID[w]; !ok {
			return taskInvalidf("%s is not a task in this workstream", w)
		}
	}
	// A circle is a path from one of waits back to id.
	seen := map[string]bool{}
	var reaches func(from string) bool
	reaches = func(from string) bool {
		if from == id {
			return true
		}
		if seen[from] {
			return false
		}
		seen[from] = true
		return slices.ContainsFunc(byID[from].WaitingOn, reaches)
	}
	if id != "" && slices.ContainsFunc(waits, reaches) {
		return TaskInvalid("that would make tasks wait on each other in a circle")
	}
	return nil
}

func cleanIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ── changes ──

// TaskPatch is any subset of a task. Due "" clears the date; ClearGroup and
// ClearAsk clear those. State "done" on a person task is the tick.
type TaskPatch struct {
	Title       *string       `json:"title,omitempty"`
	Request     *string       `json:"request,omitempty"`
	Owner       *TaskOwner    `json:"owner,omitempty"`
	WaitingOn   *[]string     `json:"waiting_on,omitempty"`
	Due         *string       `json:"due,omitempty"`
	Group       *TaskGroup    `json:"group,omitempty"`
	ClearGroup  bool          `json:"-"`
	State       *string       `json:"state,omitempty"`
	StateReason *string       `json:"state_reason,omitempty"`
	Results     *[]TaskResult `json:"results,omitempty"`
	Check       *string       `json:"check,omitempty"`
	Ask         *TaskAsk      `json:"ask,omitempty"`
	ClearAsk    bool          `json:"-"`
	MoveTo      string        `json:"move_to,omitempty"`
}

// TaskChange is what one change did, for the thread and for the tasks it
// released.
type TaskChange struct {
	Task     WorkstreamTask
	Events   []WorkstreamTaskEvent
	Released []WorkstreamTask
}

// ApplyPatch changes t by p for actor a, under the rules, and answers the
// events the thread gains. all is the workstream's tasks (t among them), so
// that ticking releases what waits on it. MoveTo is not applied here: the
// caller moves the task between workstreams. newID makes event ids.
//
// Who may change what (DESIGN "What a task contains"): the title and request,
// its owner and whoever asked; the owner (handing it on), anyone who edits;
// the order (waiting on, due date) and its heading, the owner; the results and
// the check line, whoever does the work, which on a server is anyone who
// edits; the state of a person task, its owner alone.
func ApplyPatch(t WorkstreamTask, p TaskPatch, a TaskActor, by TaskEventBy, all []WorkstreamTask, newID func() string, now time.Time) (TaskChange, error) {
	if !a.Edit {
		return TaskChange{}, ErrTaskNotOwner
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	before := t
	asker := a.Handle != "" && NormalizeHandle(t.CreatedBy.ID) == a.Handle
	var events []WorkstreamTaskEvent
	event := func(kind, text string) {
		events = append(events, WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: kind, Text: text})
	}
	if p.Title != nil || p.Request != nil {
		if !a.owns(t) && !asker {
			return TaskChange{}, TaskRefused("only the task's owner and whoever asked for it can change what it asks")
		}
		if p.Title != nil {
			t.Title = strings.TrimSpace(*p.Title)
		}
		if p.Request != nil {
			t.Request = strings.TrimSpace(*p.Request)
		}
	}
	if p.Owner != nil && *p.Owner != t.Owner {
		if err := checkOwner(*p.Owner); err != nil {
			return TaskChange{}, err
		}
		t.Owner = *p.Owner
		event(TaskEventHanded, "Handed to "+ownerWords(t.Owner))
	}
	if p.WaitingOn != nil || p.Due != nil || p.Group != nil || p.ClearGroup {
		if !a.owns(t) {
			return TaskChange{}, TaskRefused("only the task's owner changes its order and heading")
		}
		if p.WaitingOn != nil {
			t.WaitingOn = cleanIDs(*p.WaitingOn)
		}
		if p.Due != nil {
			t.Due = strings.TrimSpace(*p.Due)
		}
		if p.ClearGroup {
			t.Group = nil
		} else if p.Group != nil {
			g := groupFor(*p.Group, all)
			t.Group = &g
		}
	}
	if p.Results != nil {
		t.Results = slices.Clone(*p.Results)
	}
	if p.Check != nil {
		t.Check = strings.TrimSpace(*p.Check)
		if t.Check != "" && t.Check != before.Check {
			event(TaskEventChecked, t.Check)
		}
	}
	if p.ClearAsk {
		t.Ask = nil
	} else if p.Ask != nil {
		ask := *p.Ask
		if t.Ask != nil && t.Ask.ID == ask.ID && !a.owns(t) && ask.LaterUntil != t.Ask.LaterUntil {
			return TaskChange{}, ErrTaskNotAnswerer
		}
		if err := checkAsk(&ask, stamp); err != nil {
			return TaskChange{}, err
		}
		t.Ask = &ask
		if t.State != TaskDone {
			t.State = TaskNeeds
		}
	}
	sentBack := false
	if p.State != nil && *p.State != t.State {
		if t.DoneBy.Kind == TaskByFactory {
			return TaskChange{}, ErrTaskFactoryState
		}
		if !validState(*p.State) {
			return TaskChange{}, taskInvalidf("%q is not a task state", *p.State)
		}
		// SEND BACK: a done task back to up next, by someone other than its
		// owner or with a reason. It is an act of its own, never a comment's
		// side effect (live use, L2 #8 and L4 F3): anyone who edits may do it,
		// a reviewer included, and the thread and the task's reason say who
		// sent it back and why, so the doer sees it in the Inbox.
		why := ""
		if p.StateReason != nil {
			why = strings.TrimSpace(*p.StateReason)
		}
		back := t.State == TaskDone && *p.State == TaskUpNext && (!MayTick(t, a) || why != "")
		if t.DoneBy.Kind == TaskByPerson && !MayTick(t, a) && !back {
			return TaskChange{}, TaskRefused("only the task's owner ticks it")
		}
		t.State = *p.State
		if back {
			words := "Sent back by " + by.Name
			if why != "" {
				words += ": " + why
			}
			event(TaskEventState, words)
			t.StateReason = words
			sentBack = true
		} else {
			event(TaskEventState, StateWords(t.State))
		}
	}
	if p.StateReason != nil && !sentBack {
		t.StateReason = strings.TrimSpace(*p.StateReason)
	}
	switch {
	case t.State == TaskDone && before.State != TaskDone:
		t.DoneAt, t.Ask = stamp, nil
	case t.State != TaskDone:
		t.DoneAt = ""
	}
	if t.State != TaskFailed && !sentBack {
		t.StateReason = strings.TrimSpace(t.StateReason)
		if p.StateReason == nil {
			t.StateReason = ""
		}
	}
	if err := CheckTask(t, all); err != nil {
		return TaskChange{}, err
	}
	t.Rev++
	t.UpdatedAt = stamp
	ch := TaskChange{Task: t, Events: events}
	if t.State == TaskDone && before.State != TaskDone {
		ch.Released = Released(t.ID, replaced(all, t), by, newID, now)
	}
	return ch, nil
}

func validState(s string) bool {
	switch s {
	case TaskUpNext, TaskRunning, TaskNeeds, TaskDone, TaskFailed, TaskInterrupted, TaskStopped, TaskNoReport:
		return true
	}
	return false
}

func checkAsk(a *TaskAsk, stamp string) error {
	a.Title, a.Detail = strings.TrimSpace(a.Title), strings.TrimSpace(a.Detail)
	if a.Title == "" {
		return TaskInvalid("a question needs its words")
	}
	if len(a.Options) > taskOptionsMost {
		return taskInvalidf("a question offers at most %d answers", taskOptionsMost)
	}
	if a.ID == "" {
		a.ID = "ask_" + stamp
	}
	if a.AskedAt == "" {
		a.AskedAt = stamp
	}
	if a.Options == nil {
		a.Options = []TaskAskOption{}
	}
	return nil
}

func replaced(all []WorkstreamTask, t WorkstreamTask) []WorkstreamTask {
	out := slices.Clone(all)
	for i := range out {
		if out[i].ID == t.ID {
			out[i] = t
		}
	}
	return out
}

// Released is what finishing task id lets start: every Afi or chat task that
// waited on it, is up next, and now waits on nothing unfinished. THEY STAY UP
// NEXT. Running means something is actually doing the work, and the store
// cannot wake a doer; a task flipped to running here sat there for good with
// nothing behind it (live use, L1 F5, L2 #4, L4 B6). Whoever wakes the doer
// marks it running. The answer is those tasks, unchanged, for the thread's
// "Ready to start" line. A person task is only unblocked (it shows no wait),
// and a Factory task's state stays its job's.
func Released(id string, all []WorkstreamTask, by TaskEventBy, newID func() string, now time.Time) []WorkstreamTask {
	done := map[string]bool{}
	for _, t := range all {
		if t.State == TaskDone {
			done[t.ID] = true
		}
	}
	var out []WorkstreamTask
	for _, t := range all {
		if !slices.Contains(t.WaitingOn, id) || t.State != TaskUpNext || (t.DoneBy.Kind != TaskByAfi && t.DoneBy.Kind != TaskByChat) {
			continue
		}
		if slices.ContainsFunc(t.WaitingOn, func(w string) bool { return !done[w] }) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// AnswerTask answers t's Ask with option (an id the Ask offers) or words. The
// first answer decides: an Ask already answered is ErrTaskNoAsk.
func AnswerTask(t WorkstreamTask, option, words string, a TaskActor, by TaskEventBy, fallback string, newID func() string, now time.Time) (TaskChange, error) {
	if t.Ask == nil || t.State != TaskNeeds {
		return TaskChange{}, ErrTaskNoAsk
	}
	if !MayAnswer(t, a, fallback, now) {
		return TaskChange{}, ErrTaskNotAnswerer
	}
	words = strings.TrimSpace(words)
	chose := ""
	if option != "" {
		i := slices.IndexFunc(t.Ask.Options, func(o TaskAskOption) bool { return o.ID == option })
		if i < 0 {
			return TaskChange{}, taskInvalidf("%q is not one of this question's answers", option)
		}
		chose = t.Ask.Options[i].Label
	}
	if chose == "" && words == "" {
		return TaskChange{}, TaskInvalid("an answer is one of the question's answers, or words")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	text := t.Ask.Title + " · " + strings.TrimSpace(strings.Join(nonEmpty(chose, words), " · "))
	who := PersonRef{ID: a.Handle, Name: by.Name}
	t.Ask, t.AnsweredBy, t.State, t.Rev, t.UpdatedAt = nil, &who, TaskRunning, t.Rev+1, stamp
	return TaskChange{Task: t, Events: []WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventAnswered, Text: text}}}, nil
}

func nonEmpty(s ...string) []string {
	return slices.DeleteFunc(slices.Clone(s), func(x string) bool { return x == "" })
}

// TaskNote is a line someone adds to a thread: a comment, Afi's offer under
// one (OfferFor), a private request's one note, or an answer posted from a
// private chat.
type TaskNote struct {
	Kind      string `json:"kind,omitempty"`
	Text      string `json:"text"`
	OfferFor  string `json:"offer_for,omitempty"`
	Steps     int    `json:"steps,omitempty"`
	DurationS int    `json:"duration_s,omitempty"`
}

// Note adds n to t's thread. A comment or a post on a done task REOPENS it
// (Up next), so the history stays one thread; a Factory task's state stays
// its job's. An offer hangs under a comment of the same thread and only Afi
// or a chat makes one.
func Note(t WorkstreamTask, n TaskNote, a TaskActor, by TaskEventBy, thread []WorkstreamTaskEvent, newID func() string, now time.Time) (TaskChange, error) {
	if !a.Edit {
		return TaskChange{}, TaskRefused("commenting on a task needs edit on its workstream")
	}
	if n.Kind == "" {
		n.Kind = TaskEventComment
	}
	n.Text = strings.TrimSpace(n.Text)
	switch {
	case n.Text == "":
		return TaskChange{}, TaskInvalid("a comment needs words")
	case len(n.Text) > taskTextMost:
		return TaskChange{}, taskInvalidf("a comment is at most %d characters", taskTextMost)
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	ev := WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: n.Kind, Text: n.Text}
	switch n.Kind {
	case TaskEventComment, TaskEventPosted, TaskEventRequestNote:
	case TaskEventWorked:
		ev.Steps, ev.DurationS = max(0, n.Steps), max(0, n.DurationS)
	case TaskEventOffer:
		if by.Kind != TaskByAfi && by.Kind != TaskByChat {
			return TaskChange{}, TaskInvalid("only Afi or a chat offers")
		}
		if !slices.ContainsFunc(thread, func(e WorkstreamTaskEvent) bool { return e.ID == n.OfferFor && e.Kind == TaskEventComment }) {
			return TaskChange{}, TaskInvalid("an offer hangs under a comment on the same task")
		}
		ev.Offer = &TaskOffer{ForEvent: n.OfferFor, State: OfferOpen}
	default:
		return TaskChange{}, taskInvalidf("%q is not a line a thread takes", n.Kind)
	}
	// A COMMENT NEVER CHANGES A TASK'S STATE. A person logs a receipt or news
	// on finished work and must not find it undone, with everything after it
	// blocked again (live use, L4 F3). Reopening is its own act: the owner's
	// untick, or Send back (ApplyPatch).
	return TaskChange{Task: t, Events: []WorkstreamTaskEvent{ev}}, nil
}

// SetOffer takes, dismisses or undoes Afi's offer ev. Anyone who edits may
// take or dismiss an open offer; taking it gives the owner five seconds to
// undo (UndoUntil); an undo puts it back open and only the owner, or the one
// who took it, undoes, and only within those seconds.
func SetOffer(t WorkstreamTask, ev WorkstreamTaskEvent, state string, a TaskActor, by TaskEventBy, now time.Time) (WorkstreamTaskEvent, error) {
	if ev.Offer == nil || ev.Kind != TaskEventOffer {
		return ev, ErrTaskOfferGone
	}
	if !a.Edit {
		return ev, ErrTaskNotOwner
	}
	o := *ev.Offer
	switch state {
	case OfferTaken, OfferDismissed:
		if o.State != OfferOpen {
			return ev, ErrTaskOfferGone
		}
		o.State = state
		ev.UndoUntil = ""
		if state == OfferTaken {
			o.TakenBy = &PersonRef{ID: a.Handle, Name: by.Name}
			ev.UndoUntil = now.Add(TaskOfferUndo).UTC().Format(time.RFC3339Nano)
		}
	case OfferOpen:
		if o.State != OfferTaken {
			return ev, ErrTaskOfferGone
		}
		took := o.TakenBy != nil && NormalizeHandle(o.TakenBy.ID) == a.Handle
		if !a.owns(t) && !took {
			return ev, ErrTaskNotOwner
		}
		until, err := time.Parse(time.RFC3339Nano, ev.UndoUntil)
		if err != nil || now.After(until) {
			return ev, ErrTaskUndoOver
		}
		o.State, o.TakenBy, ev.UndoUntil = OfferOpen, nil, ""
	default:
		return ev, TaskInvalid(`an offer is "taken", "dismissed" or "open" (undo)`)
	}
	ev.Offer = &o
	return ev, nil
}

// StartAll names a group's Afi and chat tasks that are up next and wait on
// nothing unfinished: the ones Start all can start. It changes nothing.
// Running means a doer is actually at work, and only whoever wakes that doer
// can say so; marking them running here left them running for good with
// nothing behind them (live use, L4 B6). Person tasks and Factory tasks are
// never named (a person starts their own; a Factory job starts at the Factory).
func StartAll(group string, all []WorkstreamTask, now time.Time) []WorkstreamTask {
	done := map[string]bool{}
	for _, t := range all {
		if t.State == TaskDone {
			done[t.ID] = true
		}
	}
	var out []WorkstreamTask
	for _, t := range all {
		if t.Group == nil || t.Group.ID != group || t.State != TaskUpNext || (t.DoneBy.Kind != TaskByAfi && t.DoneBy.Kind != TaskByChat) {
			continue
		}
		if slices.ContainsFunc(t.WaitingOn, func(w string) bool { return !done[w] }) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// StateWords are the words a person reads for a state: the app's own for
// unfinished work.
func StateWords(s string) string {
	switch s {
	case TaskUpNext:
		return "Up next"
	case TaskRunning:
		return "Running"
	case TaskNeeds:
		return "Needs someone"
	case TaskDone:
		return "Done"
	case TaskFailed:
		return "Failed"
	case TaskInterrupted:
		return "Interrupted"
	case TaskStopped:
		return "Stopped by you"
	case TaskNoReport:
		return "Didn't report back"
	}
	return s
}

func ownerWords(o TaskOwner) string {
	if o.Kind == TaskOwnerEveryone {
		return "everyone"
	}
	if o.Name != "" {
		return o.Name
	}
	return o.ID
}

// ── a Factory job, read as a task ──

// JobTaskState is a job's task state and, for Failed, its reason. KEEP IN STEP
// WITH the desk's wsTasksModel.ts jobTaskState: the same job reads the same in
// Personal and on an organization.
//
//   - done → Done; a person's stop → Stopped by you;
//   - a failure: the machine went silent (machine_lost) → Didn't report back;
//     a crash or a lost connection → Interrupted; anything else → Failed;
//   - waiting on a person → Needs someone; running → Running;
//   - queued or in Backlog → Up next.
func JobTaskState(it Item) (string, string) {
	js := JobState(it)
	switch {
	case js == "done":
		return TaskDone, ""
	case StoppedByPerson(it):
		return TaskStopped, ""
	case it.Failure != nil:
		switch it.Failure.Code {
		case CodeMachineLost:
			return TaskNoReport, ""
		case CodeCrash, CodeTransportError:
			return TaskInterrupted, ""
		}
		reason := it.Failure.Summary
		if reason == "" {
			reason = it.Failure.Detail
		}
		return TaskFailed, reason
	case js == "needs_you":
		return TaskNeeds, ""
	case js == "running":
		return TaskRunning, ""
	}
	return TaskUpNext, ""
}

// JobTaskAsk is a job's open question as its task's Ask, nil when it waits on
// nobody. A step's question offers its own answers; any other wait is a yes or
// no ("Yes" is the factory's recommendation).
func JobTaskAsk(it Item) *TaskAsk {
	if st, _ := JobTaskState(it); st != TaskNeeds {
		return nil
	}
	at := NeedsSince(it)
	if at.IsZero() {
		at = it.Created
	}
	ask := &TaskAsk{ID: fmt.Sprintf("job:%d:%s", it.ID, at.UTC().Format(time.RFC3339Nano)), AskedAt: at.UTC().Format(time.RFC3339Nano)}
	if q := it.Asking; q != nil && q.With == AskedYou && strings.TrimSpace(q.Question) != "" {
		ask.Title, ask.Detail = strings.TrimSpace(q.Question), strings.TrimSpace(q.Why)
		for _, o := range q.Options {
			ask.Options = append(ask.Options, TaskAskOption{ID: o, Label: o, Recommended: o == q.Pick})
		}
	} else {
		ask.Title = strings.TrimSpace(it.Question)
		if ask.Title == "" {
			ask.Title = "Go on with " + it.Ref() + "?"
		}
	}
	if len(ask.Options) == 0 {
		ask.Options = []TaskAskOption{{ID: "yes", Label: "Yes", Recommended: true}, {ID: "no", Label: "No"}}
	}
	return ask
}

// JobTask is job it read as a task of workstream ws. overlay is what a person
// set on it (owner, heading, waits, due), nil for none; defaultOwner owns a
// job nobody holds. Nothing of the job is copied: every read derives it again.
func JobTask(it Item, ws string, overlay *WorkstreamTask, defaultOwner TaskOwner) WorkstreamTask {
	state, reason := JobTaskState(it)
	created := it.Created.UTC().Format(time.RFC3339Nano)
	t := WorkstreamTask{
		ID: JobTaskID(it.ID), WS: ws, Title: it.Title, Request: firstLines(it.Body, 600),
		DoneBy: TaskDoneBy{Kind: TaskByFactory, Job: it.ID}, CameFrom: TaskCameFrom{Kind: TaskByFactory, Issue: jobIssue(it)},
		State: state, StateReason: reason, WaitingOn: []string{}, Results: []TaskResult{},
		Ask: JobTaskAsk(it), CreatedBy: PersonRef{ID: "factory", Name: "the factory"}, CreatedAt: created, UpdatedAt: created,
		Group: &TaskGroup{ID: "grp_factory", Name: "Factory", Kind: "factory"},
	}
	if who := NormalizeHandle(it.Assignee); who != "" {
		t.Owner = TaskOwner{Kind: TaskOwnerPerson, ID: who, Name: who}
	} else {
		t.Owner = defaultOwner
	}
	if spent := JobLifetimeSpent(it); spent > 0 {
		t.CostUSD = &spent
	}
	if pr := it.PullRequest; pr != nil && pr.URL != "" {
		t.Results = append(t.Results, TaskResult{Kind: "url", URL: pr.URL, Label: fmt.Sprintf("Pull request %d", pr.Number)})
	}
	var claims []string
	for _, c := range it.Proof {
		if c.OK && strings.TrimSpace(c.Text) != "" {
			claims = append(claims, strings.TrimSpace(strings.Join(nonEmpty(strings.TrimSpace(c.Text), strings.TrimSpace(c.Evidence)), " · ")))
		}
	}
	t.Check = strings.Join(claims, "\n")
	if state == TaskRunning || state == TaskNeeds {
		t.Step = jobStepWords(it)
	}
	if state == TaskDone && !it.DoneAt.IsZero() {
		t.DoneAt = it.DoneAt.UTC().Format(time.RFC3339Nano)
		t.UpdatedAt = t.DoneAt
	}
	if o := overlay; o != nil {
		t.Rev, t.Owner, t.WaitingOn, t.Due, t.Group = o.Rev, o.Owner, nonNilIDs(o.WaitingOn), o.Due, o.Group
		if o.UpdatedAt > t.UpdatedAt {
			t.UpdatedAt = o.UpdatedAt
		}
		if o.AnsweredBy != nil {
			t.AnsweredBy = o.AnsweredBy
		}
	}
	return t
}

// JobOverlay is the part of a Factory task a person may set, kept under the
// job's id: its owner, what it waits on, its due date and heading.
func JobOverlay(t WorkstreamTask) WorkstreamTask {
	return WorkstreamTask{
		ID: t.ID, WS: t.WS, Rev: t.Rev, Owner: t.Owner, DoneBy: t.DoneBy, CameFrom: t.CameFrom, State: TaskUpNext,
		WaitingOn: nonNilIDs(t.WaitingOn), Due: t.Due, Group: t.Group, Results: []TaskResult{},
		Title: t.Title, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, AnsweredBy: t.AnsweredBy,
	}
}

func nonNilIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// jobStepWords is "Step 4 of 4, Review": the one step count ([StepCount])
// and the step the run is on, by its name.
func jobStepWords(it Item) string {
	done, total := StepCount(it)
	if total == 0 {
		return ""
	}
	at := min(done+1, total)
	name := ""
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		name = s.Phases[s.Cur].Name
	}
	if name == "" {
		return fmt.Sprintf("Step %d of %d", at, total)
	}
	return fmt.Sprintf("Step %d of %d, %s", at, total, strings.ToUpper(name[:1])+name[1:])
}

func jobIssue(it Item) string {
	if it.SourceIssue != "" {
		return it.SourceIssue
	}
	if it.Kind == KindIssue {
		return it.Ref()
	}
	return ""
}

func firstLines(s string, most int) string {
	s = strings.TrimSpace(s)
	if len(s) <= most {
		return s
	}
	cut := strings.LastIndexAny(s[:most], " \n")
	if cut < most/2 {
		cut = most
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// ── when the people or the files change under a task ──

// HandOn hands t to `to` when its owner, or the person who does it, has left
// the workstream (removed, by normalized handle): the rules' fallback, else
// the steward. Done work keeps who did it; a task nobody left is unchanged
// (false). The thread says "Handed to <to>", as a hand-off by hand does.
func HandOn(t WorkstreamTask, removed map[string]bool, to string, by TaskEventBy, newID func() string, now time.Time) (WorkstreamTask, []WorkstreamTaskEvent, bool) {
	to = NormalizeHandle(to)
	if t.State == TaskDone || to == "" || removed[to] {
		return t, nil, false
	}
	changed := false
	if t.Owner.Kind == TaskOwnerPerson && removed[NormalizeHandle(t.Owner.ID)] {
		t.Owner, changed = TaskOwner{Kind: TaskOwnerPerson, ID: to, Name: to}, true
	}
	if t.DoneBy.Kind == TaskByPerson && removed[NormalizeHandle(t.DoneBy.ID)] {
		t.DoneBy, changed = TaskDoneBy{Kind: TaskByPerson, ID: to, Name: to}, true
	}
	if !changed {
		return t, nil, false
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	t.Rev, t.UpdatedAt = t.Rev+1, stamp
	return t, []WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventHanded, Text: "Handed to " + to}}, true
}

// FileChangedAskID prefixes the Ask a changed file puts on a running task.
const FileChangedAskID = "ask_file_"

// FlagForFile marks a RUNNING task whose agent read `file` before someone
// changed it (DESIGN: "Marco's running task is flagged because a file it read
// has changed, and asks him whether to continue"). An Afi, chat or person task
// stops on a question to its owner, either answer carries on with the new
// version and the thread says which; a Factory task's state is its job's, so
// it only gains the line. Anything not running is unchanged (false).
func FlagForFile(t WorkstreamTask, file string, by TaskEventBy, newID func() string, now time.Time) (WorkstreamTask, []WorkstreamTaskEvent, bool) {
	if t.State != TaskRunning {
		return t, nil, false
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	line := WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventState, Text: file + " changed while this ran"}
	if _, job := IsJobTask(t.ID); job || t.DoneBy.Kind == TaskByFactory {
		return t, []WorkstreamTaskEvent{line}, true
	}
	t.State, t.Rev, t.UpdatedAt = TaskNeeds, t.Rev+1, stamp
	t.Ask = &TaskAsk{
		ID: FileChangedAskID + newID(), Title: file + " changed while this ran. Carry on?",
		Detail: "The work so far read the earlier version.",
		Options: []TaskAskOption{
			{ID: "carry_on", Label: "Carry on with the new version", Recommended: true},
			{ID: "start_again", Label: "Start again from the new version"},
		},
		AskedAt: stamp,
	}
	return t, []WorkstreamTaskEvent{line}, true
}
