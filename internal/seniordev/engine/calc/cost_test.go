//go:build !windows

package calc

import (
	"encoding/json"
	"math"
	"testing"
)

func costTokens(input, output, reasoning, cacheRead, cacheWrite float64) UsageTokens {
	return UsageTokens{
		Input:     input,
		Output:    output,
		Reasoning: reasoning,
		Cache:     UsageCache{Write: cacheWrite, Read: cacheRead},
	}
}

func TestCostPricesEveryTokenClass(t *testing.T) {
	cases := []struct {
		name  string
		toks  UsageTokens
		rates costRates
		want  float64
	}{
		{
			name:  "sonnet-shaped run",
			toks:  costTokens(3_590_000, 250_000, 100_000, 2_000_000, 500_000),
			rates: costRates{input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
			want:  18.495,
		},
		{
			name:  "reasoning is billed at the output rate",
			toks:  costTokens(0, 0, 1_000_000, 0, 0),
			rates: costRates{input: 1, output: 4},
			want:  4,
		},
		{
			name:  "no rates means free",
			toks:  costTokens(10, 10, 10, 10, 10),
			rates: costRates{},
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cost(tc.toks, tc.rates); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("cost = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetUsageSubtractsCacheTokensAndAppliesRates(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	result := GetUsage(GetUsageInput{
		Model: Model{Cost: &ModelCost{Input: 1, Output: 2, Cache: &CacheCost{Read: 0.1, Write: 1.25}}},
		Usage: LanguageModelUsage{
			InputTokens:        f(1_000_000),
			InputTokenDetails:  &InputTokenDetails{CacheReadTokens: f(400_000), CacheWriteTokens: f(100_000)},
			OutputTokens:       f(200_000),
			OutputTokenDetails: &OutputTokenDetails{ReasoningTokens: f(50_000)},
			TotalTokens:        f(1_200_000),
		},
	})
	if result.Tokens.Input != 500_000 || result.Tokens.Output != 150_000 || result.Tokens.Reasoning != 50_000 {
		t.Fatalf("tokens = %+v", result.Tokens)
	}
	if result.Tokens.Cache.Read != 400_000 || result.Tokens.Cache.Write != 100_000 {
		t.Fatalf("cache = %+v", result.Tokens.Cache)
	}
	// 0.5 + 0.3 + 0.1 + 0.04 + 0.125 = 1.065
	if math.Abs(result.Cost-1.065) > 1e-9 {
		t.Fatalf("cost = %v", result.Cost)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cost":1.065,"tokens":{"total":1200000,"input":500000,"output":150000,"reasoning":50000,"cache":{"write":100000,"read":400000}}}`
	if string(encoded) != want {
		t.Fatalf("json:\n got %s\nwant %s", encoded, want)
	}
}

func TestGetUsageReadsProviderMetadataCacheWrites(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	result := GetUsage(GetUsageInput{
		Model:    Model{Cost: &ModelCost{Input: 1, Cache: &CacheCost{Write: 2}}},
		Usage:    LanguageModelUsage{InputTokens: f(300)},
		Metadata: ProviderMetadata{"anthropic": {"cacheCreationInputTokens": float64(100)}},
	})
	if result.Tokens.Input != 200 || result.Tokens.Cache.Write != 100 {
		t.Fatalf("tokens = %+v", result.Tokens)
	}
	if got := safe(math.NaN()); got != 0 {
		t.Fatalf("safe(NaN) = %v", got)
	}
}
