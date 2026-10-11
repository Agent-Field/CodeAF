// Package serve exposes the factory seam over HTTP so every client reads the
// same floor without knowing where its store lives.
package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	// Name is the label a join preview shows. Empty uses the address host,
	// or "codeaf" when the address has none.
	Name string
	// ServedBy is "tui" when a window is serving and "factory" when
	// `codeaf factory` is. Empty is derived from the access token.
	ServedBy string
	// Access is loopback, token or tailnet. Empty is derived.
	Access string
	// Needs is what a joiner must present: none, token or device.
	// Empty is derived from Access. device is only what the host sets.
	Needs string
	// SpentToday is the runner's day figure when this process holds the
	// ledger. Nil uses the floor's own reading of the same rule.
	SpentToday func() float64
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
	viewer   *viewerState
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
	h.viewer = newViewerState()
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
	h.read("/v1/about", h.about)
	h.read("/v1/me", h.me)
	h.route("/v1/me/takes_unassigned", map[string]http.HandlerFunc{http.MethodPut: h.setTakes})
	h.read("/v1/members", h.members)
	h.read("/v1/me/sessions", h.mySessions)
	h.route("/v1/me/sessions/signout-others", map[string]http.HandlerFunc{http.MethodPost: h.signOutOthers})
	h.route("/v1/me/sessions/{id}", map[string]http.HandlerFunc{http.MethodDelete: h.deleteSession})
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
	} else if r.URL.Path != "/access" && r.URL.Path != "/v1/about" && !h.allowed(r) {
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
	s, reason, whole := h.forViewer(r, s)
	q := api.FromSnapshot(s, handle)
	for i := range q.Jobs {
		q.Jobs[i] = h.withVersion(q.Jobs[i])
	}
	q.Factory = h.factory
	q.Reason = reason
	if reason != "" {
		if q.Jobs == nil {
			q.Jobs = []api.Job{}
		}
		if q.Repos == nil {
			q.Repos = []api.Repo{}
		}
		if q.Strip == nil {
			q.Strip = []api.Cell{}
		}
	}
	// Held-by is stamped only onto jobs this caller can already see.
	// Machine counts and spent today are floor-wide, so a narrower view
	// leaves them at zero rather than describing hidden repositories.
	online, busy, err := h.applyPresence(q.Jobs)
	if h.failed(w, err) {
		return
	}
	if whole {
		q.Capacity.MachinesOnline, q.Capacity.Busy = online, busy
		if h.config.SpentToday != nil {
			q.Since.SpentToday = h.config.SpentToday()
		}
	}
	h.write(w, http.StatusOK, q)
}

func (h *Handler) job(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	jobs := []api.Job{h.withVersion(api.FromItem(it))}
	if _, _, err := h.applyPresence(jobs); h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, jobs[0]})
}

// findJob gives every job door the same id and existence checks, without
// guessing whether an untyped runner error meant a missing job.
func (h *Handler) findJob(w http.ResponseWriter, r *http.Request) (factory.Item, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		h.fail(w, http.StatusUnprocessableEntity, "job id must be a positive integer")
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
			// A hidden job is the same answer as a missing one, so the
			// caller cannot tell which repositories exist.
			if !h.repoVisible(r, itemRepo(it)) {
				break
			}
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
	keys := append([]string{}, watched...)
	for _, repo := range available {
		keys = append(keys, repo.Full)
	}
	if allow, filter := h.repoAccess(r, keys); filter {
		watched = keepNames(watched, allow)
		next := make([]factory.RepoInfo, 0, len(available))
		for _, repo := range available {
			if allow[strings.ToLower(strings.TrimSpace(repo.Full))] {
				next = append(next, repo)
			}
		}
		available = next
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
		h.fail(w, http.StatusUnprocessableEntity, "repository must be owner/name")
		return "", "", false
	}
	if !h.repoVisible(r, owner+"/"+repo) {
		h.fail(w, http.StatusNotFound, "repository not found")
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
	p, notes, ok := h.readPipeline(w, r, owner+"/"+repo, dir, recipe, problems)
	if !ok {
		return
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
	// Machines are this caller's own. Another handle is not listed; it
	// appears only as held_by on a job this caller can already see.
	machines, err := h.viewerMachines(handle)
	if h.failed(w, err) {
		return
	}
	takes, err := h.savedTakes(handle)
	if h.failed(w, err) {
		return
	}
	body := h.meBody(profile, session.id, session.invitedBy)
	body.Machines = machines
	body.TakesUnassigned = takes
	h.write(w, http.StatusOK, body)
}

// about is the join preview. It is the one /v1 read that does not require a
// token, and it names no job or repository.
func (h *Handler) about(w http.ResponseWriter, r *http.Request) {
	h.write(w, http.StatusOK, struct {
		Factory       api.Factory `json:"factory"`
		Name          string      `json:"name"`
		CodeafVersion string      `json:"codeaf_version"`
		Contract      int         `json:"contract"`
		Needs         string      `json:"needs"`
		Access        string      `json:"access"`
		PollingEvery  string      `json:"polling_every,omitempty"`
		ServedBy      string      `json:"served_by"`
	}{h.factory, h.aboutName(), h.factory.Version, api.Contract, h.needsWord(),
		h.accessMode(), h.pollingEvery(), h.servedBy()})
}

func (h *Handler) accessMode() string {
	switch h.config.Access {
	case "loopback", "token", "tailnet":
		return h.config.Access
	}
	if strings.TrimSpace(h.config.AccessToken) != "" {
		return "token"
	}
	if loopbackListen(h.config.Address) {
		return "loopback"
	}
	return "tailnet"
}

func (h *Handler) needsWord() string {
	switch h.config.Needs {
	case "token", "device", "none":
		return h.config.Needs
	}
	if h.accessMode() == "token" {
		return "token"
	}
	return "none"
}

func (h *Handler) servedBy() string {
	if s := strings.TrimSpace(h.config.ServedBy); s != "" {
		return s
	}
	if strings.TrimSpace(h.config.AccessToken) == "" && h.accessMode() == "loopback" {
		return "tui"
	}
	return "factory"
}

func (h *Handler) pollingEvery() string {
	if h.config.PollingEvery > 0 {
		return h.config.PollingEvery.String()
	}
	return ""
}

func (h *Handler) aboutName() string {
	if n := strings.TrimSpace(h.config.Name); n != "" {
		return n
	}
	u, err := url.Parse(h.config.Address)
	if err != nil {
		return "codeaf"
	}
	host := u.Hostname()
	if host == "" {
		return "codeaf"
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return "localhost"
	}
	return host
}

func loopbackListen(address string) bool {
	u, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) require(w http.ResponseWriter, exists bool) bool {
	if !exists {
		h.fail(w, http.StatusNotImplemented, "this factory does not provide that read")
	}
	return exists
}

func (h *Handler) failed(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status, reason := classify(err)
	h.refuse(w, status, err.Error(), reason, "", nil, "")
	return true
}

func (h *Handler) fail(w http.ResponseWriter, status int, words string) {
	h.refuse(w, status, words, reasonFor(status), "", nil, "")
}

// requestID is four random bytes, hex. Every error body carries one so a
// person can name the refusal they saw.
func requestID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b[:])
}

func (h *Handler) write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
