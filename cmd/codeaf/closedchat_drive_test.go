package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func closedChatWriter(t *testing.T, book *driveBook, id string) *syncsetup.Drive {
	t.Helper()
	live := book.renewalsOf(id)
	if live == nil {
		t.Fatal("the open chat has no drive")
	}
	if err := live.Gate(); err != nil {
		t.Fatalf("start the open chat's drive: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if writer := live.started(); writer != nil && writer.HoldsLease(t.Context()) {
			return writer
		}
		if time.Now().After(deadline) {
			t.Fatal("the open chat's drive never held its lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func checkClosedChatSuccessor(t *testing.T, s *syncsetup.Sync, id string, writer *syncsetup.Drive, before directory.CellView) {
	t.Helper()
	if !writer.HoldsLease(t.Context()) {
		t.Fatal("P2 lost its lease when P1's process quit after closing the chat")
	}
	checkHeldChatLease(t, s, id, before)
	view, err := s.Dir.Cell(t.Context(), id)
	if err != nil || view.Cell.Lease.Device != s.Device.ID() || view.Cell.Lease.Expires <= view.Now {
		t.Fatalf("P2's relay lease = %+v, %v; want this device's live lease", view, err)
	}
	remote, fence, err := s.SendLease(id)
	if err != nil || remote != id || fence != view.Cell.Lease.Fence {
		t.Fatalf("P2's send receipt = %s fence %d, %v; want %s fence %d", remote, fence, err, id, view.Cell.Lease.Fence)
	}
	confirmed, err := writer.ConfirmWork(t.Context())
	if err != nil || !confirmed {
		t.Fatalf("P2's next work confirmation = %v, %v; want true, nil", confirmed, err)
	}
}

func TestAChatClosedHereIsNotReleasedFromUnderItsNextOpener(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_FURROW", filepath.Join(t.TempDir(), "absent-engine"))
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root, Workspace: t.TempDir()}
	cfg := session.Config{Place: place, Workspace: place.Workspace, SessionFile: place.Transcript(), Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1"}
	s, _ := heldChatRelay(t, c.ID)
	if _, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
		t.Fatalf("create P1's relay lease: %v", err)
	}
	book := func() *driveBook {
		d := newAdmissionDrive(func(func()) (*syncsetup.Drive, error) {
			return s.Drive(t.Context(), cellstore.Engine{}, c, syncsetup.DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
				<-ctx.Done()
				return ctx.Err()
			}})
		})
		return &driveBook{drive: map[string]*liveDrive{c.ID: d}}
	}
	prior := syncDrives
	firstBook, secondBook := book(), book()
	syncDrives = firstBook
	defer func() { secondBook.closeAll(); firstBook.closeAll(); syncDrives = prior }()
	first, _, _, err := openV3Agent(cfg, place.Workspace, session.New)
	if err != nil {
		t.Fatalf("open P1's journal: %v", err)
	}
	defer first.Close()
	closedChatWriter(t, firstBook, c.ID)
	before, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || before.Cell.Lease.Fence == 0 || before.Cell.Lease.Expires <= before.Now {
		t.Fatalf("P1's initial lease = %+v, %v", before, err)
	}
	snapshotHeldChatReceipt(t, s, c.ID)
	if err := first.Close(); err != nil {
		t.Fatalf("close P1's chat while its process stays open: %v", err)
	}
	syncDrives = secondBook
	second, _, _, err := openV3Agent(cfg, place.Workspace, session.New)
	if err != nil {
		t.Fatalf("open P2's journal after P1 closed it: %v", err)
	}
	defer second.Close()
	writer := closedChatWriter(t, secondBook, c.ID)
	beforeQuit, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstBook.closeAll()
	checkClosedChatSuccessor(t, s, c.ID, writer, beforeQuit)
	if beforeQuit.Cell.Lease.Fence <= before.Cell.Lease.Fence {
		t.Fatalf("P2 resumed fence %d; want a fresh fence after P1's %d", beforeQuit.Cell.Lease.Fence, before.Cell.Lease.Fence)
	}
}

func TestAChatClosedHereKeepsItsNextOpenersCommandsSendableAfterThisProcessQuits(t *testing.T) {
	cfg, c := spokenDoorChat(t)
	s, _ := heldChatRelay(t, c.ID)
	firstBook := syncDrives
	defer firstBook.closeAll()
	first, _, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	closedChatWriter(t, firstBook, c.ID)
	if err := first.Close(); err != nil {
		t.Fatalf("close P1's spoken chat: %v", err)
	}
	secondBook := &driveBook{}
	syncDrives = secondBook
	defer func() { secondBook.closeAll(); syncDrives = firstBook }()
	second, seated, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	writer := closedChatWriter(t, secondBook, c.ID)
	beforeQuit, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstBook.closeAll()
	checkClosedChatSuccessor(t, s, c.ID, writer, beforeQuit)
	before, err := cellstore.Turns(c)
	if err != nil {
		t.Fatal(err)
	}
	events, err := second.SubmitBash(t.Context(), "! printf successor")
	if err != nil {
		t.Fatalf("P2's next command was refused: %v", err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("P2's next command reported %q", event.Text)
		}
	}
	if err := executor.Drain(t.Context(), seated.Seat); err != nil {
		t.Fatalf("settle P2's next command: %v", err)
	}
	turns, err := cellstore.Turns(c)
	if err != nil || len(turns) <= len(before) {
		t.Fatalf("P2's sealed turns = %d, %v; want more than %d", len(turns), err, len(before))
	}
	newest := turns[len(turns)-1]
	if newest.Device != s.Device.Cert.Device || newest.Fence != beforeQuit.Cell.Lease.Fence {
		t.Fatalf("P2's newest turn = %s fence %d; want %s fence %d", newest.Device, newest.Fence, s.Device.Cert.Device, beforeQuit.Cell.Lease.Fence)
	}
}

func TestClosingAChatsDriveLeavesItsReplacementAlone(t *testing.T) {
	old := &liveDrive{}
	replacement := newPendingLiveDrive(func(func()) (*syncsetup.Drive, error) { t.Error("start the replacement drive"); return nil, nil })
	book := &driveBook{drive: map[string]*liveDrive{"chat": replacement}}
	defer book.closeAll()
	if err := book.closeCellIf("chat", old); err != nil {
		t.Fatal(err)
	}
	if book.renewalsOf("chat") != replacement || !replacement.isParked() {
		t.Fatal("closing an earlier owner changed the replacement drive")
	}
	if err := book.closeCellIf("chat", replacement); err != nil {
		t.Fatal(err)
	}
	if book.renewalsOf("chat") != replacement || !replacement.isParked() {
		t.Fatal("an opener that never began driving changed the parked drive")
	}
}

func TestClosingAChatsDriveMakesFinishingSavingAndClosingItAgainHarmless(t *testing.T) {
	d := newAdmissionDrive(func(func()) (*syncsetup.Drive, error) { return nil, nil })
	book := &driveBook{drive: map[string]*liveDrive{"chat": d}}
	defer book.closeAll()
	d.begin()
	if err := book.closeCellIf("chat", d); err != nil {
		t.Fatal(err)
	}
	if book.renewalsOf("chat") != nil {
		t.Fatal("the closed conversation retained its drive")
	}
	if _, ok := workAdmissions.Load(d); ok {
		t.Fatal("the closed conversation retained its work admission")
	}
	if err := book.closeCellIf("chat", d); err != nil {
		t.Fatal(err)
	}
	if err := book.closeCell("chat"); err != nil {
		t.Fatalf("FinishSaving after the agent's release: %v", err)
	}
}
