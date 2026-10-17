package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"net/http"
	"strings"
)

// repoPower changes one watched repository through the same operator door as the whole list.
// Every refusal before SetRepos carries api.PhaseHeader (serve.go marks the route);
// the marker is cleared immediately before SetRepos, so a failure of the save or of
// the event after it reads as uncertain to a client. Turning a repository off only
// removes it from the watched list: its jobs and checkouts are not touched.
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
	// The read, the edit and the save are one step: a second operator toggling a
	// different repository must read this one's result, not the list before it.
	// Authority was judged above, per request, before any wait here.
	h.reposMu.Lock()
	repos, err := h.config.Store.Repos()
	if h.failed(w, err) {
		h.reposMu.Unlock()
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
	// THE FIRST REQUESTED CHANGE: from here a failure may follow a stored write
	// (the list is saved, then the event is recorded), so it carries no marker.
	clearPhase(w)
	saved := h.seam.SetRepos(next)
	h.reposMu.Unlock()
	if h.failed(w, saved) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.ReposChanged, "repo:"+repo, detail))
}
