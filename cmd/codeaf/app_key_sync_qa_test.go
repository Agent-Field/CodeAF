package main

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQADesktopSharesKeysAddedAfterPairing(t *testing.T) {
	for _, storageOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage on", true: "added while storage off"}[storageOff], func(t *testing.T) {
			testDesktopPostPairKey(t, storageOff)
		})
	}
}

func testDesktopPostPairKey(t *testing.T, storageOff bool) {
	celltest.Available(t)
	var off atomic.Bool
	var uploads atomic.Int32
	off.Store(storageOff)
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/store/stats" {
				state := "on"
				if off.Load() {
					state = "off"
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"storage":"` + state + `"}`))
				return
			}
			if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
				uploads.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	a, other := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", a)
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(other, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, other)
	if err := sa.Rename(t.Context(), "first computer"); err != nil {
		t.Fatal(err)
	}
	if err := sb.Rename(t.Context(), "second computer"); err != nil {
		t.Fatal(err)
	}
	if _, err := sa.Dir.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	if pair.SharingPending(a, id.ID()) {
		t.Fatal("fixture still has an unfinished pairing")
	}
	if storageOff {
		if err := syncsetup.SaveStorage(a, relay.URL, id.ID(), "off"); err != nil {
			t.Fatal(err)
		}
	}
	credentials := config.NewAppCredentials()
	restore := config.UseAppCredentials(credentials)
	defer restore()
	recorder := newAppRecorder()
	b := &appBackend{left: make(chan struct{}), emit: recorder, keys: credentials}
	round := r1PublicationRounds(t, b)
	credentials.Set("aiand", "FAKE-post-pair-desktop-key")
	round(syncsetup.DefaultInterval)
	received := config.NewAppCredentials()
	pull := func() {
		t.Helper()
		restoreSender := config.UseAppCredentials(received)
		err = sb.SyncState(t.Context())
		restoreSender()
		if err != nil {
			t.Fatal(err)
		}
	}
	if storageOff {
		pull()
		if _, known := received.Key("aiand"); known || uploads.Load() != 0 {
			t.Fatal("a desktop key uploaded while storage was off")
		}
		off.Store(false)
		if err := syncsetup.SaveStorage(a, relay.URL, id.ID(), "on"); err != nil {
			t.Fatal(err)
		}
		round(2 * syncsetup.DefaultInterval)
		recorder.waitFor(t, 2*time.Second, func(event string, data any) bool {
			if event != appwire.EventSyncChanged {
				return false
			}
			storage := data.(appwire.SyncChanged).Sync.Storage
			return storage != nil && storage.State == "on"
		})
	}
	pull()
	if key, known := received.Key("aiand"); !known || key != "FAKE-post-pair-desktop-key" {
		t.Fatal("the desktop publication worker did not share a key added after pairing")
	}
	before := uploads.Load()
	for tick := 3; tick <= 5; tick++ {
		round(time.Duration(tick) * syncsetup.DefaultInterval)
	}
	if uploads.Load() != before {
		t.Fatal("unchanged desktop keys were uploaded again")
	}
}
