package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// publicationChat uses the repository's scripted model endpoint. Like the
// engine fixture, it is optional on machines without the staged test program.
func publicationChat(t *testing.T) (session.Config, cell.Cell) {
	t.Helper()
	stub := os.Getenv("CODEAF_TEST_SCRIPTED_MODEL_URL")
	if stub == "" {
		t.Skip("stage test/remote/stub and set CODEAF_TEST_SCRIPTED_MODEL_URL")
	}
	cfg, _ := doorChat(t)
	c, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(c.Root, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.Place = session.Place{Dir: c.Root, Workspace: work, Owned: true}
	cfg.Workspace, cfg.SessionFile = work, cfg.Place.Transcript()
	cfg.Model, cfg.APIKey, cfg.BaseURL = "stub/scripted", "stub", stub
	return cfg, c
}

func publicationRelay(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	for port := 8900; port <= 8999; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		srv := httptest.NewUnstartedServer(handler)
		srv.Listener.Close()
		srv.Listener = listener
		srv.Start()
		t.Cleanup(srv.Close)
		return srv
	}
	t.Fatal("no local relay port available in 8900-8999")
	return nil
}

func publicationPair(t *testing.T, relay string) *syncsetup.Sync {
	t.Helper()
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	bHome := t.TempDir()
	if err := identity.Save(bHome, id); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, relay)
	t.Setenv(syncsetup.IntervalVar, "50")
	b, ok, err := syncsetup.Open(bHome)
	if err != nil || !ok {
		t.Fatalf("open paired computer: %v, %v", ok, err)
	}
	return b
}

func capturePublicationOutput(t *testing.T) (stdout, stderr *os.File) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	var err error
	stdout, err = os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stderr, err = os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdout, stderr
	t.Cleanup(func() { os.Stdout, os.Stderr = oldOut, oldErr; stdout.Close(); stderr.Close() })
	return
}

// C1: the real one-message road waits through first registration and releases
// its hold before the other computer lists the chat, without cleanup helping it.
func TestChatOnceWaitsForFirstRegistration(t *testing.T) {
	const wantReply = "STUB-REPLY: PROBE-ECHO publish\n"
	cfg, c := publicationChat(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	next := relayserveForMove(t)
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/dir/cells/"+c.ID {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		next.ServeHTTP(w, r)
	}))
	b := publicationPair(t, srv.URL)
	stdout, _ := capturePublicationOutput(t)
	done := make(chan error, 1)
	go func() {
		done <- runChatV3Once(t.Context(), cfg, cfg.Workspace, "PROBE-ECHO publish", "", false)
		close(done)
	}()
	t.Cleanup(func() {
		// A failing test must cancel its writer rather than publish during
		// cleanup and accidentally make the missing registration disappear.
		if t.Failed() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			for _, d := range syncDrives.take() {
				_ = d.Close(ctx)
			}
		}
		unblock()
		select {
		case <-done:
		case <-time.After(driveCloseWithin):
			t.Error("one-message test did not stop")
		}
	})
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("returned before registration: %v", err)
	case <-time.After(cellstore.SealWithin):
		t.Fatal("no registration request")
	}
	// The answer itself must be visible before testing the closing wait.
	// A slow model must not make a missing final publish look correct.
	answeredBy := time.After(cellstore.SealWithin)
	for {
		out, err := os.ReadFile(stdout.Name())
		if err != nil {
			t.Fatal(err)
		}
		if string(out) == wantReply {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("returned before registration finished: %v", err)
		case <-answeredBy:
			t.Fatal("the one-message answer did not arrive")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case err := <-done:
		t.Fatalf("returned while registration was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(driveCloseWithin):
		t.Fatal("one-message close did not finish")
	}
	listing, err := b.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec, exists := listing.Cells[c.ID]
	if !exists || rec.Head == "" || rec.Lease.Expires > listing.Now {
		t.Fatalf("other computer cannot take completed chat: %+v", listing)
	}
	out, err := os.ReadFile(stdout.Name())
	if err != nil || string(out) != wantReply {
		t.Fatalf("reply = %q: %v", out, err)
	}
}

// C2/C5: a never-paired one-message chat and doctor retain local behavior,
// produce no paired publication records, and make no relay requests.
func TestChatOnceNeverPairedRemainsLocal(t *testing.T) {
	cfg, _ := publicationChat(t)
	var requests atomic.Int32
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Setenv(syncsetup.URLVar, srv.URL)
	stdout, stderr := capturePublicationOutput(t)
	if err := runChatV3Once(t.Context(), cfg, cfg.Workspace, "PROBE-ECHO local", "", false); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(stdout.Name())
	if string(out) != "STUB-REPLY: PROBE-ECHO local\n" {
		t.Fatalf("reply = %q", out)
	}
	errout, _ := os.ReadFile(stderr.Name())
	if strings.Contains(string(errout), "other computers") {
		t.Fatalf("unexpected paired warning: %s", errout)
	}
	issues, fixes := inspectDoctorSync(true)
	if len(issues) != 0 || len(fixes) != 0 {
		t.Fatalf("doctor: %v, %v", issues, fixes)
	}
	if requests.Load() != 0 {
		t.Fatalf("relay requests = %d", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(home.Dir(), "v3", "sync", "publication")); !os.IsNotExist(err) {
		t.Fatalf("unpaired publication record: %v", err)
	}
}

// C1/C3/C4: an upload whose first registration failed is reported and repaired
// by doctor through the normal publisher; unavailable listings never prove absence.
func TestDoctorFixRegistersUnlistedChat(t *testing.T) {
	cfg, c := publicationChat(t)
	var dropCreate, dropList atomic.Bool
	dropCreate.Store(true)
	var uploads, failedListings atomic.Int32
	next := relayserveForMove(t)
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
			uploads.Add(1)
		}
		if dropList.Load() && r.Method == http.MethodGet && r.URL.Path == "/v1/dir/list" {
			failedListings.Add(1)
		}
		if (dropCreate.Load() && r.Method == http.MethodPost && r.URL.Path == "/v1/dir/cells/"+c.ID) || (dropList.Load() && r.Method == http.MethodGet && r.URL.Path == "/v1/dir/list") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	}))
	b := publicationPair(t, srv.URL)
	_, stderr := capturePublicationOutput(t)
	if err := runChatV3Once(t.Context(), cfg, cfg.Workspace, "PROBE-ECHO stranded", "", false); err != nil {
		t.Fatalf("publish failure changed turn status: %v", err)
	}
	if uploads.Load() == 0 {
		t.Fatal("fixture never uploaded")
	}
	errout, _ := os.ReadFile(stderr.Name())
	if !strings.Contains(string(errout), "this chat could not finish syncing with your other computers; run codeaf doctor --fix or reopen this chat to try again.") {
		t.Fatalf("missing final publish warning: %s", errout)
	}
	missing := "chat has not reached your other computers · chat " + c.ID
	// Another sealed local chat has no paired intent of its own. The first
	// chat's upload ledger cannot make this pre-pairing history a repair target.
	local, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	localWork := filepath.Join(local.Root, "work")
	if err := os.MkdirAll(localWork, 0o700); err != nil {
		t.Fatal(err)
	}
	localCfg := cfg
	localCfg.Place = session.Place{Dir: local.Root, Workspace: localWork, Owned: true}
	localCfg.Workspace, localCfg.SessionFile = localWork, localCfg.Place.Transcript()
	localAgent, err := session.New(localCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer localAgent.Close()
	events, err := localAgent.SubmitBash(t.Context(), "! printf local-only")
	if err != nil {
		localAgent.Close()
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatal(event.Text)
		}
	}
	if err := localAgent.Close(); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(localWork)
	engine.Workspace = localWork
	if _, err := engine.Seal(t.Context(), local, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := cellstore.Await(t.Context(), local); err != nil {
		t.Fatal(err)
	}
	issues, fixes := inspectDoctorSync(false)
	if !slices.Contains(issues, missing) || len(fixes) != 0 {
		t.Fatalf("doctor: %v, %v", issues, fixes)
	}
	if strings.Contains(strings.Join(issues, "\n"), local.ID) {
		t.Fatalf("unrelated upload made local chat a repair target: %v", issues)
	}
	dropList.Store(true)
	issues, fixes = inspectDoctorSync(true)
	if failedListings.Load() == 0 {
		t.Fatal("fixture did not fail the listing")
	}
	if slices.Contains(issues, missing) || len(fixes) != 0 || !strings.Contains(strings.Join(issues, "\n"), "could not check whether chats reached your other computers") {
		t.Fatalf("failed listing claimed absence: %v, %v", issues, fixes)
	}
	dropList.Store(false)
	// Local archives and branch copies cannot become missing-chat repairs.
	if err := session.SetArchived(c.Root, true); err != nil {
		t.Fatal(err)
	}
	issues, fixes = inspectDoctorSync(true)
	if slices.Contains(issues, missing) || len(fixes) != 0 {
		t.Fatalf("archived chat was offered: %v, %v", issues, fixes)
	}
	if err := session.SetArchived(c.Root, false); err != nil {
		t.Fatal(err)
	}
	branches, err := cellsync.OpenBranchMap(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := branches.Set(c.ID, "01J00000000000000000000999"); err != nil {
		t.Fatal(err)
	}
	issues, fixes = inspectDoctorSync(true)
	if slices.Contains(issues, missing) || len(fixes) != 0 {
		t.Fatalf("branch copy was offered: %v, %v", issues, fixes)
	}
	if err := branches.Set(c.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	dropCreate.Store(false)
	issues, fixes = inspectDoctorSync(true)
	wantFix := "fixed: sent chat " + c.ID + " to your other computers"
	if !slices.Contains(fixes, wantFix) || slices.Contains(issues, missing) {
		t.Fatalf("repair: %v, %v", issues, fixes)
	}
	listing, err := b.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok := listing.Cells[c.ID]; !ok || rec.Head == "" || rec.Lease.Expires > listing.Now {
		t.Fatalf("repair not visible or still held: %+v", listing)
	}
	issues, fixes = inspectDoctorSync(true)
	if len(fixes) != 0 || slices.Contains(issues, missing) {
		t.Fatalf("second repair changed state: %v, %v", issues, fixes)
	}
	// An existing hold and unfamiliar history are never acquired by this repair.
	view, err := b.Dir.Acquire(t.Context(), c.ID, directory.AcquireOpts{})
	if err != nil {
		t.Fatal(err)
	}
	view, err = b.Dir.Publish(t.Context(), c.ID, directory.Publish{Fence: view.Cell.Lease.Fence, OldHead: view.Cell.Head, Head: strings.Repeat("f", 64), Class: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := syncsetup.Open(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := session.LoadMeta(c.Root)
	if err := repairDoctorPublication(s, c, meta); err == nil {
		t.Fatal("repair accepted another computer's record")
	}
	after, err := b.Dir.Cell(t.Context(), c.ID)
	if err != nil || after.Cell.Lease != view.Cell.Lease || after.Cell.Head != view.Cell.Head {
		t.Fatalf("repair changed live hold: %+v, %v", after, err)
	}
	if err := b.Dir.Archive(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	issues, fixes = inspectDoctorSync(true)
	if len(fixes) != 0 || slices.Contains(issues, missing) {
		t.Fatalf("remote archive was offered: %v, %v", issues, fixes)
	}
}
