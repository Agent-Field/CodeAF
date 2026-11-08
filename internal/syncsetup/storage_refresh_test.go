package syncsetup

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageOptInSharedWaiterWork(t *testing.T) {
	for _, n := range []int{1, 10, 50} {
		for _, mode := range []string{"on", "off", "self-hosted"} {
			t.Run(fmt.Sprintf("%s/%d", mode, n), func(t *testing.T) {
				home := t.TempDir()
				relay := ProductionRelayURL
				if mode == "self-hosted" {
					relay = "http://own.test"
				}
				t.Setenv(URLVar, relay)
				id, err := identity.Ensure(home)
				if err != nil {
					t.Fatal(err)
				}
				state := mode
				if state == "self-hosted" {
					state = "on"
				}
				if err := SaveStorage(home, relay, id.ID(), state); err != nil {
					t.Fatal(err)
				}
				reads, timers, requests := 0, 0, 0
				var next func()
				p := storageProcessFor(home)
				p.load = func(home string) (identity.Identity, error) { reads++; return identity.Load(home) }
				p.state = func(home, relay, id string) string { reads++; return StorageState(home, relay, id) }
				p.fetch = func(context.Context) error { requests++; return nil }
				p.after = func(wait time.Duration, fn func()) func() bool {
					if wait > StoragePollEvery {
						t.Fatalf("wait %v", wait)
					}
					timers++
					next = fn
					return func() bool { next = nil; return true }
				}
				woke := 0
				if mode == "off" {
					for i := 0; i < n; i++ {
						p.waiting[uint64(i)] = func() { woke++ }
					}
				}
				p.check(false)
				// Setup resolves the identity once. Count actual identity and permission
				// file reads in one minute of steady-state local waiting.
				reads, timers, requests = 0, 0, 0
				if next != nil {
					fn := next
					next = nil
					fn()
				}
				wantReads, wantTimers := 0, 0
				if mode == "off" {
					wantReads, wantTimers = 2, 1
				}
				if reads != wantReads || timers != wantTimers || requests != 0 {
					t.Fatalf("reads=%d timers=%d requests=%d", reads, timers, requests)
				}
				t.Logf("chats=%d state=%s file-reads/min=%d timer-arms/min=%d relay-requests=%d", n, mode, reads, timers, requests)
				if mode == "off" {
					// A raw atomic file change models another process sharing this home.
					raw := fmt.Sprintf(`{"relay":%q,"identity":%q,"storage":"on"}`, relay, id.ID())
					if err := os.WriteFile(filepath.Join(home, StorageFile), []byte(raw), 0600); err != nil {
						t.Fatal(err)
					}
					fn := next
					fn()
					if woke != n || next != nil {
						t.Fatalf("wake=%d timer=%v", woke, next != nil)
					}
				}
			})
		}
	}
}

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
