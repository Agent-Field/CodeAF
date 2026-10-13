package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAppFactorySoloSharesListenerAndTerminalDiscovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	credentials := config.NewAppCredentials()
	restore := config.UseAppCredentials(credentials)
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := appapi.New("launch-token", appapi.Options{Context: ctx})
	server := httptest.NewServer(transport)
	defer server.Close()
	defer transport.Close()
	host, err := newAppFactoryHost(ctx, root, server.URL, "launch-token", credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	if err := transport.BindFactories(&appapi.FactoryRegistry{Home: root, URL: server.URL, Token: "launch-token", Credentials: credentials, Solo: host.mountSolo, Power: host.power}); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("POST", server.URL+"/app/v1/factories/solo", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer launch-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatal(response.StatusCode)
	}
	var row appapi.Factory
	_ = json.NewDecoder(response.Body).Decode(&row)
	if row.URL != server.URL || row.State != "ok" {
		t.Fatal(row)
	}
	target, ok := liveFactoryDiscovery()
	if !ok || target.url != server.URL || target.token != "launch-token" {
		t.Fatal(target, ok)
	}
	discovery, err := os.ReadFile(filepath.Join(root, "v3", "factory", "serve.json"))
	if err != nil || strings.Contains(string(discovery), "launch-token") {
		t.Fatal("app launch bearer was persisted", err)
	}
	response, err = http.Get(server.URL + "/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
}

func TestAppFactoryProxyKeepsIssuedRunnerCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	host := &appFactoryHost{ctx: ctx, floor: floor, token: "owner-token"}
	handler, err := host.solo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.closeSoloForTest()
	owner := httptest.NewServer(handler)
	defer owner.Close()
	proxy := httptest.NewServer(appapi.Guard("app-token", appFactoryProxy(factoryTarget{owner.URL, "owner-token"})))
	defer proxy.Close()
	worker := client.Open(proxy.URL, "app-token").Worker("")
	if _, err := worker.Attach(context.Background(), api.WorkerAttach{Machine: "this-machine", Name: "This machine", Handle: "person", Slots: 1}); err != nil {
		t.Fatal(err)
	}
	if worker.Credential() == "" {
		t.Fatal("runner was not registered")
	}
	if err := appFactoryRequest(context.Background(), factoryTarget{proxy.URL, worker.Credential()}, "POST", "/v1/runners/presence", api.RunnerPresence{Asleep: true}, nil); err != nil {
		t.Fatal("proxy replaced the runner credential:", err)
	}
	var runners api.Runners
	if err := appFactoryRequest(context.Background(), factoryTarget{proxy.URL, "app-token"}, "GET", "/v1/runners", nil, &runners); err != nil {
		t.Fatal(err)
	}
	if len(runners.Runners) != 1 || runners.Runners[0].State != api.RunnerAsleep {
		t.Fatal(runners)
	}
}

// This fixture starts only the service, leaving election and model work absent.
func (h *appFactoryHost) closeSoloForTest() {
	if h.closeDiscovery != nil {
		h.closeDiscovery()
		h.closeDiscovery = nil
	}
	if h.handler != nil {
		h.handler.Close()
		h.handler = nil
	}
	if h.release != nil {
		h.release()
		h.release = nil
	}
}

func TestAppFactoryMountTakesOverWhenTheSoloHostCloses(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owner := &appFactoryHost{ctx: ctx, floor: floor, token: "first-token"}
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { owner.handler.ServeHTTP(w, r) }))
	defer first.Close()
	owner.address = first.URL
	if _, err := owner.solo(ctx); err != nil {
		t.Fatal(err)
	}
	defer owner.closeSoloForTest()
	next := &appFactoryHost{ctx: ctx, floor: floor, token: "second-token"}
	mount, err := next.mountSolo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer next.closeSoloForTest()
	second := httptest.NewServer(appapi.Guard(next.token, mount))
	defer second.Close()
	next.address = second.URL
	for i := 0; i < 2; i++ {
		var me api.Me
		if err := appFactoryRequest(context.Background(), factoryTarget{second.URL, next.token}, "GET", "/v1/me", nil, &me); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if next.handler != nil {
				t.Fatal("second app started another solo service")
			}
			first.Close()
			owner.closeSoloForTest()
		} else if target, ok := liveFactoryDiscovery(); !ok || target.url != second.URL || target.token != next.token {
			t.Fatal("surviving app did not take over", target, ok)
		}
	}
}
func TestAppSharesSessionsWithElectedTerminalRunnerWithoutDiskSecrets(t *testing.T) {
	root := t.TempDir()
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	release, ok := floor.TryWorker()
	if !ok {
		t.Fatal("runner lock unavailable")
	}
	defer release()
	owner := &appFactoryHost{ctx: context.Background(), floor: floor, credentials: config.NewAppCredentials()}
	closeControl := owner.runnerControl()
	defer closeControl()
	source := config.NewAppCredentials()
	source.SetFactorySession("https://team.example", "only-in-memory")
	app := &appFactoryHost{ctx: context.Background(), floor: floor, credentials: source}
	app.shareRunnerSessions()
	if owner.credentials.FactorySession("https://team.example") != "only-in-memory" {
		t.Fatal("elected runner did not receive app session")
	}
	if otherRelease, ok := floor.TryWorker(); ok {
		otherRelease()
		t.Fatal("second runner elected")
	}
	entries, _ := os.ReadDir(floor.Root())
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			data, _ := os.ReadFile(filepath.Join(floor.Root(), entry.Name()))
			if strings.Contains(string(data), "only-in-memory") {
				t.Fatal("session persisted", entry.Name())
			}
		}
	}
}
func TestAppPowerUsesExistingRunnerCredential(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	states := []bool{}
	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runners/presence" || r.Header.Get("Authorization") != "Bearer elected-credential" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(401)
			return
		}
		var body api.RunnerPresence
		_ = json.NewDecoder(r.Body).Decode(&body)
		states = append(states, body.Asleep)
		w.WriteHeader(200)
	}))
	defer factory.Close()
	data, _ := json.Marshal(map[string][]string{factory.URL: {"elected-credential"}, "http://127.0.0.1:1": {"detached-credential"}})
	if err := writeFactoryPrivate(filepath.Join(floor.Root(), "worker.json"), data); err != nil {
		t.Fatal(err)
	}
	attachment, _ := json.Marshal(map[string]string{"url": factory.URL})
	if err := writeFactoryPrivate(filepath.Join(floor.Root(), "attach.json"), attachment); err != nil {
		t.Fatal(err)
	}
	host := &appFactoryHost{floor: floor}
	for _, asleep := range []bool{true, false} {
		if err := host.power(context.Background(), asleep); err != nil {
			t.Fatal(err)
		}
	}
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatal(states)
	}
}
