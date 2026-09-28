//go:build !windows

package msgmodel

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// ── AssistantError ───────────────────────────────────────────────────────
//
// An assistant error is persisted as `{name, data}`.

// APIError is the `data` payload of the APIError variant. `responseBody` is
// searched by substring by the error classifiers, so it is a string kept
// byte-for-byte, never re-encoded JSON.
type APIError struct {
	Message         string    `json:"message"`
	StatusCode      *uint64   `json:"statusCode,omitempty"`
	IsRetryable     bool      `json:"isRetryable"`
	ResponseHeaders RawObject `json:"responseHeaders,omitempty"`
	ResponseBody    *string   `json:"responseBody,omitempty"`
	Metadata        RawObject `json:"metadata,omitempty"`
}

// UnknownErrorData is the UnknownError payload.
type UnknownErrorData struct {
	Message string `json:"message"`
}

// MessageOutputLengthErrorData is the MessageOutputLengthError payload: no
// fields.
type MessageOutputLengthErrorData struct{}

// MessageAbortedErrorData is the MessageAbortedError payload.
type MessageAbortedErrorData struct {
	Message string `json:"message"`
}

// StructuredOutputErrorData is the StructuredOutputError payload.
type StructuredOutputErrorData struct {
	Message string `json:"message"`
	Retries uint64 `json:"retries"`
}

// ContextOverflowErrorData is the ContextOverflowError payload.
type ContextOverflowErrorData struct {
	Message      string  `json:"message"`
	ResponseBody *string `json:"responseBody,omitempty"`
}

// AssistantError is `{name, data}`. `Data` stays raw so an error minted
// elsewhere round-trips verbatim; the typed constructors below cover the seven
// known variants.
type AssistantError struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

func newAssistantError(name string, data any) (AssistantError, error) {
	raw, err := jsonutil.Marshal(data)
	if err != nil {
		return AssistantError{}, err
	}
	return AssistantError{Name: name, Data: raw}, nil
}

func mustAssistantError(name string, data any) AssistantError {
	e, err := newAssistantError(name, data)
	if err != nil {
		panic(fmt.Sprintf("msgmodel: encode %s: %v", name, err))
	}
	return e
}

func NewAPIError(data APIError) AssistantError {
	return mustAssistantError(ErrNameAPI, data)
}

func NewUnknownError(message string) AssistantError {
	return mustAssistantError(ErrNameUnknown, UnknownErrorData{Message: message})
}

func NewMessageOutputLengthError() AssistantError {
	return mustAssistantError(ErrNameMessageOutputLength, MessageOutputLengthErrorData{})
}

func NewMessageAbortedError(message string) AssistantError {
	return mustAssistantError(ErrNameMessageAborted, MessageAbortedErrorData{Message: message})
}

func NewStructuredOutputError(message string, retries uint64) AssistantError {
	return mustAssistantError(ErrNameStructuredOutput, StructuredOutputErrorData{Message: message, Retries: retries})
}

func NewContextOverflowError(data ContextOverflowErrorData) AssistantError {
	return mustAssistantError(ErrNameContextOverflow, data)
}

// IsAborted is a bare `name` comparison, nothing more.
func (e *AssistantError) IsAborted() bool {
	return e != nil && e.Name == ErrNameMessageAborted
}

// ── User ─────────────────────────────────────────

// UserSummary is User.summary.
type UserSummary struct {
	Title *string    `json:"title,omitempty"`
	Body  *string    `json:"body,omitempty"`
	Diffs []FileDiff `json:"diffs"`
}

// UserModel is User.model.
type UserModel struct {
	ProviderID string  `json:"providerID"`
	ModelID    string  `json:"modelID"`
	Variant    *string `json:"variant,omitempty"`
}

type User struct {
	MessageBase
	Role    string           `json:"role"`
	Time    TimeCreated      `json:"time"`
	Format  OutputFormat     `json:"format,omitempty"`
	Summary *UserSummary     `json:"summary,omitempty"`
	Agent   string           `json:"agent"`
	Model   UserModel        `json:"model"`
	System  *string          `json:"system,omitempty"`
	Tools   *map[string]bool `json:"tools,omitempty"`
}

func (m User) MessageRole() string { return "user" }
func (m User) MessageID() string   { return m.ID }
func (m User) MarshalJSON() ([]byte, error) {
	type alias User
	m.Role = "user"
	return tagged(alias(m))
}

// ── Assistant ────────────────────────────────────

// AssistantTime is Assistant.time.
type AssistantTime struct {
	Created   uint64  `json:"created"`
	Completed *uint64 `json:"completed,omitempty"`
}

// AssistantPath is Assistant.path.
type AssistantPath struct {
	Cwd  string `json:"cwd"`
	Root string `json:"root"`
}

type Assistant struct {
	MessageBase
	Role       string          `json:"role"`
	Time       AssistantTime   `json:"time"`
	Error      *AssistantError `json:"error,omitempty"`
	ParentID   string          `json:"parentID"`
	ModelID    string          `json:"modelID"`
	ProviderID string          `json:"providerID"`
	// Mode always carries the same value as Agent; both are persisted.
	Mode       string        `json:"mode"`
	Agent      string        `json:"agent"`
	Path       AssistantPath `json:"path"`
	Summary    *bool         `json:"summary,omitempty"`
	Cost       float64       `json:"cost"`
	Tokens     Tokens        `json:"tokens"`
	Structured RawValue      `json:"structured,omitempty"`
	Variant    *string       `json:"variant,omitempty"`
	// Finish is one of the unified finish reasons (orclient.Finish*).
	Finish *string `json:"finish,omitempty"`
	// Upstream is the endpoint that served the message's last step, copied
	// from the step-finish part so a message-level consumer (the agent
	// summary) can attribute cache misses without walking parts.
	Upstream string `json:"upstream,omitempty"`
}

func (m Assistant) MessageRole() string { return "assistant" }
func (m Assistant) MessageID() string   { return m.ID }
func (m Assistant) MarshalJSON() ([]byte, error) {
	type alias Assistant
	m.Role = "assistant"
	return tagged(alias(m))
}

// ── Info union ───────────────────────────────────────────────────────────

// Info is the User | Assistant union.
type Info interface {
	MessageRole() string
	MessageID() string
	json.Marshaler
}

// UnmarshalInfo dispatches on `role`.
func UnmarshalInfo(raw []byte) (Info, error) {
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	switch probe.Role {
	case "user":
		var m User
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "assistant":
		var m Assistant
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	return nil, fmt.Errorf("msgmodel: unknown message role %q", probe.Role)
}

// ── WithParts ────────────────────────────────────

type WithParts struct {
	Info  Info  `json:"info"`
	Parts Parts `json:"parts"`
}

func (w *WithParts) UnmarshalJSON(b []byte) error {
	var a struct {
		Info  json.RawMessage `json:"info"`
		Parts Parts           `json:"parts"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	info, err := UnmarshalInfo(a.Info)
	if err != nil {
		return err
	}
	w.Info = info
	w.Parts = a.Parts
	return nil
}
