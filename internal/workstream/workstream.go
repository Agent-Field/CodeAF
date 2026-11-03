// Package workstream owns workstream rules and durable state. Adapters supply
// job observations and recorded spend; execution owners stay outside this package.
package workstream

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	DocMax   = 1 << 20
	emptyDoc = `{"type":"doc","content":[]}`
)

var (
	ErrInvalid  = workspace.ErrInvalid
	ErrNotFound = errors.New("workstream target not found")
	ErrConflict = errors.New("workstream conflict")
	ErrCycle    = errors.New("page move would form a cycle")
	ErrTooLarge = errors.New("encoded document exceeds 1 MiB")
)

type ChangedPart string

const (
	PartWorkstream ChangedPart = "workstream"
	PartPlan       ChangedPart = "plan"
	PartPages      ChangedPart = "pages"
	PartRecords    ChangedPart = "records"
	PartGoAheads   ChangedPart = "goaheads"
	PartJobs       ChangedPart = "jobs"
)

// Change is empty for an unchanged write. Adapters emit it only after success.
type Change struct {
	ID    string
	Parts []ChangedPart
}

type VersionConflict struct {
	ID        string
	Version   int
	UpdatedAt time.Time
	UpdatedBy string
}

func (e *VersionConflict) Error() string { return "the version changed since it was read" }

type Wake struct{ Morning, RelatedIssues bool }
type Workstream struct {
	ID           string
	Name         string
	Why          string
	OriginChatID string
	AgentID      string
	Autonomy     string
	HowItWent    string
	ClientID     string
	Wake         Wake
	BudgetUSD    *float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	StartedAt    time.Time
	DoneAt       time.Time
	Stage        string
	Members      []workspace.Ref
	Proposed     int
	Total        int
}
type Create struct {
	Name         string
	Why          string
	OriginChatID string
	AgentID      string
	ClientID     string
	Refs         []workspace.Ref
}
type Patch struct {
	Name, Why, Autonomy *string
	Wake                *Wake
	BudgetUSD           *float64
}

type Job struct {
	Factory string
	Number  int
}

// JobFacts is absent from the facts map until a job was successfully read.
// Individual facts may remain unknown even when its state is known.
type JobFacts struct {
	State   string
	Merged  *bool
	CostUSD *float64
}
type Numbers struct {
	Total            int
	Merged, NeedsYou *int
	SpendUSD         *float64
}

type Source struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	ChatID  string `json:"chat_id,omitempty"`
	Factory string `json:"factory,omitempty"`
	Label   string `json:"label,omitempty"`
}

// Attribution permits later agent tools to use the same domain operations.
// A zero attribution means the person; proposal authors default to agent.
type Attribution struct {
	By   string
	From *Source
}

type Milestone struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type PlanItem struct {
	Kind    string `json:"kind"`
	Factory string `json:"factory,omitempty"`
	Job     int    `json:"job,omitempty"`
	DraftID string `json:"draft_id,omitempty"`
	Title   string `json:"title,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Text    string `json:"text,omitempty"`
}
type Wave struct {
	N           int        `json:"n"`
	Title       string     `json:"title,omitempty"`
	MilestoneID string     `json:"milestone_id,omitempty"`
	AutoStart   bool       `json:"auto_start"`
	Items       []PlanItem `json:"items"`
}
type Plan struct {
	WorkstreamID string
	Version      int
	UpdatedAt    time.Time
	UpdatedBy    string
	Milestones   []Milestone
	Waves        []Wave
}

type PageNode struct {
	ID                string
	WorkstreamID      string
	ParentID          string
	Title             string
	Owner             string
	ReadByJobs        bool
	Position, Version int
	UpdatedAt         time.Time
	UpdatedBy         string
	UpdatedFrom       *Source
	DeletedAt         time.Time
}
type Page struct {
	PageNode
	Doc      json.RawMessage
	Markdown *string
}
type PageVersion struct {
	Version int
	SavedAt time.Time
	SavedBy string
	From    *Source
	Title   string
	Bytes   int
}
type PageCreate struct {
	WorkstreamID string
	ParentID     string
	AfterID      string
	Title        string
	Owner        string
	ReadByJobs   bool
	Doc          json.RawMessage
	Attribution
}
type PagePatch struct {
	Title, Owner *string
	ReadByJobs   *bool
	Attribution
}

type Record struct {
	ID                       string
	WorkstreamID             string
	Kind                     string
	Text                     string
	State                    string
	By                       string
	Source                   *Source
	CreatedAt, DecidedAt     time.Time
	Supersedes, SupersededBy string
}
type RecordPropose struct {
	WorkstreamID, Kind, Text string
	Source                   *Source
	Kept                     bool
	By                       string
}
type RecordReplacement struct {
	Kind, Text string
	Source     *Source
	By         string
}

type GoAhead struct {
	ID                 string
	WorkstreamID       string
	Scope              string
	Factory            string
	MilestoneID        string
	Job, Wave          int
	CapUSD             float64
	EndsAt             time.Time
	State, EndReason   string
	GrantedAt, EndedAt time.Time
}
type GoAheadGrant struct {
	WorkstreamID string
	Scope        string
	Factory      string
	MilestoneID  string
	Job, Wave    int
	CapUSD       float64
	EndsAt       time.Time
}
