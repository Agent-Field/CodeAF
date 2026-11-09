package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestStorageOffSeatedTurnStopsWhenAnotherComputerContinuesIt(t *testing.T) {
	for _, mode := range []string{"another computer", "own lapsed hold", "free chat", "never shared", "unreachable"} {
		t.Run(mode, func(t *testing.T) {
			cfg, c := spokenDoorChat(t)
			t.Cleanup(cellstore.StopSpawned)
			id, err := identity.Ensure(home.Dir())
			if err != nil {
				t.Fatal(err)
			}
			var clock atomic.Int64
			clock.Store(time.Now().UnixMilli())
			dir := directory.NewMemory(func() time.Time { return time.UnixMilli(clock.Load()) })
			defer dir.Close()
			own, other := dir.For(mustGateDevice(t)), dir.For("other-device")
			if mode != "never shared" {
				if _, err := own.Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
					t.Fatal(err)
				}
				if err := cellstore.NotePublished(c, ""); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int32
			handler := directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return dir, nil })
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if mode == "unreachable" {
					http.Error(w, "unavailable", 503)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer relay.Close()
			t.Setenv(syncsetup.URLVar, relay.URL)
			if err := syncsetup.SaveStorage(home.Dir(), relay.URL, id.ID(), "off"); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					fmt.Fprint(w, `{"data":[]}`)
					return
				}
				var request struct {
					Tools  []json.RawMessage
					Stream bool
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if len(request.Tools) == 0 {
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"fixture"},"finish_reason":"stop"}]}`)
					return
				}
				call := calls.Add(1)
				if call == 2 {
					switch mode {
					case "another computer":
						_, err := other.Acquire(r.Context(), c.ID, directory.AcquireOpts{Force: true})
						if err != nil {
							t.Error(err)
						}
					case "own lapsed hold":
						clock.Add(int64((24 * time.Hour) / time.Millisecond))
					case "free chat":
						view, err := own.Cell(r.Context(), c.ID)
						if err != nil {
							t.Error(err)
						} else if err := own.Release(r.Context(), c.ID, view.Cell.Lease.Fence); err != nil {
							t.Error(err)
						}
					}
					// A fresh model response after the cache bound must observe the new owner.
					time.Sleep(2100 * time.Millisecond)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if call <= 2 {
					args, _ := json.Marshal(map[string]string{"command": fmt.Sprintf("printf ran > %s", filepath.Join(cfg.Workspace, fmt.Sprintf("command-%d", call)))})
					fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-%d\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":%s}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", call, strconv.Quote(string(args)))
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"finished\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer model.Close()
			cfg.BaseURL = model.URL
			cfg = v3Seated(cfg)
			cfg.Seals.(*cellstore.SealWatch).OnTail = nil
			d := syncDrives.renewalsOf(c.ID)
			startupFinished(d)
			agent, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			events, err := agent.Submit(t.Context(), "run two commands")
			if err != nil {
				t.Fatal(err)
			}
			var said strings.Builder
			for event := range events {
				if event.Err != nil {
					t.Logf("turn event %v: %v", event.Kind, event.Err)
				}
				said.WriteString(event.Text)
			}
			if _, err := os.Stat(filepath.Join(cfg.Workspace, "command-1")); err != nil {
				t.Fatalf("first command never ran: %v; %s", err, said.String())
			}
			_, secondErr := os.Stat(filepath.Join(cfg.Workspace, "command-2"))
			if mode == "another computer" {
				if !os.IsNotExist(secondErr) {
					t.Errorf("second command executed after displacement: %v", secondErr)
				}
				if !strings.Contains(said.String(), chatlist.Superseded("other-de")) {
					t.Errorf("missing viewer sentence: %s", said.String())
				}
				if err := d.Gate(); err == nil {
					t.Error("displaced window resumed tools")
				}
				turns, err := cellstore.Turns(c)
				if err != nil {
					t.Fatal(err)
				}
				eng := cellstore.EngineFor(cfg.Workspace)
				if _, err := d.Store(eng).Seal(t.Context(), c, cellstore.TurnInfo{}); !errors.Is(err, cellstore.ErrReadOnly) {
					t.Errorf("viewer seal = %v, want read only", err)
				}
				after, err := cellstore.Turns(c)
				if err != nil {
					t.Fatal(err)
				}
				if len(after) != len(turns) {
					t.Errorf("viewer admitted a new seal: %d -> %d", len(turns), len(after))
				}
				s, ok, err := syncsetup.Open(home.Dir())
				if err != nil || !ok {
					t.Fatalf("sync: %v", err)
				}
				continued, err := s.Continuer(eng, syncsetup.TakeOptions{RootFor: func(string) string { return c.Root }}).Take(t.Context(), c.ID)
				if err != nil || !continued.LocalOnly {
					t.Fatalf("continue: %+v, %v", continued, err)
				}
				if err := d.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				cfg = v3Seated(cfg)
				resumed := syncDrives.renewalsOf(c.ID)
				startupFinished(resumed)
				if err := resumed.Gate(); err != nil {
					t.Fatalf("continued writer refused tools: %v", err)
				}
				if _, err := resumed.Store(eng).Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
					t.Fatalf("continued writer refused seal: %v", err)
				}
				after, err = cellstore.Turns(c)
				if err != nil || len(after) <= len(turns) {
					t.Fatalf("continuing did not resume sealing: %d -> %d, %v", len(turns), len(after), err)
				}

			} else if secondErr != nil {
				t.Errorf("allowed second command did not run: %v; %s", secondErr, said.String())
			}
			if mode == "never shared" && requests.Load() != 0 {
				t.Errorf("never-shared chat made %d directory requests", requests.Load())
			}
			if d.started() != nil {
				t.Error("storage-off tool gate started an uploading writer")
			}
		})
	}
}

func mustGateDevice(t *testing.T) string {
	t.Helper()
	dev, err := identity.Device(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	return dev.ID()
}
