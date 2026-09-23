//go:build !windows

package app

import (
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/state"
)

type adaptiveRouterBackend interface {
	setAdaptiveRouter(*adaptive.AdaptiveModelRouter)
}

func (backend *openRouterBackend) setAdaptiveRouter(router *adaptive.AdaptiveModelRouter) {
	backend.router = router
}

func initRunRouter(args cliArgs, events ...*eventWriter) *adaptive.AdaptiveModelRouter {
	handle := state.InitRouter(adaptive.AdaptiveRouterConfig{
		// An empty low or frontier pool is left empty: the router routes
		// that tier on the high pool.
		HighModels:     configuredCandidates(args.High, adaptive.ModelTierHigh),
		LowModels:      configuredCandidates(args.Low, adaptive.ModelTierLow),
		FrontierModels: configuredCandidates(args.Frontier, adaptive.ModelTierFrontier),
		OnEvent: func(event adaptive.AdaptiveRouteEvent) {
			state.EmitRouteEvent(state.ToRouteEvent(event))
			if len(events) > 0 && events[0] != nil &&
				(event.Reason == "caller-canceled-pick" || event.Reason == "caller-canceled-request") {
				events[0].stage("router-cancellation", event.Reason, map[string]any{
					"slot": event.Slot, "tier": event.Tier, "model": event.Model,
					"provider_health_changed": false,
				})
			}
		},
	})
	router, _ := state.AdaptiveRouter(handle)
	return router
}

func configuredCandidates(raw string, tier adaptive.ModelTier) []adaptive.ModelCandidate {
	return adaptive.ParseModelList(&raw, tier)
}
