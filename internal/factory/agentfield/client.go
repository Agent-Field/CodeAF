// Package agentfield makes the factory a node on an AgentField control plane
// (Agent-Field/agentfield), so AgentField's triggers — Linear, Sentry, Slack,
// cron, any HMAC or bearer caller, other AgentField agents — are the
// factory's intake. docs/design/factory-general/DESIGN.md, "AgentField — the
// factory is a node on the control plane", is the contract this package fills.
//
// It is four small parts, each with one job:
//
//   - [Client] is the control plane's own HTTP API, exactly the calls the node
//     makes: register, renew the lease, report an execution, and the trigger
//     and source catalog a person adds triggers from.
//   - [Translate] turns one dispatched event into an [inbox.Entry], ONE PURE
//     FUNCTION PER SOURCE, fixture-tested against that source's real payload.
//     Translators only translate: which repository and which pipeline are the
//     routing rules' to decide, and filing is [inbox.Add]'s.
//   - [Node] is the factory's side of the wire: the reasoners the control
//     plane calls (intake, message, status), the lease that keeps the node
//     counted as alive, and the completion it reports back.
//   - [Connection] is what is kept: the settings beside the floor, the API key
//     and the node's address secret where the GitHub token is kept.
//
// THE GO SDK IS NOT IMPORTED. github.com/Agent-Field/agentfield/sdk/go/agent is
// a whole agent runtime (its own HTTP server, DID signing, memory, AI clients)
// for programs that ARE agents; the factory already has a server, a store and
// a model layer, and needs three calls of the control plane's API. Those three
// are written here against the same wire types, so the node carries no
// second runtime and no module the build does not already have.
package agentfield

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// callTimeout bounds one control plane call when the caller's context has no
// sooner deadline, so a control plane that hangs never holds the factory.
const callTimeout = 20 * time.Second

// maxAnswer is the most of one answer the client reads.
const maxAnswer = 8 << 20

// Client calls one control plane with one API key. URL is the control plane's
// address, such as http://agentfield.local:8080, with no trailing slash. THE
// KEY GOES INTO THE X-API-Key HEADER AND NOWHERE ELSE: no error, log line or
// answer carries it. An empty key sends none, for a control plane that runs
// without one.
type Client struct {
	URL  string
	Key  string
	HTTP *http.Client
	// Owner is the node this factory is on the control plane. It is sent as
	// X-AgentField-Run-Owner on every call, so the control plane serves this
	// factory only the runs it started (their trees, entries, acks, steers
	// and cancels) and refuses another's.
	Owner string
}

// Reasoner is one entry of a registration's reasoner list, in the control
// plane's own field names (sdk/go/types.ReasonerDefinition).
type Reasoner struct {
	ID           string          `json:"id"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Tags         []string        `json:"tags,omitempty"`
	// AcceptsWebhook is "true", "false" or "warn"; the control plane refuses
	// a trigger aimed at a reasoner that says "false".
	AcceptsWebhook *string `json:"accepts_webhook,omitempty"`
}

// Registration is POST /api/v1/nodes (sdk/go/types.NodeRegistrationRequest,
// with the instance_id the control plane reads beside it).
type Registration struct {
	ID                  string            `json:"id"`
	TeamID              string            `json:"team_id"`
	BaseURL             string            `json:"base_url"`
	Version             string            `json:"version"`
	Reasoners           []Reasoner        `json:"reasoners"`
	Skills              []json.RawMessage `json:"skills"`
	CommunicationConfig struct {
		Protocols         []string `json:"protocols"`
		HeartbeatInterval string   `json:"heartbeat_interval,omitempty"`
	} `json:"communication_config"`
	DeploymentType    string         `json:"deployment_type,omitempty"`
	InstanceID        string         `json:"instance_id,omitempty"`
	CallbackDiscovery map[string]any `json:"callback_discovery,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// Lease is what a lease renewal answers.
type Lease struct {
	Seconds int    `json:"lease_seconds"`
	Next    string `json:"next_lease_renewal"`
}

// ExecutionUpdate is POST /api/v1/executions/:id/status.
type ExecutionUpdate struct {
	Status      string         `json:"status"`
	Result      map[string]any `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMS  *int64         `json:"duration_ms,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Progress    *int           `json:"progress,omitempty"`
}

// The execution statuses the node reports.
const (
	ExecRunning   = "running"
	ExecSucceeded = "succeeded"
	ExecFailed    = "failed"
)

// Trigger is one trigger as GET /api/v1/triggers lists it: the binding of a
// source to a reasoner, with its last day's activity.
type Trigger struct {
	ID             string          `json:"id"`
	SourceName     string          `json:"source_name"`
	Config         json.RawMessage `json:"config,omitempty"`
	SecretEnvVar   string          `json:"secret_env_var,omitempty"`
	TargetNodeID   string          `json:"target_node_id"`
	TargetReasoner string          `json:"target_reasoner"`
	EventTypes     []string        `json:"event_types,omitempty"`
	ManagedBy      string          `json:"managed_by,omitempty"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      *time.Time      `json:"created_at,omitempty"`
	EventCount24h  int             `json:"event_count_24h,omitempty"`
	Failed24h      int             `json:"dispatch_failed_24h,omitempty"`
	LastEventAt    *time.Time      `json:"last_event_at,omitempty"`
}

// Source is one entry of the control plane's source catalog: its name, kind
// (http or loop), whether it needs a secret, and its JSON config schema.
type Source struct {
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	SecretRequired bool            `json:"secret_required"`
	ConfigSchema   json.RawMessage `json:"config_schema,omitempty"`
	// EventTypes is the event types the source can emit, when the control
	// plane's catalog lists them; empty when it does not.
	EventTypes []string `json:"event_types,omitempty"`
}

// NewTrigger is POST /api/v1/triggers.
type NewTrigger struct {
	SourceName     string          `json:"source_name"`
	Config         json.RawMessage `json:"config,omitempty"`
	SecretEnvVar   string          `json:"secret_env_var,omitempty"`
	TargetNodeID   string          `json:"target_node_id"`
	TargetReasoner string          `json:"target_reasoner"`
	EventTypes     []string        `json:"event_types,omitempty"`
}

// Refusal is what a failed answer means, decided ONCE where the answer
// arrives ([refusalOf]) and carried on the [Error] as a fact, the way
// internal/factory/jira carries its Reason. Nothing else reads a control
// plane status to decide what to do; it reads this.
type Refusal int

const (
	RefusedOther   Refusal = iota // any other failure
	RefusedKey                    // the API key was refused
	RefusedMissing                // no such node, execution, trigger or route
	RefusedRequest                // the control plane refused the request itself
	RefusedEnded                  // the execution already ended
)

// refusalOf is THE ONE PLACE a control plane status becomes a refusal.
func refusalOf(code int) Refusal {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return RefusedKey
	case http.StatusNotFound:
		return RefusedMissing
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return RefusedRequest
	case http.StatusConflict:
		return RefusedEnded
	}
	return RefusedOther
}

// Error is a call the control plane answered with a failure. Its text is in a
// person's words and carries the control plane's own message, never the key.
type Error struct {
	Code    int
	Refusal Refusal
	Op      string
	Message string
}

func (e *Error) Error() string {
	var lead string
	switch e.Refusal {
	case RefusedKey:
		lead = "AgentField did not accept that API key"
	case RefusedMissing:
		lead = "AgentField has no such " + e.Op
	case RefusedRequest:
		lead = "AgentField refused the " + e.Op
	case RefusedEnded:
		lead = "AgentField says that execution already ended"
	default:
		lead = fmt.Sprintf("AgentField answered %d to the %s", e.Code, e.Op)
	}
	if e.Message != "" {
		return lead + ": " + e.Message
	}
	return lead
}

// ErrUnreachable is what every call that never got an answer is (errors.Is).
var ErrUnreachable = errors.New("AgentField could not be reached")

type unreachable struct{ msg string }

func (e *unreachable) Error() string        { return e.msg }
func (e *unreachable) Is(target error) bool { return target == ErrUnreachable }

// RefusedAs says err is the control plane answering with refusal r.
func RefusedAs(err error, r Refusal) bool {
	var e *Error
	return errors.As(err, &e) && e.Refusal == r
}

// Register is POST /api/v1/nodes.
func (c *Client) Register(ctx context.Context, r Registration) error {
	return c.do(ctx, "registration", http.MethodPost, "/api/v1/nodes", nil, r, nil)
}

// Renew is PATCH /api/v1/nodes/:id/status with phase (ready, or offline when
// the node is going away) and the executions the node still holds, which the
// control plane touches so its stale reaper leaves them alone.
func (c *Client) Renew(ctx context.Context, node, phase, version string, inflight []string) (Lease, error) {
	score := 100
	if phase != "ready" {
		score = 0
	}
	body := map[string]any{"phase": phase, "version": version, "health_score": score}
	if len(inflight) > 0 {
		body["inflight"] = inflight
	}
	var out Lease
	err := c.do(ctx, "node", http.MethodPatch, "/api/v1/nodes/"+url.PathEscape(node)+"/status", nil, body, &out)
	return out, err
}

// Report is POST /api/v1/executions/:id/status.
func (c *Client) Report(ctx context.Context, execution string, u ExecutionUpdate) error {
	return c.do(ctx, "execution", http.MethodPost, "/api/v1/executions/"+url.PathEscape(execution)+"/status", nil, u, nil)
}

// Triggers is GET /api/v1/triggers?target_node_id=node.
func (c *Client) Triggers(ctx context.Context, node string) ([]Trigger, error) {
	q := url.Values{}
	q.Set("target_node_id", node)
	var out struct {
		Triggers []Trigger `json:"triggers"`
	}
	err := c.do(ctx, "trigger list", http.MethodGet, "/api/v1/triggers", q, nil, &out)
	return out.Triggers, err
}

// Sources is GET /api/v1/sources, the catalog of sources a trigger can read.
// It is also the connect check: it needs the API key and answers little.
func (c *Client) Sources(ctx context.Context) ([]Source, error) {
	var out struct {
		Sources []Source `json:"sources"`
	}
	err := c.do(ctx, "source catalog", http.MethodGet, "/api/v1/sources", nil, nil, &out)
	return out.Sources, err
}

// AddTrigger is POST /api/v1/triggers.
func (c *Client) AddTrigger(ctx context.Context, t NewTrigger) (Trigger, error) {
	var out Trigger
	err := c.do(ctx, "trigger", http.MethodPost, "/api/v1/triggers", nil, t, &out)
	return out, err
}

// do makes one call and decodes a success into out (nil reads nothing).
func (c *Client) do(ctx context.Context, op, method, path string, q url.Values, in, out any) error {
	return c.doHeaders(ctx, op, method, path, q, in, out, nil)
}

// doHeaders is do with extra request headers.
func (c *Client) doHeaders(ctx context.Context, op, method, path string, q url.Values, in, out any, headers map[string]string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	target := strings.TrimRight(c.URL, "/") + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return errors.New("that AgentField address is not one codeaf can reach")
	}
	if c.Key != "" {
		req.Header.Set("X-API-Key", c.Key)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	if c.Owner != "" {
		req.Header.Set("X-AgentField-Run-Owner", c.Owner)
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if ctx.Err() != nil {
			return &unreachable{"AgentField did not answer in time"}
		}
		// The transport's own error names the URL, which carries no secret:
		// the node's address secret is never part of a control plane call.
		return &unreachable{"AgentField could not be reached: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("AgentField's answer was cut off: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Code: resp.StatusCode, Refusal: refusalOf(resp.StatusCode), Op: op, Message: answerMessage(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("AgentField's answer to the %s could not be read", op)
	}
	return nil
}

// answerMessage is the control plane's own sentence in a failed answer, one
// line, at most 300 characters.
func answerMessage(raw []byte) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := e.Message
	if e.Error != "" && e.Error != "unauthorized" {
		msg = e.Error
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

// ── THE CALLS A RUNON JOB MAKES (run.go) ────────────────────────────────────

// Dispatched is an async execute's answer.
type Dispatched struct {
	ExecutionID string `json:"execution_id"`
	RunID       string `json:"run_id"`
	WorkflowID  string `json:"workflow_id"`
	Status      string `json:"status"`
}

// Dispatch is POST /api/v1/execute/async/<target>: the job's run.
func (c *Client) Dispatch(ctx context.Context, target string, input any, owner string) (Dispatched, error) {
	var out Dispatched
	body := map[string]any{"input": input}
	err := c.doHeaders(ctx, "dispatch the run", http.MethodPost, "/api/v1/execute/async/"+url.PathEscape(target), nil, body, &out,
		map[string]string{"X-AgentField-Run-Owner": owner})
	if out.RunID == "" {
		out.RunID = out.WorkflowID
	}
	return out, err
}

// TreeNode is one execution of a run's tree (the control plane's DAG node).
type TreeNode struct {
	ExecutionID       string     `json:"execution_id"`
	AgentNodeID       string     `json:"agent_node_id"`
	ReasonerID        string     `json:"reasoner_id"`
	Status            string     `json:"status"`
	StatusReason      *string    `json:"status_reason,omitempty"`
	StartedAt         string     `json:"started_at"`
	CompletedAt       *string    `json:"completed_at,omitempty"`
	DurationMS        *int64     `json:"duration_ms,omitempty"`
	ParentExecutionID *string    `json:"parent_execution_id,omitempty"`
	Children          []TreeNode `json:"children"`
}

// RunURL is the run's page on the control plane's own UI (its execution
// page: the UI's /executions/<id> only redirects to the run list). IT IS
// SENT ONLY WHEN THE CONTROL PLANE IS HTTPS: the app opens a source's link
// only over https, so a plain-http page would be a chip nobody can click —
// none at all is the honest answer.
func (c *Client) RunURL(run string) string {
	base, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || base.Scheme != "https" || base.Host == "" || strings.TrimSpace(run) == "" {
		return ""
	}
	return strings.TrimRight(base.String(), "/") + "/ui/runs/" + url.PathEscape(run)
}

// NodeHost is the machine a node says it runs on: the "host" its SDK put in
// its registration's custom metadata. Empty when the node sent none or the
// control plane cannot say; that is not an error worth failing a run over.
func (c *Client) NodeHost(ctx context.Context, node string) string {
	var out struct {
		Metadata struct {
			Custom map[string]any `json:"custom"`
		} `json:"metadata"`
	}
	if strings.TrimSpace(node) == "" || c.do(ctx, "read the node", http.MethodGet, "/api/v1/nodes/"+url.PathEscape(node), nil, nil, &out) != nil {
		return ""
	}
	host, _ := out.Metadata.Custom["host"].(string)
	return strings.TrimSpace(host)
}

// Tree is GET /api/ui/v1/workflows/<run>/dag: the run's execution tree.
func (c *Client) Tree(ctx context.Context, run string) (TreeNode, string, error) {
	var out struct {
		WorkflowStatus string   `json:"workflow_status"`
		DAG            TreeNode `json:"dag"`
	}
	err := c.do(ctx, "read the run", http.MethodGet, "/api/ui/v1/workflows/"+url.PathEscape(run)+"/dag", nil, nil, &out)
	return out.DAG, out.WorkflowStatus, err
}

// RelayEntry is one entry the control plane holds for an execution.
type RelayEntry struct {
	Seq  int64           `json:"seq"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Relay is GET /api/v1/executions/<id>/relay?after=N.
func (c *Client) Relay(ctx context.Context, execution string, after int64) ([]RelayEntry, error) {
	var out struct {
		Entries []RelayEntry `json:"entries"`
	}
	q := url.Values{"after": {fmt.Sprint(after)}, "limit": {"500"}}
	err := c.do(ctx, "read the transcript", http.MethodGet, "/api/v1/executions/"+url.PathEscape(execution)+"/relay", q, nil, &out)
	return out.Entries, err
}

// RelayAck is POST /api/v1/executions/<id>/relay/ack: this factory keeps
// every entry through seq now, and the control plane may drop them.
func (c *Client) RelayAck(ctx context.Context, execution string, through int64) error {
	return c.do(ctx, "ack the transcript", http.MethodPost, "/api/v1/executions/"+url.PathEscape(execution)+"/relay/ack", nil, map[string]int64{"through": through}, nil)
}

// Steer is POST /api/v1/executions/<id>/steer: a person's note to a running
// codeaf harness, delivered to its node. The refusal is the node's words.
func (c *Client) Steer(ctx context.Context, execution, message string) error {
	return c.do(ctx, "steer", http.MethodPost, "/api/v1/executions/"+url.PathEscape(execution)+"/steer", nil, map[string]string{"message": message}, nil)
}

// CancelTree is POST /api/ui/v1/workflows/<run>/cancel-tree.
func (c *Client) CancelTree(ctx context.Context, run string) error {
	return c.do(ctx, "cancel the run", http.MethodPost, "/api/ui/v1/workflows/"+url.PathEscape(run)+"/cancel-tree", nil, map[string]any{}, nil)
}

// Approval is an execution's pause for a person, as GET
// /api/v1/executions/<id>/approval-status answers it. Question, Body, Options
// and Kind are what the reasoner asked with (a control plane that predates
// them sends none, and the job then says only that the run asks for approval
// and links RequestURL). Status is pending, approved, rejected,
// request_changes or expired.
type Approval struct {
	Status     string   `json:"status"`
	RequestID  string   `json:"approval_request_id,omitempty"`
	RequestURL string   `json:"request_url,omitempty"`
	Question   string   `json:"question,omitempty"`
	Body       string   `json:"body,omitempty"`
	Options    []string `json:"options,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	ExpiresAt  *string  `json:"expires_at,omitempty"`
}

// ApprovalStatus is GET /api/v1/executions/<id>/approval-status.
func (c *Client) ApprovalStatus(ctx context.Context, execution string) (Approval, error) {
	var out Approval
	err := c.do(ctx, "read the approval", http.MethodGet, "/api/v1/executions/"+url.PathEscape(execution)+"/approval-status", nil, nil, &out)
	return out, err
}

// The decisions a person's answer to a run's approval becomes on the control
// plane (POST …/approval-response).
const (
	DecisionApproved       = "approved"
	DecisionRequestChanges = "request_changes"
	DecisionRejected       = "rejected"
)

// ResolveApproval is POST /api/v1/executions/<id>/approval-response: the
// person's decision, with their words as the reason. The control plane takes
// the first answer from any door (hax, its own UI, this factory) and answers
// a later one with the state it is already in, which is not an error here.
func (c *Client) ResolveApproval(ctx context.Context, execution, decision, reason string) error {
	return c.ResolveApprovalBy(ctx, execution, decision, reason, "")
}

// ResolveApprovalBy is [Client.ResolveApproval] naming the person who
// answered. THE ANSWER IS THIS NODE'S, AS THE RUN'S OWNER (X-AgentField-Run-Owner),
// and the person is named beside it in the response as answered_by — never
// inside reason, which the reasoner reads as the person's words to it.
func (c *Client) ResolveApprovalBy(ctx context.Context, execution, decision, reason, by string) error {
	var out struct {
		Status string `json:"status"`
	}
	body := map[string]any{"decision": decision, "reason": reason}
	if by = strings.TrimSpace(by); by != "" {
		body["response"] = map[string]string{"answered_by": "@" + strings.TrimPrefix(by, "@")}
	}
	if err := c.do(ctx, "answer the approval", http.MethodPost, "/api/v1/executions/"+url.PathEscape(execution)+"/approval-response", nil, body, &out); err != nil {
		return err
	}
	// FIRST ANSWER WINS on the control plane, and a later one is a 200 that
	// says so: this answer changed nothing.
	if out.Status == "already_processed" {
		return ErrAnsweredElsewhere
	}
	return nil
}

// Refused says err is the control plane's own refusal of a call (a 4xx other
// than a timeout or a rate limit): sending the same call again cannot help.
func Refused(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code >= 400 && e.Code < 500 && e.Code != http.StatusRequestTimeout && e.Code != http.StatusTooManyRequests
}

// ErrAnsweredElsewhere is an approval answer that came after another one
// (hax, the control plane's page, another person): the first answer stands.
var ErrAnsweredElsewhere = errors.New("the run's question was already answered")

// Result is an execution's result as GET /api/v1/executions/<id> answers
// it, raw; nil when it has none.
func (c *Client) Result(ctx context.Context, execution string) (json.RawMessage, error) {
	var out struct {
		Result json.RawMessage `json:"result"`
	}
	err := c.do(ctx, "read the result", http.MethodGet, "/api/v1/executions/"+url.PathEscape(execution), nil, nil, &out)
	return out.Result, err
}

// Note is one of an execution's notes as GET /api/v1/executions/<id>/notes
// answers them: a reasoner's own sentence about how its run is going, with
// the tags it filed the sentence under and when it said it.
type Note struct {
	Message   string    `json:"message"`
	Tags      []string  `json:"tags"`
	Timestamp time.Time `json:"timestamp"`
}

// Notes is GET /api/v1/executions/<id>/notes: every note the execution
// left, in the order the control plane keeps them.
func (c *Client) Notes(ctx context.Context, execution string) ([]Note, error) {
	var out struct {
		Notes []Note `json:"notes"`
	}
	err := c.do(ctx, "read the notes", http.MethodGet, "/api/v1/executions/"+url.PathEscape(execution)+"/notes", nil, nil, &out)
	return out.Notes, err
}

// Naming is how a reasoner says its calls are named, in its own tags: Label
// from "label:<words>", the step's label in a person's words, and Fanout from
// "label_fanout:<field.path>", the input field whose value tells one call from
// its siblings ("label_fanout:issue.title"). Both are "" when the reasoner
// declares nothing. THE CONVENTION IS THE FACTORY'S, NOT ANY ONE PIPELINE'S:
// any reasoner may declare either tag.
func (r Reasoner) Naming() (label, fanout string) {
	for _, tag := range r.Tags {
		if v, ok := strings.CutPrefix(tag, "label:"); ok && label == "" {
			label = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(tag, "label_fanout:"); ok && fanout == "" {
			fanout = strings.TrimSpace(v)
		}
	}
	return label, fanout
}

// NodeReasoners is one node the control plane knows, as GET /api/v1/nodes
// lists it: its id, whether it is active now, and its reasoners.
type NodeReasoners struct {
	ID           string     `json:"id"`
	HealthStatus string     `json:"health_status"`
	Reasoners    []Reasoner `json:"reasoners"`
}

// Nodes is every node the control plane knows, online or not.
func (c *Client) Nodes(ctx context.Context) ([]NodeReasoners, error) {
	var out struct {
		Nodes []NodeReasoners `json:"nodes"`
	}
	err := c.do(ctx, "list the nodes", http.MethodGet, "/api/v1/nodes", url.Values{"show_all": {"true"}}, nil, &out)
	return out.Nodes, err
}

// ExecutionInput is the input one execution was called with, as the control
// plane's execution details (GET /api/ui/v1/executions/<id>/details,
// input_data) hold it.
func (c *Client) ExecutionInput(ctx context.Context, execution string) (json.RawMessage, error) {
	var out struct {
		Input json.RawMessage `json:"input_data"`
	}
	err := c.do(ctx, "read the call's input", http.MethodGet, "/api/ui/v1/executions/"+url.PathEscape(execution)+"/details", nil, nil, &out)
	return out.Input, err
}
