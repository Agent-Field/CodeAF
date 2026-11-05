package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The provider holds the turn on a channel so both computers can read its
// working state without depending on how long a model or command takes.
func TestAppTakenChatAdvertisesWorkingDuringTurn(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if hold.Load() && len(req.Tools) > 0 {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
		}
	}))
	defer model.Close()
	defer close(release)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	// Acquisition precedes asynchronous drive startup. Hold its roster read
	// until the first send has reached the provider, forcing the startup race.
	var holdStartup, holdPublish atomic.Bool
	var taker atomic.Value
	taker.Store("")
	publishing, publish := make(chan struct{}), make(chan struct{})
	defer close(publish)
	starting, start := make(chan struct{}, 1), make(chan struct{})
	var startOnce sync.Once
	resumeStartup := func() { startOnce.Do(func() { close(start) }) }
	defer resumeStartup()
	_, _ = heldChatRelay(t, "fixture", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if holdStartup.Load() && r.Method == http.MethodGet && r.URL.Path == "/v1/dir/list" && r.Header.Get(reqsign.HeaderCert) == taker.Load().(string) {
				select {
				case starting <- struct{}{}:
				default:
				}
				select {
				case <-start:
				case <-r.Context().Done():
					return
				}
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/publish") && holdPublish.CompareAndSwap(true, false) {
				close(publishing)
				select {
				case <-publish:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
				taker.Store(r.Header.Get(reqsign.HeaderCert))
			}
		})
	})
	dirA, dirB := outageHomes(t, os.Getenv(syncsetup.URLVar))
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "create source")
	e2Quit(t, a)
	row := workingChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.CanTake && r.Title != "" })
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	holdStartup.Store(true)
	a = e2StartApp(t, dirA)
	hold.Store(true)
	machineCall[any](t, b.w, "agents.send", map[string]any{"id": taken.ID, "text": "held turn", "client_id": "held"})
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("turn did not reach provider")
	}
	select {
	case <-starting:
	case <-time.After(30 * time.Second):
		t.Fatal("drive startup did not reach relay")
	}
	resumeStartup()
	working := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.Working })
	if working.Here {
		t.Fatal("observer owns the taken chat")
	}
	holdPublish.Store(true)
	release <- struct{}{}
	b.wait(t, func(f e2Frame) bool { return f.Event == appwire.EventTurnDone })
	select {
	case <-publishing:
	case <-time.After(30 * time.Second):
		t.Fatal("turn did not publish")
	}
	var during appwire.ChatRow
	for _, r := range machineCall[appwire.ChatsResult](t, a.w, "chats.list", struct{}{}).Chats {
		if r.Cell == row.Cell {
			during = r
		}
	}
	if !during.Working {
		t.Fatalf("working cleared before the final publish: %+v", during)
	}
	publish <- struct{}{}
	ended := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return !r.Working && r.Seen.Head != row.Seen.Head })
	if ended.Seen.Head == "" {
		t.Fatal("finished turn has no shared head")
	}
}

// Chat changes wake the assertion; every assertion reads the list door the
// observing desktop uses, so an older event cannot satisfy a newer state.
func workingChatRow(t *testing.T, app *e2App, id string, ready func(appwire.ChatRow) bool) appwire.ChatRow {
	t.Helper()
	timeout := time.After(45 * time.Second)
	for {
		for _, row := range machineCall[appwire.ChatsResult](t, app.w, "chats.list", struct{}{}).Chats {
			if (id == "" || row.Cell == id) && ready(row) {
				return row
			}
		}
		select {
		case <-app.more:
		case <-timeout:
			t.Fatalf("chats.list never reached the expected state for %s", id)
		}
	}
}
