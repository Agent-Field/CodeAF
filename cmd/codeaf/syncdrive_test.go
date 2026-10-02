package main

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
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
	t.Setenv(syncsetup.URLVar, "off")
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

// A device the relay has stopped is a refusal the person is told of, even when
// the error arrives wrapped; a relay that is merely unreachable is not.
func TestSayRefusalOnlyForRefusals(t *testing.T) {
	if !sayRefusal(fmt.Errorf("put device: %w", wireauth.ErrRevoked)) {
		t.Error("a stopped device was not told")
	}
	if sayRefusal(errors.New("connection refused")) || sayRefusal(nil) {
		t.Error("a failure that is not a refusal was said")
	}
}

// A chat that opened before this computer had an identity starts to sync the
// moment /pair makes one, with no restart: the next call's seal is published and
// closing the chat gives the lease back, exactly as for a chat that was paired
// from the start.
func TestDriveSideStartsWhenAPairingMakesTheIdentity(t *testing.T) {
	cfg, c := doorChat(t)
	srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")

	seated := v3Seated(cfg)
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Ensure(home.Dir()); err != nil { // what /pair does in the running chat
		t.Fatal(err)
	}
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
}

// A chat open while its computer is solo uploads its next turns the moment the
// computer is approved, with no reopen.
func TestDriveSideStartsWhenTheSoloMarkerEnds(t *testing.T) {
	cfg, c := doorChat(t)
	srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	if _, err := identity.EnsureSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}

	seated := v3Seated(cfg)
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	if d := syncDrives.drive[c.ID]; d == nil || d.started() != nil {
		t.Fatal("a solo computer must hold a waiting drive side, not a running one")
	}
	if err := identity.EndSolo(home.Dir()); err != nil { // what an approval does
		t.Fatal(err)
	}
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
}

// One predicate, four states, the drive paths: startDrive and liveDrive agree
// with syncsetup.MayTalk.
func TestDrivePathsFollowMayTalk(t *testing.T) {
	cases := []struct {
		name  string
		setup func(dir string)
		talk  bool
	}{
		{"no identity", func(string) {}, false},
		{"solo", func(d string) { _, _ = identity.EnsureSolo(d) }, false},
		{"paired", func(d string) { _, _ = identity.Ensure(d) }, true},
		{"engaged", func(d string) { _, _ = identity.EnsureSolo(d); _ = identity.EndSolo(d) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, c := doorChat(t)
			srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
			t.Cleanup(srv.Close)
			t.Setenv(syncsetup.URLVar, srv.URL)
			tc.setup(home.Dir())
			if got := syncsetup.MayTalk(home.Dir()); got != tc.talk {
				t.Fatalf("MayTalk = %v, want %v", got, tc.talk)
			}
			d, err := startDrive(c, cellstore.EngineFor(""), func(error) {})
			if (d != nil) != tc.talk {
				t.Fatalf("startDrive drive=%v err=%v, want drive %v", d != nil, err, tc.talk)
			}
			if d != nil {
				_ = d.Close(context.Background())
			}
			_ = cfg
		})
	}
}

// A computer with no identity is waiting, not failing to seal: with the hosted default every fresh
// computer is in that state, and it must not show a failing seal on its first launch.
func TestStartDriveWithNoIdentityReportsNothing(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(syncsetup.URLVar, "")
	var reported []error
	d, err := startDrive(cell.Cell{}, cellstore.Engine{}, func(e error) { reported = append(reported, e) })
	if d != nil || !errors.Is(err, syncsetup.ErrNoIdentity) {
		t.Fatalf("startDrive = %v, %v; want no drive and ErrNoIdentity", d, err)
	}
	if len(reported) != 0 {
		t.Fatalf("waiting for an identity was reported as a seal outcome: %v", reported)
	}
}

// A chat that was moved to another computer and then moved back opens here with a drive side of
// its own. The drive side kept from before the move only shows the chat, so reusing it would
// refuse every tool call in the chat that was moved back.
func TestAChatMovedBackGetsADriveSideOfItsOwn(t *testing.T) {
	cfg, c := doorChat(t)
	srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	first := home.Dir()
	id, err := identity.Ensure(first)
	if err != nil {
		t.Fatal(err)
	}
	seated := v3Seated(cfg)
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	firstBook := syncDrives

	// The second computer, one identity with the first, opens the chat while the first drives it.
	second := t.TempDir()
	blob, err := identity.Export(id, "p")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := identity.Import(blob, "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(second, moved, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, second)
	syncDrives = &driveBook{}
	t.Cleanup(func() { syncDrives = firstBook })
	engine := cellstore.EngineFor(cfg.Place.Workspace)
	viewing := syncDrives.driveOf(c, engine, func(error) {})
	if viewing == nil || viewing.started() == nil {
		t.Fatal("the second computer made no drive side")
	}
	if _, viewer := viewing.started().Viewer(); !viewer {
		t.Skip("the first computer did not hold the lease yet")
	}

	// The first computer lets the chat go; opening it again here must drive it.
	t.Setenv(home.EnvVar, first)
	firstBook.closeAll()
	t.Setenv(home.EnvVar, second)
	again := syncDrives.driveOf(c, engine, func(error) {})
	if again == viewing {
		t.Fatal("the drive side that only showed the chat was reused")
	}
	if line := again.Gate(); line != nil {
		t.Fatalf("a chat opened after it was moved back is refused: %v", line)
	}
}

// A chat that another computer took and that was then taken back here, with no word reaching the
// drive side this window kept, opens with a drive side of its own: the old one publishes under a
// lease that is gone, so reusing it would refuse every tool call in the chat that came back.
func TestAChatTakenAndTakenBackGetsADriveSideOfItsOwn(t *testing.T) {
	cfg, c := doorChat(t)
	srv := httptest.NewServer(relayserve.New(relayserve.Config{Store: t.TempDir()}).Handler)
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	first := home.Dir()
	id, err := identity.Ensure(first)
	if err != nil {
		t.Fatal(err)
	}
	seated := v3Seated(cfg)
	if err := toolCallOn(t, seated.Seat, cfg.Place.Workspace); err != nil {
		t.Fatal(err)
	}
	kept := syncDrives.drive[c.ID]

	second := t.TempDir()
	blob, err := identity.Export(id, "p")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := identity.Import(blob, "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(second, moved, false); err != nil {
		t.Fatal(err)
	}
	force := func(dir string) {
		t.Helper()
		s, ok, err := syncsetup.Open(dir)
		if err != nil || !ok {
			t.Fatalf("Open(%s) = %v, %v", dir, ok, err)
		}
		if _, err := s.Dir.Acquire(context.Background(), c.ID, directory.AcquireOpts{Force: true}); err != nil {
			t.Fatal(err)
		}
	}
	force(second) // the other computer takes the chat
	force(first)  // and it is taken back here, as a take does

	again := syncDrives.driveOf(c, cellstore.EngineFor(cfg.Place.Workspace), func(error) {})
	if again == kept {
		t.Fatal("the drive side whose lease was taken was reused")
	}
	if err := again.Gate(); err != nil {
		t.Fatalf("a chat taken back is refused: %v", err)
	}
}
