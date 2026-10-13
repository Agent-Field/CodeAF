// Package svc holds the service seams of Apps: one small interface per lane.
// The gateway (internal/apps/bridge) calls them; internal/apps wires them.
// Changing an interface here is a contract change (docs/apps/CONTRACT.md).
package svc

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Owner is the only member of the personal space; it holds every role.
const Owner = "me"

// Role is an app role.
type Role string

const (
	RoleEditor Role = "editor"
	RoleUser   Role = "user"
	RoleViewer Role = "viewer"
)

// Caller is who makes a call. Wave 1: always {Member: Owner, Role: RoleEditor}.
type Caller struct {
	Member string
	Role   Role
	Agent  bool // true when the app agent calls on the member's behalf
	Space  string
	WS     WorkspaceRole
}

// DataMode picks the records a call reads and writes.
type DataMode string

const (
	DataReal   DataMode = "real"   // kept app: records.db
	DataSample DataMode = "sample" // planning/building/preview/check: sample records
)

// Scope is the app-scoped context of every call. The gateway fills Data from
// the app state: kept → real, anything else → sample.
type Scope struct {
	App    string
	Caller Caller
	Data   DataMode
}

// Store (B1) owns app folders, files, state and versions.
type Store interface {
	List(ctx context.Context, caller Caller) ([]bridge.AppSummary, error)
	Get(ctx context.Context, app string) (bridge.AppDetail, error)
	// Create allocates an id from the name and makes the folder in StatePlanning.
	Create(ctx context.Context, caller Caller, name, description string) (bridge.AppSummary, error)
	// Definition returns the current definition: the kept version, or the
	// building copy before Keep.
	Definition(ctx context.Context, app string) (*spec.Bundle, error)
	// WriteFiles replaces definition files of an app that is not kept.
	// Only the Builder calls it (rule 01 and 02).
	WriteFiles(ctx context.Context, app string, files spec.Files) error
	SetState(ctx context.Context, app string, state bridge.AppState) error
	Keep(ctx context.Context, app string) (bridge.AppSummary, error)
	Discard(ctx context.Context, app string) error
}

// Records (B2) is the records store and query compiler. It publishes a
// bridge.RecordDiff on bridge.TopicRecords after every write.
type Records interface {
	Query(ctx context.Context, scope Scope, p bridge.QueryParams) (bridge.QueryResult, error)
	Get(ctx context.Context, scope Scope, p bridge.GetRecordParams) (bridge.Record, error)
	Create(ctx context.Context, scope Scope, p bridge.CreateRecordParams) (bridge.Record, error)
	Update(ctx context.Context, scope Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error)
	Delete(ctx context.Context, scope Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error)
	// Seed replaces the sample records of an app (preview and Check).
	Seed(ctx context.Context, app string, rows map[string][]bridge.Record) error
}

// Sample (B2) generates sample records for a definition.
type Sample interface {
	Generate(ctx context.Context, def *spec.Bundle) (map[string][]bridge.Record, error)
}

// Runs (B4) is the runs engine. It subscribes to records topics for
// triggers and publishes bridge.RunState on bridge.TopicRuns.
type Runs interface {
	Start(ctx context.Context, scope Scope, p bridge.StartRunParams) (bridge.RunState, error)
	Get(ctx context.Context, scope Scope, p bridge.GetRunParams) (bridge.RunState, error)
	List(ctx context.Context, scope Scope, p bridge.ListRunsParams) (bridge.ListRunsResult, error)
}

// Approvals (B4) holds "Approval needed" items and publishes them on
// bridge.TopicApprovals. Runs and the app agent create them.
type Approvals interface {
	Open(ctx context.Context, scope Scope, a bridge.Approval) (bridge.Approval, error)
	List(ctx context.Context, scope Scope, p bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error)
	Decide(ctx context.Context, scope Scope, p bridge.DecideApprovalParams) (bridge.Approval, error)
}

// Connectors (B4) calls connector verbs. In wave 1 every call goes to a fake
// that records the call and returns a canned reply. Outbound verbs never
// reach Call without an approved approval.
type Connectors interface {
	Call(ctx context.Context, scope Scope, tool string, args map[string]any) (any, error)
	Outbound(tool string) bool
}

// Builder (B5) owns apps.create and the plan · write · check · preview loop.
// It publishes bridge.BuildEvent on bridge.TopicBuild. It reads only files,
// the kit, sample records and check reports (rule 01).
type Builder interface {
	Create(ctx context.Context, caller Caller, p bridge.CreateAppParams) (bridge.AppSummary, error)
	EditPlan(ctx context.Context, scope Scope, p bridge.PlanEditParams) (bridge.PlanEditResult, error)
	Write(ctx context.Context, scope Scope) error
	Cancel(ctx context.Context, scope Scope) error
}

// Check (B6) validates a definition: schema, kit law, render with sample
// data, runs on fakes.
type Check interface {
	Run(ctx context.Context, app string, files spec.Files, round int) (bridge.CheckReport, error)
}

// Agent (B7) is the app agent. It streams bridge.AgentEvent on
// bridge.TopicAgent and never writes definition files (rule 02).
type Agent interface {
	Send(ctx context.Context, scope Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error)
}

// Model (B5) is the one model door for the Builder and the app agent.
type Model interface {
	Complete(ctx context.Context, call ModelCall) (ModelReply, error)
}

// ModelCall is one completion request.
type ModelCall struct {
	Model    string
	System   string
	Messages []ModelMessage
	Schema   json.RawMessage // optional JSON Schema the reply must match
	App      string          // charged to this app's budget
}

// ModelMessage is one chat message.
type ModelMessage struct {
	Role string // user | assistant | tool
	Text string
}

// ModelReply is the completion and its cost in USD.
type ModelReply struct {
	Text string
	Cost float64
}

// Event is one published event.
type Event struct {
	Topic string
	Seq   int64
	Data  any
}

// Hub (B3) is in-process pub/sub. A topic ending in "/*" subscribes to
// every topic with that prefix.
type Hub interface {
	Publish(topic string, data any)
	Subscribe(topic string) (events <-chan Event, cancel func())
}

// Caps (B3) checks every call against app.json and the caller's role before
// dispatch (rule 05). Deny returns a *bridge.Error with CodeDenied.
type Caps interface {
	Allow(ctx context.Context, scope Scope, method string, params json.RawMessage) error
}

// Services is everything the gateway dispatches to.
type Services struct {
	SpacesServices
	Store      Store
	Records    Records
	Sample     Sample
	Runs       Runs
	Approvals  Approvals
	Connectors Connectors
	Builder    Builder
	Check      Check
	Agent      Agent
	Model      Model
	Hub        Hub
	Caps       Caps
}

// Config is what every service constructor receives.
type Config struct {
	Root string          // $CODEAF_HOME/apps: one folder per app
	Ctx  context.Context // server lifetime; background work stops when it ends
}

// Every lane package exports exactly this constructor shape:
//
//	func New(cfg svc.Config, all *svc.Services) *Service
//
// all is filled before the first call, so a service reaches its peers through
// it at call time (all.Records, all.Hub, …) and never needs a wiring edit.
