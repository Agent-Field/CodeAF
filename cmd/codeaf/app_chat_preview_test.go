package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestAppEntriesMarksLocalHistoryWhenRemoteViewDisappears(t *testing.T) {
	appDayProfile(t)
	const id = "old-local-chat"
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(file, []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"The old local history\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &appAgent{transcript: file, view: appwire.Agent{ID: id, Location: &appwire.Location{
		Cell: "saved-chat", Seen: &appwire.Seen{Head: "announced-remote-head"},
	}}}
	b := &appBackend{agents: map[string]*appAgent{id: a}, now: time.Now, state: &appState{}, emit: newAppRecorder()}
	page, err := b.EntriesWithFallback(id, "", appwire.MaxEntries)
	if err != nil || !page.LocalFallback || page.RemoteHead != "" || len(page.Entries) != 1 || page.Entries[0].Text != "The old local history" {
		t.Fatalf("reading after losing the remote view = %+v, %v", page, err)
	}
	// A move notice can arrive before its location fields are populated.
	a.view.Location = &appwire.Location{Line: "This chat moved to another computer."}
	b.chats.byAgent = map[string]appwire.ChatRow{id: {
		Cell: "saved-chat", Seen: appwire.Seen{Head: "announced-remote-head"},
	}}
	page, err = b.EntriesWithFallback(id, "", appwire.MaxEntries)
	if err != nil || !page.LocalFallback || len(page.Entries) != 1 {
		t.Fatalf("reading after a move notice and a missing remote view = %+v, %v", page, err)
	}

	a.view.Location = nil
	b.chats.byAgent = nil
	page, err = b.EntriesWithFallback(id, "", appwire.MaxEntries)
	if err != nil || page.LocalFallback || page.RemoteHead != "" || len(page.Entries) != 1 {
		t.Fatalf("ordinary local page = %+v, %v", page, err)
	}
}

func TestAppPassThroughTitleAndPreview(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	t.Setenv(syncsetup.LiveTransferVar, "")
	service := relayserve.New(relayserve.Config{Store: cellstoretest.EngineDir(t)})
	t.Cleanup(func() { _ = service.Close() })
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/store/stats" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"storage":"off"}`)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
			t.Error("a storage-off client tried to upload a frame")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		service.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(relay.Close)
	dirA, dirB := outageHomes(t, relay.URL)
	for _, home := range []string{dirA, dirB} {
		s := appDeviceSync(t, home)
		if err := syncsetup.SaveStorage(home, s.Relay, s.Identity.ID(), "off"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	const prompt = "Which records have arrived?"
	const title = "Pass-through title on both computers"
	e2Turn(t, a, created.ID, prompt)
	machineCall[appwire.Agent](t, a.w, "agents.rename", map[string]any{"id": created.ID, "title": title})

	row := workingChatRow(t, b, "", func(r appwire.ChatRow) bool {
		return r.Title == title && r.CanPreview && r.CanTake && !r.Working
	})
	preview := machineCall[appwire.ChatPreview](t, b.w, "chats.preview", appwire.ChatPreviewParams{Cell: row.Cell})
	if !strings.Contains(preview.LastUser, prompt) || preview.LastAssistant == "" {
		t.Fatalf("preview did not contain the saved exchange: %+v", preview)
	}
	stillRemote := workingChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return !r.Here })
	if stillRemote.Seen != row.Seen || stillRemote.LocalAgent != "" {
		t.Fatal("preview changed the lease or installed a chat")
	}
	workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.Here })
	machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: stillRemote.Seen})
	workingChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.Here && r.Title == title && !r.CanPreview })
	remote := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return !r.Here && r.Seen.Device != row.Seen.Device })
	a.wait(t, func(f e2Frame) bool {
		var changed appwire.AgentChanged
		if f.Event != appwire.EventAgentChanged || json.Unmarshal(f.Data, &changed) != nil || changed.Agent.ID != created.ID {
			return false
		}
		l := changed.Agent.Location
		return l != nil && !l.Here && l.Cell == row.Cell && l.Seen != nil && l.Seen.Device == remote.Seen.Device
	})
	page := machineCall[appwire.EntriesResult](t, a.w, "agents.entries", appwire.EntriesParams{ID: created.ID})
	if page.LocalFallback || page.RemoteHead != remote.Seen.Head || len(page.Entries) == 0 {
		t.Fatalf("the moved chat did not return a saved page at head %s: %+v", remote.Seen.Head, page)
	}
}
