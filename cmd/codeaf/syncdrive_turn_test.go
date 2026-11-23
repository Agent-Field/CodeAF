package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

type retryTurnCapture struct {
	attempts atomic.Int32
}

var errTurnCaptureFixture = errors.New("temporary save failure")

func (s *retryTurnCapture) Seal(context.Context, cell.Cell, cellstore.TurnInfo) (cellstore.Sealed, error) {
	if s.attempts.Add(1) == 1 {
		return cellstore.Sealed{}, errTurnCaptureFixture
	}
	return cellstore.Sealed{}, nil
}

func TestQATurnEndRetriesFailedCaptureWithoutAnotherTurn(t *testing.T) {
	c := cell.Cell{Root: t.TempDir()}
	transcript := filepath.Join(c.Root, cell.TranscriptPath)
	if err := os.MkdirAll(filepath.Dir(transcript), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("{\"type\":\"message\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	drive := &liveDrive{working: true, attempted: true}
	defer drive.Close(t.Context())
	watch := &cellstore.SealWatch{}
	watch.OnTurnEnd = func() { drive.TurnEnded(c, watch) }
	watch.SetIdleCheck(func() bool { return true })
	store := &retryTurnCapture{}
	recorder, err := cellstore.NewRecorder(nil, store, c, filepath.Join(c.Root, "capture.wal"), cellstore.Options{Async: true, Report: watch.Report})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Settle(t.Context())
	if err := cellstore.WaitCapture(t.Context(), c); !errors.Is(err, errTurnCaptureFixture) {
		t.Fatalf("first capture returned %v, want the temporary failure", err)
	}
	watch.TurnEnded()
	appSyncWait(t, "the recovered capture to clear working", func() bool {
		drive.mu.Lock()
		defer drive.mu.Unlock()
		return !drive.working
	})
	if got := store.attempts.Load(); got != 2 {
		t.Fatalf("capture attempts = %d, want one failure and one retry", got)
	}
}
