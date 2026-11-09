package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestSharedLocalToolsShareOneConcurrentDirectoryReading(t *testing.T) {
	for _, liveUpdates := range []bool{false, true} {
		t.Run(map[bool]string{false: "without live updates", true: "with live updates"}[liveUpdates], func(t *testing.T) {
			cfg, c := spokenDoorChat(t)
			t.Cleanup(cellstore.StopSpawned)
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
				time.Sleep(2100 * time.Millisecond)
				for range 16 {
					if err := d.Gate(); err != nil {
						t.Fatal(err)
					}
				}
				if got := reads.Load(); got != 1 {
					t.Fatalf("unchanged live feed caused %d directory readings, want 1", got)
				}
			}
		})
	}
}
