package msgmodel

import "encoding/json"

// UIMessage / UIPart mirror the AI SDK v6 UI message shape
// (ai/dist/index.d.ts, `UIMessage`), the intermediate value
// `toModelMessagesEffect` builds before handing it to `convertToModelMessages`.
//
// A UIPart is ONE flat struct rather than a Go union: the SDK itself
// duck-types with `isTextUIPart` / `isFileUIPart` / `isToolUIPart`
// (ai/dist/index.mjs:5235-5262), and the tool discriminant is a DYNAMIC string
// `"tool-" + toolName`, so an interface would buy nothing. UIMessages are
// never serialised as an output, only as fixture input, so field order here is
// documentation rather than contract.
type UIMessage struct {
	ID    string   `json:"id"`
	Role  string   `json:"role"`
	Parts []UIPart `json:"parts"`
}

// UIToolApproval is `part.approval`. codeaf never produces one; it is here so
// the convertToModelMessages port is complete against ai@6.0.168.
type UIToolApproval struct {
	ID       string   `json:"id"`
	Approved RawValue `json:"approved,omitempty"`
	Reason   RawValue `json:"reason,omitempty"`
}

type UIPart struct {
	Type string `json:"type"`

	// text / reasoning
	Text string `json:"text,omitempty"`

	// text / file / reasoning
	ProviderMetadata RawValue `json:"providerMetadata,omitempty"`

	// file
	MediaType string   `json:"mediaType,omitempty"`
	Filename  RawValue `json:"filename,omitempty"`
	URL       string   `json:"url,omitempty"`

	// dynamic-tool
	ToolName string `json:"toolName,omitempty"`

	// tool-* / dynamic-tool
	ToolCallID             string          `json:"toolCallId,omitempty"`
	State                  string          `json:"state,omitempty"`
	Input                  RawValue        `json:"input,omitempty"`
	RawInput               RawValue        `json:"rawInput,omitempty"`
	Output                 RawValue        `json:"output,omitempty"`
	ErrorText              string          `json:"errorText,omitempty"`
	ProviderExecuted       RawValue        `json:"providerExecuted,omitempty"`
	CallProviderMetadata   RawValue        `json:"callProviderMetadata,omitempty"`
	ResultProviderMetadata RawValue        `json:"resultProviderMetadata,omitempty"`
	Approval               *UIToolApproval `json:"approval,omitempty"`
}

// UI part `state` values (ai/dist/index.d.ts, ToolUIPart).
const (
	UIToolInputStreaming    = "input-streaming"
	UIToolInputAvailable    = "input-available"
	UIToolOutputAvailable   = "output-available"
	UIToolOutputError       = "output-error"
	UIToolOutputDenied      = "output-denied"
	UIToolApprovalRequested = "approval-requested"
	UIToolApprovalResponded = "approval-responded"
)

// isStaticToolUIPart / isDynamicToolUIPart / isToolUIPart / isDataUIPart —
// ai/dist/index.mjs:5235-5255.
func (p UIPart) isStaticTool() bool  { return len(p.Type) >= 5 && p.Type[:5] == "tool-" }
func (p UIPart) isDynamicTool() bool { return p.Type == "dynamic-tool" }
func (p UIPart) isTool() bool        { return p.isStaticTool() || p.isDynamicTool() }
func (p UIPart) isData() bool        { return len(p.Type) >= 5 && p.Type[:5] == "data-" }
func (p UIPart) isText() bool        { return p.Type == "text" }
func (p UIPart) isFile() bool        { return p.Type == "file" }
func (p UIPart) isReasoning() bool   { return p.Type == "reasoning" }

// ToolName resolves `getToolName` (ai/dist/index.mjs:5257-5262): a dynamic
// part carries the name; a static part is `part.type.split("-").slice(1)
// .join("-")`, which strips only the FIRST segment and preserves every
// internal dash — load-bearing, because codeaf builds the type as
// `"tool-" + part.tool` (message-v2.ts:912) and tool names contain dashes.
func (p UIPart) ResolveToolName() string {
	if p.isDynamicTool() {
		return p.ToolName
	}
	return staticToolName(p.Type)
}

func staticToolName(typ string) string {
	// split("-").slice(1).join("-") == everything after the first "-", and ""
	// when there is no "-" at all (slice(1) of a 1-element array is empty).
	for i := 0; i < len(typ); i++ {
		if typ[i] == '-' {
			return typ[i+1:]
		}
	}
	return ""
}

// ── ModelMessage (the convertToModelMessages output) ─────────────────────

// ModelMessage is one entry of the `ModelMessage[]` handed to the provider.
// `Content` is a string for `role:"system"` and a content-part slice
// otherwise, exactly as the SDK builds it.
type ModelMessage struct {
	Role            string   `json:"role"`
	Content         any      `json:"content"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// The content-part structs below declare fields in the SDK's object-literal
// order, because that order reaches the wire body byte-for-byte.

// TextContent — ai/dist/index.mjs:8345-8349, :8380-8384.
type TextContent struct {
	Type            string   `json:"type"`
	Text            string   `json:"text"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// FileContent — ai/dist/index.mjs:8352-8358, :8386-8392.
type FileContent struct {
	Type            string   `json:"type"`
	MediaType       string   `json:"mediaType"`
	Filename        RawValue `json:"filename,omitempty"`
	Data            string   `json:"data"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// ReasoningContent — ai/dist/index.mjs:8395-8400. `providerOptions` is written
// UNCONDITIONALLY there (contrast text/file); an undefined value is then
// dropped by JSON.stringify, which `omitempty` reproduces.
type ReasoningContent struct {
	Type            string   `json:"type"`
	Text            string   `json:"text"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// ToolCallContent — ai/dist/index.mjs:8404-8411.
type ToolCallContent struct {
	Type             string   `json:"type"`
	ToolCallID       string   `json:"toolCallId"`
	ToolName         string   `json:"toolName"`
	Input            RawValue `json:"input,omitempty"`
	ProviderExecuted RawValue `json:"providerExecuted,omitempty"`
	ProviderOptions  RawValue `json:"providerOptions,omitempty"`
}

// ToolApprovalRequestContent — ai/dist/index.mjs:8413-8417.
type ToolApprovalRequestContent struct {
	Type       string `json:"type"`
	ApprovalID string `json:"approvalId"`
	ToolCallID string `json:"toolCallId"`
}

// ToolResultContent — ai/dist/index.mjs:8421-8433, :8494-8507.
type ToolResultContent struct {
	Type            string     `json:"type"`
	ToolCallID      string     `json:"toolCallId"`
	ToolName        string     `json:"toolName"`
	Output          ToolOutput `json:"output"`
	ProviderOptions RawValue   `json:"providerOptions,omitempty"`
}

// ToolApprovalResponseContent — ai/dist/index.mjs:8461-8467.
type ToolApprovalResponseContent struct {
	Type             string   `json:"type"`
	ApprovalID       string   `json:"approvalId"`
	Approved         RawValue `json:"approved,omitempty"`
	Reason           RawValue `json:"reason,omitempty"`
	ProviderExecuted RawValue `json:"providerExecuted,omitempty"`
}

// ToolOutput is LanguageModelV3ToolResultOutput: one of
// text / json / error-text / error-json / content.
type ToolOutput struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// ToolOutputContentText / ToolOutputContentMedia are the two element shapes
// codeaf's `toModelOutput` emits inside `{type:"content"}`
// (message-v2.ts:773-783). Key order is codeaf's, not the SDK's, because the
// object literal is codeaf's.
type ToolOutputContentText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolOutputContentMedia struct {
	Type      string `json:"type"`
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// MessageConversionError is the only error convertToModelMessages raises
// (ai/dist/index.mjs:8531-8537). Message text is the behavioural contract.
type MessageConversionError struct {
	Message string
}

func (e *MessageConversionError) Error() string { return e.Message }

var _ json.Marshaler = Parts(nil)
