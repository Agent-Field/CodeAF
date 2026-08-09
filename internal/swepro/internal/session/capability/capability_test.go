package capability

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

// Translation of src/session/capability.test.ts. Subtest names are the TS
// describe/test strings verbatim (Go renders spaces as underscores); the
// assertions keep the same semantics, including the 1e-9 monotonicity slack and
// the toBeCloseTo(…, 10) round-trip tolerance.

// ---------------------------------------------------------------------------
// Fixtures. Hand-built LeafOutcomes — everything the estimator ignores is
// filled with harmless constants so the test reads as "who did what at which
// band with which verdict".
var (
	highModel = ModelRef{ProviderID: "openrouter", ModelID: "qwen3.6-plus"}
	lowModel  = ModelRef{ProviderID: "openrouter", ModelID: "gemma-4-26b"}
)

func tiers() *CapabilityOptions {
	return &CapabilityOptions{
		TierMap: map[string]ModelTierName{"qwen3.6-plus": TierHigh, "gemma-4-26b": TierLow},
	}
}

func outcomeFor(modelID string, band sizeband.SizeBand, verdict LeafVerdict) LeafOutcome {
	repairRounds := 0.0
	if verdict == VerdictPassAfterRepair {
		repairRounds = 1
	}
	return LeafOutcome{
		TaskID:        "t",
		Model:         &LeafOutcomeModel{ProviderID: "openrouter", ModelID: modelID},
		SizeBand:      band,
		Verdict:       verdict,
		RepairRounds:  jscompat.JSNumber(repairRounds),
		Turns:         5,
		ToolErrors:    0,
		CostUsd:       0.01,
		WallMs:        1000,
		MergeConflict: false,
		Timestamp:     0,
	}
}

func feed(tr *CapabilityTracker, modelID string, band sizeband.SizeBand, verdict LeafVerdict, n int) {
	for i := 0; i < n; i++ {
		tr.Observe(outcomeFor(modelID, band, verdict))
	}
}

// bandsIndexOf is BANDS.indexOf.
func bandsIndexOf(b sizeband.SizeBand) int {
	for i, x := range BANDS {
		if x == b {
			return i
		}
	}
	return -1
}

func tmpJsonl(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "outcomes.jsonl")
	joined := ""
	for i, l := range lines {
		if i > 0 {
			joined += "\n"
		}
		joined += l
	}
	if err := os.WriteFile(file, []byte(joined), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return file
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
func TestPriorOnlyBehaviorOptimisticByTier(t *testing.T) {
	t.Run("HIGH-tier model is reliable through at least 'l' with zero observations", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		band := tr.MaxReliableBand(highModel)
		if bandsIndexOf(band) < bandsIndexOf(sizeband.BandL) {
			t.Fatalf("BANDS.indexOf(%q) = %d, want >= indexOf(l)", band, bandsIndexOf(band))
		}
	})

	t.Run("LOW-tier model is reliable through ~'s' with zero observations", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		if got := tr.MaxReliableBand(lowModel); got != sizeband.BandS {
			t.Fatalf("maxReliableBand = %q, want \"s\"", got)
		}
	})

	t.Run("unknown model falls back to the conservative default tier (low)", func(t *testing.T) {
		tr := NewCapabilityTracker(&CapabilityOptions{}) // no tier map at all
		if got := tr.MaxReliableBand(ModelRef{ProviderID: "x", ModelID: "mystery"}); got != sizeband.BandS {
			t.Fatalf("maxReliableBand = %q, want \"s\"", got)
		}
	})
}

// ---------------------------------------------------------------------------
func TestConvergenceRealEvidenceOverridesTheOptimisticPrior(t *testing.T) {
	t.Run("repeated fails at 'l' drop maxReliableBand below 'l' within ~4 observations", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		if bandsIndexOf(tr.MaxReliableBand(highModel)) < bandsIndexOf(sizeband.BandL) {
			t.Fatalf("precondition: expected reliable through at least l")
		}

		dropped := false
		for i := 1; i <= 4; i++ {
			tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictFail))
			if bandsIndexOf(tr.MaxReliableBand(highModel)) < bandsIndexOf(sizeband.BandL) {
				dropped = true
				break
			}
		}
		if !dropped {
			t.Fatalf("dropped = false, want true")
		}
	})

	t.Run("~3-5 clean passes lift a LOW model's 'm' band above threshold (prior is weak)", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		if p := tr.PSuccess(lowModel, sizeband.BandM); !(p < 0.7) {
			t.Fatalf("pSuccess(low, m) = %v, want < 0.7", p) // prior 0.55
		}
		feed(tr, "gemma-4-26b", sizeband.BandM, VerdictPass, 5)
		if p := tr.PSuccess(lowModel, sizeband.BandM); !(p >= 0.7) {
			t.Fatalf("pSuccess(low, m) = %v, want >= 0.7", p)
		}
	})
}

// ---------------------------------------------------------------------------
func TestCrossBandPropagation(t *testing.T) {
	t.Run("clean passes at 'm' raise pSuccess at 'l' (upward), weaker than the direct band", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		lBefore := tr.PSuccess(lowModel, sizeband.BandL)
		feed(tr, "gemma-4-26b", sizeband.BandM, VerdictPass, 4)
		lAfter := tr.PSuccess(lowModel, sizeband.BandL)
		mAfter := tr.PSuccess(lowModel, sizeband.BandM)

		if !(lAfter > lBefore) {
			t.Fatalf("lAfter %v not > lBefore %v (promotion signal must propagate up)", lAfter, lBefore)
		}
		if !(mAfter > lAfter) {
			t.Fatalf("mAfter %v not > lAfter %v (direct evidence must dominate)", mAfter, lAfter)
		}
	})

	t.Run("a fail at 's' weakly informs larger bands (m, l) downward-capped by monotonicity", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		mBefore := tr.PSuccess(highModel, sizeband.BandM)
		feed(tr, "qwen3.6-plus", sizeband.BandS, VerdictFail, 3)
		if m := tr.PSuccess(highModel, sizeband.BandM); !(m < mBefore) {
			t.Fatalf("pSuccess(high, m) = %v, want < %v", m, mBefore)
		}
		m := tr.PSuccess(highModel, sizeband.BandM)
		s := tr.PSuccess(highModel, sizeband.BandS)
		if !(m <= s+1e-9) {
			t.Fatalf("monotonicity broken: m=%v > s=%v", m, s)
		}
	})

	t.Run("promotion can raise a LOW model's maxReliableBand from 's' to 'm'", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		if got := tr.MaxReliableBand(lowModel); got != sizeband.BandS {
			t.Fatalf("maxReliableBand = %q, want \"s\"", got)
		}
		feed(tr, "gemma-4-26b", sizeband.BandM, VerdictPass, 4)
		if got := bandsIndexOf(tr.MaxReliableBand(lowModel)); got < bandsIndexOf(sizeband.BandM) {
			t.Fatalf("maxReliableBand index = %d, want >= indexOf(m)", got)
		}
	})
}

// ---------------------------------------------------------------------------
func TestMonotonicityPSuccessIsNonIncreasingAcrossBands(t *testing.T) {
	// Property-style: a handful of fixed pseudo-random sequences (NO Math.random
	// in the module — the sequences are hard-coded here). After each, the curve
	// must be non-increasing xs → xl.
	verdicts := []LeafVerdict{VerdictPass, VerdictPassAfterRepair, VerdictFail, VerdictEscalated}
	seqs := [][]int{
		{3, 1, 0, 2, 4, 1, 3, 0}, // band-index, verdict-index alternating stream
		{0, 0, 4, 4, 2, 2, 1, 3},
		{4, 3, 2, 1, 0, 0, 1, 2},
		{1, 2, 3, 2, 1, 4, 0, 3},
	}

	for i, seq := range seqs {
		i, seq := i, seq
		t.Run(fmtSequenceName(i), func(t *testing.T) {
			tr := NewCapabilityTracker(tiers())
			for step := 0; step < len(seq); step++ {
				band := BANDS[seq[step]%len(BANDS)]
				verdict := verdicts[(seq[step]+step)%len(verdicts)]
				// Alternate models so both tiers get exercised.
				modelID := "gemma-4-26b"
				if step%2 == 0 {
					modelID = "qwen3.6-plus"
				}
				tr.Observe(outcomeFor(modelID, band, verdict))
			}
			for _, model := range []ModelRef{highModel, lowModel} {
				curve := make([]float64, len(BANDS))
				for j, b := range BANDS {
					curve[j] = tr.PSuccess(model, b)
				}
				for j := 1; j < len(curve); j++ {
					if !(curve[j] <= curve[j-1]+1e-9) {
						t.Fatalf("model %s: curve[%d]=%v > curve[%d]=%v", model.ModelID, j, curve[j], j-1, curve[j-1])
					}
				}
			}
		})
	}
}

func fmtSequenceName(i int) string { return "sequence " + string(rune('0'+i)) + " stays monotone" }

// ---------------------------------------------------------------------------
func TestPartialCredit(t *testing.T) {
	t.Run("pass_after_repair moves the posterior less than a clean pass, more than a fail", func(t *testing.T) {
		base := NewCapabilityTracker(tiers())
		priorM := base.PSuccess(lowModel, sizeband.BandM)

		tPass := NewCapabilityTracker(tiers())
		tPass.Observe(outcomeFor("gemma-4-26b", sizeband.BandM, VerdictPass))

		tRepair := NewCapabilityTracker(tiers())
		tRepair.Observe(outcomeFor("gemma-4-26b", sizeband.BandM, VerdictPassAfterRepair))

		tFail := NewCapabilityTracker(tiers())
		tFail.Observe(outcomeFor("gemma-4-26b", sizeband.BandM, VerdictFail))

		pPass := tPass.PSuccess(lowModel, sizeband.BandM)
		pRepair := tRepair.PSuccess(lowModel, sizeband.BandM)
		pFail := tFail.PSuccess(lowModel, sizeband.BandM)

		if !(pPass > pRepair) {
			t.Fatalf("pPass %v not > pRepair %v", pPass, pRepair)
		}
		if !(pRepair > pFail) {
			t.Fatalf("pRepair %v not > pFail %v", pRepair, pFail)
		}
		// pass_after_repair sits between fail and a clean pass, straddling prior.
		if !(pFail < priorM) {
			t.Fatalf("pFail %v not < priorM %v", pFail, priorM)
		}
		if !(pPass > priorM) {
			t.Fatalf("pPass %v not > priorM %v", pPass, priorM)
		}
	})
}

// ---------------------------------------------------------------------------
func TestDecayRecentEvidenceDominates(t *testing.T) {
	t.Run("same outcomes, recent-good history ends higher than recent-bad history", func(t *testing.T) {
		// Identical multiset {2 fails, 4 passes} at band 'm'; only the order
		// differs. If evidence decayed per observation, the history whose *recent*
		// outcomes are passes must end with the higher pSuccess.
		recentGood := NewCapabilityTracker(tiers())
		for _, v := range []LeafVerdict{VerdictFail, VerdictFail, VerdictPass, VerdictPass, VerdictPass, VerdictPass} {
			recentGood.Observe(outcomeFor("qwen3.6-plus", sizeband.BandM, v))
		}
		recentBad := NewCapabilityTracker(tiers())
		for _, v := range []LeafVerdict{VerdictPass, VerdictPass, VerdictPass, VerdictPass, VerdictFail, VerdictFail} {
			recentBad.Observe(outcomeFor("qwen3.6-plus", sizeband.BandM, v))
		}
		g := recentGood.PSuccess(highModel, sizeband.BandM)
		b := recentBad.PSuccess(highModel, sizeband.BandM)
		if !(g > b) {
			t.Fatalf("recentGood %v not > recentBad %v", g, b)
		}
	})

	t.Run("an early fail is largely washed out by many subsequent passes", func(t *testing.T) {
		withEarlyFail := NewCapabilityTracker(tiers())
		withEarlyFail.Observe(outcomeFor("qwen3.6-plus", sizeband.BandM, VerdictFail))
		feed(withEarlyFail, "qwen3.6-plus", sizeband.BandM, VerdictPass, 6)
		// A single stale fail must not forever punish a warmed-up model.
		if p := withEarlyFail.PSuccess(highModel, sizeband.BandM); !(p > 0.7) {
			t.Fatalf("pSuccess = %v, want > 0.7", p)
		}
	})
}

// ---------------------------------------------------------------------------
func TestLoadOutcomesCapabilityFromRunJSONLPersistenceWrapper(t *testing.T) {
	t.Run("skips malformed lines and never throws", func(t *testing.T) {
		good := mustJSON(t, outcomeFor("qwen3.6-plus", sizeband.BandM, VerdictPass))
		file := tmpJsonl(t, []string{
			good,
			"",          // blank
			"{not json", // malformed
			"   ",       // whitespace
			`{"model":{"modelID":""},"sizeBand":"m","verdict":"pass"}`,     // empty modelID
			`{"model":{"modelID":"x"},"sizeBand":"huge","verdict":"pass"}`, // bad band
			`{"model":{"modelID":"x"},"sizeBand":"m","verdict":"weird"}`,   // bad verdict
			mustJSON(t, outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictFail)),
			"  " + good + "  ", // whitespace-padded but valid
		})
		outcomes := LoadOutcomes(file)
		if len(outcomes) != 3 { // two `good` + the fail
			t.Fatalf("len(outcomes) = %d, want 3", len(outcomes))
		}
		for _, o := range outcomes {
			if o.Model.ModelID != "qwen3.6-plus" {
				t.Fatalf("modelID = %q, want qwen3.6-plus", o.Model.ModelID)
			}
		}
	})

	t.Run("missing file yields [] rather than throwing", func(t *testing.T) {
		got := LoadOutcomes("/no/such/path/outcomes.jsonl")
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
		if s := mustJSON(t, got); s != "[]" {
			t.Fatalf("stringify = %s, want []", s)
		}
	})

	t.Run("capabilityFromRun folds every parseable outcome into a tracker", func(t *testing.T) {
		line := mustJSON(t, outcomeFor("gemma-4-26b", sizeband.BandM, VerdictPass))
		file := tmpJsonl(t, []string{line, line, line, line})
		tr := CapabilityFromRun(file, tiers())
		if p := tr.PSuccess(lowModel, sizeband.BandM); !(p >= 0.7) {
			t.Fatalf("pSuccess = %v, want >= 0.7", p)
		}
	})
}

// ---------------------------------------------------------------------------
func TestHasObservationsT5ProbeWaveGate(t *testing.T) {
	t.Run("a fresh tracker has no observations for any model (priors only)", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		if tr.HasObservations(highModel) || tr.HasObservations(lowModel) {
			t.Fatalf("fresh tracker reported observations")
		}
	})

	t.Run("one observation flips the observed model to true, leaving others false", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		feed(tr, "qwen3.6-plus", sizeband.BandM, VerdictPass, 1)
		if !tr.HasObservations(highModel) {
			t.Fatalf("hasObservations(high) = false, want true")
		}
		if tr.HasObservations(lowModel) {
			t.Fatalf("hasObservations(low) = true, want false")
		}
	})

	t.Run("a fail counts as an observation (it carries failure mass, not zero)", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		feed(tr, "gemma-4-26b", sizeband.BandL, VerdictFail, 1)
		if !tr.HasObservations(lowModel) {
			t.Fatalf("hasObservations(low) = false, want true")
		}
	})

	t.Run("warm-started evidence counts as observations", func(t *testing.T) {
		file := tmpJsonl(t, []string{mustJSON(t, outcomeFor("qwen3.6-plus", sizeband.BandM, VerdictPass)), ""})
		tr := CapabilityFromRun(file, tiers())
		if !tr.HasObservations(highModel) {
			t.Fatalf("hasObservations(high) = false, want true")
		}
	})
}

// ---------------------------------------------------------------------------
func TestT6StreakAcceleratedPromotion(t *testing.T) {
	t.Run("K consecutive clean passes promote the model one band up faster than passive propagation", func(t *testing.T) {
		// HIGH model starts reliable through "l". Three CLEAN passes at "l" should
		// trip the accelerator and lift the reliable band to "xl".
		withAccel := NewCapabilityTracker(tiers()) // K=3, bonus=1.0 by default
		if got := withAccel.MaxReliableBand(highModel); got != sizeband.BandL {
			t.Fatalf("maxReliableBand = %q, want \"l\"", got)
		}
		feed(withAccel, "qwen3.6-plus", sizeband.BandL, VerdictPass, 3)
		if got := bandsIndexOf(withAccel.MaxReliableBand(highModel)); got < bandsIndexOf(sizeband.BandXL) {
			t.Fatalf("maxReliableBand index = %d, want >= indexOf(xl)", got)
		}

		// Same three clean passes with the accelerator disabled stay at "l".
		zero := 0.0
		opts := tiers()
		opts.PromotionStreakBonus = &zero
		noAccel := NewCapabilityTracker(opts)
		feed(noAccel, "qwen3.6-plus", sizeband.BandL, VerdictPass, 3)
		if got := noAccel.MaxReliableBand(highModel); got != sizeband.BandL {
			t.Fatalf("maxReliableBand = %q, want \"l\"", got)
		}
	})

	t.Run("fires at exactly K, not K-1", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		feed(tr, "qwen3.6-plus", sizeband.BandL, VerdictPass, 2)
		if got := tr.MaxReliableBand(highModel); got != sizeband.BandL {
			t.Fatalf("K-1 clean passes promoted early: %q", got)
		}
		feed(tr, "qwen3.6-plus", sizeband.BandL, VerdictPass, 1)
		if got := bandsIndexOf(tr.MaxReliableBand(highModel)); got < bandsIndexOf(sizeband.BandXL) {
			t.Fatalf("maxReliableBand index = %d, want >= indexOf(xl)", got)
		}
	})

	t.Run("a repair breaks the streak (no promotion)", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPassAfterRepair)) // resets streak
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))            // only 1 clean since reset
		if got := tr.MaxReliableBand(highModel); got != sizeband.BandL {
			t.Fatalf("maxReliableBand = %q, want \"l\"", got)
		}
	})

	t.Run("a fail breaks the streak (no promotion)", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictFail)) // resets streak
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		tr.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
		// Max 2 consecutive clean passes at any point since the fail → never fires.
		if got := tr.MaxReliableBand(highModel); got != sizeband.BandL {
			t.Fatalf("maxReliableBand = %q, want \"l\"", got)
		}
	})

	t.Run("promotion holds the monotone-curve invariant", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		feed(tr, "qwen3.6-plus", sizeband.BandL, VerdictPass, 3)
		curve := make([]float64, len(BANDS))
		for i, b := range BANDS {
			curve[i] = tr.PSuccess(highModel, b)
		}
		for i := 1; i < len(curve); i++ {
			if !(curve[i] <= curve[i-1]+1e-9) {
				t.Fatalf("curve[%d]=%v > curve[%d]=%v", i, curve[i], i-1, curve[i-1])
			}
		}
	})

	t.Run("promotion decays: a later fail streak pulls the promoted band back down", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		feed(tr, "qwen3.6-plus", sizeband.BandL, VerdictPass, 3)
		if got := bandsIndexOf(tr.MaxReliableBand(highModel)); got < bandsIndexOf(sizeband.BandXL) {
			t.Fatalf("maxReliableBand index = %d, want >= indexOf(xl)", got)
		}
		feed(tr, "qwen3.6-plus", sizeband.BandL, VerdictFail, 4) // emergent demotion
		if got := bandsIndexOf(tr.MaxReliableBand(highModel)); !(got < bandsIndexOf(sizeband.BandXL)) {
			t.Fatalf("maxReliableBand index = %d, want < indexOf(xl)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Not in capability.test.ts: the Go seam to the leaf-outcome port, plus the
// evidence-guard distinction that seam has to preserve (capability.ts:207-213).
func TestFromLeafOutcome(t *testing.T) {
	t.Run("a leafoutcome record observes identically to a native one", func(t *testing.T) {
		native := NewCapabilityTracker(tiers())
		bridged := NewCapabilityTracker(tiers())
		for i := 0; i < 3; i++ {
			native.Observe(outcomeFor("qwen3.6-plus", sizeband.BandL, VerdictPass))
			bridged.Observe(FromLeafOutcome(leafoutcome.LeafOutcome{
				TaskID:        "t",
				Model:         leafoutcome.LeafOutcomeModel{ProviderID: "openrouter", ModelID: "qwen3.6-plus"},
				SizeBand:      sizeband.BandL,
				Verdict:       leafoutcome.VerdictPass,
				RepairRounds:  0,
				Turns:         5,
				ToolErrors:    0,
				CostUsd:       0.01,
				WallMs:        1000,
				MergeConflict: false,
				Timestamp:     0,
			}))
		}
		if got, want := mustJSON(t, bridged.Snapshot()), mustJSON(t, native.Snapshot()); got != want {
			t.Fatalf("bridged snapshot %s != native %s", got, want)
		}
	})

	t.Run("a fully populated zero evidence object still triggers the skip", func(t *testing.T) {
		enabled := true
		opts := tiers()
		opts.AdaptiveCutsEnabled = &enabled
		tr := NewCapabilityTracker(opts)
		tr.Observe(FromLeafOutcome(leafoutcome.LeafOutcome{
			Model:    leafoutcome.LeafOutcomeModel{ModelID: "qwen3.6-plus"},
			SizeBand: sizeband.BandM,
			Verdict:  leafoutcome.VerdictFail,
			Evidence: &leafoutcome.LeafOutcomeEvidence{AuditCommandsRun: 0, AuditBlockers: 0},
		}))
		if tr.HasObservations(highModel) {
			t.Fatalf("evidence {0,0} on a fail must be skipped")
		}
	})

	t.Run("an evidence object missing auditCommandsRun does NOT trigger the skip", func(t *testing.T) {
		// `undefined === 0` is false — the guard needs the literal number 0.
		enabled := true
		opts := tiers()
		opts.AdaptiveCutsEnabled = &enabled
		tr := NewCapabilityTracker(opts)
		line := `{"model":{"modelID":"qwen3.6-plus"},"sizeBand":"m","verdict":"fail","evidence":{"auditBlockers":0}}`
		file := tmpJsonl(t, []string{line})
		for _, o := range LoadOutcomes(file) {
			tr.Observe(o)
		}
		if !tr.HasObservations(highModel) {
			t.Fatalf("a partial evidence object must NOT be skipped")
		}
	})

	t.Run("a nil evidence pointer does NOT trigger the skip", func(t *testing.T) {
		enabled := true
		opts := tiers()
		opts.AdaptiveCutsEnabled = &enabled
		tr := NewCapabilityTracker(opts)
		tr.Observe(FromLeafOutcome(leafoutcome.LeafOutcome{
			Model:    leafoutcome.LeafOutcomeModel{ModelID: "qwen3.6-plus"},
			SizeBand: sizeband.BandM,
			Verdict:  leafoutcome.VerdictFail,
		}))
		if !tr.HasObservations(highModel) {
			t.Fatalf("absent evidence must NOT be skipped")
		}
	})
}

// ---------------------------------------------------------------------------
func TestSnapshotRestoreWarmStartRoundTrip(t *testing.T) {
	t.Run("restore reproduces the estimator state", func(t *testing.T) {
		a := NewCapabilityTracker(tiers())
		feed(a, "gemma-4-26b", sizeband.BandM, VerdictPass, 4)
		feed(a, "qwen3.6-plus", sizeband.BandL, VerdictFail, 2)

		b := NewCapabilityTracker(tiers())
		snap := a.Snapshot()
		b.Restore(&snap)

		for _, model := range []ModelRef{highModel, lowModel} {
			for _, band := range BANDS {
				got := b.PSuccess(model, band)
				want := a.PSuccess(model, band)
				if math.Abs(got-want) >= 0.5e-10 { // toBeCloseTo(…, 10)
					t.Fatalf("model %s band %s: %v != %v", model.ModelID, band, got, want)
				}
			}
		}
	})

	t.Run("restore tolerates malformed snapshots without throwing", func(t *testing.T) {
		tr := NewCapabilityTracker(tiers())
		tr.Restore(&CapabilitySnapshot{Version: 2, Cells: NewSnapshotCells()}) // deliberately malformed
		tr.Restore(nil)
		short := NewSnapshotCells()
		short.Set("m1", [][]float64{{1, 2}}) // wrong band count -> skipped by length guard
		tr.Restore(&CapabilitySnapshot{Version: 1, Cells: short})
		if got := tr.MaxReliableBand(highModel); got == "" {
			t.Fatalf("maxReliableBand returned an empty band")
		}
	})

	t.Run("snapshot round-trips through JSON", func(t *testing.T) {
		a := NewCapabilityTracker(tiers())
		feed(a, "gemma-4-26b", sizeband.BandM, VerdictPass, 3)
		feed(a, "qwen3.6-plus", sizeband.BandXS, VerdictEscalated, 2)
		snapA := a.Snapshot()

		encoded := mustJSON(t, snapA)
		var decoded CapabilitySnapshot
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		b := NewCapabilityTracker(tiers())
		b.Restore(&decoded)
		if got := mustJSON(t, b.Snapshot()); got != encoded {
			t.Fatalf("round trip drifted\n want %s\n  got %s", encoded, got)
		}
	})
}
