//go:build !windows

package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

func TestGetRouterReturnsAnAdaptiveRouter(t *testing.T) {
	ResetRouterForTesting()
	defer ResetRouterForTesting()

	router, ok := AdaptiveRouter(GetRouter())
	if !ok {
		t.Fatalf("GetRouter must hand back a *adaptive.AdaptiveModelRouter, got %T", GetRouter())
	}
	// The `{}` fallback yields adaptive's own default pool.
	if len(router.CandidatesForTier(adaptive.ModelTierHigh)) == 0 {
		t.Error("the default config must still populate the pool")
	}
}

func TestInitRouterDecodesAJSONConfig(t *testing.T) {
	ResetRouterForTesting()
	defer ResetRouterForTesting()

	built := InitRouter(map[string]any{"max_attempts": 5})
	router, ok := AdaptiveRouter(built)
	if !ok {
		t.Fatalf("InitRouter must build an adaptive router, got %T", built)
	}
	if router.MaxAttempts() != 5 {
		t.Errorf("max_attempts must survive the JSON round trip, got %v", router.MaxAttempts())
	}
}

func TestAdaptiveConfigAcceptsATypedStruct(t *testing.T) {
	attempts := float64(9)
	cfg := adaptive.AdaptiveRouterConfig{MaxAttempts: &attempts}
	got := AdaptiveConfig(cfg)
	if got.MaxAttempts == nil || *got.MaxAttempts != 9 {
		t.Fatalf("a typed config must pass through untouched, got %+v", got.MaxAttempts)
	}
	if got := AdaptiveConfig(nil); got.MaxAttempts != nil {
		t.Error("a nil config must yield the zero config")
	}
	if got := AdaptiveConfig("not a config"); got.MaxAttempts != nil {
		t.Error("an undecodable config must yield the zero config, not panic")
	}
}

// Register invokes the configured OnEvent hook and nothing else emits, so the
// bridge must be installed as cfg.OnEvent and must reach EmitRouteEvent's
// stderr NDJSON line and its listeners.
func TestRegisterBridgesOntoEmitRouteEvent(t *testing.T) {
	ResetRouterForTesting()
	ResetListenersForTesting()
	defer ResetRouterForTesting()
	defer ResetListenersForTesting()

	var buf strings.Builder
	prevStderr := Stderr
	Stderr = &buf
	defer func() { Stderr = prevStderr }()

	var seen []RouteEvent
	unsubscribe := OnRouteEvent(func(ev RouteEvent) { seen = append(seen, ev) })
	defer unsubscribe()

	router, ok := AdaptiveRouter(GetRouter())
	if !ok {
		t.Fatal("expected an adaptive router")
	}
	choice := router.PickSync("coder", adaptive.ModelTierHigh)
	router.Register(choice, 1.5, 42, nil)

	if len(seen) != 1 {
		t.Fatalf("register must fan out exactly one RouteEvent, got %d", len(seen))
	}
	if seen[0].Slot != "coder" {
		t.Errorf("slot: got %q", seen[0].Slot)
	}
	if seen[0].ElapsedS != float64(1.5) {
		t.Errorf("elapsed_s: got %v", seen[0].ElapsedS)
	}
	line := buf.String()
	if !strings.HasPrefix(line, "[router] ") || !strings.HasSuffix(line, "\n") {
		t.Fatalf("stderr line must be `[router] <json>\\n`, got %q", line)
	}
	var decoded RouteEvent
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(line, "[router] "), "\n")), &decoded); err != nil {
		t.Fatalf("the NDJSON line must be valid JSON: %v", err)
	}
}

// An explicit OnEvent in the config wins — the bridge only fills a nil hook.
func TestExplicitOnEventIsNotOverwritten(t *testing.T) {
	ResetRouterForTesting()
	ResetListenersForTesting()
	defer ResetRouterForTesting()
	defer ResetListenersForTesting()

	var buf strings.Builder
	prevStderr := Stderr
	Stderr = &buf
	defer func() { Stderr = prevStderr }()

	custom := 0
	cfg := adaptive.AdaptiveRouterConfig{OnEvent: func(adaptive.AdaptiveRouteEvent) { custom++ }}
	router, ok := AdaptiveRouter(InitRouter(cfg))
	if !ok {
		t.Fatal("expected an adaptive router")
	}
	choice := router.PickSync("coder", adaptive.ModelTierHigh)
	router.Register(choice, 1, 1, nil)

	if custom != 1 {
		t.Errorf("the caller's own OnEvent must be preserved, fired %d times", custom)
	}
	if buf.Len() != 0 {
		t.Errorf("the bridge must not also emit, wrote %q", buf.String())
	}
}

// A test may still install the inert factory.
func TestSetRouterFactoryStillOverridesTheDefault(t *testing.T) {
	ResetRouterForTesting()
	defer ResetRouterForTesting()

	type fake struct{ Router }
	restore := SetRouterFactory(func(cfg RouterConfig) Router { return &fake{} })
	defer restore()

	if _, ok := AdaptiveRouter(GetRouter()); ok {
		t.Error("an injected factory must win over the adaptive default")
	}
}
