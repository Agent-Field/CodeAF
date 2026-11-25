package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
)

func TestLiveHTTPReadUsesBindingAndKeepsStableIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			http.Error(w, "fixture-secret", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/contacts" || r.URL.Query().Get("search") != "Ada & Bob" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"items":[{"key":"42","full_name":"Ada"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"items":[{"key":"42","full_name":"Ada Lovelace"}]}`))
		}
	}))
	t.Cleanup(server.Close)
	svc, store := crm(t)
	cfg := connections.Config{
		ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/contacts",
		Reads: map[string]connections.Read{
			"contacts": {Type: "contact", Key: "key", Fields: map[string]string{"name": "full_name"},
				Path: "/contacts", Rows: "items", Match: map[string]string{"name": "search"}},
		},
	}
	if err := svc.Register(cfg, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	query := connections.Query{App: "crm", Connection: "directory", Read: "contacts", Args: map[string]any{"name": "Ada & Bob"}}
	first, err := svc.Read(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Read(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 1 || len(second.Rows) != 1 || first.Rows[0]["name"] != "Ada" ||
		second.Rows[0]["name"] != "Ada Lovelace" || first.Rows[0]["id"] != second.Rows[0]["id"] ||
		first.Rows[0]["id"] == "42" || second.Source.Residency != "live" || second.Source.ObservedAt.IsZero() {
		t.Fatalf("live read: %+v, %+v", first, second)
	}
	if _, err := store.Get(data.Scope{App: "crm"}, "contact", first.Rows[0]["id"].(string), false); err == nil {
		t.Fatal("a live read copied a record into the native store")
	}
	query.App = "other"
	if _, err := svc.Read(context.Background(), query); err == nil || calls != 2 {
		t.Fatal("an app outside the connection scope could read")
	}
}

func crm(t *testing.T) (connections.Service, *data.Store) {
	t.Helper()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	t.Cleanup(func() { _ = place.Close() })
	store, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishApp(data.AppPublication{
		App: "crm", Version: 1, Metadata: []byte("{}"), Definition: crmManifest(t),
	}, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.BindCommonType(data.CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	return connections.Service{Profile: t.TempDir(), Store: func() (*data.Store, error) { return store, nil }}, store
}
