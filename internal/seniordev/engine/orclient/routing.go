//go:build !windows

package orclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ProviderRouting is OpenRouter's request-level `provider` object: the
// preferences that decide which upstream endpoint serves a model. Field names
// and enums follow https://openrouter.ai/docs/features/provider-routing
// exactly so a config author can paste from the OpenRouter docs.
//
// Every field is optional. A nil pointer or empty slice means "not set" and
// is omitted from the wire, so an all-empty value sends no `provider` key at
// all — routing is off unless something is configured. Parsing is strict: an
// unknown key or an out-of-range enum is an error, never a silent no-op, so a
// misspelled rule cannot look configured while the call routes on defaults.
type ProviderRouting struct {
	// Order lists provider slugs to try in sequence.
	Order []string `json:"order,omitempty"`
	// AllowFallbacks lets OpenRouter fall back to other providers when the
	// preferred ones are unavailable. OpenRouter's default is true.
	AllowFallbacks *bool `json:"allow_fallbacks,omitempty"`
	// RequireParameters excludes providers that do not support every
	// parameter in the request (tools, temperature, top_k, ...).
	RequireParameters *bool `json:"require_parameters,omitempty"`
	// DataCollection is "allow" or "deny" for providers that may train on
	// inputs.
	DataCollection string `json:"data_collection,omitempty"`
	// ZDR restricts routing to zero-data-retention endpoints.
	ZDR *bool `json:"zdr,omitempty"`
	// EnforceDistillableText restricts routing to endpoints whose model
	// author permits distillation.
	EnforceDistillableText *bool `json:"enforce_distillable_text,omitempty"`
	// Only is an allowlist of provider slugs; Ignore is a blocklist.
	Only   []string `json:"only,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
	// Quantizations filters endpoints by weight precision.
	Quantizations []string `json:"quantizations,omitempty"`
	// Sort orders the candidate endpoints by price, throughput or latency.
	// Setting it disables OpenRouter's default load balancing.
	Sort *RoutingSort `json:"sort,omitempty"`
	// MaxPrice is a hard cap in $/million tokens (or $/request); endpoints
	// above it are excluded.
	MaxPrice *RoutingMaxPrice `json:"max_price,omitempty"`
	// PreferredMinThroughput (tokens/s) and PreferredMaxLatency (seconds)
	// are soft preferences: endpoints outside them are deprioritised, not
	// excluded.
	PreferredMinThroughput *RoutingThreshold `json:"preferred_min_throughput,omitempty"`
	PreferredMaxLatency    *RoutingThreshold `json:"preferred_max_latency,omitempty"`
}

// RoutingSort is the `sort` field, which OpenRouter accepts either as a bare
// strategy string or as `{"by": ..., "partition": ...}`. It marshals back to
// whichever form the config used so the wire matches the docs example.
type RoutingSort struct {
	By        string `json:"by"`
	Partition string `json:"partition,omitempty"`
}

// RoutingMaxPrice is the `max_price` object.
type RoutingMaxPrice struct {
	Prompt     *float64 `json:"prompt,omitempty"`
	Completion *float64 `json:"completion,omitempty"`
	Image      *float64 `json:"image,omitempty"`
	Audio      *float64 `json:"audio,omitempty"`
	Request    *float64 `json:"request,omitempty"`
}

// RoutingThreshold is a performance preference, accepted either as a single
// number or as per-percentile values.
type RoutingThreshold struct {
	Value *float64 `json:"-"`
	P50   *float64 `json:"p50,omitempty"`
	P75   *float64 `json:"p75,omitempty"`
	P90   *float64 `json:"p90,omitempty"`
	P99   *float64 `json:"p99,omitempty"`
}

var (
	routingSortStrategies = []string{"price", "throughput", "latency"}
	routingSortPartitions = []string{"model", "none"}
	routingDataCollection = []string{"allow", "deny"}
	routingQuantizations  = []string{
		"int4", "int8", "fp4", "mxfp4", "nvfp4", "fp6", "fp8", "mxfp8",
		"fp16", "bf16", "fp32", "unknown",
	}
)

// ParseProviderRouting decodes a config value strictly: unknown keys, wrong
// shapes and out-of-range enums are errors. Empty input and `null` mean
// "nothing configured" and return nil.
func ParseProviderRouting(raw []byte) (*ProviderRouting, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var routing ProviderRouting
	if err := decodeStrict(trimmed, &routing); err != nil {
		return nil, fmt.Errorf("provider routing: %w", err)
	}
	if err := routing.Validate(); err != nil {
		return nil, fmt.Errorf("provider routing: %w", err)
	}
	return &routing, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("trailing data after value")
	}
	return nil
}

// Validate checks enums and value ranges without touching the wire shape.
func (r *ProviderRouting) Validate() error {
	if r == nil {
		return nil
	}
	if r.DataCollection != "" && !slices.Contains(routingDataCollection, r.DataCollection) {
		return fmt.Errorf("data_collection must be one of %s, got %q",
			strings.Join(routingDataCollection, "|"), r.DataCollection)
	}
	for _, q := range r.Quantizations {
		if !slices.Contains(routingQuantizations, q) {
			return fmt.Errorf("quantizations: unknown level %q (want one of %s)",
				q, strings.Join(routingQuantizations, "|"))
		}
	}
	for name, values := range map[string][]string{"order": r.Order, "only": r.Only, "ignore": r.Ignore} {
		for _, slug := range values {
			if strings.TrimSpace(slug) == "" {
				return fmt.Errorf("%s: provider slugs must be non-empty strings", name)
			}
		}
	}
	if r.Sort != nil {
		if !slices.Contains(routingSortStrategies, r.Sort.By) {
			return fmt.Errorf("sort must be one of %s, got %q",
				strings.Join(routingSortStrategies, "|"), r.Sort.By)
		}
		if r.Sort.Partition != "" && !slices.Contains(routingSortPartitions, r.Sort.Partition) {
			return fmt.Errorf("sort.partition must be one of %s, got %q",
				strings.Join(routingSortPartitions, "|"), r.Sort.Partition)
		}
	}
	if r.MaxPrice != nil {
		for name, value := range map[string]*float64{
			"prompt": r.MaxPrice.Prompt, "completion": r.MaxPrice.Completion,
			"image": r.MaxPrice.Image, "audio": r.MaxPrice.Audio, "request": r.MaxPrice.Request,
		} {
			if value != nil && *value < 0 {
				return fmt.Errorf("max_price.%s must not be negative", name)
			}
		}
	}
	for name, threshold := range map[string]*RoutingThreshold{
		"preferred_min_throughput": r.PreferredMinThroughput,
		"preferred_max_latency":    r.PreferredMaxLatency,
	} {
		if err := threshold.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (t *RoutingThreshold) validate(name string) error {
	if t == nil {
		return nil
	}
	if t.Value == nil && t.P50 == nil && t.P75 == nil && t.P90 == nil && t.P99 == nil {
		return fmt.Errorf("%s must be a number or an object with at least one of p50/p75/p90/p99", name)
	}
	for _, value := range []*float64{t.Value, t.P50, t.P75, t.P90, t.P99} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s must not be negative", name)
		}
	}
	return nil
}

// IsZero reports whether nothing is configured, in which case no `provider`
// key is sent.
func (r *ProviderRouting) IsZero() bool {
	return r == nil || (len(r.Order) == 0 && r.AllowFallbacks == nil && r.RequireParameters == nil &&
		r.DataCollection == "" && r.ZDR == nil && r.EnforceDistillableText == nil &&
		len(r.Only) == 0 && len(r.Ignore) == 0 && len(r.Quantizations) == 0 &&
		r.Sort == nil && r.MaxPrice == nil &&
		r.PreferredMinThroughput == nil && r.PreferredMaxLatency == nil)
}

// Merge returns a copy of r with every field that override sets replacing
// r's value. Lists replace wholesale rather than concatenating, so a
// narrower level (model, then agent) can drop a provider the broader level
// allowed. Nested objects (sort, max_price, the thresholds) also replace
// wholesale: they are single settings, not bags.
func (r *ProviderRouting) Merge(override *ProviderRouting) *ProviderRouting {
	if r == nil && override == nil {
		return nil
	}
	out := ProviderRouting{}
	if r != nil {
		out = *r
	}
	if override == nil {
		return &out
	}
	if len(override.Order) > 0 {
		out.Order = slices.Clone(override.Order)
	}
	if override.AllowFallbacks != nil {
		out.AllowFallbacks = override.AllowFallbacks
	}
	if override.RequireParameters != nil {
		out.RequireParameters = override.RequireParameters
	}
	if override.DataCollection != "" {
		out.DataCollection = override.DataCollection
	}
	if override.ZDR != nil {
		out.ZDR = override.ZDR
	}
	if override.EnforceDistillableText != nil {
		out.EnforceDistillableText = override.EnforceDistillableText
	}
	if len(override.Only) > 0 {
		out.Only = slices.Clone(override.Only)
	}
	if len(override.Ignore) > 0 {
		out.Ignore = slices.Clone(override.Ignore)
	}
	if len(override.Quantizations) > 0 {
		out.Quantizations = slices.Clone(override.Quantizations)
	}
	if override.Sort != nil {
		out.Sort = override.Sort
	}
	if override.MaxPrice != nil {
		out.MaxPrice = override.MaxPrice
	}
	if override.PreferredMinThroughput != nil {
		out.PreferredMinThroughput = override.PreferredMinThroughput
	}
	if override.PreferredMaxLatency != nil {
		out.PreferredMaxLatency = override.PreferredMaxLatency
	}
	return &out
}

// Object renders the routing as the ordered `provider` value for the request
// body, or nil when nothing is configured. Key order is the struct's field
// order, which mirrors the OpenRouter docs.
func (r *ProviderRouting) Object() *Object {
	if r.IsZero() {
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	object, err := ParseObject(raw)
	if err != nil {
		return nil
	}
	return object
}

// MarshalJSON emits the bare string form when no partition was given.
func (s RoutingSort) MarshalJSON() ([]byte, error) {
	if s.Partition == "" {
		return json.Marshal(s.By)
	}
	type plain RoutingSort
	return json.Marshal(plain(s))
}

// UnmarshalJSON accepts `"throughput"` or `{"by": "throughput", "partition": "none"}`.
func (s *RoutingSort) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		s.Partition = ""
		return json.Unmarshal(trimmed, &s.By)
	}
	type plain RoutingSort
	var parsed plain
	if err := decodeStrict(trimmed, &parsed); err != nil {
		return fmt.Errorf("sort: %w", err)
	}
	*s = RoutingSort(parsed)
	return nil
}

// MarshalJSON emits the bare number when the config gave one.
func (t RoutingThreshold) MarshalJSON() ([]byte, error) {
	if t.Value != nil {
		return json.Marshal(*t.Value)
	}
	type plain RoutingThreshold
	return json.Marshal(plain(t))
}

// UnmarshalJSON accepts `50` or `{"p50": 100, "p90": 50}`.
func (t *RoutingThreshold) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] != '{' {
		var value float64
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return fmt.Errorf("threshold must be a number or a percentile object: %w", err)
		}
		*t = RoutingThreshold{Value: &value}
		return nil
	}
	type plain RoutingThreshold
	var parsed plain
	if err := decodeStrict(trimmed, &parsed); err != nil {
		return fmt.Errorf("threshold: %w", err)
	}
	*t = RoutingThreshold(parsed)
	return nil
}
