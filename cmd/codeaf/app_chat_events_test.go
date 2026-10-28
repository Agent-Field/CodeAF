package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// R4 holds the watch between its version and presence frames. The relay list
// remains real; its mutations can be read before the watch announces them.
func TestAppChatEventsR4F4UnknownPresenceAndSilentLists(t *testing.T) {
	celltest.Available(t)
	frames := make(chan string, 8)
	startWatch := make(chan struct{})
	reconnect := make(chan struct{}, 1)
	connected := make(chan struct{}, 8)
	var initial atomic.Uint64
	var peerID string
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/dir/presence" {
				_ = json.NewEncoder(w).Encode(directory.PresenceView{Now: 3, Devices: map[string]directory.DevicePresence{peerID: {Online: true}}})
				return
			}
			if r.URL.Path != "/v1/dir/watch" {
				next.ServeHTTP(w, r)
				return
			}
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			connected <- struct{}{}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			go func() {
				defer cancel()
				for {
					_, text, err := conn.Read(ctx)
					if err != nil {
						return
					}
					if string(text) == "ping" {
						_ = conn.Write(ctx, websocket.MessageText, []byte("pong"))
					}
				}
			}()
			select {
			case <-startWatch:
			case <-ctx.Done():
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(`{"v":%d}`, initial.Load()))); err != nil {
				return
			}
			for {
				select {
				case text := <-frames:
					if conn.Write(ctx, websocket.MessageText, []byte(text)) != nil {
						return
					}
				case <-ctx.Done():
					return
				case <-reconnect:
					return
				}
			}
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, aHome)
	peerID = s.Device.ID()
	if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, "Mac"); err != nil {
		t.Fatal(err)
	}
	// Both paired computers already have records, so startup publication does
	// not add a directory mutation to this controlled chat-version fixture.
	other := appDeviceSync(t, bHome)
	if err := syncsetup.PutOwnDevice(t.Context(), other.Dir, other.Identity, other.Device, devname.Name(bHome)); err != nil {
		t.Fatal(err)
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "first-head"}); err != nil {
		t.Fatal(err)
	}
	l, err := s.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	initial.Store(l.Version)
	backend, rec, wire := appDeviceBackend(t, bHome)
	// Represent an already admitted local copy without opening a turn engine.
	a := backend.agent(c.ID)
	backend.locked(func() { a.transcript = filepath.Join(c.Root, cell.TranscriptPath) })
	machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	rec.mu.Lock()
	for _, name := range rec.events {
		if name == appwire.EventAgentChanged || name == appwire.EventChatChanged || name == appwire.EventChatRemoved {
			t.Errorf("R4 list emitted before watch spoke: %s", name)
		}
	}
	rec.mu.Unlock()
	close(startWatch)
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool { return name == appwire.EventChatChanged })
	row := machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats[0]
	if row.Online != nil || strings.Contains(row.Line, "offline") {
		t.Fatalf("R4 unset presence must be omitted: %+v", row)
	}
	frames <- fmt.Sprintf(`{"t":"presence","device":%q,"online":true,"at":1}`, s.Device.ID())
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		row := data.(appwire.ChatChanged).Chat
		return row.Online != nil && *row.Online
	})
	cursor := func() int {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		n := 0
		for i, name := range rec.events {
			if name == appwire.EventChatChanged && rec.data[i].(appwire.ChatChanged).Chat.Cell == c.ID {
				n++
			}
		}
		return n
	}
	before := cursor()
	for range 3 {
		machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	}
	title, err := directory.SealName(directory.MetadataKey(id.CellKey()), "Changed R4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dir.Publish(t.Context(), c.ID, directory.Publish{Fence: 1, OldHead: "first-head", Head: "second-head", Title: title}); err != nil {
		t.Fatal(err)
	}
	listed := machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	if listed.Chats[0].Title != "Changed R4" {
		t.Fatalf("R4 list did not read fresh content: %+v", listed)
	}
	if after := cursor(); after != before {
		t.Fatalf("R4 chats.list emitted: before=%d after=%d", before, after)
	}
	frames <- fmt.Sprintf(`{"v":%d}`, listed.Version)
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Title == "Changed R4"
	})
	// A real presence change may update the same inventory version.
	frames <- fmt.Sprintf(`{"t":"presence","device":%q,"online":false,"at":2}`, s.Device.ID())
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Line == "Mac offline"
	})
	frames <- fmt.Sprintf(`{"t":"presence","device":%q,"online":true,"at":3}`, s.Device.ID())
	appSyncWait(t, "R4 presence returns online", func() bool { return cursor() >= 5 })
	machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	// F4: a watch reconnect repeats the current version, without changing a row.
	<-connected
	initial.Store(listed.Version)
	beforeReconnect := cursor()
	reconnect <- struct{}{}
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		t.Fatal("F4 watch did not reconnect")
	}
	marker, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Dir.Create(t.Context(), marker.ID, directory.CellInit{Head: "marker-head"}); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	frames <- fmt.Sprintf(`{"v":%d}`, latest.Version)
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Cell == marker.ID
	})
	if got := cursor(); got != beforeReconnect {
		t.Fatalf("F4 reconnect emitted %d extra chat changes", got-beforeReconnect)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var changed []appwire.ChatChanged
	for i, name := range rec.events {
		if name == appwire.EventChatChanged && rec.data[i].(appwire.ChatChanged).Chat.Cell == c.ID {
			changed = append(changed, rec.data[i].(appwire.ChatChanged))
		}
	}
	if len(changed) != 5 {
		t.Fatalf("R4 changes=%+v; want unknown, online, title, offline, online", changed)
	}
	for i, event := range changed {
		if i == 0 && event.Chat.Online != nil {
			t.Errorf("R4 initial event invented presence: %+v", event)
		}
		if i < 2 && event.Version != l.Version || i >= 2 && event.Version != listed.Version {
			t.Errorf("R4 version: %+v", event)
		}
	}
}
