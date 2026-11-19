package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQAStorageOffDesktopQuitRecordsLocalSave(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	_, _, _ = heldChatRelay(t, "fixture", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/store/stats" {
				_, _ = w.Write([]byte(`{"storage":"off"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	relay := os.Getenv(syncsetup.URLVar)
	dir, _ := outageHomes(t, relay)
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(dir, relay, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	a := e2StartApp(t, dir)
	agent := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, agent.ID, "save this turn here")
	e2Quit(t, a)
	raw, err := os.ReadFile(a.log)
	if err != nil || strings.Count(string(raw), quitStorageOffWord) != 1 {
		t.Fatalf("desktop engine did not log exactly one local-save receipt: %v, %s", err, raw)
	}
}

// Model the client contract of a hosted operator revocation on a loopback
// relay. The first definitive answer is an upload refusal, not a local edit.
func TestQADesktopStorageRevokedWhileUploading(t *testing.T) {
	celltest.RequireEngine(t)
	t.Run("pass-through is available", func(t *testing.T) {
		t.Setenv(syncsetup.LiveTransferVar, "")
		qaDesktopStorageRevokedWhileUploading(t, chatlist.RelayStorageOffLive)
	})
	t.Run("pass-through is disabled", func(t *testing.T) {
		t.Setenv(syncsetup.LiveTransferVar, "off")
		qaDesktopStorageRevokedWhileUploading(t, chatlist.RelayStorageOff)
	})
}

func qaDesktopStorageRevokedWhileUploading(t *testing.T, storageNotice string) {
	t.Helper()
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	var revoke atomic.Bool
	var refused atomic.Int32
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/v1/store/stats":
				state := "on"
				if refused.Load() != 0 {
					state = "off"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"storage": state})
			case revoke.Load() && r.URL.Path == "/v1/store/frames" && r.Method == http.MethodPost:
				refused.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"err":"storage_off"}`))
			default:
				next.ServeHTTP(w, r)
			}
		})
	})
	dir, peerDir := outageHomes(t, relay.URL)
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(dir, relay.URL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	a, peer := e2StartApp(t, dir), e2StartApp(t, peerDir)
	agent := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, agent.ID, "published before revocation")
	row := outageChatRow(t, peer, "", func(r appwire.ChatRow) bool { return r.Seen.Head != "" && !r.Working })

	revoke.Store(true)
	e2Turn(t, a, agent.ID, "the turn whose upload was refused")
	a.wait(t, func(f e2Frame) bool {
		var event appwire.EntryAdded
		return f.Event == appwire.EventEntryAdded && json.Unmarshal(f.Data, &event) == nil &&
			event.Entry.Agent == agent.ID && event.Entry.Kind == appwire.EntryNotice && event.Entry.Text == storageNotice
	})
	if syncsetup.ReportedStorage(dir, relay.URL, id.ID()) != "off" || refused.Load() == 0 {
		t.Fatal("the relay's upload refusal did not close the local storage gate")
	}
	attempts := refused.Load()
	const local = "the turn saved locally after revocation"
	e2Turn(t, a, agent.ID, local)
	e2Quit(t, a)
	if refused.Load() != attempts {
		t.Fatalf("the client made %d more upload attempts after storage was known off", refused.Load()-attempts)
	}
	if count := e2SyncNoticeCount(a, agent.ID, func(line string) bool { return line == storageNotice }); count != 1 {
		t.Fatalf("received %d notices for one storage revocation", count)
	}
	journal, err := os.ReadFile((session.Place{Dir: takeRootForAt(dir, row.Cell)}).Transcript())
	if err != nil || !strings.Contains(string(journal), local) {
		t.Fatalf("the revoked chat's last turn was not kept locally: %v", err)
	}
	log, err := os.ReadFile(a.log)
	if err != nil || strings.Count(string(log), quitStorageOffWord) != 1 {
		t.Fatalf("desktop quit after revocation did not give one local-save receipt: %v, %s", err, log)
	}
}

func e2SyncNoticeCount(a *e2App, agent string, match func(string) bool) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, frame := range a.events {
		var event appwire.EntryAdded
		if frame.Event == appwire.EventEntryAdded && json.Unmarshal(frame.Data, &event) == nil &&
			event.Entry.Agent == agent && event.Entry.Kind == appwire.EntryNotice && match(event.Entry.Text) {
			count++
		}
	}
	return count
}
