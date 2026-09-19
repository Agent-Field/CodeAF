package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
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
