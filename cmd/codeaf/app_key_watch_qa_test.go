package main

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestQARemoteVaultChangeWakesDesktopSharing(t *testing.T) {
	celltest.Available(t)
	var failList atomic.Bool
	var failedLists atomic.Int64
	appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/list") && failList.Load() {
				failedLists.Add(1)
				http.Error(w, "try later", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	source := appDeviceSync(t, home)
	if err := source.Rename(t.Context(), "watching computer"); err != nil {
		t.Fatal(err)
	}
	b := &appBackend{emit: newAppRecorder()}
	b.chats.publicationWake = make(chan struct{}, 1)
	d := b.listenDevices()
	defer d.close()
	feed := source.Follow()
	defer feed.Close()
	appSyncWait(t, "the existing directory watch", func() bool {
		return feed.State().Up && d.watch.Snapshot(source.Identity, source.Device).Complete
	})
	failList.Store(true)
	if err := source.Dir.SetVault(t.Context(), "", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	appSyncWait(t, "a transient failure reading the new vault pointer", func() bool { return failedLists.Load() > 0 })
	failList.Store(false)
	select {
	case <-b.chats.publicationWake:
	case <-time.After(8 * time.Second):
		t.Fatal("a changed shared vault did not wake the receiving desktop")
	}
}
