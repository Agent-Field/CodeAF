package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A client that stopped waiting for agents.take reads the row's taking flag:
// set while the take runs, cleared when it ends without moving the chat.
func TestAppTakeInFlightRowClearsWhenTheTakeFails(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	_, rec, wire := appDeviceBackend(t, bHome)
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	if row.Taking {
		t.Fatalf("no take has started: %+v", row)
	}
	stale := row.Seen
	stale.Head = "another-head"
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: stale}); refusal == nil || refusal.Code != appwire.CodeConflict {
		t.Fatalf("stale take: %+v", refusal)
	}
	started := rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Cell == c.ID && data.(appwire.ChatChanged).Chat.Taking
	})
	heard, ended := -1, appwire.ChatRow{}
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		heard++
		changed, ok := data.(appwire.ChatChanged)
		if heard <= started || name != appwire.EventChatChanged || !ok || changed.Chat.Cell != c.ID || changed.Chat.Taking {
			return false
		}
		ended = changed.Chat
		return true
	})
	if ended.Here {
		t.Fatalf("a failed take left the chat here: %+v", ended)
	}
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == c.ID && r.Taking {
			t.Fatalf("the list still says a take is running: %+v", r)
		}
	}
}

// A finished take stops counting only once it holds the inventory lock, and a
// cached row that still shows the chat elsewhere is never republished as a
// take that ended: a client that stopped waiting would read it as not moved.
func TestAppTakeInFlightNeverEndsBeforeTheChatIsHere(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	backend, rec, wire := appDeviceBackend(t, bHome)
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				return true
			}
		}
		return false
	})
	taking := func() bool {
		backend.chats.mu.Lock()
		defer backend.chats.mu.Unlock()
		return backend.chats.taking[c.ID] > 0
	}
	changedSince := func(from int) []appwire.ChatRow {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		var out []appwire.ChatRow
		for i := from; i < len(rec.data); i++ {
			if changed, ok := rec.data[i].(appwire.ChatChanged); ok && changed.Chat.Cell == c.ID {
				out = append(out, changed.Chat)
			}
		}
		return out
	}
	events := func() int {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.events)
	}

	backend.chats.countTake(c.ID, 1)
	backend.chats.countTake(c.ID, -1)
	from := events()
	backend.publishTakingHere(c.ID)
	if rows := changedSince(from); len(rows) != 0 {
		t.Fatalf("a chat still elsewhere was republished after its take: %+v", rows)
	}

	backend.chats.countTake(c.ID, 1)
	backend.chats.op.Lock()
	settled := make(chan error, 1)
	go func() { settled <- backend.settleTake(t.Context(), b, c.ID) }()
	for range 100 {
		runtime.Gosched()
	}
	// The follower holding the lock emits rows from before the take; they
	// must still say it runs.
	if !taking() {
		backend.chats.op.Unlock()
		t.Fatal("a finished take stopped counting before it held the inventory lock")
	}
	backend.chats.op.Unlock()
	if err := <-settled; err != nil {
		t.Fatal(err)
	}
	if taking() {
		t.Fatal("a settled take still counts as running")
	}
}
