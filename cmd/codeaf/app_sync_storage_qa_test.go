package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQADesktopStorageStatusUsesOnlyLocalFacts(t *testing.T) {
	celltest.Available(t)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	t.Setenv(syncsetup.URLVar, syncsetup.ProductionRelayURL)
	b := &appBackend{emit: newAppRecorder()}
	state, err := b.SyncStatus()
	if err != nil || state.Storage != nil {
		t.Fatal("an unopened home offered storage access")
	}
	if _, err := identity.Load(home); err == nil {
		t.Fatal("reading storage status created an identity")
	}
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	state, err = b.SyncStatus()
	if err != nil || state.Storage == nil || state.Storage.State != "off" || state.Storage.RequestCode != id.ID() || state.Storage.RequestURL != syncsetup.StorageRequestLink(id.ID()) {
		t.Fatalf("hosted storage-off status: %+v, %v", state.Storage, err)
	}
	if err := syncsetup.SaveStorage(home, syncsetup.ProductionRelayURL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	state, err = b.SyncStatus()
	if err != nil || state.Storage == nil || state.Storage.State != "on" || state.Storage.RequestURL != "" {
		t.Fatalf("approved storage still offers access: %+v, %v", state.Storage, err)
	}
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")
	if err := syncsetup.SaveStorage(home, "http://127.0.0.1:1", id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	state, err = b.SyncStatus()
	if err != nil || state.Storage == nil || state.Storage.State != "off" || state.Storage.RequestURL != "" {
		t.Fatalf("custom relay offered hosted access: %+v, %v", state.Storage, err)
	}
	t.Setenv(syncsetup.URLVar, "off")
	state, err = b.SyncStatus()
	if err != nil || state.Storage != nil {
		t.Fatal("sync-off status offered storage access")
	}
}
