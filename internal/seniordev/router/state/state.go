//go:build !windows

// Package state holds the process-wide adaptive router singleton and the
// route-event fan-out: every route decision is written to stderr as one
// `[router] <json>` NDJSON line and delivered to the process-local
// subscribers. A panicking subscriber never breaks telemetry.
package state

import (
	"io"
	"os"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// ── the process singleton ─────────────────────────────────────────────────

// Router is the opaque router handle the singleton stores; adaptivewire.go
// narrows it to *adaptive.AdaptiveModelRouter.
type Router any

// RouterConfig is the router configuration, opaque at this boundary.
type RouterConfig any

// DefaultRouterConfig is the empty config GetRouter falls back to; the router
// fills in its default pools.
var DefaultRouterConfig RouterConfig = map[string]any{}

// placeholderRouter keeps GetRouter()'s never-nil contract until
// adaptivewire.go's init installs the adaptive constructor. It is inert on
// purpose — anything that tries to route through it should fail loudly at the
// call site rather than silently no-op.
type placeholderRouter struct{ Cfg RouterConfig }

var newRouter = func(cfg RouterConfig) Router { return &placeholderRouter{Cfg: cfg} }

// SetRouterFactory replaces the router constructor. Returns a restore func.
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

// InitRouter unconditionally replaces the singleton.
func InitRouter(cfg RouterConfig) Router {
	routerMu.Lock()
	defer routerMu.Unlock()
	router = newRouter(cfg)
	return router
}

// GetRouter returns the singleton, building a default-config one for code
// paths that run before the CLI bootstraps the router (tests, eager imports).
func GetRouter() Router {
	routerMu.Lock()
	defer routerMu.Unlock()
	if router == nil {
		router = newRouter(DefaultRouterConfig)
	}
	return router
}

// ResetRouterForTesting drops the singleton so the next GetRouter() rebuilds
// it.
func ResetRouterForTesting() {
	routerMu.Lock()
	router = nil
	routerMu.Unlock()
}

// ── route events ──────────────────────────────────────────────────────────

// RouteEvent is the route decision record written to the `[router] ` NDJSON
// line and handed to subscribers.
type RouteEvent struct {
	Slot          string  `json:"slot"`
	Tier          string  `json:"tier"`
	Model         string  `json:"model"`
	PreviousModel string  `json:"previous_model"`
	Switched      bool    `json:"switched"`
	Reason        string  `json:"reason"`
	Score         float64 `json:"score"`
	ElapsedS      float64 `json:"elapsed_s"`
	Attempts      float64 `json:"attempts"`
	Successes     float64 `json:"successes"`
	Failures      float64 `json:"failures"`
	RateLimits    float64 `json:"rate_limits"`
	LatencyEwma   float64 `json:"latency_ewma"`
	ToksecEwma    float64 `json:"toksec_ewma"`
	Error         string  `json:"error"`
}

// RouteEventListener receives every emitted route event.
type RouteEventListener func(event RouteEvent)

// Stderr receives the NDJSON lines; it is a variable so tests can capture
// them. They go to stderr so they never mix with the event stream on stdout.
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

// OnRouteEvent registers a process-local subscriber and returns its
// unsubscribe.
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

// ResetListenersForTesting drops every subscriber.
func ResetListenersForTesting() {
	listenerMu.Lock()
	listenerKeys = nil
	listenerFns = nil
	listenerMu.Unlock()
}

// EmitRouteEvent writes one NDJSON line on stderr, then fans the event out.
// Listener panics are swallowed so they cannot break router telemetry.
func EmitRouteEvent(event RouteEvent) {
	encoded, err := jsonutil.Marshal(event)
	if err != nil {
		// Keep the stream line-oriented even if encoding somehow fails.
		encoded = []byte("null")
	}
	// One Write of the whole record, serialized, so two concurrent emits
	// cannot tear a line in half.
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
			// Unsubscribed by an earlier listener in this same emit.
			continue
		}
		callListener(listener, event)
	}
}

// callListener invokes one listener, swallowing any panic.
func callListener(listener RouteEventListener, event RouteEvent) {
	defer func() { _ = recover() }()
	listener(event)
}
