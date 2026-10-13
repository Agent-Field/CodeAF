package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactorySettingsRoutesUseNativePolicyAndLocalOutput(t *testing.T) {
	s, _, profile := accountFixture(t)
	f, _ := bindTestFactory(t, s, t.TempDir())
	settingsRequest(t, s, "POST", "/app/v1/factories/solo", `{}`, 201)
	settingsRequest(t, s, "GET", "/app/v1/factories/solo", "", 200)
	w := settingsRequest(t, s, "PATCH", "/app/v1/factories/solo", `{"runner_timeout_s":90,"keep_output_days":14,"keep_output_mb":20}`, 200)
	var detail FactorySettingsDetails
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	if detail.RunnerTimeoutSeconds != 90 || detail.KeepOutputDays != 14 || detail.KeepOutputMB != 20 || !detail.Operator {
		t.Fatal(w.Body)
	}
	floor, err := store.Open(filepath.Join(f.Home, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := floor.RunnerSettings()
	if err != nil || policy.RunnerTimeoutSeconds != 90 {
		t.Fatal(policy, err)
	}
	values, _, err := config.AppSettingsAt(profile)
	raw, _ := json.Marshal(values["factory.keep_output_days"])
	if err != nil || string(raw) != "14" {
		t.Fatal(values, err)
	}
	settingsRequest(t, s, "PATCH", "/app/v1/factories/solo", `{"runner_timeout_s":120,"keep_output_days":0}`, 422)
	policy, _ = floor.RunnerSettings()
	if policy.RunnerTimeoutSeconds != 90 {
		t.Fatal("invalid patch partly reached factory")
	}
	settingsRequest(t, s, "POST", "/app/v1/factories/solo/sign-out", `{}`, 409)
	settingsRequest(t, s, "PATCH", "/app/v1/factories/missing", `{"runner_timeout_s":90}`, 404)
}
func TestFactorySettingsTeamRoutesForwardPersonScopedActions(t *testing.T) {
	s, _, _ := accountFixture(t)
	f, _ := bindTestFactory(t, s, t.TempDir())
	calls := map[string]int{}
	var operator atomic.Bool
	operator.Store(true)
	var mu sync.Mutex
	var deletedSession string
	team := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer team-grant-secret" {
			w.WriteHeader(401)
			return
		}
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		calls[key]++
		mu.Unlock()
		switch key {
		case "GET /v1/me":
			write(w, 200, factoryapi.Me{Operator: operator.Load(), SessionID: "person-session", Profile: factoryapi.Profile{Handle: "person"}, Mode: "team"})
		case "GET /v1/settings":
			write(w, 200, store.DefaultRunnerSettings())
		case "GET /v1/queue":
			write(w, 200, factoryapi.Queue{Repos: []factoryapi.Repo{{Name: "org/repo", On: true}}})
		case "GET /v1/access/links":
			write(w, 200, factoryapi.AccessLinks{Links: []factoryapi.AccessLink{{ID: "old", Name: "Team link"}}})
		case "GET /v1/people":
			write(w, 200, factoryapi.People{})
		case "PATCH /v1/settings":
			if !operator.Load() {
				w.WriteHeader(403)
				return
			}
			var body factoryapi.PatchFactorySettings
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.RunnerTimeoutSeconds == nil || *body.RunnerTimeoutSeconds != 90 || body.KeepOutputDays != nil {
				t.Error("wrong upstream policy", body)
			}
			write(w, 200, store.DefaultRunnerSettings())
		case "PUT /v1/repos/org/repo":
			var body map[string]bool
			_ = json.NewDecoder(r.Body).Decode(&body)
			if on, ok := body["on"]; !ok || on {
				t.Error("repo mutation lost enabled false", body)
			}
			write(w, 200, ActionResponse{OK: true})
		case "POST /v1/access/links":
			write(w, 201, map[string]any{"link": factoryapi.AccessLink{ID: "new", URL: "codeaf://factory/join?url=https%3A%2F%2Ffactory.test#token=invitation-secret"}})
		case "DELETE /v1/access/links/new", "DELETE /v1/access/links/old":
			write(w, 200, ActionResponse{OK: true})
		case "DELETE /v1/me/sessions/person-session":
			deletedSession = "person-session"
			write(w, 200, ActionResponse{OK: true})
		default:
			w.WriteHeader(404)
		}
	}))
	defer team.Close()
	f.Credentials.SetFactorySession(team.URL, "team-grant-secret")
	settingsRequest(t, s, "POST", "/app/v1/factories", `{"url":"`+team.URL+`"}`, 201)
	prefix := "/app/v1/factories/" + factoryID(team.URL)
	settingsRequest(t, s, "GET", prefix, "", 200)
	settingsRequest(t, s, "PATCH", prefix, `{"runner_timeout_s":90}`, 200)
	settingsRequest(t, s, "PATCH", prefix+"/repos/org%2Frepo", `{"enabled":false}`, 200)
	settingsRequest(t, s, "PATCH", prefix+"/repos/org%2Frepo", `{}`, 422)
	w := settingsRequest(t, s, "POST", prefix+"/links", `{}`, 201)
	if !strings.Contains(w.Body.String(), "invitation-secret") {
		t.Fatal("new link lost its one-time URL", w.Body)
	}
	settingsRequest(t, s, "DELETE", prefix+"/links", `{"id":"new"}`, 200)
	settingsRequest(t, s, "DELETE", prefix+"/links/old", "", 200)
	operator.Store(false)
	settingsRequest(t, s, "PATCH", prefix, `{"runner_timeout_s":90}`, 403)
	settingsRequest(t, s, "PATCH", prefix, `{"keep_output_days":10}`, 200)
	settingsRequest(t, s, "POST", prefix+"/sign-out", `{}`, 200)
	if deletedSession != "person-session" || f.Credentials.FactorySession(team.URL) != "" {
		t.Fatal("sign-out did not revoke and forget this session")
	}
	w = settingsRequest(t, s, "GET", prefix, "", 200)
	if !strings.Contains(w.Body.String(), `"signed_in":false`) {
		t.Fatal(w.Body)
	}
	saved, _ := os.ReadFile(filepath.Join(f.Home, "v3", "factory", "attach.json"))
	if strings.Contains(string(saved), "secret") {
		t.Fatal("factory action leaked a secret to attachments")
	}
	for _, key := range []string{"PUT /v1/repos/org/repo", "POST /v1/access/links", "DELETE /v1/access/links/new", "DELETE /v1/me/sessions/person-session"} {
		mu.Lock()
		count := calls[key]
		mu.Unlock()
		if count != 1 {
			t.Error(key, count)
		}
	}
	_ = s.RefreshFactories(context.Background())
}
