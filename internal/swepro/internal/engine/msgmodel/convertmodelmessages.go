package msgmodel

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ConvertToModelMessages is a port of `convertToModelMessages`
// (ai/dist/index.mjs:8311-8551) — half 2 of the pipeline described in
// ENGINE-DESIGN §5.4. The load-bearing rule (R1) is that a `step-start` part
// FLUSHES the current assistant block (`:8529-8535`), so one multi-step
// assistant UIMessage expands into an alternating
// assistant / tool / assistant / tool… run of ModelMessages.
//
// Deliberate omissions, all unreachable from codeaf:
//   - `options.convertDataPart` — codeaf never passes one, so `data-*` parts
//     evaluate to undefined and are dropped (user: by `.filter(isNonNullable)`
//     at :8367; assistant: by the explicit `!= null` guard at :8437).
//   - `source-url` / `source-document` parts match no branch on an assistant
//     message: they are ignored AND do not break the block (:8519-8526). Same
//     here — they simply never enter `block`.

// ToolModelOutputFn is the `tool.toModelOutput` callback
// (ai/dist/index.mjs:1668). `output` is the raw JS value; a nil `output` is
// `undefined`.
type ToolModelOutputFn func(toolCallID string, input RawValue, output RawValue) ToolOutput

// ConvertOptions is the subset of `convertToModelMessages`' options codeaf and
// the fixtures use.
type ConvertOptions struct {
	IgnoreIncompleteToolCalls bool
	Tools                     map[string]ToolModelOutputFn
}

func (o *ConvertOptions) tool(name string) ToolModelOutputFn {
	if o == nil || o.Tools == nil {
		return nil
	}
	return o.Tools[name]
}

// ConvertToModelMessages is `convertToModelMessages(messages, options)`.
func ConvertToModelMessages(messages []UIMessage, options *ConvertOptions) ([]ModelMessage, error) {
	modelMessages := []ModelMessage{}

	if options != nil && options.IgnoreIncompleteToolCalls {
		// :8314-8321 — a shallow message copy with incomplete tool parts filtered.
		filtered := make([]UIMessage, 0, len(messages))
		for _, message := range messages {
			parts := make([]UIPart, 0, len(message.Parts))
			for _, part := range message.Parts {
				if part.isTool() && (part.State == UIToolInputStreaming || part.State == UIToolInputAvailable) {
					continue
				}
				parts = append(parts, part)
			}
			message.Parts = parts
			filtered = append(filtered, message)
		}
		messages = filtered
	}

	for _, message := range messages {
		switch message.Role {
		case "system":
			// :8323-8338 — non-text parts are silently filtered (the v5
			// MessageConversionError for them is gone in v6), text is joined
			// with "" and providerMetadata from every text part is
			// shallow-merged one level.
			var content bytes.Buffer
			merged := []RawField{}
			for _, part := range message.Parts {
				if !part.isText() {
					continue
				}
				content.WriteString(part.Text)
				if len(part.ProviderMetadata) == 0 || string(part.ProviderMetadata) == "null" {
					continue
				}
				for _, f := range RawObject(part.ProviderMetadata).Fields() {
					merged = upsertField(merged, f)
				}
			}
			msg := ModelMessage{Role: "system", Content: content.String()}
			if len(merged) > 0 {
				msg.ProviderOptions = encodeFields(merged)
			}
			modelMessages = append(modelMessages, msg)

		case "user":
			// :8339-8368
			content := []any{}
			for _, part := range message.Parts {
				switch {
				case part.isText():
					content = append(content, TextContent{
						Type:            "text",
						Text:            part.Text,
						ProviderOptions: nonNull(part.ProviderMetadata),
					})
				case part.isFile():
					content = append(content, FileContent{
						Type:            "file",
						MediaType:       part.MediaType,
						Filename:        part.Filename,
						Data:            part.URL,
						ProviderOptions: nonNull(part.ProviderMetadata),
					})
				}
				// Every other part kind (reasoning, tool-*, source-*,
				// step-start, data-* with no convertDataPart) evaluates to
				// undefined and is dropped by .filter(isNonNullable).
			}
			modelMessages = append(modelMessages, ModelMessage{Role: "user", Content: content})

		case "assistant":
			// :8369-8542
			var block []UIPart
			processBlock := func() error {
				if len(block) == 0 {
					return nil
				}
				content := []any{}
				for _, part := range block {
					switch {
					case part.isText():
						content = append(content, TextContent{
							Type:            "text",
							Text:            part.Text,
							ProviderOptions: nonNull(part.ProviderMetadata),
						})
					case part.isFile():
						content = append(content, FileContent{
							Type:            "file",
							MediaType:       part.MediaType,
							Filename:        part.Filename,
							Data:            part.URL,
							ProviderOptions: nonNull(part.ProviderMetadata),
						})
					case part.isReasoning():
						content = append(content, ReasoningContent{
							Type: "reasoning",
							Text: part.Text,
							// :8399 sets this key UNCONDITIONALLY — an
							// explicit null stays a null, only `undefined`
							// disappears (via JSON.stringify).
							ProviderOptions: part.ProviderMetadata,
						})
					case part.isTool():
						toolName := part.ResolveToolName()
						if part.State == UIToolInputStreaming {
							// :8403 — emits nothing at all.
							break
						}
						content = append(content, ToolCallContent{
							Type:             "tool-call",
							ToolCallID:       part.ToolCallID,
							ToolName:         toolName,
							Input:            toolCallInput(part),
							ProviderExecuted: part.ProviderExecuted,
							ProviderOptions:  nonNull(part.CallProviderMetadata),
						})
						if part.Approval != nil {
							content = append(content, ToolApprovalRequestContent{
								Type:       "tool-approval-request",
								ApprovalID: part.Approval.ID,
								ToolCallID: part.ToolCallID,
							})
						}
						if isStrictTrue(part.ProviderExecuted) &&
							part.State != UIToolApprovalResponded &&
							(part.State == UIToolOutputAvailable || part.State == UIToolOutputError) {
							// :8419-8434 — provider-executed results stay
							// INSIDE the assistant message, with errorMode
							// "json" (contrast the tool-role message below).
							resultMeta := part.ResultProviderMetadata
							if len(resultMeta) == 0 || string(resultMeta) == "null" {
								resultMeta = part.CallProviderMetadata
							}
							errorMode := errorModeNone
							output := part.Output
							if part.State == UIToolOutputError {
								errorMode = errorModeJSON
								output = jsonString(part.ErrorText)
							}
							content = append(content, ToolResultContent{
								Type:            "tool-result",
								ToolCallID:      part.ToolCallID,
								ToolName:        toolName,
								Output:          createToolModelOutput(part.ToolCallID, part.Input, output, options.tool(toolName), errorMode),
								ProviderOptions: nonNull(resultMeta),
							})
						}
					case part.isData():
						// convertDataPart is never supplied — :8437 drops it.
					default:
						// :8442 — unreachable: `block` only ever receives the
						// five kinds above. Text matches `${part}` on a plain
						// object.
						return fmt.Errorf("Unsupported part: [object Object]")
					}
				}
				modelMessages = append(modelMessages, ModelMessage{Role: "assistant", Content: content})

				// :8443-8449 — provider-executed parts are excluded from the
				// tool-role message unless they carry an approval decision.
				toolParts := make([]UIPart, 0, len(block))
				for _, part := range block {
					if !part.isTool() {
						continue
					}
					if !isStrictTrue(part.ProviderExecuted) || approvalDecided(part) {
						toolParts = append(toolParts, part)
					}
				}
				if len(toolParts) > 0 {
					toolContent := []any{}
					for _, toolPart := range toolParts {
						if approvalDecided(toolPart) {
							toolContent = append(toolContent, ToolApprovalResponseContent{
								Type:             "tool-approval-response",
								ApprovalID:       toolPart.Approval.ID,
								Approved:         toolPart.Approval.Approved,
								Reason:           toolPart.Approval.Reason,
								ProviderExecuted: toolPart.ProviderExecuted,
							})
						}
						if isStrictTrue(toolPart.ProviderExecuted) {
							continue
						}
						switch toolPart.State {
						case UIToolOutputDenied:
							// :8478 — `approval.reason ?? "Tool execution denied."`
							var reason any = "Tool execution denied."
							if toolPart.Approval != nil && len(nonNull(toolPart.Approval.Reason)) > 0 {
								reason = toolPart.Approval.Reason
							}
							toolContent = append(toolContent, ToolResultContent{
								Type:            "tool-result",
								ToolCallID:      toolPart.ToolCallID,
								ToolName:        toolPart.ResolveToolName(),
								Output:          ToolOutput{Type: "error-text", Value: reason},
								ProviderOptions: nonNull(toolPart.CallProviderMetadata),
							})
						case UIToolOutputError, UIToolOutputAvailable:
							toolName := toolPart.ResolveToolName()
							errorMode := errorModeNone
							output := toolPart.Output
							if toolPart.State == UIToolOutputError {
								errorMode = errorModeText
								output = jsonString(toolPart.ErrorText)
							}
							toolContent = append(toolContent, ToolResultContent{
								Type:            "tool-result",
								ToolCallID:      toolPart.ToolCallID,
								ToolName:        toolName,
								Output:          createToolModelOutput(toolPart.ToolCallID, toolPart.Input, output, options.tool(toolName), errorMode),
								ProviderOptions: nonNull(toolPart.CallProviderMetadata),
							})
						}
					}
					// :8509-8514 — pushed ONLY if non-empty. A block whose
					// tool parts are all input-available yields an assistant
					// message with a dangling tool-call and no tool message,
					// which is exactly what message-v2.ts:947-956 prevents.
					if len(toolContent) > 0 {
						modelMessages = append(modelMessages, ModelMessage{Role: "tool", Content: toolContent})
					}
				}
				block = nil
				return nil
			}

			for _, part := range message.Parts {
				if part.isText() || part.isReasoning() || part.isFile() || part.isTool() || part.isData() {
					block = append(block, part)
					continue
				}
				if part.Type == "step-start" {
					if err := processBlock(); err != nil {
						return nil, err
					}
				}
			}
			if err := processBlock(); err != nil {
				return nil, err
			}

		default:
			return nil, &MessageConversionError{Message: "Unsupported role: " + message.Role}
		}
	}

	return modelMessages, nil
}

// ── helpers ──────────────────────────────────────────────────────────────

const (
	errorModeNone = ""
	errorModeText = "text"
	errorModeJSON = "json"
)

// toolCallInput is :8404 —
// `part.state === "output-error" ? part.input ?? ("rawInput" in part ? part.rawInput : undefined) : part.input`.
// `?? ` is nullish, so a JSON `null` input on an output-error part falls
// through to rawInput.
func toolCallInput(part UIPart) RawValue {
	if part.State != UIToolOutputError {
		return part.Input
	}
	if len(part.Input) > 0 && string(part.Input) != "null" {
		return part.Input
	}
	if len(part.RawInput) > 0 {
		return part.RawInput
	}
	return nil
}

// approvalDecided is `part.approval?.approved != null`.
func approvalDecided(part UIPart) bool {
	return part.Approval != nil &&
		len(part.Approval.Approved) > 0 &&
		string(bytes.TrimSpace(part.Approval.Approved)) != "null"
}

// nonNull collapses TS `x != null ? {k:x} : {}` — an absent OR explicitly-null
// value omits the key.
func nonNull(v RawValue) RawValue {
	if len(v) == 0 || string(bytes.TrimSpace(v)) == "null" {
		return nil
	}
	return v
}

func isStrictTrue(v RawValue) bool {
	return string(bytes.TrimSpace(v)) == "true"
}

func jsonString(s string) RawValue {
	raw, err := jscompat.Stringify(s)
	if err != nil {
		return nil
	}
	return raw
}

// createToolModelOutput — ai/dist/index.mjs:1655-1671.
func createToolModelOutput(toolCallID string, input, output RawValue, tool ToolModelOutputFn, errorMode string) ToolOutput {
	switch errorMode {
	case errorModeText:
		return ToolOutput{Type: "error-text", Value: getErrorMessage(output)}
	case errorModeJSON:
		return ToolOutput{Type: "error-json", Value: toJSONValue(output)}
	}
	if tool != nil {
		return tool(toolCallID, input, output)
	}
	if s, ok := asJSONString(output); ok {
		return ToolOutput{Type: "text", Value: s}
	}
	return ToolOutput{Type: "json", Value: toJSONValue(output)}
}

// getErrorMessage — @ai-sdk/provider/dist/index.mjs:91-102. An Error instance
// cannot survive JSON, so only the null / string / other arms are reachable.
func getErrorMessage(output RawValue) any {
	if len(output) == 0 || string(bytes.TrimSpace(output)) == "null" {
		return "unknown error"
	}
	if s, ok := asJSONString(output); ok {
		return s
	}
	return string(compactJSON(output))
}

// toJSONValue — ai/dist/index.mjs:1673-1675: `undefined` becomes null.
func toJSONValue(output RawValue) any {
	if len(output) == 0 {
		return json.RawMessage("null")
	}
	return output
}

func asJSONString(raw RawValue) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return "", false
	}
	return s, true
}

func compactJSON(raw []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// upsertField is one level of `{...acc, ...part.providerMetadata}`: a repeated
// key keeps its ORIGINAL position and takes the newer value.
func upsertField(acc []RawField, f RawField) []RawField {
	for i := range acc {
		if acc[i].Key == f.Key {
			acc[i].Value = f.Value
			return acc
		}
	}
	return append(acc, f)
}

func encodeFields(fields []RawField) RawValue {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := jscompat.Stringify(f.Key)
		if err != nil {
			return nil
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(f.Value)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
