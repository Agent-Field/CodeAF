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

func TestFoldersAdapterSatisfiesTheSurfaceSeam(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil, so the folders tool would be absent")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, folders)
	if options.Folders == nil {
		t.Fatal("a working store must assign Options.Folders, not leave the panel unavailable")
	}
	root, err := options.Folders.RootSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 0 {
		t.Fatalf("a fresh home listed %+v", root.Folders)
	}
}

func TestStoreOpenFailureLeavesFoldersNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	path := filepath.Join(home, "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if openV3Folders() != nil {
		t.Fatal("session Folders must be nil when the store cannot open")
	}
	var options tui3.Options
	options.Folders = &foldersAdapter{} // would be a fake empty graph if we left it
	attachSurfaceFolders(&options, openV3Folders())
	if options.Folders != nil {
		t.Fatal("Options.Folders must stay nil when the store cannot open, never a fake empty graph")
	}
}

func TestSurfaceFilesAsPersonAndTheToolSeamFilesAsOrganizer(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, folders)
	if options.Folders == nil {
		t.Fatal("surface Folders is nil")
	}
	ctx := context.Background()
	created, err := options.Folders.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	if err := options.Folders.AddPlacement(ctx, created.ID, "aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	why, err := options.Folders.WhyHere(ctx, created.ID, "aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if why.Origin != workspace.OriginPerson {
		t.Fatalf("TUI origin %q, want person", why.Origin)
	}

	if err := folders.File(ctx, created.ID, "bbbbbbbbbbbbbbbb", workspace.Provenance{
		Origin: workspace.OriginPerson,
		Actor:  "model",
	}); err != nil {
		t.Fatal(err)
	}
	why, err = options.Folders.WhyHere(ctx, created.ID, "bbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if why.Origin != workspace.OriginOrganizer {
		t.Fatalf("tool-seam origin %q, want organizer", why.Origin)
	}
	if why.Origin == workspace.OriginPerson {
		t.Fatal("tool seam wrote person origin")
	}
}

func TestAdapterRenameAndNestedKindSurviveTwoParents(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := wsapi.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	adapter := newFoldersAdapter(svc)
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	var folders tui3.Folders = adapter
	billing, err := folders.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := folders.CreateFolder(ctx, "Receipts")
	if err != nil {
		t.Fatal(err)
	}
	security, err := folders.CreateFolder(ctx, "Security")
	if err != nil {
		t.Fatal(err)
	}
	nest := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "nest"}
	if err := svc.AddPlacement(ctx, billing.ID, workspace.Ref{Kind: workspace.CollectionKind, ID: receipts.ID}, nest); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, security.ID, workspace.Ref{Kind: workspace.CollectionKind, ID: receipts.ID}, nest); err != nil {
		t.Fatal(err)
	}
	chat := workspace.Ref{Kind: workspace.ConversationKind, ID: "aaaaaaaaaaaaaaaa"}
	if err := svc.AddPlacement(ctx, receipts.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson, Reason: "file"}); err != nil {
		t.Fatal(err)
	}

	assertNested := func(parent tui3.FolderView, other string) {
		t.Helper()
		_, members, snapErr := folders.FolderSnapshot(ctx, parent.ID)
		if snapErr != nil {
			t.Fatal(snapErr)
		}
		if len(members) != 1 {
			t.Fatalf("%s members %+v", parent.Name, members)
		}
		place := members[0]
		if place.Kind != string(workspace.CollectionKind) {
			t.Fatalf("%s dropped nested Kind: %+v", parent.Name, place)
		}
		if place.RefID != receipts.ID {
			t.Fatalf("%s nested ref %q", parent.Name, place.RefID)
		}
		if place.Title != "Invoices" && place.Title != "Receipts" {
			t.Fatalf("%s nested title %q", parent.Name, place.Title)
		}
		if !containsName(place.AlsoIn, other) {
			t.Fatalf("%s AlsoIn %v, want %s", parent.Name, place.AlsoIn, other)
		}
	}
	assertNested(billing, "Security")
	assertNested(security, "Billing")

	if err := folders.RenameFolder(ctx, receipts.ID, "Invoices"); err != nil {
		t.Fatal(err)
	}
	_, billingMembers, err := folders.FolderSnapshot(ctx, billing.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, securityMembers, err := folders.FolderSnapshot(ctx, security.ID)
	if err != nil {
		t.Fatal(err)
	}
	if billingMembers[0].Title != "Invoices" || securityMembers[0].Title != "Invoices" {
		t.Fatalf("rename did not survive both parents: billing=%q security=%q", billingMembers[0].Title, securityMembers[0].Title)
	}
	if billingMembers[0].Kind != string(workspace.CollectionKind) || securityMembers[0].Kind != string(workspace.CollectionKind) {
		t.Fatalf("rename dropped Kind: billing=%q security=%q", billingMembers[0].Kind, securityMembers[0].Kind)
	}
	_, receiptMembers, err := folders.FolderSnapshot(ctx, receipts.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receiptMembers) != 1 || receiptMembers[0].RefID != chat.ID {
		t.Fatalf("renamed folder lost its chat: %+v", receiptMembers)
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestOpenV3FoldersWiresTheSessionSeam(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil, so the folders tool would be absent")
	}
	listed, err := folders.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("a fresh home listed %+v", listed)
	}
	_ = folderWorld{}.ConversationIDs()
	if title := (folderWorld{}).Title("missing"); title != "" {
		t.Fatalf("missing conversation titled %q", title)
	}
}
