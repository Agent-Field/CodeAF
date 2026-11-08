package syncsetup

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStorageOptInFailedRefreshPreservesCache(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"off", "on"} {
		if err := SaveStorage(home, ProductionRelayURL, id.ID(), state); err != nil {
			t.Fatal(err)
		}
		hc := &http.Client{Transport: storageTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline fixture") })}
		s := &Sync{Home: home, Relay: ProductionRelayURL, Identity: id, Store: blobstore.NewHTTP(ProductionRelayURL, func(*http.Request, []byte) {}, hc)}
		if _, err := s.RefreshStorage(t.Context()); err == nil || StorageState(home, ProductionRelayURL, id.ID()) != state {
			t.Fatal("failed refresh changed permission")
		}
	}
}
func TestStorageOptInRefresherCadenceAndApplicability(t *testing.T) {
	for _, mode := range []string{"off", "on", "self-hosted", "sync-off"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			var next func()
			now := time.Unix(100, 0)
			p := &storageProcess{ctx: t.Context(), users: 1, waiting: map[uint64]func(){}, opened: map[uint64]func(){}, now: func() time.Time { return now }}
			p.read = func() (string, string, string) {
				if mode == "self-hosted" || mode == "sync-off" {
					return "", "", ""
				}
				return ProductionRelayURL, "id", mode
			}
			p.fetch = func(context.Context) error { requests++; return nil }
			p.after = func(wait time.Duration, fn func()) func() bool {
				if wait != StorageRefreshEvery {
					t.Fatalf("cadence %v", wait)
				}
				next = fn
				return func() bool { next = nil; return true }
			}
			p.check(true)
			if mode == "off" {
				if requests != 1 || next == nil {
					t.Fatal("missing startup refresh")
				}
				now = now.Add(StorageRefreshEvery)
				next()
				if requests != 2 {
					t.Fatal("missing periodic refresh")
				}
			} else if requests != 0 || next != nil {
				t.Fatalf("inapplicable work requests=%d timer=%v", requests, next != nil)
			}
		})
	}
}

func TestStorageOptInSignedRefreshUpdatesWaitersAndRunningDrives(t *testing.T) {
	home := t.TempDir()
	t.Setenv(URLVar, ProductionRelayURL)
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	p := storageProcessFor(home)
	opened := make(chan string, 2)
	cancelRunning := OnStorageOpened(home, func() { opened <- "running" })
	defer cancelRunning()
	// Register under the coordinator lock so this test controls the initial
	// local sample and the signed answer, rather than the scheduler.
	p.mu.Lock()
	p.waiting[1] = func() { opened <- "waiting" }
	p.mu.Unlock()
	hc := &http.Client{Transport: storageTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Codeaf-Sig") == "" {
			t.Error("unsigned stats")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"storage":"on"}`))}, nil
	})}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: home, Relay: ProductionRelayURL, Identity: id, Device: dev, Store: blobstore.NewHTTP(ProductionRelayURL, reqsign.SignFor(deviceSigner{id, dev}, time.Now), hc)}
	if _, err := s.RefreshStorage(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-opened:
		case <-time.After(5 * time.Second):
			t.Fatal("refresh failed to wake")
		}
	}
}
