package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestFactoryAccessAdapterRetainsSessionAndAttachment(t *testing.T) {
	s := New(testToken, Options{})
	f, _ := bindTestFactory(t, s, t.TempDir())
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/factory-access":
			out := factoryapi.FactoryAccess{State: "ready", Factory: factoryapi.AccessFactory{Name: "Team", Repos: []string{"org/repo"}}}
			if r.Method == "POST" {
				http.SetCookie(w, &http.Cookie{Name: "factory_join", Value: "join-secret", Path: "/v1/factory-access", HttpOnly: true, Secure: true})
				out.State = "waiting"
			} else {
				if cookie, err := r.Cookie("factory_join"); err != nil || cookie.Value != "join-secret" {
					t.Error("sign-in cookie was not retained")
				}
				out.State = "joined"
				out.AccessToken = "remote-session-secret"
			}
			write(w, 200, out)
		case "/v1/me":
			if r.Header.Get("Authorization") != "Bearer remote-session-secret" {
				t.Error("session was not retained")
			}
			write(w, 200, factoryapi.Me{Factory: factoryapi.Factory{Name: "Team"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	f.Client = remote.Client()
	link := "codeaf://factory/join?url=" + url.QueryEscape(remote.URL) + "#token=link-secret"
	body := `{"link":"` + link + `","verb":"sign_in"}`
	w := request(t, s, "POST", "/v1/factory-access", body, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"waiting"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, s, "GET", "/v1/factory-access?link="+url.QueryEscape(link), "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"joined"`) || strings.Contains(w.Body.String(), "remote-session-secret") || strings.Contains(w.Body.String(), "access_token") {
		t.Fatal(w.Code, w.Body.String())
	}
	if f.Credentials.FactorySession(remote.URL) != "remote-session-secret" {
		t.Fatal("remote session missing")
	}
	rows, err := f.connections(nil)
	if err != nil || len(rows) != 1 || rows[0].URL != remote.URL {
		t.Fatalf("attachment: %+v %v", rows, err)
	}
}

func TestFactoryTeamSetupUsesNamedServerAndLocalCheckout(t *testing.T) {
	s := New(testToken, Options{})
	f, _ := bindTestFactory(t, s, t.TempDir())
	dirs := map[string]string{}
	f.SetupCheckouts = func() (map[string]string, error) { return dirs, nil }
	f.SetupCheckout = func(_ context.Context, repo, path string, clone bool) (string, error) {
		if repo != "org/repo" || path != "/local/repo" || clone {
			t.Error("wrong local checkout request")
		}
		dirs[repo] = path
		return path, nil
	}
	posts := 0
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer team-secret" {
			t.Error("team session missing")
		}
		if r.Method == "POST" {
			posts++
		}
		if r.URL.Path == "/v1/factory-setup/local" {
			path := "/server/repo"
			write(w, 200, factoryapi.FactorySetup{ID: "local", Mode: "team", State: "checkouts", Repos: []factoryapi.SetupRepo{{ID: "org/repo", FullName: "org/repo", Selected: true, Clone: "found", Checkout: &path}}})
			return
		}
		w.WriteHeader(404)
	}))
	defer remote.Close()
	f.Client = remote.Client()
	f.Credentials.SetFactorySession(remote.URL, "team-secret")
	id := factoryID(remote.URL)
	_, err := f.connections(func(saved *factoryConnections) error {
		saved.Factories = append(saved.Factories, factoryConnection{id, remote.URL, "team"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/factory-setup/" + id
	w := request(t, s, "GET", path, "", bearer(), "127.0.0.1:4000")
	var view factoryapi.FactorySetup
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.ID != id || view.Repos[0].Checkout != nil || view.Repos[0].Clone != "missing" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, s, "POST", path, `{"verb":"checkout","repo_id":"org/repo","path":"/local/repo"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Repos[0].Checkout == nil || *view.Repos[0].Checkout != "/local/repo" || posts != 0 {
		t.Fatal(w.Code, w.Body.String(), posts)
	}
	w = request(t, s, "POST", path, `{"verb":"done"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.State != "done" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, s, "POST", path, `{"verb":"connect","source":"token","token":"must-not-write"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 || posts != 0 {
		t.Fatal("team setup attempted solo connection")
	}
}
