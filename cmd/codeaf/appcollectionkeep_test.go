package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

func collectionKeepEnvironment(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, filepath.Join(root, "profile"))
	bucket, err := v3ProjectDir(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, bucket
}

// Contract (g): collection-held blanks survive launch and the app's cleanup.
func TestCollectionHeldConversationSurvivesEveryBlankCleanup(t *testing.T) {
	_, bucket := collectionKeepEnvironment(t)
	held := writeV3Session(t, bucket, "collection-held", "", time.Time{})
	unheld := writeV3Session(t, bucket, "unheld", "", time.Time{})
	path := home.Join("v3", "collections.db")
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	collection, err := store.Create(context.Background(), "Kept conversations")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), collection.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: filepath.Base(held)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if v3EmptyUnassignedSession(held) || v3EmptySession(held) {
		t.Fatal("collection-held conversation is litter")
	}
	if !v3EmptyUnassignedSession(unheld) {
		t.Fatal("unheld empty conversation was retained")
	}
	v3ReapEmpty([]v3Folder{{dir: held}, {dir: unheld}}, "")
	if _, err := os.Stat(held); err != nil {
		t.Fatal("launch reap removed collection-held conversation")
	}
	if _, err := os.Stat(unheld); !os.IsNotExist(err) {
		t.Fatalf("launch reap kept unheld conversation: %v", err)
	}
	b := newFakeBackend(newAppRecorder(), nil)
	b.profileDir = config.ProfileDir()
	row, ok := appRowOnDisk(filepath.Base(held))
	if !ok {
		t.Fatal("bare row missing")
	}
	b.remember(row)
	a := b.agent(row.ID)
	b.locked(func() { a.minted = true })
	if b.reapBlank(a) {
		t.Fatal("app cleanup removed collection-held conversation")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("blank cleanup wrote collections.db")
	}
}

// Contract (g): the real launch-time sweep keeps collection-held litter and unreadable stores.
func TestCollectionHeldConversationSurvivesBackgroundSweep(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		name := "held and unheld"
		if unreadable {
			name = "unreadable store"
		}
		t.Run(name, func(t *testing.T) {
			root, bucket := collectionKeepEnvironment(t)
			held := writeV3Session(t, bucket, "collection-held", "", time.Time{})
			unheld := writeV3Session(t, bucket, "unheld", "", time.Time{})
			old := time.Now().Add(-8 * 24 * time.Hour)
			for _, dir := range []string{held, unheld} {
				meta := session.Meta{ID: filepath.Base(dir), Workspace: root, LaunchDir: root, Created: old}
				if err := session.SaveMeta(dir, meta); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(dir, old, old); err != nil {
					t.Fatal(err)
				}
			}
			path := home.Join("v3", "collections.db")
			store, err := workspace.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			collection, err := store.Create(context.Background(), "Kept conversations")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Add(context.Background(), collection.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: filepath.Base(held)}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if unreadable {
				if err := os.WriteFile(path, []byte("unreadable database"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			proc := &v3Process{}
			proc.startPlaceSweep()
			t.Cleanup(proc.stopPlaceSweep)
			select {
			case <-proc.sweepDone:
			case <-time.After(3 * time.Second):
				t.Fatal("background sweep did not finish")
			}
			if _, err := os.Stat(held); err != nil {
				t.Fatalf("background sweep removed held conversation: %v", err)
			}
			_, err = os.Stat(unheld)
			if unreadable && err != nil {
				t.Fatalf("background sweep discarded an uncertain conversation: %v", err)
			}
			if !unreadable && !os.IsNotExist(err) {
				t.Fatalf("background sweep kept unheld control: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !info.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatal("background sweep wrote collections.db")
			}
		})
	}
}

// Contract (g): missing means unheld; every unreadable database means keep.
func TestCollectionKeepMissingUnreadableAndNoWrites(t *testing.T) {
	_, bucket := collectionKeepEnvironment(t)
	dir := writeV3Session(t, bucket, "empty-chat", "", time.Time{})
	path := home.Join("v3", "collections.db")
	if collectionKeepsConversation(dir) {
		t.Fatal("missing store retained conversation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read created a store")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := []byte("not a SQLite database")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if !collectionKeepsConversation(dir) || v3EmptyUnassignedSession(dir) {
		t.Fatal("uncertainty discarded a conversation")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, bad) {
		t.Fatal("read repaired or migrated a corrupt store")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if !collectionKeepsConversation(dir) {
		t.Fatal("unopenable store discarded a conversation")
	}
}

// Contract (g): a restart recovers every identity field from a never-spoken meta.
func TestCollectionHeldConversationIsFoundAndOpenedAfterRestart(t *testing.T) {
	root, bucket := collectionKeepEnvironment(t)
	const id = "reserved-agent"
	dir := writeV3Session(t, bucket, id, "", time.Time{})
	did, err := agentdid.New()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	meta := session.Meta{ID: id, Workspace: root, Owned: true, Title: "Reserved notes", Model: "vendor/model", Archived: true, SpentUSD: 1.25, Created: created,
		AgentIdentity: &session.AgentIdentity{DID: did, Session: id}}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	store, err := workspace.Open(home.Join("v3", "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	collection, err := store.Create(context.Background(), "Reserved")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), collection.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: id}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	want := session.SessionRow{ID: id, AgentDID: did, Dir: dir, Transcript: (session.Place{Dir: dir}).Transcript(), Workspace: root, Owned: true,
		Title: meta.Title, Model: meta.Model, Archived: true, Spend: meta.SpentUSD, At: created, Created: created}
	row, ok := appRowOnDisk(id)
	if !ok || !reflect.DeepEqual(row, want) {
		t.Fatalf("identity after restart: %+v want %+v", row, want)
	}
	opened := 0
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		opened++
		if ask.Transcript != want.Transcript || ask.Workspace != root || ask.Model != meta.Model {
			t.Errorf("open identity: %+v", ask)
		}
		return &appConn{Conv: &fakeConversation{model: ask.Model}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { _ = b.Close() })
	found, err := b.find(id)
	if err != nil || found.view.DID != did || found.view.Title != meta.Title || opened != 0 {
		t.Fatalf("find: %+v %v", found, err)
	}
	view, err := b.OpenAgent(id)
	if err != nil || view.ID != id || view.DID != did || view.Title != meta.Title || view.Model != meta.Model || opened != 1 {
		t.Fatalf("agents.open: %+v %v", view, err)
	}
	if view.State == appwire.StateWorking {
		t.Fatal("opening started a turn")
	}
}
