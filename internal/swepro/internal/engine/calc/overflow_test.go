package calc

import (
	"math"
	"testing"
)

// Verbatim translation of src/session/overflow.test.ts. The `maxAuditFixCycles`
// describe block at the bottom of that file is deliberately absent: it exercises
// src/session/fix-generator.ts, which is a different package's port.
//
// The TS suite mutates process.env with a setEnv/afterEach pair; here each test
// installs a complete env map through SetEnvForTesting, which is the same thing
// said positively — a key absent from the map is a variable that is not set.

func cfgEmpty() Config { return Config{Compaction: &CompactionConfig{}} }

func testModel(context float64, input *float64, output float64) Model {
	return Model{Limit: ModelLimit{Context: context, Input: input, Output: output}}
}

func ptr[T any](v T) *T { return &v }

func totalTokens(total float64) Tokens {
	return Tokens{Total: &total, Input: 0, Output: 0, Cache: TokenCache{Read: 0, Write: 0}}
}

// ── usable — effective context cap ───────────────────────────────────────

func TestUsableEffectiveContextCap(t *testing.T) {
	t.Run("a 1M-window model is clamped to the universal cap, not the advertised window", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		// deepseek-v4-pro registry shape: context 1,048,576, output 384,000.
		// Uncapped this would be (1,048,576 - 384,000) * 0.6 ≈ 398,745 —
		// compaction effectively never fires. Capped: 160,000 * 0.6 = 96,000.
		got := Usable(UsableInput{Cfg: cfgEmpty(), Model: testModel(1_048_576, nil, 384_000)})
		if got != 96_000 {
			t.Errorf("usable = %v, want 96000", got)
		}
	})

	t.Run("a model whose usable window is already below the cap is unaffected", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		// 128K input limit − 20K reserve = 108K usable, under the 160K cap.
		got := Usable(UsableInput{Cfg: cfgEmpty(), Model: testModel(131_072, ptr(128_000.0), 8_192)})
		want := math.Floor((128_000 - 8_192) * 0.6)
		if got != want {
			t.Errorf("usable = %v, want %v", got, want)
		}
	})

	t.Run("CODEAF_EFFECTIVE_CONTEXT=0 disables the cap", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_EFFECTIVE_CONTEXT": "0"})()
		got := Usable(UsableInput{Cfg: cfgEmpty(), Model: testModel(1_048_576, nil, 384_000)})
		// maxOutputTokens caps at OUTPUT_TOKEN_MAX (32K), so raw = 1,048,576 − 32,000.
		want := math.Floor((1_048_576 - 32_000) * 0.6)
		if got != want {
			t.Errorf("usable = %v, want %v", got, want)
		}
	})

	t.Run("CODEAF_EFFECTIVE_CONTEXT overrides the default cap", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_EFFECTIVE_CONTEXT": "100000"})()
		got := Usable(UsableInput{Cfg: cfgEmpty(), Model: testModel(1_048_576, nil, 384_000)})
		if got != 60_000 {
			t.Errorf("usable = %v, want 60000", got)
		}
	})
}

// ── isOverflow — working-set drift early-trigger ─────────────────────────

// 128K input − 8,192 reserve, ×0.6 trigger ⇒ usable cap 71,884; half 35,942.
func driftModel() Model { return testModel(131_072, ptr(128_000.0), 8_192) }

func driftCap() float64 { return Usable(UsableInput{Cfg: cfgEmpty(), Model: driftModel()}) }

func TestIsOverflowDrift(t *testing.T) {
	t.Run("no drift argument ⇒ pure occupancy path, byte-identical (count >= cap)", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		c := driftCap()
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(c - 1)}) {
			t.Error("below cap should not overflow")
		}
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(c)}) {
			t.Error("at cap should overflow")
		}
	})

	t.Run("drift above threshold + occupancy above half fires early", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		mid := math.Floor(driftCap() * 0.6) // above half (0.5), below cap
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid)}) {
			t.Error("without drift should stay under cap")
		}
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.8)}) {
			t.Error("with high drift should fire")
		}
	})

	t.Run("drift below threshold does not fire even above half occupancy", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		mid := math.Floor(driftCap() * 0.6)
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.5)}) {
			t.Error("drift 0.5 should not fire")
		}
	})

	t.Run("high drift below half occupancy does not fire", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		low := math.Floor(driftCap() * 0.4) // below half
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(low), Drift: ptr(0.95)}) {
			t.Error("below half occupancy should not fire")
		}
	})

	t.Run("CODEAF_WS_DRIFT=0 disables the drift path", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_WS_DRIFT": "0"})()
		mid := math.Floor(driftCap() * 0.6)
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.99)}) {
			t.Error("drift path should be disabled")
		}
	})

	t.Run("CODEAF_WS_DRIFT overrides the default threshold", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_WS_DRIFT": "0.9"})()
		mid := math.Floor(driftCap() * 0.6)
		// 0.8 no longer clears the raised bar; 0.95 does.
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.8)}) {
			t.Error("0.8 should not clear a 0.9 bar")
		}
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.95)}) {
			t.Error("0.95 should clear a 0.9 bar")
		}
	})

	t.Run("at/above cap, drift is irrelevant (occupancy wins)", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		c := driftCap()
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(c), Drift: ptr(0.0)}) {
			t.Error("at cap with drift 0 should overflow")
		}
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(c), Drift: ptr(0.1)}) {
			t.Error("at cap with drift 0.1 should overflow")
		}
	})

	t.Run("compaction.auto=false short-circuits regardless of drift", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		mid := math.Floor(driftCap() * 0.6)
		off := Config{Compaction: &CompactionConfig{Auto: ptr(false)}}
		if IsOverflow(OverflowInput{Cfg: off, Model: driftModel(), Tokens: totalTokens(mid), Drift: ptr(0.99)}) {
			t.Error("auto=false should short-circuit")
		}
	})
}

// ── shouldScanDrift — the processor-side scan guard (ITEM 1 seam) ─────────

func TestShouldScanDrift(t *testing.T) {
	t.Run("does not scan below half the usable cap (cheap on small contexts)", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		low := math.Floor(driftCap() * 0.4)
		if ShouldScanDrift(ScanDriftInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(low)}) {
			t.Error("should not scan below half")
		}
	})

	t.Run("scans once occupancy reaches half the usable cap", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		mid := math.Floor(driftCap() * 0.6)
		if !ShouldScanDrift(ScanDriftInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid)}) {
			t.Error("should scan at 0.6 of cap")
		}
	})

	t.Run("does not scan at/above the hard cap (pure occupancy already fires)", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		if ShouldScanDrift(ScanDriftInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(driftCap())}) {
			t.Error("should not scan at the cap")
		}
	})

	t.Run("CODEAF_WS_DRIFT=0 disables the scan entirely", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_WS_DRIFT": "0"})()
		mid := math.Floor(driftCap() * 0.6)
		if ShouldScanDrift(ScanDriftInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid)}) {
			t.Error("CODEAF_WS_DRIFT=0 should disable the scan")
		}
	})

	t.Run("compaction.auto=false disables the scan", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		mid := math.Floor(driftCap() * 0.6)
		off := Config{Compaction: &CompactionConfig{Auto: ptr(false)}}
		if ShouldScanDrift(ScanDriftInput{Cfg: off, Model: driftModel(), Tokens: totalTokens(mid)}) {
			t.Error("auto=false should disable the scan")
		}
	})

	t.Run("half-cap scan feeds a drift that then trips isOverflow (call-site contract)", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		// The exact wiring processor.ts performs: only scan when shouldScanDrift
		// is true, and a high scanned drift trips overflow early.
		mid := math.Floor(driftCap() * 0.6)
		if !ShouldScanDrift(ScanDriftInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid)}) {
			t.Fatal("expected a scan")
		}
		scannedDrift := 0.85 // what workingSetDrift would return for a stale-heavy context
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: driftModel(), Tokens: totalTokens(mid), Drift: &scannedDrift}) {
			t.Error("scanned drift should trip overflow")
		}
	})
}

// ── isOverflow — leaf (coder) absolute token trigger (W6c) ───────────────

// Big-window model so the global usable cap sits far above the leaf trigger:
// 1M context clamps to the 160K effective cap, ×0.6 ⇒ usable 96,000. The leaf
// default trigger of 60,000 is well under that, so it dominates for coder.
func bigModel() Model { return testModel(1_048_576, nil, 384_000) }

func TestIsOverflowLeafTrigger(t *testing.T) {
	t.Run("coder fires at 60_001 tokens while a non-coder with the same tokens does not", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		// usable cap = 96,000; leaf trigger = 60,000. At 60,001 the coder trips
		// the leaf bound; a non-coder (or omitted agent) is still far below 96,000.
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(60_001), Agent: ptr("coder")}) {
			t.Error("coder should overflow at 60001")
		}
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(60_001), Agent: ptr("root-orchestrator")}) {
			t.Error("root-orchestrator should not overflow at 60001")
		}
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(60_001)}) {
			t.Error("omitted agent should not overflow at 60001")
		}
	})

	t.Run("coder does not fire just below the leaf trigger", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{})()
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(59_999), Agent: ptr("coder")}) {
			t.Error("coder should not overflow at 59999")
		}
	})

	t.Run("kill-switch CODEAF_LEAF_CONTEXT_TRIGGER=0 restores global-window behavior for coder", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_LEAF_CONTEXT_TRIGGER": "0"})()
		// With the leaf cap disabled, coder behaves byte-identically to a
		// non-coder: 60,001 is under the 96,000 usable cap, so neither fires.
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(60_001), Agent: ptr("coder")}) {
			t.Error("kill switch should restore global behavior")
		}
		// And it still fires at the shared usable cap, exactly like any other agent.
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(96_000), Agent: ptr("coder")}) {
			t.Error("coder should still fire at the usable cap")
		}
	})

	t.Run("knob env override is respected — CODEAF_KNOB_LEAF_CONTEXT_TRIGGER_TOKENS=30000 trips at 30_001", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_KNOB_LEAF_CONTEXT_TRIGGER_TOKENS": "30000"})()
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(30_001), Agent: ptr("coder")}) {
			t.Error("should overflow at 30001")
		}
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(29_999), Agent: ptr("coder")}) {
			t.Error("should not overflow at 29999")
		}
	})

	t.Run("env below the knob min clamps up to 20_000", func(t *testing.T) {
		defer SetEnvForTesting(map[string]string{"CODEAF_KNOB_LEAF_CONTEXT_TRIGGER_TOKENS": "5000"})()
		// 5,000 clamps to the declared min of 20,000, so 20,001 trips but 19,999 does not.
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(20_001), Agent: ptr("coder")}) {
			t.Error("should overflow at 20001")
		}
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(19_999), Agent: ptr("coder")}) {
			t.Error("should not overflow at 19999")
		}
	})

	t.Run("the usable cap still wins when it is tighter than the leaf trigger", func(t *testing.T) {
		// Force a tiny effective context so usable() < leaf trigger; coder then
		// trips at the usable cap, not the (larger) leaf bound.
		defer SetEnvForTesting(map[string]string{"CODEAF_EFFECTIVE_CONTEXT": "50000"})() // usable = 50,000 × 0.6 = 30,000
		if !IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(30_000), Agent: ptr("coder")}) {
			t.Error("should overflow at the usable cap")
		}
		if IsOverflow(OverflowInput{Cfg: cfgEmpty(), Model: bigModel(), Tokens: totalTokens(29_999), Agent: ptr("coder")}) {
			t.Error("should not overflow below the usable cap")
		}
	})
}

// ── seams ────────────────────────────────────────────────────────────────

// OUTPUT_TOKEN_MAX is frozen at module load in TS (transform.ts:20 reads a Flag
// object built at flag.ts:30), so the runtime env seam must NOT move it —
// only SetModuleEnvForTesting may.
func TestOutputTokenMaxIsModuleLoadOnly(t *testing.T) {
	defer SetEnvForTesting(map[string]string{"CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX": "1000"})()
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 32_000 {
		t.Errorf("runtime env moved OUTPUT_TOKEN_MAX: got %v, want 32000", got)
	}
	restore := SetModuleEnvForTesting(map[string]string{"CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX": "1000"})
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 1_000 {
		t.Errorf("module env did not move OUTPUT_TOKEN_MAX: got %v, want 1000", got)
	}
	restore()
	if got := MaxOutputTokens(testModel(200_000, nil, 64_000)); got != 32_000 {
		t.Errorf("restore leaked: got %v, want 32000", got)
	}
}

// `Flag.number` rejects non-integers, non-positives and the falsy strings before
// the `|| 32_000` fallback ever runs (flag.ts:18-23).
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
		{"0x20", 32},
	} {
		got := evalOutputTokenMax(map[string]string{"CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX": tc.raw})
		if got != tc.want {
			t.Errorf("evalOutputTokenMax(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
	if got := evalOutputTokenMax(map[string]string{}); got != 32_000 {
		t.Errorf("evalOutputTokenMax(absent) = %v, want 32000", got)
	}
}

// The env seam distinguishes "absent" from "set to empty string" — the whole
// reason it is a map and not os.Getenv. CODEAF_EFFECTIVE_CONTEXT="" is
// Number("") === 0, which DISABLES the cap.
func TestEnvAbsentVersusEmpty(t *testing.T) {
	restore := SetEnvForTesting(map[string]string{})
	if got := EffectiveContextCap(); got != EFFECTIVE_CONTEXT_CAP_DEFAULT {
		t.Errorf("absent: got %v, want %v", got, EFFECTIVE_CONTEXT_CAP_DEFAULT)
	}
	restore()

	restore = SetEnvForTesting(map[string]string{"CODEAF_EFFECTIVE_CONTEXT": ""})
	if got := EffectiveContextCap(); !math.IsInf(got, 1) {
		t.Errorf("empty string: got %v, want +Inf", got)
	}
	restore()

	// The same asymmetry for the drift threshold: absent is 0.7, "" is 0.
	restore = SetEnvForTesting(map[string]string{})
	if got := DriftThreshold(); got != DRIFT_THRESHOLD_DEFAULT {
		t.Errorf("absent drift: got %v, want %v", got, DRIFT_THRESHOLD_DEFAULT)
	}
	restore()
	restore = SetEnvForTesting(map[string]string{"CODEAF_WS_DRIFT": ""})
	if got := DriftThreshold(); got != 0 {
		t.Errorf("empty drift: got %v, want 0", got)
	}
	restore()

	// But triggerPct's `Number("")` is 0, which fails `n <= 0`, so "" falls
	// back to the default rather than disabling anything.
	restore = SetEnvForTesting(map[string]string{"CODEAF_COMPACT_TRIGGER_PCT": ""})
	if got := TriggerPct(); got != TRIGGER_PCT_DEFAULT {
		t.Errorf("empty pct: got %v, want %v", got, TRIGGER_PCT_DEFAULT)
	}
	restore()
}

// The kill switch is a STRING comparison against "0" — "0.0", " 0" and "00" all
// leave the leaf trigger enabled (overflow.ts:120).
func TestLeafKillSwitchIsStringEquality(t *testing.T) {
	for _, raw := range []string{"0.0", "00", " 0", "false"} {
		restore := SetEnvForTesting(map[string]string{"CODEAF_LEAF_CONTEXT_TRIGGER": raw})
		if got := LeafTriggerTokens(); got != 60_000 {
			t.Errorf("CODEAF_LEAF_CONTEXT_TRIGGER=%q: got %v, want 60000", raw, got)
		}
		restore()
	}
	restore := SetEnvForTesting(map[string]string{"CODEAF_LEAF_CONTEXT_TRIGGER": "0"})
	if got := LeafTriggerTokens(); !math.IsInf(got, 1) {
		t.Errorf(`CODEAF_LEAF_CONTEXT_TRIGGER="0": got %v, want +Inf`, got)
	}
	restore()
}
