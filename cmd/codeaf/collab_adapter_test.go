package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
)

func TestWorkingStoreWiresCollabAndCorruptStoreLeavesItAbsent(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, folders)
	if options.Collab == nil {
		t.Fatal("a working collections.db must assign Options.Collab so k/c are present")
	}
	if sessionCollabOf(folders, "aaaaaaaaaaaaaaaa") == nil {
		t.Fatal("session Collab must be present so coordinate is on the belt")
	}
}

func TestCorruptStoreLeavesCollabNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	path := filepath.Join(home, "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	var options tui3.Options
	attachSurfaceFolders(&options, openV3Folders())
	if options.Collab != nil {
		t.Fatal("Options.Collab must stay nil when the store cannot open")
	}
}

func TestRealStoreCoordinateAndDeliverPersist(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	ctx := context.Background()
	from, to := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"
	collab := &sessionCollab{svc: svc, chatID: from}
	if err := collab.CoordinateSelected(ctx, []string{to}); err != nil {
		t.Fatalf("CoordinateSelected on real store: %v", err)
	}
	got, err := collab.InspectScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != wsapi.ScopeSelected || len(got.ChatIDs) != 1 || got.ChatIDs[0] != to {
		t.Fatalf("scope %+v, want selected [%s]", got, to)
	}
	receipts, err := collab.Deliver(ctx, []string{to}, "please look", "", "")
	if err != nil {
		t.Fatalf("Deliver on real store: %v", err)
	}
	if len(receipts) != 1 || receipts[0].ToChatID != to || receipts[0].DeliveryID == "" {
		t.Fatalf("receipts %+v", receipts)
	}
	held, err := svc.Workspace().GetDelivery(ctx, receipts[0].DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if held.Origin != workspace.OriginAgent {
		t.Fatalf("origin %q, want agent so a model cannot stamp person", held.Origin)
	}
	if held.State == "" {
		t.Fatal("delivery row has no state")
	}
}
