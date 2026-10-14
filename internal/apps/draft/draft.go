// Package draft is the Builder's draft store: the journal a task worker's
// `codeaf app` verbs write one checked piece at a time, the fold that turns the
// journal into one State, and the pure render of that State into the app's
// definition files (docs/apps/builder-loop/DESIGN.md §4). It imports spec and
// nothing that holds records, so the worker's shell can reach it without any
// service of the running server.
package draft

import _ "embed"

//go:embed testdata/crm.sh
var crmScript string

const (
	JournalFile = "draft.jsonl"
	LockFile    = "draft.lock"
	SocketFile  = "builder.sock"
	// AddrFile names the socket when its path is too long to sit in the folder.
	AddrFile = "builder.addr"
	// DefaultsFile holds the create-sheet facts the Builder saves beside the
	// journal, so `show` can render the same files the Builder renders.
	DefaultsFile = "defaults.json"
	// DirEnv overrides where the CLI looks for the draft; without it the CLI
	// reads dir($PLANDB_DB), then walks up from its working directory.
	DirEnv = "CODEAF_APP_DRAFT"
)

// Author says who wrote an entry: the worker, the owner from the plan card, or
// codeaf itself (the done phase after Check passed).
type Author string

const (
	AuthorBuilder Author = "builder"
	AuthorOwner   Author = "owner"
	AuthorCodeaf  Author = "codeaf"
)

// Piece names what one entry changed.
type Piece struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// Entry is one journal line.
type Entry struct {
	Seq    int      `json:"seq"`
	At     string   `json:"at"`
	Author Author   `json:"author"`
	Verb   string   `json:"verb"`
	Args   []string `json:"args,omitempty"`
	Body   string   `json:"body,omitempty"`
	Piece  Piece    `json:"piece"`
	// Line is the owner-readable sentence for the plan card and the log. It is
	// never a verb and never an error.
	Line string `json:"line"`
}

// Phase is where the draft is in the build.
type Phase string

const (
	PhasePlanning Phase = "planning"
	PhasePlanned  Phase = "planned"
	PhaseBuilding Phase = "building"
	PhaseDone     Phase = "done"
)

// Defaults are the facts Render takes from the create sheet, not the worker.
type Defaults struct {
	AppID string `json:"app_id"`
	Space string `json:"space,omitempty"`
	// Ask is the owner's paragraph, verbatim; plan.json carries it.
	Ask         string  `json:"ask,omitempty"`
	AgentModel  string  `json:"agent_model,omitempty"`
	BudgetMonth float64 `json:"budget_month,omitempty"`
	Kit         int     `json:"kit,omitempty"`
	// Tint is the Builder's suggestion; the worker's own `tint` wins.
	Tint string `json:"tint,omitempty"`
}

// CRMScript is the golden CRM as the verbs a worker types, for the Builder's
// teaching page. A test runs it through Main and requires Render to equal the
// golden CRM, so the page and the verbs cannot drift apart.
func CRMScript() string { return crmScript }
