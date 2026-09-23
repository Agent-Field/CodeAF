//go:build !windows

package msgmodel

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// ToModelOptions tune the conversion. Both are pointers: an absent StripMedia
// is false, and an absent or non-positive ToolOutputMaxChars leaves outputs
// untruncated.
type ToModelOptions struct {
	StripMedia         *bool
	ToolOutputMaxChars *float64
}

func (o *ToModelOptions) stripMedia() bool {
	return o != nil && o.StripMedia != nil && *o.StripMedia
}

func (o *ToModelOptions) toolOutputMaxChars() *float64 {
	if o == nil {
		return nil
	}
	return o.ToolOutputMaxChars
}

// messageIDAscending mints the id of the synthetic "Attached media from tool
// result:" UIMessage. ConvertToModelMessages drops that id, so nothing
// observable depends on the value; the default is a package-local counter.
var messageIDAscending = defaultMessageIDAscending

var syntheticMessageCounter atomic.Uint64

func defaultMessageIDAscending() string {
	return "msg_synthetic_" + strconv.FormatFloat(float64(syntheticMessageCounter.Add(1)), 'f', -1, 64)
}

// SetMessageIDFactoryForTesting swaps the synthetic-message id source and
// returns a restore func.
func SetMessageIDFactoryForTesting(f func() string) func() {
	prev := messageIDAscending
	messageIDAscending = f
	return func() { messageIDAscending = prev }
}

// IsMedia reports whether a mime type is an image or a PDF.
func IsMedia(mime string) bool {
	return strings.HasPrefix(mime, "image/") || mime == "application/pdf"
}

// DifferentModel is a plain string comparison of "<providerID>/<modelID>"
// between the model about to be called and the model that produced the
// historical turn. When they differ, that turn's provider-specific metadata
// is stripped below.
func DifferentModel(model Model, msg Assistant) bool {
	return model.ProviderID+"/"+model.ID != msg.ProviderID+"/"+msg.ModelID
}

// supportsMediaInToolResult reports whether the provider SDK accepts media
// inside a tool result. No case matches the OpenRouter provider, so on that
// path tool-result media always reaches the model through the synthetic
// attachment message instead.
func supportsMediaInToolResult(model Model, mime string) bool {
	switch model.API.Npm {
	case "@ai-sdk/anthropic":
		return true
	case "@ai-sdk/openai":
		return true
	case "@ai-sdk/amazon-bedrock":
		return strings.HasPrefix(mime, "image/")
	case "@ai-sdk/google-vertex/anthropic":
		return true
	case "@ai-sdk/google":
		id := strings.ToLower(model.API.ID)
		return strings.Contains(id, "gemini-3") && !strings.Contains(id, "gemini-2")
	}
	return false
}

// TruncateToolOutput keeps the first maxChars characters of a tool output and
// appends a marker naming how many were dropped. A nil or non-positive limit
// leaves the text alone.
func TruncateToolOutput(text string, maxChars *float64) string {
	if maxChars == nil || !(*maxChars > 0) {
		return text
	}
	limit := int(*maxChars)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	omitted := len(runes) - limit
	return string(runes[:limit]) + "\n[Tool output truncated for compaction: omitted " + strconv.Itoa(omitted) + " chars]"
}

// toModelOutput is the output converter handed to ConvertToModelMessages for
// every tool name seen, regardless of that tool's state.
func toModelOutput(_ string, _ RawValue, output RawValue) ToolOutput {
	if s, ok := asJSONString(output); ok {
		return ToolOutput{Type: "text", Value: s}
	}
	// Object-ish covers arrays and null too, but senior-dev only ever hands it
	// the {text, attachments} shape or a string.
	if isJSONObjectish(output) {
		obj := RawObject(output)
		text, _ := obj.StringField("text")
		value := []any{}
		if text != "" {
			value = append(value, ToolOutputContentText{Type: "text", Text: text})
		}
		for _, att := range attachmentList(obj) {
			url, _ := att.StringField("url")
			if !strings.HasPrefix(url, "data:") || !strings.Contains(url, ",") {
				continue
			}
			mime, _ := att.StringField("mime")
			value = append(value, ToolOutputContentMedia{
				Type:      "media",
				MediaType: mime,
				Data:      afterFirstComma(url),
			})
		}
		return ToolOutput{Type: "content", Value: value}
	}
	return ToolOutput{Type: "json", Value: toJSONValue(output)}
}

// afterFirstComma is the payload of a data: URL; the whole url when there is
// no comma at all.
func afterFirstComma(url string) string {
	i := strings.Index(url, ",")
	if i == -1 {
		return url
	}
	return url[i+1:]
}

// ToModelMessages converts stored messages into the model-facing message
// list.
func ToModelMessages(input []WithParts, model Model, options *ToModelOptions) ([]ModelMessage, error) {
	result := []UIMessage{}
	// Tool names in first-seen order, deduplicated.
	var toolNames []string
	seenTool := map[string]bool{}

	for _, msg := range input {
		if len(msg.Parts) == 0 {
			continue
		}

		if user, ok := msg.Info.(User); ok {
			userMessage := UIMessage{ID: user.ID, Role: "user", Parts: []UIPart{}}
			for _, raw := range msg.Parts {
				// The three checks below are independent, not an else-if chain.
				if part, ok := raw.(TextPart); ok {
					if !boolValue(part.Ignored) && part.Text != "" {
						userMessage.Parts = append(userMessage.Parts, UIPart{Type: "text", Text: part.Text})
					}
				}
				if part, ok := raw.(FilePart); ok {
					if part.Mime != "text/plain" && part.Mime != "application/x-directory" {
						if options.stripMedia() && IsMedia(part.Mime) {
							name := "file"
							if part.Filename != nil {
								name = *part.Filename
							}
							userMessage.Parts = append(userMessage.Parts, UIPart{
								Type: "text",
								Text: "[Attached " + part.Mime + ": " + name + "]",
							})
						} else {
							userMessage.Parts = append(userMessage.Parts, UIPart{
								Type:      "file",
								URL:       part.URL,
								MediaType: part.Mime,
								Filename:  optionalStringValue(part.Filename),
							})
						}
					}
				}
				if _, ok := raw.(CompactionPart); ok {
					userMessage.Parts = append(userMessage.Parts, UIPart{Type: "text", Text: "What did we do so far?"})
				}
			}
			if len(userMessage.Parts) > 0 {
				result = append(result, userMessage)
			}
		}

		assistant, isAssistant := msg.Info.(Assistant)
		if !isAssistant {
			continue
		}

		differentModel := DifferentModel(model, assistant)
		var media []mediaAttachment

		// Drop the whole message on any error UNLESS it is a
		// MessageAbortedError and at least one part is neither step-start nor
		// reasoning.
		if assistant.Error != nil {
			hasSubstantivePart := false
			for _, raw := range msg.Parts {
				if raw.PartType() != PartTypeStepStart && raw.PartType() != PartTypeReasoning {
					hasSubstantivePart = true
					break
				}
			}
			if !(assistant.Error.IsAborted() && hasSubstantivePart) {
				continue
			}
		}

		assistantMessage := UIMessage{ID: assistant.ID, Role: "assistant", Parts: []UIPart{}}

		// Anthropic adaptive thinking can persist an empty text
		// part as a structural separator between signed reasoning blocks;
		// replay it as a single space so it survives the SDK's empty-text
		// filter.
		hasSignedReasoning := false
		for _, raw := range msg.Parts {
			part, ok := raw.(ReasoningPart)
			if !ok {
				continue
			}
			anthropic, ok := part.Metadata.Field("anthropic")
			if !ok {
				continue
			}
			if signature, ok := RawObject(anthropic).Field("signature"); ok && !isJSONNull(signature) {
				hasSignedReasoning = true
				break
			}
		}

		for _, raw := range msg.Parts {
			switch part := raw.(type) {
			case TextPart:
				text := part.Text
				if text == "" && hasSignedReasoning {
					text = " "
				}
				ui := UIPart{Type: "text", Text: text}
				if !differentModel {
					ui.ProviderMetadata = part.Metadata.Raw()
				}
				assistantMessage.Parts = append(assistantMessage.Parts, ui)

			case StepStartPart:
				assistantMessage.Parts = append(assistantMessage.Parts, UIPart{Type: "step-start"})

			case ToolPart:
				if !seenTool[part.Tool] {
					seenTool[part.Tool] = true
					toolNames = append(toolNames, part.Tool)
				}
				providerExecuted := part.ProviderExecuted()
				callMeta := providerMeta(part.Metadata)

				switch state := part.State.(type) {
				case ToolStateCompleted:
					// `time.compacted` is read for TRUTHINESS, so
					// a stored 0 behaves as "not compacted".
					compacted := state.Time.Compacted != nil && *state.Time.Compacted != 0
					outputText := "[Old tool result content cleared]"
					if !compacted {
						outputText = TruncateToolOutput(state.Output, options.toolOutputMaxChars())
					}
					var attachments []FilePart
					if !compacted && !options.stripMedia() && state.Attachments != nil {
						attachments = *state.Attachments
					}

					var finalAttachments []FilePart
					for _, a := range attachments {
						if IsMedia(a.Mime) && !supportsMediaInToolResult(model, a.Mime) {
							media = append(media, mediaAttachment{Mime: a.Mime, URL: a.URL, Filename: a.Filename})
						}
						if !IsMedia(a.Mime) || supportsMediaInToolResult(model, a.Mime) {
							finalAttachments = append(finalAttachments, a)
						}
					}

					output := jsonString(outputText)
					if len(finalAttachments) > 0 {
						output = encodeToolOutputObject(outputText, finalAttachments)
					}

					ui := UIPart{
						Type:       "tool-" + part.Tool,
						State:      UIToolOutputAvailable,
						ToolCallID: part.CallID,
						Input:      state.Input.Value(),
						Output:     output,
					}
					if providerExecuted {
						ui.ProviderExecuted = jsonTrue
					}
					if !differentModel {
						ui.CallProviderMetadata = callMeta
					}
					assistantMessage.Parts = append(assistantMessage.Parts, ui)

				case ToolStateError:
					// Only an `interrupted === true` metadata bag can carry a
					// replayable output; the cleanup drain never writes
					// `metadata.output`, so in practice this lands on the
					// output-error branch.
					var replay string
					replayable := false
					if state.Metadata.StrictTrue("interrupted") {
						if s, ok := state.Metadata.StringField("output"); ok {
							replay, replayable = s, true
						}
					}
					ui := UIPart{
						Type:       "tool-" + part.Tool,
						ToolCallID: part.CallID,
						Input:      state.Input.Value(),
					}
					if replayable {
						ui.State = UIToolOutputAvailable
						ui.Output = jsonString(replay)
					} else {
						ui.State = UIToolOutputError
						ui.ErrorText = state.Error
					}
					if providerExecuted {
						ui.ProviderExecuted = jsonTrue
					}
					if !differentModel {
						ui.CallProviderMetadata = callMeta
					}
					assistantMessage.Parts = append(assistantMessage.Parts, ui)

				case ToolStatePending, ToolStateRunning:
					// Pending/running replay as an error so no
					// tool_use block is left dangling.
					ui := UIPart{
						Type:       "tool-" + part.Tool,
						State:      UIToolOutputError,
						ToolCallID: part.CallID,
						Input:      part.State.ToolInput().Value(),
						ErrorText:  "[Tool execution was interrupted]",
					}
					if providerExecuted {
						ui.ProviderExecuted = jsonTrue
					}
					if !differentModel {
						ui.CallProviderMetadata = callMeta
					}
					assistantMessage.Parts = append(assistantMessage.Parts, ui)
					_ = state
				}

			case ReasoningPart:
				if differentModel {
					// Downgrade to text, or DROP the part entirely
					// when it trims to nothing.
					if strings.TrimSpace(part.Text) != "" {
						assistantMessage.Parts = append(assistantMessage.Parts, UIPart{Type: "text", Text: part.Text})
					}
					continue
				}
				assistantMessage.Parts = append(assistantMessage.Parts, UIPart{
					Type: "reasoning",
					Text: part.Text,
					// part.metadata passes straight through, NOT via
					// providerMeta(), unlike every tool branch above.
					ProviderMetadata: part.Metadata.Raw(),
				})
			}
		}

		if len(assistantMessage.Parts) > 0 {
			result = append(result, assistantMessage)
			if len(media) > 0 {
				// The synthetic user message carrying the extracted media.
				parts := []UIPart{{Type: "text", Text: SyntheticAttachmentPrompt}}
				for _, a := range media {
					parts = append(parts, UIPart{
						Type:      "file",
						URL:       a.URL,
						MediaType: a.Mime,
						Filename:  optionalStringValue(a.Filename),
					})
				}
				result = append(result, UIMessage{ID: messageIDAscending(), Role: "user", Parts: parts})
			}
		}
	}

	tools := make(map[string]ToolModelOutputFn, len(toolNames))
	for _, name := range toolNames {
		tools[name] = toModelOutput
	}

	// Drop any UIMessage whose parts are ALL step-start.
	filtered := make([]UIMessage, 0, len(result))
	for _, msg := range result {
		keep := false
		for _, part := range msg.Parts {
			if part.Type != "step-start" {
				keep = true
				break
			}
		}
		if keep {
			filtered = append(filtered, msg)
		}
	}

	return ConvertToModelMessages(filtered, &ConvertOptions{Tools: tools})
}

// ── small helpers ────────────────────────────────────────────────────────

type mediaAttachment struct {
	Mime     string
	URL      string
	Filename *string
}

var jsonTrue = RawValue("true")

func boolValue(b *bool) bool { return b != nil && *b }

func optionalStringValue(s *string) RawValue {
	if s == nil {
		return nil
	}
	return jsonString(*s)
}

func isJSONNull(raw []byte) bool {
	return len(raw) == 0 || string(trimSpace(raw)) == "null"
}

// isJSONObjectish reports an object, an array or null. An array falls through
// harmlessly (no text, no attachments) and null yields
// `{type:"content", value:[]}`; the only callers set `output` to a string or
// to `{text, attachments}`, so neither branch is reached.
func isJSONObjectish(raw []byte) bool {
	t := trimSpace(raw)
	if len(t) == 0 {
		return false
	}
	return t[0] == '{' || t[0] == '[' || string(t) == "null"
}

func attachmentList(obj RawObject) []RawObject {
	raw, ok := obj.Field("attachments")
	if !ok || isJSONNull(raw) {
		return nil
	}
	items, ok := arrayElements(raw)
	if !ok {
		return nil
	}
	out := make([]RawObject, 0, len(items))
	for _, item := range items {
		out = append(out, RawObject(item))
	}
	return out
}

// encodeToolOutputObject builds `{text, attachments}` with the attachment
// parts kept verbatim, so the FilePart bytes that reach toModelOutput are the
// stored ones.
func encodeToolOutputObject(text string, attachments []FilePart) RawValue {
	payload := struct {
		Text        string     `json:"text"`
		Attachments []FilePart `json:"attachments"`
	}{Text: text, Attachments: attachments}
	raw, err := jsonutil.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}
