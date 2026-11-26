package main

import (
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
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
	if !slices.Contains(w.hello, appwire.FeatureChatsSend) {
		t.Fatal("hello does not offer chats.send where relay storage is not closed")
	}
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

// A message this computer cannot leave, because its relay does not answer,
// is refused in the person's words and with the unreachable code, never the
// transport's; and the full outbox says the limit is this computer's across
// every chat, spelled from directory.MaxNotes.
func TestAMessageToAChatElsewhereSaysWhyItDidNotGo(t *testing.T) {
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
	_, _, w := appDeviceBackend(t, deskDir)
	for i := range directory.MaxNotes {
		machineCall[appwire.ChatSendResult](t, w, "chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "note " + string(rune('a'+i))})
	}
	_, full := w.call("chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "one too many"})
	if full == nil || full.Message != appNoteWords[syncsetup.ErrOutboxFull] || !strings.HasPrefix(full.Message, "Eight messages sent from this computer, in this chat or others") {
		t.Fatalf("full outbox = %+v; want the limit across every chat", full)
	}
	relay.CloseClientConnections()
	relay.Close()
	_, down := w.call("chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "while the relay is down"})
	if down == nil || down.Code != appwire.CodeUnreachable || down.Message != appNoteUnreachable {
		t.Fatalf("relay down = %+v; want %q with the unreachable code", down, appNoteUnreachable)
	}
}

// While hosted sync storage is off a message to a chat another computer holds
// would have to wait on the sync service as sealed text, and nothing is kept
// there until storage is approved. So the verb is absent, not broken: hello
// does not offer chats.send, a send that arrives anyway is refused in the
// person's words and leaves nothing in this computer's record on the relay, and
// approving storage brings the verb back.
func TestAMessageToAChatElsewhereIsNotOfferedAndLeavesNothingWhileSyncStorageIsOff(t *testing.T) {
	celltest.Available(t)
	// The relay answers the storage question the way the hosted one does, so
	// the app's own refreshes keep what this test saved instead of replacing it
	// with the answer of a relay that has no such permission.
	var says atomic.Value
	says.Store("off")
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/store/stats" {
				_, _ = w.Write([]byte(`{"storage":"` + says.Load().(string) + `"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
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
	for _, home := range []string{towerDir, deskDir} {
		if err = syncsetup.SaveStorage(home, relay.URL, id.ID(), "off"); err != nil {
			t.Fatal(err)
		}
	}

	backend, _, w := appDeviceBackend(t, deskDir)
	if slices.Contains(w.hello, appwire.FeatureChatsSend) {
		t.Fatal("hello offers chats.send while relay storage is off")
	}
	_, refused := w.call("chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "PLAINTEXT-NOTE-MARKER-77d2 while storage is off"})
	if refused == nil || refused.Message != appNoteWords[syncwords.ErrNotesWaitForStorage] || !strings.Contains(strings.ToLower(refused.Message), "relay storage") {
		t.Fatalf("a send while storage is off = %+v; want %q", refused, appNoteWords[syncwords.ErrNotesWaitForStorage])
	}
	l, err := tower.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(l.Devices[desk.Device.ID()].Outbox); n != 0 {
		t.Fatalf("the relay holds %d sealed messages from the desk", n)
	}

	// Close the first connection and join its background refresh before
	// changing the relay answer, as a real engine restart would.
	w.close()
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	<-backend.sweepDone
	says.Store("on")
	for _, peer := range []*syncsetup.Sync{tower, desk} {
		if _, err := peer.RefreshStorage(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	_, _, again := appDeviceBackend(t, deskDir)
	if !slices.Contains(again.hello, appwire.FeatureChatsSend) {
		t.Fatal("hello does not offer chats.send once relay storage is approved")
	}
	sent := machineCall[appwire.ChatSendResult](t, again, "chats.send", appwire.ChatSendParams{Cell: c.ID, Text: "now that storage is on"})
	if sent.Note.State != appwire.ChatNoteWaiting {
		t.Fatalf("a send with storage approved = %+v; want a waiting message", sent.Note)
	}
}
