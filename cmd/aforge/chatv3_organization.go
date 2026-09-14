package main

import (
	"context"
	"errors"
	"os"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// v3Organization assembles the same backend for local and hosted conversations.
// Opening a chat does not create a collection database, and memory being off
// does not remove organization. Resolution reads existing owners without waking
// them or taking over their journals.
func v3Organization(ambient *session.Standing) *session.Organization {
	path := home.Join("v3", "collections.db")
	resolver := workspaceview.Resolver{World: session.ReadHome}
	if ambient != nil {
		resolver.Standing = ambient.Store
	}
	return &session.Organization{Path: path, Resolve: resolver.Resolve}
}

// v3Collections is THIS MACHINE'S FOLDERS as the three readings the folders
// place draws from: a folder's page, one row read closely, and the opening of a
// filed file. It is assembled once and handed to whichever door serves the
// surface — straight to the surface on an in-process launch, and to the engine
// ([remote.Engine.Collections]) on every launch that goes through one — so a
// surface reads the folders of the machine whose conversations it lists, and
// never its own database in place of an engine's.
//
// EVERY READING OPENS THE DATABASE FOR ITSELF AND CLOSES IT, which is
// [session.Organization]'s rule ("stores are opened for one operation, not held
// by a UI"), and NONE OF THEM CREATES IT: a home that has never made a folder
// answers a top level with nothing in it, a folder id there is not found, and a
// file is in no folder. The standing store is the conversation's own when it
// has one, and otherwise opened only when its directory is already there.
func v3Collections(ambient *session.Standing) collectionReader {
	reader := collectionReader{path: home.Join("v3", "collections.db")}
	if ambient != nil {
		reader.standing = ambient.Store
	}
	return reader
}

type collectionReader struct {
	path     string
	standing *standing.Store
}

// open answers the database, or os.ErrNotExist for a home with no folders.
func (r collectionReader) open() (*workspace.Store, workspaceview.Resolver, error) {
	store, err := workspace.OpenExisting(r.path)
	if err != nil {
		return nil, workspaceview.Resolver{}, err
	}
	resolver := workspaceview.Resolver{World: session.ReadHome, Standing: r.standing}
	if resolver.Standing == nil {
		if info, statErr := os.Stat(v3StandingRoot()); statErr == nil && info.IsDir() {
			resolver.Standing, _ = standing.Open(v3StandingRoot())
		}
	}
	return store, resolver, nil
}

func (r collectionReader) page(ctx context.Context, id string, limit int) (workspaceview.FolderPage, error) {
	store, resolver, err := r.open()
	if errors.Is(err, os.ErrNotExist) {
		if id == "" {
			return workspaceview.FolderPage{Rows: []workspaceview.FolderRow{}}, nil
		}
		return workspaceview.FolderPage{}, workspace.ErrNotFound
	}
	if err != nil {
		return workspaceview.FolderPage{}, err
	}
	defer store.Close()
	return resolver.Folder(ctx, store, id, limit)
}

func (r collectionReader) item(ctx context.Context, ref workspace.Ref) (workspaceview.FolderItem, error) {
	store, resolver, err := r.open()
	if errors.Is(err, os.ErrNotExist) {
		return workspaceview.FolderItem{}, workspace.ErrNotFound
	}
	if err != nil {
		return workspaceview.FolderItem{}, err
	}
	defer store.Close()
	return resolver.Item(ctx, store, ref)
}

func (r collectionReader) file(ctx context.Context, path string) (workspaceview.ArtifactPreview, error) {
	store, resolver, err := r.open()
	if errors.Is(err, os.ErrNotExist) {
		return workspaceview.ArtifactPreview{}, workspaceview.ErrNotFiled
	}
	if err != nil {
		return workspaceview.ArtifactPreview{}, err
	}
	defer store.Close()
	return resolver.Artifact(ctx, store, path)
}

// engine is the reader as the engine serves it.
func (r collectionReader) engine() remote.EngineCollections {
	return remote.EngineCollections{Page: r.page, Item: r.item, File: r.file}
}

// surface is the reader handed straight to a surface in this process.
func (r collectionReader) surface() tui3.CollectionSeam {
	return tui3.CollectionSeam{Page: r.page, Item: r.item, File: r.file}
}

// hostCollections is the surface's seam over a connection: every reading is the
// engine's answer, and nothing on this machine's disk is consulted.
func hostCollections(client *remote.Client) tui3.CollectionSeam {
	return tui3.CollectionSeam{Page: client.CollectionPage, Item: client.CollectionItem, File: client.CollectionFile}
}
