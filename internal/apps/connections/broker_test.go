package connections_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestHTTPWriteWaitsForApprovalAndPersistsAReceipt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"committed", http.StatusOK, "committed"},
		{"uncertain", http.StatusInternalServerError, "unknown"},
		{"conflict", http.StatusConflict, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, key := 0, ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`[{"key":42,"rev":"1","full_name":"Ada"}]`))
					return
				}
				calls++
				key = r.Header.Get("Idempotency-Key")
				var body map[string]any
				if r.Method != http.MethodPost || r.URL.Path != "/edit" || r.Header.Get("Authorization") != "Bearer a-test-token" ||
					json.NewDecoder(r.Body).Decode(&body) != nil || body["key"] != float64(42) || body["rev"] != "1" || body["full_name"] != "Ada Lovelace" {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"ok":true,"private":"a-test-token"}`))
			}))
			t.Cleanup(server.Close)
			svc, _ := crm(t)
			cfg := connections.Config{
				ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/contacts",
				Reads: map[string]connections.Read{"contacts": {
					Type: "contact", Key: "key", Version: "rev", Fields: map[string]string{"name": "full_name"}, Path: "/contacts",
				}},
				Writes: map[string]connections.Write{"edit": {
					Read: "contacts", Fields: []string{"name"}, Path: "/edit", Response: map[string]string{"saved": "ok"},
				}},
			}
			if err := svc.Register(cfg, "a-test-token"); err != nil {
				t.Fatal(err)
			}
			first, err := svc.Read(context.Background(), contactsQuery())
			if err != nil || len(first.Rows) != 1 {
				t.Fatalf("contacts: %+v, %v", first, err)
			}
			runCfg := crmRunConfig(t, svc)
			run := startEdit(t, runCfg, first.Rows[0].ID(), "1", func() {
				if calls != 0 {
					t.Fatal("the POST ran before approval")
				}
			})
			checkReceipt(t, run, tc.want)
			if calls != 1 || key == "" || run.Effects[0].Key != key {
				t.Fatalf("POST count %d, key %q, run %+v", calls, key, run)
			}
			raw, err := json.Marshal(run)
			if err != nil || strings.Contains(string(raw), "a-test-token") {
				t.Fatal("the run exposes the connection credential")
			}
		})
	}
}

func TestHTTPReadDoesNotFollowRedirectOrSurfaceRemoteErrors(t *testing.T) {
	var redirected, noAuth int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(destination.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/check":
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		default:
			noAuth++
			http.Error(w, "a-test-token", http.StatusUnauthorized)
		}
	}))
	t.Cleanup(server.Close)
	svc, _ := crm(t)
	cfg := connections.Config{ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/check",
		Reads: map[string]connections.Read{"contacts": {Type: "contact", Key: "key", Fields: map[string]string{"name": "name"}, Path: "/contacts"}},
	}
	if err := svc.Register(cfg, "a-test-token"); err != nil {
		t.Fatal(err)
	}
	err := svc.Test(context.Background(), "crm", "directory")
	if err == nil || redirected != 0 {
		t.Fatal("connection test followed the redirect")
	}
	if _, err := svc.Read(context.Background(), contactsQuery()); err == nil || strings.Contains(err.Error(), "a-test-token") || noAuth != 1 {
		t.Fatalf("read error %v from %d calls", err, noAuth)
	}
}

func contactsQuery() connections.Query {
	return connections.Query{App: "crm", Connection: "directory", Read: "contacts"}
}

func crmRunConfig(t *testing.T, svc connections.Service) runs.Config {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "crm", "current")
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "crm", "meta.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), crmManifest(t), 0o600); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile("testdata/edit-contact.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runs", "edit-contact.yaml"), definition, 0o600); err != nil {
		t.Fatal(err)
	}
	files := runs.Files(root)
	tools := runs.ConnectedTools{Apps: files.App, Sources: svc}
	return runs.Config{Root: t.TempDir(), Load: files.Load, Connector: tools, CheckTool: tools.Check}
}

func startEdit(t *testing.T, cfg runs.Config, id, version string, beforeApproval func()) runs.Run {
	t.Helper()
	engine, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	run, err := engine.Start("crm", "edit-contact", map[string]any{"id": id, "version": version, "name": "Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := engine.Get(run.ID)
	if err != nil || pending.State != runs.YourCall || pending.Approval == nil || len(pending.Effects) != 0 {
		t.Fatalf("approval: %+v, %v", pending, err)
	}
	beforeApproval()
	if err := engine.Answer(run.ID, pending.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err := reopened.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return finished
}

func checkReceipt(t *testing.T, run runs.Run, outcome string) {
	t.Helper()
	state := runs.Incomplete
	if outcome == "committed" {
		state = runs.Done
	}
	if run.State != state || len(run.Effects) != 1 || run.Effects[0].Outcome != outcome ||
		run.Effects[0].Connection != "directory" || run.Effects[0].Key == "" {
		t.Fatalf("wanted %s receipt, got %+v", outcome, run)
	}
}

func crmManifest(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile("testdata/crm-app.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(source, &manifest); err != nil {
		t.Fatal(err)
	}
	caps := manifest["capabilities"].(map[string]any)
	caps["tools"] = []string{"connections.directory.edit"}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
