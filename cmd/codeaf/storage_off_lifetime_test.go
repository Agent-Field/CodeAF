package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func TestSharedSealWatchLivesUntilBothConversationsClose(t *testing.T) {
	for _, parentFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "parent closes first", false: "child closes first"}[parentFirst], func(t *testing.T) {
			cfg := v3Seated(storageOffNoticeConfig(t))
			d := syncDrives.renewalsOf(cfg.Place.ID())
			startupFinished(d)
			parent, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Close()
			child, err := v3Errand(cfg, cfg.Workspace, "", true)(tui3.ErrandOrders{Dir: t.TempDir(), Workspace: cfg.Workspace})
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			first, last := tui3.Agent(parent), child
			if !parentFirst {
				first, last = child, parent
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if got := storageOffWatchCount(d.notices); got != 1 {
				t.Fatalf("first close retired a shared watch: %d", got)
			}
			d.notices.say("remaining conversation still receives notices")
			found := false
			watch := cfg.Seals.(*cellstore.SealWatch)
			for line := watch.Take(); line != ""; line = watch.Take() {
				found = found || line == "remaining conversation still receives notices"
			}
			if !found {
				t.Fatal("remaining owner lost its subscription")
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if got := storageOffWatchCount(d.notices); got != 1 {
				t.Fatalf("repeated close released another owner: %d", got)
			}
			if err := last.Close(); err != nil {
				t.Fatal(err)
			}
			if got := storageOffWatchCount(d.notices); got != 0 {
				t.Fatalf("last close retained %d watches", got)
			}
		})
	}
}

func TestCancelledSurfaceDetachesItsSealListener(t *testing.T) {
	cfg := v3Seated(storageOffNoticeConfig(t))
	startupFinished(syncDrives.renewalsOf(cfg.Place.ID()))
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	watch := cfg.Seals.(*cellstore.SealWatch)
	seam := sealSeamOf(agent, cfg.Seals)
	var active, wakes atomic.Int32
	listen := seam.Listen
	seam.Listen = func(wake func()) func() {
		active.Add(1)
		stop := listen(func() { wakes.Add(1); wake() })
		return func() { stop(); active.Add(-1) }
	}
	input, typed := io.Pipe()
	defer input.Close()
	defer typed.Close()
	screen := &syncOffFrameCapture{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, SessionFile: cfg.SessionFile, Workspace: cfg.Workspace, Seal: seam, Input: input, Output: screen, Width: 120, Height: 35, Linear: true})
	}()
	waitStorageOffBoundary(t, "surface listener", func() bool { return active.Load() == 1 && screen.Plain() != "" })
	cancel()
	<-done
	if active.Load() != 0 {
		t.Fatal("cancelled surface retained its seal listener")
	}
	before := wakes.Load()
	watch.Note("the watch outlives the window")
	if wakes.Load() != before {
		t.Fatal("retained watch rang a closed window")
	}
	found := false
	for line := watch.Take(); line != ""; line = watch.Take() {
		found = found || line == "the watch outlives the window"
	}
	if !found {
		t.Fatal("surface cancellation retired the still-owned watch")
	}
}

type storageOffCountedFrames struct {
	blobstore.Store
	writes atomic.Int32
}

func (s *storageOffCountedFrames) PutFrame(ctx context.Context, b []byte) (blobstore.FrameID, error) {
	s.writes.Add(1)
	return s.Store.PutFrame(ctx, b)
}

func waitStorageOffBoundary(t *testing.T, what string, ready func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(until) {
			t.Fatal("waiting for " + what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStorageOffContinuedChatSendsAfterItsOwnEmptyHeadHoldExpires(t *testing.T) {
	cfg, c := spokenDoorChat(t)
	// A real saved-chat location lets the ordinary Home reader find the reopened workspace.
	root := filepath.Join(session.PlacesRoot(), "project", c.ID)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(c.Root, root); err != nil {
		t.Fatal(err)
	}
	var err error
	c, err = cell.OpenAt(root, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Place.Dir = root
	cfg.SessionFile = cfg.Place.Transcript()
	t.Cleanup(cellstore.StopSpawned)
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	dir := directory.NewMemory(func() time.Time { return time.UnixMilli(clock.Load()) })
	defer dir.Close()
	store := &storageOffCountedFrames{Store: blobstore.NewMemory()}
	auth := reqsign.AuthenticateAt(time.Now)
	mux := http.NewServeMux()
	mux.Handle("/v1/dir/", directory.Handler(auth, func(string) (directory.Directory, error) { return dir, nil }))
	mux.Handle("/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return store, nil }))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)
	if err := syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("open sync: %v", err)
	}
	if _, err := dir.For("other-computer").Create(t.Context(), c.ID, directory.CellInit{}); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: cfg.Workspace}); err != nil {
		t.Fatal(err)
	}
	eng := cellstore.EngineFor(cfg.Workspace)
	if _, err := eng.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := cellstore.NotePublished(c, ""); err != nil {
		t.Fatal(err)
	}
	taker := takeover{s.Continuer(eng, syncsetup.TakeOptions{RootFor: func(string) string { return c.Root }})}
	continued, err := taker.Take(t.Context(), c.ID)
	if err != nil || !continued.LocalOnly {
		t.Fatalf("continue: %+v %v", continued, err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		marker := "before-expiry"
		for _, message := range request.Messages {
			if message.Role == "user" && strings.Contains(string(message.Content), "after-expiry") {
				marker = "after-expiry"
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer-%s\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", marker)
	}))
	defer model.Close()
	cfg.BaseURL = model.URL
	cfg = v3Seated(cfg)
	drive := syncDrives.renewalsOf(c.ID)
	startupFinished(drive)
	if drive.started() != nil {
		t.Fatal("storage-off seat started an uploading writer")
	}
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	input, typed := io.Pipe()
	defer input.Close()
	defer typed.Close()
	screen := &syncOffFrameCapture{}
	t.Cleanup(func() {
		if t.Failed() {
			raw, _ := os.ReadFile(cfg.SessionFile)
			t.Logf("surface: %s\ntranscript: %s", screen.Plain(), raw)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	open := func(workspace, transcript string) (tui3.Conversation, error) {
		reopened, err := v3Reopen(cfg, transcript, workspace)
		if err != nil {
			return tui3.Conversation{}, err
		}
		reopened = v3Seated(reopened)
		next, err := session.New(reopened)
		if err == nil {
			t.Cleanup(func() { _ = next.Close() })
		}
		return tui3.Conversation{Agent: next, Workspace: reopened.Workspace, SessionFile: transcript, Resumed: true}, err
	}
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, Workspace: cfg.Workspace, SessionFile: cfg.SessionFile, Machines: surfaceMachines{s}, Takeover: taker, Open: open, Input: input, Output: screen, Width: 160, Height: 40, Linear: true})
	}()
	defer func() { cancel(); <-done }()
	waitStorageOffBoundary(t, "first frame", func() bool { return screen.Plain() != "" })
	send := func(marker string) {
		t.Helper()
		if _, err := io.WriteString(typed, marker+"\r"); err != nil {
			t.Fatal(err)
		}
		waitStorageOffBoundary(t, "locally saved "+marker, func() bool {
			if strings.Contains(screen.Plain(), "Continue this chat here?") {
				t.Fatal("own empty-head chat asked to continue again")
			}
			raw, _ := os.ReadFile(cfg.SessionFile)
			return strings.Contains(string(raw), `"role":"assistant","content":"answer-`+marker+`"`)
		})
	}
	send("before-expiry")
	previous, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add((directory.LeaseTTL + time.Second).Milliseconds())
	expired, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || expired.Cell.Head != "" || expired.Cell.Lease.Device != s.Device.ID() || expired.Cell.Lease.Expires > expired.Now {
		t.Fatalf("not this computer's free empty-head chat: %+v %v", expired, err)
	}
	send("after-expiry")
	waitStorageOffBoundary(t, "saved local turns", func() bool {
		raw, _ := os.ReadFile(cfg.SessionFile)
		head, _ := cellstore.Head(c)
		return head != nil && head.Receipt.Transcript.End >= int64(len(raw))
	})
	raw, err := os.ReadFile(cfg.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, words := range []string{"printf fixture", "answer-before-expiry", "answer-after-expiry"} {
		if !strings.Contains(string(raw), words) {
			t.Fatalf("local history lost %q", words)
		}
	}
	latest, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || latest.Cell.Lease.Fence <= previous.Cell.Lease.Fence || latest.Cell.Lease.Device != s.Device.ID() {
		t.Fatalf("send did not reacquire: %+v %v", latest, err)
	}
	if strings.Contains(screen.Plain(), "Continue this chat here?") {
		t.Fatal("own expired chat asked to continue")
	}
	if store.writes.Load() != 0 {
		t.Fatalf("store writes=%d", store.writes.Load())
	}
}
