package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func r1SavedChat(t *testing.T, home, title string) cell.Cell {
	t.Helper()
	celltest.RequireEngine(t)
	c, err := cell.CreateIn(filepath.Join(home, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(c.Root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: title, Workspace: work, Owned: true, LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"kept history\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e := cellstore.Engine{Workspace: work, DataRoot: filepath.Join(home, "v3", "stores")}
	if _, err := e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := cellstore.Await(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

// A round acknowledgement orders relay observations after the real worker has
// finished. Sampled host ticks exercise retry pauses without wall-clock gates.
func r1PublicationRounds(t *testing.T, b *appBackend) func(time.Duration) {
	t.Helper()
	at := time.Now()
	ticks := make(chan time.Time)
	settled, done := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	var clock atomic.Int64
	now := func() time.Time { return at.Add(time.Duration(clock.Load())) }
	go func() { defer close(done); b.runLocalChatPublication(ctx, now, ticks, settled) }()
	t.Cleanup(func() { cancel(); <-done })
	await := func() {
		t.Helper()
		select {
		case <-settled:
		case <-time.After(10 * time.Second):
			t.Fatal("publication round did not settle")
		}
	}
	await()
	return func(elapsed time.Duration) {
		t.Helper()
		clock.Store(int64(elapsed))
		select {
		case ticks <- at.Add(elapsed):
		case <-time.After(10 * time.Second):
			t.Fatal("publication worker stopped accepting ticks")
		}
		await()
	}
}

func r1PairHomes(t *testing.T, rig *pairRig) *pairScreenText {
	t.Helper()
	shown := rig.show(t)
	code := shown.code(t)
	joined := make(chan error, 1)
	var out *pairScreenText
	go func() { var err error; out, err = rig.joinWith(t, code, false); joined <- err }()
	shown.out.waitFor(t, `Same three words on that screen: ([a-z ]+)\?  y / n `)
	if _, err := io.WriteString(shown.typed, "y\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestR1_7PendingMergeResumesThenGoesIdle(t *testing.T) {
	celltest.Available(t)
	var down atomic.Bool
	var requests atomic.Int32
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			if down.Load() && r.URL.Path == "/v1/dir/vault" {
				http.Error(w, "state unavailable", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	rig := &pairRig{url: relay.URL, homeA: t.TempDir(), homeB: t.TempDir()}
	if _, err := identity.Ensure(rig.homeA); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", rig.homeB)
	t.Setenv(syncsetup.URLVar, relay.URL)
	v, err := keys.Open(rig.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("kept", keys.Entry{Name: "KEY", Value: "kept"}); err != nil {
		t.Fatal(err)
	}
	r1PairHomes(t, rig)
	id, err := identity.Load(rig.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if !pair.SharingPending(rig.homeB, id.ID()) {
		t.Fatal("approved merge lost its pending state pass")
	}
	down.Store(true)
	rec := newAppRecorder()
	b := &appBackend{left: make(chan struct{}), emit: rec}
	round := r1PublicationRounds(t, b)
	rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
		return event == appwire.EventSyncChanged && strings.Contains(data.(appwire.SyncChanged).Sync.Reason, "Saved keys, memory and preferences could not be shared")
	})
	if !pair.SharingPending(rig.homeB, id.ID()) {
		t.Fatal("failed state pass was marked complete")
	}
	down.Store(false)
	round(syncsetup.DefaultInterval)
	if pair.SharingPending(rig.homeB, id.ID()) {
		t.Fatal("successful state retry remained pending")
	}
	before := requests.Load()
	for i := 2; i <= 4; i++ {
		round(time.Duration(i) * syncsetup.DefaultInterval)
	}
	if got := requests.Load(); got != before {
		t.Fatalf("already-merged idle app sent %d additional requests", got-before)
	}
}

// M5 and the relay idle contract: a paired home with no unfinished merge
// must generate zero requests from the new publication/state worker.
func TestR1_7IdlePublicationSendsNoPeriodicRequests(t *testing.T) {
	celltest.Available(t)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); next.ServeHTTP(w, r) })
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	round := r1PublicationRounds(t, b)
	for i := 1; i <= 3; i++ {
		round(time.Duration(i) * syncsetup.DefaultInterval)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("idle publication worker sent %d relay requests, want zero", got)
	}
}

func TestR1_6InventoryDoesNotWaitForPublication(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	c := r1SavedChat(t, home, "blocked upload")
	blocked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/cells/"+c.ID {
				once.Do(func() { close(blocked) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	b, _, _ := appDeviceBackend(t, home)
	<-blocked
	listed := make(chan error, 1)
	go func() { _, err := b.ListChats(); listed <- err }()
	select {
	case err := <-listed:
		if err != nil {
			t.Error(err)
		}
		listed <- err
	case <-time.After(10 * time.Second):
		t.Error("chats.list deadlocked behind a background publication request")
	}
	close(release)
	if err := <-listed; err != nil {
		t.Error(err)
	}
}

// M5: one permanently failing chat must not republish completed chats or
// retry the failed request at every interval.
func TestR1_6CompletedChatsAreNotRetried(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	good := r1SavedChat(t, home, "completed")
	bad := r1SavedChat(t, home, "pending")
	var goodReads, badReads atomic.Int32
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/cells/"+good.ID {
				goodReads.Add(1)
			}
			if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/cells/"+bad.ID {
				badReads.Add(1)
				http.Error(w, "one chat unavailable", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	round := r1PublicationRounds(t, b)
	appSyncWait(t, "initial publication", func() bool { return goodReads.Load() > 0 && badReads.Load() > 0 })
	first := goodReads.Load()
	for i := 1; i <= 3; i++ {
		round(time.Duration(i) * syncsetup.DefaultInterval)
	}
	if got := goodReads.Load(); got != first {
		t.Errorf("completed chat requested again: %d -> %d", first, got)
	}
	if got := badReads.Load(); got < 2 || got > 3 {
		t.Errorf("failed chat attempts over three intervals = %d, want 2..3 with backoff", got)
	}
}

func TestR1_6SlowFailureWaitsAfterCompletion(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	c := r1SavedChat(t, home, "slow failure")
	started := time.Now()
	var elapsed atomic.Int64
	var attempts atomic.Int32
	now := func() time.Time { return started.Add(time.Duration(elapsed.Load())) }
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/cells/"+c.ID {
				attempts.Add(1)
				elapsed.Store(int64(20 * time.Second))
				http.Error(w, "request took twenty sampled seconds", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	s := appDeviceSync(t, home)
	if err := s.PublishLocalChats(t.Context(), now, nil); err == nil {
		t.Fatal("expected publication failure")
	}
	if err := s.PublishLocalChats(t.Context(), now, nil); err == nil {
		t.Fatal("pending failure disappeared")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("slow failure retried without its pause: %d attempts", got)
	}
}

// M2/R1.6: pairing discovers chats saved since startup without retrying
// completed chats or shortening a pending chat's retry pause.
func TestR2_PairingRefreshKeepsPublicationProgress(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	good := r1SavedChat(t, home, "completed")
	bad := r1SavedChat(t, home, "pending")
	var goodReads, badReads, newReads atomic.Int32
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/dir/cells/") {
				switch r.URL.Path {
				case "/v1/dir/cells/" + good.ID:
					goodReads.Add(1)
				case "/v1/dir/cells/" + bad.ID:
					badReads.Add(1)
					http.Error(w, "one chat unavailable", http.StatusServiceUnavailable)
					return
				default:
					newReads.Add(1)
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	b.chats.publicationWake = make(chan struct{}, 1)
	settled, done := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	at := time.Now()
	go func() {
		defer close(done)
		b.runLocalChatPublication(ctx, func() time.Time { return at }, nil, settled)
	}()
	t.Cleanup(func() { cancel(); <-done })
	await := func() {
		t.Helper()
		select {
		case <-settled:
		case <-time.After(10 * time.Second):
			t.Fatal("publication did not settle")
		}
	}
	await()
	r1SavedChat(t, home, "saved after startup")
	b.chats.wakePublication()
	await()
	if goodReads.Load() != 1 || badReads.Load() != 1 || newReads.Load() != 1 {
		t.Fatalf("pairing refresh requests: completed=%d pending=%d new=%d; want 1 each", goodReads.Load(), badReads.Load(), newReads.Load())
	}
}

// M1/M2: the recent-chat policy excludes a 45-day-old original, but opening
// it takes the ordinary conversion and real-drive publication path.
func TestR1_5OpeningAnOlderChatSharesIt(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	relay := appSyncRelay(t)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	t.Setenv(syncsetup.URLVar, relay.URL)
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	original := filepath.Join(home, "v3", "projects", "old", "old-session")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	history := "{\"type\":\"session\",\"version\":1,\"id\":\"old-session\"}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"45 days ago\"}\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(history), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "old-session", Title: "Old chat", LastUserAt: time.Now().Add(-45 * 24 * time.Hour), Workspace: work}); err != nil {
		t.Fatal(err)
	}
	older := olderChats{copy: func(context.Context, string) (cell.Cell, error) {
		t.Fatal("background must skip a 45-day-old chat")
		return cell.Cell{}, nil
	}}
	if _, _, ok := older.visit(t.Context(), original); ok {
		t.Fatal("background copied an old chat")
	}
	place, err := movablePlace(t.Context(), session.Place{Dir: original, Workspace: work})
	if err != nil {
		t.Fatal(err)
	}
	if place.Dir == original {
		t.Fatal("opening did not convert old chat")
	}
	c, err := cell.OpenAt(place.Dir, place.ID())
	if err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, home)
	drive, err := s.Drive(t.Context(), cellstore.EngineFor(place.Workspace), c, syncsetup.DriveOptions{DeviceName: "joining computer"})
	if err != nil {
		t.Fatal(err)
	}
	defer drive.Close(context.Background())
	appSyncWait(t, "opened old chat appears at relay", func() bool { _, err := s.Dir.Cell(t.Context(), c.ID); return err == nil })
	if raw, err := os.ReadFile(filepath.Join(original, "transcript.jsonl")); err != nil || string(raw) != history {
		t.Fatalf("original changed: %q, %v", raw, err)
	}
}

// M1/M2/M5: the terminal explains its deferred sharing, and the real next-start
// background door publishes the chat the joining computer brought.
func TestR1_4TerminalExplainsAndResumesDeferredSharing(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeB)
	t.Setenv(syncsetup.URLVar, rig.url)
	t.Cleanup(cellstore.StopSpawned)
	c := r1SavedChat(t, rig.homeB, "brought chat")
	out := r1PairHomes(t, rig)
	if !strings.Contains(out.String(), "Sharing finishes the next time codeaf runs.\n") {
		t.Fatalf("terminal omitted when sharing finishes: %s", out.String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); runOlderChats(ctx) }()
	defer func() { cancel(); <-done }()
	s, ok, err := syncsetup.Open(rig.homeA)
	if err != nil || !ok {
		t.Fatalf("open other home: %v, %v", ok, err)
	}
	appSyncWait(t, "next start shares brought chat", func() bool {
		_, err := s.Dir.Cell(t.Context(), c.ID)
		return err == nil
	})
}
