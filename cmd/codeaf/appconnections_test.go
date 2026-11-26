package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
)

func TestAppConnectionsCLIAndWireUseTheSameDeclaredRead(t *testing.T) {
	backend, _ := dataBackend(t)
	backend.profileDir = t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-private-token" {
			http.Error(w, "fixture-private-token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/check":
			w.WriteHeader(http.StatusNoContent)
		case "/contacts":
			if r.URL.Query().Get("q") != "Ada" {
				http.Error(w, "unexpected match", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[{"pk":"7","full_name":"Ada"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	declaration := connections.Config{
		ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/check", CredentialMode: connections.CredentialPerson,
		Reads: map[string]connections.Read{"contacts": {
			Type: "contact", Path: "/contacts", Key: "pk",
			Fields: map[string]string{"name": "full_name"}, Match: map[string]string{"name": "q"},
		}},
	}
	raw, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var cli bytes.Buffer
	for _, args := range [][]string{
		{"bind", "crm", "contact", "person"},
		{"register", path},
	} {
		if err := appConnectionsCommand(backend, args, strings.NewReader("fixture-private-token\n"), &cli); err != nil {
			t.Fatal(err)
		}
	}
	wire := newActionWire(t, backend)
	for _, tc := range []struct {
		args   []string
		method string
		params map[string]any
	}{
		{[]string{"list", "crm"}, "connections.list", map[string]any{"app": "crm"}},
		{[]string{"test", "crm", "directory"}, "connections.test", map[string]any{"app": "crm", "connection": "directory"}},
		{[]string{"read", "crm", "directory", "contacts", `{"name":"Ada"}`}, "connections.read",
			map[string]any{"app": "crm", "connection": "directory", "read": "contacts", "args": map[string]any{"name": "Ada"}}},
	} {
		cli.Reset()
		if err := appConnectionsCommand(backend, tc.args, strings.NewReader(""), &cli); err != nil {
			t.Fatal(err)
		}
		wireResult, err := wire.call(tc.method, tc.params)
		if err != nil {
			t.Fatal(err)
		}
		got, want := cli.Bytes(), wireResult
		if tc.method == "connections.read" {
			var cliPage, wirePage connections.Page
			if json.Unmarshal(got, &cliPage) != nil || json.Unmarshal(want, &wirePage) != nil ||
				len(cliPage.Rows) != 1 || len(wirePage.Rows) != 1 || cliPage.Rows[0].ID() != wirePage.Rows[0].ID() ||
				wirePage.Rows[0]["name"] != "Ada" || wirePage.Source.Residency != "live" || wirePage.Source.ObservedAt.IsZero() {
				t.Fatalf("CLI and wire live rows differ: %s, %s", got, want)
			}
		} else if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
			t.Fatalf("CLI and wire differ: %s, %s", got, want)
		}
		if bytes.Contains(got, []byte("fixture-private-token")) || bytes.Contains(want, []byte("fixture-private-token")) {
			t.Fatal("a connection result exposed the profile credential")
		}
	}
	if err := appConnectionsCommand(backend, []string{"credential", "crm", "directory"}, strings.NewReader("revoked-token"), &cli); err != nil {
		t.Fatal(err)
	}
	_, refusal := wire.call("connections.test", map[string]any{"app": "crm", "connection": "directory"})
	if refusal == nil || refusal.Code != "not_allowed" || refusal.Message != "the source refused access with your credential" {
		t.Fatalf("wire source refusal = %+v", refusal)
	}
}
