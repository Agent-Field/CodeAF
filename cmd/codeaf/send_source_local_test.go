package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type sendSurfaceSource struct{ surfaceMachines }

func (sendSurfaceSource) Follow() dirwatch.Follower { return nil }

func (sendSurfaceSource) Rows(context.Context) ([]chatlist.Row, error)           { return nil, nil }
func (sendSurfaceSource) RowsAt(context.Context) ([]chatlist.Row, uint64, error) { return nil, 0, nil }

type recordingSendAgent struct {
	quietAgent
	sent chan string
	held bool
}

func (a *recordingSendAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, a.held
}

func (a *recordingSendAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	a.sent <- text
	return a.quietAgent.Submit(ctx, text)
}

// Contract E4 reserves the bounded relay read for copies without local authority.
func TestConcreteSendSourceKeepsHeldAndUnpublishedChatsOffTheNetwork(t *testing.T) {
	for _, kind := range []string{"held", "unpublished", "moved"} {
		t.Run(kind, func(t *testing.T) {
			celltest.Available(t)
			home := t.TempDir()
			var lists atomic.Int32
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/dir/list" {
					lists.Add(1)
				}
				<-r.Context().Done()
			}))
			defer relay.Close()
			t.Setenv(syncsetup.URLVar, relay.URL)
			_, err := identity.Ensure(home)
			if err != nil {
				t.Fatal(err)
			}
			dev, err := identity.LoadDevice(home)
			if err != nil {
				t.Fatal(err)
			}
			c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			s, ok, err := syncsetup.Open(home)
			if err != nil || !ok {
				t.Fatalf("open: %v", err)
			}
			if kind == "held" {
				sum := sha256.Sum256([]byte(s.Ledger + "/" + dev.ID() + "/" + c.ID))
				path := filepath.Join(home, "v3", "sync", "holds", hex.EncodeToString(sum[:]))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(map[string]any{"cell": c.ID, "fence": 7})
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "moved" {
				if err := os.WriteFile(filepath.Join(c.Root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"received"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := surfaceMachines{s}
			read := func(offline bool) ([]chatlist.Row, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				defer cancel()
				rows, _, _, err := source.RowsForChatSend(ctx, filepath.Join(c.Root, cell.TranscriptPath), offline, kind == "held")
				return rows, err
			}
			rows, err := read(false)
			if kind == "moved" {
				if err == nil || lists.Load() != 1 {
					t.Fatalf("moved copy reads=%d err=%v", lists.Load(), err)
				}
				_, _ = read(true)
				if lists.Load() != 1 {
					t.Fatal("established outage paid for another read")
				}
			} else {
				if err != nil || lists.Load() != 0 {
					t.Fatalf("%s chat reads=%d err=%v", kind, lists.Load(), err)
				}
				if kind == "held" && (len(rows) != 1 || rows[0].Status != chatlist.Here || rows[0].Fence != 7) {
					t.Fatalf("local hold=%+v", rows)
				}
				t.Setenv("CODEAF_HOME", home)
				input, typed := io.Pipe()
				defer input.Close()
				defer typed.Close()
				agent := &recordingSendAgent{sent: make(chan string, 2), held: kind == "held"}
				screen := &firstFrameSink{painted: make(chan struct{}, 1)}
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() {
					done <- tui3.Run(ctx, tui3.Options{Agent: agent, SessionFile: filepath.Join(c.Root, cell.TranscriptPath), Workspace: t.TempDir(), Machines: sendSurfaceSource{source}, Notice: "frame arrived before sync", Input: input, Output: screen, Width: 100, Height: 30})
				}()
				defer func() { cancel(); <-done }()
				<-screen.painted
				_, err := io.WriteString(typed, "one Enter sends\r")
				if err != nil {
					t.Fatal(err)
				}
				select {
				case got := <-agent.sent:
					if got != "one Enter sends" {
						t.Fatalf("sent=%q", got)
					}
				case <-time.After(time.Second):
					t.Fatal("first Enter left the words unsent")
				}
				if lists.Load() != 0 {
					t.Fatalf("sending asked the relay %d times", lists.Load())
				}
			}
		})
	}
}
