package serve

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ORGANIZATION THAT RUNS A RELAY TELLS ITS PEOPLE WHERE IT IS: every signed-in person reads it from GET /v1/org, a stranger
// does not, and an organization that runs none says nothing, so what the server answered before is what it answers now.
func TestTheOrganizationTellsItsPeopleWhereItsRelayIs(t *testing.T) {
	o := newWSOrg(t)

	var none api.OrgProfile
	o.do(t, "abir", "GET", "/v1/org", -1, nil, 200, &none)
	if none.Relay != "" {
		t.Fatalf("an organization with no relay named one: %+v", none)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/org", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Authorization", "Bearer "+o.tokens["abir"])
	o.h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "relay") {
		t.Fatalf("with no relay named the answer must be what it always was: %s", rec.Body)
	}

	o.h.config.RelayURL = "https://relay.example.com"
	var told api.OrgProfile
	o.do(t, "abir", "GET", "/v1/org", -1, nil, 200, &told)
	if told.Relay != "https://relay.example.com" || told.Name == "" {
		t.Fatalf("a member reads %+v", told)
	}

	// Someone who is not signed in learns nothing of it: the relay is for the organization's people.
	anon := httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/org", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	o.h.ServeHTTP(anon, req)
	if anon.Code != 401 && anon.Code != 403 || strings.Contains(anon.Body.String(), "relay.example.com") {
		t.Fatalf("a stranger asking for the organization: %d %s", anon.Code, anon.Body)
	}
}
