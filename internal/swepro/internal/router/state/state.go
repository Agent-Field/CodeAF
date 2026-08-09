// Package state is a bug-for-bug port of src/router/state.ts — the
// process-global AdaptiveModelRouter singleton plus the route-event fan-out.
//
// state.ts does two unrelated things and this port keeps them separate:
//
//  1. initRouter/getRouter: a lazily defaulted process singleton. state.ts
//     never calls a METHOD on the router — it constructs it, stores it and
//     hands it back — so the exact contract this package needs is an opaque
//     handle plus a constructor. adaptive.ts (the AdaptiveModelRouter itself)
//     is a sibling module outside this port's scope, so the constructor is a
//     seam: SetRouterFactory wires the real one in a single line, and until
//     then GetRouter() still honours its "never nil" contract.
//
//  2. onRouteEvent/emitRouteEvent: the interesting half. Every route decision
//     is written to stderr as `[router] <json>\n` NDJSON *and* fanned out to
//     process-local subscribers (the TUI bridge renders "retry · qwen →
//     deepseek" off these). A throwing listener must not break telemetry.
//
// Fidelity notes (deliberate, do not "fix"):
//   - The stderr line is `[router] ${JSON.stringify(event)}\n`, so RouteEvent's
//     JSON tags are in the order adaptive.ts's `register()` builds the object
//     literal — that order IS the wire format.
//   - Every numeric field is jscompat.JSNumber: JSON.stringify writes null for
//     NaN/±Infinity, and score/elapsed_s/the EWMAs are all reachable as NaN
//     (0/0 in the EWMA update) from adaptive.ts.
//   - emitRouteEvent writes the NDJSON line BEFORE invoking listeners, and a
//     panicking listener is swallowed exactly like the TS empty catch — later
//     listeners still run.
//   - KNOWN DIVERGENCE (bounded): TS holds listeners in a Set, which dedupes
//     by function identity. Go funcs are not comparable, so each OnRouteEvent
//     call gets its own token and registering the SAME func twice yields two
//     entries (TS would keep one). Unsubscribing removes only that token's
//     entry, which is the same observable in every single-registration case.
//   - KNOWN DIVERGENCE (bounded): a JS Set iterator is live. Deleting a
//     not-yet-visited listener during emit skips it — reproduced, because the
//     fan-out re-checks liveness per key. Listeners ADDED during an emit are
//     visited by the TS iterator but not by this one.
package state

import (
	"io"
	"os"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── the process singleton ─────────────────────────────────────────────────

// Router is the handle state.ts stores. Deliberately method-free: that is the
// entire contract state.ts exercises, and *adaptive.AdaptiveModelRouter will
// satisfy it unchanged once that sibling port lands.
type Router any

// RouterConfig mirrors AdaptiveRouterConfig, likewise opaque at this boundary.
type RouterConfig any

// DefaultRouterConfig is the `{}` getRouter() falls back to: the verbatim
// v3-harness lists for both tiers, since every field of AdaptiveRouterConfig
// is optional.
var DefaultRouterConfig RouterConfig = map[string]any{}

// placeholderRouter keeps GetRouter()'s never-nil contract before the adaptive
// port is wired. It is inert on purpose — anything that tries to route through
// it should fail loudly at the call site rather than silently no-op.
type placeholderRouter struct{ Cfg RouterConfig }

var newRouter = func(cfg RouterConfig) Router { return &placeholderRouter{Cfg: cfg} }

// SetRouterFactory installs the real `new AdaptiveModelRouter(cfg)`. Returns a
// restore func.
func SetRouterFactory(f func(cfg RouterConfig) Router) func() {
	routerMu.Lock()
	prev := newRouter
	newRouter = f
	routerMu.Unlock()
	return func() {
		routerMu.Lock()
		newRouter = prev
		routerMu.Unlock()
	}
}

var (
	routerMu sync.Mutex
	router   Router
)

// InitRouter mirrors initRouter(): unconditionally replaces the singleton.
func InitRouter(cfg RouterConfig) Router {
	routerMu.Lock()
	defer routerMu.Unlock()
	router = newRouter(cfg)
	return router
}

// GetRouter mirrors getRouter(). Safety net for code paths that run before the
// CLI bootstraps the router (tests, eager imports): build a default-config one.
func GetRouter() Router {
	routerMu.Lock()
	defer routerMu.Unlock()
	if router == nil {
		router = newRouter(DefaultRouterConfig)
	}
	return router
}

// ResetRouterForTesting drops the singleton so the next GetRouter() rebuilds
// it. No TS analogue — the TS module is reloaded per test file.
func ResetRouterForTesting() {
	routerMu.Lock()
	router = nil
	routerMu.Unlock()
}

// ── route events ──────────────────────────────────────────────────────────

// RouteEvent mirrors adaptive.ts's AdaptiveRouteEvent. Tag order is the
// object-literal order in AdaptiveModelRouter.register(), which is what
// JSON.stringify emits and therefore what the `[router] ` NDJSON line is.
type RouteEvent struct {
	Slot          string            `json:"slot"`
	Tier          string            `json:"tier"`
	Model         string            `json:"model"`
	PreviousModel string            `json:"previous_model"`
	Switched      bool              `json:"switched"`
	Reason        string            `json:"reason"`
	Score         jscompat.JSNumber `json:"score"`
	ElapsedS      jscompat.JSNumber `json:"elapsed_s"`
	Attempts      jscompat.JSNumber `json:"attempts"`
	Successes     jscompat.JSNumber `json:"successes"`
	Failures      jscompat.JSNumber `json:"failures"`
	RateLimits    jscompat.JSNumber `json:"rate_limits"`
	LatencyEwma   jscompat.JSNumber `json:"latency_ewma"`
	ToksecEwma    jscompat.JSNumber `json:"toksec_ewma"`
	Error         string            `json:"error"`
}

// RouteEventListener mirrors `(event: AdaptiveRouteEvent) => void`.
type RouteEventListener func(event RouteEvent)

// Stderr is `process.stderr`. Injectable so the fixture replay can capture the
// exact bytes; NDJSON goes to stderr so it doesn't pollute the bus event
// stream on stdout.
var Stderr io.Writer = os.Stderr

// routerTag prefixes every NDJSON record.
const routerTag = "[router] "

var stderrMu sync.Mutex

var (
	listenerMu   sync.Mutex
	listenerSeq  uint64
	listenerKeys []uint64
	listenerFns  map[uint64]RouteEventListener
)

// OnRouteEvent mirrors onRouteEvent(): registers a process-local subscriber
// and returns its unsubscribe.
func OnRouteEvent(listener RouteEventListener) func() {
	listenerMu.Lock()
	if listenerFns == nil {
		listenerFns = map[uint64]RouteEventListener{}
	}
	listenerSeq++
	key := listenerSeq
	listenerKeys = append(listenerKeys, key)
	listenerFns[key] = listener
	listenerMu.Unlock()
	return func() {
		listenerMu.Lock()
		if _, ok := listenerFns[key]; ok {
			delete(listenerFns, key)
			for i, k := range listenerKeys {
				if k == key {
					listenerKeys = append(listenerKeys[:i], listenerKeys[i+1:]...)
					break
				}
			}
		}
		listenerMu.Unlock()
	}
}

// ResetListenersForTesting drops every subscriber. No TS analogue.
func ResetListenersForTesting() {
	listenerMu.Lock()
	listenerKeys = nil
	listenerFns = nil
	listenerMu.Unlock()
}

// EmitRouteEvent mirrors emitRouteEvent(): one NDJSON line on stderr, then the
// fan-out. Listener faults must not break router telemetry — swallowed.
func EmitRouteEvent(event RouteEvent) {
	encoded, err := jscompat.Stringify(event)
	if err != nil {
		// JSON.stringify cannot fail for this shape; if the Go marshaller
		// somehow does, the line is still emitted so the stream stays
		// line-oriented.
		encoded = []byte("null")
	}
	// One Write of the whole record, serialized: TS gets non-interleaved
	// stderr for free from the event loop, but two concurrent Go emits could
	// otherwise tear a line in half and break the NDJSON contract downstream
	// tooling parses.
	line := make([]byte, 0, len(routerTag)+len(encoded)+1)
	line = append(line, routerTag...)
	line = append(line, encoded...)
	line = append(line, '\n')
	stderrMu.Lock()
	_, _ = Stderr.Write(line)
	stderrMu.Unlock()

	listenerMu.Lock()
	keys := make([]uint64, len(listenerKeys))
	copy(keys, listenerKeys)
	listenerMu.Unlock()

	for _, key := range keys {
		listenerMu.Lock()
		listener, live := listenerFns[key]
		listenerMu.Unlock()
		if !live {
			// Unsubscribed by an earlier listener in this same emit — a live
			// JS Set iterator would skip it too.
			continue
		}
		callListener(listener, event)
	}
}

// callListener is the TS `try { l(event) } catch {}`.
func callListener(listener RouteEventListener, event RouteEvent) {
	defer func() { _ = recover() }()
	listener(event)
}
