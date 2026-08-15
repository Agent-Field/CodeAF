package calc

import (
	"math"
	"testing"
)

func decTokens(input, output, reasoning, cacheRead, cacheWrite float64) UsageTokens {
	return UsageTokens{
		Input:     input,
		Output:    output,
		Reasoning: reasoning,
		Cache:     UsageCache{Write: cacheWrite, Read: cacheRead},
	}
}

// naiveFloatCost is the cost expression written in plain float64 — the
// implementation ENGINE-DESIGN warns against and the reason decimal.go exists.
// It is only ever called from this test.
func naiveFloatCost(t UsageTokens, r costRates) float64 {
	return t.Input*r.input/1e6 +
		t.Output*r.output/1e6 +
		t.Cache.Read*r.cacheRead/1e6 +
		t.Cache.Write*r.cacheWrite/1e6 +
		t.Reasoning*r.output/1e6
}

// The whole justification for porting decimal.js rather than using float64 (or
// an exact big.Rat) is that the three answers differ. Each `want` below is the
// value decimal.js itself produced for the same inputs; `naive` is what plain
// float64 arithmetic gives. If these ever converge the test is still correct —
// but the divergence assertions would start failing, which is the signal that
// something changed.
func TestCostDivergesFromNaiveFloat64(t *testing.T) {
	cases := []struct {
		name  string
		toks  UsageTokens
		rates costRates
		want  float64
		naive float64
	}{
		{
			// A real 5.6h-run shape at Sonnet prices: the naive float64 sum is
			// wrong in the 15th significant digit, and that value is what gets
			// persisted as the session cost.
			name:  "sonnet/3.59M-input",
			toks:  decTokens(3_590_000, 250_000, 100_000, 2_000_000, 500_000),
			rates: costRates{input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
			want:  18.495,
			naive: 18.494999999999997,
		},
		{
			name:  "long-decimal-rates/999999",
			toks:  decTokens(999_999, 999_999, 999_999, 999_999, 999_999),
			rates: costRates{input: 0.12345678901234567, output: 9.876543210987654, cacheRead: 0.000123456789012345, cacheWrite: 3.3333333333333335},
			want:  23.209976791109998,
			naive: 23.209976791109995,
		},
		{
			name:  "repeating-rates/999999",
			toks:  decTokens(999_999, 999_999, 999_999, 999_999, 999_999),
			rates: costRates{input: 1.0 / 3, output: 2.0 / 3, cacheRead: 1.0 / 7, cacheWrite: 1.0 / 9},
			want:  1.9206329999999998,
			naive: 1.920633,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := costDecimal(tc.toks, tc.rates).toNumber()
			if got != tc.want {
				t.Errorf("costDecimal = %v, want %v (decimal.js)", got, tc.want)
			}
			if naive := naiveFloatCost(tc.toks, tc.rates); naive != tc.naive {
				t.Errorf("naiveFloatCost = %v, want %v", naive, tc.naive)
			}
			if tc.want == tc.naive {
				t.Fatalf("case no longer demonstrates a divergence")
			}
		})
	}
}

// decimal.js's 20-significant-digit ROUND_HALF_UP finalise, which a big.Rat
// would not reproduce. The `want` strings come from the real module.
func TestDecimalRoundsToTwentySignificantDigits(t *testing.T) {
	cases := []struct {
		a, b float64
		want string
	}{
		// exact product is 123456.66555555664765434 — 23 significant digits.
		{999999, 0.12345678901234567, "123456.66555555664765"},
		{1, 1, "1"},
		{0, 12345, "0"},
		{-1, 0.5, "-0.5"},
	}
	for _, tc := range cases {
		if got := decFromFloat(tc.a).mul(decFromFloat(tc.b)).decString(); got != tc.want {
			t.Errorf("%v × %v = %s, want %s", tc.a, tc.b, got, tc.want)
		}
	}
	// ROUND_HALF_UP is ties-AWAY-FROM-ZERO, not ties-to-even.
	if got := decParse("1.00000000000000000005").add(decZero()).decString(); got != "1.0000000000000000001" {
		t.Errorf("half-up positive: %s", got)
	}
	if got := decParse("-1.00000000000000000005").mul(decFromFloat(1)).decString(); got != "-1.0000000000000000001" {
		t.Errorf("half-up negative: %s", got)
	}
}

// toString goes exponential exactly outside (toExpNeg, toExpPos) = (-7, 21),
// and strips trailing zeros first (decimal.mjs:2437, :3108-3138).
func TestDecimalToStringWindow(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.20", "1.2"},
		{"0.0000001", "1e-7"},
		{"0.00000001", "1e-8"},
		{"1e20", "100000000000000000000"},
		{"1e21", "1e+21"},
		{"123456789012345678901", "123456789012345678901"},
		{"0", "0"},
		{"-0", "0"},
		{"0.1", "0.1"},
		{"100", "100"},
		{"-1e-8", "-1e-8"},
		{"1.000000000000001247e+30", "1.000000000000001247e+30"},
	}
	for _, tc := range cases {
		if got := decParse(tc.in).decString(); got != tc.want {
			t.Errorf("decParse(%q).decString() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Non-finite propagation through the cost chain, matching decimal.js: 0 × ∞ is
// NaN, ∞ + (−∞) is NaN, ∞ / 1e6 is ∞. getUsage's `safe()` then collapses every
// one of these to a cost of 0.
func TestCostNonFinitePropagation(t *testing.T) {
	inf := math.Inf(1)
	cases := []struct {
		name  string
		toks  UsageTokens
		rates costRates
		want  string
	}{
		{"inf-tokens-zero-rate", decTokens(inf, 0, 0, 0, 0), costRates{}, "NaN"},
		{"inf-tokens-positive-rate", decTokens(inf, 0, 0, 0, 0), costRates{input: 3}, "Infinity"},
		{"nan-tokens", decTokens(math.NaN(), 0, 0, 0, 0), costRates{input: 3}, "NaN"},
		{"inf-tokens-negative-rate", decTokens(inf, 0, 0, 0, 0), costRates{input: -3}, "-Infinity"},
		{"opposite-infinities", decTokens(inf, math.Inf(-1), 0, 0, 0), costRates{input: 1, output: 1}, "NaN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := costDecimal(tc.toks, tc.rates)
			if got := result.decString(); got != tc.want {
				t.Errorf("decString = %q, want %q", got, tc.want)
			}
			if got := safe(result.toNumber()); got != 0 {
				t.Errorf("safe(toNumber) = %v, want 0", got)
			}
		})
	}
}

// `new Decimal(x)` for a JS number goes through the SHORTEST round-trip decimal
// string, not the exact binary value — so 0.1 is exactly one tenth here.
func TestDecFromFloatUsesShortestRepr(t *testing.T) {
	if got := decFromFloat(0.1).decString(); got != "0.1" {
		t.Errorf("decFromFloat(0.1) = %q, want \"0.1\"", got)
	}
	if got := decFromFloat(1e21).decString(); got != "1e+21" {
		t.Errorf("decFromFloat(1e21) = %q, want \"1e+21\"", got)
	}
	if got := decFromFloat(-0.0).decString(); got != "0" {
		t.Errorf("decFromFloat(-0) = %q, want \"0\"", got)
	}
}
