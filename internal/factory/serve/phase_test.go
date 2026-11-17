package serve

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A REFUSAL MADE BEFORE THE FIRST CHANGE SAYS SO; ONE MADE AFTER IT, OR A
// SUCCESS, DOES NOT. Clients read a 4xx as "nothing was written" only with the
// marker (api.PhaseHeader), because the factory writes and then records the event.
func TestAdminRoutesMarkRefusalsMadeBeforeTheirFirstChange(t *testing.T) {
	f, _ := linkFixture(t)
	marked := func(method, path, body, token string) (int, string) {
		w := identityRequest(f.h, method, path, body, token, nil)
		return w.Code, w.Header().Get(api.PhaseHeader)
	}
	for name, c := range map[string]struct {
		method, path, body, token string
		status                    int
		want                      string
	}{
		"bad login":              {"POST", "/v1/access/links", `{"login":"a b"}`, "operator-secret", 422, api.PhasePreMutation},
		"too many people":        {"POST", "/v1/access/links", `{"max_uses":51}`, "operator-secret", 422, api.PhasePreMutation},
		"unknown link":           {"DELETE", "/v1/access/links/tok_absent", "", "operator-secret", 404, api.PhasePreMutation},
		"unknown session":        {"DELETE", "/v1/me/sessions/session_absent", "", "operator-secret", 404, api.PhasePreMutation},
		"created":                {"POST", "/v1/access/links", `{}`, "operator-secret", 201, ""},
		"a read is not marked":   {"GET", "/v1/access/links", "", "operator-secret", 200, ""},
		"signout-others is not":  {"POST", "/v1/me/sessions/signout-others", "", "operator-secret", -1, ""},
		"revoke-all not allowed": {"POST", "/v1/access/revoke-sessions", "", "no-such-token", 401, api.PhasePreMutation},
	} {
		t.Run(name, func(t *testing.T) {
			status, got := marked(c.method, c.path, c.body, c.token)
			if c.status > 0 && status != c.status {
				t.Fatalf("status %d, want %d", status, c.status)
			}
			if got != c.want {
				t.Fatalf("%s = %q, want %q", api.PhaseHeader, got, c.want)
			}
		})
	}
	// A revoke that removes the row has made its change: no marker on the answer.
	link, _ := f.makeLink(t, api.CreateAccessLink{})
	if status, got := marked("DELETE", "/v1/access/links/"+link.ID, "", "operator-secret"); status != http.StatusOK || got != "" {
		t.Fatalf("revoke = %d %q", status, got)
	}
	// Revoking it again changes nothing, and says so.
	if status, got := marked("DELETE", "/v1/access/links/"+link.ID, "", "operator-secret"); status != http.StatusNotFound || got != api.PhasePreMutation {
		t.Fatalf("second revoke = %d %q", status, got)
	}
}
