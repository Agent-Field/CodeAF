package syncsetup

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// Exercise the real signed stats wire at an arbitrary relay address.
func TestStorageOptInRelayAnswerDecides(t *testing.T) {
	celltest.Available(t)
	for _, answer := range []string{`{"storage":"off"}`, `{"storage":"on"}`, `{"objects":0}`} {
		t.Run(answer, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/v1/store/stats" || r.Header.Get("Codeaf-Sig") == "" {
					t.Errorf("unexpected stats: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(answer))
			}))
			defer srv.Close()
			t.Setenv(URLVar, srv.URL)
			home := t.TempDir()
			id, err := identity.Ensure(home)
			if err != nil {
				t.Fatal(err)
			}
			if StorageState(home, srv.URL, id.ID()) != "on" {
				t.Fatal("unknown custom relay must start open")
			}
			p := storageProcessFor(home)
			p.users = 1
			var next func()
			p.after = func(_ time.Duration, fn func()) func() bool {
				next = fn
				return func() bool { next = nil; return true }
			}
			defer p.disarm()
			p.check(true)
			if requests.Load() != 1 {
				t.Fatalf("startup requests=%d want 1", requests.Load())
			}
			if strings.Contains(answer, `"off"`) {
				if StorageState(home, srv.URL, id.ID()) != "off" || next == nil {
					t.Fatal("relay's off answer must close gate and continue refresh")
				}
			} else {
				if StorageState(home, srv.URL, id.ID()) != "on" || next != nil {
					t.Fatal("no-field answer must leave gate open without a timer")
				}
				p.check(true)
				if requests.Load() != 1 {
					t.Fatalf("repeated no-field probe: %d", requests.Load())
				}
				// A new process reading the persisted answer also remains idle.
				fresh := &storageProcess{ctx: t.Context(), users: 1, read: p.read, fetch: p.fetch, after: p.after, now: time.Now}
				fresh.check(true)
				if requests.Load() != 1 {
					t.Fatal("no-field answer was not cached")
				}
			}
		})
	}
}

func TestStorageOptInUnansweredCustomProbeFailure(t *testing.T) {
	celltest.Available(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv(URLVar, srv.URL)
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	p := storageProcessFor(home)
	p.users = 1
	p.after = func(time.Duration, func()) func() bool {
		t.Error("failed custom probe armed a timer")
		return func() bool { return true }
	}
	for range 3 {
		p.check(true)
	}
	if requests.Load() != 1 || StorageState(home, srv.URL, id.ID()) != "on" || StorageNeedsRefresh(home, srv.URL, id.ID()) {
		t.Fatalf("failed discovery requests=%d state=%s", requests.Load(), StorageState(home, srv.URL, id.ID()))
	}
}

func TestStorageOptInDiscoveryQuietHomes(t *testing.T) {
	for _, mode := range []string{"sync-off", "unpaired", "no-identity"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(URLVar, "http://127.0.0.1:1")
			if mode == "sync-off" {
				t.Setenv(URLVar, "off")
				if _, err := identity.Ensure(home); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unpaired" {
				if _, err := identity.EnsureSolo(home); err != nil {
					t.Fatal(err)
				}
			}
			p := storageProcessFor(home)
			p.users = 1
			p.fetch = func(context.Context) error { t.Error("quiet home requested stats"); return nil }
			p.after = func(time.Duration, func()) func() bool {
				t.Error("quiet home armed timer")
				return func() bool { return true }
			}
			p.check(true)
		})
	}
}

func TestStorageOptInCustomProbeSharedByCallers(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	const relay = "http://127.0.0.1:18831"
	for i := 0; i < 50; i++ {
		want := i == 0
		if got := StorageNeedsRefresh(home, relay, id.ID()); got != want {
			t.Fatalf("caller %d requests=%v want=%v", i, got, want)
		}
	}
}
