package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestIdleRelease_R7_Reopen_AfterFailedCloseInstallsNewWriter(t *testing.T) {
	cfg, c := spokenDoorChat(t)
	_, _, _ = heldChatRelay(t, c.ID)
	book := syncDrives
	takeFinalSaveFailure()
	t.Cleanup(func() { takeFinalSaveFailure() })
	t.Cleanup(book.closeAll)
	first, seated, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	old := book.renewalsOf(c.ID)
	if old == nil {
		t.Fatal("opener installed no writer")
	}
	if err := old.Gate(); err != nil || old.started() == nil {
		t.Fatalf("initial writer did not install: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := executor.Drain(t.Context(), seated.Seat); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	closeErr := old.Close(ctx)
	if closeErr == nil || !errors.Is(old.Gate(), cellstore.ErrReadOnly) {
		t.Fatalf("fixture needs a failed final save and a stopped writer: close=%v gate=%v", closeErr, old.Gate())
	}
	reopened, _, _, err := openV3Agent(cfg, cfg.Place.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	fresh := book.renewalsOf(c.ID)
	if fresh == nil || fresh == old {
		t.Fatal("reopening reattached the stopped writer after its failed final save")
	}
	failure, _, _ := takeFinalSaveFailure()
	if !errors.Is(failure, closeErr) || unsavedTranscript(failure) != cfg.SessionFile {
		t.Fatalf("old final-save failure was not retained with its transcript: got=%v want=%v", failure, closeErr)
	}
	if err := fresh.Gate(); err != nil || fresh.started() == nil {
		t.Fatalf("replacement writer did not install: %v", err)
	}
	if err := reopened.CheckWork(); err != nil {
		t.Fatalf("reopened session cannot admit work: %v", err)
	}
	if !errors.Is(old.Gate(), cellstore.ErrReadOnly) {
		t.Fatal("replacement revived the closed writer")
	}
}
