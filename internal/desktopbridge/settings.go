package desktopbridge

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
)

// Settings routes share one table so a sibling file can add a path without
// editing the dispatch. ServeHTTP calls settingsRoutes once the route-table
// integrator wires `if b.settingsRoutes(w, r, path) { return }`. Until that
// line lands, tests enter through ServeSettings, which is the same table.

const (
	// The three source words the settings page is allowed to see. They name
	// the rung [config.APIKeySourceAt] resolved. They are never the key.
	keySourceOpenRouter = "OPENROUTER_API_KEY"
	keySourceProfile    = "profile"
	keySourceOpenAI     = "OPENAI_API_KEY"

	engineConnectionLocal     = "local"
	engineConnectionForwarded = "forwarded"
)

// KeyStatus is GET /settings/key. Source is omitted when no key is present,
// and it is never the key itself.
type KeyStatus struct {
	Present bool   `json:"present"`
	Source  string `json:"source,omitempty"`
}

// EngineStatus is GET /settings/engine. Connection is local or forwarded.
type EngineStatus struct {
	Local      bool   `json:"local"`
	Model      string `json:"model,omitempty"`
	Version    string `json:"version,omitempty"`
	Connection string `json:"connection"`
}

// settingsRoute is one row a sibling registers. Path is the engine path
// ServeHTTP already trimmed, such as "/settings/key".
type settingsRoute struct {
	method string
	path   string
	handle func(*Bridge, http.ResponseWriter, *http.Request)
}

var settingsTable struct {
	sync.Mutex
	routes []settingsRoute
}

func init() {
	registerSettingsRoute(http.MethodGet, "/settings/key", (*Bridge).serveKeyStatus)
	registerSettingsRoute(http.MethodGet, "/settings/engine", (*Bridge).serveEngineStatus)
}

// registerSettingsRoute adds one settings path. A sibling file calls it from
// init. The first row registered for a method and path is the one that runs.
func registerSettingsRoute(method, path string, handle func(*Bridge, http.ResponseWriter, *http.Request)) {
	path = settingsPath(path)
	settingsTable.Lock()
	defer settingsTable.Unlock()
	for _, route := range settingsTable.routes {
		if route.method == method && route.path == path {
			return
		}
	}
	settingsTable.routes = append(settingsTable.routes, settingsRoute{method: method, path: path, handle: handle})
}

// ServeSettings is the settings table as an HTTP handler. Tests call it
// before ServeHTTP is wired to settingsRoutes. The token check lives in
// settingsRoutes, so this door cannot skip it.
func (b *Bridge) ServeSettings(w http.ResponseWriter, r *http.Request) {
	if !b.settingsRoutes(w, r, r.URL.Path) {
		fail(w, http.StatusNotFound, "unknown engine action")
	}
}

// settingsRoutes serves /settings/… and reports whether the path was one of
// its own. The bearer token is required here as well as in ServeHTTP, so a
// direct call cannot read whether a provider key is present.
func (b *Bridge) settingsRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	path = settingsPath(path)
	if path != "/settings" && !strings.HasPrefix(path, "/settings/") {
		return false
	}
	if !b.tokenOK(r) {
		fail(w, http.StatusUnauthorized, "engine connection required")
		return true
	}
	settingsTable.Lock()
	routes := append([]settingsRoute(nil), settingsTable.routes...)
	settingsTable.Unlock()
	var allowed []string
	for _, route := range routes {
		if route.path != path {
			continue
		}
		if r.Method == route.method {
			route.handle(b, w, r)
			return true
		}
		allowed = append(allowed, route.method)
	}
	if len(allowed) > 0 {
		fail(w, http.StatusMethodNotAllowed, allowed[0]+" required")
		return true
	}
	fail(w, http.StatusNotFound, "unknown engine action")
	return true
}

func settingsPath(path string) string {
	path = strings.TrimPrefix(path, "/api/engine")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

// tokenOK is the same bearer compare ServeHTTP uses. An empty bridge token
// refuses everyone, including a request that also presented nothing.
func (b *Bridge) tokenOK(r *http.Request) bool {
	if b.token == "" {
		return false
	}
	presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(presented), []byte(b.token)) == 1
}

func (b *Bridge) serveKeyStatus(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	// The key lives in the same profile as the model roles. With no profile
	// door attached there is nothing honest to read, and an empty directory
	// would mean the ordinary profile on this machine.
	b.mu.Lock()
	models := b.models
	b.mu.Unlock()
	if models == nil {
		fail(w, http.StatusNotFound, "this engine has no model settings")
		return
	}
	// APIKeySourceAt names the rung and does not return the key. This handler
	// must not call APIKeyAt: the value is never copied into a response,
	// a log line, or an error.
	present, source := keySourceWord(config.APIKeySourceAt(models.ProfileDir))
	write(w, KeyStatus{Present: present, Source: source})
}

// keySourceWord maps the config package's sentences onto the three words the
// page is allowed to see. Anything else is not echoed: a future rung must
// not be able to carry the key into this response. An unknown non-empty rung
// still means a key is present.
func keySourceWord(rung string) (present bool, source string) {
	switch rung {
	case config.APIKeySourceOpenRouter:
		return true, keySourceOpenRouter
	case config.APIKeySourceProfile:
		return true, keySourceProfile
	case config.APIKeySourceOpenAI:
		return true, keySourceOpenAI
	default:
		return rung != "", ""
	}
}

func (b *Bridge) serveEngineStatus(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	write(w, b.engineStatus())
}

// engineStatus reports this bridge. local follows Connection.Local, which is
// true only for an engine the bridge started on this machine. With no
// conversation open yet, nothing has been opened elsewhere, so the bridge
// itself is local. model is the Conversation role in the profile when that
// door is attached, and the desktop default otherwise — never a profile read
// against the machine's ordinary home just because the door is missing.
func (b *Bridge) engineStatus() EngineStatus {
	b.mu.Lock()
	models := b.models
	local := true
	for _, s := range b.sessions {
		if s != nil && !s.conn.Local {
			local = false
			break
		}
	}
	b.mu.Unlock()
	connection := engineConnectionLocal
	if !local {
		connection = engineConnectionForwarded
	}
	model := Model
	if models != nil {
		if chosen, _, _ := config.DesktopRoleChoice(models.ProfileDir, config.DesktopRoleConversation); chosen != "" {
			model = chosen
		}
	}
	return EngineStatus{Local: local, Model: model, Version: buildinfo.String(), Connection: connection}
}
