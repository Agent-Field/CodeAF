package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── A WORKSTREAM'S TASKS ON THE WIRE (Workstreams v5, SPEC §2.2) ──────────
//
//	GET   /v1/workstreams/{id}/tasks                    -> WorkstreamTasks: the kept tasks and every job of the workstream as a task
//	POST  /v1/workstreams/{id}/tasks                    TaskCreate -> WorkstreamTask (201)
//	GET   /v1/workstreams/{id}/tasks/{tid}              -> WorkstreamTask
//	PATCH /v1/workstreams/{id}/tasks/{tid}  If-Match:task rev  TaskPatchBody -> WorkstreamTask, 412 stale
//	GET   /v1/workstreams/{id}/tasks/{tid}/events       -> WorkstreamTaskEvents, oldest first
//	POST  /v1/workstreams/{id}/tasks/{tid}/answer       TaskAnswer -> WorkstreamTask; 409 when already answered
//	POST  /v1/workstreams/{id}/tasks/{tid}/comment      TaskComment -> WorkstreamTaskEvent (201)
//	POST  /v1/workstreams/{id}/tasks/start_all          TasksStartAll -> WorkstreamTasks, the tasks it started
//
// Reading needs view on the workstream; every write needs edit. Answering an
// Ask is its owner's (with Everyone, anyone who edits; after a day, also the
// workstream's fallback), and ticking a person task is its owner's alone. A
// Factory job's task ("job:<n>") is derived from the job: its state and Ask
// are the job's, and answering it answers the job through the job's own door
// and rules. Every change publishes workstream.changed with part "tasks" and
// the task's id.

// PartTasks is the part a task change names.
const PartTasks = "tasks"

// The task shapes are the factory's own.
type (
	WorkstreamTask      = factory.WorkstreamTask
	WorkstreamTaskEvent = factory.WorkstreamTaskEvent
	TaskCreate          = factory.TaskCreate
)

// WorkstreamTasks is a list of tasks.
type WorkstreamTasks struct {
	Tasks []WorkstreamTask `json:"tasks"`
}

// WorkstreamTaskEvents is a task's thread.
type WorkstreamTaskEvents struct {
	Events []WorkstreamTaskEvent `json:"events"`
}

// TaskPatchBody is a task change: any subset of factory.TaskPatch's fields;
// "group": null and "ask": null clear those, "due": "" clears the date.
// move_to is the workstream to move it to (edit is needed on both).
type TaskPatchBody = map[string]any

// TaskAnswer answers a task's Ask: one of its options by id, or words.
type TaskAnswer struct {
	// Ask names which of the task's Asks is answered; absent, its own.
	Ask    string `json:"ask,omitempty"`
	Option string `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}

// TaskViewSet is one person's view on someone else's Ask: Text "" takes it
// away.
type TaskViewSet struct {
	Ask  string `json:"ask"`
	Text string `json:"text"`
}

// DecisionReplace is the words a decision is replaced with.
type DecisionReplace struct {
	Text string `json:"text"`
}

// TaskComment adds a line to a thread (kind comment, the default; posted;
// request_note; worked; or offer with offer_for), or, with Offer, takes,
// dismisses or undoes an offer already there.
type TaskComment struct {
	factory.TaskNote
	Offer *TaskOfferSet `json:"offer,omitempty"`
}

// TaskOfferSet names an offer event and the state it moves to: "taken",
// "dismissed", or "open" to undo a take within its five seconds.
type TaskOfferSet struct {
	Event string `json:"event"`
	State string `json:"state"`
}

// TasksStartAll names the group whose up-next tasks start.
type TasksStartAll struct {
	Group string   `json:"group"`
	IDs   []string `json:"ids"`
}

// RequestableWorkstream is one workstream a person may send a request into:
// its name, whether they are in it, and who receives it (its owner of
// incoming work, absent when that is Everyone).
type RequestableWorkstream struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Parent   string             `json:"parent,omitempty"`
	In       bool               `json:"in"`
	Receiver *factory.PersonRef `json:"receiver,omitempty"`
}

// RequestableWorkstreams is GET /v1/workstreams/requestable.
type RequestableWorkstreams struct {
	Workstreams []RequestableWorkstream `json:"workstreams"`
}

// RequestSent is what a request into a workstream answers: the task it
// became, by id, title and owner, and the whole task only to someone in it.
type RequestSent struct {
	ID     string                  `json:"id"`
	WS     string                  `json:"ws"`
	WSName string                  `json:"ws_name"`
	Title  string                  `json:"title"`
	Owner  factory.TaskOwner       `json:"owner"`
	Task   *factory.WorkstreamTask `json:"task,omitempty"`
}
