package main

import (
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestSharedLocalToolsShareOneConcurrentDirectoryReading(t *testing.T) {
	for _, liveUpdates := range []bool{false, true} {
		t.Run(map[bool]string{false: "without live updates", true: "with live updates"}[liveUpdates], func(t *testing.T) {
			cellstoretest.EngineDir(t)
			cfg, c := spokenDoorChatWithPairing(t, true)
			id, err := identity.Ensure(home.Dir())
			if err != nil {
				t.Fatal(err)
			}
			dev, err := identity.Device(home.Dir())
			if err != nil {
				t.Fatal(err)
			}
			dir := directory.NewMemory(time.Now)
			defer dir.Close()
			if _, err := dir.For(dev.ID()).Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
				t.Fatal(err)
			}
			if err := cellstore.NotePublished(c, ""); err != nil {
				t.Fatal(err)
			}
			var reads atomic.Int32
			handler := directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return dir, nil })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !liveUpdates && strings.HasSuffix(r.URL.Path, "/watch") {
					http.NotFound(w, r)
					return
				}
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+c.ID) {
					reads.Add(1)
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			t.Setenv(syncsetup.URLVar, server.URL)
			if err := syncsetup.SaveStorage(home.Dir(), server.URL, id.ID(), "off"); err != nil {
				t.Fatal(err)
			}
			cfg = v3Seated(cfg)
			d := syncDrives.renewalsOf(c.ID)
			startupFinished(d)
			if liveUpdates {
				s, ok, err := syncsetup.Open(home.Dir())
				if err != nil || !ok {
					t.Fatalf("open sync: %v", err)
				}
				feed := s.Follow()
				defer feed.Close()
				waitStorageOffBoundary(t, "shared directory feed", func() bool { return feed.State().Up })
			}
			var group sync.WaitGroup
			for range 16 {
				group.Add(1)
				go func() {
					defer group.Done()
					if err := d.Gate(); err != nil {
						t.Error(err)
					}
				}()
			}
			group.Wait()
			if got := reads.Load(); got != 1 {
				t.Fatalf("16 simultaneous gates read the directory %d times, want 1", got)
			}
			if liveUpdates {
				time.Sleep(14 * time.Second)
				for range 16 {
					if err := d.Gate(); err != nil {
						t.Fatal(err)
					}
				}
				if got := reads.Load(); got != 1 {
					t.Fatalf("unchanged live feed caused %d directory readings before 15 seconds, want 1", got)
				}
				time.Sleep(1100 * time.Millisecond)
				if err := d.Gate(); err != nil {
					t.Fatal(err)
				}
				if got := reads.Load(); got != 2 {
					t.Fatalf("live feed made %d readings after 15 seconds, want 2", got)
				}
			}
		})
	}
}

func TestSharedLocalToolsReuseASlowFailedReading(t *testing.T) {
	cellstoretest.EngineDir(t)
	cfg, c := spokenDoorChatWithPairing(t, true)
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cellstore.NotePublished(c, ""); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/"+c.ID) {
			reads.Add(1)
			select {
			case <-time.After(2100 * time.Millisecond):
			case <-r.Context().Done():
			}
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	if err := syncsetup.SaveStorage(home.Dir(), server.URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	cfg = v3Seated(cfg)
	d := syncDrives.renewalsOf(c.ID)
	startupFinished(d)
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			if err := d.Gate(); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	group.Wait()
	if got := reads.Load(); got != 1 {
		t.Fatalf("16 queued gates made %d slow failed readings, want 1", got)
	}
	time.Sleep(2100 * time.Millisecond)
	if err := d.Gate(); err != nil {
		t.Fatal(err)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("expired failure made %d readings, want 2", got)
	}
}

func TestSharedLocalToolsExpireOwnershipWhilePongsKeepArriving(t *testing.T) {
	cellstoretest.EngineDir(t)
	cfg, c := spokenDoorChatWithPairing(t, true)
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	dir := directory.NewMemory(time.Now)
	defer dir.Close()
	if _, err := dir.For(dev.ID()).Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
		t.Fatal(err)
	}
	if err := cellstore.NotePublished(c, ""); err != nil {
		t.Fatal(err)
	}
	var pongs, reads atomic.Int32
	handler := directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return dir, nil })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/watch") {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"v":1}`)); err != nil {
				return
			}
			for {
				_, data, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				if string(data) == "ping" {
					if err := conn.Write(r.Context(), websocket.MessageText, []byte("pong")); err != nil {
						return
					}
					pongs.Add(1)
				}
			}
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+c.ID) {
			reads.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	if err := syncsetup.SaveStorage(home.Dir(), server.URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	cfg = v3Seated(cfg)
	d := syncDrives.renewalsOf(c.ID)
	startupFinished(d)
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("sync: %v", err)
	}
	feed := s.Follow()
	defer feed.Close()
	waitStorageOffBoundary(t, "live feed", func() bool { return feed.State().Up })
	if err := d.Gate(); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.For("other-device").Acquire(t.Context(), c.ID, directory.AcquireOpts{Force: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15100 * time.Millisecond)
	if pongs.Load() == 0 || !feed.State().Up || feed.State().Version != 1 {
		t.Fatalf("fixture lost live unchanged feed: %+v, pongs %d", feed.State(), pongs.Load())
	}
	if err := d.Gate(); err == nil {
		t.Error("tool allowed after 15 seconds on a stale live feed")
	}
	if got := reads.Load(); got != 2 {
		t.Error(fmt.Sprintf("ownership readings = %d, want 2", got))
	}
}
