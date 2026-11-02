package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestR43ServeHomeStateCancelsAndJoinsInFlightRound(t *testing.T) {
	celltest.Available(t)
	t.Setenv(config.ProfileDirEnv, "")
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "cluster") {
			t.Error("serve started a cluster member")
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	stateHome := t.TempDir()
	if _, err := identity.Ensure(stateHome); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	join := startServeHomeState(ctx, stateHome, func(err error) { t.Errorf("cancelled round printed: %v", err) })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not start its home-state round")
	}
	cancel()
	joined := make(chan struct{})
	go func() { join(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("serve returned without joining its home-state goroutine")
	}
}

func TestR45DevicesPrintsMachineAliasAndFallsBackToRelayName(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	now := time.Now().Add(-time.Minute)
	for _, known := range []pair.Known{
		{Name: "finch-magnolia-60", Alias: "build-box", Key: "aliased-test-key", Since: now},
		{Name: "otter-lamp-42", Key: "plain-test-key", Since: now},
	} {
		if err := pair.MachineBook().Remember(known); err != nil {
			t.Fatal(err)
		}
	}
	printed := listed(t, bookKind{pair.DeviceBook()})
	for _, want := range []string{"machines this device can reach", "build-box", "otter-lamp-42", "open one with `codeaf chat --at <name>`"} {
		if !strings.Contains(printed, want) {
			t.Errorf("devices lacks %q: %s", want, printed)
		}
	}
	if strings.Contains(printed, "finch-magnolia-60") {
		t.Errorf("alias row uses its relay name: %s", printed)
	}
}

// R4.4 goes through the real engine boot and an HTTP provider request. A daemon
// may already hold its process resources when a serving round writes the key.
func TestR44ServeEngineReadsProviderKeyThatArrivedAfterStartup(t *testing.T) {
	server := newV3ClientDoorServer(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_BASE_URL", server.URL)
	workspace := t.TempDir()
	t.Chdir(workspace)
	freshEngineProcess(t)
	if _, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace, Model: "test/model"}, "", ""); !errors.Is(err, config.ErrNoAPIKey) {
		t.Fatalf("keyless engine boot = %v", err)
	}
	if _, err := os.Stat(config.BudgetConfigPath("")); !os.IsNotExist(err) {
		t.Fatalf("expected a keyless profile at startup: %v", err)
	}
	const arrivedKey = "sk-or-v1-FAKE-arrived-from-vault"
	if err := config.WriteAPIKey("", arrivedKey); err != nil {
		t.Fatal(err)
	}
	engine, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace, Model: "test/model"}, "", "")
	if err != nil {
		t.Fatalf("engine ignored the arrived key: %v", err)
	}
	defer engine.Agent.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events, err := engine.Agent.Submit(ctx, "say done")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if got := server.call(t, 0).authorization; got != "Bearer "+arrivedKey {
		t.Fatal("engine did not use the newly saved provider key")
	}
}
