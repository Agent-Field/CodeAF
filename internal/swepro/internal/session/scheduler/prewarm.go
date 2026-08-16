// This file ports src/session/plandb-scheduler.ts:2097-2132 from swe-pro
// (commit 3b25a1a).
package scheduler

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

const modelPrewarmTimeout = 3 * time.Second

var schedulerLog = logshim.Create(map[string]any{"service": "session.scheduler"})

type warmedTierRegistry struct {
	mu     sync.Mutex
	warmed map[ModelTier]struct{}
}

func newWarmedTierRegistry() *warmedTierRegistry {
	return &warmedTierRegistry{warmed: map[ModelTier]struct{}{}}
}

// markIfCold combines the source's has+add pair into one critical section.
// The mark deliberately precedes all pool/provider work: failure is sticky.
func (r *warmedTierRegistry) markIfCold(tier ModelTier) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.warmed[tier]; ok {
		return false
	}
	r.warmed[tier] = struct{}{}
	return true
}

func (r *warmedTierRegistry) has(tier ModelTier) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.warmed[tier]
	return ok
}

var schedulerWarmedTiers = newWarmedTierRegistry()

type prewarmOptions struct {
	Timeout time.Duration
}

// prewarmTierPools performs the source's one-shot, sequential pool warm. Each
// provider operation gets one shared per-model deadline. Errors and timeouts
// are swallowed; malformed model IDs are skipped; and a tier is never retried
// once its first caller marks it.
func prewarmTierPools(
	ctx context.Context,
	needed []ModelTier,
	pools modelPoolResolver,
	provider providerResolver,
	options ...prewarmOptions,
) {
	timeout := modelPrewarmTimeout
	if len(options) > 0 && options[0].Timeout != 0 {
		timeout = options[0].Timeout
	}

	// `new Set(needed)` preserves first occurrence order.
	tiers := make([]ModelTier, 0, len(needed))
	seen := map[ModelTier]struct{}{}
	for _, tier := range needed {
		if _, ok := seen[tier]; ok {
			continue
		}
		seen[tier] = struct{}{}
		tiers = append(tiers, tier)
	}

	for _, tier := range tiers {
		if !schedulerWarmedTiers.markIfCold(tier) {
			continue
		}
		for _, candidate := range pools.CandidatesForTier(tier) {
			providerID, modelID, ok := splitModelID(candidate.ID)
			if !ok {
				continue
			}
			warmCtx, cancel := context.WithTimeout(ctx, timeout)
			model, err := provider.GetModel(warmCtx, providerID, modelID)
			if err == nil {
				_, _ = provider.GetLanguage(warmCtx, model)
			}
			cancel()
		}
		schedulerLog.Info("tier pre-warmed", map[string]any{"tier": string(tier)})
	}
}

func splitModelID(fullID string) (providerID, modelID string, ok bool) {
	slash := strings.Index(fullID, "/")
	if slash <= 0 {
		return "", "", false
	}
	return fullID[:slash], fullID[slash+1:], true
}
