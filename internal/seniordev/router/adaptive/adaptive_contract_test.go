//go:build !windows

package adaptive

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

type statusErr struct {
	msg    string
	status float64
}

func (e statusErr) Error() string                    { return e.msg }
func (e statusErr) ErrorStatusCode() (float64, bool) { return e.status, true }

func TestRegisterOutcomesDriveCooldownsAndStats(t *testing.T) {
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{cand("openrouter/qwen/only")},
		RandomSeed: seed(5),
	})
	first := router.TryPick("s", ModelTierHigh)
	if !first.Ok {
		t.Fatal("first pick must succeed")
	}
	event := router.Register(first.Choice, 2, 100, nil)
	if event.Attempts != 1 || event.Successes != 1 || event.Failures != 0 || event.Error != "" {
		t.Fatalf("success event = %+v", event)
	}
	if event.LatencyEwma != 2 || event.ToksecEwma != 50 {
		t.Fatalf("ewma after one sample = %+v", event)
	}

	second := router.TryPick("s", ModelTierHigh)
	event = router.Register(second.Choice, 1, 0, statusErr{msg: "Too Many Requests", status: 429})
	if event.Failures != 1 || event.RateLimits != 1 || !strings.Contains(event.Error, "Too Many Requests") {
		t.Fatalf("rate-limit event = %+v", event)
	}
	// The only candidate is now cooling, so a non-blocking pick reports busy.
	third := router.TryPick("s", ModelTierHigh)
	if third.Ok || third.Reason != "all-busy" || third.RetryAfterMs <= 0 {
		t.Fatalf("pick during cooldown = %+v", third)
	}
	// PickSync forces a choice anyway and says so.
	forced := router.PickSync("s", ModelTierHigh)
	if !strings.Contains(forced.Reason, "all-cooling") {
		t.Fatalf("forced reason = %q", forced.Reason)
	}
	router.Register(forced, 1, 1, nil)
	if st := router.statsFor("s", forced.Candidate); st.Inflight != 0 {
		t.Fatalf("inflight after settling every lease = %v", st.Inflight)
	}
}

func TestClassifiersReadStatusNameAndDetail(t *testing.T) {
	if !IsLikelyRateLimit(statusErr{msg: "nope", status: 429}) {
		t.Error("a 429 status is a rate limit")
	}
	if !IsLikelyTransientProviderError(statusErr{msg: "nope", status: 503}) {
		t.Error("a 5xx status is transient")
	}
	if !IsLikelyTimeout(errors.New("SSE read timed out")) {
		t.Error("timed out is a timeout")
	}
	if !IsLikelyProviderIncompatible(errors.New("No endpoints found for this model")) {
		t.Error("no endpoints is an incompatibility")
	}
	if IsRetryableRouteError(errors.New("the model refused")) {
		t.Error("an ordinary failure is not retryable")
	}
	if IsRetryableRouteError(nil) {
		t.Error("nil is not an error")
	}
}

func TestBlankCandidateIDsAreDropped(t *testing.T) {
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{cand("  "), cand("openrouter/qwen/real"), cand("")},
	})
	pool := router.CandidatesForTier(ModelTierHigh)
	if len(pool) != 1 || pool[0].ID != "openrouter/qwen/real" || pool[0].Priority != 0 {
		t.Fatalf("pool = %+v", pool)
	}
}

func TestNaNPriceStillYieldsAFiniteScore(t *testing.T) {
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{{ID: "openrouter/qwen/nanprice", PromptUSDPerMtok: math.NaN()}},
		RandomSeed: seed(37),
	})
	result := router.TryPick("s", ModelTierHigh)
	if !result.Ok || math.IsNaN(result.Choice.Score) || math.IsInf(result.Choice.Score, 0) {
		t.Fatalf("pick = %+v", result)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("a pick result must always be encodable: %v", err)
	}
}

func TestEmptyPoolInstallsTheFallbackCandidate(t *testing.T) {
	pinRuntime(t)
	// A pool whose every entry is blank normalizes to nothing, which is the
	// only way to reach the router with no candidates at all: an unset pool
	// takes the defaults instead.
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{HighModels: []ModelCandidate{cand("  ")}})
	result := router.TryPick("coder", ModelTierHigh)
	if !result.Ok || result.Choice.Candidate.ID != "openrouter/openai/gpt-oss-120b" {
		t.Fatalf("fallback pick = %+v", result)
	}
}

func TestSeededRoutersAreReproducible(t *testing.T) {
	pinRuntime(t)
	build := func() *AdaptiveModelRouter {
		return NewAdaptiveModelRouter(AdaptiveRouterConfig{
			HighModels: []ModelCandidate{cand("openrouter/qwen/a"), cand("openrouter/deepseek/b"), cand("openrouter/moonshotai/c")},
			RandomSeed: seed(99),
		})
	}
	a, b := build(), build()
	for i := 0; i < 5; i++ {
		ca := a.PickSync("s", ModelTierHigh)
		cb := b.PickSync("s", ModelTierHigh)
		if ca.Candidate.ID != cb.Candidate.ID || ca.Score != cb.Score {
			t.Fatalf("pick %d diverged: %+v vs %+v", i, ca, cb)
		}
		a.Register(ca, 1, 10, nil)
		b.Register(cb, 1, 10, nil)
	}
}

func TestRouteChoiceRoundTripsThroughJSON(t *testing.T) {
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{cand("openrouter/qwen/a@x")},
	})
	choice := router.PickSync("s", ModelTierHigh)
	encoded, err := json.Marshal(choice)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RouteChoice
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != choice {
		t.Fatalf("round trip changed the choice:\n %+v\n %+v", choice, decoded)
	}
	busy := TryPickResult{Reason: "all-busy", RetryAfterMs: 100}
	encoded, err = json.Marshal(busy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"choice"`) {
		t.Fatalf("a busy result must not carry an empty choice: %s", encoded)
	}
}

func TestParseModelListPricesAreOptional(t *testing.T) {
	raw := "openrouter/qwen/a@0.325/1.95,openrouter/deepseek/b@bogus/2,openrouter/moonshotai/c"
	got := ParseModelList(&raw, ModelTierHigh)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].PromptUSDPerMtok != 0.325 || got[0].CompletionUSDPerMtok != 1.95 {
		t.Fatalf("priced entry = %+v", got[0])
	}
	if got[1].PromptUSDPerMtok != 0 || got[1].CompletionUSDPerMtok != 2 || got[1].ID != "openrouter/deepseek/b" {
		t.Fatalf("partially priced entry = %+v", got[1])
	}
	if got[2].Priority != 2 || got[2].ID != "openrouter/moonshotai/c" {
		t.Fatalf("unpriced entry = %+v", got[2])
	}
}
