//go:build unix

package standing

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A NAMED PIPE IN A WATCHED FOLDER DOES NOT HANG THE PASS. The walk puts every
// entry it reaches into the reading, a pipe included, and opening a pipe to
// read it waits for a writer that never comes — so the condition's excerpt
// stalled every standing item behind it, forever. It is named in the evidence
// as not a regular file, and never opened.
func TestANamedPipeInAWatchedFolderDoesNotHangThePass(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	_, workspace := clientsWatch(t, store)
	runner := &occurrenceRunner{}
	sentinel, shown := evidenceJudge("inbox/clients/acme/pipe")
	ticker := newTicker(store, runner, now)
	ticker.Sentinel = sentinel
	mustTick(t, ticker)

	if err := syscall.Mkfifo(filepath.Join(workspace, "inbox", "clients", "acme", "pipe"), 0o600); err != nil {
		t.Skipf("this filesystem makes no named pipes: %v", err)
	}
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	ticker = newTicker(store, runner, later)
	ticker.Sentinel = sentinel
	done := make(chan Pass, 1)
	go func() {
		pass, _ := ticker.Tick(context.Background())
		done <- pass
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the pass hung opening a named pipe in the watched folder")
	}
	if len(*shown) != 1 || !strings.Contains((*shown)[0], "not a regular file") {
		t.Fatalf("the pipe was not named as not a regular file: %q", *shown)
	}
}
