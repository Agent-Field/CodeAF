//go:build !windows

package state

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// These cover the process singleton and the listener registry.

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
	// Unconditional: every InitRouter call builds a fresh router.
	if again := InitRouter(map[string]any{}); again == built {
		t.Errorf("a second InitRouter must replace the singleton again")
	}
}

func TestSetRouterFactoryReplacesTheConstructor(t *testing.T) {
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
	// GetRouter's fallback is the empty config; the router fills in its own
	// defaults.
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
		Slot:          "coder",
		Model:         "openrouter/z-ai/glm-5.1",
		PreviousModel: "",
		Switched:      false,
		Reason:        "sticky",
		Score:         float64(0.5),
		ElapsedS:      float64(1),
		Attempts:      float64(1),
		Successes:     float64(1),
		Failures:      float64(0),
		RateLimits:    float64(0),
		LatencyEwma:   float64(1),
		ToksecEwma:    float64(0),
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
	want, err := jsonutil.Marshal(event)
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

// TestListenerRegistrySemantics pins how the listener registry treats
// duplicate registrations and registrations made during an emit.
func TestListenerRegistrySemantics(t *testing.T) {
	t.Run("registering the same func twice yields two entries", func(t *testing.T) {
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

		if count != 2 {
			t.Errorf("listener calls = %d, want 2", count)
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

		if lateCalls != 0 {
			t.Errorf("late listener calls = %d, want 0", lateCalls)
		}
		// It is registered for the next emit, though.
		EmitRouteEvent(RouteEvent{})
		if lateCalls == 0 {
			t.Errorf("the listener added mid-emit must fire on the following emit")
		}
	})
}

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
