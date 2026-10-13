package appapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCanonicalAccountPostsReturnReceiptsAndPollPreservesClaim(t *testing.T) {
	s, source, _ := accountFixture(t)
	source.Set("github", "github-memory-key")
	source.Set("google", "google-memory-key")
	for _, tc := range []struct{ path, body string }{
		{"/app/v1/accounts/github/access", `{"access":"yes"}`},
		{"/app/v1/accounts/google/access/mail-read", `{"access":"off"}`},
		{"/app/v1/accounts/google/access/mail-send", `{"access":"ask_first"}`},
	} {
		w := settingsRequest(t, s, "POST", tc.path, tc.body, 200)
		if strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
			t.Fatal("wrong mutation receipt", w.Body)
		}
	}
	settingsRequest(t, s, "POST", "/app/v1/accounts/google/access/mail-send", `{"access":"yes"}`, 422)
	settingsRequest(t, s, "POST", "/app/v1/accounts/google/access/missing", `{"access":"off"}`, 404)
	settingsRequest(t, s, "POST", "/app/v1/accounts/github/access", `{"access":"invalid"}`, 422)
	s.accounts.Lock()
	s.accounts.issued["google"] = "one-time-service-credential"
	s.accounts.Unlock()
	w := settingsRequest(t, s, "POST", "/app/v1/accounts/google/poll", `{}`, 200)
	if strings.TrimSpace(w.Body.String()) != `{"state":"connected"}` {
		t.Fatal(w.Body)
	}
	claim := settingsRequest(t, s, "GET", "/app/v1/accounts/google/connect", "", 200)
	if !strings.Contains(claim.Body.String(), "one-time-service-credential") {
		t.Fatal("poll consumed the native claim")
	}
	settingsRequest(t, s, "POST", "/app/v1/accounts/google/disconnect", `{}`, 200)
	w = settingsRequest(t, s, "POST", "/app/v1/accounts/google/poll", `{}`, 200)
	if strings.TrimSpace(w.Body.String()) != `{"state":"revoked"}` {
		t.Fatal(w.Body)
	}
	settingsRequest(t, s, "POST", "/app/v1/accounts/google/disconnect", `{"access":"off"}`, 422)
}

func TestCanonicalMCPCreateSavesNameAndAccessAtomically(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "test"}, nil)
	host := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer host.Close()
	s, _, profile := accountFixture(t)
	w := settingsRequest(t, s, "POST", "/app/v1/mcp", `{"name":"My tools","address":"`+host.URL+`","access":"off"}`, 201)
	var row McpServer
	if json.Unmarshal(w.Body.Bytes(), &row) != nil || row.Name != "My tools" || row.Access != ServiceOff || row.State != "connected" {
		t.Fatal(w.Body)
	}
	w = settingsRequest(t, s, "POST", "/app/v1/mcp/"+row.ID+"/access", `{"access":"yes"}`, 200)
	if strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
		t.Fatal(w.Body)
	}
	settingsRequest(t, s, "POST", "/app/v1/mcp", `{"name":"Bad","address":"`+host.URL+`/other","access":"invalid"}`, 422)
	rows, err := config.AppMCPServersAt(profile)
	if err != nil || len(rows) != 1 || rows[0].Name != "My tools" {
		t.Fatal("invalid creation saved a partial server", rows, err)
	}
	values, _, err := config.AppSettingsAt(profile)
	if err != nil || values["accounts.mcp:"+row.ID] != "yes" {
		t.Fatal("canonical policy did not persist")
	}
}

func TestCanonicalFactorySettingsAndOperatorPosts(t *testing.T) {
	s, _, _ := accountFixture(t)
	f, _ := bindTestFactory(t, s, t.TempDir())
	settingsRequest(t, s, "POST", "/app/v1/factories/solo", `{}`, 201)
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/advanced", `{"runner_timeout_s":90}`, 200)
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/runner", `{"keep_output_days":14,"keep_output_mb":20}`, 200)
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/advanced", `{"keep_output_days":2}`, 422)
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/runner", `{"runner_timeout_s":90}`, 422)
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/runner", `{}`, 422)
	detail := settingsRequest(t, s, "GET", "/app/v1/factories/solo/settings", "", 200)
	if !strings.Contains(detail.Body.String(), `"runner_timeout_s":90`) || !strings.Contains(detail.Body.String(), `"keep_output_days":14`) {
		t.Fatal(detail.Body)
	}
	var operator atomic.Bool
	operator.Store(true)
	var mu sync.Mutex
	calls := map[string]int{}
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer canonical-team-key" {
			w.WriteHeader(401)
			return
		}
		key := r.Method + " " + r.URL.Path
		switch key {
		case "GET /v1/me":
			write(w, 200, factoryapi.Me{Operator: operator.Load(), Mode: "team", SessionID: "session", Profile: factoryapi.Profile{Handle: "person"}})
		case "GET /v1/settings":
			write(w, 200, store.DefaultRunnerSettings())
		case "GET /v1/queue":
			write(w, 200, factoryapi.Queue{Repos: []factoryapi.Repo{{Name: "org/repo", On: true}}})
		case "GET /v1/access/links":
			write(w, 200, factoryapi.AccessLinks{Links: []factoryapi.AccessLink{}})
		case "GET /v1/people":
			write(w, 200, factoryapi.People{})
		default:
			if !operator.Load() {
				w.WriteHeader(403)
				return
			}
			mu.Lock()
			calls[key]++
			mu.Unlock()
			switch key {
			case "POST /v1/access/links":
				write(w, 201, map[string]any{"link": factoryapi.AccessLink{ID: "new", Name: "Team link", URL: "codeaf://factory/join#token=new-secret"}})
			case "PUT /v1/repos/org/repo", "DELETE /v1/access/links/new", "POST /v1/access/revoke-sessions", "DELETE /v1/me/sessions/session":
				write(w, 200, ActionResponse{OK: true})
			default:
				w.WriteHeader(404)
			}
		}
	}))
	defer team.Close()
	f.Credentials.SetFactorySession(team.URL, "canonical-team-key")
	settingsRequest(t, s, "POST", "/app/v1/factories", `{"url":"`+team.URL+`"}`, 201)
	prefix := "/app/v1/factories/" + factoryID(team.URL)
	settingsRequest(t, s, "GET", prefix+"/settings", "", 200)
	settingsRequest(t, s, "POST", prefix+"/repos/org%2Frepo", `{"enabled":false}`, 200)
	link := settingsRequest(t, s, "POST", prefix+"/links", `{}`, 201)
	var created FactoryLinkResponse
	if json.Unmarshal(link.Body.Bytes(), &created) != nil || created.ID != "new" || created.Name != "Team link" || created.URL == "" {
		t.Fatal(link.Body)
	}
	settingsRequest(t, s, "POST", prefix+"/links/new", `{}`, 200)
	settingsRequest(t, s, "POST", prefix+"/revoke-sessions", `{}`, 200)
	operator.Store(false)
	settingsRequest(t, s, "POST", prefix+"/revoke-sessions", `{}`, 403)
	operator.Store(true)
	settingsRequest(t, s, "POST", prefix+"/sign-out", `{}`, 200)
	for _, key := range []string{"PUT /v1/repos/org/repo", "DELETE /v1/access/links/new", "POST /v1/access/revoke-sessions", "DELETE /v1/me/sessions/session"} {
		mu.Lock()
		count := calls[key]
		mu.Unlock()
		if count != 1 {
			t.Fatalf("%s ran %d times", key, count)
		}
	}
}
