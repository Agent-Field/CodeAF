package main

import (
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

// A take that acquired the chat keeps saying it runs on every row from before
// it, whatever ends the call: a client that stopped waiting reads taking:false
// on a chat still elsewhere as a take that did not move it.
func TestAppTakeInFlightRowsFromBeforeALandedTakeStillSayItRuns(t *testing.T) {
	const id = "chat"
	var c appChats
	mark := func(row appwire.ChatRow) bool {
		rows := []appwire.ChatRow{row}
		c.markTaking(rows)
		return rows[0].Taking
	}
	before := appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "other", Fence: 4}}
	if mark(before) {
		t.Fatal("no take has started")
	}
	c.countTake(id, 1)
	c.landTake(id, 5)
	c.countTake(id, -1)
	if !mark(before) {
		t.Fatal("a row from before the landed take says it ended")
	}
	if !mark(appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "this", Fence: 5}}) {
		t.Fatal("the taken fence not yet installed here says the take ended")
	}
	if mark(appwire.ChatRow{Cell: id, Here: true, Seen: appwire.Seen{Device: "this", Fence: 5}}) {
		t.Fatal("the row that says the chat is here still says a take runs")
	}
	if mark(before) {
		t.Fatal("a take already seen here kept counting")
	}
	// A later take elsewhere ends it too, so the flag never sticks.
	c.landTake(id, 5)
	if mark(appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "other", Fence: 6}}) || mark(before) {
		t.Fatal("a chat another computer took again still says this take runs")
	}
}

// The cached row republished after a landed take whose reading failed still
// shows the chat elsewhere, so it still says the take runs.
func TestAppTakeInFlightCacheAfterALandedTakeStillSaysItRuns(t *testing.T) {
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
	backend.chats.countTake(c.ID, 1)
	backend.publishTaking()
	backend.chats.landTake(c.ID, row.Seen.Fence+1)
	backend.chats.countTake(c.ID, -1)
	rec.mu.Lock()
	from := len(rec.data)
	rec.mu.Unlock()
	backend.publishTaking()
	if !machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats[0].Taking {
		t.Fatal("the list read after a landed take says it ended with the chat elsewhere")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data[from:] {
		if changed, ok := data.(appwire.ChatChanged); ok && changed.Chat.Cell == c.ID && !changed.Chat.Here && !changed.Chat.Taking {
			t.Fatalf("a landed take was announced as one that did not move the chat: %+v", changed.Chat)
		}
	}
}
