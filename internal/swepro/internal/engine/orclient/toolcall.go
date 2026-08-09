package orclient

// Tool-call validation, repair, and the `invalid` tool — ENGINE-DESIGN §3.3a.
//
// A non-obvious three-stage fallback:
//
//  1. VALIDATE — `doParseToolCall` (`ai/dist/index.mjs:3723-3760`). An unknown
//     tool name raises NoSuchToolError; otherwise
//     `input.trim() === "" ? validate({}, schema) : parseAndValidate(input, schema)`
//     — note that an EMPTY input string validates `{}`, it is not an error.
//     A validation failure raises InvalidToolInputError.
//  2. REPAIR — `parseToolCall` (`:3657-3686`) calls
//     `experimental_repairToolCall` for THOSE TWO ERROR TYPES ONLY. codeaf's
//     implementation (`llm.ts:427-447`) lowercases a mis-cased tool name when a
//     lowercase tool exists, and otherwise rewrites the call to
//     `toolName:"invalid"` with `input: JSON.stringify({tool, error})`.
//     Semantics of the return value: an object is RE-VALIDATED (a second
//     failure is NOT re-repaired); `null` rethrows the ORIGINAL error; a throw
//     is wrapped in ToolCallRepairError.
//  3. SAFETY NET — and this is the part that must not be missed: if repair is
//     absent, returns null, or the repaired call still fails, `parseToolCall`
//     DOES NOT THROW. It returns a synthetic
//     `{type:"tool-call", …, dynamic:true, invalid:true, error}` (`:3687-3702`),
//     which the step layer emits as a normal `tool-call` part AND immediately as
//     a `tool-error` part, WITHOUT executing the tool (`:6226-6237`).
//
// Stage 3 is what keeps §2.2's exit condition working: the assistant message
// still ends up with a tool part, so `hasToolCalls` is true and the outer loop
// iterates, giving the model a chance to correct itself.
//
// `activeTools` excludes "invalid" (`llm.ts:452`) so the model can never CHOOSE
// it, but the tool map passed to the SDK includes it so repair can TARGET it.
//
// ── the validator seam ───────────────────────────────────────────────────
//
// `asSchema(tool.inputSchema).validate` is OPTIONAL
// (`@ai-sdk/provider-utils/dist/index.mjs:2105-2120`): a bare `jsonSchema({...})`
// has no `validate` at all and `safeValidateTypes` then accepts any parsed
// value, while a zod schema gets a real validator. Go has no zod, so
// ToolSpec.Validate is an injectable func with exactly those two behaviours —
// nil means "accept anything that parses", which is the `jsonSchema()` default.
// Porting a JSON Schema validator is a separate decision (it would be this
// repo's first non-stdlib dependency, ENGINE-DESIGN F3).

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// InvalidToolName is the tool repair rewrites an unrepairable call to.
const InvalidToolName = "invalid"

// ToolSpec is the map entry `doParseToolCall` looks up.
type ToolSpec struct {
	Name string
	// Validate is `asSchema(inputSchema).validate`. Nil accepts any value that
	// parsed as JSON.
	Validate func(input json.RawMessage) error
}

// ToolMap is `tools`, ordered because `NoSuchToolError.availableTools` is
// `Object.keys(tools)` and codeaf sorts the map with `localeCompare`
// (`llm.ts:308`) — a third localeCompare site, and one that changes the
// serialized request body and therefore prompt-cache hit rates.
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

// SortedToolMap is `Object.fromEntries(Object.entries(tools).toSorted(([a],[b]) =>
// a.localeCompare(b)))` (`llm.ts:308`).
func SortedToolMap(specs ...ToolSpec) *ToolMap {
	m := NewToolMap(specs...)
	sort.SliceStable(m.order, func(i, j int) bool {
		return jscompat.LocaleCompare(m.order[i], m.order[j]) < 0
	})
	return m
}

// Names is `Object.keys(tools)`.
func (m *ToolMap) Names() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.order...)
}

// ActiveTools is `Object.keys(sortedTools).filter(x => x !== "invalid")`
// (`llm.ts:452`).
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

// NoSuchToolError is `NoSuchToolError` — message text is the behavioural
// contract, since the repair callback puts `error.message` into the `invalid`
// tool's input and the model reads it.
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

// InvalidToolInputError is `InvalidToolInputError`.
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

// TypeValidationError is `@ai-sdk/provider`'s error of the same name
// (`dist/index.mjs:283-313`). It is what a real `validate` hook's rejection is
// wrapped in, and its message reaches the model verbatim through the `invalid`
// tool's input — so the exact format
//
//	Type validation failed: Value: <JSON.stringify(value)>.\nError message: <cause>
//
// is a behavioural contract, not a log line. ToolSpec.Validate implementations
// should return one of these.
type TypeValidationError struct {
	Value   json.RawMessage
	Cause   error
	Message string
}

func (e *TypeValidationError) Error() string { return e.Message }

// NewTypeValidationError builds one with the SDK's message format.
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

// JSONParseError is `@ai-sdk/provider`'s error of the same name. Its message
// format —
//
//	JSON parsing failed: Text: <text>.\nError message: <cause>
//
// — reaches the model verbatim through the `invalid` tool's input, so the
// prefix is reproduced. The CAUSE text is engine-specific (JavaScriptCore under
// Bun, V8 under Node, encoding/json here) and is a documented KNOWN DIVERGENCE;
// the fixture corpus normalises from `JSON parsing failed:` onwards, exactly
// like tools/diffharness's existing `normalize()` filter does for the same
// class of text.
type JSONParseError struct {
	Text    string
	Cause   error
	Message string
}

func (e *JSONParseError) Error() string { return e.Message }

// NewJSONParseError builds one with the SDK's message format.
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

// ToolCallRepairError wraps a THROW out of the repair callback (`:3677-3680`).
type ToolCallRepairError struct {
	Cause         error
	OriginalError error
	Message       string
}

func (e *ToolCallRepairError) Error() string { return e.Message }

// ── the call shapes ───────────────────────────────────────────────────────

// RawToolCall is the provider-side `LanguageModelV3ToolCall`: `input` is a raw
// JSON STRING, never a parsed value.
type RawToolCall struct {
	ToolCallID       string
	ToolName         string
	Input            string
	ProviderExecuted bool
	ProviderMetadata json.RawMessage
}

// ParsedToolCall is `parseToolCall`'s return value. `Invalid` marks the
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

// MarshalJSON writes the object-literal key order of the two return sites
// (`:3687-3702` for the invalid case, `:3752-3760` for the valid one).
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

// RepairFn is `experimental_repairToolCall`. Returning (nil, nil) is the JS
// `null` — rethrow the ORIGINAL error.
type RepairFn func(call RawToolCall, tools *ToolMap, failure error) (*RawToolCall, error)

// ── codeaf's repair callback (llm.ts:427-447) ─────────────────────────────

// CodeafRepairToolCall is codeaf's `experimental_repairToolCall`.
//
// Two arms, in order:
//
//  1. if the LOWERCASED name differs from the emitted one AND a tool with the
//     lowercase name exists → return the call with the name lowercased. Note it
//     keeps the ORIGINAL input, so a call that failed VALIDATION (not
//     name-lookup) and happens to be mis-cased gets re-validated against the
//     lowercase tool's schema and can fail a second time — which then lands in
//     stage 3 rather than the `invalid` tool.
//  2. otherwise → rewrite to `toolName:"invalid"` with
//     `input: JSON.stringify({tool, error})`. The callback must return `input`
//     as a raw JSON STRING (`LanguageModelV3ToolCall.input: string`), so
//     codeaf's JSON.stringify is correct and a Go port must not hand back a map.
func CodeafRepairToolCall(call RawToolCall, tools *ToolMap, failure error) (*RawToolCall, error) {
	lower := jsLowerCase(call.ToolName)
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

// ParseToolCall is `parseToolCall` (`ai/dist/index.mjs:3657-3703`). It never
// returns an error: every failure path collapses into the synthetic invalid
// call, which is the whole point of stage 3.
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
		// `repairedToolCall == null` rethrows the ORIGINAL error, which is
		// already in `err`.
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

// invalidToolCall is `:3687-3702`. `input` is the BEST-EFFORT parse of the raw
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
	encoded, err := jscompat.Stringify(raw)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

// doParseToolCall is `doParseToolCall` (`:3723-3760`).
func doParseToolCall(call RawToolCall, tools *ToolMap) (ParsedToolCall, error) {
	spec, ok := tools.Get(call.ToolName)
	if !ok {
		return ParsedToolCall{}, newNoSuchToolError(call.ToolName, tools.Names())
	}

	// `input.trim() === ""` validates `{}` — an empty input is NOT an error.
	var value json.RawMessage
	if jscompat.Trim(call.Input) == "" {
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

// parseSecureJSONValue is provider-utils' secureJsonParse: JSON.parse followed
// by rejection of own `__proto__` keys and `constructor.prototype` pairs at
// any depth. Both the streaming "is parsable" check and tool validation use
// this exact parser.
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
