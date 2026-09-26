//go:build !windows

package msgmodel

import "encoding/json"

// UIMessage / UIPart are the intermediate value ToModelMessages builds before
// handing it to ConvertToModelMessages.
//
// A UIPart is ONE flat struct rather than a Go union: the parts are
// duck-typed on their `type` string, and the tool discriminant is a DYNAMIC
// string `"tool-" + toolName`, so an interface would buy nothing. UIMessages
// are never serialised as an output, so field order here is documentation
// rather than contract.
type UIMessage struct {
	ID    string   `json:"id"`
	Role  string   `json:"role"`
	Parts []UIPart `json:"parts"`
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
	ToolCallID             string   `json:"toolCallId,omitempty"`
	State                  string   `json:"state,omitempty"`
	Input                  RawValue `json:"input,omitempty"`
	RawInput               RawValue `json:"rawInput,omitempty"`
	Output                 RawValue `json:"output,omitempty"`
	ErrorText              string   `json:"errorText,omitempty"`
	ProviderExecuted       RawValue `json:"providerExecuted,omitempty"`
	CallProviderMetadata   RawValue `json:"callProviderMetadata,omitempty"`
	ResultProviderMetadata RawValue `json:"resultProviderMetadata,omitempty"`
}

// UI part `state` values.
const (
	UIToolInputStreaming  = "input-streaming"
	UIToolInputAvailable  = "input-available"
	UIToolOutputAvailable = "output-available"
	UIToolOutputError     = "output-error"
)

// Part-kind predicates over the `type` string.
func (p UIPart) isStaticTool() bool  { return len(p.Type) >= 5 && p.Type[:5] == "tool-" }
func (p UIPart) isDynamicTool() bool { return p.Type == "dynamic-tool" }
func (p UIPart) isTool() bool        { return p.isStaticTool() || p.isDynamicTool() }
func (p UIPart) isData() bool        { return len(p.Type) >= 5 && p.Type[:5] == "data-" }
func (p UIPart) isText() bool        { return p.Type == "text" }
func (p UIPart) isFile() bool        { return p.Type == "file" }
func (p UIPart) isReasoning() bool   { return p.Type == "reasoning" }

// ResolveToolName returns the tool a part refers to: a dynamic part carries
// the name; a static part's name is everything after the first dash of its
// type, which preserves every internal dash. That is load-bearing because the
// type is built as `"tool-" + part.tool` and tool names contain dashes.
func (p UIPart) ResolveToolName() string {
	if p.isDynamicTool() {
		return p.ToolName
	}
	return staticToolName(p.Type)
}

func staticToolName(typ string) string {
	// Everything after the first "-", and "" when there is no "-" at all.
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
// otherwise.
type ModelMessage struct {
	Role            string   `json:"role"`
	Content         any      `json:"content"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// The content-part structs below declare fields in the order they reach the
// wire body.

// TextContent is a text content part.
type TextContent struct {
	Type            string   `json:"type"`
	Text            string   `json:"text"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// FileContent is a file content part.
type FileContent struct {
	Type            string   `json:"type"`
	MediaType       string   `json:"mediaType"`
	Filename        RawValue `json:"filename,omitempty"`
	Data            string   `json:"data"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// ReasoningContent is a reasoning content part. `providerOptions` is copied
// unconditionally (contrast text/file); an absent value is dropped by
// `omitempty`.
type ReasoningContent struct {
	Type            string   `json:"type"`
	Text            string   `json:"text"`
	ProviderOptions RawValue `json:"providerOptions,omitempty"`
}

// ToolCallContent is a tool-call content part.
type ToolCallContent struct {
	Type             string   `json:"type"`
	ToolCallID       string   `json:"toolCallId"`
	ToolName         string   `json:"toolName"`
	Input            RawValue `json:"input,omitempty"`
	ProviderExecuted RawValue `json:"providerExecuted,omitempty"`
	ProviderOptions  RawValue `json:"providerOptions,omitempty"`
}

// ToolResultContent is a tool-result content part.
type ToolResultContent struct {
	Type            string     `json:"type"`
	ToolCallID      string     `json:"toolCallId"`
	ToolName        string     `json:"toolName"`
	Output          ToolOutput `json:"output"`
	ProviderOptions RawValue   `json:"providerOptions,omitempty"`
}

// ToolOutput is a tool result as the model sees it: one of
// text / json / error-text / error-json / content.
type ToolOutput struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// ToolOutputContentText / ToolOutputContentMedia are the two element shapes
// toModelOutput emits inside `{type:"content"}`.
type ToolOutputContentText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolOutputContentMedia struct {
	Type      string `json:"type"`
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// MessageConversionError is the only error ConvertToModelMessages raises.
type MessageConversionError struct {
	Message string
}

func (e *MessageConversionError) Error() string { return e.Message }

var _ json.Marshaler = Parts(nil)
