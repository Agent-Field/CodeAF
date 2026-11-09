package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func TestFinalPublishDisplacementReachesTheRestoredWindowOnce(t *testing.T) {
	for _, road := range []string{"engine", "no-host"} {
		for _, displaced := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/displaced=%t", road, displaced), func(t *testing.T) {
				t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
				cfg, c := spokenDoorChat(t)
				t.Cleanup(cellstore.StopSpawned)
				t.Cleanup(stopFinalSaveProgress)
				id, err := identity.Ensure(home.Dir())
				if err != nil {
					t.Fatal(err)
				}
				dir := directory.NewMemory(time.Now)
				defer dir.Close()
				key := directory.MetadataKey(id.CellKey())
				name, err := directory.SealName(key, "desk")
				if err != nil {
					t.Fatal(err)
				}
				if err := dir.For("other-device").PutDevice(t.Context(), "other-device", directory.Device{V: 1, Name: name}); err != nil {
					t.Fatal(err)
				}
				auth := reqsign.AuthenticateAt(time.Now)
				mux := http.NewServeMux()
				mux.Handle("/v1/dir/", directory.Handler(auth, func(string) (directory.Directory, error) { return dir, nil }))
				frames := blobstore.NewMemory()
				mux.Handle("/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return frames, nil }))
				var closing, discovered atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if displaced && closing.Load() && strings.HasSuffix(r.URL.Path, "/publish") && discovered.CompareAndSwap(false, true) {
						if _, err := dir.For("other-device").Acquire(r.Context(), c.ID, directory.AcquireOpts{Force: true}); err != nil {
							t.Error(err)
						}
					}
					mux.ServeHTTP(w, r)
				}))
				defer server.Close()
				t.Setenv(syncsetup.URLVar, server.URL)
				t.Setenv(syncsetup.IntervalVar, "3600000")
				if err := syncsetup.SaveStorage(home.Dir(), server.URL, id.ID(), "on"); err != nil {
					t.Fatal(err)
				}
				cfg = v3Seated(cfg)
				watch := cfg.Seals.(*cellstore.SealWatch)
				watch.OnTail, watch.OnTurnEnd = nil, nil
				d := syncDrives.renewalsOf(c.ID)
				startupFinished(d)
				if d.started() == nil {
					t.Fatal("writer did not start")
				}
				if _, err := d.started().Store(cellstore.EngineFor(cfg.Workspace)).Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
					t.Fatal(err)
				}
				d.Idle()
				waitStorageOffBoundary(t, "initial publish", func() bool {
					view, err := dir.For("observer").Cell(t.Context(), c.ID)
					return err == nil && view.Cell.Head != ""
				})
				agent, err := session.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer agent.Close()
				events, err := agent.SubmitBash(t.Context(), "! printf final-work > final-work")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == session.EventError {
						t.Fatal(event.Text)
					}
				}
				var window tui3.Agent = agent
				if road == "engine" {
					loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
						return &remote.Engine{Agent: agent, SessionFile: cfg.SessionFile, Workspace: cfg.Workspace, FinishSaving: func(remote.WrappedAgent) error { return syncDrives.closeCell(c.ID) }}, nil
					}})
					if err != nil {
						t.Fatal(err)
					}
					defer loop.Close()
					window = loop.Client.Agent()
				}
				input, typed := io.Pipe()
				defer input.Close()
				defer typed.Close()
				screen := &surfaceFrameWriter{ready: make(chan struct{})}
				previousRun := runSurfaceProgram
				runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
					options.Input, options.Output = input, screen
					options.UpdateCheck = nil
					return tui3.Run(ctx, options)
				}
				defer func() { runSurfaceProgram = previousRun }()
				ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- runSurface(ctx, tui3.Options{Agent: window, SessionFile: cfg.SessionFile, Workspace: cfg.Workspace, ProfileDir: home.Dir(), Seal: sealSeamOf(window, cfg.Seals), Input: input, Output: screen, Width: 160, Height: 35, Linear: true})
				}()
				select {
				case <-screen.ready:
				case err := <-done:
					t.Fatalf("window closed before first frame: %v", err)
				case <-ctx.Done():
					t.Fatal("window never painted")
				}
				closing.Store(true)
				if _, err := io.WriteString(typed, "/quit\r"); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if displaced && !discovered.Load() {
					t.Fatal("final publish did not encounter displacement")
				}
				oldOut, oldFlush := os.Stdout, flushFinalSaves
				out, err := os.CreateTemp(t.TempDir(), "receipt")
				if err != nil {
					t.Fatal(err)
				}
				defer out.Close()
				os.Stdout = out
				flushFinalSaves = func(context.Context) error { return nil }
				defer func() { os.Stdout, flushFinalSaves = oldOut, oldFlush }()
				_, _ = io.WriteString(out, "screen restored\n")
				_ = finishFinalSaving(t.Context())
				_ = finishFinalSaving(t.Context())
				raw, err := os.ReadFile(out.Name())
				if err != nil {
					t.Fatal(err)
				}
				count := strings.Count(string(raw), chatlist.Superseded("desk"))
				if displaced && count != 1 || !displaced && count != 0 {
					t.Fatalf("restored receipt: %q", raw)
				}
				if !displaced && string(raw) != "screen restored\n" {
					t.Fatalf("clean final save added a sentence: %q", raw)
				}
			})
		}
	}
}
