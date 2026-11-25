package connections_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSharedSourceKeyCannotCrossAppTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"42","name":"Ada"}]`))
	}))
	t.Cleanup(server.Close)
	svc, store := crm(t)
	if err := store.BindCommonType(data.CommonOrganization, "company"); err != nil {
		t.Fatal(err)
	}
	cfg := connections.Config{ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/check",
		Reads: map[string]connections.Read{
			"contacts":  {Type: "contact", Key: "key", Fields: map[string]string{"name": "name"}, Path: "/same"},
			"companies": {Type: "company", Key: "key", Fields: map[string]string{"name": "name"}, Path: "/same"},
		},
		Writes: map[string]connections.Write{"company": {Read: "companies", Fields: []string{"name"}, Path: "/write"}},
	}
	if err := svc.Register(cfg, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	query := contactsQuery()
	person, err := svc.Read(context.Background(), query)
	if err != nil || len(person.Rows) != 1 {
		t.Fatalf("contact: %+v %v", person, err)
	}
	query.Read = "companies"
	if page, err := svc.Read(context.Background(), query); err == nil {
		t.Errorf("company read reused a contact identity: %+v", page)
	}
	if _, err := svc.PrepareEffect("crm", "directory", "company", map[string]any{
		"id": person.Rows[0].ID(), "set": map[string]any{"name": "Wrong type"},
	}); err == nil {
		t.Error("company effect accepted a contact identity")
	}
}

func TestConnectionDeclarationKeepsCredentialsOnTheConfiguredOrigin(t *testing.T) {
	svc, _ := crm(t)
	for _, tc := range []struct {
		name   string
		origin string
		path   string
	}{
		{"plaintext remote", "http://example.test", "/contacts"},
		{"credentials in origin", "https://user:password@example.test", "/contacts"},
		{"absolute request target", "https://example.test", "//other.test/contacts"},
		{"embedded query", "https://example.test", "/contacts?other=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := connections.Config{ID: "directory", App: "crm", Kind: connections.HTTP,
				Origin: tc.origin, TestPath: "/check", Reads: map[string]connections.Read{"contacts": {
					Type: "contact", Path: tc.path, Key: "key", Fields: map[string]string{"name": "full_name"},
				}},
			}
			if err := svc.Register(cfg, "fixture-secret"); err == nil || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("registration accepted an unsafe request target or exposed the credential")
			}
		})
	}
	list, err := svc.List("crm")
	if err != nil || len(list) != 0 {
		t.Fatalf("invalid declaration was saved: %+v %v", list, err)
	}
}

func TestHTTPReadRejectsAnOversizedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", (1<<20)+1)))
	}))
	t.Cleanup(server.Close)
	svc, _ := crm(t)
	cfg := connections.Config{ID: "directory", Kind: connections.HTTP, App: "crm", Origin: server.URL, TestPath: "/check",
		Reads: map[string]connections.Read{"contacts": {Type: "contact", Key: "key", Fields: map[string]string{"name": "name"}, Path: "/contacts"}},
	}
	if err := svc.Register(cfg, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	if page, err := svc.Read(context.Background(), contactsQuery()); !errors.Is(err, connections.ErrTooLarge) || len(page.Rows) != 0 {
		t.Fatalf("oversized response: %+v %v", page, err)
	}
}
