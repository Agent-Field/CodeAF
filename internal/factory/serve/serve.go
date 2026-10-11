// Package serve exposes the factory seam over HTTP so every client reads the
// same floor without knowing where its store lives.
package serve

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Config is supplied by the host so the handler never reads a person's home
// or guesses the published address from a client-controlled Host header.
type Config struct {
	Address      string
	Version      string
	AccessToken  string
	PollingEvery time.Duration
}

// Handler keeps the seam and host identity together for every response.
type Handler struct {
	seam    factory.Seam
	config  Config
	factory api.Factory
	mux     *http.ServeMux
}

// New binds only reads; nil seam doors answer a refusal rather than panic.
func New(seam factory.Seam, config Config) *Handler {
	if config.Version == "" {
		config.Version = buildinfo.String()
	}
	h := &Handler{seam: seam, config: config,
		factory: api.Factory{Address: config.Address, Version: config.Version}, mux: http.NewServeMux()}
	h.mux.HandleFunc("/v1/queue", h.queue)
	h.mux.HandleFunc("/v1/jobs/{id}", h.job)
	h.mux.HandleFunc("/v1/repos", h.repos)
	h.mux.HandleFunc("/v1/repos/{owner}/{repo}/pipeline", h.pipeline)
	h.mux.HandleFunc("/v1/github", h.github)
	h.mux.HandleFunc("/v1/me", h.me)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, http.StatusNotFound, "factory endpoint not found")
	})
	return h
}

// ServeHTTP checks the peer before routing, so even errors cannot reveal the
// floor to a remote caller without the host's access token.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.allowed(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, "a bearer access token is required")
		return
	}
	// Noncanonical paths are refused here so ServeMux cannot replace the JSON
	// contract with an HTML redirect that carries no factory identity.
	if r.URL.Path != path.Clean(r.URL.Path) {
		h.fail(w, http.StatusNotFound, "factory endpoint not found")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		h.fail(w, http.StatusMethodNotAllowed, "this endpoint accepts GET")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	link, err := h.link(r)
	if h.failed(w, err) {
		return
	}
	q := api.FromSnapshot(s, link.Login)
	q.Factory = h.factory
	h.write(w, http.StatusOK, q)
}

func (h *Handler) job(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		h.fail(w, http.StatusBadRequest, "job id must be a positive integer")
		return
	}
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	for _, it := range s.Items {
		if it.ID == id {
			h.write(w, http.StatusOK, struct {
				Factory api.Factory `json:"factory"`
				api.Job
			}{h.factory, api.FromItem(it)})
			return
		}
	}
	h.fail(w, http.StatusNotFound, "job not found")
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

func (h *Handler) pipeline(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if strings.ContainsAny(owner+repo, "/\\") || owner == "." || owner == ".." || repo == "." || repo == ".." {
		h.fail(w, http.StatusBadRequest, "repository must be owner/name")
		return
	}
	if !h.require(w, h.seam.RecipeAt != nil) {
		return
	}
	recipe, problems, dir, err := h.seam.RecipeAt(owner + "/" + repo)
	if h.failed(w, err) {
		return
	}
	p := api.FromRecipe(recipe)
	// RecipeAt guarantees defaults without a checkout, but does not say
	// whether a known checkout has a file. Leave that unknown source absent.
	if dir == "" {
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
	link, err := h.link(r)
	if h.failed(w, err) {
		return
	}
	// Identity is the connected GitHub login until viewer sessions and worker
	// registration can supply the person's own machines and preferences.
	h.write(w, http.StatusOK, struct {
		Factory         api.Factory     `json:"factory"`
		Handle          string          `json:"handle"`
		Machines        []api.Worker    `json:"machines"`
		TakesUnassigned map[string]bool `json:"takes_unassigned"`
	}{h.factory, link.Login, []api.Worker{}, map[string]bool{}})
}

func (h *Handler) require(w http.ResponseWriter, exists bool) bool {
	if !exists {
		h.fail(w, http.StatusNotImplemented, "this factory does not provide that read")
	}
	return exists
}

func (h *Handler) failed(w http.ResponseWriter, err error) bool {
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
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
