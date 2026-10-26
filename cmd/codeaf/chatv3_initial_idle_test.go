package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestNewChatSealsBackgroundRecordWithoutFirstForegroundTurn(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	seat, _, watch := v3SeatOf(session.Place{Dir: c.Root, Workspace: t.TempDir()}, nil)
	closeClassSeat(t, seat)
	if watch == nil {
		t.Fatal("new chat has no seal watch")
	}
	// Observe the tail-seal callback at the real seat-construction seam. This
	// proves scheduling only; the public terminal relay test proves transport.
	checkpoint := filepath.Join(c.Root, "background-record.json")
	sealed := make(chan string, 1)
	watch.Quiet = 10 * time.Millisecond
	watch.OnTail = func() {
		raw, err := os.ReadFile(checkpoint)
		if err == nil {
			sealed <- string(raw)
		}
	}
	if err := os.WriteFile(checkpoint, []byte(`{"state":"finished"}`), 0600); err != nil {
		t.Fatal(err)
	}
	watch.Wrote()
	select {
	case got := <-sealed:
		if got != `{"state":"finished"}` {
			t.Fatalf("sealed stale background record: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("background record before the first foreground turn never reached the tail sealer")
	}
}
