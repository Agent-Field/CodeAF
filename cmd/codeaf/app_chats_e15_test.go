package main

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// E15 holds the watch at its initial online snapshot. Chat inventory joins
// after those frames, so no later frame can rescue a missed initial event.
func TestAppChatsE15AlreadyConnectedWatchEmitsOnlineRow(t *testing.T) {
	celltest.Available(t)
	var version uint64
	var peer string
	appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/dir/watch" {
				next.ServeHTTP(w, r)
				return
			}
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			ctx := conn.CloseRead(r.Context())
			for _, frame := range []string{
				fmt.Sprintf(`{"v":%d}`, version),
				fmt.Sprintf(`{"t":"presence","device":%q,"online":true,"at":1}`, peer),
			} {
				if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
					return
				}
			}
			<-ctx.Done()
		})
	})
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	peer = a.Device.ID()
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
	listing, err := a.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	version = listing.Version
	_, rec, wire := appDeviceBackend(t, bHome)
	feed := b.Follow()
	defer feed.Close()
	appSyncWait(t, "E15 watch snapshot before chat subscription", func() bool {
		state := feed.State()
		return state.Up && state.Version == version && slices.Contains(state.Online, peer)
	})
	listed := machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	if len(listed.Chats) != 1 || listed.Chats[0].Online == nil || !*listed.Chats[0].Online {
		t.Fatalf("E15 initial online chat: %+v", listed)
	}
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		row := data.(appwire.ChatChanged).Chat
		return row.Cell == c.ID && row.Online != nil && *row.Online
	})
}
