package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func operatorSecret(t *testing.T, link api.AccessLink) string {
	t.Helper()
	u, err := url.Parse(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if !link.Operator || u.Query().Get("operator") != "true" || fragment.Get("token") == "" {
		t.Fatalf("operator link: %+v", link)
	}
	return fragment.Get("token")
}

func TestOperatorClaimDeviceSignInAndRecovery(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	if err := os.Remove(filepath.Join(f.st.Root(), "operator.json")); err != nil {
		t.Fatal(err)
	}
	g := &fakeGitHub{t: t, login: "owner"}
	f.h.config.GitHubClientID, f.h.config.GitHubHTTPClient = "client", g.client()
	f.h.config.OperatorLogin = "imposter"
	f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
	if f.h.operatorLogin(nil) != "" {
		t.Fatal("host identity became operator without a claim")
	}
	link, err := f.h.NewOperatorLink(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	gate := operatorSecret(t, link)
	f.request(t, "GET", "/v1/me", gate, nil, 401, nil)
	signed := signInHandle(t, f.h, g, &f.now, "owner", gate)
	var me api.Me
	f.request(t, "GET", "/v1/me", signed, nil, 200, &me)
	if !me.Operator || me.Profile.Handle != "owner" {
		t.Fatalf("claim: %+v", me)
	}
	f.request(t, "POST", "/v1/auth/device", gate, nil, 401, nil)
	f.now = link.ExpiresAt
	f.request(t, "GET", "/v1/me", signed, nil, 200, &me)
	if !me.Operator {
		t.Fatal("operator session expired with its invitation")
	}
	f.h.Close()
	f.cfg.OperatorLogin = "other-host"
	f.cfg.GitHubClientID, f.cfg.GitHubHTTPClient = "client", g.client()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	f.request(t, "GET", "/v1/me", signed, nil, 200, &me)
	if !me.Operator {
		t.Fatal("operator identity lost at restart")
	}
	if link, err := f.h.NewOperatorLink(context.Background(), false); err != nil || link.URL != "" {
		t.Fatalf("printed after claim: %+v %v", link, err)
	}
	recovery, err := f.h.NewOperatorLink(context.Background(), true)
	if err != nil || recovery.Login != "owner" {
		t.Fatalf("recovery: %+v %v", recovery, err)
	}
	recovered := signInHandle(t, f.h, g, &f.now, "owner", operatorSecret(t, recovery))
	f.request(t, "GET", "/v1/me", recovered, nil, 200, &me)
	if !me.Operator {
		t.Fatal("recovery lost operator")
	}
	events, err := f.cfg.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	claims := 0
	for _, event := range events {
		if event.Kind == "operator_claimed" {
			claims++
			if !event.Actor.Operator || event.Actor.Name != "@owner" || !strings.Contains(event.Detail, "as operator") {
				t.Fatalf("claim Activity: %+v", event)
			}
		}
	}
	if claims != 2 {
		t.Fatalf("claim events: %d", claims)
	}
}

func TestGitHubReadGrantOnlyForOperator(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "owner"}
	f.h.config.GitHubClientID, f.h.config.GitHubHTTPClient = "client", g.client()
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(f.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	owner := signInHandle(t, f.h, g, &f.now, "owner", row.Token)
	f.request(t, "PUT", "/v1/github/grant", row.Token, map[string]string{"token": "github-access-secret"}, 403, nil)
	memberRow, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(f.cfg.TokensPath, memberRow); err != nil {
		t.Fatal(err)
	}
	member := signInHandle(t, f.h, g, &f.now, "member", memberRow.Token)
	f.request(t, "PUT", "/v1/github/grant", member, map[string]string{"token": "github-access-secret"}, 403, nil)
	f.request(t, "PUT", "/v1/github/grant", owner, map[string]string{"token": "github-access-secret"}, 403, nil)
	g.login = "owner"
	raw := f.request(t, "PUT", "/v1/github/grant", owner, map[string]string{"token": "github-access-secret"}, 200, nil)
	if strings.Contains(raw, "github-access-secret") {
		t.Fatal("grant response exposed token")
	}
	var body struct {
		Login     string    `json:"login"`
		GrantedBy string    `json:"granted_by"`
		GrantedAt time.Time `json:"granted_at"`
	}
	f.request(t, "GET", "/v1/github", owner, nil, 200, &body)
	if body.Login != "owner" || body.GrantedBy != "owner" || !body.GrantedAt.Equal(f.now) {
		t.Fatalf("grant metadata: %+v", body)
	}
	grant, err := factorygithub.LoadReadGrant(factorygithub.GrantPath(f.cfg.TokensPath))
	if err != nil || grant.Token != "github-access-secret" {
		t.Fatalf("saved grant: %+v %v", grant, err)
	}
	events, err := f.cfg.Record.Since(context.Background(), time.Time{})
	data, _ := json.Marshal(events)
	if err != nil || strings.Contains(string(data), grant.Token) {
		t.Fatal("Activity exposed grant")
	}
}

func TestReadGrantRejectsClassicWriteScopes(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "owner"}
	transport := g.client().Transport
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if err == nil {
			response.Header.Set("X-OAuth-Scopes", "read:user, repo")
		}
		return response, err
	})}
	if _, err := f.h.readGrantLogin(httptest.NewRequest("PUT", "/v1/github/grant", nil), "github-access-secret"); err == nil || !strings.Contains(err.Error(), "read-only permissions") {
		t.Fatalf("write scopes accepted: %v", err)
	}
	if grant, err := factorygithub.LoadReadGrant(factorygithub.GrantPath(f.cfg.TokensPath)); err != nil || grant.Token != "" {
		t.Fatal("rejected grant was persisted")
	}
}
