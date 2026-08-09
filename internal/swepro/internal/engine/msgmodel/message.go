package msgmodel

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ── AssistantError (message-v2.ts:40-62, 462-472) ────────────────────────
//
// `namedSchemaError` emits the wire shape `{name, data}`
// (src/util/named-schema-error.ts:9-11), which is what `.toObject()` returns
// and therefore what lands in Assistant.error.

// APIError is the `data` payload of the APIError variant (message-v2.ts:50-57).
// `responseBody` is matched by SUBSTRING in retry.ts:130/:143, so it is a
// string kept byte-for-byte, never re-encoded JSON.
type APIError struct {
	Message         string    `json:"message"`
	StatusCode      *uint64   `json:"statusCode,omitempty"`
	IsRetryable     bool      `json:"isRetryable"`
	ResponseHeaders RawObject `json:"responseHeaders,omitempty"`
	ResponseBody    *string   `json:"responseBody,omitempty"`
	Metadata        RawObject `json:"metadata,omitempty"`
}

// APIErrorEnvelope is `APIError.EffectSchema` — the `{name, data}` WIRE shape
// (named-schema-error.ts:29-32), which is what RetryPart.error carries
// (message-v2.ts:247). ENGINE-DESIGN §5.1's "Error APIError (full struct)" is
// this envelope, not the bare payload.
type APIErrorEnvelope struct {
	Name string   `json:"name"`
	Data APIError `json:"data"`
}

func (e APIErrorEnvelope) MarshalJSON() ([]byte, error) {
	type alias APIErrorEnvelope
	e.Name = ErrNameAPI
	return tagged(alias(e))
}

// ProviderAuthErrorData is message-v2.ts:46-49.
type ProviderAuthErrorData struct {
	ProviderID string `json:"providerID"`
	Message    string `json:"message"`
}

// UnknownErrorData is NamedError.Unknown's payload (core/util/error.ts:54-58).
type UnknownErrorData struct {
	Message string `json:"message"`
}

// MessageOutputLengthErrorData is message-v2.ts:40 — no fields.
type MessageOutputLengthErrorData struct{}

// MessageAbortedErrorData is message-v2.ts:41.
type MessageAbortedErrorData struct {
	Message string `json:"message"`
}

// StructuredOutputErrorData is message-v2.ts:42-45.
type StructuredOutputErrorData struct {
	Message string `json:"message"`
	Retries uint64 `json:"retries"`
}

// ContextOverflowErrorData is message-v2.ts:58-61.
type ContextOverflowErrorData struct {
	Message      string  `json:"message"`
	ResponseBody *string `json:"responseBody,omitempty"`
}

// AssistantError is `{name, data}`. `Data` stays raw so an error minted by
// llm/llmerr round-trips verbatim; typed constructors below cover the seven
// known variants.
type AssistantError struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

func newAssistantError(name string, data any) (AssistantError, error) {
	raw, err := jscompat.Stringify(data)
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

func NewProviderAuthError(data ProviderAuthErrorData) AssistantError {
	return mustAssistantError(ErrNameProviderAuth, data)
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

// IsAborted is `AbortedError.isInstance(e)` (named-schema-error.ts:41-43) —
// a bare `name` comparison, nothing more.
func (e *AssistantError) IsAborted() bool {
	return e != nil && e.Name == ErrNameMessageAborted
}

// ── User (message-v2.ts:378-403) ─────────────────────────────────────────

// UserSummary is User.summary (:385-391).
type UserSummary struct {
	Title *string    `json:"title,omitempty"`
	Body  *string    `json:"body,omitempty"`
	Diffs []FileDiff `json:"diffs"`
}

// UserModel is User.model (:393-397).
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

// ── Assistant (message-v2.ts:546-586) ────────────────────────────────────

// AssistantTime is Assistant.time (:549-552).
type AssistantTime struct {
	Created   uint64  `json:"created"`
	Completed *uint64 `json:"completed,omitempty"`
}

// AssistantPath is Assistant.path (:561-564).
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
	// Mode is `@deprecated` upstream (:557-560) but still required.
	Mode       string            `json:"mode"`
	Agent      string            `json:"agent"`
	Path       AssistantPath     `json:"path"`
	Summary    *bool             `json:"summary,omitempty"`
	Cost       jscompat.JSNumber `json:"cost"`
	Tokens     Tokens            `json:"tokens"`
	Structured RawValue          `json:"structured,omitempty"`
	Variant    *string           `json:"variant,omitempty"`
	// Finish is one of the six unified reasons; see ENGINE-DESIGN F2.
	Finish *string `json:"finish,omitempty"`
}

func (m Assistant) MessageRole() string { return "assistant" }
func (m Assistant) MessageID() string   { return m.ID }
func (m Assistant) MarshalJSON() ([]byte, error) {
	type alias Assistant
	m.Role = "assistant"
	return tagged(alias(m))
}

// ── Info union ───────────────────────────────────────────────────────────

// Info is `User | Assistant` (message-v2.ts:588-592).
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

// ── WithParts (message-v2.ts:646-653) ────────────────────────────────────

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
