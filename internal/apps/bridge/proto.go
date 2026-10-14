package bridge

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Path is where the gateway listens, behind the app transport guard.
const Path = "/app/v1/apps/ws"

// Subprotocol is the WebSocket subprotocol. Browsers cannot set an
// Authorization header on a WebSocket, so the client also offers
// "bearer.<token>" as a second subprotocol; the server never echoes it.
const (
	Subprotocol    = "codeaf.apps.v1"
	BearerProtocol = "bearer."
)

// MessageType is the envelope kind.
type MessageType string

const (
	TypeCall   MessageType = "call"   // client → server: invoke Method
	TypeResult MessageType = "result" // server → client: answer to call/sub/unsub with the same ID
	TypeError  MessageType = "error"  // server → client: failed call/sub with the same ID
	TypeSub    MessageType = "sub"    // client → server: subscribe to Topic
	TypeUnsub  MessageType = "unsub"  // client → server: stop Topic
	TypeEvent  MessageType = "event"  // server → client: one event on Topic
)

// Envelope is every message on the socket, both directions.
type Envelope struct {
	ID     string          `json:"id,omitempty"`
	Type   MessageType     `json:"type"`
	Method string          `json:"method,omitempty"`
	App    string          `json:"app,omitempty"` // app id for every app-scoped method
	Topic  string          `json:"topic,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	Event  json.RawMessage `json:"event,omitempty"`
	Seq    int64           `json:"seq,omitempty"` // per-topic, increasing, events only
}

// Methods, wave 1.
const (
	MethodAppsList        = "apps.list"
	MethodAppsGet         = "apps.get"
	MethodAppsCreate      = "apps.create"
	MethodAppsKeep        = "apps.keep"
	MethodAppsDiscard     = "apps.discard"
	MethodAppSetTint      = "app.setTint"
	MethodBuilderPlanEdit = "builder.plan.edit"
	MethodBuilderWrite    = "builder.write"
	MethodBuilderCancel   = "builder.cancel"
	MethodRecordsQuery    = "records.query"
	MethodRecordsGet      = "records.get"
	MethodRecordsCreate   = "records.create"
	MethodRecordsUpdate   = "records.update"
	MethodRecordsDelete   = "records.delete"
	MethodRunsStart       = "runs.start"
	MethodRunsGet         = "runs.get"
	MethodRunsList        = "runs.list"
	MethodApprovalsList   = "approvals.list"
	MethodApprovalsDecide = "approvals.decide"
	MethodAgentSend       = "agent.send"
	MethodAgentStop       = "agent.stop"
)

// SetTintParams changes a kept app's identity tint.
type SetTintParams struct {
	App  string `json:"app"`
	Tint string `json:"tint"`
}

// Topic builders. Every topic is app-scoped.
func TopicBuild(app string) string               { return "app/" + app + "/build" }
func TopicState(app string) string               { return "app/" + app + "/state" }
func TopicRecords(app, recordType string) string { return "app/" + app + "/records/" + recordType }
func TopicRuns(app string) string                { return "app/" + app + "/runs" }
func TopicApprovals(app string) string           { return "app/" + app + "/approvals" }
func TopicAgent(app, thread string) string       { return "app/" + app + "/agent/" + thread }

// ErrorCode is the typed error code.
type ErrorCode string

const (
	CodeDenied      ErrorCode = "denied"       // capability or role refused (rule 05)
	CodeInvalid     ErrorCode = "invalid"      // params or record values fail validation
	CodeNotFound    ErrorCode = "not_found"    // app, record, run or approval missing
	CodeBudget      ErrorCode = "budget"       // would pass the app or run budget (rule 10)
	CodeCheckFailed ErrorCode = "check_failed" // Check failed after 3 rounds
	CodeConflict    ErrorCode = "conflict"     // wrong app state for the method, stale version
	CodeNotBuilt    ErrorCode = "not_built"    // the lane has not built this service yet
	CodeInternal    ErrorCode = "internal"
)

// Error is the typed error on the wire and in Go. Services return *Error;
// the gateway maps any other error to CodeInternal.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Errorf builds a typed error.
func Errorf(code ErrorCode, message string) *Error { return &Error{Code: code, Message: message} }

// NotBuilt is what every stub service returns.
func NotBuilt(service string) *Error {
	return &Error{Code: CodeNotBuilt, Message: service + " is not built yet"}
}

// ---- apps ----

// AppState is the app lifecycle in wave 1.
type AppState string

const (
	StatePlanning AppState = "planning" // plan.json written, plan card shown
	StateBuilding AppState = "building" // Write · Check running
	StateFailed   AppState = "failed"   // Check failed 3 rounds: Fix it · Cancel
	StatePreview  AppState = "preview"  // runs on sample data and fakes
	StateKept     AppState = "kept"     // v1+, real records
)

// AppSummary is one row of the Apps rail section.
type AppSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Tint        string   `json:"tint,omitempty"`
	Description string   `json:"description,omitempty"`
	State       AppState `json:"state"`
	Version     int      `json:"version"` // 0 until Keep
	Space       string   `json:"space"`
	UpdatedAt   string   `json:"updated_at"`          // RFC 3339
	NeedsYou    int      `json:"needs_you,omitempty"` // open approvals
}

// AppDetail is apps.get: the summary plus the definition files and Needs.
type AppDetail struct {
	AppSummary
	Files map[string]string `json:"files"` // relative path → text (app.json, plan.json, screens/x.json, runs/x.yaml, agent.md)
	Needs []NeedLine        `json:"needs"`
}

// NeedLine is one computed line of Needs on the plan card.
type NeedLine struct {
	Kind string `json:"kind"` // records | connector | ai | budget | approval
	Line string `json:"line"`
}

// AppsListResult answers apps.list.
type AppsListResult struct {
	Apps []AppSummary `json:"apps"`
}

// CreateAppParams is apps.create: the New app sheet.
type CreateAppParams struct {
	Paragraph   string   `json:"paragraph"`
	Model       string   `json:"model,omitempty"`        // app agent model; Builder uses the server default
	BudgetMonth float64  `json:"budget_month,omitempty"` // USD, default 20
	Connectors  []string `json:"connectors,omitempty"`
	JustBuild   bool     `json:"just_build,omitempty"` // skip the plan card, go straight to Write
}

// PlanEdit is one edited plan line, or a free request when ID is "".
type PlanEdit struct {
	Section string `json:"section,omitempty"` // data | automation | screens | agent
	ID      string `json:"id,omitempty"`
	Text    string `json:"text"`
}

// PlanEditParams is builder.plan.edit.
type PlanEditParams struct {
	Edits []PlanEdit `json:"edits"`
}

// PlanEditResult is the rewritten plan with recomputed Needs.
type PlanEditResult struct {
	Plan  spec.Plan  `json:"plan"`
	Needs []NeedLine `json:"needs"`
}

// Empty is the params or result of methods that carry none.
type Empty struct{}

// ---- build events: topic app/<id>/build ----

// BuildStep is a Building row.
type BuildStep string

const (
	StepPlan    BuildStep = "plan"
	StepWrite   BuildStep = "write"
	StepCheck   BuildStep = "check"
	StepPreview BuildStep = "preview"
)

// BuildStatus is the state of one step.
type BuildStatus string

const (
	BuildRunning BuildStatus = "running"
	BuildDone    BuildStatus = "done"
	BuildFailed  BuildStatus = "failed"
)

// BuildEvent is one Builder progress event.
type BuildEvent struct {
	Step   BuildStep    `json:"step"`
	Status BuildStatus  `json:"status"`
	Round  int          `json:"round,omitempty"` // check round 1..3
	Log    string       `json:"log,omitempty"`   // one human line
	Files  []string     `json:"files,omitempty"` // files written in this step
	Plan   *spec.Plan   `json:"plan,omitempty"`  // after plan: the plan card
	Needs  []NeedLine   `json:"needs,omitempty"`
	Check  *CheckReport `json:"check,omitempty"`
	Cost   Cost         `json:"cost"`
	At     string       `json:"at"`
}

// Cost is spend so far against the cap, in USD.
type Cost struct {
	Spent float64 `json:"spent"`
	Cap   float64 `json:"cap"`
}

// CheckReport is the result of one Check round.
type CheckReport struct {
	Pass     bool         `json:"pass"`
	Round    int          `json:"round"`
	Issues   []spec.Issue `json:"issues"`
	Rendered []string     `json:"rendered"` // screen ids rendered with sample data
	Runs     []string     `json:"runs"`     // run ids executed on fakes
}

// ---- records: topic app/<id>/records/<type> ----

// Record is one stored record. AI lists fields whose value an AI set
// (shown "· AI"); a person's edit removes the field from AI (pins win).
type Record struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Version   int            `json:"version"`
	Fields    map[string]any `json:"fields"`
	AI        []string       `json:"ai,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	CreatedBy string         `json:"created_by"`
	UpdatedBy string         `json:"updated_by"`
}

// QueryParams is records.query. Query uses QueryField tokens.
type QueryParams struct {
	Type    string `json:"type"`
	Query   string `json:"query,omitempty"`
	Limit   int    `json:"limit,omitempty"`    // default and max 100
	After   string `json:"after,omitempty"`    // cursor from QueryResult.Next
	CountBy string `json:"count_by,omitempty"` // enum field for StateMeter counts
}

// QueryResult is one page of rows.
type QueryResult struct {
	Rows   []Record       `json:"rows"`
	Next   string         `json:"next,omitempty"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts,omitempty"`
}

// GetRecordParams is records.get.
type GetRecordParams struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// CreateRecordParams is records.create.
type CreateRecordParams struct {
	Type string         `json:"type"`
	Set  map[string]any `json:"set"`
}

// UpdateRecordParams is records.update. Exactly one of ID and IDs.
type UpdateRecordParams struct {
	Type string         `json:"type"`
	ID   string         `json:"id,omitempty"`
	IDs  []string       `json:"ids,omitempty"`
	Set  map[string]any `json:"set"`
}

// UpdateRecordResult lists the written rows.
type UpdateRecordResult struct {
	Rows []Record `json:"rows"`
}

// DeleteRecordParams is records.delete. Exactly one of ID and IDs.
type DeleteRecordParams struct {
	Type string   `json:"type"`
	ID   string   `json:"id,omitempty"`
	IDs  []string `json:"ids,omitempty"`
}

// DeleteRecordResult lists deleted ids.
type DeleteRecordResult struct {
	IDs []string `json:"ids"`
}

// DiffOp is a row diff kind.
type DiffOp string

const (
	DiffUpsert DiffOp = "upsert"
	DiffDelete DiffOp = "delete"
)

// RecordDiff is one event on app/<id>/records/<type>.
type RecordDiff struct {
	Op     DiffOp  `json:"op"`
	Type   string  `json:"type"`
	ID     string  `json:"id"`
	Record *Record `json:"record,omitempty"` // upsert only
	By     string  `json:"by"`
}

// ---- runs: topic app/<id>/runs ----

// RunStatus is a run's state.
type RunStatus string

const (
	RunQueued   RunStatus = "queued"
	RunRunning  RunStatus = "running"
	RunWaiting  RunStatus = "waiting"   // gate or wait
	RunNeedsYou RunStatus = "needs_you" // budget cap or out of rounds
	RunDone     RunStatus = "done"
	RunFailed   RunStatus = "failed"
	RunStopped  RunStatus = "stopped"
)

// StartRunParams is runs.start.
type StartRunParams struct {
	Run    string         `json:"run"`
	Params map[string]any `json:"params,omitempty"`
}

// GetRunParams is runs.get.
type GetRunParams struct {
	RunID string `json:"run_id"`
}

// ListRunsParams is runs.list. For filters by a record id in params.
type ListRunsParams struct {
	Run string `json:"run,omitempty"`
	For string `json:"for,omitempty"`
}

// ListRunsResult answers runs.list, newest first.
type ListRunsResult struct {
	Runs []RunState `json:"runs"`
}

// RunState is a run and its steps; also the event on app/<id>/runs.
type RunState struct {
	ID        string         `json:"id"`
	App       string         `json:"app"`
	Run       string         `json:"run"`
	Trigger   string         `json:"trigger"` // "manual" or a trigger id
	Params    map[string]any `json:"params,omitempty"`
	Status    RunStatus      `json:"status"`
	Steps     []StepState    `json:"steps"`
	Cost      Cost           `json:"cost"`
	Fake      bool           `json:"fake"` // connectors were fakes (preview, check)
	StartedAt string         `json:"started_at"`
	EndedAt   string         `json:"ended_at,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// StepState is one step, or one loop round as a child.
type StepState struct {
	ID       string      `json:"id"`
	Title    string      `json:"title,omitempty"`
	Status   RunStatus   `json:"status"`
	AI       bool        `json:"ai,omitempty"` // output came from a model: label it
	Output   any         `json:"output,omitempty"`
	Error    string      `json:"error,omitempty"`
	Approval string      `json:"approval,omitempty"` // approval id while waiting on a gate
	Rounds   []StepState `json:"rounds,omitempty"`
}

// ---- approvals: topic app/<id>/approvals ----

// ApprovalState is open until a person answers. A standalone approval (no
// run) carrying a Tool moves on from approved once its action has run: done
// with Result, or failed with Error.
type ApprovalState string

const (
	ApprovalOpen     ApprovalState = "open"
	ApprovalApproved ApprovalState = "approved"
	ApprovalRejected ApprovalState = "rejected"
	ApprovalDone     ApprovalState = "done"
	ApprovalFailed   ApprovalState = "failed"
)

// Approval is "Approval needed: …" (rule 03).
type Approval struct {
	ID        string         `json:"id"`
	App       string         `json:"app"`
	RunID     string         `json:"run_id,omitempty"`
	Step      string         `json:"step,omitempty"`
	Kind      string         `json:"kind"`  // send | plan | proof | start
	Title     string         `json:"title"` // "Approval needed: send email"
	Summary   string         `json:"summary,omitempty"`
	Tool      string         `json:"tool,omitempty"` // gmail.send
	Payload   map[string]any `json:"payload,omitempty"`
	Who       string         `json:"who"`
	State     ApprovalState  `json:"state"`
	CreatedAt string         `json:"created_at"`
	DecidedAt string         `json:"decided_at,omitempty"`
	Thread    string         `json:"thread,omitempty"` // the agent thread that asked; it hears the outcome
	Result    string         `json:"result,omitempty"` // done: a short summary of what ran
	Error     string         `json:"error,omitempty"`  // failed: why the action did not complete
}

// ListApprovalsParams is approvals.list.
type ListApprovalsParams struct {
	State ApprovalState `json:"state,omitempty"` // default open
}

// ListApprovalsResult answers approvals.list.
type ListApprovalsResult struct {
	Approvals []Approval `json:"approvals"`
}

// DecideApprovalParams is approvals.decide. Edit replaces payload fields
// before approving ("Edit" on the ApprovalCard).
type DecideApprovalParams struct {
	ApprovalID string         `json:"approval_id"`
	Answer     string         `json:"answer"` // yes | no
	Edit       map[string]any `json:"edit,omitempty"`
}

// ---- app agent: topic app/<id>/agent/<thread> ----

// AgentSendParams is agent.send. An empty Thread starts a new thread.
type AgentSendParams struct {
	Thread string `json:"thread,omitempty"`
	Text   string `json:"text"`
	Record string `json:"record,omitempty"` // "type/id" in view
	Screen string `json:"screen,omitempty"`
}

// AgentStopParams is agent.stop: cancel the thread's turn in flight.
type AgentStopParams struct {
	Thread string `json:"thread"`
}

// AgentSendResult names the thread and turn the events stream on.
type AgentSendResult struct {
	Thread string `json:"thread"`
	Turn   string `json:"turn"`
}

// AgentEventKind is one streamed agent event.
type AgentEventKind string

const (
	AgentDelta     AgentEventKind = "delta"      // Text appended to the reply
	AgentTool      AgentEventKind = "tool"       // Tool + Summary: a bridge call it made
	AgentApproval  AgentEventKind = "approval"   // Approval created (outbound draft)
	AgentShapeThis AgentEventKind = "shape_this" // asked to change the app: "That changes the app"
	AgentDone      AgentEventKind = "done"
	AgentError     AgentEventKind = "error"
)

// AgentEvent is one event on app/<id>/agent/<thread>.
type AgentEvent struct {
	Kind     AgentEventKind `json:"kind"`
	Turn     string         `json:"turn"`
	Text     string         `json:"text,omitempty"`
	Tool     string         `json:"tool,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Approval *Approval      `json:"approval,omitempty"`
	Request  string         `json:"request,omitempty"` // shape_this: the words to pre-fill
	Cost     *Cost          `json:"cost,omitempty"`
}
