package state

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// state.ts has no .test.ts of its own; these cover the half the fixtures
// cannot reach (the process singleton, which is opaque at this port's
// boundary) plus the two documented Set-semantics divergences.

func TestGetRouterBuildsADefaultSingleton(t *testing.T) {
	ResetRouterForTesting()
	t.Cleanup(ResetRouterForTesting)

	first := GetRouter()
	if first == nil {
		t.Fatalf("GetRouter must never return nil — it is the safety net for code paths that run before the CLI bootstraps the router")
	}
	if second := GetRouter(); second != first {
		t.Errorf("GetRouter must return the same process singleton on every call")
	}
}

func TestInitRouterReplacesTheSingleton(t *testing.T) {
	ResetRouterForTesting()
	t.Cleanup(ResetRouterForTesting)

	before := GetRouter()
	built := InitRouter(map[string]any{"max_attempts": 5})
	if built == before {
		t.Fatalf("InitRouter must construct a fresh router, not reuse the default")
	}
	if got := GetRouter(); got != built {
		t.Errorf("GetRouter must hand back what InitRouter stored")
	}
	// Unconditional: TS reassigns `router` on every initRouter call.
	if again := InitRouter(map[string]any{}); again == built {
		t.Errorf("a second InitRouter must replace the singleton again")
	}
}

func TestSetRouterFactoryWiresTheAdaptivePort(t *testing.T) {
	ResetRouterForTesting()
	t.Cleanup(ResetRouterForTesting)

	type stub struct{ cfg RouterConfig }
	var seen []RouterConfig
	restore := SetRouterFactory(func(cfg RouterConfig) Router {
		seen = append(seen, cfg)
		return &stub{cfg: cfg}
	})
	defer restore()

	built := GetRouter()
	if _, ok := built.(*stub); !ok {
		t.Fatalf("GetRouter must build through the injected factory, got %T", built)
	}
	if len(seen) != 1 {
		t.Fatalf("factory called %d times, want 1", len(seen))
	}
	// getRouter()'s fallback is `new AdaptiveModelRouter({})` — an empty
	// config object, every field of AdaptiveRouterConfig being optional.
	if cfg, ok := seen[0].(map[string]any); !ok || len(cfg) != 0 {
		t.Errorf("default config = %#v, want the empty object literal", seen[0])
	}

	InitRouter(map[string]any{"random_seed": 7})
	if len(seen) != 2 {
		t.Fatalf("InitRouter must also go through the factory")
	}
}

func TestOnRouteEventReturnsAWorkingUnsubscribe(t *testing.T) {
	ResetListenersForTesting()
	t.Cleanup(ResetListenersForTesting)
	var sink bytes.Buffer
	previous := Stderr
	Stderr = &sink
	t.Cleanup(func() { Stderr = previous })

	count := 0
	off := OnRouteEvent(func(RouteEvent) { count++ })
	EmitRouteEvent(RouteEvent{Model: "a"})
	off()
	EmitRouteEvent(RouteEvent{Model: "b"})

	if count != 1 {
		t.Errorf("listener called %d times, want 1", count)
	}
	// Telemetry keeps flowing regardless of subscribers.
	if got := strings.Count(sink.String(), "[router] "); got != 2 {
		t.Errorf("stderr lines = %d, want 2", got)
	}
}

func TestEmitRouteEventLineIsExactlyOneNDJSONRecord(t *testing.T) {
	ResetListenersForTesting()
	t.Cleanup(ResetListenersForTesting)
	var sink bytes.Buffer
	previous := Stderr
	Stderr = &sink
	t.Cleanup(func() { Stderr = previous })

	event := RouteEvent{
		Slot:          "explorer",
		Tier:          "low",
		Model:         "openrouter/z-ai/glm-5.1",
		PreviousModel: "",
		Switched:      false,
		Reason:        "sticky",
		Score:         jscompat.JSNumber(0.5),
		ElapsedS:      jscompat.JSNumber(1),
		Attempts:      jscompat.JSNumber(1),
		Successes:     jscompat.JSNumber(1),
		Failures:      jscompat.JSNumber(0),
		RateLimits:    jscompat.JSNumber(0),
		LatencyEwma:   jscompat.JSNumber(1),
		ToksecEwma:    jscompat.JSNumber(0),
		Error:         "",
	}
	EmitRouteEvent(event)

	line := sink.String()
	if !strings.HasPrefix(line, "[router] ") {
		t.Fatalf("line %q must start with the [router] tag", line)
	}
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("line %q must be exactly one newline-terminated record", line)
	}
	// The payload must be the event verbatim — the wire format downstream
	// tooling parses.
	want, err := jscompat.Stringify(event)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	if got := strings.TrimSuffix(strings.TrimPrefix(line, "[router] "), "\n"); got != string(want) {
		t.Errorf("payload = %s, want %s", got, want)
	}
}

func TestPanickingListenerDoesNotBreakTelemetry(t *testing.T) {
	ResetListenersForTesting()
	t.Cleanup(ResetListenersForTesting)
	var sink bytes.Buffer
	previous := Stderr
	Stderr = &sink
	t.Cleanup(func() { Stderr = previous })

	reached := false
	OnRouteEvent(func(RouteEvent) { panic("listener exploded") })
	OnRouteEvent(func(RouteEvent) { reached = true })

	EmitRouteEvent(RouteEvent{Model: "m"})

	if !reached {
		t.Errorf("a panicking listener must not stop the ones registered after it")
	}
	if !strings.Contains(sink.String(), "[router] ") {
		t.Errorf("the NDJSON line must still be written")
	}
}

// TestKnownDivergences documents, in executable form, the two places the Go
// listener registry cannot match a JS Set exactly. Both are called out in the
// package doc.
func TestKnownDivergences(t *testing.T) {
	t.Run("registering the same func twice yields two entries (a JS Set would dedupe)", func(t *testing.T) {
		ResetListenersForTesting()
		t.Cleanup(ResetListenersForTesting)
		var sink bytes.Buffer
		previous := Stderr
		Stderr = &sink
		t.Cleanup(func() { Stderr = previous })

		count := 0
		listener := func(RouteEvent) { count++ }
		OnRouteEvent(listener)
		OnRouteEvent(listener)
		EmitRouteEvent(RouteEvent{})

		// Go funcs are not comparable, so identity dedupe is not expressible.
		if count != 2 {
			t.Errorf("listener calls = %d, want 2 (the documented divergence; TS would say 1)", count)
		}
	})

	t.Run("a listener added during an emit is not visited by that emit", func(t *testing.T) {
		ResetListenersForTesting()
		t.Cleanup(ResetListenersForTesting)
		var sink bytes.Buffer
		previous := Stderr
		Stderr = &sink
		t.Cleanup(func() { Stderr = previous })

		lateCalls := 0
		OnRouteEvent(func(RouteEvent) {
			OnRouteEvent(func(RouteEvent) { lateCalls++ })
		})
		EmitRouteEvent(RouteEvent{})

		// A live JS Set iterator WOULD reach the newly added entry.
		if lateCalls != 0 {
			t.Errorf("late listener calls = %d, want 0 (the documented divergence)", lateCalls)
		}
		// It is registered for the next emit, though.
		EmitRouteEvent(RouteEvent{})
		if lateCalls == 0 {
			t.Errorf("the listener added mid-emit must fire on the following emit")
		}
	})
}

// TestConcurrentEmitIsSafe is a Go-only concern: the TS module runs on one
// thread, so the registry needs a lock here that has no TS counterpart.
func TestConcurrentEmitIsSafe(t *testing.T) {
	ResetListenersForTesting()
	t.Cleanup(ResetListenersForTesting)
	var sink bytes.Buffer
	previous := Stderr
	Stderr = &sink
	t.Cleanup(func() { Stderr = previous })

	var mu sync.Mutex
	seen := 0
	OnRouteEvent(func(RouteEvent) {
		mu.Lock()
		seen++
		mu.Unlock()
	})

	var wait sync.WaitGroup
	for i := 0; i < 50; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			EmitRouteEvent(RouteEvent{Model: "m"})
		}()
	}
	wait.Wait()

	mu.Lock()
	defer mu.Unlock()
	if seen != 50 {
		t.Errorf("listener calls = %d, want 50", seen)
	}
}
