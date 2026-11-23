package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/rotate"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// R5.2: an explicit serving relay cannot bypass any home-sync gate.
func TestDevboxR52ServePresenceRespectsEligibility(t *testing.T) {
	celltest.Available(t)
	for _, mode := range []string{"quiet", "off", "unavailable", "rotating"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "unexpected", 404) }))
			defer server.Close()
			dir := t.TempDir()
			t.Setenv(home.EnvVar, dir)
			t.Setenv(syncsetup.URLVar, server.URL)
			if mode == "quiet" {
				if _, err := identity.EnsureSolo(dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := identity.Ensure(dir); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "off":
				t.Setenv(syncsetup.URLVar, "off")
			case "unavailable":
				t.Cleanup(cell.UseAvailabilityInTests(false))
			case "rotating":
				if err := os.WriteFile(filepath.Join(dir, rotate.File), []byte("pending"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			keepServePresence(ctx, server.URL)
			if requests.Load() != 0 {
				t.Fatalf("R5.2 %s published %d requests", mode, requests.Load())
			}
		})
	}
}

// R5.2: a serving worker releases its old signer after identity/device/relay rebinding.
func TestDevboxR52ServePresenceStopsOnRebinding(t *testing.T) {
	celltest.Available(t)
	for _, mode := range []string{"identity", "device", "relay"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(home.EnvVar, dir)
			started := make(chan struct{}, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case started <- struct{}{}:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`))
			}))
			defer server.Close()
			t.Setenv(syncsetup.URLVar, server.URL)
			if _, err := identity.Ensure(dir); err != nil {
				t.Fatal(err)
			}
			ticks := make(chan time.Time)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); keepServeHomePresence(ctx, dir, ticks) }()
			t.Cleanup(func() { cancel(); <-done })
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("R5.2 no initial presence")
			}
			switch mode {
			case "identity":
				id, err := identity.Ensure(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := identity.Save(dir, id); err != nil {
					t.Fatal(err)
				}
			case "device":
				if err := os.Remove(filepath.Join(dir, "device.json")); err != nil {
					t.Fatal(err)
				}
				if _, err := identity.Device(dir); err != nil {
					t.Fatal(err)
				}
			case "relay":
				t.Setenv(syncsetup.URLVar, "https://new-relay.example")
			}
			select {
			case ticks <- time.Now():
			case <-done:
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("R5.2 retained old presence binding")
			}
		})
	}
}
