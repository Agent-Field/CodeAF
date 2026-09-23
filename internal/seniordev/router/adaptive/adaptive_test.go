//go:build !windows

package adaptive

// These tests install a fake sleeper that advances a fake clock, so no test
// can wall-clock sleep: a regression that made Pick block fails fast instead
// of hanging the suite.

import (
	"errors"
	"testing"
)

func cand(id string) ModelCandidate {
	return ModelCandidate{
		ID:                   id,
		PromptUSDPerMtok:     1,
		CompletionUSDPerMtok: 1,
		Priority:             0,
	}
}

func seed(v float64) *float64 { return &v }

// pinRuntime freezes the clock and turns the Pick backoff into a clock advance.
func pinRuntime(t *testing.T) {
	t.Helper()
	now := 1_700_000_000_000.0
	t.Cleanup(SetClockForTesting(func() float64 { return now }))
	t.Cleanup(SetSleeperForTesting(func(ms float64, _ *AbortSignal) { now += ms }))
}

func mustPick(
	t *testing.T, r *AdaptiveModelRouter, slot string, tier ModelTier,
	opts ...PickOptions,
) RouteChoice {
	t.Helper()
	choice, err := r.Pick(slot, tier, opts...)
	if err != nil {
		t.Fatalf("pick(%s): %v", slot, err)
	}
	return choice
}

func TestPickTimeoutAndAbort(t *testing.T) {
	t.Run("timeout names the reason and the slot", func(t *testing.T) {
		pinRuntime(t)
		router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/qwen-a"), cand("openrouter/deepseek/deepseek-a")},
			RandomSeed: seed(22),
		})
		// Cool BOTH models for the slot for an hour.
		for i := 0; i < 2; i++ {
			r := router.TryPick("coder", ModelTierHigh)
			router.Register(r.Choice, 1, 1, errors.New("No endpoints found for this model"))
		}

		timeout := 5000.0
		_, err := router.Pick("coder", ModelTierHigh, PickOptions{TimeoutMs: &timeout})
		if err == nil {
			t.Fatalf("expected a timeout error")
		}
		want := "router.pick timeout after 5000ms (all-busy) — slot=coder tier=high"
		if err.Error() != want {
			t.Errorf("error = %q,\n want %q", err.Error(), want)
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
	got := ParseModelList(&raw, ModelTierHigh)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].PromptUSDPerMtok != 0.325 || got[0].CompletionUSDPerMtok != 1.95 || got[0].Priority != 0 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "openrouter/deepseek/b" || got[1].Priority != 1 {
		t.Errorf("second = %+v", got[1])
	}
	if got[1].PromptUSDPerMtok != 0 || got[1].CompletionUSDPerMtok != 0 {
		t.Errorf("unpriced entry should default to 0/0, got %+v", got[1])
	}
	// An empty string yields no candidates, the same as nil.
	empty := ""
	if n := len(ParseModelList(&empty, ModelTierHigh)); n != 0 {
		t.Errorf("empty string produced %d candidates", n)
	}
	if n := len(ParseModelList(nil, ModelTierHigh)); n != 0 {
		t.Errorf("nil produced %d candidates", n)
	}
}
