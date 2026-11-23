package main

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageOptInStatsWakesOpenChatAndRevokesLocally(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	var state atomic.Bool
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/store/stats" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		word := "off"
		if state.Load() {
			word = "on"
		}
		_, _ = w.Write([]byte(`{"storage":"` + word + `"}`))
	}))
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)
	if err := syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	if _, err := startDrive(cell.Cell{ID: "open"}, cellstore.Engine{}, func(error) {}); !errors.Is(err, wireauth.ErrStorageOff) || requests.Load() != 0 {
		t.Fatalf("off startup: %v requests=%d", err, requests.Load())
	}
	old := syncDrives
	var starts atomic.Int32
	l := &liveDrive{now: time.Now, changes: make(chan struct{}, 1), after: func(time.Duration, func()) func() bool { return func() bool { return true } }, start: func() (*syncsetup.Drive, error) {
		starts.Add(1)
		if syncsetup.StorageForHome(home.Dir()) != "on" {
			return nil, wireauth.ErrStorageOff
		}
		return nil, nil
	}}
	syncDrives = &driveBook{drive: map[string]*liveDrive{"open": l}}
	t.Cleanup(func() { syncDrives = old })
	l.try()
	startupFinished(l)
	if starts.Load() != 1 {
		t.Fatal("local wait did not start")
	}
	state.Store(true)
	counts, err := chatSyncStats(t.Context())
	if err != nil || counts.Storage != "on" {
		t.Fatalf("approval refresh: %+v %v", counts, err)
	}
	startupFinished(l)
	if starts.Load() != 2 || syncsetup.StorageForHome(home.Dir()) != "on" {
		t.Fatal("approval did not wake open chat")
	}
	state.Store(false)
	_ = syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), "off")
	if _, err := chatSyncStats(t.Context()); err != nil {
		t.Fatal(err)
	}
	if syncsetup.StorageForHome(home.Dir()) != "off" || teamPaired() {
		t.Fatal("revocation did not stop uploads")
	}
}

func TestStorageOptInSettingsOnlyPairedHosted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(syncsetup.URLVar, syncsetup.ProductionRelayURL)
	id, err := identity.EnsureSolo(dir)
	if err != nil {
		t.Fatal(err)
	}
	door := newApprovalsDoor(dir)
	if code, _ := door.RelayStorage(); code != "" {
		t.Fatal("unpaired row present")
	}
	if err = identity.EndSolo(dir); err != nil {
		t.Fatal(err)
	}
	if code, state := door.RelayStorage(); code != id.ID() || state != "off" {
		t.Fatalf("hosted row: %s %s", code, state)
	}
	if err = syncsetup.SaveStorage(dir, syncsetup.ProductionRelayURL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	if _, state := door.RelayStorage(); state != "on" {
		t.Fatal("approved row not on")
	}
	portHome := t.TempDir()
	portID, err := identity.Ensure(portHome)
	if err != nil {
		t.Fatal(err)
	}
	portDoor := newApprovalsDoor(portHome)
	for _, raw := range []string{syncsetup.ProductionRelayURL, syncsetup.StagingRelayURL, syncsetup.DevRelayURL} {
		u, _ := url.Parse(raw)
		u.Host = strings.ToUpper(u.Host) + ":443"
		t.Setenv(syncsetup.URLVar, u.String())
		if code, state := portDoor.RelayStorage(); code != portID.ID() || state != "off" {
			t.Fatalf("default-port row: %s %s %s", u, code, state)
		}
	}

	for _, relay := range []string{"http://own.test", "off"} {
		t.Setenv(syncsetup.URLVar, relay)
		if code, _ := door.RelayStorage(); code != "" {
			t.Fatalf("inapplicable row present: %s", relay)
		}
	}
}

func TestStorageOptInSettingsRelayAnswerDecides(t *testing.T) {
	celltest.Available(t)
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	id, err := identity.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var absent atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		answer := `{"storage":"off"}`
		if absent.Load() {
			answer = `{"objects":0}`
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)
	door := newApprovalsDoor(dir)
	if code, _ := door.RelayStorage(); code != "" {
		t.Fatal("unknown custom relay has row")
	}
	if _, err := chatSyncStats(t.Context()); err != nil {
		t.Fatal(err)
	}
	if code, state := door.RelayStorage(); code != id.ID() || state != "off" {
		t.Fatalf("reported row: %s %s", code, state)
	}
	absent.Store(true)
	if _, err := chatSyncStats(t.Context()); err != nil {
		t.Fatal(err)
	}
	if code, _ := door.RelayStorage(); code != "" {
		t.Fatal("no-field answer retained row")
	}
	for range 3 {
		if _, err := chatSyncStats(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("no-field requests: %d", requests.Load())
	}
}

func TestStorageOptInQuitReceiptAfterRevocation(t *testing.T) {
	armFinalSaving()
	retainLocalDurability(func() bool { return true })
	before := os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "receipt")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Stdout = out
	t.Cleanup(func() { os.Stdout = before })
	recordFinalSaveFailure(syncwords.UploadFailure(context.DeadlineExceeded))
	recordFinalSaveFailure(syncwords.UploadFailure(wireauth.ErrStorageOff))
	if !finishFinalSaving(context.Background()) {
		t.Fatal("local save changed exit status")
	}
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "saved here; relay storage is off for this account, so it stays on this computer" {
		t.Fatalf("receipt: %s", raw)
	}
}
