package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestPairingMakesStorageVisibleToAnotherReaderImmediately(t *testing.T) {
	for _, state := range []string{"off", "on", "", "unreachable"} {
		label := state
		if label == "" {
			label = "no storage field"
		}
		t.Run(label, func(t *testing.T) {
			celltest.Available(t)
			rig := &pairRig{homeA: t.TempDir(), homeB: t.TempDir()}
			if _, err := identity.Ensure(rig.homeA); err != nil {
				t.Fatal(err)
			}
			dir := directory.NewMemory(time.Now)
			defer dir.Close()
			links := directory.NewLinks(time.Now, directory.DefaultLinkLimits)
			dir.SetLinks(links)
			mux := http.NewServeMux()
			mux.Handle("/v1/dir/", directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return dir, nil }))
			mux.Handle(directory.LinkPath+"/", directory.LinkHandler(links, func(*http.Request) string { return "local" }))
			var asks atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/store/stats" {
					mux.ServeHTTP(w, r)
					return
				}
				asks.Add(1)
				if state == "unreachable" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if state == "" {
					_, _ = w.Write([]byte(`{}`))
				} else {
					_, _ = w.Write([]byte(`{"storage":"` + state + `"}`))
				}
			}))
			defer srv.Close()
			rig.url = srv.URL
			t.Setenv(syncsetup.URLVar, srv.URL)
			for _, dir := range []string{rig.homeA, rig.homeB} {
				if err := os.WriteFile(filepath.Join(dir, syncsetup.StorageFile), []byte("prior answer\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			screen, done := rig.asking(t)
			typed := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
			if _, err := rig.approveWith(t, []string{"approve", typed}, "a\n"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{rig.homeA, rig.homeB} {
				id, err := identity.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				if state == "off" || state == "on" {
					if got := syncsetup.ReportedStorage(dir, srv.URL, id.ID()); got != state {
						t.Errorf("separate reader got %q, want %q immediately", got, state)
					}
				} else {
					got, err := os.ReadFile(filepath.Join(dir, syncsetup.StorageFile))
					if err != nil || string(got) != "prior answer\n" {
						t.Errorf("absent or failed reading changed saved bytes: %q, %v", got, err)
					}
				}
			}
			if got := asks.Load(); got != 2 {
				t.Errorf("storage asks = %d, want one per successful side", got)
			}
		})
	}
}
