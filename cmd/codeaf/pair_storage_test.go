package main

import (
	"context"
	"encoding/json"
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
	"github.com/Agent-Field/codeaf/internal/pair"
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
			testPairingStorageAnswer(t, state)
		})
	}
}

func TestPairingFinishesPromptlyWhenStorageStatsStall(t *testing.T) {
	testPairingStorageAnswer(t, "stalled")
}

func TestQAPairingStorageWaitsForItsDeviceRecord(t *testing.T) {
	testPairingStorageAnswer(t, "off", true)
}

func TestQAPairingRetriesItsOptionalStorageReading(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			home := t.TempDir()
			id, err := identity.Ensure(home)
			if err != nil {
				t.Fatal(err)
			}
			var asks atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if asks.Add(1) == 1 {
					http.Error(w, "temporary refusal", status)
					return
				}
				_, _ = w.Write([]byte(`{"storage":"off"}`))
			}))
			defer srv.Close()
			samplePairedStorage(context.Background(), home, pair.Mailbox{URL: srv.URL})
			if asks.Load() != 2 || syncsetup.ReportedStorage(home, srv.URL, id.ID()) != "off" {
				t.Fatalf("temporary %d lost the pairing storage answer, requests=%d", status, asks.Load())
			}
		})
	}
}

func testPairingStorageAnswer(t *testing.T, state string, requireRegistered ...bool) {
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
		if len(requireRegistered) > 0 && requireRegistered[0] {
			_, device, err := reqsign.AuthenticateAt(time.Now)(r, nil)
			listing, listErr := dir.For(device).List(r.Context())
			_, registered := listing.Devices[device]
			if err != nil || listErr != nil || !registered {
				http.Error(w, "device is not registered", http.StatusUnauthorized)
				return
			}
		}
		if state == "stalled" {
			<-r.Context().Done()
			return
		}
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
	started := time.Now()
	if _, err := rig.approveWith(t, []string{"approve", typed}, "a\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state == "stalled" && time.Since(started) > 1900*time.Millisecond {
		t.Errorf("pairing waited %s for stalled stats, want about 1.5 seconds", time.Since(started))
	}
	for _, dir := range []string{rig.homeA, rig.homeB} {
		id, err := identity.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if state == "" {
			raw, readErr := os.ReadFile(filepath.Join(dir, syncsetup.StorageFile))
			var saved struct {
				Identity       string
				ReportsStorage *bool `json:"reports_storage"`
			}
			if readErr != nil || json.Unmarshal(raw, &saved) != nil || saved.Identity != id.ID() || saved.ReportsStorage == nil || *saved.ReportsStorage {
				t.Errorf("a successful legacy answer was not saved for this identity: %v", readErr)
			}
		} else if state == "off" || state == "on" {
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
	want := int32(2)
	if state == "unreachable" {
		want = 6
	}
	if got := asks.Load(); got != want {
		t.Errorf("storage asks = %d, want %d bounded attempts", got, want)
	}
}
