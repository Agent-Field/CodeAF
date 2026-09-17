package plandb

import "time"

// The model is the earlier store's own shape, carried over rather than
// reinvented (docs/design/plandb-cli/DESIGN.md): one Status ladder, one
// dependency vocabulary, and the two graphs — containment and dependency —
// every law in store.go is written against. What changed in the adaptation is
// written at the field or the function that changed it, not here.

type Status string

const (
	StatusPending   Status = "pending"
	StatusReady     Status = "ready"
	StatusClaimed   Status = "claimed"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type DepKind string

const (
	DepFeedsInto DepKind = "feeds_into"
	DepBlocks    DepKind = "blocks"
	DepSuggests  DepKind = "suggests"
)

type Dependency struct {
	TaskID string  `json:"task_id"`
	Kind   DepKind `json:"kind,omitempty"`
}

type Effect string

const (
	EffectObserve         Effect = "observe"
	EffectReversibleWrite Effect = "reversible_write"
	EffectExternalAction  Effect = "external_action"
	EffectIrreversible    Effect = "irreversible"
	EffectMixed           Effect = "mixed"
)

type ResourceClaim struct {
	URI  string `json:"uri"`
	Mode string `json:"mode"`
}

type TaskSpec struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description,omitempty"`
	Kind         string          `json:"kind,omitempty"`
	ParentID     string          `json:"parent_id,omitempty"`
	Dependencies []Dependency    `json:"dependencies,omitempty"`
	Priority     int             `json:"priority,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
	Resources    []ResourceClaim `json:"resources,omitempty"`
	Effect       Effect          `json:"effect,omitempty"`
	// parallel and isolation say how two running tasks may share the machine,
	// and their DEFAULT IS PARALLEL UNLESS DECLARED OTHERWISE. The earlier port
	// defaulted serial, which would have made every store-driven dispatch one
	// at a time and quietly unmade the loop the belt is measuring.
	Parallel  string `json:"parallel,omitempty"`
	Isolation string `json:"isolation,omitempty"`
	// The contract fields below stay on the type because the store persists
	// them and its laws read them — an evidence requirement makes `done` answer
	// with evidence — while no CLI verb requires any of them. An empty field is
	// the ordinary case.
	Role                 string   `json:"role,omitempty"`
	ContextInputs        []string `json:"context_inputs,omitempty"`
	Deliverables         []string `json:"deliverables,omitempty"`
	EvidenceRequirements []string `json:"evidence_requirements,omitempty"`
	Agent                string   `json:"agent,omitempty"`
	Acceptance           string   `json:"acceptance,omitempty"`
}

// TaskPatch is the contract half of a task that may be revised before it
// executes. Parent and dependency rewrites stay graph operations on purpose:
// a revision that could rewrite the ready frontier silently would make
// "ready" a word that means different things a minute apart.
type TaskPatch struct {
	Title                *string
	Description          *string
	Kind                 *string
	Priority             *int
	Capabilities         *[]string
	Resources            *[]ResourceClaim
	Effect               *Effect
	Parallel             *string
	Isolation            *string
	Role                 *string
	ContextInputs        *[]string
	Deliverables         *[]string
	EvidenceRequirements *[]string
	Agent                *string
	Acceptance           *string
}

type Task struct {
	TaskSpec
	Status    Status `json:"status"`
	Composite bool   `json:"composite"`
	// Paused is the status-independent hold on a task and everything under it.
	// A paused task keeps the status it had — ready stays ready — and leaves
	// the ready frontier whole while the flag is set; Resume clears it. It is
	// not a rung of the status ladder, which is why it lives here beside the
	// containment flag and not among the statuses.
	Paused bool `json:"paused,omitempty"`
	// Project and Chat are the row's tags: the run it belongs to and the
	// conversation it was made in. They are not the caller's to set — a task
	// inherits them from its parent — so they live beside the status ladder
	// and not on the spec. A row made before the tags existed carries the
	// empty string.
	Project     string    `json:"project,omitempty"`
	Chat        string    `json:"chat,omitempty"`
	ClaimedBy   string    `json:"claimed_by,omitempty"`
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	Evidence    []string  `json:"evidence,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// Note is a task-scoped message one worker leaves for the others working
// around the same task — the CLI's `task note`/`task notes`, which the
// earlier port did not carry. Context (below) is the project-wide cousin;
// the two stay separate because a note is about one task and a context entry
// is about the run.
type Note struct {
	ID      string    `json:"id"`
	TaskID  string    `json:"task_id"`
	Agent   string    `json:"agent,omitempty"`
	Body    string    `json:"body"`
	Project string    `json:"project,omitempty"`
	Chat    string    `json:"chat,omitempty"`
	At      time.Time `json:"at"`
}

type ContextEntry struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id,omitempty"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Project   string    `json:"project,omitempty"`
	Chat      string    `json:"chat,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Filter narrows a reading verb to the rows carrying one project tag and one
// chat tag. THE ZERO VALUE FILTERS NOTHING: an empty field is a wildcard, so
// a caller that names neither gets every row — the answer the store gave
// before the tags existed.
type Filter struct {
	Project string
	Chat    string
}

// admits reports whether a row's tags pass the filter. An empty filter field
// is a wildcard, which is what makes the zero Filter keep everything.
func (f Filter) admits(project, chat string) bool {
	return (f.Project == "" || f.Project == project) && (f.Chat == "" || f.Chat == chat)
}

// firstFilter answers the filter a reading verb was handed. The verbs take it
// variadically so every call site that names no filter keeps its argument
// list; no filter is the zero Filter, which keeps everything.
func firstFilter(filters []Filter) Filter {
	if len(filters) == 0 {
		return Filter{}
	}
	return filters[0]
}

type Summary struct {
	Project   string `json:"project"`
	RootID    string `json:"root_id"`
	Total     int    `json:"total"`
	Pending   int    `json:"pending"`
	Ready     int    `json:"ready"`
	Running   int    `json:"running"`
	Done      int    `json:"done"`
	Failed    int    `json:"failed"`
	Cancelled int    `json:"cancelled"`
	// ProjectSpend and ChatSpend are the ledger's per-project and per-chat
	// totals, read from the spend table — the dollars spent and the calls
	// that spent them. A store that has never been charged carries neither,
	// and --json omits both.
	ProjectSpend map[string]SpendTotal `json:"project_spend,omitempty"`
	ChatSpend    map[string]SpendTotal `json:"chat_spend,omitempty"`
}

// SpendTotal is one project's or one chat's share of the ledger: the dollars
// spent under that tag and the number of calls that spent them.
type SpendTotal struct {
	USD   float64 `json:"usd"`
	Calls int     `json:"calls"`
}

type BlockedTask struct {
	Task    *Task    `json:"task"`
	Reasons []string `json:"reasons"`
}

type ReadySet struct {
	Runnable []*Task       `json:"runnable"`
	Blocked  []BlockedTask `json:"blocked,omitempty"`
}
