package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The real CLI door must clear the HTTP handler's map, including when discovery
// names a wildcard listener and a browser has already followed the login redirect.
func TestFactoryRevokeSessionsCommandSignsOutRemoteBrowser(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	h := serve.New(factory.FixtureSeam(time.Now()), serve.Config{AccessToken: "test-secret"})
	defer h.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "192.0.2.1:1234"
		h.ServeHTTP(w, r)
	}))
	defer s.Close()
	address, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	address.Host = net.JoinHostPort("0.0.0.0", address.Port())
	body, err := json.Marshal(factoryDiscovery{URL: address.String(), PID: os.Getpid(), Token: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFactoryPrivate(filepath.Join(v3FactoryRoot(), "serve.json"), body); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	response, err := client.PostForm(s.URL+"/access", url.Values{"token": {"test-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/" {
		t.Fatalf("browser login redirect = %d at %s", response.StatusCode, response.Request.URL)
	}
	queueStatus := func(want int) {
		t.Helper()
		response, err := client.Get(s.URL + "/v1/queue")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("browser queue = %d, want %d", response.StatusCode, want)
		}
	}
	queueStatus(http.StatusOK)
	if err := runFactory([]string{"revoke-sessions"}); err != nil {
		t.Fatal(err)
	}
	queueStatus(http.StatusUnauthorized)
	if err := runFactory([]string{"revoke-sessions", "extra"}); err == nil {
		t.Fatal("revocation accepts extra arguments")
	}
}

func TestFactoryRevokeSessionsRequiresRunningService(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if err := revokeFactorySessions(io.Discard); err == nil {
		t.Fatal("revocation succeeded without service discovery")
	}
	body, err := json.Marshal(factoryDiscovery{URL: "http://127.0.0.1:7420", PID: 0, Token: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFactoryPrivate(filepath.Join(v3FactoryRoot(), "serve.json"), body); err != nil {
		t.Fatal(err)
	}
	if err := revokeFactorySessions(io.Discard); err == nil {
		t.Fatal("revocation accepted stale discovery")
	}
}
