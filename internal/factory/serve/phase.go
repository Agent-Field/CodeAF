package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// markPreMutation is true for the routes whose handlers clear the phase marker
// (api.PhaseHeader) before their first requested change. ONLY these routes
// carry it: a route that is not listed here, or a handler that forgets to call
// clearPhase, can never claim a refusal wrote nothing, so a new mutating route
// is uncertain-on-failure to its clients until it is added deliberately.
func markPreMutation(r *http.Request) bool {
	// The factory's own copy of one repository's steps, saved or stopped:
	// exactly /v1/repos/{owner}/{repo}/pipeline/factory-copy.
	if r.Method == http.MethodPut || r.Method == http.MethodDelete {
		if rest, ok := strings.CutPrefix(r.URL.Path, "/v1/repos/"); ok {
			if name, ok := strings.CutSuffix(rest, "/pipeline/factory-copy"); ok {
				owner, repo, two := strings.Cut(name, "/")
				if two && owner != "" && repo != "" && !strings.Contains(repo, "/") {
					return true
				}
			}
		}
	}
	switch r.Method {
	case http.MethodPost:
		return r.URL.Path == "/v1/access/links" || r.URL.Path == "/v1/access/revoke-sessions"
	case http.MethodPut:
		// The factory's own rules: exactly /v1/rules.
		if r.URL.Path == "/v1/rules" {
			return true
		}
		// One runner's settings: exactly /v1/runners/{runner}.
		if rest, ok := strings.CutPrefix(r.URL.Path, "/v1/runners/"); ok {
			return rest != "" && !strings.Contains(rest, "/")
		}
		// One repository's power switch: exactly /v1/repos/{owner}/{repo}. The
		// whole-list door (PUT /v1/repos) and anything deeper are not marked.
		rest, ok := strings.CutPrefix(r.URL.Path, "/v1/repos/")
		owner, repo, two := strings.Cut(rest, "/")
		return ok && two && owner != "" && repo != "" && !strings.Contains(repo, "/")
	case http.MethodDelete:
		rest, ok := strings.CutPrefix(r.URL.Path, "/v1/access/links/")
		if ok && rest != "" && !strings.Contains(rest, "/") {
			return true
		}
		rest, ok = strings.CutPrefix(r.URL.Path, "/v1/me/sessions/")
		return ok && rest != "" && rest != "signout-others" && !strings.Contains(rest, "/")
	}
	return false
}

// clearPhase is called immediately before a handler's first requested change.
func clearPhase(w http.ResponseWriter) { w.Header().Del(api.PhaseHeader) }
