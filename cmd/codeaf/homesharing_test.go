package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// olderChatAt writes an older chat (a journal, no cell) last used at used.
func olderChatAt(t *testing.T, root, name, workspace string, used time.Time) string {
	t.Helper()
	dir := filepath.Join(root, "v3", "projects", "old", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	history := `{"type":"session","version":1,"id":"` + name + `"}` + "\n" + `{"type":"message","role":"user","content":"kept history"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(history), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: name, Title: name, LastUserAt: used, Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestOneProcessSharesAHome: the app engine and a terminal on the same home
// once both published its chats. Whoever holds the home's sharing lock does;
// the other takes over when the holder lets go.
func TestOneProcessSharesAHome(t *testing.T) {
	root := t.TempDir()
	app, terminal := newHomeSharing(root), newHomeSharing(root)
	if !app.claim() {
		t.Fatal("the first process could not take the home's sharing")
	}
	if terminal.claim() {
		t.Fatal("a second process on the same home also took its sharing")
	}
	if !app.claim() {
		t.Fatal("the holder lost its claim on asking again")
	}
	app.release()
	if !terminal.claim() {
		t.Fatal("the second process could not take over after the holder let go")
	}
	terminal.release()
}

// TestOlderChatsAreCountedByWhyTheyAreNotShared: a recent older chat is
// waiting for its copy, one used over 30 days ago or in the home folder is kept
// here, one whose copy failed is failing until it changes, and a chat with a
// cell or a finished copy is left to the cell count. The copy of a home-folder
// chat is kept here unless it was shared.
func TestOlderChatsAreCountedByWhyTheyAreNotShared(t *testing.T) {
	root := t.TempDir()
	person := t.TempDir()
	t.Setenv("HOME", person)
	now := time.Now()
	work := t.TempDir()
	recent := olderChatAt(t, root, "recent", work, now)
	olderChatAt(t, root, "old", work, now.Add(-45*24*time.Hour))
	olderChatAt(t, root, "home", person, now)
	failing := olderChatAt(t, root, "failing", work, now)
	celled := olderChatAt(t, root, "celled", work, now)
	if err := os.MkdirAll(filepath.Join(celled, filepath.Dir(cell.MetaPath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(celled, cell.MetaPath), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	// Copies of home-folder chats from older builds: one never shared, which
	// stays here, and one shared when it was made, which is not counted.
	homeCopy := func(name string, shared bool) {
		original := olderChatAt(t, root, name, person, now)
		moving := filepath.Join(filepath.Dir(original), ".moving")
		c, err := cell.CreateIn(moving, cell.Options{Class: cell.FilesOnly})
		if err != nil {
			t.Fatal(err)
		}
		at := cell.CopyRoot(original)
		if err := os.Rename(c.Root, at); err != nil {
			t.Fatal(err)
		}
		if c, err = cell.OpenAt(at, filepath.Base(at)); err != nil {
			t.Fatal(err)
		}
		if err := cell.StampCopyReady(at); err != nil {
			t.Fatal(err)
		}
		if shared {
			if err := cellstore.NotePublished(c, "turn"); err != nil {
				t.Fatal(err)
			}
		}
	}
	homeCopy("home-copy", false)
	homeCopy("home-copy-shared", true)
	chats := olderChats{copy: func(context.Context, string) (cell.Cell, error) {
		return cell.Cell{}, errors.New("files kept changing while saving this chat; try again")
	}}
	chats.visit(t.Context(), failing)
	got := chats.count(olderChatDirs(root), now)
	if want := (syncsetup.Unshared{Waiting: 1, Failing: 1, KeptHere: 3}); got != want {
		t.Fatalf("older chats counted %+v, want %+v (recent is %s)", got, want, recent)
	}
}

// TestTheAppEngineCopiesAndSharesOlderChats: older chats were copied only by
// the terminal, so with only the desktop app open they never reached the other
// computer. The app engine now copies a recent one and shares the copy, and
// the sync status counts the one a rule keeps here.
func TestTheAppEngineCopiesAndSharesOlderChats(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	if _, err := identity.Ensure(root); err != nil {
		t.Fatal(err)
	}
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	work := t.TempDir()
	recent := olderChatAt(t, root, "recent-session", work, time.Now())
	olderChatAt(t, root, "old-session", work, time.Now().Add(-45*24*time.Hour))
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	b.chats.publicationWake = make(chan struct{}, 1)
	round := r1PublicationRounds(t, b)
	round(time.Second)
	copied := cell.Copied(recent)
	if copied == recent {
		t.Fatal("the app engine made no copy of a recent older chat")
	}
	s, ok, err := syncsetup.Open(root)
	if err != nil || !ok {
		t.Fatalf("open: %v, %v", ok, err)
	}
	if _, err := s.Dir.Cell(t.Context(), filepath.Base(copied)); err != nil {
		t.Fatalf("the copy of an older chat was not shared: %v", err)
	}
	status, err := b.SyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if want := (appwire.SyncUnshared{Total: 1, KeptHere: 1}); status.Unshared == nil || *status.Unshared != want {
		t.Fatalf("sync status unshared = %+v, want %+v", status.Unshared, want)
	}
}

// TestTheAppEngineLeavesAHomeAnotherProcessShares: with a terminal holding
// the home's sharing, the app engine copies nothing, so the two never export and
// upload the same chat twice; it still counts what is not shared.
func TestTheAppEngineLeavesAHomeAnotherProcessShares(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	if _, err := identity.Ensure(root); err != nil {
		t.Fatal(err)
	}
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	recent := olderChatAt(t, root, "recent-session", t.TempDir(), time.Now())
	terminal := newHomeSharing(root)
	if !terminal.claim() {
		t.Fatal("terminal could not take the home's sharing")
	}
	defer terminal.release()
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	b.chats.publicationWake = make(chan struct{}, 1)
	round := r1PublicationRounds(t, b)
	round(time.Second)
	if cell.Copied(recent) != recent {
		t.Fatal("the app engine copied an older chat while another process shares the home")
	}
	status, err := b.SyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if want := (appwire.SyncUnshared{Total: 1, Waiting: 1}); status.Unshared == nil || *status.Unshared != want {
		t.Fatalf("sync status unshared = %+v, want %+v", status.Unshared, want)
	}
}

// TestKeysAreSharedBeforeTheChatBacklog: keys, memory and preferences were
// shared after the round's chat pass, and a chat pass lasts as long as its
// uploads keep moving. A computer just paired waited for this one's whole
// backlog before it got the keys.
func TestKeysAreSharedBeforeTheChatBacklog(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	var hold atomic.Bool
	release := make(chan struct{})
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hold.Load() && r.URL.Path == "/v1/store/frames" {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
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
	r1PairHomes(t, rig)
	id, err := identity.Load(rig.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if !pair.SharingPending(rig.homeB, id.ID()) {
		t.Fatal("pairing left no state pass pending")
	}
	r1SavedChat(t, rig.homeB, "a backlog that does not finish")
	hold.Store(true)
	b := &appBackend{left: make(chan struct{}), emit: newAppRecorder()}
	b.chats.publicationWake = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); b.runLocalChatPublication(ctx, time.Now, make(chan time.Time), nil) }()
	t.Cleanup(func() { close(release); cancel(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for pair.SharingPending(rig.homeB, id.ID()) {
		if time.Now().After(deadline) {
			t.Fatal("keys, memory and preferences waited for the chat backlog")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
