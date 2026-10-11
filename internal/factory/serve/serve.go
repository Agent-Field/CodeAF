// Package serve exposes the factory seam over HTTP so every client reads the
// same floor without knowing where its store lives.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Config is supplied by the host so the handler never reads a person's home
// or guesses the published address from a client-controlled Host header.
type Config struct {
	// Pipeline reads the same repository rules as intake, including personal limits.
	Pipeline    func(context.Context, string, string) (pipeline.File, error)
	Address     string
	Version     string
	AccessToken string
	// GitHubClientID enables GitHub device sign-in; Team optionally restricts handles.
	GitHubClientID string
	Team           []string
	// GitHubHTTPClient permits a fake transport while keeping production URLs fixed.
	GitHubHTTPClient *http.Client
	// TokensPath names access-token rows beside the host config. Empty means
	// only AccessToken is accepted. It is read on each request so invite and
	// revoke apply without restarting the service.
	TokensPath   string
	PollingEvery time.Duration
	// Store enables worker writes to the same documents the local runner uses.
	Store *store.Store
	// LeaseOptions supplies the claims desk's TTL and deterministic clock.
	LeaseOptions lease.Options
	// PullWait bounds long polling; zero uses 30 seconds and larger values are capped.
	PullWait time.Duration
	// Record is supplied by the host at the same root as the job store.
	Record *record.Record
	// RecordApproval supports hosts that bind recording through their own door.
	RecordApproval func(ctx context.Context, id int, edited bool) (event string, err error)
	// Root is the factory directory the event stream watches. Empty means
	// the stream stays open and sends no ticks, which is a handler with no
	// store of its own rather than a broken watch.
	Root string
}

// Handler keeps the seam and host identity together for every response.
type Handler struct {
	seam    factory.Seam
	config  Config
	factory api.Factory
	mux     *http.ServeMux
	watch   *rootWatch

	workers  *workerService
	sessions *accessSessions
	auth     *deviceFlows
}

// New binds the seam's reads and verbs; absent doors refuse rather than panic.
func New(seam factory.Seam, config Config) *Handler {
	if config.Version == "" {
		config.Version = buildinfo.String()
	}
	h := &Handler{seam: seam, config: config,
		factory: api.Factory{Address: config.Address, Version: config.Version}, mux: http.NewServeMux()}
	h.sessions = &accessSessions{ids: make(map[string]accessSession), now: time.Now}
	h.auth = &deviceFlows{pending: make(map[string]*deviceFlow)}
	h.route("/v1/auth/device", map[string]http.HandlerFunc{http.MethodPost: h.startDevice})
	h.route("/v1/auth/device/poll", map[string]http.HandlerFunc{http.MethodPost: h.pollDevice})
	h.route("/access", map[string]http.HandlerFunc{http.MethodGet: h.access, http.MethodPost: h.access})
	h.route("/v1/access/revoke-sessions", map[string]http.HandlerFunc{http.MethodPost: h.revokeSessions})
	h.read("/v1/queue", h.queue)
	h.read("/v1/jobs/{id}", h.job)
	h.read("/v1/jobs/{id}/thread", h.thread)
	h.read("/v1/events", h.events)
	h.route("/v1/repos", map[string]http.HandlerFunc{http.MethodGet: h.repos, http.MethodPut: h.setRepos})
	h.read("/v1/repos/{owner}/{repo}/pipeline", h.pipeline)
	h.read("/v1/repos/{owner}/{repo}/dryrun", h.dryrun)
	h.read("/v1/github", h.github)
	h.read("/v1/me", h.me)
	h.read("/v1/audit", h.audit)
	h.verbs()
	h.workerRoutes()
	h.mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, http.StatusNotFound, "factory endpoint not found")
	})
	// The dashboard lives in the codeaf app; outside /v1 the factory only says so.
	h.read("/", h.root)
	if root := strings.TrimSpace(config.Root); root != "" {
		h.watch = watchRoot(root)
	}
	return h
}

// root answers any path outside /v1 with one plain line, never a page.
func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "codeaf factory %s \u2014 open it in codeaf (TUI) or the codeaf app; API at /v1\n", h.factory.Version)
}

// Close stops the filesystem watch. The HTTP server can still be running;
// a change after Close is not delivered.
func (h *Handler) Close() {
	if h != nil && h.watch != nil {
		h.watch.stop()
	}
}

// ServeHTTP checks the peer before routing, so even errors cannot reveal the
// floor to a remote caller without the host's access token or a live session.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if workerPath(r.URL.Path) {
		var ok bool
		r, ok = h.authorizeWorker(w, r)
		if !ok {
			return
		}
	} else if r.URL.Path != "/access" && !h.allowed(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, "a bearer access token or live session is required; open /access to enter the token")
		return
	}
	if session, ok := h.credential(r); ok && session.profile != nil && !h.teamAllows(session.profile.Handle) {
		h.fail(w, http.StatusForbidden, "this GitHub handle is not on the factory team")
		return
	}
	// Noncanonical paths are refused here so ServeMux cannot replace the JSON
	// contract with an HTML redirect that carries no factory identity.
	if r.URL.Path != path.Clean(r.URL.Path) {
		h.fail(w, http.StatusNotFound, "factory endpoint not found")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) read(pattern string, handler http.HandlerFunc) {
	h.route(pattern, map[string]http.HandlerFunc{http.MethodGet: handler})
}

// route checks methods inside the mux so its automatic HTML errors never
// replace the JSON response or omit the factory's identity.
func (h *Handler) route(pattern string, methods map[string]http.HandlerFunc) {
	allowed := make([]string, 0, len(methods))
	for method := range methods {
		allowed = append(allowed, method)
	}
	sort.Strings(allowed)
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if handler := methods[r.Method]; handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		h.fail(w, http.StatusMethodNotAllowed, "this endpoint accepts "+strings.Join(allowed, ", "))
	})
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	q := api.FromSnapshot(s, handle)
	q.Factory = h.factory
	h.write(w, http.StatusOK, q)
}

func (h *Handler) job(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if ok {
		h.write(w, http.StatusOK, struct {
			Factory api.Factory `json:"factory"`
			api.Job
		}{h.factory, api.FromItem(it)})
	}
}

// findJob gives every job door the same id and existence checks, without
// guessing whether an untyped runner error meant a missing job.
func (h *Handler) findJob(w http.ResponseWriter, r *http.Request) (factory.Item, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		h.fail(w, http.StatusBadRequest, "job id must be a positive integer")
		return factory.Item{}, false
	}
	if !h.require(w, h.seam.Load != nil) {
		return factory.Item{}, false
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return factory.Item{}, false
	}
	for _, it := range s.Items {
		if it.ID == id {
			return it, true
		}
	}
	h.fail(w, http.StatusNotFound, "job not found")
	return factory.Item{}, false
}

// Repository preserves the picker's facts with product names and JSON keys,
// rather than exposing the Go fields of factory.RepoInfo directly.
type Repository struct {
	Full     string    `json:"full"`
	Name     string    `json:"name"`
	Owner    string    `json:"owner"`
	Private  bool      `json:"private"`
	Pushed   time.Time `json:"pushed"`
	Open     int       `json:"open"`
	Checkout string    `json:"checkout,omitempty"`
}

func (h *Handler) repos(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Repos != nil) {
		return
	}
	watched, available, err := h.seam.Repos(r.Context())
	if h.failed(w, err) {
		return
	}
	if watched == nil {
		watched = []string{}
	}
	repos := make([]Repository, len(available))
	for i, repo := range available {
		repos[i] = Repository{repo.Full, repo.Name, repo.Owner, repo.Private, repo.Pushed, repo.Open, repo.Dir}
	}
	h.write(w, http.StatusOK, struct {
		Factory   api.Factory  `json:"factory"`
		Watched   []string     `json:"watched"`
		Available []Repository `json:"available"`
	}{h.factory, watched, repos})
}

// splitRepo is the owner and name in the path. A slash inside either
// piece would be a second repository, so it is refused before any read.
func (h *Handler) splitRepo(w http.ResponseWriter, r *http.Request) (owner, repo string, ok bool) {
	owner, repo = r.PathValue("owner"), r.PathValue("repo")
	if strings.ContainsAny(owner+repo, "/\\") || owner == "." || owner == ".." || repo == "." || repo == ".." {
		h.fail(w, http.StatusBadRequest, "repository must be owner/name")
		return "", "", false
	}
	return owner, repo, true
}

func (h *Handler) pipeline(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	if !h.require(w, h.seam.RecipeAt != nil) {
		return
	}
	recipe, problems, dir, err := h.seam.RecipeAt(owner + "/" + repo)
	// A failed file read is still a repository payload with its reason.
	if len(problems) == 0 && h.failed(w, err) {
		return
	}
	p := api.FromRecipe(recipe)
	// RecipeAt guarantees defaults without a checkout, but does not say
	// whether a known checkout has a file. Leave that unknown source absent.
	if p.Source == "" && dir == "" {
		p.Source = "defaults"
	}
	notes := make([]string, len(problems))
	for i, problem := range problems {
		notes[i] = problem.Error()
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Pipeline
		Problems []string `json:"problems"`
		Checkout string   `json:"checkout,omitempty"`
	}{h.factory, p, notes, dir})
}

func (h *Handler) link(r *http.Request) (factory.GitHubLink, error) {
	if h.seam.GitHub == nil {
		return factory.GitHubLink{}, nil
	}
	return h.seam.GitHub(r.Context())
}

func (h *Handler) github(w http.ResponseWriter, r *http.Request) {
	link, err := h.link(r)
	if h.failed(w, err) {
		return
	}
	var every string
	if h.config.PollingEvery > 0 {
		every = h.config.PollingEvery.String()
	}
	h.write(w, http.StatusOK, struct {
		Factory      api.Factory `json:"factory"`
		Login        string      `json:"login"`
		Via          string      `json:"via"`
		PollingEvery string      `json:"polling_every,omitempty"`
	}{h.factory, link.Login, link.Via, every})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	session, _ := h.credential(r)
	profile := api.Profile{Handle: handle, Scopes: []string{}}
	if session.profile != nil {
		profile = *session.profile
	}
	h.write(w, http.StatusOK, h.meBody(profile, session.id, session.invitedBy))
}

func (h *Handler) require(w http.ResponseWriter, exists bool) bool {
	if !exists {
		h.fail(w, http.StatusNotImplemented, "this factory does not provide that read")
	}
	return exists
}

func (h *Handler) failed(w http.ResponseWriter, err error) bool {
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, fs.ErrNotExist):
			status = http.StatusNotFound
		case errors.Is(err, fs.ErrPermission), errors.Is(err, errSignInApproval):
			status = http.StatusForbidden
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		case errors.Is(err, context.Canceled):
			status = http.StatusRequestTimeout
		case errors.Is(err, run.ErrAlreadyRunning):
			// A second run conflicts with the one already under way. The
			// sentence stays the refusal the runner wrote.
			status = http.StatusConflict
		}
		h.fail(w, status, err.Error())
	}
	return err != nil
}

func (h *Handler) fail(w http.ResponseWriter, status int, words string) {
	h.write(w, status, struct {
		Factory api.Factory `json:"factory"`
		Error   string      `json:"error"`
	}{h.factory, words})
}

func (h *Handler) write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
