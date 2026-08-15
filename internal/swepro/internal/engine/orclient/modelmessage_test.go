package orclient

// Test-only decoder for the JSON form of `ModelMessage[]`.
//
// `msgmodel.ModelMessage.Content` is `any` — a string for `role:"system"` and a
// `[]any` of value-typed content structs otherwise — because that is the shape
// `convertToModelMessages` PRODUCES. Nothing in the production path ever has to
// read one back from JSON: the step layer hands the structs straight over. The
// fixture corpus does, so the decoder lives here rather than in msgmodel.
//
// Parts are decoded to the VALUE forms (msgmodel.TextContent, not a pointer),
// matching the msgmodel convention — the transform type-switches on values and
// a pointer would silently miss every branch.

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

func decodeModelMessages(t *testing.T, raw json.RawMessage) []msgmodel.ModelMessage {
	t.Helper()
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode ModelMessage[]: %v", err)
	}
	out := make([]msgmodel.ModelMessage, 0, len(entries))
	for _, e := range entries {
		out = append(out, decodeModelMessage(t, e))
	}
	return out
}

func decodeModelMessage(t *testing.T, raw json.RawMessage) msgmodel.ModelMessage {
	t.Helper()
	var shell struct {
		Role            string          `json:"role"`
		Content         json.RawMessage `json:"content"`
		ProviderOptions json.RawMessage `json:"providerOptions"`
	}
	if err := json.Unmarshal(raw, &shell); err != nil {
		t.Fatalf("decode ModelMessage: %v", err)
	}
	msg := msgmodel.ModelMessage{Role: shell.Role, ProviderOptions: shell.ProviderOptions}

	var asString string
	if err := json.Unmarshal(shell.Content, &asString); err == nil {
		msg.Content = asString
		return msg
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(shell.Content, &parts); err != nil {
		t.Fatalf("decode ModelMessage content: %v", err)
	}
	decoded := make([]any, 0, len(parts))
	for _, p := range parts {
		decoded = append(decoded, decodeContentPart(t, p))
	}
	msg.Content = decoded
	return msg
}

func decodeContentPart(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode content part: %v", err)
	}
	switch probe.Type {
	case "text":
		var p msgmodel.TextContent
		mustUnmarshal(t, raw, &p)
		return p
	case "file":
		var p msgmodel.FileContent
		mustUnmarshal(t, raw, &p)
		return p
	case "reasoning":
		var p msgmodel.ReasoningContent
		mustUnmarshal(t, raw, &p)
		return p
	case "tool-call":
		var p msgmodel.ToolCallContent
		mustUnmarshal(t, raw, &p)
		return p
	case "tool-result":
		return decodeToolResult(t, raw)
	case "tool-approval-request":
		var p msgmodel.ToolApprovalRequestContent
		mustUnmarshal(t, raw, &p)
		return p
	case "tool-approval-response":
		var p msgmodel.ToolApprovalResponseContent
		mustUnmarshal(t, raw, &p)
		return p
	}
	t.Fatalf("unknown content part type %q", probe.Type)
	return nil
}

func decodeToolResult(t *testing.T, raw json.RawMessage) msgmodel.ToolResultContent {
	t.Helper()
	var shell struct {
		Type            string          `json:"type"`
		ToolCallID      string          `json:"toolCallId"`
		ToolName        string          `json:"toolName"`
		Output          json.RawMessage `json:"output"`
		ProviderOptions json.RawMessage `json:"providerOptions"`
	}
	mustUnmarshal(t, raw, &shell)
	out := msgmodel.ToolResultContent{
		Type:            shell.Type,
		ToolCallID:      shell.ToolCallID,
		ToolName:        shell.ToolName,
		ProviderOptions: shell.ProviderOptions,
	}
	var outputShell struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	mustUnmarshal(t, shell.Output, &outputShell)
	out.Output.Type = outputShell.Type

	switch outputShell.Type {
	case "text", "error-text", "execution-denied":
		var s string
		mustUnmarshal(t, outputShell.Value, &s)
		out.Output.Value = s
	case "content":
		var items []json.RawMessage
		mustUnmarshal(t, outputShell.Value, &items)
		decoded := make([]any, 0, len(items))
		for _, item := range items {
			var probe struct {
				Type string `json:"type"`
			}
			mustUnmarshal(t, item, &probe)
			switch probe.Type {
			case "text":
				var p msgmodel.ToolOutputContentText
				mustUnmarshal(t, item, &p)
				decoded = append(decoded, p)
			case "media":
				var p msgmodel.ToolOutputContentMedia
				mustUnmarshal(t, item, &p)
				decoded = append(decoded, p)
			default:
				decoded = append(decoded, json.RawMessage(item))
			}
		}
		out.Output.Value = decoded
	default:
		// json / error-json carry an arbitrary value; keep the bytes.
		out.Output.Value = json.RawMessage(outputShell.Value)
	}
	return out
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}
