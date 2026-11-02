package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// DESIGN section 2: only a paired, sync-enabled serve opens a task door; its
// roster is the identity directory, independent of machine-access grants.
func TestServeTasksUsesThePairedIdentityRoster(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	profile := t.TempDir()
	id, err := identity.Ensure(profile)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.LoadDevice(profile)
	if err != nil {
		t.Fatal(err)
	}
	state := directory.NewMemory(time.Now)
	active, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	client := state.For(dev.ID())
	for _, d := range []identity.Dev{dev, active, removed} {
		if err := client.PutDevice(context.Background(), d.ID(), directory.Device{V: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Revoke(context.Background(), removed.ID()); err != nil {
		t.Fatal(err)
	}
	// This fixture needs the actual signed directory wire, including its
	// per-device revocation field, but no provider or remote service.
	server := httptest.NewServer(directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return state, nil }))
	t.Cleanup(server.Close)
	config, ok, err := serveTasksConfig(profile, server.URL)
	if runtime.GOOS == "windows" {
		if ok {
			t.Fatal("Windows offered a task door")
		}
		return
	}
	if err != nil || !ok {
		t.Fatalf("paired: %t, %v", ok, err)
	}
	if config.Root != filepath.Join(profile, "v3", "taskdoor") || config.Service != server.URL {
		t.Fatalf("wrong attempt home: %+v", config)
	}
	roster, err := config.Roster(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !roster[active.ID()] || roster[removed.ID()] {
		t.Fatalf("roster ignored revocation: %v", roster)
	}
	if _, err := os.Stat(filepath.Join(profile, "v3", "remote", "devices.json")); !os.IsNotExist(err) {
		t.Fatal("task door consulted machine-access grants")
	}
}

// DESIGN section 2: quiet and unpaired homes do not register or mention tasks.
func TestServeTasksLeavesUnpairedAndQuietHomesAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	for _, kind := range []string{"unpaired", "solo", "off"} {
		t.Run(kind, func(t *testing.T) {
			profile := t.TempDir()
			if kind == "solo" {
				if _, err := identity.EnsureSolo(profile); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "off" {
				if _, err := identity.Ensure(profile); err != nil {
					t.Fatal(err)
				}
				t.Setenv("CODEAF_SYNC_URL", "off")
			}
			_, ok, err := serveTasksConfig(profile, "http://127.0.0.1:8999")
			if err != nil || ok {
				t.Fatalf("%s opened a task door: %t, %v", kind, ok, err)
			}
		})
	}
}
