package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// P2: reopen the durable relay on the same address with empty presence memory,
// while the apps stay alive. Delay B's reconnect until A has read the fresh relay.
func TestAppRelayRestartP2UnknownUntilPeerReturnsThenActuallyOffline(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	model := appTwoMessageSource(t, "echo presence")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	r := &outageRelay{t: t, clock: &outageClock{}, connections: map[net.Conn]bool{}}
	r.blocked.Store("")
	store := t.TempDir()
	var db *directory.SQLite
	blobs := blobstore.NewMemory()
	var reads atomic.Int64
	start := func() {
		var err error
		db, err = directory.OpenSQLite(filepath.Join(store, "directory.db"), r.clock.Now)
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		auth := reqsign.AuthenticateAt(r.clock.Now)
		mux.Handle("/v1/dir/", directory.Handler(auth, func(string) (directory.Directory, error) { return db, nil }))
		mux.Handle("/v1/store/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return blobs, nil }))
		r.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mux.ServeHTTP(w, req)
			if req.URL.Path == "/v1/dir/presence" {
				reads.Add(1)
			}
		})
		r.start()
	}
	start()
	t.Cleanup(func() { r.stop(); _ = db.Close() })
	dirA, dirB := outageHomes(t, r.server.URL)
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, b.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, b, created.ID, "B stays running")
	row := outageChatRow(t, a, "", func(row appwire.ChatRow) bool { return row.Online != nil && *row.Online && !row.Here })
	dev, err := identity.LoadDevice(dirB)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := json.Marshal(dev.Cert)
	r.blocked.Store(base64.RawURLEncoding.EncodeToString(cert))
	a.mu.Lock()
	outageMark := len(a.events)
	a.mu.Unlock()
	r.stop()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Wait for the observing app's ordinary outage expiry, so the restart
	// cannot merely retain its old online value and hide a bad refresh.
	seen := 0
	a.wait(t, func(f e2Frame) bool {
		seen++
		if seen <= outageMark {
			return false
		}
		var changed appwire.DevicesResult
		if f.Event != appwire.EventDevicesChanged || json.Unmarshal(f.Data, &changed) != nil {
			return false
		}
		for _, peer := range changed.Devices.Devices {
			if peer.ID == dev.ID() && peer.Online == nil {
				return true
			}
		}
		return false
	})
	a.mu.Lock()
	mark := len(a.events)
	a.mu.Unlock()
	before := reads.Load()
	start()
	outageWait(t, "P2 A refreshed presence after relay restart", func() bool { return reads.Load() > before })
	unknown := outageChatRow(t, a, row.Cell, func(row appwire.ChatRow) bool { return row.Online == nil })
	if strings.Contains(unknown.Line, "offline") {
		t.Fatalf("P2 unknown peer has an offline line: %+v", unknown)
	}
	r.blocked.Store("")
	outageChatRow(t, a, row.Cell, func(row appwire.ChatRow) bool { return row.Online != nil && *row.Online })
	a.mu.Lock()
	events := append([]e2Frame(nil), a.events[mark:]...)
	a.mu.Unlock()
	for _, f := range events {
		var devices appwire.DevicesResult
		if f.Event == appwire.EventDevicesChanged && json.Unmarshal(f.Data, &devices) == nil {
			for _, peer := range devices.Devices.Devices {
				if peer.ID == dev.ID() && peer.Online != nil && !*peer.Online {
					t.Fatalf("P2 running peer was announced offline after restart: %s", f.Data)
				}
			}
		}
		var changed appwire.ChatChanged
		if f.Event == appwire.EventChatChanged && json.Unmarshal(f.Data, &changed) == nil && changed.Chat.Cell == row.Cell && (changed.Chat.Online != nil && !*changed.Chat.Online || strings.Contains(changed.Chat.Line, "offline")) {
			t.Fatalf("P2 running peer's chat announced offline: %s", f.Data)
		}
	}
	e2Quit(t, b)
	r.clock.Advance(directory.Window(directory.DefaultBeat) + time.Second)
	outageChatRow(t, a, row.Cell, func(row appwire.ChatRow) bool { return row.Online != nil && !*row.Online })
}
