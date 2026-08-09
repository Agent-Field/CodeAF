package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePools struct {
	mu    sync.Mutex
	pools map[ModelTier][]ModelCandidate
	calls []ModelTier
}

func (f *fakePools) CandidatesForTier(tier ModelTier) []ModelCandidate {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tier)
	return append([]ModelCandidate(nil), f.pools[tier]...)
}

type fakeProvider struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeProvider) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeProvider) GetModel(ctx context.Context, providerID, modelID string) (any, error) {
	f.record("model:" + providerID + "/" + modelID)
	switch modelID {
	case "hang":
		<-ctx.Done()
		return nil, ctx.Err()
	case "fail":
		return nil, errors.New("provider unavailable")
	default:
		return providerID + "/" + modelID, nil
	}
}

func (f *fakeProvider) GetLanguage(_ context.Context, model any) (any, error) {
	f.record("language:" + model.(string))
	return model, nil
}

func TestPrewarmTierPoolsSequentialTimeoutAndStickyFailure(t *testing.T) {
	schedulerWarmedTiers = newWarmedTierRegistry()
	pools := &fakePools{pools: map[ModelTier][]ModelCandidate{
		ModelTierHigh: {
			{ID: "openrouter/first"},
			{ID: "invalid"},
			{ID: "openrouter/hang"},
			{ID: "openrouter/last"},
		},
		ModelTierLow: {
			{ID: "openrouter/fail"},
		},
	}}
	provider := &fakeProvider{}

	start := time.Now()
	prewarmTierPools(
		context.Background(),
		[]ModelTier{ModelTierHigh, ModelTierHigh, ModelTierLow},
		pools,
		provider,
		prewarmOptions{Timeout: 20 * time.Millisecond},
	)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("prewarm took %v", elapsed)
	}
	wantEvents := []string{
		"model:openrouter/first",
		"language:openrouter/first",
		"model:openrouter/hang",
		"model:openrouter/last",
		"language:openrouter/last",
		"model:openrouter/fail",
	}
	if len(provider.events) != len(wantEvents) {
		t.Fatalf("events = %#v", provider.events)
	}
	for i := range wantEvents {
		if provider.events[i] != wantEvents[i] {
			t.Fatalf("event %d = %q, want %q; all=%#v", i, provider.events[i], wantEvents[i], provider.events)
		}
	}
	if len(pools.calls) != 2 || pools.calls[0] != ModelTierHigh || pools.calls[1] != ModelTierLow {
		t.Fatalf("pool calls = %#v", pools.calls)
	}
	if !schedulerWarmedTiers.has(ModelTierHigh) || !schedulerWarmedTiers.has(ModelTierLow) {
		t.Fatal("tiers were not marked warmed")
	}

	// Both the timed-out HIGH pool and failed LOW pool are sticky-success.
	prewarmTierPools(
		context.Background(),
		[]ModelTier{ModelTierLow, ModelTierHigh},
		pools,
		provider,
		prewarmOptions{Timeout: 20 * time.Millisecond},
	)
	if len(provider.events) != len(wantEvents) || len(pools.calls) != 2 {
		t.Fatalf("sticky warm retried: events=%#v pools=%#v", provider.events, pools.calls)
	}
}

func TestPrewarmTierPoolsMarksBeforeConcurrentWork(t *testing.T) {
	schedulerWarmedTiers = newWarmedTierRegistry()
	pools := &fakePools{pools: map[ModelTier][]ModelCandidate{
		ModelTierHigh: {{ID: "openrouter/one"}},
	}}
	provider := &fakeProvider{}
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			prewarmTierPools(context.Background(), []ModelTier{ModelTierHigh}, pools, provider)
		}()
	}
	wait.Wait()
	if len(pools.calls) != 1 {
		t.Fatalf("CandidatesForTier called %d times, want 1", len(pools.calls))
	}
}

func TestSplitModelIDUsesFirstSlash(t *testing.T) {
	provider, model, ok := splitModelID("openrouter/qwen/model")
	if !ok || provider != "openrouter" || model != "qwen/model" {
		t.Fatalf("split = %q %q %v", provider, model, ok)
	}
	for _, invalid := range []string{"", "plain", "/leading"} {
		if _, _, ok := splitModelID(invalid); ok {
			t.Fatalf("splitModelID(%q) succeeded", invalid)
		}
	}
}
