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
	switch r.Method {
	case http.MethodPost:
		return r.URL.Path == "/v1/access/links" || r.URL.Path == "/v1/access/revoke-sessions"
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
