package adaptive

// Translation of src/router/adaptive.test.ts. Subtest names are verbatim, so a
// failure here names the same case the bun suite would name.
//
// The TS suite awaits `router.pick(...)`; none of these scenarios can actually
// block (they either have an available candidate or relax structurally), so the
// Go twin installs a fake sleeper that ADVANCES the fake clock. A regression
// that made pick() block therefore fails fast instead of hanging the suite.

import (
	"math"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// cand mirrors the `cand()` helper at the top of adaptive.test.ts.
func cand(id string, tier ...ModelTier) ModelCandidate {
	t := ModelTierHigh
	if len(tier) > 0 {
		t = tier[0]
	}
	return ModelCandidate{
		ID:                   id,
		Tier:                 t,
		Family:               DeriveFamily(id),
		PromptUSDPerMtok:     1,
		CompletionUSDPerMtok: 1,
		Priority:             0,
	}
}

func seed(v float64) *float64 { return &v }

// pinRuntime freezes Date.now and turns the pick() backoff into a clock
// advance, so no test can wall-clock sleep.
func pinRuntime(t *testing.T) {
	t.Helper()
	now := 1_700_000_000_000.0
	t.Cleanup(SetClockForTesting(func() float64 { return now }))
	t.Cleanup(SetSleeperForTesting(func(ms float64, _ *AbortSignal) { now += ms }))
}

func mustPick(t *testing.T, r *AdaptiveModelRouter, slot string, tier ModelTier, opts ...PickOptions) RouteChoice {
	t.Helper()
	choice, err := r.Pick(slot, tier, opts...)
	if err != nil {
		t.Fatalf("pick(%s, %s): %v", slot, tier, err)
	}
	return choice
}

func TestDeriveFamily(t *testing.T) {
	t.Run("extracts vendor from openrouter id", func(t *testing.T) {
		if got := DeriveFamily("openrouter/qwen/qwen3.6-plus"); got != "qwen" {
			t.Errorf("got %q, want %q", got, "qwen")
		}
		if got := DeriveFamily("openrouter/deepseek/deepseek-v4-pro"); got != "deepseek" {
			t.Errorf("got %q, want %q", got, "deepseek")
		}
		if got := DeriveFamily("openrouter/moonshotai/kimi-k2.5"); got != "moonshotai" {
			t.Errorf("got %q, want %q", got, "moonshotai")
		}
	})
	t.Run("falls back gracefully for short ids", func(t *testing.T) {
		if got := DeriveFamily("vendor/model"); got != "vendor" {
			t.Errorf("got %q, want %q", got, "vendor")
		}
		if got := DeriveFamily("solo"); got != "solo" {
			t.Errorf("got %q, want %q", got, "solo")
		}
		if got := DeriveFamily(""); got != "unknown" {
			t.Errorf("got %q, want %q", got, "unknown")
		}
	})
}

func TestAdaptiveModelRouterCrossRoleFamilyExclusion(t *testing.T) {
	t.Run("Prover avoids Investigator's family when both families available", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{
				cand("openrouter/qwen/qwen-a"),
				cand("openrouter/deepseek/deepseek-a"),
				cand("openrouter/moonshotai/kimi-a"),
			},
			RandomSeed: seed(42),
		})

		invChoice := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(invChoice, 1, 100, nil)
		invFamily := invChoice.Candidate.Family

		provChoice := mustPick(t, router, "review-prover", ModelTierHigh)
		if provChoice.Candidate.Family == invFamily {
			t.Errorf("prover reused the investigator's family %q", invFamily)
		}
	})

	t.Run("Investigator (no adversary declared) can use any family", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
			RandomSeed: seed(7),
		})
		choice := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		if f := choice.Candidate.Family; f != "qwen" && f != "deepseek" {
			t.Errorf("family %q is neither qwen nor deepseek", f)
		}
	})
}

func TestAdaptiveModelRouterSingleModelSingleFamilyConfigs(t *testing.T) {
	t.Run("Prover with single model in pool relaxes (doesn't block)", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-only")},
			RandomSeed: seed(1),
		})
		inv := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(inv, 1, 100, nil)
		if inv.Candidate.Family != "qwen" {
			t.Fatalf("investigator family = %q, want qwen", inv.Candidate.Family)
		}

		timeout := 2000.0
		prov := mustPick(t, router, "review-prover", ModelTierHigh, PickOptions{TimeoutMs: &timeout})
		if prov.Candidate.Family != "qwen" {
			t.Errorf("prover family = %q, want qwen", prov.Candidate.Family)
		}
		if !strings.Contains(prov.Reason, "constraint-relaxed") {
			t.Errorf("reason %q does not match /constraint-relaxed/", prov.Reason)
		}
	})

	t.Run("Prover with single-family-only pool relaxes", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{
				cand("openrouter/qwen/qwen-a"), cand("openrouter/qwen/qwen-b"), cand("openrouter/qwen/qwen-c"),
			},
			RandomSeed: seed(2),
		})
		inv := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(inv, 1, 100, nil)

		timeout := 2000.0
		prov := mustPick(t, router, "review-prover", ModelTierHigh, PickOptions{TimeoutMs: &timeout})
		if prov.Candidate.Family != "qwen" {
			t.Errorf("prover family = %q, want qwen", prov.Candidate.Family)
		}
		if !strings.Contains(prov.Reason, "constraint-relaxed") {
			t.Errorf("reason %q does not match /constraint-relaxed/", prov.Reason)
		}
	})

	t.Run("Prover with two families respects constraint (no relax)", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
			RandomSeed: seed(3),
		})
		inv := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(inv, 1, 100, nil)

		timeout := 2000.0
		prov := mustPick(t, router, "review-prover", ModelTierHigh, PickOptions{TimeoutMs: &timeout})
		if prov.Candidate.Family == inv.Candidate.Family {
			t.Errorf("prover reused family %q", inv.Candidate.Family)
		}
		if strings.Contains(prov.Reason, "constraint-relaxed") {
			t.Errorf("reason %q unexpectedly matches /constraint-relaxed/", prov.Reason)
		}
	})
}

func TestAdaptiveModelRouterCandidatesForTierFiltered(t *testing.T) {
	t.Run("respects ROLE_ADVERSARIES for seed selection", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
			RandomSeed: seed(4),
		})
		inv := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(inv, 1, 100, nil)

		filteredForProver := router.CandidatesForTierFiltered(ModelTierHigh, "review-prover")
		for _, c := range filteredForProver {
			if c.Family == inv.Candidate.Family {
				t.Errorf("filtered pool still contains family %q", c.Family)
			}
		}
		if len(filteredForProver) != 1 {
			t.Errorf("len = %d, want 1", len(filteredForProver))
		}
	})

	t.Run("falls back to full pool when constraint structurally unsatisfiable", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-only")},
		})
		inv := mustPick(t, router, "review-lens-investigator", ModelTierHigh)
		router.Register(inv, 1, 100, nil)

		filteredForProver := router.CandidatesForTierFiltered(ModelTierHigh, "review-prover")
		if len(filteredForProver) != 1 { // soft-fallback to full pool
			t.Fatalf("len = %d, want 1", len(filteredForProver))
		}
		if filteredForProver[0].Family != "qwen" {
			t.Errorf("family = %q, want qwen", filteredForProver[0].Family)
		}
	})

	t.Run("returns full pool for roles without declared adversaries", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
		})
		filtered := router.CandidatesForTierFiltered(ModelTierHigh, "review-anatomist")
		if len(filtered) != 2 {
			t.Errorf("len = %d, want 2", len(filtered))
		}
	})
}

func TestROLEADVERSARIESTable(t *testing.T) {
	t.Run("review-prover declares review-lens-investigator as adversary", func(t *testing.T) {
		got := RoleAdversaries["review-prover"]
		if len(got) != 1 || got[0] != "review-lens-investigator" {
			t.Errorf("got %v, want [review-lens-investigator]", got)
		}
	})
}

func TestW8aFRONTIERTierResolutionFallback(t *testing.T) {
	t.Run("effectiveTier: frontier falls back to HIGH when the frontier pool is empty", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a")},
			// no frontier_models configured
		})
		if got := router.EffectiveTier(ModelTierFrontier); got != ModelTierHigh {
			t.Errorf("effectiveTier(frontier) = %q, want %q", got, ModelTierHigh)
		}
		if n := len(router.CandidatesForTier(ModelTierFrontier)); n != 0 {
			t.Errorf("frontier pool size = %d, want 0", n)
		}
	})

	t.Run("effectiveTier: frontier stays FRONTIER when the pool is populated", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels:     []ModelCandidate{cand("openrouter/qwen/qwen-a")},
			FrontierModels: []ModelCandidate{cand("openrouter/anthropic/claude-opus-4.8", ModelTierFrontier)},
		})
		if got := router.EffectiveTier(ModelTierFrontier); got != ModelTierFrontier {
			t.Errorf("effectiveTier(frontier) = %q, want %q", got, ModelTierFrontier)
		}
		c := router.CandidatesForTier(ModelTierFrontier)
		if len(c) != 1 {
			t.Fatalf("frontier pool size = %d, want 1", len(c))
		}
		if c[0].Family != "anthropic" {
			t.Errorf("family = %q, want anthropic", c[0].Family)
		}
	})

	t.Run("effectiveTier: high and low are always themselves", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{})
		if got := router.EffectiveTier(ModelTierHigh); got != ModelTierHigh {
			t.Errorf("effectiveTier(high) = %q", got)
		}
		if got := router.EffectiveTier(ModelTierLow); got != ModelTierLow {
			t.Errorf("effectiveTier(low) = %q", got)
		}
	})

	t.Run("pick routes within the frontier pool when populated", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			FrontierModels: []ModelCandidate{
				cand("openrouter/anthropic/claude-opus-4.8", ModelTierFrontier),
				cand("openrouter/anthropic/claude-sonnet-5", ModelTierFrontier),
			},
			RandomSeed: seed(3),
		})
		choice := mustPick(t, router, "adjudicator", ModelTierFrontier)
		if !strings.HasPrefix(choice.Candidate.ID, "openrouter/anthropic/") {
			t.Errorf("id %q is outside the frontier pool", choice.Candidate.ID)
		}
	})
}

// ── kept-bug pins (no TS counterpart; these lock the quirks in place) ─────

func TestKeptTSQuirks(t *testing.T) {
	t.Run("inflight leaks when register is skipped", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/only")},
			RandomSeed: seed(31),
		})
		for i := 0; i < 4; i++ {
			if r := router.TryPick("s", ModelTierHigh); !r.Ok {
				t.Fatalf("tryPick %d not ok", i)
			}
		}
		// Four picks, one register: three in-flight slots are leaked forever
		// and each costs 0.12 of score through pressurePen.
		st := router.statsFor("s", cand("openrouter/qwen/only"))
		if st.Inflight != 4 {
			t.Fatalf("inflight = %v, want 4", st.Inflight)
		}
		router.Register(RouteChoice{Slot: "s", Candidate: cand("openrouter/qwen/only")}, 1, 10, nil)
		if st.Inflight != 3 {
			t.Errorf("inflight after one register = %v, want 3 (the leak)", st.Inflight)
		}
		// register never drives it below zero, so the leak is one-directional.
		for i := 0; i < 10; i++ {
			router.Register(RouteChoice{Slot: "s", Candidate: cand("openrouter/qwen/only")}, 1, 10, nil)
		}
		if st.Inflight != 0 {
			t.Errorf("inflight = %v, want 0 (clamped, never negative)", st.Inflight)
		}
	})

	t.Run("statsKey merges the openrouter-prefixed and bare spellings", func(t *testing.T) {
		prefixed := cand("openrouter/qwen/dup")
		bare := cand("qwen/dup")
		if statsKey("s", prefixed) != statsKey("s", bare) {
			t.Errorf("keys differ: %q vs %q", statsKey("s", prefixed), statsKey("s", bare))
		}
		if got := statsKey(" s ", prefixed); got != "s:qwen/dup" {
			t.Errorf("statsKey = %q, want %q", got, "s:qwen/dup")
		}
		// Two DIFFERENT candidates therefore share one stats row: cooling one
		// cools the other.
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{prefixed, bare},
			RandomSeed: seed(33),
		})
		first := router.TryPick("s", ModelTierHigh)
		router.Register(first.Choice, 1, 1, Err("429 rate limit"))
		second := router.TryPick("s", ModelTierHigh)
		if second.Ok {
			t.Errorf("expected all-acceptable-busy, got a pick of %q", second.Choice.Candidate.ID)
		}
		if second.Reason != "all-acceptable-busy" {
			t.Errorf("reason = %q", second.Reason)
		}
	})

	t.Run("all-NaN scores leave the choice at -Infinity", func(t *testing.T) {
		pinRuntime(t)
		nan := ModelCandidate{
			ID: "openrouter/qwen/nanprice", Tier: ModelTierHigh, Family: "qwen",
			PromptUSDPerMtok:     jscompat.JSNumber(math.NaN()),
			CompletionUSDPerMtok: 1,
		}
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{nan},
			RandomSeed: seed(37),
		})
		result := router.TryPick("s", ModelTierHigh)
		if !result.Ok {
			t.Fatalf("tryPick not ok")
		}
		if !math.IsInf(float64(result.Choice.Score), -1) {
			t.Errorf("score = %v, want -Inf (NaN > -Infinity is false in both passes)", result.Choice.Score)
		}
		encoded, err := jscompat.Stringify(result.Choice.Score)
		if err != nil || string(encoded) != "null" {
			t.Errorf("stringify(score) = %s (%v), want null", encoded, err)
		}
	})

	t.Run("blank ids are skipped in the main pass but not the forced pass", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("  "), cand("")},
			RandomSeed: seed(36),
		})
		result := router.TryPick("s", ModelTierHigh)
		if !result.Ok {
			t.Fatalf("tryPick not ok")
		}
		if result.Choice.Reason != "all-cooling" {
			t.Errorf("reason = %q, want all-cooling", result.Choice.Reason)
		}
		if jscompat.Trim(result.Choice.Candidate.ID) != "" {
			t.Errorf("expected a blank-id candidate, got %q", result.Choice.Candidate.ID)
		}
	})

	t.Run("empty pool installs the hard-coded gpt-oss-120b fallback", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			FrontierModels: []ModelCandidate{},
			RandomSeed:     seed(2),
		})
		if n := len(router.CandidatesForTier(ModelTierFrontier)); n != 0 {
			t.Fatalf("frontier pool size = %d, want 0", n)
		}
		result := router.TryPick("adjudicator", ModelTierFrontier)
		if !result.Ok {
			t.Fatalf("tryPick not ok")
		}
		if result.Choice.Candidate.ID != "openrouter/openai/gpt-oss-120b" {
			t.Errorf("id = %q", result.Choice.Candidate.ID)
		}
		if result.Choice.Tier != ModelTierFrontier {
			t.Errorf("tier = %q, want frontier (the fallback keeps the REQUESTED tier)", result.Choice.Tier)
		}
	})
}

func TestPickTimeoutAndAbort(t *testing.T) {
	t.Run("timeout carries the excluded families", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
			RandomSeed: seed(22),
		})
		// Cool BOTH models for the prover slot for an hour.
		for i := 0; i < 2; i++ {
			r := router.TryPick("review-prover", ModelTierHigh)
			router.Register(r.Choice, 1, 1, Err("No endpoints found for this model"))
		}
		// Let the investigator claim a family so the prover's acceptable set is
		// non-empty (not structurally relaxed) but entirely cooling.
		router.TryPick("review-lens-investigator", ModelTierHigh)

		timeout := 5000.0
		_, err := router.Pick("review-prover", ModelTierHigh, PickOptions{TimeoutMs: &timeout})
		if err == nil {
			t.Fatalf("expected a timeout error")
		}
		want := "router.pick timeout after 5000ms (all-acceptable-busy) — slot=review-prover tier=high excludedFamilies=["
		if !strings.HasPrefix(err.Error(), want) {
			t.Errorf("error = %q,\n want prefix %q", err.Error(), want)
		}
	})

	t.Run("abort before the first tryPick throws immediately", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a")},
			RandomSeed: seed(23),
		})
		signal := NewAbortSignal()
		signal.Abort()
		_, err := router.Pick("s", ModelTierHigh, PickOptions{Signal: signal})
		if err == nil || err.Error() != "router.pick aborted for slot=s tier=high" {
			t.Errorf("error = %v", err)
		}
	})
}

func TestParseModelListShapes(t *testing.T) {
	raw := "openrouter/qwen/a@0.325/1.95, ,openrouter/deepseek/b"
	got := ParseModelList(&raw, ModelTierLow)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].PromptUSDPerMtok != 0.325 || got[0].CompletionUSDPerMtok != 1.95 || got[0].Priority != 0 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "openrouter/deepseek/b" || got[1].Family != "deepseek" || got[1].Priority != 1 {
		t.Errorf("second = %+v", got[1])
	}
	if got[1].PromptUSDPerMtok != 0 || got[1].CompletionUSDPerMtok != 0 {
		t.Errorf("unpriced entry should default to 0/0, got %+v", got[1])
	}
	// `if (!raw)` treats the empty string like undefined.
	empty := ""
	if n := len(ParseModelList(&empty, ModelTierHigh)); n != 0 {
		t.Errorf("empty string produced %d candidates", n)
	}
	if n := len(ParseModelList(nil, ModelTierHigh)); n != 0 {
		t.Errorf("nil produced %d candidates", n)
	}
}
