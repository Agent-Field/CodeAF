package main

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func r6AppRow(t *testing.T, released bool) (appwire.ChatRow, directory.CellView, directory.CellView) {
	t.Helper()
	celltest.Available(t)
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	var offset atomic.Int64
	service := relayserve.New(relayserve.Config{Store: t.TempDir(), Now: func() time.Time {
		return time.Now().Add(time.Duration(offset.Load()))
	}})
	t.Cleanup(func() { _ = service.Close() })
	relay := httptest.NewServer(service.Handler)
	t.Cleanup(relay.Close)
	t.Setenv(syncsetup.URLVar, relay.URL)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for s, name := range map[*syncsetup.Sync]string{a: "tower", b: "laptop"} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, name); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Heartbeat(t.Context(), c.ID, directory.Beat{Fence: created.Cell.Lease.Fence, Working: true}); err != nil {
		t.Fatal(err)
	}
	if released {
		if err := a.Dir.Release(t.Context(), c.ID, created.Cell.Lease.Fence); err != nil {
			t.Fatal(err)
		}
	} else {
		offset.Store(int64(directory.LeaseTTL + time.Second))
	}
	before, err := a.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, bHome)
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { _ = backend.Close() })
	listed, err := backend.readChats(t.Context(), b, nil, false)
	if err != nil || len(listed.Chats) != 1 {
		t.Fatalf("app rows=%+v err=%v", listed, err)
	}
	after, err := a.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return listed.Chats[0], before, after
}

func TestIdleOpen_R6_LapsedWhileWorkingKeepsTheCard(t *testing.T) {
	row, before, after := r6AppRow(t, false)
	if !before.Cell.Lease.Working || before.Cell.Lease.Expires == 0 || before.Cell.Lease.Expires > before.Now {
		t.Fatalf("fixture must lapse while working: %+v", before)
	}
	if row.CanOpen || !row.CanTake || row.Here || row.Status != "off" {
		t.Fatalf("app lapsed working row must keep Continue here: %+v", row)
	}
	if before.Cell.Head != after.Cell.Head || before.Cell.Lease != after.Cell.Lease {
		t.Fatal("listing a lapsed chat took it")
	}
}

func TestIdleOpen_R6_ReleasedStillOpensDirectly(t *testing.T) {
	row, before, after := r6AppRow(t, true)
	if !row.CanOpen || !row.CanTake || row.Here || before.Cell.Lease.Expires != 0 {
		t.Fatalf("app released row lost direct opening: %+v", row)
	}
	if before.Cell.Head != after.Cell.Head || before.Cell.Lease != after.Cell.Lease {
		t.Fatal("listing a released chat took it before open")
	}
}
