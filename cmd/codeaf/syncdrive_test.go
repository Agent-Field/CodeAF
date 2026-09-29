package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// doorChat is a chat cell opened through the door's own seat construction, in a
// home of its own.
func doorChat(t *testing.T) (session.Config, cell.Cell) {
	t.Helper()
	if _, err := furrow.ResolveOwned(); err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv(cell.EnvVar, "1")
	// The engine daemon has no sync verbs until its own lane lands, so the
	// door's sync runs over the spawn transport, which has them.
	t.Setenv("CODEAF_ENGINE_DAEMON", "0")
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	prior := syncDrives
	syncDrives = &driveBook{}
	t.Cleanup(func() { syncDrives.closeAll(); syncDrives = prior })
	return session.Config{Place: session.Place{Dir: c.Root, Workspace: t.TempDir()}}, c
}

func toolCallOn(t *testing.T, seat executor.Seat, workspace string) error {
	t.Helper()
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	return seat.Around(context.Background(), call, func() ([]byte, bool) {
		return nil, os.WriteFile(filepath.Join(workspace, "f.txt"), []byte("x"), 0o600) != nil
	})
}

// With no relay set the door is Stage 0 exactly: no drive side is made, the seat
// is not gated, and a turn is sealed by the zero device under fence 0.
func TestDriveSideOffIsStageZero(t *testing.T) {
	cfg, c := doorChat(t)
	t.Setenv(syncsetup.URLVar, "")
	seated := v3Seated(cfg)
	if len(syncDrives.drive) != 0 {
		t.Fatal("a drive side was made with sync off")
	}
	if seated.Seat == nil {
		t.Fatal("no seat")
	}
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	turns, err := cellstore.Turns(c)
	if err != nil || len(turns) == 0 {
		t.Fatalf("turns = %v, %v", turns, err)
	}
	if got := turns[len(turns)-1]; got.Fence != 0 || got.Device != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("a Stage 0 turn carries %q fence %d", got.Device, got.Fence)
	}
}

// With a relay set the door notes every sealed turn for it, and closing the
// door gives the lease back after publishing what was sealed.
func TestDriveSideDoorPublishesAndReleases(t *testing.T) {
	cfg, c := doorChat(t)
	srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
	t.Cleanup(srv.Close)
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")

	seated := v3Seated(cfg)
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	syncDrives.closeAll()

	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("Open = %v, %v", ok, err)
	}
	head, _ := cellstore.Head(c)
	v, err := s.Dir.Cell(context.Background(), c.ID)
	if err != nil || v.Cell.Head != head.Turn.ID {
		t.Fatalf("directory head %q (%v), want the chat's %q", v.Cell.Head, err, head.Turn.ID)
	}
	if v.Cell.Lease.Expires != 0 || v.Cell.Lease.Device != s.Device.ID() {
		t.Fatalf("lease after close = %+v, want released by this device", v.Cell.Lease)
	}
}
