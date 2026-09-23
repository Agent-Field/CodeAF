//go:build !windows

package calc

import (
	"encoding/json"
	"strconv"
)

// ── stage 1: the provider's usage block ──────────────────────────────────

// OpenRouterPromptTokensDetails is `usage.prompt_tokens_details`.
type OpenRouterPromptTokensDetails struct {
	CachedTokens     *float64 `json:"cached_tokens"`
	CacheWriteTokens *float64 `json:"cache_write_tokens"`
}

// OpenRouterCompletionTokensDetails is `usage.completion_tokens_details`.
type OpenRouterCompletionTokensDetails struct {
	ReasoningTokens *float64 `json:"reasoning_tokens"`
}

// OpenRouterUsage is the numeric projection of OpenRouter's `usage` object
// that the token arithmetic reads, plus the untouched original carried
// through as Raw.
type OpenRouterUsage struct {
	PromptTokens            *float64                           `json:"prompt_tokens"`
	CompletionTokens        *float64                           `json:"completion_tokens"`
	PromptTokensDetails     *OpenRouterPromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *OpenRouterCompletionTokensDetails `json:"completion_tokens_details"`

	// Raw is the untouched wire object, including fields such as `cost` and
	// `is_byok` that senior-dev never reads.
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
	Total      *float64 `json:"total,omitempty"`
	NoCache    *float64 `json:"noCache,omitempty"`
	CacheRead  *float64 `json:"cacheRead,omitempty"`
	CacheWrite *float64 `json:"cacheWrite,omitempty"`
}

// LanguageModelV3OutputTokens is the provider-side `outputTokens` block.
type LanguageModelV3OutputTokens struct {
	Total     *float64 `json:"total,omitempty"`
	Text      *float64 `json:"text,omitempty"`
	Reasoning *float64 `json:"reasoning,omitempty"`
}

// LanguageModelV3Usage is ComputeTokenUsage's return shape.
type LanguageModelV3Usage struct {
	InputTokens  LanguageModelV3InputTokens  `json:"inputTokens"`
	OutputTokens LanguageModelV3OutputTokens `json:"outputTokens"`
	Raw          json.RawMessage             `json:"raw,omitempty"`
}

// ComputeTokenUsage splits the provider's usage block into input and output
// token groups. An absent cache-write count stays absent (nil) so that the
// provider-metadata fallbacks in GetUsage can still supply it.
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

// ── stage 2: the flattened usage ─────────────────────────────────────────

// InputTokenDetails is the flattened input breakdown.
type InputTokenDetails struct {
	NoCacheTokens    *float64 `json:"noCacheTokens,omitempty"`
	CacheReadTokens  *float64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *float64 `json:"cacheWriteTokens,omitempty"`
}

// OutputTokenDetails is the flattened output breakdown.
type OutputTokenDetails struct {
	TextTokens      *float64 `json:"textTokens,omitempty"`
	ReasoningTokens *float64 `json:"reasoningTokens,omitempty"`
}

// LanguageModelUsage is the flattened usage GetUsage consumes. The details
// blocks are optional; the two trailing fields are flat aliases GetUsage
// falls back to when the details are absent.
type LanguageModelUsage struct {
	InputTokens        *float64            `json:"inputTokens,omitempty"`
	InputTokenDetails  *InputTokenDetails  `json:"inputTokenDetails,omitempty"`
	OutputTokens       *float64            `json:"outputTokens,omitempty"`
	OutputTokenDetails *OutputTokenDetails `json:"outputTokenDetails,omitempty"`
	TotalTokens        *float64            `json:"totalTokens,omitempty"`
	Raw                json.RawMessage     `json:"raw,omitempty"`
	ReasoningTokens    *float64            `json:"reasoningTokens,omitempty"`
	CachedInputTokens  *float64            `json:"cachedInputTokens,omitempty"`
}

// AsLanguageModelUsage flattens the token groups. totalTokens is recomputed
// as input + output; the provider's own total survives only inside Raw.
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

// addTokenCounts is nil only when BOTH operands are absent; otherwise the
// absent side counts as 0.
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

// ── stage 3: usage and cost ──────────────────────────────────────────────

// ProviderMetadata is the per-provider metadata map a response may carry.
type ProviderMetadata map[string]map[string]any

// UsageCache is the cache block of a usage result.
type UsageCache struct {
	Write float64 `json:"write"`
	Read  float64 `json:"read"`
}

// UsageTokens is the token block of a usage result. Total is optional.
type UsageTokens struct {
	Total     *float64   `json:"total,omitempty"`
	Input     float64    `json:"input"`
	Output    float64    `json:"output"`
	Reasoning float64    `json:"reasoning"`
	Cache     UsageCache `json:"cache"`
}

// UsageResult is GetUsage's result: the call's cost in USD and its tokens.
type UsageResult struct {
	Cost   float64     `json:"cost"`
	Tokens UsageTokens `json:"tokens"`
}

// GetUsageInput is GetUsage's parameter object.
type GetUsageInput struct {
	Model    Model
	Usage    LanguageModelUsage
	Metadata ProviderMetadata
}

// GetUsage derives the billed token counts and the cost of one model call.
// Cached input tokens are subtracted from the input count, since providers
// report inputTokens inclusive of cache reads and writes.
func GetUsage(input GetUsageInput) UsageResult {
	usage := input.Usage

	inputTokens := safe(orZero(usage.InputTokens))
	outputTokens := safe(orZero(usage.OutputTokens))

	var reasoningTokens float64
	if usage.OutputTokenDetails != nil && usage.OutputTokenDetails.ReasoningTokens != nil {
		reasoningTokens = *usage.OutputTokenDetails.ReasoningTokens
	} else if usage.ReasoningTokens != nil {
		reasoningTokens = *usage.ReasoningTokens
	}
	reasoningTokens = safe(reasoningTokens)

	var cacheReadInputTokens float64
	if usage.InputTokenDetails != nil && usage.InputTokenDetails.CacheReadTokens != nil {
		cacheReadInputTokens = *usage.InputTokenDetails.CacheReadTokens
	} else if usage.CachedInputTokens != nil {
		cacheReadInputTokens = *usage.CachedInputTokens
	}
	cacheReadInputTokens = safe(cacheReadInputTokens)

	cacheWriteInputTokens := safe(numberOf(cacheWriteCandidate(usage, input.Metadata)))

	adjustedInputTokens := safe(inputTokens - cacheReadInputTokens - cacheWriteInputTokens)

	tokens := UsageTokens{
		Total:     usage.TotalTokens,
		Input:     adjustedInputTokens,
		Output:    safe(outputTokens - reasoningTokens),
		Reasoning: reasoningTokens,
		Cache: UsageCache{
			Write: cacheWriteInputTokens,
			Read:  cacheReadInputTokens,
		},
	}

	rates := baseRates(input.Model.Cost)
	if input.Model.Cost != nil && input.Model.Cost.ExperimentalOver200K != nil &&
		tokens.Input+tokens.Cache.Read > 200_000 {
		rates = over200KRates(input.Model.Cost.ExperimentalOver200K)
	}

	return UsageResult{
		Cost:   safe(cost(tokens, rates)),
		Tokens: tokens,
	}
}

// cacheWriteCandidate finds the cache-write token count: the flattened
// details first, then the provider-specific metadata keys some providers use
// instead. A nil result means none was reported.
func cacheWriteCandidate(usage LanguageModelUsage, metadata ProviderMetadata) any {
	if usage.InputTokenDetails != nil && usage.InputTokenDetails.CacheWriteTokens != nil {
		return *usage.InputTokenDetails.CacheWriteTokens
	}
	if value, ok := metadataGet(metadata, "anthropic", "cacheCreationInputTokens"); ok {
		return value
	}
	if value, ok := metadataGet(metadata, "vertex", "cacheCreationInputTokens"); ok {
		return value
	}
	if value, ok := metadataGetNested(metadata, "bedrock", "usage", "cacheWriteInputTokens"); ok {
		return value
	}
	if value, ok := metadataGetNested(metadata, "venice", "usage", "cacheCreationInputTokens"); ok {
		return value
	}
	return nil
}

// metadataGet is metadata[provider][key]; ok=false when either level is
// absent or null.
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

// metadataGetNested is metadata[provider][outer][key].
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

// numberOf reads a token count out of a decoded JSON value. Anything that is
// not a number (or a numeric string) counts as 0.
func numberOf(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		f, _ := typed.Float64()
		return f
	case string:
		f, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return f
	}
	return 0
}

func orZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// costRates is the resolved price table in $/Mtok; an absent rate is 0.
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

// cost prices the token block in USD. Reasoning tokens are billed at the
// output rate because catalogs carry no separate reasoning price.
func cost(tokens UsageTokens, rates costRates) float64 {
	return (tokens.Input*rates.input +
		tokens.Output*rates.output +
		tokens.Cache.Read*rates.cacheRead +
		tokens.Cache.Write*rates.cacheWrite +
		tokens.Reasoning*rates.output) / 1_000_000
}
