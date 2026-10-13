package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Task struct {
	Version  uint64       `json:"version,omitempty"`
	ID       string       `json:"id"`
	AgentID  string       `json:"agent_id"`
	ParentID string       `json:"parent_id,omitempty"`
	Title    string       `json:"title"`
	Brief    string       `json:"brief"`
	State    string       `json:"state" jsonschema:"enum=proposed,enum=held,enum=waiting,enum=running,enum=paused,enum=your_call,enum=done,enum=stopped,enum=failed,enum=declined,enum=handed_off"`
	WaitsOn  []string     `json:"waits_on"`
	Clock    TaskClock    `json:"clock"`
	StartsAt *time.Time   `json:"starts_at,omitempty"`
	Model    string       `json:"model"`
	Cap      Money        `json:"cap"`
	Step     int          `json:"step"`
	Files    []FileChange `json:"files"`
	Notes    []TaskNote   `json:"notes"`
	Origin   string       `json:"origin"`
	HandedTo *SourceRef   `json:"handed_to,omitempty"`
	Spent    *Money       `json:"spent,omitempty"`
	Elapsed  *int64       `json:"elapsed,omitempty"`
}
type TaskClock struct {
	State    string     `json:"state" jsonschema:"enum=running,enum=held,enum=off"`
	StartsAt *time.Time `json:"starts_at,omitempty"`
	PausesAt *time.Time `json:"pauses_at,omitempty"`
	Seconds  *int       `json:"seconds,omitempty"`
}
type TaskNote struct {
	Text       string `json:"text"`
	ReadAtStep *int   `json:"read_at_step,omitempty"`
}
type TaskGroup struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	TaskIDs []string `json:"task_ids"`
}
type TaskProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
type TasksResponse struct {
	Groups   []TaskGroup  `json:"groups"`
	Tasks    []Task       `json:"tasks"`
	Progress TaskProgress `json:"progress"`
	Spent    *Money       `json:"spent,omitempty"`
	Steps    int          `json:"steps"`
}
type TaskResponse struct {
	Task
	Result  string         `json:"result"`
	WorkLog []DisplayEntry `json:"work_log"`
}
type TaskDecisionRequest struct {
	Verb   string       `json:"verb" jsonschema:"enum=accept,enum=not_right,enum=tell,enum=recheck,enum=let_it_decide"`
	Words  string       `json:"words,omitempty"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type TaskChangedEvent struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
	State string `json:"state"`
	Clock string `json:"clock,omitempty"`
	Step  *int   `json:"step,omitempty"`
}

func taskContract() resourceContract {
	return resourceContract{Name: "task", Types: []any{Task{}, TaskClock{}, TaskNote{}, TaskGroup{}, TaskProgress{}, TasksResponse{}, TaskResponse{}, TaskDecisionRequest{}, TaskChangedEvent{}}, Routes: taskContractRoutes, Events: []eventContract{{"task.created", TaskChangedEvent{}}, {"task.changed", TaskChangedEvent{}}}}
}
