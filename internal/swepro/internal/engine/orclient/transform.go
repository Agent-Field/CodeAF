package orclient

// The reachable half of src/provider/transform.ts, per ENGINE-DESIGN §3.2.
//
// All eight default-pool entries resolve to providerID "openrouter",
// api.npm "@openrouter/ai-sdk-provider" and api.id == model.id, which leaves
// exactly TWO live branches of `normalizeMessages` (transform.ts:60-340):
// unconditional surrogate sanitisation (:80-125) and the DeepSeek
// empty-reasoning stub (:287-303). Everything else is guarded by a provider
// signal none of the pool models carry — the Anthropic/Bedrock empty-content
// filters, the claude toolCallId scrub, the Anthropic tool_use reorder, the
// Mistral early-return, `applyCaching` (BUGS-KEPT #1) and the `sdkKey` remap.
// The interleaved-field folding branch is explicitly excluded for this npm
// package (transform.ts:305-309) and is omitted (BUGS-KEPT #9).
//
// Also here: the sampling-knob substring tables (transform.ts:478-513) and the
// OpenRouter arm of `options()` (:1071-1077, :1175-1177), because both are rows
// of the ENGINE-DESIGN §3.1 field-provenance table.

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
)

// ModelAPI is the `api` sub-object of Provider.Model.
type ModelAPI struct {
	Npm string `json:"npm"`
	ID  string `json:"id"`
}

// ModelCapabilities is the slice of `Provider.Model.capabilities` request
// assembly reads. `temperature` gates whether a temperature is sent at all
// (`llm.ts:251-253`); `input` gates `unsupportedParts`.
type ModelCapabilities struct {
	Temperature bool            `json:"temperature"`
	Reasoning   bool            `json:"reasoning"`
	Attachment  bool            `json:"attachment"`
	ToolCall    bool            `json:"toolcall"`
	Input       map[string]bool `json:"input"`
	Output      map[string]bool `json:"output"`
}

// ModelLimit is `Provider.Model.limit`.
type ModelLimit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input"`
	Output  float64  `json:"output"`
}

// Model is the Provider.Model projection this package needs. Field order is the
// TS object-literal order so a fixture can round-trip it.
type Model struct {
	ProviderID   string            `json:"providerID"`
	ID           string            `json:"id"`
	API          ModelAPI          `json:"api"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Limit        ModelLimit        `json:"limit"`
}

// ── sanitizeSurrogates (transform.ts:22-24) ───────────────────────────────

// SanitizeSurrogates replaces every UNPAIRED UTF-16 surrogate with U+FFFD.
//
// The TS is a single regex with a LOOKBEHIND:
//
//	/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g
//
// Go's RE2 has no lookbehind and no lookahead, so this is a hand-rolled UTF-16
// scan — and it MUST be UTF-16, not runes: the whole point is code units, and a
// surrogate pair must survive untouched while its halves individually do not.
//
// ENGINE-DESIGN R5 flags that a lone surrogate may be unreachable in Go, since
// `encoding/json` maps a `\uD800` escape to U+FFFD on decode. It is not
// unreachable in principle: a Go string is a byte string and can carry the
// WTF-8 encoding of a surrogate code point (ED A0 80 … ED BF BF), which is what
// a non-strict decoder or a byte-level splice produces. Both forms are handled.
//
// The original string is returned BY VALUE when no replacement happens, so a
// CESU-8-encoded (surrogate-pair) input is not silently re-encoded to canonical
// UTF-8 — TS returns the input unchanged in that case too.
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

// ── unsupportedParts (transform.ts:393-429) ───────────────────────────────

// mimeToModality is `transform.ts:12-18`.
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

// UnsupportedParts runs UNCONDITIONALLY, before normalizeMessages
// (`transform.ts:432`). It only touches ARRAY-content user messages, and only
// their `file` parts: a part whose modality the model does not accept becomes a
// text part telling the model to inform the user.
//
// The `part.type === "image"` arm (the empty-base64 check, `:401-412`) is an
// AI-SDK-v4 part type that `ModelMessage` v6 cannot carry, so it is dead and
// omitted.
//
// DIVERGENCE (bounded): TS indexes `model.capabilities.input[modality]` without
// a guard, so a Provider.Model whose `capabilities.input` is undefined throws a
// TypeError. Go treats a nil map as "supports nothing" and rewrites the part.
// Every real catalog entry has the field, so the input domain of the divergence
// is a malformed catalog.
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
				if decoded := jsString(rawJSONValue(file.Filename)); decoded != "" {
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

// Message is `ProviderTransform.message(msgs, model, options)`
// (`transform.ts:431-476`) restricted to what an OpenRouter model reaches:
// `unsupportedParts`, then `normalizeMessages`. `applyCaching` is guarded on an
// anthropic/claude/alibaba signal none of the eight default pool models carries
// (BUGS-KEPT #1), and the `sdkKey` remap is a no-op because
// `sdkKey("@openrouter/ai-sdk-provider") === "openrouter" === providerID`.
//
// This is the seam `llm.ts:482-496` installs as a `wrapLanguageModel`
// middleware, fired ONLY when `args.type === "stream"`. In Go it is a plain
// call made immediately before BuildRequestBody.
func Message(msgs []msgmodel.ModelMessage, model Model) []msgmodel.ModelMessage {
	return NormalizeMessages(UnsupportedParts(msgs, model), model)
}

// ── normalizeMessages, reachable branches only ────────────────────────────

// NormalizeMessages is `normalizeMessages(msgs, model, options)` restricted to
// the branches an OpenRouter model can reach.
//
// The TS MUTATES its argument in place (`msg.content = ...`) and codeaf relies
// on that nowhere observable, so this returns a new slice and leaves the input
// alone — the same observable, minus the aliasing hazard.
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
			// `if (!Array.isArray(msg.content)) return msg`
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
	// The TS switch has no default arm, so a message with an unknown role maps
	// to `undefined` and later blows up. Go keeps the message.
	return msg
}

// sanitizeToolResultOutput is transform.ts:65-78. `json` / `error-json` /
// `execution-denied` outputs are deliberately untouched.
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

// deepseekReasoningStub is transform.ts:287-303 — "Deepseek requires all
// assistant messages to have reasoning on them".
//
// Every assistant message gets `{type:"reasoning", text:""}` APPENDED AT THE
// END of its content (after any tool-calls), unless it already carries a
// reasoning part. String content becomes `[{type:"text", text}]` first, and an
// EMPTY string produces no text part at all (`...(msg.content ? [...] : [])`).
//
// This fires for every DeepSeek API id, including the default
// `deepseek/deepseek-v4-flash-0731`, so it is on the hot path, not an edge case.
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

// ── sampling knobs (transform.ts:478-513) ─────────────────────────────────
//
// These are ID-SUBSTRING matches, not a model enum: the substring table is the
// port, so a pool entry nobody anticipated behaves the same way in both
// implementations.

// Temperature is `ProviderTransform.temperature(model)`.
func Temperature(model Model) *float64 {
	id := strings.ToLower(model.ID)
	switch {
	case strings.Contains(id, "qwen"):
		return f64(0.55)
	case strings.Contains(id, "claude"):
		return nil
	case strings.Contains(id, "gemini"):
		return f64(1.0)
	case strings.Contains(id, "glm-4.6"):
		return f64(1.0)
	case strings.Contains(id, "glm-4.7"):
		return f64(1.0)
	case strings.Contains(id, "minimax-m2"):
		return f64(1.0)
	case strings.Contains(id, "kimi-k2"):
		for _, s := range []string{"thinking", "k2.", "k2p", "k2-5"} {
			if strings.Contains(id, s) {
				return f64(1.0)
			}
		}
		return f64(0.6)
	}
	return nil
}

// TopP is `ProviderTransform.topP(model)`.
func TopP(model Model) *float64 {
	id := strings.ToLower(model.ID)
	if strings.Contains(id, "qwen") {
		return f64(1)
	}
	for _, s := range []string{"minimax-m2", "gemini", "kimi-k2.5", "kimi-k2p5", "kimi-k2-5"} {
		if strings.Contains(id, s) {
			return f64(0.95)
		}
	}
	return nil
}

// TopK is `ProviderTransform.topK(model)`.
func TopK(model Model) *float64 {
	id := strings.ToLower(model.ID)
	if strings.Contains(id, "minimax-m2") {
		for _, s := range []string{"m2.", "m25", "m21"} {
			if strings.Contains(id, s) {
				return f64(40)
			}
		}
		return f64(20)
	}
	if strings.Contains(id, "gemini") {
		return f64(64)
	}
	return nil
}

// MaxOutputTokens is `ProviderTransform.maxOutputTokens(model)`
// (`transform.ts:1280-1282`): `Math.min(model.limit.output, OUTPUT_TOKEN_MAX) ||
// OUTPUT_TOKEN_MAX`, with the JS `||` falsiness that turns a 0 (or NaN) limit
// into the cap. Delegated to internal/engine/calc so the module-load
// OUTPUT_TOKEN_MAX env seam lives in exactly one place.
func MaxOutputTokens(model Model) float64 {
	return calc.MaxOutputTokens(calc.Model{Limit: calc.ModelLimit{
		Context: model.Limit.Context,
		Input:   model.Limit.Input,
		Output:  model.Limit.Output,
	}})
}

func f64(v float64) *float64 { return &v }

// ── options() / providerOptions(), OpenRouter arms only ───────────────────

// OptionsInput mirrors `ProviderTransform.options`'s parameter object.
type OptionsInput struct {
	Model     Model  `json:"model"`
	SessionID string `json:"sessionID"`
}

// Options is the OpenRouter-reachable subset of `ProviderTransform.options`
// (`transform.ts:1043-1185`). Key insertion order is the TS assignment order,
// which is what reaches the wire after the top-level spread:
//
//	usage            (:1071-1073, npm === "@openrouter/ai-sdk-provider")
//	reasoning        (:1074-1076, only when api.id contains "gemini-3")
//	prompt_cache_key (:1175-1177, providerID === "openrouter")
//
// Every other arm is gated on a providerID / npm none of the default pool
// entries has.
func Options(input OptionsInput) *Object {
	result := NewObject()
	if input.Model.API.Npm == "@openrouter/ai-sdk-provider" || input.Model.API.Npm == "@llmgateway/ai-sdk-provider" {
		usage := NewObject()
		usage.SetBool("include", true)
		result.SetObject("usage", usage)
		if strings.Contains(input.Model.API.ID, "gemini-3") {
			reasoning := NewObject()
			reasoning.SetString("effort", "high")
			result.SetObject("reasoning", reasoning)
		}
	}
	if input.Model.ProviderID == "openrouter" {
		result.SetString("prompt_cache_key", input.SessionID)
	}
	return result
}

// ProviderOptions is `ProviderTransform.providerOptions(model, options)`
// (`transform.ts:1230-1278`) for this npm package: `sdkKey(npm)` is
// "openrouter", so the whole merged option bag is wrapped under that one key —
// which is exactly the namespace `doStream` unwraps and spreads over the body
// (ENGINE-DESIGN §3.1). Callers that go straight to BuildRequestBody can skip
// the round-trip; this exists so the wrapping is testable on its own.
func ProviderOptions(model Model, options *Object) *Object {
	out := NewObject()
	out.SetObject(sdkKeyFor(model.API.Npm), options)
	return out
}

func sdkKeyFor(npm string) string {
	switch npm {
	case "@openrouter/ai-sdk-provider":
		return "openrouter"
	}
	// The full sdkKey table (transform.ts:27-57) is dead for this package's
	// scope; the fallback is `model.providerID`, and for the eight pool entries
	// that is "openrouter" too.
	return "openrouter"
}
