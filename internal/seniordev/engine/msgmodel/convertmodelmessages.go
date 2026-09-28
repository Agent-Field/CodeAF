//go:build !windows

package msgmodel

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// ConvertToModelMessages turns UI messages into the model-facing message
// list. The load-bearing rule is that a `step-start` part FLUSHES the current
// assistant block, so one multi-step assistant UIMessage expands into an
// alternating assistant / tool / assistant / tool… run of ModelMessages.
//
// Deliberate omissions, all unreachable from senior-dev:
//   - custom data-part conversion: senior-dev never supplies one, so `data-*`
//     parts are dropped.
//   - `source-url` / `source-document` parts match no branch on an assistant
//     message: they are ignored AND do not break the block. They simply never
//     enter `block`.

// ToolModelOutputFn converts a tool's stored output into the shape the model
// sees. `output` is the raw JSON value; a nil `output` is absent.
type ToolModelOutputFn func(toolCallID string, input RawValue, output RawValue) ToolOutput

// ConvertOptions are the conversion options.
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

// ConvertToModelMessages is the conversion described above.
func ConvertToModelMessages(messages []UIMessage, options *ConvertOptions) ([]ModelMessage, error) {
	modelMessages := []ModelMessage{}

	if options != nil && options.IgnoreIncompleteToolCalls {
		// A shallow message copy with incomplete tool parts filtered.
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
			// Non-text parts are silently filtered, text is joined with ""
			// and providerMetadata from every text part is shallow-merged one
			// level.
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
				// step-start, data-*) is dropped.
			}
			modelMessages = append(modelMessages, ModelMessage{Role: "user", Content: content})

		case "assistant":
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
							// Set unconditionally: an explicit null stays a
							// null; only an absent value disappears.
							ProviderOptions: part.ProviderMetadata,
						})
					case part.isTool():
						toolName := part.ResolveToolName()
						if part.State == UIToolInputStreaming {
							// Emits nothing at all.
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
						if isStrictTrue(part.ProviderExecuted) &&
							(part.State == UIToolOutputAvailable || part.State == UIToolOutputError) {
							// Provider-executed results stay INSIDE the
							// assistant message, with errorMode
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
						// No data-part conversion is supplied; dropped.
					default:
						// Unreachable: `block` only ever receives the five
						// kinds above.
						return fmt.Errorf("Unsupported part: %s", part.Type)
					}
				}
				modelMessages = append(modelMessages, ModelMessage{Role: "assistant", Content: content})

				// Provider-executed parts are excluded from the tool-role
				// message: their results already sit in the assistant message.
				toolParts := make([]UIPart, 0, len(block))
				for _, part := range block {
					if !part.isTool() {
						continue
					}
					if !isStrictTrue(part.ProviderExecuted) {
						toolParts = append(toolParts, part)
					}
				}
				if len(toolParts) > 0 {
					toolContent := []any{}
					for _, toolPart := range toolParts {
						switch toolPart.State {
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
					// Pushed only if non-empty. A block whose tool parts are
					// all input-available would yield an assistant message
					// with a dangling tool-call and no tool message;
					// ToModelMessages prevents that by replaying pending and
					// running tools as errors.
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

// toolCallInput is the input recorded on the call. On an output-error part a
// null or absent input falls through to rawInput.
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

// nonNull returns nil for an absent or explicitly-null value, so the key is
// omitted.
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
	raw, err := jsonutil.Marshal(s)
	if err != nil {
		return nil
	}
	return raw
}

// createToolModelOutput builds the tool-result output the model sees: error
// text, error JSON, the tool's own converter, or a plain text/json value.
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

// getErrorMessage renders a stored error output as text: "unknown error" for
// null or absent, the string itself, or the compact JSON otherwise.
func getErrorMessage(output RawValue) any {
	if len(output) == 0 || string(bytes.TrimSpace(output)) == "null" {
		return "unknown error"
	}
	if s, ok := asJSONString(output); ok {
		return s
	}
	return string(compactJSON(output))
}

// toJSONValue maps an absent value to JSON null.
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
		key, err := jsonutil.Marshal(f.Key)
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
