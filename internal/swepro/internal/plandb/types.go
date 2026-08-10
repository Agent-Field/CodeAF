// Package plandb is a faithful Go port of swe-pro's native PlanDB
// (src/plandb/{types,store,persist,cli-bridge}.ts at commit 3b25a1a).
//
// Field names and JSON struct-tag ORDER match the snake_case shape the
// original emits (which itself mirrors the retired standalone Rust CLI), so
// downstream parsers and the TS↔Go differential harness see byte-compatible
// output. Do not reorder struct fields: encoding/json emits declaration
// order, and that order is part of the parity contract.
package plandb

import (
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type TaskID = string
type ProjectID = string

type TaskStatus string

const (
	StatusPending     TaskStatus = "pending"
	StatusReady       TaskStatus = "ready"
	StatusClaimed     TaskStatus = "claimed"
	StatusRunning     TaskStatus = "running"
	StatusDone        TaskStatus = "done"
	StatusDonePartial TaskStatus = "done_partial"
	StatusFailed      TaskStatus = "failed"
	StatusCancelled   TaskStatus = "cancelled"
)

type TaskKind string

type DepKind string

const (
	DepFeedsInto DepKind = "feeds_into"
	DepBlocks    DepKind = "blocks"
	DepSuggests  DepKind = "suggests"
)

type Project struct {
	ID          ProjectID       `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Status      string          `json:"status"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   int64           `json:"updated_at"`
}

type Task struct {
	ID           TaskID            `json:"id"`
	ProjectID    ProjectID         `json:"project_id"`
	ParentTaskID *TaskID           `json:"parent_task_id"`
	IsComposite  bool              `json:"is_composite"`
	Title        string            `json:"title"`
	Description  *string           `json:"description"`
	Status       TaskStatus        `json:"status"`
	Kind         TaskKind          `json:"kind"`
	Priority     jscompat.JSNumber `json:"priority"`
	AgentID      *string           `json:"agent_id"`
	ClaimedAt    *int64            `json:"claimed_at"`
	StartedAt    *int64            `json:"started_at"`
	CompletedAt  *int64            `json:"completed_at"`
	Result       json.RawMessage   `json:"result"`
	Error        *string           `json:"error"`
	Files        []string          `json:"files"`
	Metadata     json.RawMessage   `json:"metadata"`
	Tags         []string          `json:"tags"`
	CreatedAt    int64             `json:"created_at"`
	UpdatedAt    int64             `json:"updated_at"`
}

type Dependency struct {
	FromTask TaskID  `json:"from_task"`
	ToTask   TaskID  `json:"to_task"`
	Kind     DepKind `json:"kind"`
}

type ContextEntry struct {
	ID        string    `json:"id"`
	ProjectID ProjectID `json:"project_id"`
	TaskID    *TaskID   `json:"task_id"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	CreatedAt int64     `json:"created_at"`
}

// DepSpec is one entry of AddTaskInput.Deps. Kind nil means "unspecified"
// (TS `undefined`), which addTask resolves to feeds_into; a non-nil pointer
// to "" is preserved as "" — the TS `?? "feeds_into"` operator only replaces
// nullish values, and the cli-bridge can genuinely produce an empty-string
// kind from a trailing-colon dep spec.
type DepSpec struct {
	TaskID TaskID
	Kind   *DepKind
}

// AddTaskInput mirrors the TS AddTaskInput. Zero values represent TS
// `undefined` except where nil-vs-empty is semantically load-bearing
// (Deps: nil means "unset" and lets insertTask supply its default; an empty
// non-nil slice means "explicitly no deps"). Priority nil means unset (→ 0);
// a pointer to NaN is representable because the bridge's Number() coercion
// can produce it.
type AddTaskInput struct {
	Title       string
	Description *string
	Kind        TaskKind
	Priority    *float64
	Parent      TaskID
	Project     string
	Deps        []DepSpec
	Tags        []string
	Access      string
	Parallel    string
	Worktree    string
	TaskRole    string
	Outputs     []string
	CustomID    TaskID
}

// CoalesceTasksOpts mirrors the TS CoalesceTasksOpts.
type CoalesceTasksOpts struct {
	FileScope   []string
	ScopeTagFor func(mergedDescription string) string // "" return = no tag (TS undefined)
}

// Status mirrors the TS Status counter block; field order is the JSON
// emission order.
type Status struct {
	Total     int `json:"total"`
	Done      int `json:"done"`
	Pending   int `json:"pending"`
	Ready     int `json:"ready"`
	Running   int `json:"running"`
	Claimed   int `json:"claimed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// Snapshot mirrors the TS snapshot() shape; key order is part of the journal
// format ({"k":"s","v":{projects,tasks,contexts,dependencies}}).
type Snapshot struct {
	Projects     []*Project      `json:"projects"`
	Tasks        []*Task         `json:"tasks"`
	Contexts     []*ContextEntry `json:"contexts"`
	Dependencies []Dependency    `json:"dependencies"`
}
