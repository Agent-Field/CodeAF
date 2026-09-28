//go:build !windows

package orclient

// Message normalisation before a request: surrogate sanitisation,
// unsupported-modality rewriting and the DeepSeek empty-reasoning stub; plus
// the OpenRouter-specific request options.

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// ModelAPI is the `api` sub-object of a catalog model.
type ModelAPI struct {
	Npm string `json:"npm"`
	ID  string `json:"id"`
}

// ModelCapabilities is the slice of a catalog model's capabilities request
// assembly reads. `temperature` gates whether a temperature is sent at all;
// `input` gates UnsupportedParts.
type ModelCapabilities struct {
	Temperature bool            `json:"temperature"`
	Reasoning   bool            `json:"reasoning"`
	Attachment  bool            `json:"attachment"`
	ToolCall    bool            `json:"toolcall"`
	Input       map[string]bool `json:"input"`
	Output      map[string]bool `json:"output"`
}

// ModelLimit is a catalog model's limits.
type ModelLimit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input"`
	Output  float64  `json:"output"`
}

// Model is the catalog-model projection this package needs.
type Model struct {
	ProviderID   string            `json:"providerID"`
	ID           string            `json:"id"`
	API          ModelAPI          `json:"api"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Limit        ModelLimit        `json:"limit"`
}

// ── SanitizeSurrogates ───────────────────────────────────────────────────

// SanitizeSurrogates replaces every UNPAIRED UTF-16 surrogate with U+FFFD.
//
// This is a hand-rolled UTF-16 scan, and it MUST be UTF-16, not runes: the
// whole point is code units, and a surrogate pair must survive untouched while
// its halves individually do not.
//
// A lone surrogate is reachable: `encoding/json` maps a `\uD800` escape to
// U+FFFD on decode, but a Go string is a byte string and can carry the WTF-8
// encoding of a surrogate code point (ED A0 80 … ED BF BF), which is what a
// non-strict decoder or a byte-level splice produces. Both forms are handled.
//
// The original string is returned BY VALUE when no replacement happens, so a
// CESU-8-encoded (surrogate-pair) input is not silently re-encoded to canonical
// UTF-8.
func SanitizeSurrogates(content string) string {
	if !mayContainSurrogate(content) {
		return content
	}
	units := utf16Units(content)
	changed := false
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			// High surrogate: paired only if followed by a low surrogate.
			if i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
				i++
				continue
			}
			units[i] = 0xFFFD
			changed = true
		case u >= 0xDC00 && u <= 0xDFFF:
			// Low surrogate reached without having been consumed as the tail
			// of a pair, i.e. not preceded by a high surrogate.
			units[i] = 0xFFFD
			changed = true
		}
	}
	if !changed {
		return content
	}
	return string(utf16.Decode(units))
}

// mayContainSurrogate is the cheap pre-test: a surrogate code unit can only
// appear in a Go string as the WTF-8 sequence ED A0..BF xx, or as a genuine
// astral character (F0..F4 lead byte) whose UTF-16 form is a well-formed pair.
// A well-formed pair is never rewritten, so only the WTF-8 form matters.
func mayContainSurrogate(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			return true
		}
	}
	return false
}

// utf16Units decodes a Go string to UTF-16 code units, accepting the WTF-8
// encoding of an unpaired surrogate (which utf8.DecodeRuneInString rejects).
func utf16Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			cp := rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			out = append(out, uint16(cp))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			out = append(out, 0xFFFD)
			i++
			continue
		}
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			out = append(out, uint16(hi), uint16(lo))
		} else {
			out = append(out, uint16(r))
		}
		i += size
	}
	return out
}

// ── UnsupportedParts ─────────────────────────────────────────────────────

// mimeToModality maps a mime type to the capability key that gates it.
func mimeToModality(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case mime == "application/pdf":
		return "pdf"
	}
	return ""
}

// UnsupportedParts runs before NormalizeMessages. It only touches
// ARRAY-content user messages, and only their `file` parts: a part whose
// modality the model does not accept becomes a text part telling the model to
// inform the user.
//
// A nil capabilities.input map counts as "supports nothing", so a malformed
// catalog entry rewrites every file part rather than failing.
func UnsupportedParts(msgs []msgmodel.ModelMessage, model Model) []msgmodel.ModelMessage {
	out := make([]msgmodel.ModelMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		if msg.Role != "user" {
			continue
		}
		parts, ok := msg.Content.([]any)
		if !ok {
			continue
		}
		out[i].Content = mapParts(parts, func(part any) any {
			file, ok := part.(msgmodel.FileContent)
			if !ok {
				return part
			}
			modality := mimeToModality(file.MediaType)
			if modality == "" {
				return part
			}
			if model.Capabilities.Input[modality] {
				return part
			}
			name := modality
			if len(file.Filename) > 0 {
				if decoded := textOf(rawJSONValue(file.Filename)); decoded != "" {
					name = `"` + decoded + `"`
				}
			}
			return msgmodel.TextContent{
				Type: "text",
				Text: "ERROR: Cannot read " + name + " (this model does not support " + modality + " input). Inform the user.",
			}
		})
	}
	return out
}

// Message prepares a message list for an OpenRouter model: UnsupportedParts,
// then NormalizeMessages. It is called immediately before BuildRequestBody.
func Message(msgs []msgmodel.ModelMessage, model Model) []msgmodel.ModelMessage {
	return NormalizeMessages(UnsupportedParts(msgs, model), model)
}

// ── normalizeMessages, reachable branches only ────────────────────────────

// NormalizeMessages sanitises surrogates in every text-bearing part and, for
// a DeepSeek model, appends the empty reasoning stub. It returns a new slice
// and leaves the input alone.
func NormalizeMessages(msgs []msgmodel.ModelMessage, model Model) []msgmodel.ModelMessage {
	out := make([]msgmodel.ModelMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = sanitizeMessage(msg)
	}
	if strings.Contains(strings.ToLower(model.API.ID), "deepseek") {
		out = deepseekReasoningStub(out)
	}
	return out
}

func sanitizeMessage(msg msgmodel.ModelMessage) msgmodel.ModelMessage {
	switch msg.Role {
	case "tool":
		parts, ok := msg.Content.([]any)
		if !ok {
			// Non-array content is left alone.
			return msg
		}
		msg.Content = mapParts(parts, func(part any) any {
			if tr, ok := part.(msgmodel.ToolResultContent); ok {
				return sanitizeToolResultOutput(tr)
			}
			return part
		})
		return msg

	case "system":
		if s, ok := msg.Content.(string); ok {
			msg.Content = SanitizeSurrogates(s)
		}
		return msg

	case "user":
		if s, ok := msg.Content.(string); ok {
			msg.Content = SanitizeSurrogates(s)
			return msg
		}
		parts, ok := msg.Content.([]any)
		if !ok {
			return msg
		}
		msg.Content = mapParts(parts, func(part any) any {
			if t, ok := part.(msgmodel.TextContent); ok {
				t.Text = SanitizeSurrogates(t.Text)
				return t
			}
			return part
		})
		return msg

	case "assistant":
		if s, ok := msg.Content.(string); ok {
			msg.Content = SanitizeSurrogates(s)
			return msg
		}
		parts, ok := msg.Content.([]any)
		if !ok {
			return msg
		}
		msg.Content = mapParts(parts, func(part any) any {
			switch p := part.(type) {
			case msgmodel.TextContent:
				p.Text = SanitizeSurrogates(p.Text)
				return p
			case msgmodel.ReasoningContent:
				p.Text = SanitizeSurrogates(p.Text)
				return p
			case msgmodel.ToolResultContent:
				return sanitizeToolResultOutput(p)
			}
			return part
		})
		return msg
	}
	// An unknown role is passed through untouched.
	return msg
}

// sanitizeToolResultOutput sanitises the text of a tool result. `json` /
// `error-json` / `execution-denied` outputs are deliberately untouched.
func sanitizeToolResultOutput(tr msgmodel.ToolResultContent) msgmodel.ToolResultContent {
	switch tr.Output.Type {
	case "text", "error-text":
		if s, ok := tr.Output.Value.(string); ok {
			tr.Output.Value = SanitizeSurrogates(s)
		}
	case "content":
		items, ok := tr.Output.Value.([]any)
		if !ok {
			return tr
		}
		tr.Output.Value = mapParts(items, func(item any) any {
			if t, ok := item.(msgmodel.ToolOutputContentText); ok {
				t.Text = SanitizeSurrogates(t.Text)
				return t
			}
			return item
		})
	}
	return tr
}

func mapParts(parts []any, f func(any) any) []any {
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = f(p)
	}
	return out
}

// deepseekReasoningStub: DeepSeek requires every assistant message to carry
// reasoning.
//
// Every assistant message gets `{type:"reasoning", text:""}` APPENDED AT THE
// END of its content (after any tool-calls), unless it already carries a
// reasoning part. String content becomes `[{type:"text", text}]` first, and an
// EMPTY string produces no text part at all.
//
// This fires for every DeepSeek API id.
func deepseekReasoningStub(msgs []msgmodel.ModelMessage) []msgmodel.ModelMessage {
	out := make([]msgmodel.ModelMessage, len(msgs))
	for i, msg := range msgs {
		if msg.Role != "assistant" {
			out[i] = msg
			continue
		}
		if parts, ok := msg.Content.([]any); ok {
			hasReasoning := false
			for _, p := range parts {
				if _, is := p.(msgmodel.ReasoningContent); is {
					hasReasoning = true
					break
				}
			}
			if hasReasoning {
				out[i] = msg
				continue
			}
			next := make([]any, 0, len(parts)+1)
			next = append(next, parts...)
			next = append(next, msgmodel.ReasoningContent{Type: "reasoning", Text: ""})
			msg.Content = next
			out[i] = msg
			continue
		}
		text, _ := msg.Content.(string)
		next := make([]any, 0, 2)
		if text != "" {
			next = append(next, msgmodel.TextContent{Type: "text", Text: text})
		}
		next = append(next, msgmodel.ReasoningContent{Type: "reasoning", Text: ""})
		msg.Content = next
		out[i] = msg
	}
	return out
}

// ── output cap ────────────────────────────────────────────────────────────
//
// No generation parameter (temperature, top_p, ...) has a built-in default;
// the provider's own defaults apply unless the caller sets one.
// MaxOutputTokens is the one value with a built-in default, below.

// MaxOutputTokens is `min(model.limit.output, OUTPUT_TOKEN_MAX)`, falling
// back to OUTPUT_TOKEN_MAX when the minimum is 0 or NaN. Delegated to
// internal/engine/calc so the SENIOR_DEV_OUTPUT_TOKEN_MAX read lives in exactly
// one place.
func MaxOutputTokens(model Model) float64 {
	return calc.MaxOutputTokens(calc.Model{Limit: calc.ModelLimit{
		Context: model.Limit.Context,
		Input:   model.Limit.Input,
		Output:  model.Limit.Output,
	}})
}

// ── Options / ProviderOptions, OpenRouter cases only ─────────────────────

// OptionsInput is what Options needs.
type OptionsInput struct {
	Model     Model  `json:"model"`
	SessionID string `json:"sessionID"`
}

// Options is the OpenRouter-specific request option bag, in the order the
// keys reach the wire after the top-level spread:
//
//	usage            (npm === "@openrouter/ai-sdk-provider")
//	prompt_cache_key (providerID === "openrouter")
//
// No reasoning effort is set here; it is sent only when a variant or config
// sets it.
func Options(input OptionsInput) *Object {
	result := NewObject()
	if input.Model.API.Npm == "@openrouter/ai-sdk-provider" || input.Model.API.Npm == "@llmgateway/ai-sdk-provider" {
		usage := NewObject()
		usage.SetBool("include", true)
		result.SetObject("usage", usage)
	}
	if input.Model.ProviderID == Service {
		result.SetString("prompt_cache_key", input.SessionID)
	}
	return result
}

// ProviderOptions wraps the merged option bag under the provider's SDK key,
// which for OpenRouter is "openrouter": exactly the namespace BuildRequestBody
// unwraps and spreads over the body. Callers that go straight to
// BuildRequestBody can skip the round-trip; this exists so the wrapping is
// testable on its own.
func ProviderOptions(model Model, options *Object) *Object {
	out := NewObject()
	out.SetObject(sdkKeyFor(model.API.Npm), options)
	return out
}

// sdkKeyFor is the key a model's options are kept under. Every model this
// client serves speaks OpenRouter's wire, whatever package name it came with,
// so the answer is always the one service identity.
func sdkKeyFor(npm string) string {
	return Service
}
