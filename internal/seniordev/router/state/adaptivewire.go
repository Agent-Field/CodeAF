//go:build !windows

package state

// The adaptive router wiring: GetRouter hands back a real
// *adaptive.AdaptiveModelRouter whose event hook is bridged onto
// EmitRouteEvent, so every Register lands on the `[router]` NDJSON line.

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

// ToRouteEvent converts an adaptive event into the emitted record.
func ToRouteEvent(ev adaptive.AdaptiveRouteEvent) RouteEvent {
	return RouteEvent{
		Slot:          ev.Slot,
		Tier:          string(ev.Tier),
		Model:         ev.Model,
		PreviousModel: ev.PreviousModel,
		Switched:      ev.Switched,
		Reason:        ev.Reason,
		Score:         ev.Score,
		ElapsedS:      ev.ElapsedS,
		Attempts:      ev.Attempts,
		Successes:     ev.Successes,
		Failures:      ev.Failures,
		RateLimits:    ev.RateLimits,
		LatencyEwma:   ev.LatencyEwma,
		ToksecEwma:    ev.ToksecEwma,
		Error:         ev.Error,
	}
}

// AdaptiveConfig coerces the opaque RouterConfig into adaptive's struct. Both
// a typed struct and a decoded JSON map work; anything else yields the zero
// config, which the router fills with its own defaults.
func AdaptiveConfig(cfg RouterConfig) adaptive.AdaptiveRouterConfig {
	switch typed := cfg.(type) {
	case adaptive.AdaptiveRouterConfig:
		return typed
	case *adaptive.AdaptiveRouterConfig:
		if typed != nil {
			return *typed
		}
		return adaptive.AdaptiveRouterConfig{}
	case nil:
		return adaptive.AdaptiveRouterConfig{}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return adaptive.AdaptiveRouterConfig{}
	}
	var out adaptive.AdaptiveRouterConfig
	if err := json.Unmarshal(encoded, &out); err != nil {
		return adaptive.AdaptiveRouterConfig{}
	}
	return out
}

// NewAdaptiveRouter builds the real router and wires its event hook to
// EmitRouteEvent unless the config already carries one.
func NewAdaptiveRouter(cfg RouterConfig) Router {
	resolved := AdaptiveConfig(cfg)
	if resolved.OnEvent == nil {
		resolved.OnEvent = func(ev adaptive.AdaptiveRouteEvent) { EmitRouteEvent(ToRouteEvent(ev)) }
	}
	return adaptive.NewAdaptiveModelRouter(resolved)
}

// AdaptiveRouter narrows GetRouter()'s opaque handle. It returns false when a
// test has installed a different factory through SetRouterFactory, which is the
// only way the singleton can be anything else.
func AdaptiveRouter(r Router) (*adaptive.AdaptiveModelRouter, bool) {
	router, ok := r.(*adaptive.AdaptiveModelRouter)
	return router, ok
}

func init() {
	newRouter = NewAdaptiveRouter
}
