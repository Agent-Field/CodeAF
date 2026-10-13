package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
)

// Contract changes only when the app wire makes a breaking change.
const Contract = 1

var errLoopback = errors.New("app transport must listen on a loopback address")

// Hello is the single stdout line by which the app discovers this launch.
// The epoch is independent of the bearer token, so public event ids disclose
// no part of the credential.
type Hello struct {
	URL           string `json:"url"`
	Token         string `json:"token"`
	PID           int    `json:"pid"`
	CodeafVersion string `json:"codeaf_version"`
	Contract      int    `json:"contract"`
	Home          string `json:"home"`
	Epoch         string `json:"epoch"`
}

type Options struct {
	Version        string
	Epoch          string
	Shutdown       func()
	Credentials    *config.AppCredentials
	ProfileDir     string
	KeyHTTPClient  *http.Client
	ModelCatalog   func(context.Context) ([]CatalogModel, error)
	Context        context.Context
	OpenRouterAuth openrouterauth.Options
	Agents         *AgentRegistry
	CrewRoles      CrewRoleSource
	CrewItems      CrewItemSource
}

// Server keeps every registered handler behind the same transport guard.
// Later lanes mount their resource handlers through Handle, never on a second
// listener with a different authentication policy.
type Server struct {
	options       Options
	mux           *http.ServeMux
	guarded       http.Handler
	once          sync.Once
	events        *EventHub
	keyState      keyState
	settingsState settingsState
	signIn        browserSignIn
	mutations     mutationState
	crewLogs      crewLogState
	inbox         *inboxState
}

type route struct {
	method string
	path   string
	serve  func(*Server, http.ResponseWriter, *http.Request)
}

var routes = []route{
	{http.MethodGet, "/app/v1/health", (*Server).health},
	{http.MethodPost, "/app/v1/shutdown", (*Server).shutdown},
	{http.MethodGet, "/app/v1/events", (*Server).stream},
}

func New(token string, options Options) *Server {
	if options.Epoch == "" {
		options.Epoch, _ = Token()
	}
	s := &Server{options: options, mux: http.NewServeMux(), events: newEventHub(options.Epoch)}
	for _, route := range routes {
		s.mux.HandleFunc(route.path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != route.method {
				w.Header().Set("Allow", route.method)
				fail(w, http.StatusMethodNotAllowed, "not_allowed", "this route does not accept that method")
				return
			}
			route.serve(s, w, r)
		})
	}
	s.registerKeys()
	s.registerContractRoutes()
	s.mux.HandleFunc("/app/v1/agents", s.agentsRoute)
	s.mux.HandleFunc("/app/v1/agents/", s.agentsRoute)
	s.registerSettings()
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "no such app route")
	})
	s.guarded = Guard(token, s.searchActivityMiddleware(s.inboxSourceGuard(s.crewTurnGuard(s.mux))))
	s.startCrewBudgetWatch()
	s.startInbox()
	return s
}

func (s *Server) Handle(pattern string, handler http.Handler) { s.mux.Handle(pattern, handler) }

func (s *Server) Handler() http.Handler { return s }

// Events is the one publication door for all app resource handlers.
func (s *Server) Events() *EventHub { return s.events }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.guarded.ServeHTTP(w, r) }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, struct {
		Status        string `json:"status"`
		CodeafVersion string `json:"codeaf_version"`
		Contract      int    `json:"contract"`
		Epoch         string `json:"epoch"`
	}{"ready", s.options.Version, Contract, s.options.Epoch})
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	var body *struct {
		Reason json.RawMessage `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body == nil {
		fail(w, http.StatusUnprocessableEntity, "bad_input", "shutdown needs a JSON object with an optional reason")
		return
	}
	if len(body.Reason) > 0 && (body.Reason[0] != '"' || json.Unmarshal(body.Reason, new(string)) != nil) {
		fail(w, http.StatusUnprocessableEntity, "bad_input", "shutdown reason must be a string")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, http.StatusUnprocessableEntity, "bad_input", "shutdown needs one JSON object")
		return
	}
	write(w, http.StatusOK, map[string]bool{"ok": true})
	// Flush the acknowledgment before waking shutdown, which waits for this
	// handler to return. Repeated requests still initiate shutdown only once.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	s.once.Do(func() {
		s.Close()
		if s.options.Shutdown != nil {
			s.options.Shutdown()
		}
	})
}

var requestSequence atomic.Uint64

func fail(w http.ResponseWriter, status int, reason, sentence string) {
	write(w, status, struct {
		Error     string `json:"error"`
		Reason    string `json:"reason"`
		RequestID string `json:"request_id"`
	}{sentence, reason, fmt.Sprintf("%08x", requestSequence.Add(1))})
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
