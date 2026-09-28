//go:build !windows

package steploop

import (
	"context"
	"encoding/json"
	"io"
	"math"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
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

// ToolResult is what a tool returns when it completes.
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

// Model is the resolved model in the three projections the loop needs: the
// message-conversion view, the budget view and the request parameters.
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

// TaskInput is the context handed to the compaction branch of the loop.
type TaskInput struct {
	SessionID string
	Messages  []msgmodel.WithParts
	User      msgmodel.User
	Model     Model
}

// TaskController is the narrow seam to the compaction service. The step loop
// owns when each operation runs; the controller owns the operations.
type TaskController interface {
	ProcessCompaction(ctx context.Context, input TaskInput, task msgmodel.CompactionPart) (Result, error)
	IsOverflow(ctx context.Context, assistant msgmodel.Assistant, model Model) (bool, error)
	CreateCompaction(ctx context.Context, sessionID string, user msgmodel.User, overflow bool) error
	Prune(ctx context.Context, sessionID string) error
}

// RunOptions are the session and agent values one Run needs.
type RunOptions struct {
	SessionID string
	ParentID  string
	Workspace string
	Worktree  string

	// MaxSteps is agent.steps. Nil means Infinity.
	MaxSteps *float64
	Tools    []ToolDefinition

	// InjectReminders may add in-memory-only reminder parts to the prompt
	// before each request. It must return fresh values.
	InjectReminders func(context.Context, []msgmodel.WithParts, msgmodel.User) ([]msgmodel.WithParts, error)

	// AfterAssistant runs after each processor turn, including stop and
	// error turns, with the assistant message ID; the instruction tracker
	// uses it to release the claims made for that message.
	AfterAssistant func(context.Context, string)

	// AfterTurn observes a fully persisted assistant turn and may stop the loop
	// before another provider call.
	AfterTurn func(context.Context, msgmodel.Assistant, msgmodel.Parts) error
}

func (o RunOptions) maxSteps() float64 {
	if o.MaxSteps == nil {
		return math.Inf(1)
	}
	return *o.MaxSteps
}

// Result is what a processed turn asks the loop to do next.
type Result string

const (
	ResultCompact  Result = "compact"
	ResultStop     Result = "stop"
	ResultContinue Result = "continue"
)

// MaxStepsPrompt is appended to the prompt once the step cap is reached.
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
