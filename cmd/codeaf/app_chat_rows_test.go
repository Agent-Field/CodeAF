package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestAppChatRowsR1PresenceIsIndependentOfClosedChat(t *testing.T) {
	celltest.Available(t)
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/dir/list" {
				next.ServeHTTP(w, r)
				return
			}
			reply := httptest.NewRecorder()
			next.ServeHTTP(reply, r)
			var listing directory.Listing
			if err := json.Unmarshal(reply.Body.Bytes(), &listing); err != nil {
				t.Error(err)
				return
			}
			for id, c := range listing.Cells {
				c.Lease.Expires = listing.Now - 1
				listing.Cells[id] = c
			}
			for key, values := range reply.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(reply.Code)
			_ = json.NewEncoder(w).Encode(listing)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	homes := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	id, err := identity.Ensure(homes[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, home := range homes[1:] {
		if _, err := identity.Adopt(home, id, false); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	var holder *syncsetup.Sync
	for i, name := range []string{"Mac", "Windows", "Sleeping"} {
		s := appDeviceSync(t, homes[i])
		if i == 0 {
			holder = s
		}
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, name); err != nil {
			t.Fatal(err)
		}
		c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	feed := holder.Follow()
	defer feed.Close()
	_, _, wire := appDeviceBackend(t, homes[1])
	var rows map[string]appwire.ChatRow
	appSyncWait(t, "R1 holder presence", func() bool {
		rows = map[string]appwire.ChatRow{}
		for _, row := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			rows[row.Cell] = row
		}
		row := rows[ids[0]]
		return row.Online != nil && *row.Online
	})
	// The explicit computer presence snapshot also establishes the offline
	// holder; absence from an unfinished watch snapshot is still unknown.
	machineCall[appwire.DevicesResult](t, wire, "devices.list", struct{}{})
	for _, row := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		rows[row.Cell] = row
	}
	for _, id := range ids[:2] {
		row := rows[id]
		if row.Status != "off" || row.Online == nil || !*row.Online || row.Line != "" || row.TakeoverLine != "" {
			t.Errorf("R1 online holder with closed chat: %+v", row)
		}
	}
	row := rows[ids[2]]
	if row.Online == nil || *row.Online || row.Line != "Sleeping offline" || row.TakeoverLine != "Sleeping offline" {
		t.Errorf("R1 offline holder: %+v", row)
	}
}

func TestAppChatRowsR2SavedTurnTime(t *testing.T) {
	celltest.RequireEngine(t)
	relay := e2WireRelay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	home, peer := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(peer, id, false); err != nil {
		t.Fatal(err)
	}
	c := appFinishedCell(t, home, "Saved R2")
	meta, err := session.LoadMeta(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	saved := time.Date(2026, 10, 28, 3, 54, 0, 123000000, time.UTC)
	e := cellstore.Engine{DataRoot: filepath.Join(home, "v3", "stores"), Workspace: meta.Workspace, Now: func() time.Time { return saved }}
	if _, err := e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	b, _, _ := appDeviceBackend(t, home)
	if _, err := b.ListChats(); err != nil {
		t.Fatal(err)
	}
	_, _, wire := appDeviceBackend(t, peer)
	check := func(want time.Time) {
		t.Helper()
		rows := machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats
		if len(rows) != 1 || !rows[0].SavedAt.Equal(want) {
			t.Errorf("R2 saved_at: %+v, want %s", rows, want)
		}
		for _, row := range rows {
			if strings.Contains(row.TakeoverLine, "Last saved") || strings.Contains(row.TakeoverLine, "UTC") || strings.Contains(row.TakeoverLine, "ago") {
				t.Errorf("R2 timestamp in takeover line: %+v", row)
			}
		}
	}
	check(saved)
	s := appDeviceSync(t, home)
	v, err := s.Dir.Acquire(t.Context(), c.ID, directory.AcquireOpts{})
	if err != nil {
		t.Fatal(err)
	}
	saved = time.Date(2026, 10, 28, 4, 0, 0, 456000000, time.UTC)
	if err := os.WriteFile(filepath.Join(meta.Workspace, "new-turn.txt"), []byte("new saved work"), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := e.Seal(t.Context(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	// A later seal must not lend its timestamp to the head being uploaded.
	newer := saved.Add(time.Minute)
	e.Now = func() time.Time { return newer }
	if _, err := e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	publisher := cellsync.Publisher{Engine: e.Sync(cellstore.SyncKeys{CellKey: id.CellKey(), Dedup: id.DedupSecret()}, s.Ledger), Dir: s.Dir, Store: s.Store}
	driving := cellsync.Driving{Cell: c, Fence: v.Cell.Lease.Fence, Head: v.Cell.Head}
	if err := publisher.Publish(t.Context(), &driving, head.Turn.ID, cellsync.PublishInfo{Class: string(cell.FilesOnly)}); err != nil {
		t.Fatal(err)
	}
	check(saved)
	if err := s.Dir.Release(t.Context(), c.ID, driving.Fence); err != nil {
		t.Fatal(err)
	}
	check(saved)
}

func TestAppChatStartupR3QuietRelease(t *testing.T) {
	celltest.RequireEngine(t)
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	var releases atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/release") {
			svc.Handler.ServeHTTP(w, r)
			return
		}
		reply := httptest.NewRecorder()
		svc.Handler.ServeHTTP(reply, r)
		releases.Add(1)
		if reply.Code >= 400 {
			t.Errorf("R3 refused release: %d %s", reply.Code, reply.Body.String())
		}
		for key, values := range reply.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(reply.Body.Bytes())
	}))
	t.Cleanup(func() { _ = svc.Close(); relay.Close() })
	t.Setenv(syncsetup.URLVar, relay.URL)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, home)
	if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, "Mac"); err != nil {
		t.Fatal(err)
	}
	c := appFinishedCell(t, home, "Startup R3")
	b, _, _ := appDeviceBackend(t, home)
	// List joins the finite startup pass without opening a chat.
	if _, err := b.ListChats(); err != nil {
		t.Fatal(err)
	}
	v, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	head, err := cellstore.Head(c)
	if err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 || v.Cell.Head != head.Turn.ID || v.Cell.Lease.Device != s.Device.ID() || v.Cell.Lease.Fence != 1 || v.Cell.Lease.Expires != 0 || v.Cell.Lease.ReleasedAt != 0 {
		t.Fatalf("R3 startup state: releases=%d cell=%+v", releases.Load(), v.Cell)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := appDeviceBackend(t, home)
	if _, err := restarted.ListChats(); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 {
		t.Fatal("R3 restart released an existing chat")
	}
}
