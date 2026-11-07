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
