package orclient

// The chunk → stream-part translation (`internal/index.mjs:3833-4192`) —
// ENGINE-DESIGN §3.3.
//
// This is a straight transliteration of the `TransformStream`'s `transform` and
// `flush`, in the order the TS actually tests things, because the ORDER is the
// contract: `finish_reason` is captured BEFORE the delta of the same chunk; a
// zod parse failure or a top-level `error` payload RETURNS without looking at
// anything else; `reasoning-end` fires before `text-start`, not after.
//
// Three upstream bugs are reproduced deliberately:
//
//	BUGS-KEPT #5  index = toolCallDelta.index ?? toolCalls.length - 1  (:3962)
//	              — a first delta with no index targets slot -1, which in JS is
//	              a STRING PROPERTY on the array. `toolCalls.length` stays 0, so
//	              `hasToolCalls` is false and flush emits nothing: the tool call
//	              is silently DROPPED. Verified against the running provider.
//	BUGS-KEPT #6  the parsable-args check at :4064 has no `sent` guard, so
//	              `tool-input-end` + `tool-call` are re-emitted on EVERY
//	              subsequent delta once the buffer first parses.
//	(F2)          the two synthetic promotions to `tool-calls` at flush
//	              (:4098-4106), which are what keep the §2.2 exit condition
//	              consistent for models that report `stop` alongside tool calls.

import (
	"encoding/json"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/fixflag"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// toolCallSlot is one entry of the provider's `toolCalls` array.
type toolCallSlot struct {
	id           string
	name         string
	arguments    string
	inputStarted bool
	sent         bool
}

// toolCallArray models a JS array well enough for the -1 bug: an integer index
// ≥ 0 extends `length`, anything else is a plain property that does NOT.
type toolCallArray struct {
	slots  map[string]*toolCallSlot
	length int
}

func newToolCallArray() *toolCallArray {
	return &toolCallArray{slots: map[string]*toolCallSlot{}}
}

func (a *toolCallArray) get(i int) *toolCallSlot {
	return a.getKey(jscompat.FormatNumber(float64(i)))
}

func (a *toolCallArray) set(i int, slot *toolCallSlot) {
	a.setKey(jscompat.FormatNumber(float64(i)), slot)
}

func (a *toolCallArray) getKey(key string) *toolCallSlot { return a.slots[key] }

// setKey is JS array property assignment. Only canonical array-index property
// names extend `length`; fractional, negative and 2^32-1 indices remain plain
// string properties. The wire schema deliberately accepts any number for
// toolCallDelta.index, so this is observable.
func (a *toolCallArray) setKey(key string, slot *toolCallSlot) {
	a.slots[key] = slot
	if index, ok := arrayIndex(key); ok && int(index)+1 > a.length {
		a.length = int(index) + 1
	}
}

// iterate walks indices 0..length-1, exactly like `for (const x of arr)`. Holes
// yield nil, which the `if (toolCall && ...)` guard skips.
func (a *toolCallArray) iterate(f func(*toolCallSlot)) {
	for i := 0; i < a.length; i++ {
		f(a.get(i))
	}
}

// Translator is the stateful half of `doStream`: everything declared between
// `:3739` and `:3772` and mutated by the transform.
type Translator struct {
	// IncludeRawChunks is `options.includeRawChunks`. codeaf never sets it.
	IncludeRawChunks bool

	toolCalls       *toolCallArray
	seenToolCallIDs map[string]bool
	finishReason    FinishReason

	usage          calc.LanguageModelV3Usage
	openrouterUse  *Object
	rawUsage       json.RawMessage
	accumulated    []ReasoningDetail
	detailsOnCall  bool
	fileAnnotation []json.RawMessage

	promptTokensSeen     *float64
	completionTokensSeen *float64

	textStarted      bool
	reasoningStarted bool
	textID           string
	reasoningID      string
	responseID       string
	provider         *string

	// streamError is `withStreamErrorHandling`'s captured read error. It does
	// NOT throw: it surfaces as an `error` part at flush (`:4085-4090`).
	streamError json.RawMessage
	flushed     bool
}

// NewTranslator builds the initial state. `finishReason` starts as
// `createFinishReason("other")` (`:3742`), NOT undefined.
func NewTranslator() *Translator {
	return &Translator{
		toolCalls:       newToolCallArray(),
		seenToolCallIDs: map[string]bool{},
		openrouterUse:   NewObject(),
		finishReason:    FinishReason{Unified: FinishOther},
		accumulated:     []ReasoningDetail{},
	}
}

// SetStreamError records the mid-stream reader error `withStreamErrorHandling`
// catches (`:2551-2573`). It is never a thrown exception — the reader is
// cancelled and the stream CLOSES, so the error only shows up in flush.
func (t *Translator) SetStreamError(value json.RawMessage) { t.streamError = value }

// Transform is one `transform(chunk, controller)` call. A returned error is a
// THROW out of the transform (only `InvalidResponseDataError` from the tool-call
// accumulator), which tears the stream down; it is not an `error` part.
func (t *Translator) Transform(chunk Chunk) ([]StreamPart, error) {
	var out []StreamPart
	emit := func(p StreamPart) { out = append(out, p) }

	if t.IncludeRawChunks {
		emit(RawPart{RawValue: chunk.RawValue})
	}
	if !chunk.Success {
		t.finishReason = FinishReason{Unified: FinishError}
		emit(ErrorPart{Error: chunk.ParseError})
		return out, nil
	}
	value := chunk.Value
	if value.ErrorField != nil {
		t.finishReason = FinishReason{Unified: FinishError}
		emit(ErrorPart{Error: value.ErrorField})
		return out, nil
	}
	if value.Provider != nil && *value.Provider != "" {
		p := *value.Provider
		t.provider = &p
	}
	if value.ID != nil && *value.ID != "" {
		t.responseID = *value.ID
		emit(ResponseMetadataPart{ID: *value.ID})
	}
	if value.Model != nil && *value.Model != "" {
		emit(ResponseMetadataPart{ModelID: *value.Model, IsModel: true})
	}
	if value.Usage != nil {
		t.accumulateUsage(value.Usage)
	}

	// `const choice = value.choices[0]` — only the FIRST choice is ever read.
	var choice *Choice
	if len(value.Choices) > 0 {
		choice = &value.Choices[0]
	}
	if choice != nil && choice.FinishReason != nil {
		t.finishReason = MapOpenRouterFinishReason(choice.FinishReason)
	}
	if choice == nil || choice.Delta == nil {
		return out, nil
	}
	delta := choice.Delta

	emitReasoningChunk := func(text string) {
		if !t.reasoningStarted {
			t.reasoningID = generateId()
			emit(ReasoningStartPart{ID: t.reasoningID})
			t.reasoningStarted = true
		}
		id := t.reasoningID
		if id == "" {
			id = generateId()
		}
		emit(ReasoningDeltaPart{Delta: text, ID: id})
	}

	if delta.HasReasoningDeta && len(delta.ReasoningDetails) > 0 {
		for _, detail := range delta.ReasoningDetails {
			if detail.Type == ReasoningDetailText {
				if n := len(t.accumulated); n > 0 && t.accumulated[n-1].Type == ReasoningDetailText {
					last := &t.accumulated[n-1]
					// `(lastDetail.text || "") + (detail.text || "")` — a null
					// text on either side contributes "", and the result is
					// always a string, so a merge turns an ABSENT text into "".
					last.Text = rawStringLiteral(rawString(last.Text) + rawString(detail.Text))
					last.Signature = jsOr(last.Signature, detail.Signature)
					last.Format = jsOr(last.Format, detail.Format)
					last.Raw = nil
					continue
				}
				// `push({...detail})` — a COPY, so later merges do not mutate
				// the caller's entry.
				t.accumulated = append(t.accumulated, detail)
				continue
			}
			t.accumulated = append(t.accumulated, detail)
		}
		if !t.textStarted {
			for _, detail := range delta.ReasoningDetails {
				switch detail.Type {
				case ReasoningDetailText:
					emitReasoningChunk(rawString(detail.Text))
				case ReasoningDetailEncrypted:
					// Emits NOTHING — it only accumulates.
				case ReasoningDetailSummary:
					if detail.Summary != "" {
						emitReasoningChunk(detail.Summary)
					}
				}
			}
		}
	} else if delta.Reasoning != nil && *delta.Reasoning != "" && !t.textStarted {
		emitReasoningChunk(*delta.Reasoning)
	}

	// `if (delta.content)` is a FALSY test: "" produces nothing at all.
	if delta.Content != nil && *delta.Content != "" {
		if t.reasoningStarted && !t.textStarted {
			id := t.reasoningID
			if id == "" {
				id = generateId()
			}
			emit(ReasoningEndPart{ID: id, Details: t.detailsView()})
			t.reasoningStarted = false
		}
		if !t.textStarted {
			t.textID = t.responseID
			if t.textID == "" {
				t.textID = generateId()
			}
			emit(TextStartPart{ID: t.textID})
			t.textStarted = true
		}
		id := t.textID
		if id == "" {
			id = generateId()
		}
		emit(TextDeltaPart{Delta: *delta.Content, ID: id})
	}

	if delta.HasAnnotations {
		for _, annotation := range delta.Annotations {
			switch annotation.Type {
			case "url_citation":
				part := SourcePart{URL: annotation.URL}
				if annotation.Title != nil {
					part.Title = *annotation.Title
				}
				if annotation.Content != nil {
					part.Content = *annotation.Content
				}
				if annotation.StartIndex != nil {
					part.StartIndex = *annotation.StartIndex
				}
				if annotation.EndIndex != nil {
					part.EndIndex = *annotation.EndIndex
				}
				emit(part)
			case "file":
				t.fileAnnotation = append(t.fileAnnotation, annotation.Raw)
			}
			// `file_annotation` (the old format) is parsed and then IGNORED.
		}
	}

	if delta.HasToolCalls {
		parts, err := t.accumulateToolCalls(delta.ToolCalls)
		out = append(out, parts...)
		if err != nil {
			return out, err
		}
	}

	for _, image := range delta.Images {
		emit(FilePart{
			MediaType: getMediaType(image.URL, "image/jpeg"),
			Data:      base64FromDataURLLoose(image.URL),
		})
	}
	return out, nil
}

// accumulateToolCalls is `:3960-4083`.
func (t *Translator) accumulateToolCalls(deltas []ToolCallDelta) ([]StreamPart, error) {
	var out []StreamPart
	emit := func(p StreamPart) { out = append(out, p) }

	for _, d := range deltas {
		// BUGS-KEPT #5.
		fallback := t.toolCalls.length - 1
		if fixflag.Enabled("CODEAF_GO_FIX_ORCLIENT_TOOLCALL_INDEX") && fallback < 0 {
			fallback = 0
		}
		index := jscompat.FormatNumber(float64(fallback))
		if d.Index != nil {
			index = jscompat.FormatNumber(*d.Index)
		}

		if t.toolCalls.getKey(index) == nil {
			if d.Type == nil || *d.Type != "function" {
				return out, newInvalidResponseDataError("Expected 'function' type.", json.RawMessage(d.Raw))
			}
			if d.Name == nil {
				return out, newInvalidResponseDataError("Expected 'function.name' to be a string.", json.RawMessage(d.Raw))
			}
			toolCallID := ""
			if d.ID != nil {
				toolCallID = *d.ID
			}
			// Id uniqueness is enforced by the CLIENT, not the server.
			if toolCallID == "" || t.seenToolCallIDs[toolCallID] {
				toolCallID = generateId()
			}
			t.seenToolCallIDs[toolCallID] = true

			arguments := ""
			if d.Arguments != nil {
				arguments = *d.Arguments
			}
			slot := &toolCallSlot{id: toolCallID, name: *d.Name, arguments: arguments}
			t.toolCalls.setKey(index, slot)

			// The whole burst fires immediately when the FIRST delta already
			// carries parsable arguments (`:4009-4033`).
			if isParsableJSON(slot.arguments) {
				slot.inputStarted = true
				emit(ToolInputStartPart{ID: slot.id, ToolName: slot.name})
				emit(ToolInputDeltaPart{ID: slot.id, Delta: slot.arguments})
				emit(ToolInputEndPart{ID: slot.id})
				emit(t.toolCallPart(slot))
				slot.sent = true
			}
			continue
		}

		slot := t.toolCalls.getKey(index)
		if !slot.inputStarted {
			slot.inputStarted = true
			emit(ToolInputStartPart{ID: slot.id, ToolName: slot.name})
			if slot.arguments != "" {
				emit(ToolInputDeltaPart{ID: slot.id, Delta: slot.arguments})
			}
		}
		if d.Arguments != nil {
			slot.arguments += *d.Arguments
		}
		// Emitted on EVERY subsequent delta, even for null args.
		deltaText := ""
		if d.Arguments != nil {
			deltaText = *d.Arguments
		}
		emit(ToolInputDeltaPart{ID: slot.id, Delta: deltaText})

		// BUGS-KEPT #6: no `sent` guard.
		if isParsableJSON(slot.arguments) && (!fixflag.Enabled("CODEAF_GO_FIX_ORCLIENT_TOOLCALL_RESEND") || !slot.sent) {
			emit(ToolInputEndPart{ID: slot.id})
			emit(t.toolCallPart(slot))
			slot.sent = true
		}
	}
	return out, nil
}

// toolCallPart builds a `tool-call` and consumes the one-shot
// reasoning-details attachment.
func (t *Translator) toolCallPart(slot *toolCallSlot) ToolCallPart {
	part := ToolCallPart{
		ToolCallID: slot.id,
		ToolName:   slot.name,
		Input:      slot.arguments,
	}
	if !t.detailsOnCall {
		part.HasProviderMetadata = true
		part.Details = t.detailsView()
	}
	t.detailsOnCall = true
	return part
}

// detailsView hands out a LIVE reference to the accumulator — never a copy.
//
// In TS every part that carries `reasoning_details` holds the SAME array
// object, so a `reasoning-end` emitted at chunk 2 shows entries that arrived at
// chunk 3, and a merged `reasoning.text` entry shows its FINAL concatenated
// text everywhere it appears. Both are verified against the running provider.
// A per-emit copy is the obvious Go implementation and diverges on the very
// first stream that interleaves reasoning with text.
func (t *Translator) detailsView() ReasoningDetailsView {
	return detailsRef(&t.accumulated)
}

func (t *Translator) accumulateUsage(usage *calc.OpenRouterUsage) {
	computed := calc.ComputeTokenUsage(usage)
	// `Object.assign(usage.inputTokens, computed.inputTokens)` — a per-FIELD
	// merge. computeTokenUsage always writes all four input keys and all three
	// output keys (cacheWrite as `?? undefined`, which Object.assign DOES
	// copy), so the whole block is replaced every time. Reproduced literally.
	t.usage.InputTokens = computed.InputTokens
	t.usage.OutputTokens = computed.OutputTokens
	t.rawUsage = computed.Raw

	promptTokens := float64(0)
	if usage.PromptTokens != nil {
		promptTokens = *usage.PromptTokens
	}
	completionTokens := float64(0)
	if usage.CompletionTokens != nil {
		completionTokens = *usage.CompletionTokens
	}
	t.openrouterUse.SetNumber("promptTokens", promptTokens)
	if usage.PromptTokensDetails != nil {
		cached := float64(0)
		if usage.PromptTokensDetails.CachedTokens != nil {
			cached = *usage.PromptTokensDetails.CachedTokens
		}
		inner := NewObject()
		inner.SetNumber("cachedTokens", cached)
		t.openrouterUse.SetObject("promptTokensDetails", inner)
	}
	t.openrouterUse.SetNumber("completionTokens", completionTokens)
	if usage.CompletionTokensDetails != nil {
		reasoning := float64(0)
		if usage.CompletionTokensDetails.ReasoningTokens != nil {
			reasoning = *usage.CompletionTokensDetails.ReasoningTokens
		}
		inner := NewObject()
		inner.SetNumber("reasoningTokens", reasoning)
		t.openrouterUse.SetObject("completionTokensDetails", inner)
	}
	extra := usageExtras(usage.Raw)
	if extra.cost != nil {
		t.openrouterUse.SetNumber("cost", *extra.cost)
	}
	// `openrouterUsage.totalTokens = value.usage.total_tokens` is
	// UNCONDITIONAL once a usage object is present — the schema makes
	// total_tokens required, so it is never undefined here.
	if extra.totalTokens != nil {
		t.openrouterUse.SetNumber("totalTokens", *extra.totalTokens)
	}
	if extra.upstreamInferenceCost != nil {
		inner := NewObject()
		inner.SetNumber("upstreamInferenceCost", *extra.upstreamInferenceCost)
		t.openrouterUse.SetObject("costDetails", inner)
	}
	t.promptTokensSeen = &promptTokens
	t.completionTokensSeen = &completionTokens
}

type usageExtraFields struct {
	cost                  *float64
	totalTokens           *float64
	upstreamInferenceCost *float64
}

// usageExtras reads the three fields calc.OpenRouterUsage does not project:
// `cost`, `total_tokens` and `cost_details.upstream_inference_cost`. They only
// feed `providerMetadata.openrouter.usage`, which codeaf ignores entirely — but
// they are on the wire, so they are reproduced.
func usageExtras(raw json.RawMessage) usageExtraFields {
	var out usageExtraFields
	if len(raw) == 0 {
		return out
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return out
	}
	if v, ok := obj["cost"]; ok && !isJSONNull(v) {
		var n float64
		if err := json.Unmarshal(v, &n); err == nil {
			out.cost = &n
		}
	}
	if v, ok := obj["total_tokens"]; ok && !isJSONNull(v) {
		var n float64
		if err := json.Unmarshal(v, &n); err == nil {
			out.totalTokens = &n
		}
	}
	if v, ok := obj["cost_details"]; ok && !isJSONNull(v) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(v, &fields); err == nil {
			if u, ok := fields["upstream_inference_cost"]; ok && !isJSONNull(u) {
				var n float64
				if err := json.Unmarshal(u, &n); err == nil {
					out.upstreamInferenceCost = &n
				}
			}
		}
	}
	return out
}

// Flush is `flush(controller)` (`:4093-4192`).
func (t *Translator) Flush() []StreamPart {
	if t.flushed {
		return nil
	}
	t.flushed = true

	var out []StreamPart
	emit := func(p StreamPart) { out = append(out, p) }

	hasToolCalls := t.toolCalls.length > 0
	if t.streamError != nil {
		t.finishReason = FinishReason{Unified: FinishError}
		emit(ErrorPart{Error: t.streamError})
	}

	hasEncryptedReasoning := false
	for _, d := range t.accumulated {
		if d.Type == ReasoningDetailEncrypted && d.Data != "" {
			hasEncryptedReasoning = true
			break
		}
	}
	// The two synthetic promotions (ENGINE-DESIGN F2). `raw` is preserved.
	if hasToolCalls && hasEncryptedReasoning && t.finishReason.Unified == FinishStop {
		t.finishReason = FinishReason{Unified: FinishToolCalls, Raw: t.finishReason.Raw}
	}
	if hasToolCalls && t.finishReason.Unified == FinishOther {
		t.finishReason = FinishReason{Unified: FinishToolCalls, Raw: t.finishReason.Raw}
	}

	// Unsent tool calls are flushed ONLY when the final unified reason is
	// `tool-calls` — which is exactly what the promotions above buy.
	if t.finishReason.Unified == FinishToolCalls {
		t.toolCalls.iterate(func(slot *toolCallSlot) {
			if slot == nil || slot.sent {
				return
			}
			input := slot.arguments
			if !isParsableJSON(input) {
				input = "{}"
			}
			if !slot.inputStarted {
				emit(ToolInputStartPart{ID: slot.id, ToolName: slot.name})
				emit(ToolInputDeltaPart{ID: slot.id, Delta: input})
			}
			emit(ToolInputEndPart{ID: slot.id})
			part := ToolCallPart{ToolCallID: slot.id, ToolName: slot.name, Input: input}
			if !t.detailsOnCall {
				part.HasProviderMetadata = true
				part.Details = t.detailsView()
			}
			t.detailsOnCall = true
			emit(part)
			slot.sent = true
		})
	}

	if t.reasoningStarted {
		id := t.reasoningID
		if id == "" {
			id = generateId()
		}
		emit(ReasoningEndPart{ID: id, Details: t.detailsView()})
	}
	if t.textStarted {
		id := t.textID
		if id == "" {
			id = generateId()
		}
		emit(TextEndPart{ID: id})
	}

	metadata := OpenRouterMetadata{
		Usage:            t.openrouterUse,
		Provider:         t.provider,
		ReasoningDetails: t.detailsView(),
	}
	if len(t.fileAnnotation) > 0 {
		metadata.Annotations = t.fileAnnotation
	}

	// The two late fallbacks (`:4177-4184`).
	usage := t.usage
	if usage.InputTokens.Total == nil && t.promptTokensSeen != nil {
		usage.InputTokens.Total = t.promptTokensSeen
	}
	if usage.OutputTokens.Total == nil && t.completionTokensSeen != nil {
		usage.OutputTokens.Total = t.completionTokensSeen
	}
	usage.Raw = t.rawUsage

	emit(FinishPart{FinishReason: t.finishReason, Usage: usage, Metadata: metadata})
	return out
}

// FinishReasonSnapshot exposes the running finish reason. §2.2's exit condition
// reads it, and the step layer needs it before flush to decide whether a
// still-unsent tool call will ever arrive.
func (t *Translator) FinishReasonSnapshot() FinishReason { return t.finishReason }

// isParsableJSON is `isParsableJson` (`:2097-2103`) — `secureJsonParse` in a
// try/catch. `secureJsonParse` rejects `__proto__` / `constructor.prototype`
// keys, which is stricter than `JSON.parse`; encoding/json has no prototype to
// pollute, so the only observable difference is a payload that deliberately
// carries `__proto__`, and that would be rejected on both sides for different
// reasons only if it appears at an object key position — reproduced explicitly.
func isParsableJSON(input string) bool {
	if input == "" {
		return false
	}
	_, err := parseSecureJSONValue(input)
	return err == nil
}

// base64FromDataURLLoose is `getBase64FromDataUrl` — `/^data:[^;]*;base64,(.+)$/`
// with the input returned unchanged on no match.
func base64FromDataURLLoose(dataURL string) string {
	return base64FromDataURL(dataURL)
}
