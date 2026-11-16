package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
)

func TestAppWireAddsSeparateCustomConnections(t *testing.T) {
	b, _, profile := appProviderTest(t)
	w := wireOver(t, b)
	var hello struct {
		Data appwire.Hello `json:"data"`
	}
	if err := json.Unmarshal([]byte(w.hello), &hello); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(hello.Data.Features, "providers.instances") {
		t.Fatalf("hello missing providers.instances: %s", w.hello)
	}
	first := sourcestub.New("first-model")
	defer first.Close()
	second := sourcestub.New("second-model")
	defer second.Close()
	ids := []string{}
	const key = "local-fixture-secret-12345"
	for i, address := range []string{first.URL(), second.URL()} {
		params := map[string]any{"provider": "custom", "mode": "add", "address": address}
		if i == 1 {
			params["key"] = key
		}
		var check struct {
			OK         bool            `json:"ok"`
			Models     []appwire.Model `json:"models"`
			Connection map[string]any  `json:"connection"`
		}
		w.must("keys.check", params, &check)
		if !check.OK || len(check.Models) != 1 || check.Connection["id"] == nil {
			t.Fatalf("check = %+v", check)
		}
		ids = append(ids, check.Connection["id"].(string))
		params["connection"] = check.Connection
		w.must("keys.set", params, nil)
	}
	if ids[0] == ids[1] {
		t.Fatalf("same identity %v", ids)
	}
	rows := config.PersistedSources(profile)
	if len(rows) != 2 || rows[0].Address != first.URL() || rows[1].Address != second.URL() {
		t.Fatalf("rows = %+v", rows)
	}
	assertNoKeyOnDisk(t, profile, key)
	restore := config.UseAppCredentials(config.NewAppCredentials())
	defer restore()
	reopened := newFakeBackend(newAppRecorder(), nil)
	defer reopened.Close()
	again := wireOver(t, reopened)
	again.must("keys.set", map[string]any{"provider": ids[1], "key": key}, nil)
	var result appwire.ProvidersResult
	again.must("providers.list", map[string]any{}, &result)
	var models struct {
		Models []appwire.Model `json:"models"`
	}
	again.must("models.list", map[string]any{}, &models)
	for _, name := range []string{"first-model", "second-model"} {
		found := false
		for _, m := range models.Models {
			if m.Name == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("reopened models missing %s: %+v", name, models)
		}
	}
	for i, id := range ids {
		found := false
		for _, p := range result.Providers {
			if p.ID == id && p.SignedIn {
				found = true
			}
		}
		if !found {
			t.Fatalf("connection %d missing after reopen: %+v", i, result)
		}
	}
}

func TestAppWireRefusesAnEmptyCatalogAtCheckAndFinalSave(t *testing.T) {
	for _, provider := range []string{"custom", "openrouter"} {
		t.Run(provider, func(t *testing.T) {
			b, _, profile := appProviderTest(t)
			var empty atomic.Bool
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/key" {
					w.Write([]byte(`{"data":{"label":"fixture"}}`))
					return
				}
				if empty.Load() {
					w.Write([]byte(`{"data":[]}`))
				} else {
					w.Write([]byte(`{"data":[{"id":"deepseek/deepseek-v4.1-flash-1017"}]}`))
				}
			}))
			defer endpoint.Close()
			b.keyAddress = func(string) string { return endpoint.URL }
			w := wireOver(t, b)
			params := map[string]any{"provider": provider, "key": "sk-or-v1-fixture-not-a-real-key-1234567890", "address": endpoint.URL}
			if provider == "custom" {
				params["mode"] = "add"
			}
			var check appwire.KeyCheck
			w.must("keys.check", params, &check)
			if !check.OK || len(check.Models) != 1 || (provider == "openrouter" && check.Models[0].ID != "deepseek/deepseek-v4.1-flash-1017") {
				t.Fatalf("check = %+v", check)
			}
			params["connection"] = check.Connection
			empty.Store(true)
			w.mustFail("keys.set", params, appwire.CodeBadParams)
			w.must("keys.check", params, &check)
			if check.OK || check.Status != "empty" {
				t.Fatalf("empty check = %+v", check)
			}
			if len(config.PersistedSources(profile)) != 0 {
				t.Fatal("failed save persisted a connection")
			}
			if _, held := b.keys.Key(provider); held {
				t.Fatal("failed save adopted a credential")
			}
		})
	}
}

func TestAppWireCustomStorageFailureCanRetryWithoutDuplicate(t *testing.T) {
	b, _, profile := appProviderTest(t)
	w := wireOver(t, b)
	endpoint := sourcestub.New("local-model")
	defer endpoint.Close()
	params := map[string]any{"provider": "custom", "mode": "add", "address": endpoint.URL()}
	var check appwire.KeyCheck
	w.must("keys.check", params, &check)
	params["connection"] = check.Connection
	// A directory at the profile file makes the real profile writer fail.
	path := filepath.Join(profile, "config.json")
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, code, _ := w.call("keys.set", params)
	if code == "" {
		t.Fatal("storage failure reported success")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	w.must("keys.set", params, nil)
	if rows := config.PersistedSources(profile); len(rows) != 1 {
		t.Fatalf("retry rows = %+v", rows)
	}
}

func TestAppWireCustomAddCollisionAndReplace(t *testing.T) {
	b, _, profile := appProviderTest(t)
	w := wireOver(t, b)
	first := sourcestub.New("first")
	defer first.Close()
	second := sourcestub.New("second")
	defer second.Close()
	add := map[string]any{"provider": "custom", "mode": "add", "address": first.URL()}
	var stale appwire.KeyCheck
	w.must("keys.check", add, &stale)
	add["connection"] = stale.Connection
	w.must("keys.set", add, nil)
	w.mustFail("keys.set", add, appwire.CodeBadParams)
	replace := map[string]any{"provider": "custom", "mode": "replace", "address": second.URL()}
	var check appwire.KeyCheck
	w.must("keys.check", replace, &check)
	replace["connection"] = check.Connection
	w.must("keys.set", replace, nil)
	if rows := config.PersistedSources(profile); len(rows) != 1 || rows[0].ID != "custom" || rows[0].Address != second.URL() {
		t.Fatalf("replace = %+v", rows)
	}
}
