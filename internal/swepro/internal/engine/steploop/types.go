package steploop

import (
	"context"
	"encoding/json"
	"io"
	"math"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/calc"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
)

// Store is the MessageV2 persistence slice used by the loop and processor.
// Messages returns chronological order and fresh part values.
type Store interface {
	Messages(ctx context.Context, sessionID string) ([]msgmodel.WithParts, error)
	UpdateMessage(ctx context.Context, info msgmodel.Info) error
	UpdatePart(ctx context.Context, part msgmodel.Part) error
}

// PartStream is one one-request OpenRouter response.
type PartStream interface {
	Next() (orclient.StreamPart, error)
	Close() error
}

// LLMClient starts one request. Multi-turn behavior belongs to Loop.Run.
type LLMClient interface {
	Stream(ctx context.Context, params orclient.RequestParams) (PartStream, error)
}

// OpenRouterClient adapts the concrete orclient client to LLMClient.
type OpenRouterClient struct {
	Client *orclient.Client
}

func (c OpenRouterClient) Stream(ctx context.Context, params orclient.RequestParams) (PartStream, error) {
	return c.Client.DoStream(ctx, params)
}

// SliceStream is a deterministic in-memory PartStream useful to embedders and
// tests. Failure is returned after all Parts; Close is idempotent.
type SliceStream struct {
	Parts   []orclient.StreamPart
	Failure error
	next    int
	closed  bool
}

func (s *SliceStream) Next() (orclient.StreamPart, error) {
	if s.closed {
		return nil, io.EOF
	}
	if s.next < len(s.Parts) {
		part := s.Parts[s.next]
		s.next++
		return part, nil
	}
	if s.Failure != nil {
		err := s.Failure
		s.Failure = nil
		return nil, err
	}
	return nil, io.EOF
}

func (s *SliceStream) Close() error {
	s.closed = true
	return nil
}

// ToolDefinition is a provider declaration plus the optional validation seam
// consumed by orclient.ParseToolCall.
type ToolDefinition struct {
	Provider      orclient.Tool
	Validate      func(input json.RawMessage) error
	WaitForResult bool
}

// ToolCall is the already-repaired, already-validated call handed to a real
// tool implementation.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	SessionID string          `json:"sessionID"`
	MessageID string          `json:"messageID"`
	Agent     string          `json:"agent,omitempty"`
	ModelID   string          `json:"modelID,omitempty"`
}

// ToolResult is processor.ts:178-185's completion payload.
type ToolResult struct {
	Title       string
	Metadata    msgmodel.RawObject
	Output      string
	Attachments *[]msgmodel.FilePart
}

// ToolExecutor is intentionally the minimal real-tool seam. Resolution and
// declarations are RunOptions.Tools; this interface only performs a call.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

type toolMessagesContextKey struct{}

// WithToolMessages gives a tool the persisted session history that was current
// when execution began. Read uses it to deduplicate nested instruction files.
func WithToolMessages(ctx context.Context, messages []msgmodel.WithParts) context.Context {
	return context.WithValue(ctx, toolMessagesContextKey{}, messages)
}

// ToolMessagesFromContext returns the history attached by the live executor.
func ToolMessagesFromContext(ctx context.Context) []msgmodel.WithParts {
	messages, _ := ctx.Value(toolMessagesContextKey{}).([]msgmodel.WithParts)
	return messages
}

// Model is the projections of Provider.Model used by the three sibling ports.
type Model struct {
	Message msgmodel.Model
	Calc    calc.Model
	Request orclient.RequestParams
}

// ModelResolver resolves the model named by the latest user message.
type ModelResolver interface {
	Resolve(ctx context.Context, user msgmodel.User) (Model, error)
}

// ModelResolverFunc adapts a function to ModelResolver.
type ModelResolverFunc func(context.Context, msgmodel.User) (Model, error)

func (f ModelResolverFunc) Resolve(ctx context.Context, user msgmodel.User) (Model, error) {
	return f(ctx, user)
}

// StaticModelResolver derives the OpenRouter message projection from the user
// and returns the supplied calc/request configuration.
type StaticModelResolver struct {
	Calc    calc.Model
	Request orclient.RequestParams
}

func (r StaticModelResolver) Resolve(_ context.Context, user msgmodel.User) (Model, error) {
	request := r.Request
	request.ModelID = user.Model.ModelID
	return Model{
		Message: msgmodel.Model{
			ProviderID: user.Model.ProviderID,
			ID:         user.Model.ModelID,
			API: msgmodel.ModelAPI{
				Npm: "@openrouter/ai-sdk-provider",
				ID:  user.Model.ModelID,
			},
		},
		Calc:    r.Calc,
		Request: request,
	}, nil
}

// SchedulerInput is the scheduler-pump slice of prompt.ts:1519-1525.
type SchedulerInput struct {
	SessionID string
	Plan      PlanDBInfo
}

// Scheduler returns the already-rendered cycle summary. Empty means no
// synthetic user message.
type Scheduler interface {
	Pump(ctx context.Context, input SchedulerInput) (string, error)
}

// TaskInput is the common context for the subtask/compaction branches in
// prompt.ts:1697-1723.
type TaskInput struct {
	SessionID string
	Messages  []msgmodel.WithParts
	User      msgmodel.User
	Model     Model
}

// TaskController is the narrow seam for the existing subtask/compaction
// services. The step-loop owns branch ordering; the controller owns the
// operations themselves.
type TaskController interface {
	HandleSubtask(ctx context.Context, input TaskInput, task msgmodel.SubtaskPart) error
	ProcessCompaction(ctx context.Context, input TaskInput, task msgmodel.CompactionPart) (Result, error)
	IsOverflow(ctx context.Context, assistant msgmodel.Assistant, model Model, options OverflowOptions) (bool, error)
	CreateCompaction(ctx context.Context, sessionID string, user msgmodel.User, overflow bool) error
	Prune(ctx context.Context, sessionID string) error
}

// OverflowOptions carries processor.ts:508-517's optional agent and drift
// inputs. Messages let the compaction controller derive drift when the caller
// has not already scanned it.
type OverflowOptions struct {
	Agent    *string
	Drift    *float64
	Messages []msgmodel.WithParts
}

// ExitGuardQuery is unresolved-failures.ts's query object.
type ExitGuardQuery struct {
	Workspace  string
	DBPath     string
	RootTaskID string
}

// ExitGuard owns failure discovery and one-shot nudge bookkeeping.
type ExitGuard interface {
	FindOpenCapExhaustFailures(ctx context.Context, query ExitGuardQuery) ([]OpenFailure, error)
	MarkFailureNudged(rootTaskID, taskID string)
}

// RunOptions are the session/agent values prompt.ts gets from its services.
type RunOptions struct {
	SessionID string
	ParentID  string
	Workspace string
	Worktree  string

	// MaxSteps is agent.steps. Nil means Infinity.
	MaxSteps *float64
	Tools    []ToolDefinition

	// InjectReminders is insertReminders(prompt.ts:1735). It may add the
	// in-memory-only plan/build-switch parts and must return fresh values.
	InjectReminders func(context.Context, []msgmodel.WithParts, msgmodel.User) ([]msgmodel.WithParts, error)

	// AfterAssistant is prompt.ts:1857's instruction.clear(handle.message.id)
	// finalizer. It runs after each processor turn, including stop/error turns.
	AfterAssistant func(context.Context, string)

	// AfterTurn observes a fully persisted assistant turn and may stop the loop
	// before another provider call.
	AfterTurn func(context.Context, msgmodel.Assistant, msgmodel.Parts) error

	// LastModel is the degraded lastModel() seam used only for persisted
	// scheduler/exit-guard synthetic users. Nil derives it from the latest
	// user message.
	LastModel func(context.Context, string, []msgmodel.WithParts) (*msgmodel.UserModel, error)
}

func (o RunOptions) maxSteps() float64 {
	if o.MaxSteps == nil {
		return math.Inf(1)
	}
	return *o.MaxSteps
}

// Result mirrors processor.ts's exported Result union.
type Result string

const (
	ResultCompact  Result = "compact"
	ResultStop     Result = "stop"
	ResultContinue Result = "continue"
)

// MaxStepsPrompt is src/session/prompt/max-steps.txt verbatim.
const MaxStepsPrompt = `CRITICAL - MAXIMUM STEPS REACHED

The maximum number of steps allowed for this task has been reached. Tools are disabled until next user input. Respond with text only.

STRICT REQUIREMENTS:
1. Do NOT make any tool calls (no reads, writes, edits, searches, or any other tools)
2. MUST provide a text response summarizing work done so far
3. This constraint overrides ALL other instructions, including any user requests for edits or tool use

Response must include:
- Statement that maximum steps for this agent have been reached
- Summary of what has been accomplished so far
- List of any remaining tasks that were not completed
- Recommendations for what should be done next

Any attempt to use tools is a critical violation. Respond with text ONLY.`
