package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"net/http"
	"strings"
)

// repoPower changes one watched repository through the same operator door as the whole list.
func (h *Handler) repoPower(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.Store != nil && h.seam.SetRepos != nil) {
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	if !store.RepoName(repo) {
		h.fail(w, http.StatusUnprocessableEntity, "repo must be owner/name")
		return
	}
	if !h.repoVisible(r, repo) {
		h.fail(w, http.StatusNotFound, "repository not found")
		return
	}
	var body struct {
		On *bool `json:"on"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.On == nil {
		h.fail(w, http.StatusUnprocessableEntity, "on must be true or false")
		return
	}
	repos, err := h.config.Store.Repos()
	if h.failed(w, err) {
		return
	}
	next := []string{}
	for _, name := range repos {
		if !strings.EqualFold(name, repo) {
			next = append(next, name)
		}
	}
	detail := "turned off " + repo
	if *body.On {
		next = append(next, repo)
		detail = "connected repo " + repo
	}
	if h.failed(w, h.seam.SetRepos(next)) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.ReposChanged, "repo:"+repo, detail))
}
