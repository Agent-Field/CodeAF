package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQANumericPairingCachesStorageOnCLIAndTUI(t *testing.T) {
	celltest.RequireEngine(t)
	for _, mode := range []string{"CLI", "TUI"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			s, _, _ := heldChatRelay(t, "fixture", func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/store/stats" {
						_, _ = w.Write([]byte(`{"storage":"off"}`))
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			rig := &pairRig{url: s.Relay, homeA: s.Home, homeB: t.TempDir()}
			t.Setenv(home.EnvVar, rig.homeB)
			if _, err := identity.EnsureSolo(rig.homeB); err != nil {
				t.Fatal(err)
			}
			show := rig.show(t)
			code := show.code(t)
			done := make(chan error, 1)
			go func() {
				if mode == "CLI" {
					_, err := rig.joinWith(t, code, false)
					done <- err
				} else {
					_, err := (chatPairDoor{}).Join(t.Context(), code, joinScreen{newTerminal(strings.NewReader(""), io.Discard)})
					done <- err
				}
			}()
			show.out.waitFor(t, `y / n`)
			_, _ = io.WriteString(show.typed, "y\n")
			if err := show.end(t); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("numeric pairing did not finish")
			}
			joined, err := identity.Load(rig.homeB)
			if err != nil {
				t.Fatal(err)
			}
			if got := syncsetup.ReportedStorage(rig.homeB, rig.url, joined.ID()); got != "off" {
				t.Fatalf("%s join returned before saving permission: %q", mode, got)
			}
		})
	}
}
