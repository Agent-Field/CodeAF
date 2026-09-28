//go:build !windows

package adaptive

import "testing"

// tierRouter builds a router whose pools are exactly what is passed: the
// helper never falls back to the shipped defaults, so an empty pool here
// really is empty.
func tierRouter(t *testing.T, high, low, frontier []string) *AdaptiveModelRouter {
	t.Helper()
	pool := func(ids []string) []ModelCandidate {
		out := make([]ModelCandidate, 0, len(ids))
		for _, id := range ids {
			out = append(out, cand(id))
		}
		return out
	}
	return NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels:     pool(high),
		LowModels:      pool(low),
		FrontierModels: pool(frontier),
		RandomSeed:     seed(5),
	})
}

func TestEmptyTierPoolsResolveToHigh(t *testing.T) {
	pinRuntime(t)
	router := tierRouter(t, []string{"openrouter/qwen/high-a"}, nil, nil)
	for _, tier := range []ModelTier{ModelTierLow, ModelTierFrontier, ModelTierHigh} {
		if got := router.EffectiveTier(tier); got != ModelTierHigh {
			t.Errorf("EffectiveTier(%q) = %q, want %q", tier, got, ModelTierHigh)
		}
		pool := router.CandidatesForTier(tier)
		if len(pool) != 1 || pool[0].ID != "openrouter/qwen/high-a" {
			t.Errorf("CandidatesForTier(%q) = %+v, want the high pool", tier, pool)
		}
	}
	if got := router.EffectiveTier("platinum"); got != ModelTierHigh {
		t.Errorf("EffectiveTier(unknown) = %q, want %q", got, ModelTierHigh)
	}
}

func TestAConfiguredTierPoolStandsOnItsOwn(t *testing.T) {
	pinRuntime(t)
	router := tierRouter(t,
		[]string{"openrouter/qwen/high-a"},
		[]string{"openrouter/qwen/low-a"},
		[]string{"openrouter/anthropic/frontier-a"},
	)
	for tier, want := range map[ModelTier]string{
		ModelTierHigh:     "openrouter/qwen/high-a",
		ModelTierLow:      "openrouter/qwen/low-a",
		ModelTierFrontier: "openrouter/anthropic/frontier-a",
	} {
		if got := router.EffectiveTier(tier); got != tier {
			t.Errorf("EffectiveTier(%q) = %q, want it unchanged", tier, got)
		}
		choice := router.PickSync("slot", tier)
		if choice.Candidate.ID != want {
			t.Errorf("PickSync(%q) = %q, want %q", tier, choice.Candidate.ID, want)
		}
		if choice.Tier != tier {
			t.Errorf("choice tier = %q, want %q", choice.Tier, tier)
		}
		router.Register(choice, 1, 10, nil)
	}
}

func TestOneModelHighPoolServesEveryTier(t *testing.T) {
	pinRuntime(t)
	// The trivial configuration: `--high m` and nothing else.
	router := tierRouter(t, []string{"openrouter/qwen/only"}, nil, nil)
	for _, tier := range []ModelTier{ModelTierHigh, ModelTierLow, ModelTierFrontier} {
		choice := router.PickSync("slot", tier)
		if choice.Candidate.ID != "openrouter/qwen/only" {
			t.Fatalf("PickSync(%q) = %q", tier, choice.Candidate.ID)
		}
		// The degradation happens before anything reads the tier, so the
		// choice and its event report the tier actually routed on.
		if choice.Tier != ModelTierHigh {
			t.Fatalf("choice tier = %q, want %q", choice.Tier, ModelTierHigh)
		}
		event := router.Register(choice, 1, 10, nil)
		if event.Tier != ModelTierHigh || event.Model != "openrouter/qwen/only" {
			t.Fatalf("event = %+v", event)
		}
	}
}

func TestDegradedTierIsScoredAsHigh(t *testing.T) {
	pinRuntime(t)
	// Two models that the two weightings rank differently: the cheap, fast
	// one wins on LOW, the expensive, reliable one wins on HIGH. With no low
	// pool configured, a LOW request must score exactly as a HIGH request
	// does, down to the value.
	slow := ModelCandidate{ID: "openrouter/qwen/slow", PromptUSDPerMtok: 9, CompletionUSDPerMtok: 9}
	fast := ModelCandidate{ID: "openrouter/qwen/fast", PromptUSDPerMtok: 0.01, CompletionUSDPerMtok: 0.01}
	build := func(low []ModelCandidate) *AdaptiveModelRouter {
		return NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{slow, fast},
			LowModels:  low,
			RandomSeed: seed(11),
		})
	}
	degraded := build(nil).PickSync("slot", ModelTierLow)
	asHigh := build(nil).PickSync("slot", ModelTierHigh)
	if degraded.Candidate.ID != asHigh.Candidate.ID || degraded.Score != asHigh.Score {
		t.Fatalf("degraded low pick %+v differs from the high pick %+v", degraded, asHigh)
	}
	// With a pool of its own, LOW scores on its own weighting.
	configured := build([]ModelCandidate{slow, fast}).PickSync("slot", ModelTierLow)
	if configured.Tier != ModelTierLow {
		t.Fatalf("choice tier = %q, want %q", configured.Tier, ModelTierLow)
	}
	if configured.Score == asHigh.Score {
		t.Fatalf("the low weighting produced the high score %v", configured.Score)
	}
}

func TestParseModelListStampsTheTier(t *testing.T) {
	raw := "openrouter/qwen/a,openrouter/qwen/b"
	for _, tier := range []ModelTier{ModelTierHigh, ModelTierLow, ModelTierFrontier} {
		for _, candidate := range ParseModelList(&raw, tier) {
			if candidate.Tier != tier {
				t.Errorf("%q parsed with tier %q, want %q", candidate.ID, candidate.Tier, tier)
			}
		}
	}
}
