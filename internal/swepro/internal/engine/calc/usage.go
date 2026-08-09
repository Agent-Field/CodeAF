package calc

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// objWriter builds a JSON object the way JSON.stringify does over a JS object
// literal: fields are emitted in declaration order and an `undefined` value
// DROPS THE KEY (a nil pointer / nil RawMessage here), where Go's encoding/json
// would have written null.
type objWriter struct {
	buf    bytes.Buffer
	fields int
	err    error
}

func (w *objWriter) comma() {
	if w.fields > 0 {
		w.buf.WriteByte(',')
	}
	w.fields++
}

// num writes `"key": <number>`, skipping the field when the pointer is nil.
func (w *objWriter) num(key string, value *float64) {
	if value == nil || w.err != nil {
		return
	}
	encoded, err := jscompat.JSNumber(*value).MarshalJSON()
	if err != nil {
		w.err = err
		return
	}
	w.comma()
	w.buf.WriteString(`"` + key + `":`)
	w.buf.Write(encoded)
}

// obj writes `"key": <marshalled>`, skipping the field when the value is nil.
func (w *objWriter) obj(key string, value json.Marshaler) {
	if value == nil || w.err != nil {
		return
	}
	encoded, err := value.MarshalJSON()
	if err != nil {
		w.err = err
		return
	}
	w.comma()
	w.buf.WriteString(`"` + key + `":`)
	w.buf.Write(encoded)
}

// raw writes `"key": <verbatim JSON>`, skipping the field when the value is nil.
// This is the opaque-provider-JSON path: the bytes are re-emitted unchanged.
func (w *objWriter) raw(key string, value json.RawMessage) {
	if value == nil || w.err != nil {
		return
	}
	w.comma()
	w.buf.WriteString(`"` + key + `":`)
	w.buf.Write(value)
}

func (w *objWriter) done() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	out.Write(w.buf.Bytes())
	out.WriteByte('}')
	return out.Bytes(), nil
}

// ── stage 1: the provider's computeTokenUsage ────────────────────────────
//
// `@openrouter/ai-sdk-provider/dist/index.mjs:2560-2581`. ENGINE-DESIGN §3.5
// pins this as the first hop of the usage pipeline.

// OpenRouterPromptTokensDetails is `usage.prompt_tokens_details`.
type OpenRouterPromptTokensDetails struct {
	CachedTokens     *float64 `json:"cached_tokens"`
	CacheWriteTokens *float64 `json:"cache_write_tokens"`
}

// OpenRouterCompletionTokensDetails is `usage.completion_tokens_details`.
type OpenRouterCompletionTokensDetails struct {
	ReasoningTokens *float64 `json:"reasoning_tokens"`
}

// OpenRouterUsage is the numeric projection of OpenRouter's `usage` object that
// computeTokenUsage reads. The canonical wire DTO (with `cost`, `is_byok` and
// the rest) belongs to internal/llm/wire; this is only what §3.5's arithmetic
// touches, plus the opaque original carried through as `raw`.
type OpenRouterUsage struct {
	PromptTokens            *float64                           `json:"prompt_tokens"`
	CompletionTokens        *float64                           `json:"completion_tokens"`
	PromptTokensDetails     *OpenRouterPromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *OpenRouterCompletionTokensDetails `json:"completion_tokens_details"`

	// Raw is the untouched wire object. computeTokenUsage carries the WHOLE
	// `usage` payload through as `raw` (index.mjs:2579) — including `cost` and
	// `is_byok`, which codeaf never reads — so it round-trips verbatim as
	// json.RawMessage rather than being re-serialised from the typed fields.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the numeric projection and keeps the original bytes.
func (u *OpenRouterUsage) UnmarshalJSON(data []byte) error {
	type shadow OpenRouterUsage
	var decoded shadow
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = OpenRouterUsage(decoded)
	u.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// LanguageModelV3InputTokens is the provider-side `inputTokens` block.
type LanguageModelV3InputTokens struct {
	Total      *float64
	NoCache    *float64
	CacheRead  *float64
	CacheWrite *float64
}

// LanguageModelV3OutputTokens is the provider-side `outputTokens` block.
type LanguageModelV3OutputTokens struct {
	Total     *float64
	Text      *float64
	Reasoning *float64
}

// LanguageModelV3Usage is computeTokenUsage's return shape.
type LanguageModelV3Usage struct {
	InputTokens  LanguageModelV3InputTokens
	OutputTokens LanguageModelV3OutputTokens
	Raw          json.RawMessage
}

// MarshalJSON writes the return literal's key order (index.mjs:2568-2580) and
// drops an undefined `raw`.
func (u LanguageModelV3Usage) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.obj("inputTokens", u.InputTokens)
	w.obj("outputTokens", u.OutputTokens)
	w.raw("raw", u.Raw)
	return w.done()
}

// MarshalJSON drops undefined members, notably `cacheWrite`.
func (t LanguageModelV3InputTokens) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.num("total", t.Total)
	w.num("noCache", t.NoCache)
	w.num("cacheRead", t.CacheRead)
	w.num("cacheWrite", t.CacheWrite)
	return w.done()
}

// MarshalJSON drops undefined members.
func (t LanguageModelV3OutputTokens) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.num("total", t.Total)
	w.num("text", t.Text)
	w.num("reasoning", t.Reasoning)
	return w.done()
}

// ComputeTokenUsage is `computeTokenUsage(usage)`.
//
// Note the asymmetry at index.mjs:2565: cacheRead and reasoning are `?? 0`,
// but cacheWrite is `?? undefined` — an absent `cache_write_tokens` stays
// undefined all the way into getUsage's provider-metadata `??` chain, which is
// the only reason those four dead fallbacks can ever be reached.
func ComputeTokenUsage(usage *OpenRouterUsage) LanguageModelV3Usage {
	promptTokens := float64(0)
	completionTokens := float64(0)
	cacheReadTokens := float64(0)
	var cacheWriteTokens *float64
	reasoningTokens := float64(0)

	if usage != nil {
		if usage.PromptTokens != nil {
			promptTokens = *usage.PromptTokens
		}
		if usage.CompletionTokens != nil {
			completionTokens = *usage.CompletionTokens
		}
		if usage.PromptTokensDetails != nil {
			if usage.PromptTokensDetails.CachedTokens != nil {
				cacheReadTokens = *usage.PromptTokensDetails.CachedTokens
			}
			cacheWriteTokens = usage.PromptTokensDetails.CacheWriteTokens
		}
		if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
			reasoningTokens = *usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	noCache := promptTokens - cacheReadTokens
	text := completionTokens - reasoningTokens
	return LanguageModelV3Usage{
		InputTokens: LanguageModelV3InputTokens{
			Total:      &promptTokens,
			NoCache:    &noCache,
			CacheRead:  &cacheReadTokens,
			CacheWrite: cacheWriteTokens,
		},
		OutputTokens: LanguageModelV3OutputTokens{
			Total:     &completionTokens,
			Text:      &text,
			Reasoning: &reasoningTokens,
		},
		Raw: rawOf(usage),
	}
}

func rawOf(usage *OpenRouterUsage) json.RawMessage {
	if usage == nil {
		return nil
	}
	return usage.Raw
}

// ── stage 2: the SDK's asLanguageModelUsage ──────────────────────────────
//
// `ai/dist/index.mjs:2423-2444`.

// InputTokenDetails is `LanguageModelUsage["inputTokenDetails"]`.
type InputTokenDetails struct {
	NoCacheTokens    *float64
	CacheReadTokens  *float64
	CacheWriteTokens *float64
}

// OutputTokenDetails is `LanguageModelUsage["outputTokenDetails"]`.
type OutputTokenDetails struct {
	TextTokens      *float64
	ReasoningTokens *float64
}

// LanguageModelUsage is `ai@6`'s flattened usage type (index.d.ts:267-325).
// The details blocks are pointers because getUsage reaches them through `?.`
// (session.ts:359, :362, :364) even though the type declares them present.
type LanguageModelUsage struct {
	InputTokens        *float64
	InputTokenDetails  *InputTokenDetails
	OutputTokens       *float64
	OutputTokenDetails *OutputTokenDetails
	TotalTokens        *float64
	Raw                json.RawMessage
	// Deprecated flat aliases, still read by getUsage as `??` fallbacks.
	ReasoningTokens   *float64
	CachedInputTokens *float64
}

// MarshalJSON writes asLanguageModelUsage's literal order — note `raw` sits
// between `totalTokens` and the two deprecated aliases (index.mjs:2437-2443).
func (u LanguageModelUsage) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.num("inputTokens", u.InputTokens)
	if u.InputTokenDetails != nil {
		w.obj("inputTokenDetails", *u.InputTokenDetails)
	}
	w.num("outputTokens", u.OutputTokens)
	if u.OutputTokenDetails != nil {
		w.obj("outputTokenDetails", *u.OutputTokenDetails)
	}
	w.num("totalTokens", u.TotalTokens)
	w.raw("raw", u.Raw)
	w.num("reasoningTokens", u.ReasoningTokens)
	w.num("cachedInputTokens", u.CachedInputTokens)
	return w.done()
}

// MarshalJSON drops undefined members.
func (d InputTokenDetails) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.num("noCacheTokens", d.NoCacheTokens)
	w.num("cacheReadTokens", d.CacheReadTokens)
	w.num("cacheWriteTokens", d.CacheWriteTokens)
	return w.done()
}

// MarshalJSON drops undefined members.
func (d OutputTokenDetails) MarshalJSON() ([]byte, error) {
	var w objWriter
	w.num("textTokens", d.TextTokens)
	w.num("reasoningTokens", d.ReasoningTokens)
	return w.done()
}

// AsLanguageModelUsage is `asLanguageModelUsage(usage)`.
//
// **totalTokens is RECOMPUTED as input + output.** OpenRouter's own
// `total_tokens` never reaches this type — it survives only inside `raw` and
// `providerMetadata.openrouter.usage.totalTokens`, both of which codeaf
// ignores. ENGINE-DESIGN §3.5: "Reproduce the recomputation; do not 'fix' it by
// using the provider's total."
func AsLanguageModelUsage(usage LanguageModelV3Usage) LanguageModelUsage {
	return LanguageModelUsage{
		InputTokens: usage.InputTokens.Total,
		InputTokenDetails: &InputTokenDetails{
			NoCacheTokens:    usage.InputTokens.NoCache,
			CacheReadTokens:  usage.InputTokens.CacheRead,
			CacheWriteTokens: usage.InputTokens.CacheWrite,
		},
		OutputTokens: usage.OutputTokens.Total,
		OutputTokenDetails: &OutputTokenDetails{
			TextTokens:      usage.OutputTokens.Text,
			ReasoningTokens: usage.OutputTokens.Reasoning,
		},
		TotalTokens:       addTokenCounts(usage.InputTokens.Total, usage.OutputTokens.Total),
		Raw:               usage.Raw,
		ReasoningTokens:   usage.OutputTokens.Reasoning,
		CachedInputTokens: usage.InputTokens.CacheRead,
	}
}

// addTokenCounts is `ai/dist/index.mjs:2503-2505`: undefined only when BOTH
// operands are absent, otherwise the absent side counts as 0.
func addTokenCounts(a, b *float64) *float64 {
	if a == nil && b == nil {
		return nil
	}
	sum := float64(0)
	if a != nil {
		sum += *a
	}
	if b != nil {
		sum += *b
	}
	return &sum
}

// ── stage 3: Session.getUsage ────────────────────────────────────────────
//
// `src/session/session.ts:353-414`.

// ProviderMetadata is `ProviderMetadata` = `Record<string, Record<string, JSONValue>>`.
// Only lookups happen, never iteration, so a plain Go map is enough — no key
// order reaches output.
type ProviderMetadata map[string]map[string]any

// UsageCache mirrors the cache block of `getUsage`'s object literal
// (session.ts:389-392): WRITE first, then read. The persisted schema declares
// the opposite order (message-v2.ts:276-279) but JSON.stringify follows the
// runtime object, so this is what a step-finish part serialises as.
type UsageCache struct {
	Write float64
	Read  float64
}

// UsageTokens mirrors `getUsage`'s `tokens` object literal (session.ts:384-393).
type UsageTokens struct {
	Total     *float64
	Input     float64
	Output    float64
	Reasoning float64
	Cache     UsageCache
}

// MarshalJSON writes the literal's key order and drops an absent `total` the
// way JSON.stringify drops an `undefined` property. Note `total` is the one
// field getUsage does NOT pass through safe() (session.ts:385), so a
// non-finite value survives here and marshals as null.
func (t UsageTokens) MarshalJSON() ([]byte, error) {
	return marshalTokens(t.Total, t.Input, t.Output, t.Reasoning, t.Cache.Read, t.Cache.Write, true)
}

// UsageResult is `getUsage`'s return literal (session.ts:400-414): cost, then
// tokens.
type UsageResult struct {
	Cost   float64     `json:"-"`
	Tokens UsageTokens `json:"-"`
}

// MarshalJSON keeps the return literal's key order and V8 number formatting.
func (r UsageResult) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"cost":`)
	cost, err := jscompat.JSNumber(r.Cost).MarshalJSON()
	if err != nil {
		return nil, err
	}
	buf.Write(cost)
	buf.WriteString(`,"tokens":`)
	tokens, err := r.Tokens.MarshalJSON()
	if err != nil {
		return nil, err
	}
	buf.Write(tokens)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalTokens is the shared writer for the two token shapes. cacheWriteFirst
// selects between getUsage's runtime literal order and the declared schema
// order; see the fidelity note in the package comment.
func marshalTokens(total *float64, input, output, reasoning, cacheRead, cacheWrite float64, cacheWriteFirst bool) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	if total != nil {
		buf.WriteString(`"total":`)
		encoded, err := jscompat.JSNumber(*total).MarshalJSON()
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		buf.WriteByte(',')
	}
	write := func(key string, value float64, comma bool) error {
		buf.WriteString(`"` + key + `":`)
		encoded, err := jscompat.JSNumber(value).MarshalJSON()
		if err != nil {
			return err
		}
		buf.Write(encoded)
		if comma {
			buf.WriteByte(',')
		}
		return nil
	}
	if err := write("input", input, true); err != nil {
		return nil, err
	}
	if err := write("output", output, true); err != nil {
		return nil, err
	}
	if err := write("reasoning", reasoning, true); err != nil {
		return nil, err
	}
	buf.WriteString(`"cache":{`)
	if cacheWriteFirst {
		if err := write("write", cacheWrite, true); err != nil {
			return nil, err
		}
		if err := write("read", cacheRead, false); err != nil {
			return nil, err
		}
	} else {
		if err := write("read", cacheRead, true); err != nil {
			return nil, err
		}
		if err := write("write", cacheWrite, false); err != nil {
			return nil, err
		}
	}
	buf.WriteString("}}")
	return buf.Bytes(), nil
}

// GetUsageInput is `getUsage`'s parameter object (session.ts:353).
type GetUsageInput struct {
	Model    Model
	Usage    LanguageModelUsage
	Metadata ProviderMetadata
}

// GetUsage is `Session.getUsage` (session.ts:353-414).
func GetUsage(input GetUsageInput) UsageResult {
	usage := input.Usage

	inputTokens := safe(orZero(usage.InputTokens))
	outputTokens := safe(orZero(usage.OutputTokens))

	// outputTokenDetails?.reasoningTokens ?? reasoningTokens ?? 0
	var reasoningTokens float64
	if usage.OutputTokenDetails != nil && usage.OutputTokenDetails.ReasoningTokens != nil {
		reasoningTokens = *usage.OutputTokenDetails.ReasoningTokens
	} else if usage.ReasoningTokens != nil {
		reasoningTokens = *usage.ReasoningTokens
	}
	reasoningTokens = safe(reasoningTokens)

	// inputTokenDetails?.cacheReadTokens ?? cachedInputTokens ?? 0
	var cacheReadInputTokens float64
	if usage.InputTokenDetails != nil && usage.InputTokenDetails.CacheReadTokens != nil {
		cacheReadInputTokens = *usage.InputTokenDetails.CacheReadTokens
	} else if usage.CachedInputTokens != nil {
		cacheReadInputTokens = *usage.CachedInputTokens
	}
	cacheReadInputTokens = safe(cacheReadInputTokens)

	cacheWriteInputTokens := safe(jsNumberOfValue(cacheWriteCandidate(usage, input.Metadata)))

	// AI SDK v6 normalized inputTokens to include cached tokens across all
	// providers, so cache tokens are ALWAYS subtracted (session.ts:379-382).
	adjustedInputTokens := safe(inputTokens - cacheReadInputTokens - cacheWriteInputTokens)

	// NOT wrapped in safe() — the one field that is not (session.ts:385).
	total := usage.TotalTokens

	tokens := UsageTokens{
		Total:     total,
		Input:     adjustedInputTokens,
		Output:    safe(outputTokens - reasoningTokens),
		Reasoning: reasoningTokens,
		Cache: UsageCache{
			Write: cacheWriteInputTokens,
			Read:  cacheReadInputTokens,
		},
	}

	// costInfo = model.cost?.experimentalOver200K && input + cache.read > 200_000
	//   ? model.cost.experimentalOver200K
	//   : model.cost
	rates := baseRates(input.Model.Cost)
	if input.Model.Cost != nil && input.Model.Cost.ExperimentalOver200K != nil &&
		tokens.Input+tokens.Cache.Read > 200_000 {
		rates = over200KRates(input.Model.Cost.ExperimentalOver200K)
	}

	return UsageResult{
		Cost:   safe(costDecimal(tokens, rates).toNumber()),
		Tokens: tokens,
	}
}

// cacheWriteCandidate is the five-deep `??` chain at session.ts:363-374, before
// the `Number(...)` wrapper. A nil result stands in for the literal 0 the chain
// ends on (Number(0) === 0, so the two are indistinguishable downstream — but
// jsNumberOfValue(nil) is Number(null) === 0 as well, so it is written out).
//
// The four provider-metadata arms are dead on the OpenRouter path; kept per
// ENGINE-DESIGN §3.5. `metadata?.["bedrock"]?.["usage"]?.["cacheWriteInputTokens"]`
// is a three-level optional chain — only null/undefined short-circuits, so a
// non-object `usage` value yields undefined rather than throwing.
func cacheWriteCandidate(usage LanguageModelUsage, metadata ProviderMetadata) any {
	if usage.InputTokenDetails != nil && usage.InputTokenDetails.CacheWriteTokens != nil {
		return *usage.InputTokenDetails.CacheWriteTokens
	}
	if value, ok := metadataGet(metadata, "anthropic", "cacheCreationInputTokens"); ok {
		return value
	}
	// google-vertex-anthropic returns metadata under the "vertex" key.
	if value, ok := metadataGet(metadata, "vertex", "cacheCreationInputTokens"); ok {
		return value
	}
	if value, ok := metadataGetNested(metadata, "bedrock", "usage", "cacheWriteInputTokens"); ok {
		return value
	}
	if value, ok := metadataGetNested(metadata, "venice", "usage", "cacheCreationInputTokens"); ok {
		return value
	}
	return float64(0)
}

// metadataGet is `metadata?.[provider]?.[key]`; ok=false means the `??` chain
// continues (JS null and undefined are both "absent" for `??`).
func metadataGet(metadata ProviderMetadata, provider, key string) (any, bool) {
	inner, ok := metadata[provider]
	if !ok || inner == nil {
		return nil, false
	}
	value, ok := inner[key]
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

// metadataGetNested is `metadata?.[provider]?.[outer]?.[key]`. A non-object
// `outer` value yields undefined (property access on a primitive), matching JS.
func metadataGetNested(metadata ProviderMetadata, provider, outer, key string) (any, bool) {
	middle, ok := metadataGet(metadata, provider, outer)
	if !ok {
		return nil, false
	}
	object, ok := middle.(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := object[key]
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func orZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// costRates is the resolved `costInfo` — the four `?? 0`-defaulted rates the
// decimal chain multiplies by, in $/Mtok.
type costRates struct {
	input      float64
	output     float64
	cacheRead  float64
	cacheWrite float64
}

func baseRates(cost *ModelCost) costRates {
	if cost == nil {
		return costRates{}
	}
	rates := costRates{input: cost.Input, output: cost.Output}
	if cost.Cache != nil {
		rates.cacheRead = cost.Cache.Read
		rates.cacheWrite = cost.Cache.Write
	}
	return rates
}

func over200KRates(cost *Over200KCost) costRates {
	rates := costRates{input: cost.Input, output: cost.Output}
	if cost.Cache != nil {
		rates.cacheRead = cost.Cache.Read
		rates.cacheWrite = cost.Cache.Write
	}
	return rates
}

// costDecimal is session.ts:401-411 — the decimal.js chain, term by term and
// in the same order, because every intermediate finalises to 20 significant
// digits and reordering would change the result.
//
//	new Decimal(0)
//	  .add(new Decimal(tokens.input).mul(costInfo?.input ?? 0).div(1_000_000))
//	  .add(new Decimal(tokens.output).mul(costInfo?.output ?? 0).div(1_000_000))
//	  .add(new Decimal(tokens.cache.read).mul(costInfo?.cache?.read ?? 0).div(1_000_000))
//	  .add(new Decimal(tokens.cache.write).mul(costInfo?.cache?.write ?? 0).div(1_000_000))
//	  .add(new Decimal(tokens.reasoning).mul(costInfo?.output ?? 0).div(1_000_000))
//
// The last term is the TODO at session.ts:407-409: reasoning tokens are billed
// at the OUTPUT rate because models.dev has no reasoning price. Kept.
func costDecimal(tokens UsageTokens, rates costRates) dec {
	acc := decZero()
	acc = acc.add(decFromFloat(tokens.Input).mul(decFromFloat(rates.input)).divPow10(6))
	acc = acc.add(decFromFloat(tokens.Output).mul(decFromFloat(rates.output)).divPow10(6))
	acc = acc.add(decFromFloat(tokens.Cache.Read).mul(decFromFloat(rates.cacheRead)).divPow10(6))
	acc = acc.add(decFromFloat(tokens.Cache.Write).mul(decFromFloat(rates.cacheWrite)).divPow10(6))
	acc = acc.add(decFromFloat(tokens.Reasoning).mul(decFromFloat(rates.output)).divPow10(6))
	return acc
}
