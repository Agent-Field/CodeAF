package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A chat the app moves here opens before its files and its after-work are
// done. On dev a failing vault pull, index rebuild or file restore refused the
// take and the person read why; behind the open there is no refusal to carry
// it, so the app must put a line about it in front of the person, in that
// chat. A move that fails behind the open and tells nobody is the failure.
func TestAppTakeTellsThePersonWhenTheAfterWorkFailsBehindTheOpen(t *testing.T) {
	h := newHostedMoveRig(t)
	drive := h.publish(h.a, h.ea, h.c, "sender")
	if err := drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*syncsetup.Sync{h.a, h.b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	// This computer's vault exchange cannot start: its lock file is not one.
	lock := filepath.Join(h.b.Home, "state-sync.lock")
	_ = os.Remove(lock)
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatal(err)
	}

	_, rec, wire := appDeviceBackend(t, h.b.Home)
	seen, opened := appTakeSeen, appTakeOpen
	t.Cleanup(func() { appTakeSeen, appTakeOpen = seen, opened })
	appTakeOpen = appOpened
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == h.c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}); refusal != nil {
		t.Fatalf("the move itself should open the chat: %+v", refusal)
	}
	rec.waitFor(t, 20*time.Second, func(name string, data any) bool {
		entry, ok := data.(appwire.EntryAdded)
		return name == appwire.EventEntryAdded && ok && entry.Entry.Kind == appwire.EntryNotice &&
			strings.Contains(entry.Entry.Text, "part of its state could not be updated yet")
	})
	// THE NOTICE COMES FROM THE MIDDLE OF THE AFTER-WORK, NOT FROM ITS END. The
	// rest of the hooks and the look at the folder still follow it, and a test
	// that returns on the notice leaves them writing into folders the
	// temporary-directory cleanup is removing ("directory not empty" on the full
	// suite). The arrival is complete only after all of that is done.
	appSyncWait(t, "the after-work behind the open to finish", func() bool {
		a := cell.LookupWorkspaceArrival(takeRootForAt(h.b.Home, h.c.ID))
		return a == nil || a.Ready()
	})
}
