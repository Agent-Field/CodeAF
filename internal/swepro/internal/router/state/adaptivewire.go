package state

// The F8 gap, closed: `GetRouter()` now hands back a real
// `*adaptive.AdaptiveModelRouter` instead of the inert `placeholderRouter`, and
// every `register()` it performs is bridged onto `EmitRouteEvent`.
//
// ENGINE-DESIGN §0 F8 / R13: `state.SetRouterFactory` was never called outside
// tests and nothing outside `internal/router` imported any router package, so
// the router was functionally unreachable — `llm.ts:97-146`'s `router.pick()` /
// `router.register()` had no Go counterpart to call. The design flags the
// wiring plus an `AdaptiveRouteEvent → RouteEvent → EmitRouteEvent` bridge as
// unclaimed work belonging to the engine layer's bootstrap. It lands here
// rather than in a bootstrap package because `newRouter` is this file's own
// package-level seam and because state.ts's `getRouter()` is exactly the "build
// the default one lazily" contract adaptive needs.
//
// The import direction is safe: `adaptive` imports only `internal/jscompat`, so
// `state → adaptive` introduces no cycle.
//
// Fidelity notes (deliberate, do not "fix"):
//   - state.ts's `getRouter()` falls back to `{}` — every field of
//     AdaptiveRouterConfig is optional, and adaptive's own constructor supplies
//     the verbatim v3-harness pools for both tiers. DefaultRouterConfig stays
//     `map[string]any{}` so the fallback is byte-identical to the TS.
//   - `register()` invokes the configured `on_event` hook, "so we don't
//     double-emit here" (`llm.ts:141-142`). The bridge is therefore installed as
//     `cfg.OnEvent` — NOT as a second call at the register site — and a config
//     that already carries an OnEvent keeps it, exactly like the TS would.
//   - KNOWN DIVERGENCE (bounded): `adaptive.AdaptiveRouteEvent.Error` is
//     `ErrorText`, a string type whose MarshalJSON preserves an UNPAIRED
//     SURROGATE left behind by register()'s UTF-16 clip; `state.RouteEvent.Error`
//     is a plain string, and jscompat.Stringify routes it through encoding/json,
//     which substitutes U+FFFD. The `[router] …` NDJSON line therefore differs
//     from TS for the one input domain "the model's error message was clipped
//     mid-astral-character". Widening RouteEvent.Error would change the type of
//     a field three existing fixture suites assert against, for a case no real
//     provider message reaches; the clip length is 200 chars
//     (adaptive.go's register()), so it needs a >200-char message whose 200th
//     UTF-16 unit lands inside a surrogate pair.

import (
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

// ToRouteEvent is the `AdaptiveRouteEvent → RouteEvent` bridge. The two structs
// are field-for-field identical apart from `Tier` (ModelTier vs string) and
// `Error` (ErrorText vs string), so this is a total conversion with no
// information loss beyond the surrogate case documented above.
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
		Error:         string(ev.Error),
	}
}

// AdaptiveConfig coerces the opaque RouterConfig into adaptive's struct.
//
// `initRouter(cfg)` is called from the CLI bootstrap with a parsed JSON object,
// and `getRouter()` falls back to `{}`, so both a typed struct and a decoded
// JSON map have to work. Anything else — or a map that fails to decode — yields
// the zero config, which adaptive then fills with its own defaults, matching
// the TS behaviour of `new AdaptiveModelRouter(undefined as any)`.
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

// NewAdaptiveRouter is the default `newRouter`: build the real router and wire
// its event hook to EmitRouteEvent.
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
	// Replaces the placeholder factory declared in state.go. A test that wants
	// the inert one back can install it through SetRouterFactory.
	newRouter = NewAdaptiveRouter
}
