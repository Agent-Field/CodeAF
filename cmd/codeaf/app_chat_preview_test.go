package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

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
	machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: stillRemote.Seen})
	workingChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.Here && r.Title == title && !r.CanPreview })
}
