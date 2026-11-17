package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// B4 crosses the engine/window protocol with the real seat watch and remote client.
func TestStorageNoticeReachesEngineWindow(t *testing.T) {
	for _, mode := range []string{"refused start", "revoke", "no-host", "storage on", "self-hosted"} {
		t.Run(mode, func(t *testing.T) {
			cellstoretest.EngineDir(t)
			cfg, c := spokenDoorChatWithPairing(t, true)
			oldDesk := surfaceNotices
			surfaceNotices = tui3.NewNotices()
			defer func() { surfaceNotices = oldDesk }()
			service := relayserve.New(relayserve.Config{Store: t.TempDir()})
			defer service.Close()
			var refuse atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if refuse.Load() && r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"err":"storage_off"}`))
					return
				}
				service.Handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			t.Setenv(syncsetup.URLVar, srv.URL)
			t.Setenv(syncsetup.IntervalVar, "50")
			id, err := identity.Ensure(home.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "self-hosted" {
				state := "on"
				if mode == "refused start" || mode == "no-host" {
					state = "off"
				}
				if err := syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), state); err != nil {
					t.Fatal(err)
				}
			}
			cfg = v3Seated(cfg)
			// This fixture drives its seals explicitly and must not leave a trailing timer writing during cleanup.
			cfg.Seals.(*cellstore.SealWatch).OnTail = nil
			drive := syncDrives.renewalsOf(c.ID)
			if drive == nil {
				t.Fatal("seat has no live drive")
			}
			startupFinished(drive)
			defer func() { _ = syncDrives.closeCell(c.ID) }()
			seat := cfg.Seat
			cfg.Seat = nil
			agent, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var take func() string
			if mode == "no-host" {
				defer agent.Close()
				if got := storageOffLocalNoticeCount(t, agent, cfg); got != 1 {
					t.Fatalf("local window received notice %d times", got)
				}
				return
			} else {
				loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: agent, SessionFile: cfg.SessionFile}, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer loop.Close()
				take = loop.Client.TakeNotice
			}
			if mode == "revoke" {
				if drive.started() == nil {
					t.Fatal("revoke fixture never started")
				}
				refuse.Store(true)
				if err := toolCallOn(t, seat, cfg.Place.Workspace); err != nil {
					t.Fatal(err)
				}
				drive.Idle()
			}
			count := 0
			deadline := time.After(3 * time.Second)
			quiet := time.NewTimer(350 * time.Millisecond)
			defer quiet.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			want := mode != "storage on" && mode != "self-hosted"
		waiting:
			for {
				for line := take(); line != ""; line = take() {
					if line == chatlist.RelayStorageOff {
						count++
					}
				}
				select {
				case <-tick.C:
				case <-quiet.C:
					if !want || count > 0 {
						break waiting
					}
				case <-deadline:
					break waiting
				}
			}
			if want && count != 1 || !want && count != 0 {
				t.Fatalf("window received storage-off %d times", count)
			}
		})
	}
}

// storageOffLocalNoticeCount gives both in-process notice roads to the real surface.
func storageOffLocalNoticeCount(t *testing.T, agent *session.Agent, cfg session.Config) int {
	t.Helper()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	screen := &syncOffFrameCapture{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, SessionFile: cfg.SessionFile, Workspace: cfg.Place.Workspace, Seal: sealSeamOf(agent, cfg.Seals), Notices: surfaceNotices, Input: input, Output: screen, Width: 200, Height: 40, Linear: true})
	}()
	defer func() { cancel(); <-done }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for {
		shown := strings.Join(strings.Fields(screen.Plain()), " ")
		if strings.Contains(shown, chatlist.RelayStorageOff) {
			return strings.Count(shown, chatlist.RelayStorageOff)
		}
		select {
		case <-ticks.C:
		case <-deadline.C:
			t.Fatalf("local window missed storage notice: %s", shown)
			return 0
		}
	}
}

type storageOffNoticeAgent struct {
	quietAgent
	watch *cellstore.SealWatch
}

func (a *storageOffNoticeAgent) TakeSealNotice() string { return a.watch.Take() }

// B4 gives each seat sharing a drive one notice over its own real window connection.
func TestSharedDriveNoticesEachWindow(t *testing.T) {
	cfg, c := spokenDoorChatWithPairing(t, true)
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer service.Close()
	srv := httptest.NewServer(service.Handler)
	defer srv.Close()
	defer syncDrives.closeAll()
	t.Setenv(syncsetup.URLVar, srv.URL)
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(home.Dir(), syncsetup.Resolve(home.Dir()).URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	cfg = v3Seated(cfg)
	d := syncDrives.renewalsOf(c.ID)
	startupFinished(d)
	_, _, second := v3SeatWith(cfg.Place, nil, syncDrives)
	if syncDrives.renewalsOf(c.ID) != d {
		t.Fatal("seats did not share a drive")
	}
	for _, watch := range []*cellstore.SealWatch{cfg.Seals.(*cellstore.SealWatch), second} {
		loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: &storageOffNoticeAgent{watch: watch}}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer loop.Close()
			count := 0
			ticks := time.NewTicker(10 * time.Millisecond)
			defer ticks.Stop()
			deadline := time.NewTimer(350 * time.Millisecond)
			defer deadline.Stop()
			for {
				for line := loop.Client.TakeNotice(); line != ""; line = loop.Client.TakeNotice() {
					if line == chatlist.RelayStorageOff {
						count++
					}
				}
				select {
				case <-ticks.C:
				case <-deadline.C:
					if count != 1 {
						t.Errorf("shared-drive window received %d notices", count)
					}
					return
				}
			}
		}()
	}

	// Approval must not replay the old refusal to a newly opened seat.
	if err := syncsetup.SaveStorage(home.Dir(), syncsetup.Resolve(home.Dir()).URL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	startupFinished(d)
	_, _, approved := v3SeatWith(cfg.Place, nil, syncDrives)
	for line := approved.Take(); line != ""; line = approved.Take() {
		if line == chatlist.RelayStorageOff {
			t.Fatal("approved seat replayed an obsolete refusal")
		}
	}
}
