package main

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L4-B: The existing older-chat road makes a pre-identity chat visible and
// readable on another computer in the account, leaving its original untouched.
func TestL4Fix4PreIdentityChatSyncsAfterJoining(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	local, other := newShortOlderChatHome(t), newShortOlderChatHome(t)
	t.Setenv(home.EnvVar, local)
	t.Setenv("CODEAF_PROFILE_DIR", local)
	server := httptest.NewServer(relayserveForMove(t))
	t.Cleanup(server.Close)
	t.Setenv(syncsetup.URLVar, server.URL)
	work := t.TempDir()
	bucket, err := v3ProjectDir(work)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(bucket, "before-identity")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	const journal = "{\"type\":\"session\",\"version\":1,\"id\":\"before-identity\"}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"my pre-identity turn\"}\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "before-identity", Title: "Before pairing", Workspace: work, LastUserAt: time.Unix(1_700_000_000, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Load(local); !errors.Is(err, identity.ErrNone) {
		t.Fatalf("fixture has an identity: %v", err)
	}
	account, err := identity.Ensure(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(local, account, false); err != nil {
		t.Fatal(err)
	}
	if !syncsetup.MayTalk(local) {
		t.Fatal("the newly joined account cannot sync")
	}
	place, err := movablePlace(t.Context(), session.Place{Dir: original, Workspace: work})
	if err != nil {
		t.Fatal(err)
	}
	if place.Dir == original || cell.Copied(original) != place.Dir {
		t.Fatal("the pre-identity chat was not exposed as a completed copy")
	}
	if dirs := session.BucketSessionDirs(bucket); len(dirs) != 1 || dirs[0] != place.Dir {
		t.Fatalf("Home's conversations = %v", dirs)
	}
	copied, err := cell.OpenAt(place.Dir, place.ID())
	if err != nil {
		t.Fatal(err)
	}
	localSync, ok, err := syncsetup.Open(local)
	if err != nil || !ok {
		t.Fatalf("open joined sync: %v, %v", ok, err)
	}
	engine := cellstore.EngineFor(work)
	if err := localSync.PublishCopy(t.Context(), engine, copied, "new computer", "Before pairing"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, other)
	t.Setenv("CODEAF_PROFILE_DIR", other)
	otherSync, ok, err := syncsetup.Open(other)
	if err != nil || !ok {
		t.Fatalf("open account sync: %v, %v", ok, err)
	}
	receiver := cellstore.EngineFor("")
	receiver.DataRoot = filepath.Join(other, "v3/stores")
	continuer := otherSync.Continuer(receiver, syncsetup.TakeOptions{
		DeviceName: "other computer",
		RootFor:    func(id string) string { return filepath.Join(other, "v3/projects/copied", id) },
		BucketFor:  func(string) string { return filepath.Join(other, "v3/projects/copied") },
	})
	opened, err := continuer.Take(t.Context(), copied.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(opened.Taken.Cell.Root, cell.TranscriptPath)); err != nil || !strings.Contains(string(raw), "my pre-identity turn") {
		t.Fatalf("the account lost the pre-identity chat: %s, %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(original, "transcript.jsonl")); err != nil || string(raw) != journal {
		t.Fatal("sync changed the original", err)
	}
}
