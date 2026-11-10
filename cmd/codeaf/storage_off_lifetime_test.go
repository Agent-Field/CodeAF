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
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
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

func storageOffSavedChat(t *testing.T) (session.Config, cell.Cell) {
	t.Helper()
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
	return cfg, c
}

func TestStorageOffContinuedChatSendsAfterItsOwnEmptyHeadHoldExpires(t *testing.T) {
	cellstoretest.EngineDir(t)
	cfg, c := storageOffSavedChat(t)
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
	taker := takeover{c: s.Continuer(eng, syncsetup.TakeOptions{RootFor: func(string) string { return c.Root }})}
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

func TestStorageOffChatKeepsSendingAfterItsOwnRecordedHeadHoldExpires(t *testing.T) {
	testRecordedHeadReturn(t, "local turns ahead")
}

func TestStorageOffChatWithMissingSavedTurnsDoesNotReturnQuietly(t *testing.T) {
	testRecordedHeadReturn(t, "missing saved turns")
}

func TestStorageOnChatWithDifferentHeadsStillAsksToContinue(t *testing.T) {
	testRecordedHeadReturn(t, "storage on")
}

func testRecordedHeadReturn(t *testing.T, mode string) {
	t.Helper()
	cellstoretest.EngineDir(t)
	cfg, c := storageOffSavedChat(t)
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
	permission := func(state string) {
		t.Helper()
		if err := syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), state); err != nil {
			t.Fatal(err)
		}
	}
	permission("on")
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("open sync: %v", err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: cfg.Workspace}); err != nil {
		t.Fatal(err)
	}
	eng := cellstore.EngineFor(cfg.Workspace)
	if _, err := eng.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	approved := v3Seated(cfg)
	startupFinished(syncDrives.renewalsOf(c.ID))
	syncDrives.renewalsOf(c.ID).Idle()
	syncDrives.closeAll()
	approved.Seals.(*cellstore.SealWatch).Retire()
	published, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || published.Cell.Head == "" || store.writes.Load() == 0 {
		t.Fatalf("approved chat never published: %+v, writes=%d, %v", published, store.writes.Load(), err)
	}
	permission("off")
	writesAtRevoke := store.writes.Load()
	taker := takeover{c: s.Continuer(eng, syncsetup.TakeOptions{RootFor: func(string) string { return c.Root }})}
	continued, err := taker.Take(t.Context(), c.ID)
	if err != nil || !continued.LocalOnly {
		t.Fatalf("continue after revoke: %+v %v", continued, err)
	}
	var turns atomic.Int32
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
		marker := ""
		for _, message := range request.Messages {
			if message.Role == "user" {
				for _, candidate := range []string{"local-one", "local-two", "before-expiry", "after-first-expiry", "after-second-expiry"} {
					if strings.Contains(string(message.Content), candidate) {
						marker = candidate
					}
				}
			}
		}
		turns.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer-%s\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", marker)
	}))
	defer model.Close()
	cfg.BaseURL = model.URL
	localCfg := v3Seated(cfg)
	startupFinished(syncDrives.renewalsOf(c.ID))
	local, err := session.New(localCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	for _, words := range []string{"local-one", "local-two"} {
		events, err := local.Submit(t.Context(), words)
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == session.EventError {
				t.Fatal(event.Text)
			}
		}
	}
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	syncDrives.closeAll()
	reopened, err := v3Reopen(cfg, cfg.SessionFile, cfg.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	cfg = v3Seated(reopened)
	startupFinished(syncDrives.renewalsOf(c.ID))
	if syncDrives.renewalsOf(c.ID).started() != nil {
		t.Fatal("storage-off chat started an uploading writer")
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
	var opens atomic.Int32
	open := func(workspace, transcript string) (tui3.Conversation, error) {
		opens.Add(1)
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
	t.Cleanup(func() {
		if t.Failed() {
			raw, _ := os.ReadFile(cfg.SessionFile)
			t.Logf("surface: %s\ntranscript: %s", screen.Plain(), raw)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	seam := sealSeamOf(agent, cfg.Seals)
	var storageNotices atomic.Int32
	notice := seam.Notice
	seam.Notice = func() string {
		line := notice()
		if line == chatlist.RelayStorageOff {
			storageNotices.Add(1)
		}
		return line
	}
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, Workspace: cfg.Workspace, SessionFile: cfg.SessionFile, Seal: seam, Machines: surfaceMachines{s}, Takeover: taker, Open: open, Input: input, Output: screen, Width: 200, Height: 40, Linear: true})
	}()
	defer func() { cancel(); <-done }()
	shown := func() string { return strings.Join(strings.Fields(screen.Plain()), " ") }
	waitStorageOffBoundary(t, "storage-off sentence", func() bool { return strings.Contains(shown(), chatlist.RelayStorageOff) })
	send := func(marker string) {
		t.Helper()
		if _, err := io.WriteString(typed, marker+"\r"); err != nil {
			t.Fatal(err)
		}
		waitStorageOffBoundary(t, "saved "+marker, func() bool {
			if strings.Contains(shown(), "Continue this chat here?") {
				t.Fatal("own chat asked to continue again")
			}
			raw, _ := os.ReadFile(cfg.SessionFile)
			head, _ := cellstore.Head(c)
			return strings.Contains(string(raw), `"role":"assistant","content":"answer-`+marker+`"`) && head != nil && head.Receipt.Transcript.End >= int64(len(raw))
		})
	}
	send("before-expiry")
	before, err := os.ReadFile(cfg.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"after-first-expiry", "after-second-expiry"} {
		clock.Add((directory.LeaseTTL + time.Second).Milliseconds())
		if mode == "missing saved turns" {
			// Another computer publishes a real child of the shared turn, without receiving our local turns.
			other := cell.Cell{ID: c.ID, Root: filepath.Join(t.TempDir(), c.ID)}
			otherEngine := eng
			otherEngine.DataRoot = t.TempDir()
			synced := otherEngine.Sync(cellstore.SyncKeys{CellKey: id.CellKey(), Dedup: id.DedupSecret()}, "fedcba9876543210")
			fetch := cellsync.Fetcher{Engine: synced, Store: store.Store, Inbox: synced.Inbox}
			if err := fetch.Fetch(t.Context(), other, published.Cell.Head); err != nil {
				t.Fatal(err)
			}
			other, err = cell.OpenAt(other.Root, other.ID)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(filepath.Join(other.Root, cell.TranscriptPath), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := io.WriteString(file, "{\"type\":\"message\",\"role\":\"user\",\"content\":\"another computer's turn\"}\n")
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Fatalf("other turn: %v %v", writeErr, closeErr)
			}
			sealed, err := otherEngine.Seal(t.Context(), other, cellstore.TurnInfo{})
			if err != nil {
				t.Fatal(err)
			}
			otherDir := dir.For("other-computer")
			got, err := otherDir.Acquire(t.Context(), c.ID, directory.AcquireOpts{})
			if err != nil {
				t.Fatal(err)
			}
			driving := cellsync.Driving{Cell: other, Fence: got.Cell.Lease.Fence, Head: got.Cell.Head, Frames: got.Cell.Frames}
			publisher := cellsync.Publisher{Engine: synced, Store: store.Store, Dir: otherDir}
			if err := publisher.Publish(t.Context(), &driving, sealed.Turn.ID, cellsync.PublishInfo{}); err != nil {
				t.Fatal(err)
			}
			if err := otherDir.Release(t.Context(), c.ID, driving.Fence); err != nil {
				t.Fatal(err)
			}
			// Returning the recorded hold here without fetching makes this an own free chat with missing history.
			got, err = s.Dir.Acquire(t.Context(), c.ID, directory.AcquireOpts{})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Dir.Release(t.Context(), c.ID, got.Cell.Lease.Fence); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "storage on" {
			// Stop background retries so this send, rather than an approval upload, observes the differing heads.
			syncDrives.closeAll()
			permission("on")
		}
		free, err := s.Dir.Cell(t.Context(), c.ID)
		if err != nil || free.Cell.Head == "" || free.Cell.Lease.Device != s.Device.ID() || free.Cell.Lease.Expires > free.Now {
			t.Fatalf("not this computer's free recorded chat: %+v %v", free, err)
		}
		extends, err := cellstore.Extends(c, free.Cell.Head)
		if err != nil || extends != (mode != "missing saved turns") {
			t.Fatalf("fixture ancestry=%v: %v", extends, err)
		}
		if mode != "local turns ahead" {
			calls := turns.Load()
			if _, err := io.WriteString(typed, marker+"\r"); err != nil {
				t.Fatal(err)
			}
			waitStorageOffBoundary(t, "continue question or missing-turns refusal", func() bool {
				return strings.Contains(shown(), "Continue this chat here?") || mode == "missing saved turns" && strings.Contains(shown(), "this computer is missing saved turns; your local turns stay here")
			})
			latest, err := s.Dir.Cell(t.Context(), c.ID)
			if err != nil || latest.Cell.Lease.Fence != free.Cell.Lease.Fence || turns.Load() != calls {
				t.Fatalf("control acquired or sent: %+v calls=%d/%d %v", latest, turns.Load(), calls, err)
			}
			after, err := os.ReadFile(cfg.SessionFile)
			if err != nil || string(after) != string(before) {
				t.Fatalf("control changed local turns: %v", err)
			}
			break
		}
		send(marker)
		latest, err := s.Dir.Cell(t.Context(), c.ID)
		if err != nil || latest.Cell.Lease.Fence <= free.Cell.Lease.Fence || latest.Cell.Lease.Device != s.Device.ID() || latest.Cell.Head != published.Cell.Head {
			t.Fatalf("send did not return without publishing: %+v %v", latest, err)
		}
	}
	if opens.Load() != 0 {
		t.Fatalf("chat reopened %d times", opens.Load())
	}
	if got := storageNotices.Load(); got != 1 {
		t.Fatalf("storage-off sentence shown %d times", got)
	}
	if store.writes.Load() != writesAtRevoke {
		t.Fatalf("store writes after revoke=%d", store.writes.Load()-writesAtRevoke)
	}
	raw, err := os.ReadFile(cfg.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, words := range []string{"printf fixture", "answer-local-one", "answer-local-two", "answer-before-expiry"} {
		if !strings.Contains(string(raw), words) {
			t.Fatalf("local history lost %q", words)
		}
	}
}
