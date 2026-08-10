package codeaf

import (
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/state"
)

type adaptiveRouterBackend interface {
	setAdaptiveRouter(*adaptive.AdaptiveModelRouter)
}

func (backend *openRouterBackend) setAdaptiveRouter(router *adaptive.AdaptiveModelRouter) {
	backend.router = router
}

func initRunRouter(args cliArgs) *adaptive.AdaptiveModelRouter {
	high := configuredCandidates(args.High, adaptive.ModelTierHigh)
	low := configuredCandidates(args.Low, adaptive.ModelTierLow)
	if len(low) == 0 {
		low = retierCandidates(high, adaptive.ModelTierLow)
	}
	frontier := configuredCandidates(args.Frontier, adaptive.ModelTierFrontier)
	handle := state.InitRouter(adaptive.AdaptiveRouterConfig{
		HighModels: high, LowModels: low, FrontierModels: frontier,
	})
	router, _ := state.AdaptiveRouter(handle)
	return router
}

func configuredCandidates(raw string, tier adaptive.ModelTier) []adaptive.ModelCandidate {
	return adaptive.ParseModelList(&raw, tier)
}

func retierCandidates(
	input []adaptive.ModelCandidate, tier adaptive.ModelTier,
) []adaptive.ModelCandidate {
	out := make([]adaptive.ModelCandidate, len(input))
	copy(out, input)
	for index := range out {
		out[index].Tier = tier
	}
	return out
}
