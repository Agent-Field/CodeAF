//go:build !windows

package orclient

// Tool-call validation, repair, and the `invalid` tool.
//
// A three-stage fallback:
//
//  1. VALIDATE. An unknown tool name raises NoSuchToolError; otherwise an
//     EMPTY input string validates as `{}` (it is not an error) and anything
//     else is parsed and validated. A validation failure raises
//     InvalidToolInputError.
//  2. REPAIR. The repair callback runs for THOSE TWO ERROR TYPES ONLY.
//     senior-dev's implementation lowercases a mis-cased tool name when a
//     lowercase tool exists, and otherwise rewrites the call to
//     `toolName:"invalid"` with `input: {tool, error}` as a JSON string. A
//     repaired call is RE-VALIDATED (a second failure is NOT re-repaired); a
//     nil result keeps the ORIGINAL error; a returned error is wrapped in
//     ToolCallRepairError.
//  3. SAFETY NET. If repair is absent, returns nil, or the repaired call
//     still fails, ParseToolCall DOES NOT return an error. It returns a
//     synthetic `{type:"tool-call", …, dynamic:true, invalid:true, error}`,
//     which the step loop persists as a tool part and immediately fails,
//     WITHOUT executing the tool.
//
// Stage 3 is what keeps the loop's exit condition working: the assistant
// message still ends up with a tool part, so the turn counts as a tool-call
// turn and the loop iterates, giving the model a chance to correct itself.
//
// ActiveTools excludes "invalid" so the model can never CHOOSE it, but the
// tool map passed to the parser includes it so repair can TARGET it.
//
// ── the validator seam ───────────────────────────────────────────────────
//
// ToolSpec.Validate is optional: nil means "accept anything that parses"; a
// tool with a schema installs a real validator.

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// InvalidToolName is the tool repair rewrites an unrepairable call to.
const InvalidToolName = "invalid"

// ToolSpec is one registered tool as the parser sees it.
type ToolSpec struct {
	Name string
	// Validate checks a parsed input against the tool's schema. Nil accepts
	// any value that parsed as JSON.
	Validate func(input json.RawMessage) error
}

// ToolMap is the registered tool set in a fixed order. The order reaches the
// request body and the `invalid` tool's availableTools list, so it is part
// of what keeps prompt-cache keys stable.
type ToolMap struct {
	order []string
	specs map[string]ToolSpec
}

// NewToolMap builds a map in the given order.
func NewToolMap(specs ...ToolSpec) *ToolMap {
	m := &ToolMap{specs: map[string]ToolSpec{}}
	for _, s := range specs {
		if _, ok := m.specs[s.Name]; !ok {
			m.order = append(m.order, s.Name)
		}
		m.specs[s.Name] = s
	}
	return m
}

// SortedToolMap builds a ToolMap sorted by tool name.
func SortedToolMap(specs ...ToolSpec) *ToolMap {
	m := NewToolMap(specs...)
	sort.SliceStable(m.order, func(i, j int) bool {
		return m.order[i] < m.order[j]
	})
	return m
}

// Names lists the tools in map order.
func (m *ToolMap) Names() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.order...)
}

// ActiveTools is Names without the invalid tool: the set the model is offered.
func (m *ToolMap) ActiveTools() []string {
	out := make([]string, 0, len(m.order))
	for _, name := range m.Names() {
		if name == InvalidToolName {
			continue
		}
		out = append(out, name)
	}
	return out
}

// Get looks up a tool.
func (m *ToolMap) Get(name string) (ToolSpec, bool) {
	if m == nil {
		return ToolSpec{}, false
	}
	spec, ok := m.specs[name]
	return spec, ok
}

// ── errors ────────────────────────────────────────────────────────────────

// NoSuchToolError reports a call to an unregistered tool. The message text
// matters: the repair callback puts it into the `invalid` tool's input and the
// model reads it.
type NoSuchToolError struct {
	ToolName       string
	AvailableTools []string
	Message        string
}

func (e *NoSuchToolError) Error() string { return e.Message }

func newNoSuchToolError(toolName string, available []string) *NoSuchToolError {
	msg := "Model tried to call unavailable tool '" + toolName + "'. "
	if len(available) == 0 {
		msg += "No tools are available."
	} else {
		msg += "Available tools: " + strings.Join(available, ", ") + "."
	}
	return &NoSuchToolError{ToolName: toolName, AvailableTools: available, Message: msg}
}

// InvalidToolInputError reports an input that failed to parse or validate.
type InvalidToolInputError struct {
	ToolName  string
	ToolInput string
	Cause     error
	Message   string
}

func (e *InvalidToolInputError) Error() string { return e.Message }

func newInvalidToolInputError(toolName, toolInput string, cause error) *InvalidToolInputError {
	msg := "Invalid input for tool " + toolName + ": "
	if cause != nil {
		msg += cause.Error()
	}
	return &InvalidToolInputError{ToolName: toolName, ToolInput: toolInput, Cause: cause, Message: msg}
}

// TypeValidationError is what a Validate hook's rejection is wrapped in. Its
// message reaches the model verbatim through the `invalid` tool's input, in
// the format
//
//	Type validation failed: Value: <value JSON>.\nError message: <cause>
//
// ToolSpec.Validate implementations should return one of these.
type TypeValidationError struct {
	Value   json.RawMessage
	Cause   error
	Message string
}

func (e *TypeValidationError) Error() string { return e.Message }

// NewTypeValidationError builds one in the format above.
func NewTypeValidationError(value json.RawMessage, cause error) *TypeValidationError {
	rendered := "undefined"
	if len(value) > 0 {
		rendered = string(value)
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return &TypeValidationError{
		Value:   value,
		Cause:   cause,
		Message: "Type validation failed: Value: " + rendered + ".\nError message: " + message,
	}
}

// JSONParseError reports an input that is not JSON. Its message, in the format
//
//	JSON parsing failed: Text: <text>.\nError message: <cause>
//
// reaches the model verbatim through the `invalid` tool's input. The cause
// text is whatever encoding/json reports.
type JSONParseError struct {
	Text    string
	Cause   error
	Message string
}

func (e *JSONParseError) Error() string { return e.Message }

// NewJSONParseError builds one in the format above.
func NewJSONParseError(text string, cause error) *JSONParseError {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return &JSONParseError{
		Text:    text,
		Cause:   cause,
		Message: "JSON parsing failed: Text: " + text + ".\nError message: " + message,
	}
}

// ToolCallRepairError wraps an error returned by the repair callback.
type ToolCallRepairError struct {
	Cause         error
	OriginalError error
	Message       string
}

func (e *ToolCallRepairError) Error() string { return e.Message }

// ── the call shapes ───────────────────────────────────────────────────────

// RawToolCall is a tool call as the stream delivered it: `input` is a raw
// JSON STRING, never a parsed value.
type RawToolCall struct {
	ToolCallID       string
	ToolName         string
	Input            string
	ProviderExecuted bool
	ProviderMetadata json.RawMessage
}

// ParsedToolCall is ParseToolCall's result. `Invalid` marks the
// synthetic stage-3 result, which is emitted as a `tool-call` part and
// immediately as a `tool-error` part, and is NEVER executed.
type ParsedToolCall struct {
	Type             string
	ToolCallID       string
	ToolName         string
	Input            json.RawMessage
	Dynamic          bool
	Invalid          bool
	Error            error
	ProviderExecuted bool
	ProviderMetadata json.RawMessage
}

// MarshalJSON writes the call with a fixed key order.
func (c ParsedToolCall) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.str("type", "tool-call")
	w.str("toolCallId", c.ToolCallID)
	w.str("toolName", c.ToolName)
	w.raw("input", c.Input)
	if c.Invalid {
		w.raw("dynamic", json.RawMessage("true"))
		w.raw("invalid", json.RawMessage("true"))
		if c.Error != nil {
			w.str("error", c.Error.Error())
		}
	}
	if c.ProviderExecuted {
		w.raw("providerExecuted", json.RawMessage("true"))
	}
	w.raw("providerMetadata", c.ProviderMetadata)
	return w.done()
}

// RepairFn tries to fix a call that failed validation. Returning (nil, nil)
// keeps the ORIGINAL error.
type RepairFn func(call RawToolCall, tools *ToolMap, failure error) (*RawToolCall, error)

// ── senior-dev's repair callback ─────────────────────────────────────────────

// SeniorDevRepairToolCall is senior-dev's RepairFn.
//
// Two steps, in order:
//
//  1. if the LOWERCASED name differs from the emitted one AND a tool with the
//     lowercase name exists → return the call with the name lowercased. Note it
//     keeps the ORIGINAL input, so a call that failed VALIDATION (not
//     name-lookup) and happens to be mis-cased gets re-validated against the
//     lowercase tool's schema and can fail a second time — which then lands in
//     stage 3 rather than the `invalid` tool.
//  2. otherwise → rewrite to `toolName:"invalid"` with `input: {tool, error}`
//     encoded as a raw JSON STRING, which is what RawToolCall.Input holds.
func SeniorDevRepairToolCall(call RawToolCall, tools *ToolMap, failure error) (*RawToolCall, error) {
	lower := unicodeLower(call.ToolName)
	if lower != call.ToolName {
		if _, ok := tools.Get(lower); ok {
			repaired := call
			repaired.ToolName = lower
			return &repaired, nil
		}
	}
	payload := NewObject()
	payload.SetString("tool", call.ToolName)
	message := ""
	if failure != nil {
		message = failure.Error()
	}
	payload.SetString("error", message)
	encoded, err := payload.MarshalJSON()
	if err != nil {
		return nil, err
	}
	repaired := call
	repaired.Input = string(encoded)
	repaired.ToolName = InvalidToolName
	return &repaired, nil
}

// ── parseToolCall ─────────────────────────────────────────────────────────

// ParseToolCall validates and, if needed, repairs one call. It never returns
// an error: every failure path collapses into the synthetic invalid call,
// which is the whole point of stage 3.
func ParseToolCall(call RawToolCall, tools *ToolMap, repair RepairFn) ParsedToolCall {
	parsed, err := doParseToolCall(call, tools)
	if err == nil {
		return parsed
	}

	if repair != nil && isRepairable(err) {
		repaired, repairErr := repair(call, tools, err)
		if repairErr != nil {
			err = &ToolCallRepairError{
				Cause:         repairErr,
				OriginalError: err,
				Message:       "Error repairing tool call: " + repairErr.Error(),
			}
		} else if repaired != nil {
			// A second failure is NOT re-repaired.
			parsed, secondErr := doParseToolCall(*repaired, tools)
			if secondErr == nil {
				return parsed
			}
			err = secondErr
		}
		// A nil repair result keeps the ORIGINAL error, which is already in
		// `err`.
	}

	return invalidToolCall(call, err)
}

func isRepairable(err error) bool {
	switch err.(type) {
	case *NoSuchToolError, *InvalidToolInputError:
		return true
	}
	return false
}

// invalidToolCall builds the stage-3 result. `input` is the BEST-EFFORT parse of the raw
// string, falling back to the raw string itself when it is not JSON — so
// `input` can be either a parsed value or a bare string.
func invalidToolCall(call RawToolCall, err error) ParsedToolCall {
	input := bestEffortParse(call.Input)
	return ParsedToolCall{
		Type:             "tool-call",
		ToolCallID:       call.ToolCallID,
		ToolName:         call.ToolName,
		Input:            input,
		Dynamic:          true,
		Invalid:          true,
		Error:            err,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: call.ProviderMetadata,
	}
}

func bestEffortParse(raw string) json.RawMessage {
	if normalized, err := parseSecureJSONValue(raw); err == nil {
		if encoded, err := marshalJSONValue(normalized); err == nil {
			return encoded
		}
	}
	encoded, err := jsonutil.Marshal(raw)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

// doParseToolCall is stage 1: look the tool up, parse and validate the input.
func doParseToolCall(call RawToolCall, tools *ToolMap) (ParsedToolCall, error) {
	spec, ok := tools.Get(call.ToolName)
	if !ok {
		return ParsedToolCall{}, newNoSuchToolError(call.ToolName, tools.Names())
	}

	// An empty input validates as `{}`; it is NOT an error.
	var value json.RawMessage
	if strings.TrimSpace(call.Input) == "" {
		value = json.RawMessage("{}")
	} else {
		normalized, err := parseSecureJSONValue(call.Input)
		if err != nil {
			return ParsedToolCall{}, newInvalidToolInputError(call.ToolName, call.Input, NewJSONParseError(call.Input, err))
		}
		encoded, err := marshalJSONValue(normalized)
		if err != nil {
			return ParsedToolCall{}, newInvalidToolInputError(call.ToolName, call.Input, NewJSONParseError(call.Input, err))
		}
		value = encoded
	}
	if spec.Validate != nil {
		if err := spec.Validate(value); err != nil {
			return ParsedToolCall{}, newInvalidToolInputError(call.ToolName, call.Input, err)
		}
	}
	return ParsedToolCall{
		Type:             "tool-call",
		ToolCallID:       call.ToolCallID,
		ToolName:         call.ToolName,
		Input:            value,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: call.ProviderMetadata,
	}, nil
}

// parseSecureJSONValue parses JSON and rejects `__proto__` keys and
// `constructor.prototype` pairs at any depth, so a tool input can never
// smuggle a prototype into a consumer that evaluates it. Both the streaming
// "is parsable" check and tool validation use this exact parser.
func parseSecureJSONValue(input string) (jsonValue, error) {
	normalized, err := parseJSONValue([]byte(input))
	if err != nil {
		return jsonValue{}, err
	}
	if hasForbiddenPrototypeJSONValue(normalized) {
		return jsonValue{}, errors.New("Object contains forbidden prototype property")
	}
	return normalized, nil
}
