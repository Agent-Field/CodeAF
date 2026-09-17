package plandb

import "time"

// The model is the aforge-v1 store's own shape, carried over rather than
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
	// parallel and isolation keep the reference's meaning — how two running
	// tasks may share the machine — and their DEFAULT is the one the rust CLI
	// models: parallel unless declared otherwise. The aforge-v1 port defaulted
	// serial, which would have made every store-driven dispatch one at a time
	// and quietly unmade the loop the belt is measuring.
	Parallel  string `json:"parallel,omitempty"`
	Isolation string `json:"isolation,omitempty"`
	// The contract fields below stay on the type because the CLI's `done`
	// writes evidence and the reference's task cards carry them; no CLI verb
	// requires any of them. An empty field is the ordinary case.
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
	Status      Status    `json:"status"`
	Composite   bool      `json:"composite"`
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
// around the same task — the rust CLI's `task note`/`task notes`, which the
// aforge-v1 port did not carry. Context (below) is the project-wide cousin;
// the two stay separate because a note is about one task and a context entry
// is about the run.
type Note struct {
	ID     string    `json:"id"`
	TaskID string    `json:"task_id"`
	Agent  string    `json:"agent,omitempty"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

type ContextEntry struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id,omitempty"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
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
}

type BlockedTask struct {
	Task    *Task    `json:"task"`
	Reasons []string `json:"reasons"`
}

type ReadySet struct {
	Runnable []*Task       `json:"runnable"`
	Blocked  []BlockedTask `json:"blocked,omitempty"`
}
