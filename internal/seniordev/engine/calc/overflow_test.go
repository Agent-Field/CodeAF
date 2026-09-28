//go:build !windows

package calc

import (
	"math"
	"testing"
)

func cfgEmpty() Config { return Config{Compaction: &CompactionConfig{}} }

func cfgWith(mutate func(*CompactionConfig)) Config {
	c := &CompactionConfig{}
	mutate(c)
	return Config{Compaction: c}
}

func testModel(context float64, input *float64, output float64) Model {
	return Model{Limit: ModelLimit{Context: context, Input: input, Output: output}}
}

func ptr[T any](v T) *T { return &v }

func totalTokens(total float64) Tokens {
	return Tokens{Total: &total, Input: 0, Output: 0, Cache: TokenCache{Read: 0, Write: 0}}
}

// ── capacity ─────────────────────────────────────────────────────────────

func TestEffectiveInputCapacity(t *testing.T) {
	t.Run("a small window is the budget", func(t *testing.T) {
		// 128K input limit - 8,192 output reserve, under the default cap.
		got := EffectiveInputCapacity(UsableInput{Cfg: cfgEmpty(), Model: testModel(131_072, ptr(128_000.0), 8_192)})
		if got != 128_000-8_192 {
			t.Errorf("capacity = %v, want %v", got, 128_000-8_192)
		}
	})

	t.Run("a large window is capped at the default capacity", func(t *testing.T) {
		got := EffectiveInputCapacity(UsableInput{Cfg: cfgEmpty(), Model: testModel(1_310_720, nil, 943_718)})
		if got != DefaultCapacityTokens {
			t.Errorf("capacity = %v, want the %v default", got, DefaultCapacityTokens)
		}
		if absent := EffectiveInputCapacity(UsableInput{Model: testModel(1_310_720, nil, 943_718)}); absent != DefaultCapacityTokens {
			t.Errorf("capacity with no compaction block = %v, want the default", absent)
		}
	})

	t.Run("an absent input limit is budgeted from the context minus the output cap", func(t *testing.T) {
		// OUTPUT_TOKEN_MAX (32,000) is the reservation when limit.output exceeds it.
		got := EffectiveInputCapacity(UsableInput{Cfg: cfgEmpty(), Model: testModel(400_000, nil, 384_000)})
		if got != 400_000-32_000 {
			t.Errorf("capacity = %v, want %v", got, 400_000-32_000)
		}
	})

	t.Run("capacity_tokens tightens and never widens", func(t *testing.T) {
		model := testModel(1_310_720, nil, 943_718)
		tight := EffectiveInputCapacity(UsableInput{Cfg: cfgWith(func(c *CompactionConfig) { c.CapacityTokens = ptr(100_000.0) }), Model: model})
		if tight != 100_000 {
			t.Errorf("tightened capacity = %v, want 100000", tight)
		}
		wide := EffectiveInputCapacity(UsableInput{Cfg: cfgWith(func(c *CompactionConfig) { c.CapacityTokens = ptr(5_000_000.0) }), Model: model})
		if wide != 1_310_720-32_000 {
			t.Errorf("a cap above the window must not widen it: %v", wide)
		}
	})

	t.Run("reserved overrides the output reservation", func(t *testing.T) {
		got := EffectiveInputCapacity(UsableInput{
			Cfg:   cfgWith(func(c *CompactionConfig) { c.Reserved = ptr(131_072.0) }),
			Model: testModel(400_000, ptr(400_000.0), 943_718),
		})
		if got != 400_000-131_072 {
			t.Errorf("capacity = %v, want %v", got, 400_000-131_072)
		}
	})

	t.Run("a zero context has no capacity", func(t *testing.T) {
		if got := EffectiveInputCapacity(UsableInput{Cfg: cfgEmpty(), Model: testModel(0, nil, 0)}); got != 0 {
			t.Errorf("capacity = %v, want 0", got)
		}
	})
}

func TestWatermarksAreASixtyFortySplitOfTheCapacity(t *testing.T) {
	marks := Watermarks(UsableInput{
		Cfg:   cfgWith(func(c *CompactionConfig) { c.CapacityTokens = ptr(500_000.0) }),
		Model: testModel(1_310_720, nil, 943_718),
	})
	if marks.Capacity != 500_000 || marks.High != 300_000 || marks.Low != 200_000 {
		t.Fatalf("watermarks = %#v, want 500000/300000/200000", marks)
	}
	small := Watermarks(UsableInput{Cfg: cfgEmpty(), Model: testModel(100_000, ptr(100_000.0), 10_000)})
	if small.Capacity != 90_000 || small.High != 54_000 || small.Low != 36_000 {
		t.Fatalf("small watermarks = %#v, want 90000/54000/36000", small)
	}
}

// ── the trigger ──────────────────────────────────────────────────────────

func TestIsOverflowTriggersOnOccupancyOnly(t *testing.T) {
	cfg := cfgWith(func(c *CompactionConfig) { c.CapacityTokens = ptr(500_000.0) })
	model := testModel(1_310_720, nil, 943_718)
	if IsOverflow(OverflowInput{Cfg: cfg, Model: model, Tokens: totalTokens(299_999)}) {
		t.Error("should not fire below high")
	}
	if !IsOverflow(OverflowInput{Cfg: cfg, Model: model, Tokens: totalTokens(300_000)}) {
		t.Error("should fire at high")
	}
	// A total of 0 falls through to the component sum.
	summed := Tokens{Input: 200_000, Output: 50_000, Cache: TokenCache{Read: 50_000}}
	if !IsOverflow(OverflowInput{Cfg: cfg, Model: model, Tokens: summed}) {
		t.Error("the component sum should trigger when total is absent")
	}
	// auto:false disables everything.
	off := cfgWith(func(c *CompactionConfig) { c.Auto = ptr(false) })
	if IsOverflow(OverflowInput{Cfg: off, Model: model, Tokens: totalTokens(9_000_000)}) {
		t.Error("auto=false must win")
	}
	// A model with no context never overflows.
	if IsOverflow(OverflowInput{Cfg: cfg, Model: testModel(0, nil, 0), Tokens: totalTokens(9_000_000)}) {
		t.Error("a zero-context model must not overflow")
	}
}

// ── the output cap ───────────────────────────────────────────────────────

// OUTPUT_TOKEN_MAX is read once at package init; only SetModuleEnvForTesting
// moves it.
func TestOutputTokenMaxIsReadAtInit(t *testing.T) {
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 32_000 {
		t.Errorf("default OUTPUT_TOKEN_MAX: got %v, want 32000", got)
	}
	restore := SetModuleEnvForTesting(map[string]string{"SENIOR_DEV_OUTPUT_TOKEN_MAX": "1000"})
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 1_000 {
		t.Errorf("module env did not move OUTPUT_TOKEN_MAX: got %v, want 1000", got)
	}
	restore()
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 32_000 {
		t.Errorf("restore leaked: got %v, want 32000", got)
	}
	// A model whose own output limit is below the cap keeps its limit.
	if got := MaxOutputTokens(testModel(200_000, nil, 12_000)); got != 12_000 {
		t.Errorf("model limit below the cap: got %v, want 12000", got)
	}
}

// Only a positive integer moves the cap; the falsy strings and malformed
// values fall back to the default.
func TestEvalOutputTokenMax(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want float64
	}{
		{"", 32_000},
		{"0", 32_000},
		{"-4", 32_000},
		{"1.5", 32_000},
		{"banana", 32_000},
		{"8000", 8_000},
		{"1", 1},
		{"1e4", 10_000},
		{"0x20", 32_000},
		{"131072", 131_072},
	} {
		got := evalOutputTokenMax(map[string]string{"SENIOR_DEV_OUTPUT_TOKEN_MAX": tc.raw})
		if got != tc.want {
			t.Errorf("evalOutputTokenMax(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
	if got := evalOutputTokenMax(map[string]string{}); got != 32_000 {
		t.Errorf("evalOutputTokenMax(absent) = %v, want 32000", got)
	}
}

// ── validation ───────────────────────────────────────────────────────────

func TestPolicyValidation(t *testing.T) {
	for _, name := range []string{"", PolicyWindow} {
		if err := ValidatePolicy(Config{Compaction: &CompactionConfig{Policy: name}}); err != nil {
			t.Errorf("policy %q should validate: %v", name, err)
		}
	}
	if err := ValidatePolicy(Config{}); err != nil {
		t.Errorf("absent block should validate: %v", err)
	}
	for _, name := range []string{"legacy", "adaptive"} {
		if err := ValidatePolicy(Config{Compaction: &CompactionConfig{Policy: name}}); err == nil {
			t.Errorf("policy %q must be refused, not ignored", name)
		}
	}
	for _, bad := range []float64{0, 1, 1.5, -0.2, math.NaN()} {
		f := bad
		if err := ValidatePolicy(cfgWith(func(c *CompactionConfig) { c.PreserveRecentFraction = &f })); err == nil {
			t.Errorf("preserve_recent_fraction %v must be refused", bad)
		}
	}
	for _, bad := range []float64{0, -1, math.Inf(1), math.NaN()} {
		c := bad
		if err := ValidatePolicy(cfgWith(func(cfg *CompactionConfig) { cfg.CapacityTokens = &c })); err == nil {
			t.Errorf("capacity_tokens %v must be refused", bad)
		}
	}
}
