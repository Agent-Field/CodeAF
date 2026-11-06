package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The sender's half through the app wire: chats.send leaves a message for the
// computer holding the chat, the chat's row says it is waiting, and once that
// computer says it took it the row says delivered and the outbox empties.
func TestAMessageSentToAChatElsewhereIsWaitingThenDeliveredOnItsRow(t *testing.T) {
	celltest.Available(t)
	relay := e2WireRelay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	towerDir, deskDir := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(towerDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(deskDir, id, false); err != nil {
		t.Fatal(err)
	}
	tower := appDeviceSync(t, towerDir)
	if err = syncsetup.PutOwnDevice(t.Context(), tower.Dir, tower.Identity, tower.Device, "tower"); err != nil {
		t.Fatal(err)
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tower.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "sealed-head"}); err != nil {
		t.Fatal(err)
	}
	desk := appDeviceSync(t, deskDir)
	if err = syncsetup.PutOwnDevice(t.Context(), desk.Dir, desk.Identity, desk.Device, "desk"); err != nil {
		t.Fatal(err)
	}
	_, rec, w := appDeviceBackend(t, deskDir)
	if _, err := w.call("chats.send", appwire.ChatSendParams{Cell: c.ID, Text: " "}); err == nil {
		t.Fatal("an empty message was sent")
	}
	sent := machineCall[appwire.ChatSendResult](t, w, "chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "add a LICENSE too", ClientID: "x"})
	if sent.Note.ID == "" || sent.Note.State != appwire.ChatNoteWaiting || sent.Note.Text != "add a LICENSE too" {
		t.Fatalf("chats.send = %+v; want a waiting message", sent.Note)
	}
	notes := func() []appwire.ChatNote {
		for _, r := range machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID {
				return r.Notes
			}
		}
		return nil
	}
	if got := notes(); len(got) != 1 || got[0].ID != sent.Note.ID || got[0].State != appwire.ChatNoteWaiting {
		t.Fatalf("row notes = %+v; want the message waiting", got)
	}
	if err = tower.TookNotes(t.Context(), []string{sent.Note.ID}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		r := data.(appwire.ChatChanged).Chat
		return r.Cell == c.ID && len(r.Notes) == 1 && r.Notes[0].State == appwire.ChatNoteDelivered
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		l, err := tower.Dir.List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Devices[desk.Device.ID()].Outbox) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the desk's outbox still lists a message its holder took")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := notes(); len(got) != 1 || got[0].State != appwire.ChatNoteDelivered {
		t.Fatalf("row notes after the drop = %+v; delivered stays on the row", got)
	}
}
