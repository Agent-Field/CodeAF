package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestQARemoteVaultChangeWakesDesktopSharing(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
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
	if err := source.Dir.SetVault(t.Context(), "", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.chats.publicationWake:
	case <-time.After(5 * time.Second):
		t.Fatal("a changed shared vault did not wake the receiving desktop")
	}
}
