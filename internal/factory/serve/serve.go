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
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// Config is supplied by the host so the handler never reads a person's home
// or guesses the published address from a client-controlled Host header.
type Config struct {
	// GitHubToken supplies the local profile credential only on a solo host.
	GitHubToken func(context.Context) string

	// Mode is solo or team; an unset mode follows the host access configuration.
	Mode string
	// ProfileDir resolves the solo factory's existing GitHub connection for setup reads.
	ProfileDir string
	// OperatorLogin names a solo host; team identity comes from the persisted claim.
	OperatorLogin string
	// ChangeAssignee mirrors a person's decision to GitHub before storing it.
	ChangeAssignee func(context.Context, factory.Item, string) error
	// DeleteBranch removes only a dropped job's branch after retention expires.
	DeleteBranch func(context.Context, factory.Item) error

	// PersonProfile reads cached public GitHub identity, including outside authors.
	PersonProfile func(context.Context, string) (factory.GitHubPerson, error)
	// CodeOwners reads a watched repository's cached default-branch ownership.
	CodeOwners func(context.Context, string) ([]factory.CodeOwner, error)
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
	// Now supplies the session clock, including pruning at startup.
	Now func() time.Time
	// TokensPath names access-token rows beside the host config. Empty means
	// only AccessToken is accepted. It is read on each request so a revoke
	// applies without restarting the service.
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
	// Name is the factory's display name. Empty uses the first watched
	// repository's owner, or this machine's short hostname without repositories.
	Name string
	// ServedBy is tui, app or factory. App requires the per-launch host token
	// on every person route, including about, and never trusts loopback.
	ServedBy string
	// Access is loopback, token or tailnet. Empty is derived.
	Access string
	// Needs is what a joiner must present: none, token or device.
	// Empty is derived from Access. device is only what the host sets.
	Needs string
	// TrustedProxies are the addresses whose X-Forwarded-For and
	// X-Forwarded-Proto are believed. Empty believes nobody's, and a request
	// that carries them anyway is never treated as a caller on this machine.
	TrustedProxies []netip.Prefix
	// SpentToday is the runner's day figure when this process holds the
	// ledger. Nil uses the floor's own reading of the same rule.
	SpentToday func() float64
}

// Handler keeps the seam and host identity together for every response.
type Handler struct {
	setupMu     sync.Mutex
	setupBusy   bool
	setupClosed bool
	setupCancel context.CancelFunc
	setupDone   chan struct{}
	setupTimer  *time.Timer
	accessMu    sync.Mutex
	accessFlows map[string]accessLinkFlow

	// reposMu serializes every change to the watched list: the single-repo switch
	// reads, edits and saves it, so any other writer must queue behind it. It is
	// never held over the event append or any network call.
	reposMu sync.Mutex

	jobsMu     sync.Mutex
	jobsTimer  *time.Timer
	jobsClosed bool

	seam    factory.Seam
	config  Config
	factory api.Factory
	mux     *http.ServeMux
	watch   *rootWatch

	workers        *workerService
	sessions       *accessSessions
	auth           *deviceFlows
	viewer         *viewerState
	consentMu      sync.Mutex
	joinMu         sync.Mutex
	replays        replayCache
	machinesMu     sync.Mutex
	machines       map[string]machineRow
	machinesTimer  *time.Timer
	machinesClosed bool
}

// New binds the seam's reads and verbs; absent doors refuse rather than panic.
func New(seam factory.Seam, config Config) *Handler {
	if config.ServedBy == "app" {
		config.Access = "token"
	}
	if config.Version == "" {
		config.Version = buildinfo.String()
	}
	h := &Handler{seam: seam, config: config,
		factory: api.Factory{Address: config.Address, Version: config.Version}, mux: http.NewServeMux()}
	h.factory.Name = factory.DisplayName(config.Name, h.nameRepos())
	h.sessions = &accessSessions{ids: make(map[string]accessSession), now: time.Now}
	if config.Now != nil {
		h.sessions.now = config.Now
	}
	h.auth = &deviceFlows{pending: make(map[string]*deviceFlow)}
	h.viewer = newViewerState()
	h.sessions.err = h.loadSessions()
	if _, err := tokens.LoadOperator(config.TokensPath); err != nil {
		h.sessions.err = err
	}
	h.route("/v1/auth/device", map[string]http.HandlerFunc{http.MethodPost: h.startDevice, http.MethodDelete: h.cancelDevice})
	h.route("/v1/auth/device/poll", map[string]http.HandlerFunc{http.MethodPost: h.pollDevice})
	h.route("/access", map[string]http.HandlerFunc{http.MethodGet: h.access, http.MethodPost: h.access})
	h.route("/v1/access/revoke-sessions", map[string]http.HandlerFunc{http.MethodPost: h.revokeSessions})
	h.read("/v1/queue", h.queue)
	h.read("/v1/jobs/{id}", h.job)
	h.read("/v1/jobs/{id}/thread", h.thread)
	h.read("/v1/events", h.events)
	h.route("/v1/repos", map[string]http.HandlerFunc{http.MethodGet: h.repos, http.MethodPut: h.setRepos})
	h.read("/v1/repos/{owner}/{repo}/pipeline", h.pipeline)
	h.read("/v1/repos/{owner}/{repo}/pipeline/edit-source", h.pipelineEditSource)
	h.read("/v1/repos/{owner}/{repo}/dryrun", h.dryrun)
	h.read("/v1/github", h.github)
	h.route("/v1/github/grant", map[string]http.HandlerFunc{http.MethodPut: h.grantGitHub})
	h.read("/v1/about", h.about)
	h.read("/v1/me", h.me)
	h.route("/v1/me/takes_unassigned", map[string]http.HandlerFunc{http.MethodPut: h.setTakes})
	h.read("/v1/members", h.members)
	h.read("/v1/people/{handle}", h.person)
	h.read("/v1/me/sessions", h.mySessions)
	h.route("/v1/me/sessions/signout-others", map[string]http.HandlerFunc{http.MethodPost: h.signOutOthers})
	h.route("/v1/me/sessions/{id}", map[string]http.HandlerFunc{http.MethodDelete: h.deleteSession})
	h.read("/v1/audit", h.audit)
	h.read("/v1/publish/codes", h.publishCodes)
	h.verbs()
	h.workerRoutes()
	h.v2Routes()
	h.route("/v1/factory-setup/{id}", map[string]http.HandlerFunc{http.MethodGet: h.factorySetup, http.MethodPost: h.factorySetup})
	h.route("/v1/factory-setup", map[string]http.HandlerFunc{http.MethodPost: h.factorySetup})
	h.route("/v1/factory-access", map[string]http.HandlerFunc{http.MethodGet: h.factoryAccess, http.MethodPost: h.factoryAccess})
	h.read("/v1/factory-days/today", h.factoryDay)
	h.jobVerbs()
	h.jobScreens()
	h.scheduleJobTimer()
	if config.Store != nil {
		h.setupMu.Lock()
		if saved, err := h.loadSetup("local"); err == nil {
			h.startSetup(saved)
		}
		h.setupMu.Unlock()
	}
	h.mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, http.StatusNotFound, "factory endpoint not found")
	})
	// The dashboard lives in the codeaf app; outside /v1 the factory only says so.
	h.read("/", h.root)
	if root := strings.TrimSpace(h.eventRoot()); root != "" {
		h.watch = watchRoot(root)
	}
	h.machineEvents()
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
	if h != nil {
		h.setupMu.Lock()
		h.setupClosed = true
		if h.setupTimer != nil {
			h.setupTimer.Stop()
		}
		if h.setupCancel != nil {
			h.setupCancel()
		}
		h.setupMu.Unlock()
	}
	if h != nil {
		h.jobsMu.Lock()
		h.jobsClosed = true
		if h.jobsTimer != nil {
			h.jobsTimer.Stop()
		}
		h.jobsMu.Unlock()
	}

	if h != nil {
		h.machinesMu.Lock()
		h.machinesClosed = true
		if h.machinesTimer != nil {
			h.machinesTimer.Stop()
		}
		h.machinesMu.Unlock()
	}
	if h != nil && h.watch != nil {
		h.watch.stop()
	}
}

// ServeHTTP checks the peer before routing, so even errors cannot reveal the
// floor to a remote caller without the host's access token or a live session.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = authRequest(h.withClient(r))
	w.Header().Set("Cache-Control", "no-store")
	if markPreMutation(r) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
	}
	// A caller who reached the factory over HTTPS is told to keep doing so.
	// Over plain HTTP the header means nothing and is not sent.
	if secureRequest(r) {
		w.Header().Set("Strict-Transport-Security", hstsValue)
	}
	if h.config.ServedBy == "app" && !workerPath(r.URL.Path) && !h.floorToken(bearer(r)) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, "the app bearer token is required")
		return
	}
	if workerPath(r.URL.Path) {
		var ok bool
		r, ok = h.authorizeWorker(w, r)
		if !ok {
			return
		}
	} else if r.URL.Path != "/access" && r.URL.Path != "/v1/factory-access" && r.URL.Path != "/v1/about" && !h.allowed(r) {
		if h.authFailed(w, r) {
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		// A secret that was sent and refused names why. A request with no
		// secret keeps the sentence that says how to sign in.
		if token := bearer(r); token != "" {
			h.refuseToken(w, token)
			return
		}
		h.fail(w, http.StatusUnauthorized, "a bearer access token or live session is required; open /access to enter the token")
		return
	}
	if h.authFailed(w, r) {
		return
	}
	if !workerPath(r.URL.Path) && r.URL.Path != "/v1/about" && r.URL.Path != "/access" && r.URL.Path != "/v1/auth/device" && r.URL.Path != "/v1/auth/device/poll" {
		if session, ok := h.credential(r); ok && session.tokenID != "" {
			for _, tok := range h.tokenRows() {
				if tok.ID == session.tokenID && (tok.AccessLink || tok.InvitedBy != "") {
					if session.profile == nil || session.githubToken == "" {
						h.fail(w, http.StatusUnauthorized, "Sign in with GitHub through an access link")
						return
					}
				}
			}
		}
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
	if workerPath(r.URL.Path) || r.URL.Path == "/v1/workers/attach" || r.URL.Path == "/v1/runners/attach" {
		defer h.machineEvents()
	}
	// Expiry precedes snapshot reads so the first response already shows Needs you.
	if h.workers != nil && h.workers.desk != nil {
		if _, err := h.leases(); h.failed(w, err) {
			return
		}
	}
	h.settleJobs(r.Method == http.MethodPost)
	h.idempotent(w, r)
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
	s, _ = h.withPeople(r, s)
	if h.authFailed(w, r) {
		return
	}
	query := r.URL.Query().Get("q")
	match := factory.MatchAt(query, h.clock(), handle)
	items := make([]factory.Item, 0, len(s.Items))
	for _, it := range s.Items {
		if !match(it) {
			continue
		}
		if !factory.HasWord(query, "is:dismissed") && factory.IsDismissed(it, handle) {
			continue
		}
		items = append(items, it)
	}
	s.Items = items
	q := api.FromSnapshot(s, handle, h.clock())
	for i := range q.Jobs {
		q.Jobs[i] = h.withVersion(q.Jobs[i])
	}
	q.Factory = h.factory
	q.Mode = h.mode()
	if q.Mode == api.FactorySolo {
		for i := range q.Jobs {
			h.soloJob(&q.Jobs[i], s.Items[i], handle)
		}
	}
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
	for i := range q.Jobs {
		h.presentJob(r, &q.Jobs[i])
	}
	for _, job := range q.Jobs {
		if job.HeldBy != nil {
			h.addSummary(r, q.People, job.HeldBy.Handle)
		}
	}
	h.write(w, http.StatusOK, q)
}

func (h *Handler) job(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	jobs := []api.Job{h.withVersion(api.FromItem(it))}
	if h.config.CodeOwners != nil && len(itemFiles(it)) > 0 {
		rules, err := h.config.CodeOwners(r.Context(), itemRepo(it))
		if err == nil {
			jobs[0].Owners = factory.OwnersFor(rules, itemFiles(it))
		}
	}
	jobs[0].Can = api.CanFor(it, handle, h.clock())
	api.ForViewer(&jobs[0], it, handle)
	if h.mode() == api.FactorySolo {
		h.soloJob(&jobs[0], it, handle)
	}
	if _, _, err := h.applyPresence(jobs); h.failed(w, err) {
		return
	}
	h.presentJob(r, &jobs[0])
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, jobs[0]})
}

// findJob gives every job door the same id and existence checks, without
// guessing whether an untyped runner error meant a missing job.
func (h *Handler) findJob(w http.ResponseWriter, r *http.Request) (factory.Item, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if (err != nil || id <= 0) && !strings.Contains(r.PathValue("id"), "#") {
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
		if it.ID == id || factory.JobRef(it) == r.PathValue("id") {
			// A hidden job is the same answer as a missing one, so the
			// caller cannot tell which repositories exist.
			visible := h.repoVisible(r, itemRepo(it))
			if h.authFailed(w, r) {
				return factory.Item{}, false
			}
			if !visible {
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
	if h.authFailed(w, r) {
		return
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
	visible := h.repoVisible(r, owner+"/"+repo)
	if h.authFailed(w, r) {
		return "", "", false
	}
	if !visible {
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
	grant, err := factorygithub.LoadReadGrant(factorygithub.GrantPath(h.config.TokensPath))
	if h.failed(w, err) {
		return
	}
	var grantedAt *time.Time
	if !grant.GrantedAt.IsZero() {
		grantedAt = &grant.GrantedAt
	}
	if link.Login == "" && grant.Login != "" {
		link.Login, link.Via = grant.Login, "grant"
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
		GrantedBy    string      `json:"granted_by,omitempty"`
		GrantedAt    *time.Time  `json:"granted_at,omitempty"`
	}{h.factory, link.Login, link.Via, every, grant.Login, grantedAt})
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
	body := h.meBody(profile, session.id)
	// Only a credential that carries a GitHub-verified profile is a sign-in; a
	// script token, access-link secret or legacy dashboard session is not.
	body.GitHubSignedIn = session.profile != nil && session.profile.Handle != ""
	// What this factory is configured to do, said by the factory itself so a
	// client never probes a real GitHub device flow to find out.
	body.GitHubSignInAvailable = h.config.GitHubClientID != ""
	body.InviteAvailable = body.GitHubSignInAvailable && h.config.TokensPath != ""
	body.Machines = machines
	body.Mode, body.Operator = h.mode(), h.isOperator(r)
	if body.Mode == api.FactoryTeam && h.operatorLogin(r) == "" {
		body.Operator = false
	}
	body.Runners, err = h.runnerList(r, handle)
	if h.failed(w, err) {
		return
	}
	body.TakesUnassigned = takes
	h.write(w, http.StatusOK, body)
}

// about is the join preview. It is the one /v1 read that does not require a
// token, and it names no job or repository.
func (h *Handler) about(w http.ResponseWriter, r *http.Request) {
	h.write(w, http.StatusOK, struct {
		Factory       api.Factory `json:"factory"`
		Scheme        string      `json:"scheme"`
		Name          string      `json:"name"`
		CodeafVersion string      `json:"codeaf_version"`
		Contract      int         `json:"contract"`
		Needs         string      `json:"needs"`
		Access        string      `json:"access"`
		PollingEvery  string      `json:"polling_every,omitempty"`
		ServedBy      string      `json:"served_by"`
		// AccessCheckPost is optional and public: POST /v1/factory-access takes
		// verb "check", so a client need not put a link's secret in a URL.
		AccessCheckPost bool `json:"access_check_post"`
	}{h.factory, scheme(r), h.aboutName(), h.factory.Version, api.Contract, h.needsWord(),
		h.accessMode(), h.pollingEvery(), h.servedBy(), true})
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
	return h.factory.Name
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
